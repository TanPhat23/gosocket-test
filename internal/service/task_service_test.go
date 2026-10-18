package service_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/service"
	"agent-task-manager/internal/ws"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", common.NewID())
	gormDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
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
			display_name TEXT,
			sessions_valid_after DATETIME
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
			repo_path TEXT NOT NULL,
			runtime_config TEXT NOT NULL DEFAULT '{}'
		);
		CREATE TABLE tasks (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			heartbeat_at DATETIME,
			integration_id TEXT,
			asynq_task_id TEXT,
			session_id TEXT,
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
			priority INTEGER NOT NULL DEFAULT 50,
			parent_task_id TEXT
		);
		CREATE TABLE task_attempts (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			attempt_number INTEGER NOT NULL,
			status TEXT NOT NULL,
			session_id TEXT,
			container_id TEXT,
			worktree_path TEXT,
			branch_name TEXT,
			exit_code INTEGER,
			failure_reason TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			started_at DATETIME,
			finished_at DATETIME
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

	if err := gormDB.Create(&models.TeamMembership{
		TeamID: team.ID,
		UserID: user.ID,
		Role:   models.RoleMember,
	}).Error; err != nil {
		t.Fatalf("failed to create team membership: %v", err)
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
	user, _, task := createTestUserAndTask(t, gormDB)
	taskRepo := repository.NewTaskRepository(gormDB)

	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	client := ws.NewMockClient("client-test", task.ID)
	hub.Register(client)
	time.Sleep(10 * time.Millisecond)

	mockQueue := &mockQueueClient{}
	attemptRepo := repository.NewAttemptRepository(gormDB)
	projectRepo := repository.NewProjectRepository(gormDB)
	teamRepo := repository.NewTeamRepository(gormDB)
	taskSvc := service.NewTaskService(
		taskRepo,
		attemptRepo,
		mockQueue,
		hub,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo),
	)

	ctx := context.Background()

	// GetTask
	fetched, err := taskRepo.GetByID(ctx, task.ID)
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

	// CancelTask with empty userID -> ErrForbidden
	err = taskSvc.CancelTask(ctx, task.ID, "")
	if !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("expected ErrForbidden on empty userID, got %v", err)
	}

	// CancelTask with authorized user -> success
	err = taskSvc.CancelTask(ctx, task.ID, user.ID)
	if err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}

	if mockQueue.cancelledID != "asynq-123" {
		t.Fatalf("expected cancel of asynq-123, got %s", mockQueue.cancelledID)
	}

	// Verify DB status is cancelled
	updated, err := taskRepo.GetByID(ctx, task.ID)
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

func TestTaskServiceCreateSubtaskAndListChildren(t *testing.T) {
	gormDB := setupTestDB(t)
	user, project, parentTask := createTestUserAndTask(t, gormDB)
	taskRepo := repository.NewTaskRepository(gormDB)
	attemptRepo := repository.NewAttemptRepository(gormDB)
	mockQueue := &mockQueueClient{}

	taskSvc := service.NewTaskService(taskRepo, attemptRepo, mockQueue, nil,
		service.WithTaskSessionDependencies(nil, repository.NewProjectRepository(gormDB), repository.NewTeamRepository(gormDB)))
	ctx := context.Background()

	subtaskReq := &dto.CreateTaskRequest{
		ProjectID:    project.ID,
		ParentTaskID: &parentTask.ID,
		Title:        "Child task 1",
		Prompt:       "Child prompt 1",
		AssigneeID:   "sub-agent-1",
		AssigneeType: models.AssigneeAgent,
		Priority:     int(models.PriorityNormal),
	}

	createdChild, err := taskSvc.CreateTask(ctx, subtaskReq, user.ID)
	if err != nil {
		t.Fatalf("CreateTask child failed: %v", err)
	}

	if createdChild.ParentTaskID == nil || *createdChild.ParentTaskID != parentTask.ID {
		t.Fatalf("expected ParentTaskID %s, got %+v", parentTask.ID, createdChild.ParentTaskID)
	}

	children, err := taskSvc.ListSubtasks(ctx, parentTask.ID, user.ID)
	if err != nil {
		t.Fatalf("ListSubtasks failed: %v", err)
	}

	if len(children) != 1 {
		t.Fatalf("expected 1 child task, got %d", len(children))
	}
	if children[0].ID != createdChild.ID {
		t.Errorf("expected child ID %s, got %s", createdChild.ID, children[0].ID)
	}
	if children[0].ParentTaskID == nil || *children[0].ParentTaskID != parentTask.ID {
		t.Errorf("expected child ParentTaskID %s, got %+v", parentTask.ID, children[0].ParentTaskID)
	}
}

type mockRunnerCapture struct {
	capturedTask    *models.Task
	capturedAttempt *models.TaskAttempt
}

func (m *mockRunnerCapture) RunTask(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) error {
	m.capturedTask = task
	m.capturedAttempt = attempt
	return nil
}

