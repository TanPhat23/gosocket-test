package service

import (
	"context"
	"fmt"
	"time"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/ws"
)

// TaskService defines the business operations for tasks.
type TaskService interface {
	GetTask(ctx context.Context, taskID string) (*models.Task, error)
	UpdateStatus(ctx context.Context, taskID string, status models.TaskStatus) error
	CancelTask(ctx context.Context, taskID string) error
}

type taskService struct {
	taskRepo    repository.TaskRepository
	queueClient queue.Client
	hub         *ws.Hub
}

// NewTaskService creates a new TaskService implementation.
func NewTaskService(taskRepo repository.TaskRepository, queueClient queue.Client, hub *ws.Hub) TaskService {
	return &taskService{
		taskRepo:    taskRepo,
		queueClient: queueClient,
		hub:         hub,
	}
}

// GetTask retrieves a task by ID.
func (s *taskService) GetTask(ctx context.Context, taskID string) (*models.Task, error) {
	return s.taskRepo.GetByID(ctx, taskID)
}

// UpdateStatus updates the task status in the database and broadcasts the change via WebSocket.
func (s *taskService) UpdateStatus(ctx context.Context, taskID string, status models.TaskStatus) error {
	if !status.IsValid() {
		return fmt.Errorf("invalid status: %s", status)
	}

	if err := s.taskRepo.UpdateStatus(ctx, taskID, status); err != nil {
		return fmt.Errorf("task not found or update failed: %w", err)
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
	logRepo repository.LogRepository
	hub     *ws.Hub
}

// NewLogService creates a new LogService implementation.
func NewLogService(logRepo repository.LogRepository, hub *ws.Hub) LogService {
	return &logService{
		logRepo: logRepo,
		hub:     hub,
	}
}

// AppendLog persists a chunk of log to TaskLog and broadcasts to WebSocket subscribers.
func (s *logService) AppendLog(ctx context.Context, taskID string, attemptID *string, chunk string) error {
	logEntry := models.TaskLog{
		TaskID:    taskID,
		AttemptID: attemptID,
		LogChunk:  chunk,
		Timestamp: time.Now().UTC(),
	}

	if err := s.logRepo.Append(ctx, &logEntry); err != nil {
		return fmt.Errorf("failed to persist log entry: %w", err)
	}

	if s.hub != nil {
		_ = s.hub.BroadcastTaskLog(taskID, chunk)
	}

	return nil
}

// GetLogs returns all persisted log entries for a given task ID ordered by timestamp.
func (s *logService) GetLogs(ctx context.Context, taskID string) ([]models.TaskLog, error) {
	return s.logRepo.ListByTaskID(ctx, taskID)
}
