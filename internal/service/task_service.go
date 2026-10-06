package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"
	"agent-task-manager/internal/ws"
	"gorm.io/gorm"
)

// TaskService defines the business operations for tasks.
type TaskService interface {
	GetTask(ctx context.Context, taskID string) (*models.Task, error)
	UpdateStatus(ctx context.Context, taskID string, status models.TaskStatus) error
	CancelTask(ctx context.Context, taskID string) error
}

type taskService struct {
	db          *gorm.DB
	queueClient queue.Client
	hub         *ws.Hub
}

// NewTaskService creates a new TaskService implementation.
func NewTaskService(db *gorm.DB, queueClient queue.Client, hub *ws.Hub) TaskService {
	return &taskService{
		db:          db,
		queueClient: queueClient,
		hub:         hub,
	}
}

// GetTask retrieves a task by ID.
func (s *taskService) GetTask(ctx context.Context, taskID string) (*models.Task, error) {
	if taskID == "" {
		return nil, errors.New("task ID cannot be empty")
	}

	var task models.Task
	if err := s.db.WithContext(ctx).First(&task, "id = ?", taskID).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// UpdateStatus updates the task status in the database and broadcasts the change via WebSocket.
func (s *taskService) UpdateStatus(ctx context.Context, taskID string, status models.TaskStatus) error {
	if taskID == "" {
		return errors.New("task ID cannot be empty")
	}
	if !status.IsValid() {
		return fmt.Errorf("invalid status: %s", status)
	}

	res := s.db.WithContext(ctx).Model(&models.Task{}).
		Where("id = ?", taskID).
		Updates(map[string]any{
			"status":     status,
			"updated_at": time.Now().UTC(),
		})

	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("task not found: %s", taskID)
	}

	if s.hub != nil {
		_ = s.hub.BroadcastTaskStatus(taskID, status)
	}

	return nil
}

// CancelTask cancels an ongoing or queued task, notifying the queue client and updating DB status.
func (s *taskService) CancelTask(ctx context.Context, taskID string) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	if s.queueClient != nil && task.AsynqTaskID != nil && *task.AsynqTaskID != "" {
		tier := queue.ResolveQueueTier(task.Priority)
		_ = s.queueClient.CancelTask(ctx, tier, *task.AsynqTaskID)
	}

	return s.UpdateStatus(ctx, taskID, models.TaskStatusCancelled)
}

// LogService defines the business operations for appending and streaming task logs.
type LogService interface {
	AppendLog(ctx context.Context, taskID string, attemptID *string, chunk string) error
	GetLogs(ctx context.Context, taskID string) ([]models.TaskLog, error)
}

type logService struct {
	db  *gorm.DB
	hub *ws.Hub
}

// NewLogService creates a new LogService implementation.
func NewLogService(db *gorm.DB, hub *ws.Hub) LogService {
	return &logService{
		db:  db,
		hub: hub,
	}
}

// AppendLog persists a chunk of log to TaskLog and broadcasts to WebSocket subscribers.
func (s *logService) AppendLog(ctx context.Context, taskID string, attemptID *string, chunk string) error {
	if taskID == "" {
		return errors.New("task ID cannot be empty")
	}

	logEntry := models.TaskLog{
		TaskID:    taskID,
		AttemptID: attemptID,
		LogChunk:  chunk,
		Timestamp: time.Now().UTC(),
	}

	if err := s.db.WithContext(ctx).Create(&logEntry).Error; err != nil {
		return fmt.Errorf("failed to persist log entry: %w", err)
	}

	if s.hub != nil {
		_ = s.hub.BroadcastTaskLog(taskID, chunk)
	}

	return nil
}

// GetLogs returns all persisted log entries for a given task ID ordered by timestamp.
func (s *logService) GetLogs(ctx context.Context, taskID string) ([]models.TaskLog, error) {
	if taskID == "" {
		return nil, errors.New("task ID cannot be empty")
	}

	var logs []models.TaskLog
	err := s.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("timestamp ASC, id ASC").
		Find(&logs).Error
	if err != nil {
		return nil, err
	}
	return logs, nil
}
