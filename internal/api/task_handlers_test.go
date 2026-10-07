package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/common"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/service"
	"agent-task-manager/internal/ws"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type mockQueueClient struct{}

func (m *mockQueueClient) EnqueueTask(ctx context.Context, task *models.Task) (string, error) {
	id := "mock-asynq-id-123"
	return id, nil
}

func (m *mockQueueClient) CancelTask(ctx context.Context, queueName string, asynqTaskID string) error {
	return nil
}

func (m *mockQueueClient) Close() error {
	return nil
}

func setupAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}

	tables := []string{
		`CREATE TABLE users (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			issuer TEXT NOT NULL,
			subject TEXT NOT NULL,
			email TEXT,
			display_name TEXT
		);`,
		`CREATE TABLE teams (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			name TEXT NOT NULL,
			slug TEXT NOT NULL
		);`,
		`CREATE TABLE team_memberships (
			team_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			role INTEGER NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (team_id, user_id)
		);`,
		`CREATE TABLE projects (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			team_id TEXT NOT NULL,
			name TEXT NOT NULL,
			repo_path TEXT NOT NULL
		);`,
		`CREATE TABLE tasks (
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
		);`,
		`CREATE TABLE task_attempts (
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
		);`,
	}

	for _, stmt := range tables {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to run setup migration %q: %v", stmt, err)
		}
	}

	return db
}

