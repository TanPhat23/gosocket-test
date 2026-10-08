package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/dto"
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

func TestTaskServiceCreateSubtaskAndListChildren(t *testing.T) {
	gormDB := setupTestDB(t)
	_, project, parentTask := createTestUserAndTask(t, gormDB)
	taskRepo := repository.NewTaskRepository(gormDB)
	attemptRepo := repository.NewAttemptRepository(gormDB)
	mockQueue := &mockQueueClient{}

	taskSvc := service.NewTaskService(taskRepo, attemptRepo, mockQueue, nil)
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

	createdChild, err := taskSvc.CreateTask(ctx, subtaskReq)
	if err != nil {
		t.Fatalf("CreateTask child failed: %v", err)
	}

	if createdChild.ParentTaskID == nil || *createdChild.ParentTaskID != parentTask.ID {
		t.Fatalf("expected ParentTaskID %s, got %+v", parentTask.ID, createdChild.ParentTaskID)
	}

	children, err := taskSvc.ListSubtasks(ctx, parentTask.ID)
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
	_, project, _ := createTestUserAndTask(t, gormDB)
	taskRepo := repository.NewTaskRepository(gormDB)
	attemptRepo := repository.NewAttemptRepository(gormDB)
	mockQueue := &mockQueueClient{}

	taskSvc := service.NewTaskService(taskRepo, attemptRepo, mockQueue, nil)
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

	created, err := taskSvc.CreateTask(ctx, req)
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
	getSessionFunc        func(ctx context.Context, sessionID string) (*runner.OpenCodeSessionDTO, error)
	getSessionMsgsFunc    func(ctx context.Context, sessionID string) ([]dto.OpenCodeMessageDTO, error)
	promptSessionFunc     func(ctx context.Context, sessionID, prompt string) error
	compactSessionFunc    func(ctx context.Context, sessionID string) error
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

	// 1. GetTaskSession with viewer user -> Success
	sessResp, err := taskSvc.GetTaskSession(ctx, task.ID, viewerUser.ID)
	if err != nil {
		t.Fatalf("expected viewer to be able to GetTaskSession: %v", err)
	}
	if sessResp.SessionID != sessionID || sessResp.TaskID != task.ID {
		t.Errorf("unexpected session response: %+v", sessResp)
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

	// 4. PromptTaskSession with member user -> Success
	err = taskSvc.PromptTaskSession(ctx, task.ID, memberUser.ID, promptReq)
	if err != nil {
		t.Fatalf("expected member to be able to PromptTaskSession: %v", err)
	}

	// 5. CompactTaskSession with viewer user -> 403 Forbidden
	err = taskSvc.CompactTaskSession(ctx, task.ID, viewerUser.ID)
	if !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for viewer on CompactTaskSession, got %v", err)
	}

	// 6. CompactTaskSession with member user -> Success
	err = taskSvc.CompactTaskSession(ctx, task.ID, memberUser.ID)
	if err != nil {
		t.Fatalf("expected member to be able to CompactTaskSession: %v", err)
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

	_, err = offlineSvc.GetTaskSession(ctx, task.ID, memberUser.ID)
	if !errors.Is(err, common.ErrServiceUnavailable) {
		t.Fatalf("expected ErrServiceUnavailable on GetTaskSession, got %v", err)
	}

	err = offlineSvc.PromptTaskSession(ctx, task.ID, memberUser.ID, promptReq)
	if !errors.Is(err, common.ErrServiceUnavailable) {
		t.Fatalf("expected ErrServiceUnavailable on PromptTaskSession, got %v", err)
	}

	err = offlineSvc.CompactTaskSession(ctx, task.ID, memberUser.ID)
	if !errors.Is(err, common.ErrServiceUnavailable) {
		t.Fatalf("expected ErrServiceUnavailable on CompactTaskSession, got %v", err)
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
	if err != nil {
		t.Fatalf("expected nil error from GetTaskDiff: %v", err)
	}
	if diff != "" {
		t.Errorf("expected empty diff on fresh dir, got: %s", diff)
	}
}
