package service_test

import (
	"context"
	"testing"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
	"agent-task-manager/internal/ws"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}

	err = gormDB.Exec(`
		CREATE TABLE teams (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			name TEXT NOT NULL,
			slug TEXT NOT NULL UNIQUE
		);
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			issuer TEXT NOT NULL,
			subject TEXT NOT NULL,
			email TEXT,
			display_name TEXT
		);
		CREATE TABLE team_memberships (
			team_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			role TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (team_id, user_id)
		);
		CREATE TABLE api_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			team_id TEXT NOT NULL,
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			expires_at DATETIME,
			last_used_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE credentials (
			id TEXT PRIMARY KEY,
			team_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			label TEXT NOT NULL,
			encrypted_data TEXT NOT NULL,
			key_fingerprint TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE project_integrations (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			credential_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			external_id TEXT NOT NULL,
			external_url TEXT,
			config TEXT NOT NULL DEFAULT '{}',
			enabled BOOLEAN NOT NULL DEFAULT 1,
			last_synced_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE projects (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			team_id TEXT NOT NULL,
			name TEXT NOT NULL,
			repo_path TEXT NOT NULL
		);
		CREATE TABLE tasks (
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
		);
		CREATE TABLE task_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			task_id TEXT NOT NULL,
			attempt_id TEXT,
			log_chunk TEXT NOT NULL
		);
	`).Error
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}
	return gormDB
}

type mockQueueClient struct {
	cancelledQueue string
	cancelledID    string
}

func (m *mockQueueClient) EnqueueTask(ctx context.Context, task *models.Task) (string, error) {
	id := "mock-asynq-id"
	task.AsynqTaskID = &id
	return id, nil
}

func (m *mockQueueClient) CancelTask(ctx context.Context, queueName string, asynqTaskID string) error {
	m.cancelledQueue = queueName
	m.cancelledID = asynqTaskID
	return nil
}

func (m *mockQueueClient) Close() error {
	return nil
}

func createTestUserAndTask(t *testing.T, gormDB *gorm.DB) (*models.User, *models.Project, *models.Task) {
	t.Helper()
	team := &models.Team{
		ID:   common.NewID(),
		Name: "Core Team",
		Slug: "core-team",
	}
	if err := gormDB.Create(team).Error; err != nil {
		t.Fatalf("failed to create team: %v", err)
	}

	user := &models.User{
		ID:          common.NewID(),
		Issuer:      "https://accounts.google.com",
		Subject:     "sub-" + common.NewID(),
		Email:       "dev@example.com",
		DisplayName: "Developer",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := gormDB.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	project := &models.Project{
		ID:        common.NewID(),
		TeamID:    team.ID,
		Name:      "Test Project",
		RepoPath:  "/srv/agent-tasks/repos/test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := gormDB.Create(project).Error; err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	asynqID := "asynq-123"
	task := &models.Task{
		ID:           common.NewID(),
		ProjectID:    project.ID,
		Title:        "Implement feature",
		Prompt:       "Write unit tests",
		AssigneeID:   user.ID,
		Status:       models.TaskStatusQueued,
		AssigneeType: models.AssigneeAgent,
		Priority:     models.PriorityNormal,
		AsynqTaskID:  &asynqID,
		Skills:       models.StringArray{},
		MCPServers:   models.StringArray{},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := gormDB.Create(task).Error; err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	return user, project, task
}

func TestTaskServiceTransitionsAndCancellation(t *testing.T) {
	gormDB := setupTestDB(t)
	_, _, task := createTestUserAndTask(t, gormDB)
	taskRepo := repository.NewTaskRepository(gormDB)

	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	client := ws.NewMockClient("client-test", task.ID)
	hub.Register(client)
	time.Sleep(10 * time.Millisecond)

	mockQueue := &mockQueueClient{}
	attemptRepo := repository.NewAttemptRepository(gormDB)
	taskSvc := service.NewTaskService(taskRepo, attemptRepo, mockQueue, hub)

	ctx := context.Background()

	// GetTask
	fetched, err := taskSvc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if fetched.ID != task.ID {
		t.Fatalf("task ID mismatch: got %s, want %s", fetched.ID, task.ID)
	}

	// UpdateStatus
	err = taskSvc.UpdateStatus(ctx, task.ID, models.TaskStatusInProgress)
	if err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	// Verify WebSocket received status broadcast
	select {
	case msg := <-client.Messages():
		if len(msg) == 0 {
			t.Errorf("empty status broadcast")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for status broadcast")
	}

	// CancelTask
	err = taskSvc.CancelTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}

	if mockQueue.cancelledID != "asynq-123" {
		t.Fatalf("expected cancel of asynq-123, got %s", mockQueue.cancelledID)
	}

	// Verify DB status is cancelled
	updated, err := taskSvc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask failed after cancel: %v", err)
	}
	if updated.Status != models.TaskStatusCancelled {
		t.Fatalf("expected status %s, got %s", models.TaskStatusCancelled, updated.Status)
	}
}

func TestLogServiceAppendAndBroadcast(t *testing.T) {
	gormDB := setupTestDB(t)
	_, _, task := createTestUserAndTask(t, gormDB)
	logRepo := repository.NewLogRepository(gormDB)

	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	client := ws.NewMockClient("client-log", task.ID)
	hub.Register(client)
	time.Sleep(10 * time.Millisecond)

	logSvc := service.NewLogService(logRepo, hub)
	ctx := context.Background()

	attemptID := "attempt-1"
	err := logSvc.AppendLog(ctx, task.ID, &attemptID, "Compiling package...\n")
	if err != nil {
		t.Fatalf("AppendLog failed: %v", err)
	}

	// Verify WebSocket received log broadcast
	select {
	case msg := <-client.Messages():
		if len(msg) == 0 {
			t.Errorf("empty log broadcast")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for log broadcast")
	}

	// Verify DB persisted log entry
	logs, err := logSvc.GetLogs(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) != 1 || logs[0].LogChunk != "Compiling package...\n" {
		t.Fatalf("unexpected logs: %+v", logs)
	}
}
