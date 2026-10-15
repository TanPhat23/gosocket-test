package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
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
	"agent-task-manager/internal/ws"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
)

type routeAuthzFixture struct {
	server *api.Server
	hub    *ws.Hub

	cookies map[string]*http.Cookie

	teamA, teamB             *models.Team
	projA, projB             *models.Project
	credA                    *models.Credential
	integA, integB           *models.ProjectIntegration
	taskA, taskB             *models.Task
	countProjects            func() int64
	integrationExternalID    func(id string) (string, bool)
	projectExists            func(id string) bool
	createTeamProjectForTest func(team *models.Team, name string) *models.Project
}

// setupRouteAuthzFixture wires a full Server with two teams; team A has owner/admin/member/viewer users,
// team B has a single member, and "outsider" belongs to no team.
func setupRouteAuthzFixture(t *testing.T) *routeAuthzFixture {
	t.Helper()
	db := setupAPITestDB(t)
	// Each new connection to ":memory:" would open an empty database; pin the pool to one connection
	// because /ws/events authorizes broadcasts from concurrent goroutines.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	for _, stmt := range []string{
		`CREATE TABLE credentials (
			id TEXT PRIMARY KEY,
			team_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			label TEXT NOT NULL,
			encrypted_data TEXT NOT NULL,
			key_fingerprint TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE project_integrations (
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
		);`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}

	cfg := &config.Config{AppEnv: "development", SessionSecret: "test-session-secret-32-bytes-long!"}
	sm := auth.NewSessionManager([]byte(cfg.SessionSecret), 24*time.Hour, false)

	taskRepo := repository.NewTaskRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	authSvc := service.NewAuthService(repository.NewUserRepository(db), teamRepo, repository.NewTokenRepository(db), nil)

	hub := ws.NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)

	taskSvc := service.NewTaskService(taskRepo, repository.NewAttemptRepository(db), &mockQueueClient{}, hub,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo))
	integSvc := service.NewIntegrationService(integRepo, projectRepo, credRepo)
	wsHandler := api.NewWSHandler(hub, api.WithWSServices(taskSvc, nil))
	server := api.NewServer(cfg, sm, authSvc, nil, nil,
		api.WithTaskService(taskSvc),
		api.WithIntegrationService(integSvc),
		api.WithTeamRepository(teamRepo),
		api.WithProjectRepository(projectRepo),
		api.WithWSHandler(wsHandler))

	f := &routeAuthzFixture{server: server, hub: hub, cookies: map[string]*http.Cookie{}}

	mkUser := func(name string) *models.User {
		u := &models.User{ID: common.NewID(), Email: name + "@example.com", DisplayName: name, Subject: "sub-" + name, Issuer: "test"}
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("user: %v", err)
		}
		v, err := sm.SignPayload(auth.SessionClaims{UserID: u.ID, Issuer: u.Issuer, Subject: u.Subject, ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatalf("cookie: %v", err)
		}
		f.cookies[name] = &http.Cookie{Name: auth.SessionCookieName, Value: v}
		return u
	}
	mkTeam := func(name string) *models.Team {
		team := &models.Team{ID: common.NewID(), Name: name, Slug: name}
		if err := db.Create(team).Error; err != nil {
			t.Fatalf("team: %v", err)
		}
		return team
	}
	mkProject := func(team *models.Team, name string) *models.Project {
		p := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: name, RepoPath: "/" + name}
		if err := db.Create(p).Error; err != nil {
			t.Fatalf("project: %v", err)
		}
		return p
	}
	member := func(team *models.Team, u *models.User, role models.TeamRole) {
		if err := db.Create(&models.TeamMembership{TeamID: team.ID, UserID: u.ID, Role: role}).Error; err != nil {
			t.Fatalf("membership: %v", err)
		}
	}

	f.teamA, f.teamB = mkTeam("team-a"), mkTeam("team-b")
	member(f.teamA, mkUser("owner-a"), models.RoleOwner)
	member(f.teamA, mkUser("admin-a"), models.RoleAdmin)
	member(f.teamA, mkUser("member-a"), models.RoleMember)
	member(f.teamA, mkUser("viewer-a"), models.RoleViewer)
	member(f.teamB, mkUser("member-b"), models.RoleMember)
	mkUser("outsider")

	f.projA, f.projB = mkProject(f.teamA, "proj-a"), mkProject(f.teamB, "proj-b")

	mkCred := func(team *models.Team) *models.Credential {
		c := &models.Credential{ID: common.NewID(), TeamID: team.ID, Provider: models.ProviderGitHub, Label: "PAT-" + team.Name, EncryptedData: "x", KeyFingerprint: "fp"}
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("credential: %v", err)
		}
		return c
	}
	f.credA = mkCred(f.teamA)
	credB := mkCred(f.teamB)
	mkInteg := func(p *models.Project, c *models.Credential, ext string) *models.ProjectIntegration {
		i := &models.ProjectIntegration{ID: common.NewID(), ProjectID: p.ID, CredentialID: c.ID, Provider: models.IntegrationProvider("github"),
			ExternalID: ext, Enabled: true, Config: models.JSONMap{}}
		if err := db.Create(i).Error; err != nil {
			t.Fatalf("integration: %v", err)
		}
		return i
	}
	f.integA = mkInteg(f.projA, f.credA, "octo/SECRET-REPO-A")
	f.integB = mkInteg(f.projB, credB, "octo/SECRET-REPO-B")

	mkTask := func(p *models.Project) *models.Task {
		tk := &models.Task{ID: common.NewID(), ProjectID: p.ID, Title: "t", Prompt: "p", AssigneeID: "a",
			AssigneeType: models.AssigneeAgent, Status: models.TaskStatusReview, Priority: models.PriorityNormal,
			Skills: models.StringArray{}, MCPServers: models.StringArray{}}
		if err := db.Create(tk).Error; err != nil {
			t.Fatalf("task: %v", err)
		}
		return tk
	}
	f.taskA, f.taskB = mkTask(f.projA), mkTask(f.projB)

	f.countProjects = func() int64 {
		var n int64
		db.Model(&models.Project{}).Count(&n)
		return n
	}
	f.projectExists = func(id string) bool {
		var n int64
		db.Model(&models.Project{}).Where("id = ?", id).Count(&n)
		return n == 1
	}
	f.integrationExternalID = func(id string) (string, bool) {
		var i models.ProjectIntegration
		if err := db.Where("id = ?", id).First(&i).Error; err != nil {
			return "", false
		}
		return i.ExternalID, true
	}
	f.createTeamProjectForTest = mkProject
	return f
}

