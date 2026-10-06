package queue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/models"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

// TaskEnqueuer abstracts the Asynq task enqueue operation.
type TaskEnqueuer interface {
	Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

// TaskInspector abstracts the Asynq task inspection and cancellation operation.
type TaskInspector interface {
	DeleteTask(queue string, id string) error
	Close() error
}

// Client defines the interface for interacting with the distributed task queue engine.
type Client interface {
	EnqueueTask(ctx context.Context, task *models.Task) (string, error)
	CancelTask(ctx context.Context, queueName string, asynqTaskID string) error
	Close() error
}

type asynqClient struct {
	client    TaskEnqueuer
	inspector TaskInspector
	db        *gorm.DB
	cfg       *config.Config
}

// NewClient initializes a new Asynq-backed task queue Client.
func NewClient(cfg *config.Config, db *gorm.DB) (Client, error) {
	if cfg == nil {
		return nil, errors.New("config cannot be nil")
	}

	redisOpt := asynq.RedisClientOpt{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	}

	client := asynq.NewClient(redisOpt)
	inspector := asynq.NewInspector(redisOpt)

	return NewClientWithInterfaces(client, inspector, cfg, db), nil
}

// NewClientWithInterfaces allows dependency injection of TaskEnqueuer and TaskInspector (e.g. for testing).
func NewClientWithInterfaces(enqueuer TaskEnqueuer, inspector TaskInspector, cfg *config.Config, db *gorm.DB) Client {
	return &asynqClient{
		client:    enqueuer,
		inspector: inspector,
		db:        db,
		cfg:       cfg,
	}
}

// EnqueueTask serializes task data into an Asynq task and enqueues it with proper priority queue and retry settings.
func (c *asynqClient) EnqueueTask(ctx context.Context, task *models.Task) (string, error) {
	if task == nil {
		return "", errors.New("task cannot be nil")
	}
	if task.ID == "" {
		return "", errors.New("task ID cannot be empty")
	}

	payload := TaskExecutionPayload{
		TaskID:    task.ID,
		ProjectID: task.ProjectID,
	}

	payloadBytes, err := common.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal task execution payload: %w", err)
	}

	asynqTask := asynq.NewTask(TypeExecuteAgentTask, payloadBytes)
	queueTier := ResolveQueueTier(task.Priority)

	timeout := 60 * time.Minute
	if c.cfg != nil && c.cfg.TaskTimeoutMinutes > 0 {
		timeout = time.Duration(c.cfg.TaskTimeoutMinutes) * time.Minute
	}

	opts := []asynq.Option{
		asynq.Queue(queueTier),
		asynq.MaxRetry(3),
		asynq.Timeout(timeout),
	}

	info, err := c.client.Enqueue(asynqTask, opts...)
	if err != nil {
		return "", fmt.Errorf("failed to enqueue task to asynq: %w", err)
	}

	asynqID := info.ID
	task.AsynqTaskID = &asynqID

	if c.db != nil {
		dbCtx := c.db.WithContext(ctx)
		if err := dbCtx.Model(&models.Task{}).Where("id = ?", task.ID).Update("asynq_task_id", asynqID).Error; err != nil {
			return asynqID, fmt.Errorf("failed to update task asynq_task_id in db: %w", err)
		}
	}

	return asynqID, nil
}

// CancelTask invokes inspector.DeleteTask to remove/cancel a task in the specified queue.
func (c *asynqClient) CancelTask(ctx context.Context, queueName string, asynqTaskID string) error {
	if asynqTaskID == "" {
		return errors.New("asynq task ID cannot be empty")
	}
	if queueName == "" {
		queueName = QueueDefault
	}

	if err := c.inspector.DeleteTask(queueName, asynqTaskID); err != nil {
		return fmt.Errorf("failed to cancel asynq task: %w", err)
	}

	return nil
}

// Close closes the underlying Asynq client and inspector connections.
func (c *asynqClient) Close() error {
	var errs []error
	if c.client != nil {
		if err := c.client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.inspector != nil {
		if err := c.inspector.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