func TestTaskAndTeamHandlers(t *testing.T) {
	db := setupAPITestDB(t)

	cfg := &config.Config{
		AppEnv:        "development",
		SessionSecret: "test-session-secret-32-bytes-long!",
	}
	sessionManager := auth.NewSessionManager([]byte(cfg.SessionSecret), 24*time.Hour, false)

	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	queueClient := &mockQueueClient{}
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	taskSvc := service.NewTaskService(taskRepo, attemptRepo, queueClient, hub)
	wsHandler := api.NewWSHandler(hub, api.WithWSServices(taskSvc, nil))

	server := api.NewServer(
		cfg,
		sessionManager,
		nil,
		nil,
		nil,
		api.WithTaskService(taskSvc),
		api.WithTeamRepository(teamRepo),
		api.WithProjectRepository(projectRepo),
		api.WithWSHandler(wsHandler),
	)

	// Seed team and project
	teamID := common.NewID()
	projectID := common.NewID()
	team := &models.Team{
		ID:   teamID,
		Name: "Engineering",
		Slug: "engineering",
	}
	if err := db.Create(team).Error; err != nil {
		t.Fatalf("failed to seed team: %v", err)
	}

	project := &models.Project{
		ID:       projectID,
		TeamID:   teamID,
		Name:     "Backend API",
		RepoPath: "/repos/backend",
	}
	if err := db.Create(project).Error; err != nil {
		t.Fatalf("failed to seed project: %v", err)
	}

	t.Run("GET /api/v1/teams", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/teams", nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("GET /api/v1/teams failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", resp.StatusCode)
		}

		var teams []models.Team
		if err := json.NewDecoder(resp.Body).Decode(&teams); err != nil {
			t.Fatalf("failed to decode teams response: %v", err)
		}

		if len(teams) != 1 || teams[0].ID != teamID {
			t.Errorf("expected 1 team with id %s, got %+v", teamID, teams)
		}
	})

	t.Run("GET /api/v1/projects", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects?team_id="+teamID, nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("GET /api/v1/projects failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", resp.StatusCode)
		}

		var projects []models.Project
		if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
			t.Fatalf("failed to decode projects response: %v", err)
		}

		if len(projects) != 1 || projects[0].ID != projectID {
			t.Errorf("expected 1 project with id %s, got %+v", projectID, projects)
		}
	})

	var createdTaskID string

	t.Run("POST /api/v1/tasks - create valid task", func(t *testing.T) {
		model := "claude-3-5-sonnet"
		payload := dto.CreateTaskRequest{
			ProjectID:    projectID,
			Title:        "Implement new API route",
			Prompt:       "Add routes for team & project management",
			AssigneeID:   "agent-claude",
			AssigneeType: models.AssigneeAgent,
			Priority:     int(models.PriorityHigh),
			Model:        &model,
			Skills:       []string{"go", "api"},
			MCPServers:   []string{"db-mcp"},
		}

		jsonBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")

		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("POST /api/v1/tasks failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201 Created, got: %d", resp.StatusCode)
		}

		var created dto.TaskResponse
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			t.Fatalf("failed to decode task response: %v", err)
		}

		if created.ID == "" || created.Title != payload.Title {
			t.Errorf("unexpected created task: %+v", created)
		}
		if created.Status != models.TaskStatusQueued {
			t.Errorf("expected status queued, got %s", created.Status)
		}
		if created.AsynqTaskID == nil || *created.AsynqTaskID != "mock-asynq-id-123" {
			t.Errorf("expected enqueued asynq id, got %+v", created.AsynqTaskID)
		}

		createdTaskID = created.ID
	})

	t.Run("POST /api/v1/tasks - invalid validation", func(t *testing.T) {
		payload := dto.CreateTaskRequest{
			ProjectID: "invalid-uuid",
			Title:     "",
		}

		jsonBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")

		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("POST /api/v1/tasks failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got: %d", resp.StatusCode)
		}
	})

	t.Run("GET /api/v1/tasks/:task_id - found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+createdTaskID, nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("GET /api/v1/tasks/:id failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", resp.StatusCode)
		}

		var got dto.TaskResponse
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if got.ID != createdTaskID {
			t.Errorf("expected task id %s, got %s", createdTaskID, got.ID)
		}
	})

	t.Run("GET /api/v1/tasks/:task_id - not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/non-existent-id", nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got: %d", resp.StatusCode)
		}
	})

	t.Run("GET /api/v1/tasks - list tasks", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?project_id="+projectID, nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("GET /api/v1/tasks failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", resp.StatusCode)
		}

		var tasks []dto.TaskResponse
		if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
			t.Fatalf("failed to decode list response: %v", err)
		}

		if len(tasks) != 1 || tasks[0].ID != createdTaskID {
			t.Errorf("expected 1 task with id %s, got %+v", createdTaskID, tasks)
		}
	})

	var createdSubtaskID string

	t.Run("POST /api/v1/tasks - create child task with parent_task_id", func(t *testing.T) {
		subtaskPayload := dto.CreateTaskRequest{
			ProjectID:    projectID,
			ParentTaskID: &createdTaskID,
			Title:        "Implement subtask for parent",
			Prompt:       "Break down problem and report",
			AssigneeID:   "agent-sub",
			AssigneeType: models.AssigneeAgent,
			Priority:     int(models.PriorityNormal),
		}

		jsonBytes, _ := json.Marshal(subtaskPayload)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")

		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("POST /api/v1/tasks subtask failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201 Created, got: %d", resp.StatusCode)
		}

		var createdSub dto.TaskResponse
		if err := json.NewDecoder(resp.Body).Decode(&createdSub); err != nil {
			t.Fatalf("failed to decode subtask response: %v", err)
		}

		if createdSub.ParentTaskID == nil || *createdSub.ParentTaskID != createdTaskID {
			t.Errorf("expected ParentTaskID %s, got %+v", createdTaskID, createdSub.ParentTaskID)
		}
		createdSubtaskID = createdSub.ID
	})

	t.Run("GET /api/v1/tasks?parent_task_id=... - list subtasks", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?parent_task_id="+createdTaskID, nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("GET /api/v1/tasks with parent_task_id failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", resp.StatusCode)
		}

		var subtasks []dto.TaskResponse
		if err := json.NewDecoder(resp.Body).Decode(&subtasks); err != nil {
			t.Fatalf("failed to decode subtasks list: %v", err)
		}

		if len(subtasks) != 1 || subtasks[0].ID != createdSubtaskID {
			t.Errorf("expected 1 subtask with id %s, got %+v", createdSubtaskID, subtasks)
		}
		if subtasks[0].ParentTaskID == nil || *subtasks[0].ParentTaskID != createdTaskID {
			t.Errorf("expected subtask parent_task_id %s, got %+v", createdTaskID, subtasks[0].ParentTaskID)
		}
	})

	t.Run("POST /api/v1/tasks - invalid parent_task_id format", func(t *testing.T) {
		badParentID := "not-a-valid-uuid"
		payload := dto.CreateTaskRequest{
			ProjectID:    projectID,
			ParentTaskID: &badParentID,
			Title:        "Invalid parent uuid",
			Prompt:       "testing",
			AssigneeID:   "agent-sub",
			AssigneeType: models.AssigneeAgent,
			Priority:     int(models.PriorityNormal),
		}

		jsonBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewReader(jsonBytes))
		req.Header.Set("Content-Type", "application/json")

		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("POST /api/v1/tasks failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on invalid parent_task_id, got: %d", resp.StatusCode)
		}
	})

	t.Run("POST /api/v1/tasks/:task_id/cancel", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+createdTaskID+"/cancel", nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("POST cancel failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got: %d", resp.StatusCode)
		}

		var body map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode cancel response: %v", err)
		}

		if body["status"] != "cancelled" {
			t.Errorf("expected status cancelled, got %v", body["status"])
		}

		// Verify task status in DB
		task, err := taskSvc.GetTask(context.Background(), createdTaskID)
		if err != nil {
			t.Fatalf("failed to get task: %v", err)
		}
		if task.Status != models.TaskStatusCancelled {
			t.Errorf("expected task status %s, got %s", models.TaskStatusCancelled, task.Status)
		}
	})

	t.Run("GET /api/v1/ws/terminal/:task_id - non-websocket returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ws/terminal/"+createdTaskID, nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for non-websocket request, got %d", resp.StatusCode)
		}
	})

	t.Run("POST /api/v1/tasks/:id/pr - manual PR creation endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+createdTaskID+"/pr", nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		// Without git remote worktree configured in this mock, it returns 400 Bad Request with descriptive message
		if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusOK {
			t.Errorf("expected 400 or 200, got %d", resp.StatusCode)
		}
	})
}