func (f *routeAuthzFixture) do(t *testing.T, method, path string, body any, who string) (int, string) {
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
	if who != "" {
		c, ok := f.cookies[who]
		if !ok {
			t.Fatalf("unknown user %q", who)
		}
		req.AddCookie(c)
	}
	resp, err := f.server.App.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestModelsRoute_RequiresAuthentication(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	if code, _ := f.do(t, http.MethodGet, "/api/v1/models", nil, ""); code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: expected 401, got %d", code)
	}
	if code, body := f.do(t, http.MethodGet, "/api/v1/models", nil, "outsider"); code != http.StatusOK {
		t.Errorf("authenticated: expected 200, got %d %s", code, body)
	}
}

func TestTeamsRoute_AuthAndMembershipFiltering(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	if code, body := f.do(t, http.MethodGet, "/api/v1/teams", nil, ""); code != http.StatusUnauthorized || strings.Contains(body, f.teamA.ID) {
		t.Fatalf("unauthenticated: expected 401 without data, got %d %s", code, body)
	}
	for who, want := range map[string]*models.Team{"viewer-a": f.teamA, "owner-a": f.teamA, "member-b": f.teamB} {
		code, body := f.do(t, http.MethodGet, "/api/v1/teams", nil, who)
		var teams []models.Team
		if code != http.StatusOK || json.Unmarshal([]byte(body), &teams) != nil {
			t.Fatalf("%s: expected 200 JSON, got %d %s", who, code, body)
		}
		if len(teams) != 1 || teams[0].ID != want.ID {
			t.Errorf("%s: expected only team %s, got %+v", who, want.Name, teams)
		}
	}
	if code, body := f.do(t, http.MethodGet, "/api/v1/teams", nil, "outsider"); code != http.StatusOK || strings.TrimSpace(body) != "[]" {
		t.Errorf("outsider: expected 200 [], got %d %s", code, body)
	}
}

