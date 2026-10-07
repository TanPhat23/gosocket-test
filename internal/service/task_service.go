package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/ws"
)

// TaskRunner is aliased from queue.TaskRunner.
type TaskRunner = queue.TaskRunner

// TaskService defines the business operations for tasks.
type TaskService interface {
	GetTask(ctx context.Context, taskID string) (*models.Task, error)
	ListTasks(ctx context.Context, projectID string) ([]models.Task, error)
	ListSubtasks(ctx context.Context, parentTaskID string) ([]models.Task, error)
	CreateTask(ctx context.Context, req *dto.CreateTaskRequest) (*models.Task, error)
	UpdateStatus(ctx context.Context, taskID string, status models.TaskStatus) error
	CancelTask(ctx context.Context, taskID string) error
	ExecuteTask(ctx context.Context, taskID string, runner TaskRunner) error
}

type taskService struct {
	taskRepo    repository.TaskRepository
	attemptRepo repository.AttemptRepository
	queueClient queue.Client
	hub         *ws.Hub
}

// NewTaskService creates a new TaskService implementation.
func NewTaskService(
	taskRepo repository.TaskRepository,
	attemptRepo repository.AttemptRepository,
	queueClient queue.Client,
	hub *ws.Hub,
) TaskService {
	return &taskService{
		taskRepo:    taskRepo,
		attemptRepo: attemptRepo,
		queueClient: queueClient,
		hub:         hub,
	}
}

// GetTask retrieves a task by ID.
func (s *taskService) GetTask(ctx context.Context, taskID string) (*models.Task, error) {
	return s.taskRepo.GetByID(ctx, taskID)
}

// ListTasks lists tasks, optionally filtered by project ID.
func (s *taskService) ListTasks(ctx context.Context, projectID string) ([]models.Task, error) {
	return s.taskRepo.List(ctx, projectID)
}

// ListSubtasks lists subtasks for a given parent task ID.
func (s *taskService) ListSubtasks(ctx context.Context, parentTaskID string) ([]models.Task, error) {
	return s.taskRepo.ListByParentID(ctx, parentTaskID)
}

// CreateTask validates input, creates a new task in DB, enqueues to Asynq queue if queued, and broadcasts status via WebSocket.
func (s *taskService) CreateTask(ctx context.Context, req *dto.CreateTaskRequest) (*models.Task, error) {
	if req == nil {
		return nil, errors.New("request cannot be nil")
	}

	priority := models.TaskPriority(req.Priority)
	if !priority.IsValid() {
		priority = models.PriorityNormal
	}

	task := &models.Task{
		ID:           common.NewID(),
		ProjectID:    req.ProjectID,
		ParentTaskID: req.ParentTaskID,
		Title:        req.Title,
		Prompt:       req.Prompt,
		AssigneeID:   req.AssigneeID,
		AssigneeType: req.AssigneeType,
		Priority:     priority,
		Model:        req.Model,
		Skills:       models.StringArray(req.Skills),
		MCPServers:   models.StringArray(req.MCPServers),
		Status:       models.TaskStatusQueued,
	}

	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	if s.queueClient != nil {
		asynqID, err := s.queueClient.EnqueueTask(ctx, task)
		if err == nil {
			task.AsynqTaskID = &asynqID
			_ = s.taskRepo.Update(ctx, task)
		}
	}

	if s.hub != nil {
		_ = s.hub.BroadcastTaskStatus(task.ID, task.Status)
	}

	return task, nil
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

// ExecuteTask orchestrates the task execution lifecycle: status transition, attempt tracking, runner execution, and status completion.
func (s *taskService) ExecuteTask(ctx context.Context, taskID string, runner TaskRunner) error {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to find task %q: %w", taskID, err)
	}

	// If cancelled, skip execution
	if task.Status == models.TaskStatusCancelled {
		return nil
	}

	// Update task status to in_progress and record heartbeat
	now := time.Now().UTC()
	task.Status = models.TaskStatusInProgress
	task.HeartbeatAt = &now
	if err := s.taskRepo.Update(ctx, task); err != nil {
		return fmt.Errorf("failed to mark task as in_progress: %w", err)
	}
	if s.hub != nil {
		_ = s.hub.BroadcastTaskStatus(task.ID, models.TaskStatusInProgress)
	}

	// Count existing attempts for this task
	count, err := s.attemptRepo.CountByTaskID(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("failed to count task attempts: %w", err)
	}

	startedAt := time.Now().UTC()
	attempt := &models.TaskAttempt{
		ID:            common.NewID(),
		TaskID:        task.ID,
		AttemptNumber: int(count) + 1,
		Status:        models.AttemptRunning,
		StartedAt:     &startedAt,
	}
	if err := s.attemptRepo.Create(ctx, attempt); err != nil {
		return fmt.Errorf("failed to create task attempt: %w", err)
	}

	var runErr error
	if runner != nil {
		runErr = runner.RunTask(ctx, task, attempt)
	}

	finishedAt := time.Now().UTC()
	attempt.FinishedAt = &finishedAt

	if runErr == nil {
		attempt.Status = models.AttemptSucceeded
		_ = s.attemptRepo.Update(ctx, attempt)
		_ = s.UpdateStatus(ctx, task.ID, models.TaskStatusReview)
		return nil
	}

	failReason := runErr.Error()
	attempt.Status = models.AttemptFailed
	attempt.FailureReason = &failReason
	_ = s.attemptRepo.Update(ctx, attempt)
	_ = s.UpdateStatus(ctx, task.ID, models.TaskStatusFailed)

	return runErr
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
