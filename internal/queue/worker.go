package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"agent-task-manager/internal/config"
	"agent-task-manager/internal/models"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

// TaskRunner defines the interface for executing a task and tracking its attempt.
type TaskRunner interface {
	RunTask(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) error
}

// WorkerServer wraps an Asynq Server and dispatches tasks to execution handlers.
type WorkerServer struct {
	server      *asynq.Server
	mux         *asynq.ServeMux
	db          *gorm.DB
	runner      TaskRunner
	concurrency int
}

// NewWorkerServer creates a new WorkerServer configured with bounded concurrency and priority queue distribution.
func NewWorkerServer(cfg *config.Config, db *gorm.DB, runner TaskRunner) *WorkerServer {
	concurrency := 4
	var redisOpt asynq.RedisClientOpt

	if cfg != nil {
		if cfg.MaxConcurrentTasks > 0 {
			concurrency = cfg.MaxConcurrentTasks
		}
		redisOpt = asynq.RedisClientOpt{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
		}
	}

	asynqCfg := asynq.Config{
		Concurrency: concurrency,
		Queues: map[string]int{
			QueueCritical: 6,
			QueueDefault:  3,
			QueueLow:      1,
		},
	}

	srv := asynq.NewServer(redisOpt, asynqCfg)
	mux := asynq.NewServeMux()

	ws := &WorkerServer{
		server:      srv,
		mux:         mux,
		db:          db,
		runner:      runner,
		concurrency: concurrency,
	}

	mux.HandleFunc(TypeExecuteAgentTask, ws.handleExecuteAgentTask)

	return ws
}

// Concurrency returns the configured max concurrent task workers.
func (ws *WorkerServer) Concurrency() int {
	return ws.concurrency
}

// Handler returns the underlying Asynq ServeMux (useful for testing and inspection).
func (ws *WorkerServer) Handler() asynq.Handler {
	return ws.mux
}

// Start begins processing tasks in the Asynq worker server (non-blocking).
func (ws *WorkerServer) Start() error {
	if ws.server == nil {
		return errors.New("server is not initialized")
	}
	return ws.server.Start(ws.mux)
}

// Run starts the Asynq worker server and blocks until stopped.
func (ws *WorkerServer) Run() error {
	if ws.server == nil {
		return errors.New("server is not initialized")
	}
	return ws.server.Run(ws.mux)
}

// Stop signals the worker server to stop pulling new tasks and wait for active tasks to finish.
func (ws *WorkerServer) Stop() {
	if ws.server != nil {
		ws.server.Stop()
	}
}

// Shutdown shuts down the worker server gracefully.
func (ws *WorkerServer) Shutdown() {
	if ws.server != nil {
		ws.server.Shutdown()
	}
}

func (ws *WorkerServer) handleExecuteAgentTask(ctx context.Context, asynqTask *asynq.Task) error {
	var payload TaskExecutionPayload
	if err := json.Unmarshal(asynqTask.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal task execution payload: %w", err)
	}

	if payload.TaskID == "" {
		return errors.New("payload task_id is empty")
	}

	if ws.db == nil {
		// If no DB is attached, direct dispatch to runner if runner is provided
		if ws.runner != nil {
			dummyTask := &models.Task{ID: payload.TaskID, ProjectID: payload.ProjectID}
			dummyAttempt := &models.TaskAttempt{
				ID:            uuid.New().String(),
				TaskID:        payload.TaskID,
				AttemptNumber: 1,
				Status:        models.AttemptRunning,
			}
			return ws.runner.RunTask(ctx, dummyTask, dummyAttempt)
		}
		return nil
	}

	var task models.Task
	if err := ws.db.WithContext(ctx).First(&task, "id = ?", payload.TaskID).Error; err != nil {
		return fmt.Errorf("failed to find task %q: %w", payload.TaskID, err)
	}

	// If cancelled, skip execution
	if task.Status == models.TaskStatusCancelled {
		return nil
	}

	// Update task status to in_progress and record heartbeat
	now := time.Now().UTC()
	task.Status = models.TaskStatusInProgress
	task.HeartbeatAt = &now
	if err := ws.db.WithContext(ctx).Model(&models.Task{}).
		Where("id = ?", task.ID).
		Updates(map[string]any{
			"status":       models.TaskStatusInProgress,
			"heartbeat_at": now,
		}).Error; err != nil {
		return fmt.Errorf("failed to mark task as in_progress: %w", err)
	}

	// Count existing attempts for this task
	var count int64
	if err := ws.db.WithContext(ctx).Model(&models.TaskAttempt{}).
		Where("task_id = ?", task.ID).
		Count(&count).Error; err != nil {
		return fmt.Errorf("failed to count task attempts: %w", err)
	}

	startedAt := time.Now().UTC()
	attempt := models.TaskAttempt{
		ID:            uuid.New().String(),
		TaskID:        task.ID,
		AttemptNumber: int(count) + 1,
		Status:        models.AttemptRunning,
		StartedAt:     &startedAt,
	}
	if err := ws.db.WithContext(ctx).Create(&attempt).Error; err != nil {
		return fmt.Errorf("failed to create task attempt: %w", err)
	}

	var runErr error
	if ws.runner != nil {
		runErr = ws.runner.RunTask(ctx, &task, &attempt)
	}

	finishedAt := time.Now().UTC()
	attempt.FinishedAt = &finishedAt

	if runErr == nil {
		attempt.Status = models.AttemptSucceeded
		_ = ws.db.WithContext(ctx).Model(&models.TaskAttempt{}).
			Where("id = ?", attempt.ID).
			Updates(map[string]any{
				"status":      models.AttemptSucceeded,
				"finished_at": finishedAt,
			})

		_ = ws.db.WithContext(ctx).Model(&models.Task{}).
			Where("id = ?", task.ID).
			Updates(map[string]any{
				"status": models.TaskStatusReview,
			})
		return nil
	}

	failReason := runErr.Error()
	attempt.Status = models.AttemptFailed
	attempt.FailureReason = &failReason
	_ = ws.db.WithContext(ctx).Model(&models.TaskAttempt{}).
		Where("id = ?", attempt.ID).
		Updates(map[string]any{
			"status":         models.AttemptFailed,
			"finished_at":    finishedAt,
			"failure_reason": failReason,
		})

	_ = ws.db.WithContext(ctx).Model(&models.Task{}).
		Where("id = ?", task.ID).
		Updates(map[string]any{
			"status": models.TaskStatusFailed,
		})

	return runErr
}