func TestTaskServiceSessionIDGenerationAndExecution(t *testing.T) {
	gormDB := setupTestDB(t)
	user, project, _ := createTestUserAndTask(t, gormDB)
	taskRepo := repository.NewTaskRepository(gormDB)
	attemptRepo := repository.NewAttemptRepository(gormDB)
	mockQueue := &mockQueueClient{}

	taskSvc := service.NewTaskService(taskRepo, attemptRepo, mockQueue, nil,
		service.WithTaskSessionDependencies(nil, repository.NewProjectRepository(gormDB), repository.NewTeamRepository(gormDB)))
	ctx := context.Background()

	// 1. CreateTask initializes task.SessionID
	req := &dto.CreateTaskRequest{
		ProjectID:    project.ID,
		Title:        "Test Session Determinism",
		Prompt:       "Run prompt",
		AssigneeID:   "agent-1",
		AssigneeType: models.AssigneeAgent,
		Priority:     int(models.PriorityNormal),
	}

	created, err := taskSvc.CreateTask(ctx, req, user.ID)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	expectedSessionID := "ses_atm_" + created.ID
	if created.SessionID == nil || *created.SessionID != expectedSessionID {
		t.Fatalf("expected SessionID %s, got %+v", expectedSessionID, created.SessionID)
	}

	// Verify dto.ToTaskResponse mapping includes SessionID
	resp := dto.ToTaskResponse(created)
	if resp.SessionID == nil || *resp.SessionID != expectedSessionID {
		t.Fatalf("expected dto.TaskResponse.SessionID %s, got %+v", expectedSessionID, resp.SessionID)
	}

	// 2. ExecuteTask ensures attempt.SessionID matches task.SessionID
	runnerCapture := &mockRunnerCapture{}
	err = taskSvc.ExecuteTask(ctx, created.ID, runnerCapture)
	if err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}

	if runnerCapture.capturedAttempt == nil {
		t.Fatalf("expected runner to be called with TaskAttempt")
	}
	if runnerCapture.capturedAttempt.SessionID == nil || *runnerCapture.capturedAttempt.SessionID != expectedSessionID {
		t.Fatalf("expected attempt.SessionID %s, got %+v", expectedSessionID, runnerCapture.capturedAttempt.SessionID)
	}

	// 3. ExecuteTask on a legacy task with nil SessionID ensures it gets populated
	legacyTask := &models.Task{
		ID:           common.NewID(),
		ProjectID:    project.ID,
		Title:        "Legacy Task",
		Prompt:       "Legacy prompt",
		AssigneeID:   "agent-legacy",
		Status:       models.TaskStatusQueued,
		AssigneeType: models.AssigneeAgent,
		Priority:     models.PriorityNormal,
		SessionID:    nil, // intentionally nil
		Skills:       models.StringArray{},
		MCPServers:   models.StringArray{},
	}
	if err := taskRepo.Create(ctx, legacyTask); err != nil {
		t.Fatalf("failed to create legacy task: %v", err)
	}

	legacyCapture := &mockRunnerCapture{}
	err = taskSvc.ExecuteTask(ctx, legacyTask.ID, legacyCapture)
	if err != nil {
		t.Fatalf("ExecuteTask on legacy task failed: %v", err)
	}

	expectedLegacySessionID := "ses_atm_" + legacyTask.ID
	if legacyCapture.capturedTask.SessionID == nil || *legacyCapture.capturedTask.SessionID != expectedLegacySessionID {
		t.Fatalf("expected legacy task.SessionID to be %s, got %+v", expectedLegacySessionID, legacyCapture.capturedTask.SessionID)
	}
	if legacyCapture.capturedAttempt.SessionID == nil || *legacyCapture.capturedAttempt.SessionID != expectedLegacySessionID {
		t.Fatalf("expected legacy attempt.SessionID to be %s, got %+v", expectedLegacySessionID, legacyCapture.capturedAttempt.SessionID)
	}
}

type mockOpenCodeClient struct {
	getSessionFunc     func(ctx context.Context, sessionID string) (*runner.OpenCodeSessionDTO, error)
	getSessionMsgsFunc func(ctx context.Context, sessionID string) ([]dto.OpenCodeMessageDTO, error)
	promptSessionFunc  func(ctx context.Context, sessionID, prompt string) error
	compactSessionFunc func(ctx context.Context, sessionID string) error
}

func (m *mockOpenCodeClient) GetSession(ctx context.Context, sessionID string) (*runner.OpenCodeSessionDTO, error) {
	if m.getSessionFunc != nil {
		return m.getSessionFunc(ctx, sessionID)
	}
	return &runner.OpenCodeSessionDTO{ID: sessionID, Title: "Mock Session"}, nil
}

func (m *mockOpenCodeClient) GetSessionMessages(ctx context.Context, sessionID string) ([]dto.OpenCodeMessageDTO, error) {
	if m.getSessionMsgsFunc != nil {
		return m.getSessionMsgsFunc(ctx, sessionID)
	}
	return []dto.OpenCodeMessageDTO{{ID: "msg-1"}}, nil
}

func (m *mockOpenCodeClient) PromptSession(ctx context.Context, sessionID, prompt string) error {
	if m.promptSessionFunc != nil {
		return m.promptSessionFunc(ctx, sessionID, prompt)
	}
	return nil
}

func (m *mockOpenCodeClient) CompactSession(ctx context.Context, sessionID string) error {
	if m.compactSessionFunc != nil {
		return m.compactSessionFunc(ctx, sessionID)
	}
	return nil
}

func (m *mockOpenCodeClient) ListChildSessions(ctx context.Context, parentID string) ([]runner.OpenCodeSessionDTO, error) {
	return nil, nil
}

func (m *mockOpenCodeClient) InterruptSession(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockOpenCodeClient) InterruptSessionTree(ctx context.Context, rootSessionID string) error {
	return nil
}

func (m *mockOpenCodeClient) EnsureSessionAtLocation(ctx context.Context, sessionID, title, directory string) error {
	return nil
}