func TestTaskSessionEndpoints(t *testing.T) {
	db := setupAPITestDB(t)

	cfg := &config.Config{
		AppEnv:        "development",
		SessionSecret: "test-session-secret-32-bytes-long!",
	}
	sessionManager := auth.NewSessionManager([]byte(cfg.SessionSecret), 24*time.Hour, false)

	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	tokenRepo := repository.NewTokenRepository(db)
	userRepo := repository.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, teamRepo, tokenRepo, nil)

	teamID := common.NewID()
	projectID := common.NewID()
	team := &models.Team{
		ID:        teamID,
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
		ID:       projectID,
		TeamID:   teamID,
		Name:     "Project Session",
		RepoPath: "/repos/session",
	}
	db.Create(project)

	sessionID := "ses_atm_task100"
	task := &models.Task{
		ID:           common.NewID(),
		ProjectID:    project.ID,
		Title:        "Session Endpoints Task",
		Prompt:       "Run and inspect session",
		AssigneeID:   "agent-1",
		AssigneeType: models.AssigneeAgent,
		Status:       models.TaskStatusInProgress,
		Priority:     models.PriorityNormal,
		SessionID:    &sessionID,
		Skills:       models.StringArray{},
		MCPServers:   models.StringArray{},
	}
	db.Create(task)

	// Mock OpenCodeClient
	mockClient := &mockOpenCodeClientRunner{}
	taskSvc := service.NewTaskService(
		taskRepo,
		attemptRepo,
		nil,
		nil,
		service.WithTaskSessionDependencies(mockClient, projectRepo, teamRepo),
	)

	server := api.NewServer(
		cfg,
		sessionManager,
		authSvc,
		nil,
		nil,
		api.WithTaskService(taskSvc),
		api.WithTeamRepository(teamRepo),
		api.WithProjectRepository(projectRepo),
	)

	// Helper to create session cookie
	createCookie := func(user *models.User) *http.Cookie {
		cookieVal, err := sessionManager.SignPayload(auth.SessionClaims{
			UserID:    user.ID,
			Issuer:    user.Issuer,
			Subject:   user.Subject,
			ExpiresAt: time.Now().Add(1 * time.Hour),
		})
		if err != nil {
			t.Fatalf("failed to create session cookie: %v", err)
		}
		return &http.Cookie{
			Name:  auth.SessionCookieName,
			Value: cookieVal,
		}
	}

	viewerCookie := createCookie(viewerUser)
	memberCookie := createCookie(memberUser)
	outsiderCookie := createCookie(outsiderUser)

	// 1. AC-SEC-01: 401 Unauthenticated when no auth provided
	t.Run("GET /api/v1/tasks/:id/session - 401 unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+task.ID+"/session", nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", resp.StatusCode)
		}
	})

	t.Run("POST /api/v1/tasks/:id/session/prompt - 401 unauthenticated", func(t *testing.T) {
		body, _ := json.Marshal(dto.TaskSessionPromptRequest{Prompt: "hello"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/session/prompt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", resp.StatusCode)
		}
	})

	t.Run("POST /api/v1/tasks/:id/session/compact - 401 unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/session/compact", nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", resp.StatusCode)
		}
	})

	// 2. AC-SEC-02: 403 Forbidden for outsider (non-member)
	t.Run("GET /api/v1/tasks/:id/session - 403 non-member", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+task.ID+"/session", nil)
		req.AddCookie(outsiderCookie)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
		}
	})

	t.Run("POST /api/v1/tasks/:id/session/prompt - 403 for viewer (requires member)", func(t *testing.T) {
		body, _ := json.Marshal(dto.TaskSessionPromptRequest{Prompt: "hello"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/session/prompt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(viewerCookie)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
		}
	})

	// 3. AC-SEC-06: Input validation on PromptSession
	t.Run("POST /api/v1/tasks/:id/session/prompt - input validation empty prompt", func(t *testing.T) {
		body, _ := json.Marshal(dto.TaskSessionPromptRequest{Prompt: ""})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/session/prompt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(memberCookie)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for empty prompt, got %d", resp.StatusCode)
		}
	})

	t.Run("POST /api/v1/tasks/:id/session/prompt - input validation prompt exceeding 32768 bytes", func(t *testing.T) {
		hugePrompt := strings.Repeat("A", 32769)
		body, _ := json.Marshal(dto.TaskSessionPromptRequest{Prompt: hugePrompt})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/session/prompt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(memberCookie)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for oversized prompt, got %d", resp.StatusCode)
		}
	})

	// 4. Successful inspection & prompt & compact
	t.Run("GET /api/v1/tasks/:id/session - success for viewer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+task.ID+"/session", nil)
		req.AddCookie(viewerCookie)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
		}

		var sessResp dto.TaskSessionResponseDTO
		if err := json.NewDecoder(resp.Body).Decode(&sessResp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if sessResp.SessionID != sessionID || sessResp.TaskID != task.ID {
			t.Errorf("unexpected session response: %+v", sessResp)
		}
	})

	t.Run("POST /api/v1/tasks/:id/session/prompt - success for member", func(t *testing.T) {
		body, _ := json.Marshal(dto.TaskSessionPromptRequest{Prompt: "Next instruction"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/session/prompt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(memberCookie)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
		}
	})

	t.Run("POST /api/v1/tasks/:id/session/compact - success for member", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/session/compact", nil)
		req.AddCookie(memberCookie)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
		}
	})

	// 5. 503 Daemon Offline
	t.Run("GET /api/v1/tasks/:id/session - 503 daemon offline", func(t *testing.T) {
		mockClient.failWith503 = true
		defer func() { mockClient.failWith503 = false }()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+task.ID+"/session", nil)
		req.AddCookie(memberCookie)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("expected 503 Service Unavailable, got %d", resp.StatusCode)
		}
	})
}

