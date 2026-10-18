package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"

	"gorm.io/gorm"
)

// selectCounter records the SELECT statements a request issues, per table.
type selectCounter struct {
	byTable map[string]int
	total   int
}

func (c *selectCounter) reset() {
	c.byTable = map[string]int{}
	c.total = 0
}

// countSelects registers a query callback on db that counts SELECTs by table. The fixture gives every
// test its own database, so the callback name cannot collide.
func countSelects(t *testing.T, db *gorm.DB) *selectCounter {
	t.Helper()
	c := &selectCounter{}
	c.reset()
	if err := db.Callback().Query().After("gorm:query").Register("test:count_selects", func(tx *gorm.DB) {
		c.byTable[tx.Statement.Table]++
		c.total++
	}); err != nil {
		t.Fatalf("register query counter: %v", err)
	}
	return c
}

// addOwnedTeam creates a team that owner is a member of, with an explicit created_at so ordering is deterministic.
func addOwnedTeam(t *testing.T, db *gorm.DB, owner *models.User, name string, created time.Time) *models.Team {
	t.Helper()
	team := &models.Team{ID: common.NewID(), Name: name, Slug: name, CreatedAt: created, UpdatedAt: created}
	if err := db.Create(team).Error; err != nil {
		t.Fatalf("team %s: %v", name, err)
	}
	if err := db.Create(&models.TeamMembership{TeamID: team.ID, UserID: owner.ID, Role: models.RoleViewer}).Error; err != nil {
		t.Fatalf("membership %s: %v", name, err)
	}
	return team
}

func addProjectAt(t *testing.T, db *gorm.DB, team *models.Team, name string, created time.Time) *models.Project {
	t.Helper()
	p := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: name, RepoPath: "/" + name, CreatedAt: created, UpdatedAt: created}
	if err := db.Create(p).Error; err != nil {
		t.Fatalf("project %s: %v", name, err)
	}
	return p
}

func decodeIDs(t *testing.T, body string, key string) []string {
	t.Helper()
	ids := []string{}
	switch key {
	case "teams":
		var rows []models.Team
		if err := json.Unmarshal([]byte(body), &rows); err != nil {
			t.Fatalf("decode teams %q: %v", body, err)
		}
		for i := range rows {
			ids = append(ids, rows[i].ID)
		}
	case "projects":
		var rows []models.Project
		if err := json.Unmarshal([]byte(body), &rows); err != nil {
			t.Fatalf("decode projects %q: %v", body, err)
		}
		for i := range rows {
			ids = append(ids, rows[i].ID)
		}
	}
	return ids
}

// TestListTeams_ResultOrderAndMembershipFilter pins which teams owner-a sees and their newest-first order.
// Team Z is newer than X and Y but owner-a is not a member, so it must not appear.
func TestListTeams_ResultOrderAndMembershipFilter(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	owner := f.users["owner-a"]
	base := time.Now().UTC().Add(-time.Hour)

	x := addOwnedTeam(t, f.db, owner, "team-x", base.Add(10*time.Minute))
	y := addOwnedTeam(t, f.db, owner, "team-y", base.Add(20*time.Minute))
	z := &models.Team{ID: common.NewID(), Name: "team-z", Slug: "team-z", CreatedAt: base.Add(30 * time.Minute), UpdatedAt: base}
	if err := f.db.Create(z).Error; err != nil {
		t.Fatalf("team z: %v", err)
	}

	code, body := f.do(t, http.MethodGet, "/api/v1/teams", nil, "owner-a")
	if code != http.StatusOK {
		t.Fatalf("GET /teams: status %d body %s", code, body)
	}
	got := strings.Join(decodeIDs(t, body, "teams"), ",")
	want := strings.Join([]string{f.teamA.ID, y.ID, x.ID}, ",")
	if got != want {
		t.Errorf("GET /teams order = %s, want %s (team Z must be hidden)", got, want)
	}
}