func TestTaskService_SessionOperationsAndRBAC(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	mockQueue := &mockQueueClient{}
	ctx := context.Background()

	team := &models.Team{
		ID:        common.NewID(),
		Name:      "Session Team",
		Slug:      "session-team",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	db.Create(team)

	viewerUser := &models.User{
		ID:          common.NewID(),
		Email:       "viewer@example.com",
		DisplayName: "Viewer",
		Subject:     "sub-viewer",
		Issuer:      "test",
	}
	memberUser := &models.User{
		ID:          common.NewID(),
		Email:       "member@example.com",
		DisplayName: "Member",
		Subject:     "sub-member",
		Issuer:      "test",
	}
	outsiderUser := &models.User{
		ID:          common.NewID(),
		Email:       "outsider@example.com",
		DisplayName: "Outsider",
		Subject:     "sub-outsider",
		Issuer:      "test",
	}
	db.Create(viewerUser)
	db.Create(memberUser)
	db.Create(outsiderUser)

	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: viewerUser.ID, Role: models.RoleViewer})
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: memberUser.ID, Role: models.RoleMember})

	project := &models.Project{
		ID:       common.NewID(),
		TeamID:   team.ID,
		Name:     "Session Project",
		RepoPath: "/tmp/session-repo",
	}
	db.Create(project)

	sessionID := "ses_atm_task123"
	task := &models.Task{
		ID:           common.NewID(),
		ProjectID:    project.ID,
		Title:        "Session Task",
		Prompt:       "Do something",
		AssigneeID:   "agent-1",
		AssigneeType: models.AssigneeAgent,
		Status:       models.TaskStatusInProgress,
		Priority:     models.PriorityNormal,
		SessionID:    &sessionID,
		Skills:       models.StringArray{},
		MCPServers:   models.StringArray{},
	}
	db.Create(task)

	mockClient := &mockOpenCodeClient{}
	taskSvc := service.NewTaskService(
		taskRepo,
		attemptRepo,
		mockQueue,
		nil,
		service.WithTaskSessionDependencies(mockClient, projectRepo, teamRepo),
	)

	// 1. GetTaskSession with viewer user -> authorized, but session inspection is disabled (quarantine)
	sessResp, err := taskSvc.GetTaskSession(ctx, task.ID, viewerUser.ID)
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("expected ErrRuntimeExecutionUnavailable for viewer on GetTaskSession, got %v", err)
	}
	if sessResp != nil {
		t.Errorf("expected nil session response, got %+v", sessResp)
	}

	// 2. GetTaskSession with outsider user -> 403 Forbidden (AC-SEC-02)
	_, err = taskSvc.GetTaskSession(ctx, task.ID, outsiderUser.ID)
	if !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for outsider on GetTaskSession, got %v", err)
	}

	// 3. PromptTaskSession with viewer user -> 403 Forbidden (AC-SEC-02: requires RoleMember)
	promptReq := &dto.TaskSessionPromptRequest{Prompt: "Next instructions"}
	err = taskSvc.PromptTaskSession(ctx, task.ID, viewerUser.ID, promptReq)
	if !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for viewer on PromptTaskSession, got %v", err)
	}

	// 4. PromptTaskSession with member user -> authorized, but execution is disabled (quarantine)
	err = taskSvc.PromptTaskSession(ctx, task.ID, memberUser.ID, promptReq)
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("expected ErrRuntimeExecutionUnavailable for member on PromptTaskSession, got %v", err)
	}

	// 5. CompactTaskSession with viewer user -> 403 Forbidden
	err = taskSvc.CompactTaskSession(ctx, task.ID, viewerUser.ID)
	if !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for viewer on CompactTaskSession, got %v", err)
	}

	// 6. CompactTaskSession with member user -> authorized, but execution is disabled (quarantine)
	err = taskSvc.CompactTaskSession(ctx, task.ID, memberUser.ID)
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("expected ErrRuntimeExecutionUnavailable for member on CompactTaskSession, got %v", err)
	}

	// 7. Daemon offline returns common.ErrServiceUnavailable (AC-SEC-04)
	offlineClient := &mockOpenCodeClient{
		getSessionFunc: func(ctx context.Context, sessionID string) (*runner.OpenCodeSessionDTO, error) {
			return nil, common.ErrServiceUnavailable
		},
		promptSessionFunc: func(ctx context.Context, sessionID, prompt string) error {
			return common.ErrServiceUnavailable
		},
		compactSessionFunc: func(ctx context.Context, sessionID string) error {
			return common.ErrServiceUnavailable
		},
	}
	offlineSvc := service.NewTaskService(
		taskRepo,
		attemptRepo,
		mockQueue,
		nil,
		service.WithTaskSessionDependencies(offlineClient, projectRepo, teamRepo),
	)

	// GetTaskSession is quarantined before the OpenCode client is ever consulted.
	_, err = offlineSvc.GetTaskSession(ctx, task.ID, memberUser.ID)
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("expected ErrRuntimeExecutionUnavailable on GetTaskSession, got %v", err)
	}

	// PromptTaskSession is quarantined before the OpenCode client is ever consulted.
	err = offlineSvc.PromptTaskSession(ctx, task.ID, memberUser.ID, promptReq)
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("expected ErrRuntimeExecutionUnavailable on PromptTaskSession, got %v", err)
	}

	err = offlineSvc.CompactTaskSession(ctx, task.ID, memberUser.ID)
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("expected ErrRuntimeExecutionUnavailable on CompactTaskSession, got %v", err)
	}
	// 8. CancelTask authorization tests
	// CancelTask with viewer user -> 403 Forbidden
	err = taskSvc.CancelTask(ctx, task.ID, viewerUser.ID)
	if !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for viewer on CancelTask, got %v", err)
	}

	// CancelTask with outsider user -> 403 Forbidden
	err = taskSvc.CancelTask(ctx, task.ID, outsiderUser.ID)
	if !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for outsider on CancelTask, got %v", err)
	}

	// CancelTask with member user -> Success
	err = taskSvc.CancelTask(ctx, task.ID, memberUser.ID)
	if err != nil {
		t.Fatalf("expected member to be able to CancelTask, got %v", err)
	}
	reloadedTask, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to reload task: %v", err)
	}
	if reloadedTask.Status != models.TaskStatusCancelled {
		t.Fatalf("expected task status to be %s, got %s", models.TaskStatusCancelled, reloadedTask.Status)
	}
}