func TestProjectsList_AuthAndMembershipFiltering(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	if code, body := f.do(t, http.MethodGet, "/api/v1/projects", nil, ""); code != http.StatusUnauthorized || strings.Contains(body, f.projA.ID) {
		t.Fatalf("unauthenticated: expected 401 without data, got %d %s", code, body)
	}
	if code, _ := f.do(t, http.MethodGet, "/api/v1/projects?team_id="+f.teamA.ID, nil, ""); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated with team_id: expected 401, got %d", code)
	}

	t.Run("without team_id returns only the caller's teams' projects", func(t *testing.T) {
		for who, c := range map[string]struct{ want, not string }{
			"viewer-a": {f.projA.ID, f.projB.ID},
			"member-b": {f.projB.ID, f.projA.ID},
		} {
			code, body := f.do(t, http.MethodGet, "/api/v1/projects", nil, who)
			if code != http.StatusOK || !strings.Contains(body, c.want) || strings.Contains(body, c.not) {
				t.Errorf("%s: got %d %s", who, code, body)
			}
		}
		if code, body := f.do(t, http.MethodGet, "/api/v1/projects", nil, "outsider"); code != http.StatusOK || strings.TrimSpace(body) != "[]" {
			t.Errorf("outsider: expected 200 [], got %d %s", code, body)
		}
	})

	t.Run("with team_id requires membership of that team", func(t *testing.T) {
		if code, body := f.do(t, http.MethodGet, "/api/v1/projects?team_id="+f.teamA.ID, nil, "viewer-a"); code != http.StatusOK || !strings.Contains(body, f.projA.ID) {
			t.Errorf("viewer-a own team: got %d %s", code, body)
		}
		for _, who := range []string{"outsider", "member-b"} {
			if code, body := f.do(t, http.MethodGet, "/api/v1/projects?team_id="+f.teamA.ID, nil, who); code != http.StatusForbidden || strings.Contains(body, f.projA.ID) {
				t.Errorf("%s foreign team: expected 403 without data, got %d %s", who, code, body)
			}
		}
	})
}

func TestProjectsCreate_RequiresAdminOfTargetTeam(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	payload := map[string]string{"name": "new-proj", "team_id": f.teamA.ID}
	before := f.countProjects()

	if code, _ := f.do(t, http.MethodPost, "/api/v1/projects", payload, ""); code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: expected 401, got %d", code)
	}
	for _, who := range []string{"viewer-a", "member-a", "member-b", "outsider"} {
		if code, body := f.do(t, http.MethodPost, "/api/v1/projects", payload, who); code != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d %s", who, code, body)
		}
	}
	unknownTeam := map[string]string{"name": "x", "team_id": common.NewID()}
	if code, _ := f.do(t, http.MethodPost, "/api/v1/projects", unknownTeam, "owner-a"); code != http.StatusForbidden {
		t.Errorf("unknown team: expected 403, got %d", code)
	}
	if after := f.countProjects(); after != before {
		t.Fatalf("denied creates persisted rows: %d -> %d", before, after)
	}
	if code, _ := f.do(t, http.MethodPost, "/api/v1/projects", map[string]string{"name": "x"}, "admin-a"); code != http.StatusBadRequest {
		t.Errorf("missing team_id: expected 400, got %d", code)
	}
	for _, who := range []string{"admin-a", "owner-a"} {
		p := map[string]string{"name": "new-" + who, "team_id": f.teamA.ID}
		if code, body := f.do(t, http.MethodPost, "/api/v1/projects", p, who); code != http.StatusCreated {
			t.Errorf("%s: expected 201, got %d %s", who, code, body)
		}
	}
	if after := f.countProjects(); after != before+2 {
		t.Errorf("expected 2 new projects, got %d -> %d", before, after)
	}
}

func TestProjectsDelete_RequiresAdminOfProjectTeam(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	path := "/api/v1/projects/" + f.projA.ID

	if code, _ := f.do(t, http.MethodDelete, path, nil, ""); code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: expected 401, got %d", code)
	}
	for _, who := range []string{"viewer-a", "member-a", "member-b", "outsider"} {
		if code, body := f.do(t, http.MethodDelete, path, nil, who); code != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d %s", who, code, body)
		}
	}
	if code, _ := f.do(t, http.MethodDelete, "/api/v1/projects/"+common.NewID(), nil, "owner-a"); code != http.StatusForbidden {
		t.Errorf("unknown project: expected 403 (no existence oracle), got %d", code)
	}
	if !f.projectExists(f.projA.ID) {
		t.Fatal("denied delete removed the project")
	}
	if code, body := f.do(t, http.MethodDelete, path, nil, "admin-a"); code != http.StatusNoContent {
		t.Fatalf("admin-a: expected 204, got %d %s", code, body)
	}
	if f.projectExists(f.projA.ID) {
		t.Fatal("project still exists after authorized delete")
	}
	other := f.createTeamProjectForTest(f.teamA, "proj-a2")
	if code, _ := f.do(t, http.MethodDelete, "/api/v1/projects/"+other.ID, nil, "owner-a"); code != http.StatusNoContent {
		t.Errorf("owner-a: expected 204, got %d", code)
	}
}

