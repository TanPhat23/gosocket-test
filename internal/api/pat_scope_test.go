package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
)

// setupPATScopeFixture makes owner-a an owner of team B as well, so only the PAT's team scope (team A)
// separates the two teams. It returns a team-A-scoped PAT for owner-a.
func setupPATScopeFixture(t *testing.T) (*routeAuthzFixture, string) {
	t.Helper()
	f := setupRouteAuthzFixture(t)
	if err := f.db.Create(&models.TeamMembership{TeamID: f.teamB.ID, UserID: f.users["owner-a"].ID, Role: models.RoleOwner}).Error; err != nil {
		t.Fatalf("membership: %v", err)
	}
	return f, f.pat(t, "owner-a", f.teamA)
}

func TestPATScope_ListsOnlyTheTokenTeam(t *testing.T) {
	f, token := setupPATScopeFixture(t)

	teamIDs := func(body string) []string {
		var teams []models.Team
		if err := json.Unmarshal([]byte(body), &teams); err != nil {
			t.Fatalf("decode teams %q: %v", body, err)
		}
		ids := []string{}
		for _, tm := range teams {
			ids = append(ids, tm.ID)
		}
		return ids
	}

	// Control: the same user's session sees both teams.
	if code, body := f.do(t, http.MethodGet, "/api/v1/teams", nil, "owner-a"); code != http.StatusOK || len(teamIDs(body)) != 2 {
		t.Fatalf("cookie: expected both teams, got %d %s", code, body)
	}
	if code, body := f.doPAT(t, http.MethodGet, "/api/v1/teams", nil, token); code != http.StatusOK ||
		strings.Join(teamIDs(body), ",") != f.teamA.ID {
		t.Errorf("PAT /teams: expected only team A, got %d %s", code, body)
	}

	if code, body := f.doPAT(t, http.MethodGet, "/api/v1/projects", nil, token); code != http.StatusOK ||
		!strings.Contains(body, f.projA.ID) || strings.Contains(body, f.projB.ID) {
		t.Errorf("PAT /projects: expected only project A, got %d %s", code, body)
	}
	if code, body := f.doPAT(t, http.MethodGet, "/api/v1/tasks", nil, token); code != http.StatusOK ||
		!strings.Contains(body, f.taskA.ID) || strings.Contains(body, f.taskB.ID) {
		t.Errorf("PAT /tasks: expected only task A, got %d %s", code, body)
	}
}

func TestPATScope_DeniesOtherTeams(t *testing.T) {
	f, token := setupPATScopeFixture(t)
	projB, teamB := f.projB.ID, f.teamB.ID

	denied := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/projects?team_id=" + teamB, nil},
		{http.MethodGet, "/api/v1/tasks?project_id=" + projB, nil},
		{http.MethodGet, "/api/v1/tasks/" + f.taskB.ID, nil},
		{http.MethodGet, "/api/v1/tasks?parent_task_id=" + f.taskB.ID, nil},
		{http.MethodPost, "/api/v1/tasks", dto.CreateTaskRequest{ProjectID: projB, Title: "x", Prompt: "p", AssigneeID: "a", AssigneeType: models.AssigneeAgent, Priority: int(models.PriorityNormal)}},
		{http.MethodPost, "/api/v1/tasks/" + f.taskB.ID + "/cancel", nil},
		{http.MethodGet, "/api/v1/tasks/" + f.taskB.ID + "/diff", nil},
		{http.MethodPost, "/api/v1/projects", map[string]string{"name": "pat-b", "team_id": teamB}},
		{http.MethodDelete, "/api/v1/projects/" + projB, nil},
		{http.MethodGet, "/api/v1/projects/" + projB + "/integrations", nil},
		{http.MethodGet, "/api/v1/projects/" + projB + "/integrations/" + f.integB.ID, nil},
		{http.MethodGet, "/api/v1/teams/" + teamB + "/credentials", nil},
		{http.MethodGet, "/api/v1/teams/" + teamB + "/credentials/x/github/repos", nil},
	}
	for _, r := range denied {
		code, body := f.doPAT(t, r.method, r.path, r.body, token)
		if code != http.StatusForbidden || strings.Contains(body, "SECRET-REPO-B") {
			t.Errorf("PAT %s %s: expected 403 without data, got %d %s", r.method, r.path, code, body)
		}
	}
	if !f.projectExists(projB) {
		t.Fatal("PAT scoped to team A deleted a team B project")
	}

	// The token's own team stays reachable. (Credential routes are not listed: the fixture wires no
	// CredentialService, so an authorized request would reach a nil service.)
	allowed := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/projects?team_id=" + f.teamA.ID},
		{http.MethodGet, "/api/v1/tasks?project_id=" + f.projA.ID},
		{http.MethodGet, "/api/v1/tasks/" + f.taskA.ID},
		{http.MethodGet, "/api/v1/projects/" + f.projA.ID + "/integrations"},
	}
	for _, r := range allowed {
		if code, body := f.doPAT(t, r.method, r.path, nil, token); code != http.StatusOK {
			t.Errorf("PAT %s %s: expected 200, got %d %s", r.method, r.path, code, body)
		}
	}
}

// A token without a team scope reaches no team (fail closed).
func TestPATScope_EmptyTeamIDReachesNothing(t *testing.T) {
	f, _ := setupPATScopeFixture(t)
	token := f.pat(t, "owner-a", &models.Team{})
	if code, body := f.doPAT(t, http.MethodGet, "/api/v1/teams", nil, token); code != http.StatusOK || strings.TrimSpace(body) != "[]" {
		t.Errorf("expected 200 [], got %d %s", code, body)
	}
	if code, body := f.doPAT(t, http.MethodGet, "/api/v1/tasks/"+f.taskA.ID, nil, token); code != http.StatusForbidden {
		t.Errorf("expected 403, got %d %s", code, body)
	}
}

func TestPATScope_WebSockets(t *testing.T) {
	f, token := setupPATScopeFixture(t)
	addr := startWSServer(t, f)
	bearer := http.Header{"Authorization": {"Bearer " + token}}

	if _, code := dialWSHeader(t, addr, "/api/v1/ws/terminal/"+f.taskB.ID, bearer); code != http.StatusForbidden {
		t.Errorf("PAT terminal for team B task: expected 403, got %d", code)
	}

	conn, code := dialWSHeader(t, addr, "/api/v1/ws/events", bearer)
	if code != http.StatusSwitchingProtocols {
		t.Fatalf("PAT events: expected 101, got %d", code)
	}
	waitGlobalSubscriber(t, f)
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
		t.Errorf("expected event for team A task, got %q", all)
	}
	if strings.Contains(all, f.taskB.ID) {
		t.Errorf("event for team B task leaked to team-A-scoped PAT: %q", all)
	}
}