type delayedMockRunner struct {
	started chan struct{}
	block   chan struct{}
}

func (d *delayedMockRunner) RunTask(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) error {
	close(d.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-d.block:
		return nil
	}
}

func TestTaskService_InFlightCancellation(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	teamRepo := repository.NewTeamRepository(db)

	ctx := context.Background()
	ownerUser := &models.User{ID: common.NewID(), Email: "cancel-owner@test.com", DisplayName: "Owner", Subject: "sub-owner", Issuer: "test"}
	db.Create(ownerUser)

	team := &models.Team{ID: common.NewID(), Name: "Cancel Team"}
	db.Create(team)
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: ownerUser.ID, Role: models.RoleOwner})

	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Cancel Project", RepoPath: "/tmp"}
	db.Create(project)

	task := &models.Task{
		ID:        common.NewID(),
		ProjectID: project.ID,
		Title:     "In-flight Cancel Task",
		Prompt:    "Long running",
		Status:    models.TaskStatusQueued,
	}
	db.Create(task)

	runner := &delayedMockRunner{
		started: make(chan struct{}),
		block:   make(chan struct{}),
	}

	taskSvc := service.NewTaskService(
		taskRepo,
		attemptRepo,
		nil,
		nil,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo),
	)

	execDone := make(chan error, 1)
	go func() {
		execDone <- taskSvc.ExecuteTask(context.Background(), task.ID, runner)
	}()

	// Wait for runner to start
	<-runner.started

	// Verify task is in_progress
	tInProgress, _ := taskRepo.GetByID(ctx, task.ID)
	if tInProgress.Status != models.TaskStatusInProgress {
		t.Fatalf("expected task in_progress, got %s", tInProgress.Status)
	}

	// Now cancel task via CancelTask
	err := taskSvc.CancelTask(ctx, task.ID, ownerUser.ID)
	if err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}

	// Execution should return
	select {
	case err := <-execDone:
		// ExecuteTask returns error or nil, but task and attempt must remain canceled
		_ = err
	case <-time.After(3 * time.Second):
		t.Fatalf("ExecuteTask did not terminate after cancellation")
	}

	// Check final DB task and attempt status
	finalTask, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to fetch final task: %v", err)
	}
	if finalTask.Status != models.TaskStatusCancelled {
		t.Errorf("expected final task status to be %s, got %s", models.TaskStatusCancelled, finalTask.Status)
	}

	var attempts []models.TaskAttempt
	if err := db.Where("task_id = ?", task.ID).Find(&attempts).Error; err != nil || len(attempts) == 0 {
		t.Fatalf("expected at least 1 attempt, got: %v", err)
	}
	if attempts[0].Status != models.AttemptCancelled {
		t.Errorf("expected final attempt status to be %s, got %s", models.AttemptCancelled, attempts[0].Status)
	}
}

func TestTaskService_CancellationCompletionRace(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	teamRepo := repository.NewTeamRepository(db)

	ctx := context.Background()
	ownerUser := &models.User{ID: common.NewID(), Email: "race-owner@test.com", DisplayName: "Owner", Subject: "sub-owner", Issuer: "test"}
	db.Create(ownerUser)

	team := &models.Team{ID: common.NewID(), Name: "Race Team"}
	db.Create(team)
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: ownerUser.ID, Role: models.RoleOwner})

	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Race Project", RepoPath: "/tmp"}
	db.Create(project)

	task := &models.Task{
		ID:        common.NewID(),
		ProjectID: project.ID,
		Title:     "Race Task",
		Prompt:    "Race",
		Status:    models.TaskStatusQueued,
	}
	db.Create(task)

	runner := &delayedMockRunner{
		started: make(chan struct{}),
		block:   make(chan struct{}),
	}

	taskSvc := service.NewTaskService(
		taskRepo,
		attemptRepo,
		nil,
		nil,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo),
	)

	execDone := make(chan error, 1)
	go func() {
		execDone <- taskSvc.ExecuteTask(context.Background(), task.ID, runner)
	}()

	<-runner.started

	// Cancel and release runner simultaneously
	go func() {
		_ = taskSvc.CancelTask(ctx, task.ID, ownerUser.ID)
	}()
	close(runner.block)

	select {
	case <-execDone:
	case <-time.After(3 * time.Second):
		t.Fatalf("ExecuteTask timed out")
	}

	// Give cancellation goroutine a moment to complete
	time.Sleep(50 * time.Millisecond)

	// Verify terminal status is cancelled, never resurrected to review/done
	finalTask, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to fetch final task: %v", err)
	}
	if finalTask.Status != models.TaskStatusCancelled {
		t.Errorf("task was resurrected! Expected status %s, got %s", models.TaskStatusCancelled, finalTask.Status)
	}
}

