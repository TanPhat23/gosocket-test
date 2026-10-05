// internal/queue/client_test.go
package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"agent-task-manager/internal/config"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"

	"github.com/glebarez/sqlite"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

func TestQueuePriorityTiers(t *testing.T) {
	cases := []struct {
		priority models.TaskPriority
		expected string
	}{
		{models.PriorityUrgent, queue.QueueCritical},
		{models.PriorityHigh, queue.QueueCritical},
		{models.PriorityNormal, queue.QueueDefault},
		{models.PriorityLow, queue.QueueLow},
		{models.TaskPriority(999), queue.QueueDefault}, // fallback unknown
	}

	for _, tc := range cases {
		tier := queue.ResolveQueueTier(tc.priority)
		if tier != tc.expected {
			t.Errorf("for priority %d expected queue %s, got %s", tc.priority, tc.expected, tier)
		}
	}
}

func TestTaskExecutionPayloadMarshaling(t *testing.T) {
	payload := queue.TaskExecutionPayload{
		TaskID:    "task-123",
		ProjectID: "proj-456",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	var unmarshaled queue.TaskExecutionPayload
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if unmarshaled.TaskID != "task-123" || unmarshaled.ProjectID != "proj-456" {
		t.Fatalf("unexpected unmarshaled payload: %+v", unmarshaled)
	}
}

// Mock TaskEnqueuer
type mockEnqueuer struct {
	lastTask *asynq.Task
	lastOpts []asynq.Option
	returnID string
	err      error
	closed   bool
}

func (m *mockEnqueuer) Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.lastTask = task
	m.lastOpts = opts
	return &asynq.TaskInfo{
		ID:    m.returnID,
		Queue: "default",
	}, nil
}

func (m *mockEnqueuer) Close() error {
	m.closed = true
	return nil
}

// Mock TaskInspector
type mockInspector struct {
	lastQueue string
	lastID    string
	err       error
	closed    bool
}

func (m *mockInspector) DeleteTask(queue string, id string) error {
	if m.err != nil {
		return m.err
	}
	m.lastQueue = queue
	m.lastID = id
	return nil
}

func (m *mockInspector) Close() error {
	m.closed = true
	return nil
}

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite test db: %v", err)
	}
	err = db.Exec(`CREATE TABLE tasks (
		id TEXT PRIMARY KEY,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		heartbeat_at DATETIME,
		integration_id TEXT,
		asynq_task_id TEXT,
		external_id TEXT,
		external_url TEXT,
		pr_url TEXT,
		worktree_path TEXT,
		branch_name TEXT,
		model TEXT,
		container_id TEXT,
		exit_code INTEGER,
		skills TEXT NOT NULL DEFAULT '[]',
		mcp_servers TEXT NOT NULL DEFAULT '[]',
		project_id TEXT NOT NULL,
		title TEXT NOT NULL,
		prompt TEXT NOT NULL,
		assignee_id TEXT NOT NULL,
		status TEXT NOT NULL,
		assignee_type TEXT NOT NULL DEFAULT 'agent',
		priority INTEGER NOT NULL DEFAULT 50
	)`).Error
	if err != nil {
		t.Fatalf("failed to create tasks table: %v", err)
	}
	return db
}

