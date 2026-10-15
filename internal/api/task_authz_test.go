package api_test

import (
	"bytes"
	"encoding/json"
	"io"
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
	"agent-task-manager/internal/service"
)

// TestTaskRoutes_AuthenticationAndTeamAuthorization covers every previously open task route.
func TestTaskRoutes_AuthenticationAndTeamAuthorization(t *testing.T) {
	db := setupAPITestDB(t)
	cfg := &config.Config{AppEnv: "development", SessionSecret: "test-session-secret-32-bytes-long!"}
	sm := auth.NewSessionManager([]byte(cfg.SessionSecret), 24*time.Hour, false)

	taskRepo := repository.NewTaskRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	authSvc := service.NewAuthService(repository.NewUserRepository(db), teamRepo, repository.NewTokenRepository(db), nil)
	taskSvc := service.NewTaskService(taskRepo, repository.NewAttemptRepository(db), &mockQueueClient{}, nil,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo))
	server := api.NewServer(cfg, sm, authSvc, nil, nil, api.WithTaskService(taskSvc),
		api.WithTeamRepository(teamRepo), api.WithProjectRepository(projectRepo))

	mkUser := func(name string) (*models.User, *http.Cookie) {
		u := &models.User{ID: common.NewID(), Email: name + "@example.com", DisplayName: name, Subject: "sub-" + name, Issuer: "test"}
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("user: %v", err)
		}
		v, err := sm.SignPayload(auth.SessionClaims{IssuedAt: time.Now(), UserID: u.ID, Issuer: u.Issuer, Subject: u.Subject, ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatalf("cookie: %v", err)
		}
		return u, &http.Cookie{Name: auth.SessionCookieName, Value: v}
	}
	memberA, memberACookie := mkUser("member-a")
	viewerA, viewerACookie := mkUser("viewer-a")
	memberB, memberBCookie := mkUser("member-b")
	_, outsiderCookie := mkUser("outsider")

	mkTeamProject := func(name string) (*models.Team, *models.Project) {
		team := &models.Team{ID: common.NewID(), Name: name, Slug: name}
		proj := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "P-" + name, RepoPath: "/" + name}
		db.Create(team)
		db.Create(proj)
		return team, proj
	}
	teamA, projA := mkTeamProject("team-a")
	teamB, projB := mkTeamProject("team-b")
	db.Create(&models.TeamMembership{TeamID: teamA.ID, UserID: memberA.ID, Role: models.RoleMember})
	db.Create(&models.TeamMembership{TeamID: teamA.ID, UserID: viewerA.ID, Role: models.RoleViewer})
	db.Create(&models.TeamMembership{TeamID: teamB.ID, UserID: memberB.ID, Role: models.RoleMember})

	mkTask := func(p *models.Project, title string) *models.Task {
		tk := &models.Task{ID: common.NewID(), ProjectID: p.ID, Title: title, Prompt: "p", AssigneeID: "a",
			AssigneeType: models.AssigneeAgent, Status: models.TaskStatusReview, Priority: models.PriorityNormal,
			Skills: models.StringArray{}, MCPServers: models.StringArray{}}
		if err := db.Create(tk).Error; err != nil {
			t.Fatalf("task: %v", err)
		}
		return tk
	}
	taskA := mkTask(projA, "SECRET-TITLE-A")
	taskB := mkTask(projB, "SECRET-TITLE-B")

	do := func(t *testing.T, method, path string, body any, cookie *http.Cookie) (int, string) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, path, rd)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.AddCookie(cookie)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	// Task-ID routes: viewer allowed except PR routes, which need RoleMember (quarantined routes then 503), others rejected before any data/503.
	type route struct {
		name, method, path string
		okStatus           int // status for an authorized member
		viewerStatus       int // status for a viewer: PR routes need RoleMember
	}
	routes := []route{
		{"get", http.MethodGet, "/api/v1/tasks/" + taskA.ID, http.StatusOK, http.StatusOK},
		{"diff", http.MethodGet, "/api/v1/tasks/" + taskA.ID + "/diff", http.StatusServiceUnavailable, http.StatusServiceUnavailable},
		{"create-pr", http.MethodPost, "/api/v1/tasks/" + taskA.ID + "/pr", http.StatusServiceUnavailable, http.StatusForbidden},
		{"sync-pr", http.MethodPost, "/api/v1/tasks/" + taskA.ID + "/sync-pr", http.StatusServiceUnavailable, http.StatusForbidden},
		{"pr-sync", http.MethodPost, "/api/v1/tasks/" + taskA.ID + "/pr/sync", http.StatusServiceUnavailable, http.StatusForbidden},
	}
	for _, r := range routes {
		t.Run(r.name, func(t *testing.T) {
			if code, body := do(t, r.method, r.path, nil, nil); code != http.StatusUnauthorized || strings.Contains(body, "SECRET") {
				t.Errorf("unauthenticated: expected 401 without data, got %d %s", code, body)
			}
			for who, c := range map[string]*http.Cookie{"outsider": outsiderCookie, "member of other team": memberBCookie} {
				code, body := do(t, r.method, r.path, nil, c)
				if code != http.StatusForbidden || strings.Contains(body, "SECRET") || strings.Contains(body, "unavailable") {
					t.Errorf("%s: expected 403 without data, got %d %s", who, code, body)
				}
			}
			for who, c := range map[string]struct {
				cookie *http.Cookie
				want   int
			}{"viewer": {viewerACookie, r.viewerStatus}, "member": {memberACookie, r.okStatus}} {
				code, body := do(t, r.method, r.path, nil, c.cookie)
				if code != c.want {
					t.Errorf("%s: expected %d, got %d %s", who, c.want, code, body)
				}
			}
			// Unknown task keeps the existing not-found behaviour for an authenticated caller.
			unknown := strings.Replace(r.path, taskA.ID, common.NewID(), 1)
			if code, _ := do(t, r.method, unknown, nil, viewerACookie); code != http.StatusNotFound {
				t.Errorf("unknown task: expected 404, got %d", code)
			}
		})
	}

	t.Run("get returns task to viewer", func(t *testing.T) {
		code, body := do(t, http.MethodGet, "/api/v1/tasks/"+taskA.ID, nil, viewerACookie)
		if code != http.StatusOK || !strings.Contains(body, "SECRET-TITLE-A") {
			t.Fatalf("expected task data, got %d %s", code, body)
		}
	})

	t.Run("list without project_id", func(t *testing.T) {
		if code, _ := do(t, http.MethodGet, "/api/v1/tasks", nil, nil); code != http.StatusUnauthorized {
			t.Errorf("unauthenticated: expected 401, got %d", code)
		}
		for name, c := range map[string]struct {
			cookie *http.Cookie
			want   string
			not    string
		}{
			"team A viewer": {viewerACookie, taskA.ID, taskB.ID},
			"team B member": {memberBCookie, taskB.ID, taskA.ID},
		} {
			code, body := do(t, http.MethodGet, "/api/v1/tasks", nil, c.cookie)
			if code != http.StatusOK || !strings.Contains(body, c.want) || strings.Contains(body, c.not) {
				t.Errorf("%s: got %d %s", name, code, body)
			}
		}
		code, body := do(t, http.MethodGet, "/api/v1/tasks", nil, outsiderCookie)
		if code != http.StatusOK || strings.TrimSpace(body) != "[]" {
			t.Errorf("outsider: expected [], got %d %s", code, body)
		}
	})

	t.Run("list with project_id", func(t *testing.T) {
		if code, _ := do(t, http.MethodGet, "/api/v1/tasks?project_id="+projA.ID, nil, nil); code != http.StatusUnauthorized {
			t.Errorf("unauthenticated: expected 401, got %d", code)
		}
		if code, body := do(t, http.MethodGet, "/api/v1/tasks?project_id="+projA.ID, nil, viewerACookie); code != http.StatusOK || !strings.Contains(body, taskA.ID) {
			t.Errorf("viewer: got %d %s", code, body)
		}
		for who, c := range map[string]*http.Cookie{"outsider": outsiderCookie, "other team": memberBCookie} {
			if code, body := do(t, http.MethodGet, "/api/v1/tasks?project_id="+projA.ID, nil, c); code != http.StatusForbidden || strings.Contains(body, taskA.ID) {
				t.Errorf("%s: expected 403, got %d %s", who, code, body)
			}
		}
	})

	t.Run("list subtasks via parent", func(t *testing.T) {
		path := "/api/v1/tasks?parent_task_id=" + taskA.ID
		if code, _ := do(t, http.MethodGet, path, nil, nil); code != http.StatusUnauthorized {
			t.Errorf("unauthenticated: expected 401, got %d", code)
		}
		if code, _ := do(t, http.MethodGet, path, nil, viewerACookie); code != http.StatusOK {
			t.Errorf("viewer: expected 200, got %d", code)
		}
		for who, c := range map[string]*http.Cookie{"outsider": outsiderCookie, "other team": memberBCookie} {
			if code, _ := do(t, http.MethodGet, path, nil, c); code != http.StatusForbidden {
				t.Errorf("%s: expected 403, got %d", who, code)
			}
		}
	})

	t.Run("create", func(t *testing.T) {
		var before int64
		db.Model(&models.Task{}).Count(&before)
		payload := dto.CreateTaskRequest{ProjectID: projA.ID, Title: "new", Prompt: "p", AssigneeID: "a",
			AssigneeType: models.AssigneeAgent, Priority: int(models.PriorityNormal)}

		if code, _ := do(t, http.MethodPost, "/api/v1/tasks", payload, nil); code != http.StatusUnauthorized {
			t.Errorf("unauthenticated: expected 401, got %d", code)
		}
		for who, c := range map[string]*http.Cookie{"viewer": viewerACookie, "outsider": outsiderCookie, "other team": memberBCookie} {
			if code, _ := do(t, http.MethodPost, "/api/v1/tasks", payload, c); code != http.StatusForbidden {
				t.Errorf("%s: expected 403, got %d", who, code)
			}
		}
		var after int64
		db.Model(&models.Task{}).Count(&after)
		if after != before {
			t.Fatalf("denied creates must not persist rows: %d -> %d", before, after)
		}
		if code, body := do(t, http.MethodPost, "/api/v1/tasks", payload, memberACookie); code != http.StatusCreated {
			t.Fatalf("member: expected 201, got %d %s", code, body)
		}

		// Parent task from another project is rejected with 400 and persists nothing.
		db.Model(&models.Task{}).Count(&before)
		bad := payload
		bad.ParentTaskID = &taskB.ID
		if code, _ := do(t, http.MethodPost, "/api/v1/tasks", bad, memberACookie); code != http.StatusBadRequest {
			t.Errorf("cross-project parent: expected 400, got %d", code)
		}
		db.Model(&models.Task{}).Count(&after)
		if after != before {
			t.Errorf("rejected create persisted a row")
		}
	})
}