func TestTaskService_CancelBeforeStart_DoesNotStart(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	teamRepo := repository.NewTeamRepository(db)

	ctx := context.Background()
	ownerUser := &models.User{ID: common.NewID(), Email: "pre-owner@test.com", DisplayName: "Owner", Subject: "sub-owner", Issuer: "test"}
	db.Create(ownerUser)

	team := &models.Team{ID: common.NewID(), Name: "Pre Team"}
	db.Create(team)
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: ownerUser.ID, Role: models.RoleOwner})

	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Pre Project", RepoPath: "/tmp"}
	db.Create(project)

	task := &models.Task{
		ID:        common.NewID(),
		ProjectID: project.ID,
		Title:     "Pre Cancel Task",
		Prompt:    "Pre Cancel",
		Status:    models.TaskStatusQueued,
	}
	db.Create(task)

	taskSvc := service.NewTaskService(
		taskRepo,
		attemptRepo,
		nil,
		nil,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo),
	)

	// Cancel task before ExecuteTask begins
	err := taskSvc.CancelTask(ctx, task.ID, ownerUser.ID)
	if err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}

	runnerCalled := false
	runner := &delayedMockRunner{
		started: make(chan struct{}),
		block:   make(chan struct{}),
	}
	go func() {
		close(runner.block)
	}()

	err = taskSvc.ExecuteTask(ctx, task.ID, runner)
	if err != nil {
		t.Fatalf("ExecuteTask returned error on pre-cancelled task: %v", err)
	}
	_ = runnerCalled

	// Verify task is still cancelled and no in_progress overwrite occurred
	tFinal, _ := taskRepo.GetByID(ctx, task.ID)
	if tFinal.Status != models.TaskStatusCancelled {
		t.Errorf("expected task status to remain %s, got %s", models.TaskStatusCancelled, tFinal.Status)
	}
}

func TestGetTaskDiff(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	mockQueue := &mockQueueClient{}

	ctx := context.Background()
	ownerUser := &models.User{ID: common.NewID(), Email: "diff-owner@test.com", DisplayName: "Owner", Subject: "sub-owner", Issuer: "test"}
	db.Create(ownerUser)

	team := &models.Team{ID: common.NewID(), Name: "Diff Team"}
	db.Create(team)
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: ownerUser.ID, Role: models.RoleOwner})

	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Diff Project", RepoPath: "/tmp"}
	db.Create(project)

	worktreeDir := t.TempDir()
	task := &models.Task{
		ID:           common.NewID(),
		ProjectID:    project.ID,
		Title:        "Diff Task",
		Prompt:       "Change file",
		WorktreePath: &worktreeDir,
		Status:       models.TaskStatusReview,
	}
	db.Create(task)

	taskSvc := service.NewTaskService(
		taskRepo,
		attemptRepo,
		mockQueue,
		nil,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo),
	)

	diff, err := taskSvc.GetTaskDiff(ctx, task.ID, ownerUser.ID)
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("expected ErrRuntimeExecutionUnavailable from GetTaskDiff, got %v", err)
	}
	if diff != "" {
		t.Errorf("expected empty diff on fresh dir, got: %s", diff)
	}
}

type mockTestPRProvider struct {
	merged bool
}

func (m *mockTestPRProvider) CreatePullRequest(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, branch, title, body string) (string, error) {
	return "https://github.com/owner/repo/pull/1", nil
}

func (m *mockTestPRProvider) GetPullRequestStatus(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, prNumberOrURL string) (*integrations.PullRequestStatus, error) {
	return &integrations.PullRequestStatus{
		Number:  1,
		State:   "closed",
		Merged:  m.merged,
		HTMLURL: "https://github.com/owner/repo/pull/1",
	}, nil
}

func TestSyncTaskPRStatus(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	cipher, _ := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))

	ctx := context.Background()
	ownerUser := &models.User{ID: common.NewID(), Email: "pr-owner@test.com", DisplayName: "Owner", Subject: "sub-pr-owner", Issuer: "test"}
	db.Create(ownerUser)

	team := &models.Team{ID: common.NewID(), Name: "PR Team"}
	db.Create(team)
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: ownerUser.ID, Role: models.RoleOwner})

	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "PR Project"}
	db.Create(project)

	credEnc, _ := cipher.Encrypt("pat-test")
	cred := &models.Credential{
		ID:            common.NewID(),
		TeamID:        team.ID,
		Provider:      models.ProviderGitHub,
		Label:         "PAT",
		EncryptedData: credEnc,
	}
	db.Create(cred)

	integ := &models.ProjectIntegration{
		ID:           common.NewID(),
		ProjectID:    project.ID,
		CredentialID: cred.ID,
		Provider:     models.IntegrationGitHub,
		ExternalID:   "owner/repo",
		Enabled:      true,
	}
	db.Create(integ)

	prURL := "https://github.com/owner/repo/pull/1"
	task := &models.Task{
		ID:        common.NewID(),
		ProjectID: project.ID,
		Title:     "PR Sync Task",
		Prompt:    "Implement feature",
		PRURL:     &prURL,
		Status:    models.TaskStatusReview,
	}
	db.Create(task)

	mockPR := &mockTestPRProvider{merged: true}
	taskSvc := service.NewTaskService(
		taskRepo,
		attemptRepo,
		&mockQueueClient{},
		nil,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo),
		service.WithTaskGitHubIntegration(integRepo, credRepo, cipher, mockPR),
	)

	// The PR is merged on GitHub, so sync marks the task done.
	syncedTask, err := taskSvc.SyncTaskPRStatus(ctx, task.ID, ownerUser.ID)
	if err != nil {
		t.Fatalf("SyncTaskPRStatus: %v", err)
	}
	if syncedTask == nil || syncedTask.Status != models.TaskStatusDone {
		t.Fatalf("expected synced task status %s, got %+v", models.TaskStatusDone, syncedTask)
	}

	reloaded, _ := taskRepo.GetByID(ctx, task.ID)
	if reloaded.Status != models.TaskStatusDone {
		t.Fatalf("expected persisted task status %s, got %s", models.TaskStatusDone, reloaded.Status)
	}
}