func TestIntegrationRoutes_AuthRolesAndProjectOwnership(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	base := "/api/v1/projects/" + f.projA.ID + "/integrations"
	itemA := base + "/" + f.integA.ID
	createBody := dto.CreateIntegrationRequest{CredentialID: f.credA.ID, Provider: "github", Repo: "octo/new-repo"}
	enabled := false
	patchBody := dto.UpdateIntegrationRequest{Enabled: &enabled}

	type route struct {
		name, method, path string
		body               any
		write              bool
		okStatus           int
	}
	routes := []route{
		{"list", http.MethodGet, base, nil, false, http.StatusOK},
		{"get", http.MethodGet, itemA, nil, false, http.StatusOK},
		{"create", http.MethodPost, base, createBody, true, http.StatusCreated},
		{"patch", http.MethodPatch, itemA, patchBody, true, http.StatusOK},
	}
	for _, r := range routes {
		t.Run(r.name, func(t *testing.T) {
			if code, body := f.do(t, r.method, r.path, r.body, ""); code != http.StatusUnauthorized || strings.Contains(body, "SECRET") {
				t.Errorf("unauthenticated: expected 401 without data, got %d %s", code, body)
			}
			for _, who := range []string{"outsider", "member-b"} {
				if code, body := f.do(t, r.method, r.path, r.body, who); code != http.StatusForbidden || strings.Contains(body, "SECRET") {
					t.Errorf("%s: expected 403 without data, got %d %s", who, code, body)
				}
			}
			for _, who := range []string{"viewer-a", "member-a"} {
				code, body := f.do(t, r.method, r.path, r.body, who)
				if r.write && code != http.StatusForbidden {
					t.Errorf("%s write: expected 403, got %d %s", who, code, body)
				}
				if !r.write && code != r.okStatus {
					t.Errorf("%s read: expected %d, got %d %s", who, r.okStatus, code, body)
				}
			}
			if code, body := f.do(t, r.method, r.path, r.body, "admin-a"); code != r.okStatus {
				t.Errorf("admin-a: expected %d, got %d %s", r.okStatus, code, body)
			}
		})
	}

	t.Run("unknown project is 403 without existence oracle", func(t *testing.T) {
		if code, _ := f.do(t, http.MethodGet, "/api/v1/projects/"+common.NewID()+"/integrations", nil, "owner-a"); code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", code)
		}
	})

	t.Run("integration of another project is not reachable through an authorized project", func(t *testing.T) {
		cross := base + "/" + f.integB.ID
		if code, body := f.do(t, http.MethodGet, cross, nil, "owner-a"); code != http.StatusNotFound || strings.Contains(body, "SECRET-REPO-B") {
			t.Errorf("cross-project get: expected 404 without data, got %d %s", code, body)
		}
		if code, _ := f.do(t, http.MethodPatch, cross, patchBody, "owner-a"); code != http.StatusNotFound {
			t.Errorf("cross-project patch: expected 404, got %d", code)
		}
		if code, _ := f.do(t, http.MethodDelete, cross, nil, "owner-a"); code != http.StatusNotFound {
			t.Errorf("cross-project delete: expected 404, got %d", code)
		}
		if _, ok := f.integrationExternalID(f.integB.ID); !ok {
			t.Fatal("cross-project delete removed integration B")
		}
	})

	t.Run("delete", func(t *testing.T) {
		if code, _ := f.do(t, http.MethodDelete, itemA, nil, ""); code != http.StatusUnauthorized {
			t.Errorf("unauthenticated: expected 401, got %d", code)
		}
		for _, who := range []string{"outsider", "member-b", "viewer-a", "member-a"} {
			if code, _ := f.do(t, http.MethodDelete, itemA, nil, who); code != http.StatusForbidden {
				t.Errorf("%s: expected 403, got %d", who, code)
			}
		}
		if _, ok := f.integrationExternalID(f.integA.ID); !ok {
			t.Fatal("denied delete removed integration A")
		}
		if code, body := f.do(t, http.MethodDelete, itemA, nil, "owner-a"); code != http.StatusOK {
			t.Fatalf("owner-a: expected 200, got %d %s", code, body)
		}
		if _, ok := f.integrationExternalID(f.integA.ID); ok {
			t.Fatal("integration A still exists after authorized delete")
		}
	})
}

