// internal/queue/worker_test.go
package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"agent-task-manager/internal/config"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

type mockTaskRunner struct {
	runErr   error
	runFunc  func(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) error
	calls    int
	lastTask *models.Task
	lastAtt  *models.TaskAttempt
}

func (m *mockTaskRunner) RunTask(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) error {
	m.calls++
	m.lastTask = task
	m.lastAtt = attempt
	if m.runFunc != nil {
		return m.runFunc(ctx, task, attempt)
	}
	return m.runErr
}

func TestWorkerServerConfig(t *testing.T) {
	cfg := &config.Config{
		RedisAddr:          "localhost:6379",
		MaxConcurrentTasks: 4,
	}

	srv := queue.NewWorkerServer(cfg, nil, nil)
	if srv == nil {
		t.Fatalf("expected non-nil worker server")
	}
	if srv.Concurrency() != 4 {
		t.Errorf("expected concurrency 4, got %d", srv.Concurrency())
	}

	// Test default fallback when MaxConcurrentTasks <= 0
	cfgDefault := &config.Config{
		RedisAddr:          "localhost:6379",
		MaxConcurrentTasks: 0,
	}
	srvDefault := queue.NewWorkerServer(cfgDefault, nil, nil)
	if srvDefault.Concurrency() != 4 {
		t.Errorf("expected fallback concurrency 4, got %d", srvDefault.Concurrency())
	}
}

func setupFullTestDB(t *testing.T) *gorm.DB {
	db := setupTestDB(t)

	err := db.Exec(`CREATE TABLE task_attempts (
		id TEXT PRIMARY KEY,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		started_at DATETIME,
		finished_at DATETIME,
		session_id TEXT,
		container_id TEXT,
		worktree_path TEXT,
		branch_name TEXT,
		exit_code INTEGER,
		failure_reason TEXT,
		task_id TEXT NOT NULL,
		attempt_number INTEGER NOT NULL,
		status TEXT NOT NULL
	)`).Error
	if err != nil {
		t.Fatalf("failed to create task_attempts table: %v", err)
	}
	return db
}