type mockOpenCodeClientRunner struct {
	failWith503 bool
}

func (m *mockOpenCodeClientRunner) GetSession(ctx context.Context, sessionID string) (*runner.OpenCodeSessionDTO, error) {
	if m.failWith503 {
		return nil, common.ErrServiceUnavailable
	}
	return &runner.OpenCodeSessionDTO{ID: sessionID, Title: "Test Session"}, nil
}

func (m *mockOpenCodeClientRunner) GetSessionMessages(ctx context.Context, sessionID string) ([]dto.OpenCodeMessageDTO, error) {
	if m.failWith503 {
		return nil, common.ErrServiceUnavailable
	}
	return []dto.OpenCodeMessageDTO{{ID: "msg-1"}}, nil
}

func (m *mockOpenCodeClientRunner) PromptSession(ctx context.Context, sessionID, prompt string) error {
	if m.failWith503 {
		return common.ErrServiceUnavailable
	}
	return nil
}

func (m *mockOpenCodeClientRunner) CompactSession(ctx context.Context, sessionID string) error {
	if m.failWith503 {
		return common.ErrServiceUnavailable
	}
	return nil
}

func (m *mockOpenCodeClientRunner) EnsureSessionAtLocation(ctx context.Context, sessionID, title, directory string) error {
	if m.failWith503 {
		return common.ErrServiceUnavailable
	}
	return nil
}