// startWSServer serves the fixture's app on a loopback listener for real WebSocket handshakes.
func startWSServer(t *testing.T, f *routeAuthzFixture) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = f.server.App.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = f.server.App.ShutdownWithTimeout(2 * time.Second) })
	return ln.Addr().String()
}

// dialWS performs a handshake and returns the connection or the HTTP status of the rejected handshake.
func dialWS(t *testing.T, f *routeAuthzFixture, addr, path, who, origin string) (*websocket.Conn, int) {
	t.Helper()
	h := http.Header{}
	if who != "" {
		h.Set("Cookie", f.cookies[who].String())
	}
	if origin != "" {
		h.Set("Origin", origin)
	}
	d := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, resp, err := d.Dial("ws://"+addr+path, h)
	if err != nil {
		if errors.Is(err, websocket.ErrBadHandshake) && resp != nil {
			return nil, resp.StatusCode
		}
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, http.StatusSwitchingProtocols
}

func TestWSTerminal_AuthOriginAndTaskAuthorization(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	addr := startWSServer(t, f)
	self := "http://" + addr
	path := "/api/v1/ws/terminal/" + f.taskA.ID

	if _, code := dialWS(t, f, addr, path, "", self); code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: expected 401, got %d", code)
	}
	for _, who := range []string{"outsider", "member-b"} {
		if _, code := dialWS(t, f, addr, path, who, self); code != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d", who, code)
		}
	}
	if _, code := dialWS(t, f, addr, "/api/v1/ws/terminal/"+common.NewID(), "viewer-a", self); code != http.StatusNotFound {
		t.Errorf("unknown task: expected 404 (matches task routes), got %d", code)
	}
	for _, origin := range []string{"http://evil.example", "null", "http://" + addr + ".evil.example"} {
		if _, code := dialWS(t, f, addr, path, "viewer-a", origin); code != http.StatusForbidden {
			t.Errorf("cross-site origin %q: expected 403, got %d", origin, code)
		}
	}
	if _, code := dialWS(t, f, addr, path, "viewer-a", self); code != http.StatusSwitchingProtocols {
		t.Errorf("viewer same-origin: expected 101, got %d", code)
	}
	if _, code := dialWS(t, f, addr, path, "member-a", ""); code != http.StatusSwitchingProtocols {
		t.Errorf("member without Origin header (non-browser client): expected 101, got %d", code)
	}
}

func TestWSEvents_AuthOriginAndEventFiltering(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	addr := startWSServer(t, f)
	self := "http://" + addr
	path := "/api/v1/ws/events"

	if _, code := dialWS(t, f, addr, path, "", self); code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: expected 401, got %d", code)
	}
	if _, code := dialWS(t, f, addr, path, "viewer-a", "http://evil.example"); code != http.StatusForbidden {
		t.Errorf("cross-site origin: expected 403, got %d", code)
	}

	conn, code := dialWS(t, f, addr, path, "viewer-a", self)
	if code != http.StatusSwitchingProtocols {
		t.Fatalf("viewer-a: expected 101, got %d", code)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !f.hub.HasSubscribers("global") {
		if time.Now().After(deadline) {
			t.Fatal("events client never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := f.hub.BroadcastTaskStatus(f.taskB.ID, models.TaskStatusInProgress); err != nil {
		t.Fatalf("broadcast B: %v", err)
	}
	if err := f.hub.BroadcastTaskStatus(f.taskA.ID, models.TaskStatusInProgress); err != nil {
		t.Fatalf("broadcast A: %v", err)
	}

	var got []string
	_ = conn.SetReadDeadline(time.Now().Add(750 * time.Millisecond))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		got = append(got, string(msg))
	}
	all := strings.Join(got, "\n")
	if !strings.Contains(all, f.taskA.ID) {
		t.Errorf("expected event for visible task A, got %q", all)
	}
	if strings.Contains(all, f.taskB.ID) {
		t.Errorf("event for team B task leaked to team A viewer: %q", all)
	}
}