// countingPRProvider records every call so tests can prove it was never reached.
type countingPRProvider struct {
	createCalls int
	statusCalls int
}

func (c *countingPRProvider) CreatePullRequest(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, branch, title, body string) (string, error) {
	c.createCalls++
	return "https://github.com/owner/repo/pull/99", nil
}

func (c *countingPRProvider) GetPullRequestStatus(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, prNumberOrURL string) (*integrations.PullRequestStatus, error) {
	c.statusCalls++
	return &integrations.PullRequestStatus{Number: 99, State: "open"}, nil
}

func TestTaskService_PromptTaskSession_QuarantineHasNoSideEffects(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	ctx := context.Background()

	team := &models.Team{ID: common.NewID(), Name: "Quarantine Team", Slug: "quarantine-team"}
	db.Create(team)
	member := &models.User{ID: common.NewID(), Email: "q-member@example.com", DisplayName: "Member", Subject: "sub-q-member", Issuer: "test"}
	db.Create(member)
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: member.ID, Role: models.RoleMember})
	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Quarantine Project", RepoPath: "/tmp/quarantine-repo"}
	db.Create(project)

	sessionID := "ses_atm_quarantine"
	worktree := t.TempDir()
	task := &models.Task{
		ID:           common.NewID(),
		ProjectID:    project.ID,
		Title:        "Quarantine Task",
		Prompt:       "Do something",
		AssigneeID:   "agent-1",
		AssigneeType: models.AssigneeAgent,
		Status:       models.TaskStatusReview, // would be moved to in_progress by a real steer
		Priority:     models.PriorityNormal,
		SessionID:    &sessionID,
		WorktreePath: &worktree, // would start watchSteeringTurn
		Skills:       models.StringArray{},
		MCPServers:   models.StringArray{},
	}
	db.Create(task)

	var openCodeCalls atomic.Int64
	client := &mockOpenCodeClient{
		getSessionFunc: func(ctx context.Context, sessionID string) (*runner.OpenCodeSessionDTO, error) {
			openCodeCalls.Add(1)
			return &runner.OpenCodeSessionDTO{ID: sessionID}, nil
		},
		getSessionMsgsFunc: func(ctx context.Context, sessionID string) ([]dto.OpenCodeMessageDTO, error) {
			openCodeCalls.Add(1)
			return nil, nil
		},
		promptSessionFunc: func(ctx context.Context, sessionID, prompt string) error {
			openCodeCalls.Add(1)
			return nil
		},
	}
	svc := service.NewTaskService(taskRepo, attemptRepo, &mockQueueClient{}, nil,
		service.WithTaskSessionDependencies(client, projectRepo, teamRepo))

	err := svc.PromptTaskSession(ctx, task.ID, member.ID, &dto.TaskSessionPromptRequest{Prompt: "steer"})
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("expected ErrRuntimeExecutionUnavailable, got %v", err)
	}
	if !errors.Is(err, common.ErrRuntimeQuarantined) {
		t.Errorf("expected error to wrap common.ErrRuntimeQuarantined, got %v", err)
	}

	// watchSteeringTurn polls on a 1500ms ticker (task_service.go), so wait past its first tick
	// to catch a (wrongly) started watcher reaching the OpenCode mock.
	time.Sleep(2 * time.Second)
	if n := openCodeCalls.Load(); n != 0 {
		t.Errorf("expected zero OpenCode calls, got %d", n)
	}
	reloaded, getErr := taskRepo.GetByID(ctx, task.ID)
	if getErr != nil {
		t.Fatalf("failed to reload task: %v", getErr)
	}
	if reloaded.Status != models.TaskStatusReview {
		t.Errorf("task status must not change, got %s", reloaded.Status)
	}

	// Authorization still runs first: a non-member gets Forbidden, not the quarantine error.
	outsider := &models.User{ID: common.NewID(), Email: "q-out@example.com", DisplayName: "Out", Subject: "sub-q-out", Issuer: "test"}
	db.Create(outsider)
	err = svc.PromptTaskSession(ctx, task.ID, outsider.ID, &dto.TaskSessionPromptRequest{Prompt: "steer"})
	if !errors.Is(err, common.ErrForbidden) {
		t.Errorf("expected ErrForbidden for outsider, got %v", err)
	}
	if n := openCodeCalls.Load(); n != 0 {
		t.Errorf("expected zero OpenCode calls after outsider attempt, got %d", n)
	}
}