func TestWorkerExecutionHandler_Success(t *testing.T) {
	db := setupFullTestDB(t)
	cfg := &config.Config{
		RedisAddr:          "localhost:6379",
		MaxConcurrentTasks: 2,
	}

	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	taskSvc := service.NewTaskService(taskRepo, attemptRepo, nil, nil)

	runner := &mockTaskRunner{}
	srv := queue.NewWorkerServer(cfg, taskSvc, runner)

	task := models.Task{
		ID:           "task-exec-1",
		ProjectID:    "proj-1",
		Title:        "Implement feature",
		Prompt:       "Run this code",
		AssigneeID:   "agent-1",
		Status:       models.TaskStatusQueued,
		AssigneeType: models.AssigneeAgent,
		Priority:     models.PriorityNormal,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	payload := queue.TaskExecutionPayload{
		TaskID:    task.ID,
		ProjectID: task.ProjectID,
	}
	payloadBytes, _ := json.Marshal(payload)
	asynqTask := asynq.NewTask(queue.TypeExecuteAgentTask, payloadBytes)

	handler := srv.Handler()
	if err := handler.ProcessTask(context.Background(), asynqTask); err != nil {
		t.Fatalf("ProcessTask failed: %v", err)
	}

	if runner.calls != 1 {
		t.Fatalf("expected runner to be called once, got %d", runner.calls)
	}

	var updatedTask models.Task
	if err := db.First(&updatedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatalf("failed to fetch updated task: %v", err)
	}
	if updatedTask.Status != models.TaskStatusReview {
		t.Errorf("expected task status %s, got %s", models.TaskStatusReview, updatedTask.Status)
	}
	if updatedTask.HeartbeatAt == nil {
		t.Errorf("expected task heartbeat_at to be populated")
	}

	var attempts []models.TaskAttempt
	if err := db.Where("task_id = ?", task.ID).Find(&attempts).Error; err != nil {
		t.Fatalf("failed to fetch attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(attempts))
	}
	if attempts[0].AttemptNumber != 1 {
		t.Errorf("expected attempt_number 1, got %d", attempts[0].AttemptNumber)
	}
	if attempts[0].Status != models.AttemptSucceeded {
		t.Errorf("expected attempt status succeeded, got %s", attempts[0].Status)
	}
	if attempts[0].StartedAt == nil || attempts[0].FinishedAt == nil {
		t.Errorf("expected attempt started_at and finished_at to be non-nil")
	}
}

func TestWorkerExecutionHandler_CancelledSkipsRunner(t *testing.T) {
	db := setupFullTestDB(t)
	cfg := &config.Config{
		RedisAddr:          "localhost:6379",
		MaxConcurrentTasks: 2,
	}

	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	taskSvc := service.NewTaskService(taskRepo, attemptRepo, nil, nil)

	runner := &mockTaskRunner{}
	srv := queue.NewWorkerServer(cfg, taskSvc, runner)

	task := models.Task{
		ID:           "task-cancelled",
		ProjectID:    "proj-1",
		Title:        "Cancelled feature",
		Prompt:       "Do not run",
		AssigneeID:   "agent-1",
		Status:       models.TaskStatusCancelled,
		AssigneeType: models.AssigneeAgent,
		Priority:     models.PriorityNormal,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	payload := queue.TaskExecutionPayload{
		TaskID:    task.ID,
		ProjectID: task.ProjectID,
	}
	payloadBytes, _ := json.Marshal(payload)
	asynqTask := asynq.NewTask(queue.TypeExecuteAgentTask, payloadBytes)

	handler := srv.Handler()
	if err := handler.ProcessTask(context.Background(), asynqTask); err != nil {
		t.Fatalf("ProcessTask failed: %v", err)
	}

	if runner.calls != 0 {
		t.Fatalf("expected runner NOT to be called, got %d calls", runner.calls)
	}

	var checkTask models.Task
	db.First(&checkTask, "id = ?", task.ID)
	if checkTask.Status != models.TaskStatusCancelled {
		t.Errorf("expected task status to remain cancelled, got %s", checkTask.Status)
	}
}

func TestWorkerExecutionHandler_RunnerFailure(t *testing.T) {
	db := setupFullTestDB(t)
	cfg := &config.Config{
		RedisAddr:          "localhost:6379",
		MaxConcurrentTasks: 2,
	}

	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	taskSvc := service.NewTaskService(taskRepo, attemptRepo, nil, nil)

	runnerErr := errors.New("container execution failed with exit code 1")
	runner := &mockTaskRunner{runErr: runnerErr}
	srv := queue.NewWorkerServer(cfg, taskSvc, runner)

	task := models.Task{
		ID:           "task-fail",
		ProjectID:    "proj-1",
		Title:        "Failing feature",
		Prompt:       "Run and fail",
		AssigneeID:   "agent-1",
		Status:       models.TaskStatusQueued,
		AssigneeType: models.AssigneeAgent,
		Priority:     models.PriorityNormal,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	payload := queue.TaskExecutionPayload{
		TaskID:    task.ID,
		ProjectID: task.ProjectID,
	}
	payloadBytes, _ := json.Marshal(payload)
	asynqTask := asynq.NewTask(queue.TypeExecuteAgentTask, payloadBytes)

	handler := srv.Handler()
	err := handler.ProcessTask(context.Background(), asynqTask)
	if err == nil {
		t.Fatalf("expected handler to return error, got nil")
	}

	var updatedTask models.Task
	if err := db.First(&updatedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatalf("failed to fetch updated task: %v", err)
	}
	if updatedTask.Status != models.TaskStatusFailed {
		t.Errorf("expected task status %s, got %s", models.TaskStatusFailed, updatedTask.Status)
	}

	var attempts []models.TaskAttempt
	if err := db.Where("task_id = ?", task.ID).Find(&attempts).Error; err != nil {
		t.Fatalf("failed to fetch attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(attempts))
	}
	if attempts[0].Status != models.AttemptFailed {
		t.Errorf("expected attempt status %s, got %s", models.AttemptFailed, attempts[0].Status)
	}
	if attempts[0].FailureReason == nil || *attempts[0].FailureReason != runnerErr.Error() {
		t.Errorf("expected failure reason %q, got %v", runnerErr.Error(), attempts[0].FailureReason)
	}
}

func TestWorkerExecutionHandler_SecondAttemptNumber(t *testing.T) {
	db := setupFullTestDB(t)
	cfg := &config.Config{
		RedisAddr:          "localhost:6379",
		MaxConcurrentTasks: 2,
	}

	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	taskSvc := service.NewTaskService(taskRepo, attemptRepo, nil, nil)

	runner := &mockTaskRunner{}
	srv := queue.NewWorkerServer(cfg, taskSvc, runner)

	task := models.Task{
		ID:           "task-retry",
		ProjectID:    "proj-1",
		Title:        "Retry feature",
		Prompt:       "Run retry",
		AssigneeID:   "agent-1",
		Status:       models.TaskStatusQueued,
		AssigneeType: models.AssigneeAgent,
		Priority:     models.PriorityNormal,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create previous attempt
	firstAttempt := models.TaskAttempt{
		ID:            "att-1",
		TaskID:        task.ID,
		AttemptNumber: 1,
		Status:        models.AttemptFailed,
	}
	if err := db.Create(&firstAttempt).Error; err != nil {
		t.Fatalf("failed to create first attempt: %v", err)
	}

	payload := queue.TaskExecutionPayload{
		TaskID:    task.ID,
		ProjectID: task.ProjectID,
	}
	payloadBytes, _ := json.Marshal(payload)
	asynqTask := asynq.NewTask(queue.TypeExecuteAgentTask, payloadBytes)

	handler := srv.Handler()
	if err := handler.ProcessTask(context.Background(), asynqTask); err != nil {
		t.Fatalf("ProcessTask failed: %v", err)
	}

	var attempts []models.TaskAttempt
	if err := db.Where("task_id = ?", task.ID).Order("attempt_number ASC").Find(&attempts).Error; err != nil {
		t.Fatalf("failed to fetch attempts: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(attempts))
	}
	if attempts[1].AttemptNumber != 2 {
		t.Errorf("expected second attempt number to be 2, got %d", attempts[1].AttemptNumber)
	}
}

func TestWorkerExecutionHandler_EmptyOrInvalidPayload(t *testing.T) {
	cfg := &config.Config{
		RedisAddr: "localhost:6379",
	}
	srv := queue.NewWorkerServer(cfg, nil, nil)
	handler := srv.Handler()

	// Invalid JSON
	badTask := asynq.NewTask(queue.TypeExecuteAgentTask, []byte("invalid-json"))
	if err := handler.ProcessTask(context.Background(), badTask); err == nil {
		t.Error("expected error for invalid json payload, got nil")
	}

	// Empty TaskID
	emptyPayloadTask := asynq.NewTask(queue.TypeExecuteAgentTask, []byte(`{"task_id":""}`))
	if err := handler.ProcessTask(context.Background(), emptyPayloadTask); err == nil {
		t.Error("expected error for empty task_id, got nil")
	}
}

func TestWorkerExecutionHandler_TaskNotFoundInDB(t *testing.T) {
	db := setupFullTestDB(t)
	cfg := &config.Config{
		RedisAddr: "localhost:6379",
	}
	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	taskSvc := service.NewTaskService(taskRepo, attemptRepo, nil, nil)

	srv := queue.NewWorkerServer(cfg, taskSvc, nil)
	handler := srv.Handler()

	payloadBytes, _ := json.Marshal(queue.TaskExecutionPayload{TaskID: "non-existent-task"})
	task := asynq.NewTask(queue.TypeExecuteAgentTask, payloadBytes)

	if err := handler.ProcessTask(context.Background(), task); err == nil {
		t.Error("expected error when task not in db, got nil")
	}
}

type mockTaskExecutor struct {
	called bool
	taskID string
}

func (m *mockTaskExecutor) ExecuteTask(ctx context.Context, taskID string, runner queue.TaskRunner) error {
	m.called = true
	m.taskID = taskID
	if runner != nil {
		return runner.RunTask(ctx, &models.Task{ID: taskID}, &models.TaskAttempt{})
	}
	return nil
}

func TestWorkerExecutionHandler_RunWithExecutor(t *testing.T) {
	cfg := &config.Config{
		RedisAddr: "localhost:6379",
	}
	runner := &mockTaskRunner{}
	executor := &mockTaskExecutor{}
	srv := queue.NewWorkerServer(cfg, executor, runner)
	handler := srv.Handler()

	payloadBytes, _ := json.Marshal(queue.TaskExecutionPayload{TaskID: "task-test-id", ProjectID: "proj-1"})
	task := asynq.NewTask(queue.TypeExecuteAgentTask, payloadBytes)

	if err := handler.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executor.called {
		t.Errorf("expected executor to be called")
	}
	if executor.taskID != "task-test-id" {
		t.Errorf("expected task id task-test-id, got %s", executor.taskID)
	}
	if runner.calls != 1 {
		t.Fatalf("expected runner to be called once, got %d", runner.calls)
	}
}