// TestListProjects_ResultOrderAndMembershipFilter pins the project order without team_id: teams in the
// order GET /teams returns them, and each team's projects newest first.
func TestListProjects_ResultOrderAndMembershipFilter(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	owner := f.users["owner-a"]
	base := time.Now().UTC().Add(-time.Hour)

	x := addOwnedTeam(t, f.db, owner, "team-x", base.Add(10*time.Minute))
	y := addOwnedTeam(t, f.db, owner, "team-y", base.Add(20*time.Minute))
	x1 := addProjectAt(t, f.db, x, "x1", base.Add(11*time.Minute))
	y1 := addProjectAt(t, f.db, y, "y1", base.Add(21*time.Minute))
	y2 := addProjectAt(t, f.db, y, "y2", base.Add(25*time.Minute))
	addProjectAt(t, f.db, f.teamB, "hidden", base.Add(26*time.Minute))

	code, body := f.do(t, http.MethodGet, "/api/v1/projects", nil, "owner-a")
	if code != http.StatusOK {
		t.Fatalf("GET /projects: status %d body %s", code, body)
	}
	got := strings.Join(decodeIDs(t, body, "projects"), ",")
	want := strings.Join([]string{f.projA.ID, y2.ID, y1.ID, x1.ID}, ",")
	if got != want {
		t.Errorf("GET /projects order = %s, want %s", got, want)
	}
}

// TestListTeams_QueryCountDoesNotGrowWithTeams fails while the handler issues one membership read per team.
// The same request must cost the same number of SELECTs whether the caller belongs to 1 team or 11.
func TestListTeams_QueryCountDoesNotGrowWithTeams(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	owner := f.users["owner-a"]
	counter := countSelects(t, f.db)
	base := time.Now().UTC().Add(-time.Hour)

	counter.reset()
	if code, body := f.do(t, http.MethodGet, "/api/v1/teams", nil, "owner-a"); code != http.StatusOK {
		t.Fatalf("GET /teams (1 team): status %d body %s", code, body)
	}
	oneTeam := counter.total
	oneTeamMemberships := counter.byTable["team_memberships"]

	for i := 0; i < 10; i++ {
		addOwnedTeam(t, f.db, owner, fmt.Sprintf("bulk-%d", i), base.Add(time.Duration(i)*time.Minute))
	}

	counter.reset()
	if code, body := f.do(t, http.MethodGet, "/api/v1/teams", nil, "owner-a"); code != http.StatusOK {
		t.Fatalf("GET /teams (11 teams): status %d body %s", code, body)
	}
	t.Logf("GET /teams SELECTs: 1 team=%d, 11 teams=%d", oneTeam, counter.total)
	if counter.total != oneTeam {
		t.Errorf("GET /teams issued %d SELECTs for 11 teams, %d for 1 team; want equal (N+1)", counter.total, oneTeam)
	}
	if counter.byTable["team_memberships"] != oneTeamMemberships || counter.byTable["team_memberships"] != 1 {
		t.Errorf("team_memberships SELECTs = %d, want 1 regardless of team count", counter.byTable["team_memberships"])
	}
}

// TestListProjects_QueryCountDoesNotGrowWithTeams fails while the handler issues one project read per team.
func TestListProjects_QueryCountDoesNotGrowWithTeams(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	owner := f.users["owner-a"]
	counter := countSelects(t, f.db)
	base := time.Now().UTC().Add(-time.Hour)

	counter.reset()
	if code, body := f.do(t, http.MethodGet, "/api/v1/projects", nil, "owner-a"); code != http.StatusOK {
		t.Fatalf("GET /projects (1 team): status %d body %s", code, body)
	}
	oneTeam := counter.total

	for i := 0; i < 10; i++ {
		team := addOwnedTeam(t, f.db, owner, fmt.Sprintf("bulk-%d", i), base.Add(time.Duration(i)*time.Minute))
		addProjectAt(t, f.db, team, fmt.Sprintf("bulk-p-%d", i), base.Add(time.Duration(i)*time.Minute))
	}

	counter.reset()
	if code, body := f.do(t, http.MethodGet, "/api/v1/projects", nil, "owner-a"); code != http.StatusOK {
		t.Fatalf("GET /projects (11 teams): status %d body %s", code, body)
	}
	t.Logf("GET /projects SELECTs: 1 team=%d, 11 teams=%d", oneTeam, counter.total)
	if counter.total != oneTeam {
		t.Errorf("GET /projects issued %d SELECTs for 11 teams, %d for 1 team; want equal (N+1)", counter.total, oneTeam)
	}
	if counter.byTable["projects"] != 1 {
		t.Errorf("projects SELECTs = %d, want 1 regardless of team count", counter.byTable["projects"])
	}
}