func TestTaskService_CreatePullRequestForTask_QuarantineHasNoSideEffects(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	cipher, _ := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))
	ctx := context.Background()

	team := &models.Team{ID: common.NewID(), Name: "PR Quarantine Team", Slug: "pr-quarantine-team"}
	db.Create(team)
	member := &models.User{ID: common.NewID(), Email: "prq-member@example.com", DisplayName: "V", Subject: "sub-prq-m", Issuer: "test"}
	outsider := &models.User{ID: common.NewID(), Email: "prq-out@example.com", DisplayName: "O", Subject: "sub-prq-o", Issuer: "test"}
	db.Create(member)
	viewer := &models.User{ID: common.NewID(), Email: "prq-viewer@example.com", DisplayName: "W", Subject: "sub-prq-w", Issuer: "test"}
	db.Create(viewer)
	db.Create(outsider)
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: member.ID, Role: models.RoleMember})
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: viewer.ID, Role: models.RoleViewer})
	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "PR Quarantine Project"}
	db.Create(project)

	credEnc, _ := cipher.Encrypt("pat-test")
	cred := &models.Credential{ID: common.NewID(), TeamID: team.ID, Provider: models.ProviderGitHub, Label: "PAT", EncryptedData: credEnc}
	db.Create(cred)
	db.Create(&models.ProjectIntegration{
		ID: common.NewID(), ProjectID: project.ID, CredentialID: cred.ID,
		Provider: models.IntegrationGitHub, ExternalID: "owner/repo", Enabled: true,
	})

	// Real worktree repo whose push target (the exact URL the unquarantined code would use) is
	// redirected via url.insteadOf to a local bare repo. A push would then succeed and leave refs there.
	worktree := t.TempDir()
	bare := t.TempDir()
	runGit := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, gitErr := cmd.CombinedOutput()
		if gitErr != nil {
			t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), gitErr, out)
		}
		return string(out)
	}
	runGit(bare, "init", "--bare", "-b", "main")
	runGit(worktree, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(worktree, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(worktree, "add", "f.txt")
	runGit(worktree, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-m", "init")
	runGit(worktree, "config", "url.file://"+bare+".insteadOf", "https://github.com/owner/repo.git")
	existingPR := "https://github.com/owner/repo/pull/1"
	taskNoPR := &models.Task{ID: common.NewID(), ProjectID: project.ID, Title: "No PR", Prompt: "p", WorktreePath: &worktree, Status: models.TaskStatusReview}
	taskWithPR := &models.Task{ID: common.NewID(), ProjectID: project.ID, Title: "Has PR", Prompt: "p", WorktreePath: &worktree, PRURL: &existingPR, Status: models.TaskStatusReview}
	db.Create(taskNoPR)
	db.Create(taskWithPR)

	runGit(worktree, "branch", "task/"+taskNoPR.ID)
	runGit(worktree, "branch", "task/"+taskWithPR.ID)

	provider := &countingPRProvider{}
	svc := service.NewTaskService(taskRepo, attemptRepo, &mockQueueClient{}, nil,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo),
		service.WithTaskGitHubIntegration(integRepo, credRepo, cipher, provider),
	)

	// Authorized member: the refusal happens only after authorization.
	for _, id := range []string{taskNoPR.ID, taskWithPR.ID} {
		got, err := svc.CreatePullRequestForTask(ctx, id, member.ID)
		if got != nil {
			t.Errorf("task %q: expected nil task, got %+v", id, got)
		}
		if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
			t.Errorf("task %q: expected ErrRuntimeExecutionUnavailable, got %v", id, err)
		}
		if !errors.Is(err, common.ErrRuntimeQuarantined) {
			t.Errorf("task %q: expected error to wrap common.ErrRuntimeQuarantined, got %v", id, err)
		}
	}
	// Unauthorized / unknown callers never reach the quarantine refusal.
	for name, c := range map[string]struct{ task, user string }{
		"empty user":     {taskNoPR.ID, ""},
		"outsider":       {taskNoPR.ID, outsider.ID},
		"viewer":         {taskNoPR.ID, viewer.ID},
		"unknown task":   {"does-not-exist", member.ID},
		"empty task id":  {"", member.ID},
	} {
		got, err := svc.CreatePullRequestForTask(ctx, c.task, c.user)
		if got != nil || err == nil || errors.Is(err, common.ErrRuntimeQuarantined) {
			t.Errorf("%s: expected non-quarantine error and nil task, got %+v / %v", name, got, err)
		}
	}
	if provider.createCalls != 0 || provider.statusCalls != 0 {
		t.Errorf("PR provider must not be invoked, got create=%d status=%d", provider.createCalls, provider.statusCalls)
	}
	if refs := strings.TrimSpace(runGit(bare, "for-each-ref")); refs != "" {
		t.Errorf("nothing may be pushed, but the remote has refs:\n%s", refs)
	}
	reloaded, err := taskRepo.GetByID(ctx, taskNoPR.ID)
	if err != nil {
		t.Fatalf("failed to reload task: %v", err)
	}
	if reloaded.PRURL != nil || reloaded.Status != models.TaskStatusReview {
		t.Errorf("task must be unchanged, got status=%s pr=%v", reloaded.Status, reloaded.PRURL)
	}
}

// spyOpenCodeClient counts every call made through the OpenCode client interface.
type spyOpenCodeClient struct{ calls atomic.Int64 }

func (s *spyOpenCodeClient) GetSession(ctx context.Context, sessionID string) (*runner.OpenCodeSessionDTO, error) {
	s.calls.Add(1)
	return &runner.OpenCodeSessionDTO{ID: sessionID}, nil
}
func (s *spyOpenCodeClient) GetSessionMessages(ctx context.Context, sessionID string) ([]dto.OpenCodeMessageDTO, error) {
	s.calls.Add(1)
	return nil, nil
}
func (s *spyOpenCodeClient) PromptSession(ctx context.Context, sessionID, prompt string) error {
	s.calls.Add(1)
	return nil
}
func (s *spyOpenCodeClient) CompactSession(ctx context.Context, sessionID string) error {
	s.calls.Add(1)
	return nil
}
func (s *spyOpenCodeClient) ListChildSessions(ctx context.Context, parentID string) ([]runner.OpenCodeSessionDTO, error) {
	s.calls.Add(1)
	return nil, nil
}
func (s *spyOpenCodeClient) InterruptSession(ctx context.Context, sessionID string) error {
	s.calls.Add(1)
	return nil
}
func (s *spyOpenCodeClient) InterruptSessionTree(ctx context.Context, rootSessionID string) error {
	s.calls.Add(1)
	return nil
}
func (s *spyOpenCodeClient) EnsureSessionAtLocation(ctx context.Context, sessionID, title, directory string) error {
	s.calls.Add(1)
	return nil
}