func TestEnqueueTaskSuccess(t *testing.T) {
	db := setupTestDB(t)
	cfg := &config.Config{
		TaskTimeoutMinutes: 45,
	}

	mockQ := &mockEnqueuer{returnID: "asynq-job-999"}
	mockInsp := &mockInspector{}

	client := queue.NewClientWithInterfaces(mockQ, mockInsp, cfg, db)

	// Seed task in DB
	initialTask := models.Task{
		ID:           "task-abc",
		ProjectID:    "proj-123",
		Title:        "Implement feature",
		Prompt:       "Do work",
		AssigneeID:   "agent-1",
		Status:       models.TaskStatusQueued,
		AssigneeType: models.AssigneeAgent,
		Priority:     models.PriorityUrgent,
		Skills:       models.StringArray{"go"},
		MCPServers:   models.StringArray{"context7"},
	}
	if err := db.Create(&initialTask).Error; err != nil {
		t.Fatalf("failed to create task in db: %v", err)
	}

	ctx := context.Background()
	asynqID, err := client.EnqueueTask(ctx, &initialTask)
	if err != nil {
		t.Fatalf("EnqueueTask failed: %v", err)
	}

	if asynqID != "asynq-job-999" {
		t.Errorf("expected asynqID %q, got %q", "asynq-job-999", asynqID)
	}
	if initialTask.AsynqTaskID == nil || *initialTask.AsynqTaskID != "asynq-job-999" {
		t.Errorf("expected task.AsynqTaskID to be updated on struct, got %v", initialTask.AsynqTaskID)
	}

	// Verify task in DB updated
	var dbTask models.Task
	if err := db.First(&dbTask, "id = ?", initialTask.ID).Error; err != nil {
		t.Fatalf("failed to retrieve task from db: %v", err)
	}
	if dbTask.AsynqTaskID == nil || *dbTask.AsynqTaskID != "asynq-job-999" {
		t.Errorf("expected dbTask.AsynqTaskID to be %q, got %v", "asynq-job-999", dbTask.AsynqTaskID)
	}

	// Verify Asynq payload
	if mockQ.lastTask == nil {
		t.Fatal("expected task to be enqueued")
	}
	if mockQ.lastTask.Type() != queue.TypeExecuteAgentTask {
		t.Errorf("expected task type %q, got %q", queue.TypeExecuteAgentTask, mockQ.lastTask.Type())
	}

	var payload queue.TaskExecutionPayload
	if err := json.Unmarshal(mockQ.lastTask.Payload(), &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if payload.TaskID != "task-abc" || payload.ProjectID != "proj-123" {
		t.Errorf("unexpected payload content: %+v", payload)
	}
}

func TestEnqueueTaskNilAndEmpty(t *testing.T) {
	mockQ := &mockEnqueuer{}
	mockInsp := &mockInspector{}
	client := queue.NewClientWithInterfaces(mockQ, mockInsp, nil, nil)

	ctx := context.Background()
	if _, err := client.EnqueueTask(ctx, nil); err == nil {
		t.Error("expected error when enqueuing nil task, got nil")
	}

	emptyTask := &models.Task{}
	if _, err := client.EnqueueTask(ctx, emptyTask); err == nil {
		t.Error("expected error when task ID is empty, got nil")
	}
}

func TestEnqueueTaskEnqueueError(t *testing.T) {
	mockQ := &mockEnqueuer{err: errors.New("redis connection refused")}
	mockInsp := &mockInspector{}
	client := queue.NewClientWithInterfaces(mockQ, mockInsp, nil, nil)

	task := &models.Task{
		ID:        "task-err",
		ProjectID: "proj-err",
		Priority:  models.PriorityNormal,
	}
	_, err := client.EnqueueTask(context.Background(), task)
	if err == nil {
		t.Error("expected enqueue error, got nil")
	}
}

func TestCancelTask(t *testing.T) {
	mockQ := &mockEnqueuer{}
	mockInsp := &mockInspector{}
	client := queue.NewClientWithInterfaces(mockQ, mockInsp, nil, nil)

	ctx := context.Background()

	// Empty task ID
	if err := client.CancelTask(ctx, "critical", ""); err == nil {
		t.Error("expected error for empty asynqTaskID, got nil")
	}

	// Successful cancel with default queue fallback
	if err := client.CancelTask(ctx, "", "task-to-cancel"); err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}
	if mockInsp.lastQueue != queue.QueueDefault || mockInsp.lastID != "task-to-cancel" {
		t.Errorf("expected queue=%q id=%q, got queue=%q id=%q", queue.QueueDefault, "task-to-cancel", mockInsp.lastQueue, mockInsp.lastID)
	}

	// Inspector returns error
	mockInsp.err = errors.New("task not found")
	if err := client.CancelTask(ctx, "critical", "non-existent"); err == nil {
		t.Error("expected error from inspector DeleteTask, got nil")
	}
}

func TestClientClose(t *testing.T) {
	mockQ := &mockEnqueuer{}
	mockInsp := &mockInspector{}
	client := queue.NewClientWithInterfaces(mockQ, mockInsp, nil, nil)

	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if !mockQ.closed || !mockInsp.closed {
		t.Errorf("expected both mock enqueuer and inspector to be closed, got enqueuer=%v, inspector=%v", mockQ.closed, mockInsp.closed)
	}
}

func TestNewClientConstructor(t *testing.T) {
	if _, err := queue.NewClient(nil, nil); err == nil {
		t.Error("expected error when config is nil, got nil")
	}

	cfg := &config.Config{
		RedisAddr:     "127.0.0.1:6379",
		RedisPassword: "",
	}
	client, err := queue.NewClient(cfg, nil)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	_ = client.Close()
}
