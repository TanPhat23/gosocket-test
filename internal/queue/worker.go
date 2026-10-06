package queue

import (
	"context"
	"errors"
	"fmt"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/models"

	"github.com/hibiken/asynq"
)

// TaskRunner defines the interface for executing a task and tracking its attempt.
type TaskRunner interface {
	RunTask(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) error
}

// TaskExecutor defines the contract for executing a task by ID.
type TaskExecutor interface {
	ExecuteTask(ctx context.Context, taskID string, runner TaskRunner) error
}

// WorkerServer wraps an Asynq Server and dispatches tasks to execution handlers.
type WorkerServer struct {
	server      *asynq.Server
	mux         *asynq.ServeMux
	executor    TaskExecutor
	runner      TaskRunner
	concurrency int
}

// NewWorkerServer creates a new WorkerServer configured with bounded concurrency and priority queue distribution.
func NewWorkerServer(cfg *config.Config, executor TaskExecutor, runner TaskRunner) *WorkerServer {
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
		executor:    executor,
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
	payload, err := common.Unmarshal[TaskExecutionPayload](asynqTask.Payload())
	if err != nil {
		return fmt.Errorf("failed to unmarshal task execution payload: %w", err)
	}

	if payload.TaskID == "" {
		return errors.New("payload task_id is empty")
	}

	if ws.executor == nil {
		return nil
	}

	return ws.executor.ExecuteTask(ctx, payload.TaskID, ws.runner)
}