func TestTaskService_CompactTaskSession_QuarantineHasNoSideEffects(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	ctx := context.Background()

	member := &models.User{ID: common.NewID(), Email: "cq-member@example.com", DisplayName: "M", Subject: "sub-cq-m", Issuer: "test"}
	viewer := &models.User{ID: common.NewID(), Email: "cq-viewer@example.com", DisplayName: "V", Subject: "sub-cq-v", Issuer: "test"}
	db.Create(member)
	db.Create(viewer)
	team := &models.Team{ID: common.NewID(), Name: "Compact Quarantine Team", Slug: "compact-quarantine-team"}
	db.Create(team)
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: member.ID, Role: models.RoleMember})
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: viewer.ID, Role: models.RoleViewer})
	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Compact Quarantine Project"}
	db.Create(project)
	sessionID := "ses_atm_compact_q"
	task := &models.Task{ID: common.NewID(), ProjectID: project.ID, Title: "T", Prompt: "p", SessionID: &sessionID, Status: models.TaskStatusReview}
	db.Create(task)

	spy := &spyOpenCodeClient{}
	svc := service.NewTaskService(taskRepo, attemptRepo, &mockQueueClient{}, nil,
		service.WithTaskSessionDependencies(spy, projectRepo, teamRepo))

	err := svc.CompactTaskSession(ctx, task.ID, member.ID)
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) || !errors.Is(err, common.ErrRuntimeQuarantined) {
		t.Fatalf("expected quarantine error, got %v", err)
	}
	if err := svc.CompactTaskSession(ctx, task.ID, viewer.ID); !errors.Is(err, common.ErrForbidden) {
		t.Errorf("expected ErrForbidden for viewer, got %v", err)
	}
	if n := spy.calls.Load(); n != 0 {
		t.Errorf("expected zero OpenCode client calls, got %d", n)
	}
}

// snapshotTree returns path -> content for every file and directory under root (including .git).
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			out[rel] = "<dir>"
			return nil
		}
		b, rErr := os.ReadFile(p)
		if rErr != nil {
			return rErr
		}
		out[rel] = string(b)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("snapshot walk: %v", walkErr)
	}
	return out
}

func TestTaskService_GetTaskDiff_QuarantineHasNoSideEffects(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	ctx := context.Background()

	viewer := &models.User{ID: common.NewID(), Email: "dq-viewer@example.com", DisplayName: "V", Subject: "sub-dq-v", Issuer: "test"}
	outsider := &models.User{ID: common.NewID(), Email: "dq-out@example.com", DisplayName: "O", Subject: "sub-dq-o", Issuer: "test"}
	db.Create(viewer)
	db.Create(outsider)
	team := &models.Team{ID: common.NewID(), Name: "Diff Quarantine Team", Slug: "diff-quarantine-team"}
	db.Create(team)
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: viewer.ID, Role: models.RoleViewer})
	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Diff Quarantine Project"}
	db.Create(project)

	// Real repo with an uncommitted change: CaptureSnapshot would add/commit it and edit config.
	worktree := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = worktree
		if out, gitErr := cmd.CombinedOutput(); gitErr != nil {
			t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), gitErr, out)
		}
	}
	runGit("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(worktree, "f.txt"), []byte("one"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit("add", "f.txt")
	runGit("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(worktree, "f.txt"), []byte("two"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "untracked.txt"), []byte("u"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	task := &models.Task{ID: common.NewID(), ProjectID: project.ID, Title: "T", Prompt: "p", WorktreePath: &worktree, Status: models.TaskStatusReview}
	db.Create(task)

	// PATH shim: any `git` invocation is recorded and fails.
	shimDir := t.TempDir()
	record := filepath.Join(shimDir, "invocations.log")
	shim := "#!/bin/sh\necho \"$@\" >> '" + record + "'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(shimDir, "git"), []byte(shim), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	before := snapshotTree(t, worktree)

	svc := service.NewTaskService(taskRepo, attemptRepo, &mockQueueClient{}, nil,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo))

	diff, err := svc.GetTaskDiff(ctx, task.ID, viewer.ID)
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) || !errors.Is(err, common.ErrRuntimeQuarantined) {
		t.Fatalf("viewer: expected quarantine error, got %v", err)
	}
	if diff != "" {
		t.Errorf("viewer: expected empty diff, got %q", diff)
	}
	if d, err := svc.GetTaskDiff(ctx, task.ID, ""); !errors.Is(err, common.ErrForbidden) || d != "" {
		t.Errorf("empty userID: expected ErrForbidden and empty diff, got %q / %v", d, err)
	}
	if _, err := svc.GetTaskDiff(ctx, task.ID, outsider.ID); !errors.Is(err, common.ErrForbidden) {
		t.Errorf("expected ErrForbidden for outsider, got %v", err)
	}

	if b, err := os.ReadFile(record); err == nil {
		t.Fatalf("git was invoked: %s", b)
	}
	after := snapshotTree(t, worktree)
	if len(before) != len(after) {
		t.Fatalf("worktree file listing changed: %d -> %d entries", len(before), len(after))
	}
	for p, c := range before {
		if after[p] != c {
			t.Errorf("worktree entry %s changed", p)
		}
	}
}
