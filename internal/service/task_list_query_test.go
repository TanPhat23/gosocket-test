package service_test

import (
	"context"
	"testing"
	"time"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
	"gorm.io/gorm"
)

// countQueries counts the SELECT statements issued on db from now on.
func countQueries(t *testing.T, db *gorm.DB) *int {
	t.Helper()
	n := new(int)
	if err := db.Callback().Query().After("gorm:query").Register("test:count_queries_"+common.NewID(), func(*gorm.DB) { *n++ }); err != nil {
		t.Fatalf("register query counter: %v", err)
	}
	return n
}

// listTasksFixture gives one user membership in three of five teams, two projects per team and two tasks per
// project. Tasks are inserted newest first, so insertion order is the expected ListTasks order for the user.
type listTasksFixture struct {
	db      *gorm.DB
	user    *models.User
	visible []string // task IDs in the user's teams, newest first
}

func newListTasksFixture(t *testing.T) *listTasksFixture {
	t.Helper()
	db := setupTestDB(t)
	f := &listTasksFixture{db: db}

	f.user = &models.User{ID: common.NewID(), Email: "u@example.com", DisplayName: "u", Subject: "sub-" + common.NewID(), Issuer: "test"}
	if err := db.Create(f.user).Error; err != nil {
		t.Fatalf("user: %v", err)
	}

	now := time.Now().UTC()
	seq := 0
	for i := 0; i < 5; i++ {
		team := &models.Team{ID: common.NewID(), Name: "T", Slug: "t-" + common.NewID()}
		if err := db.Create(team).Error; err != nil {
			t.Fatalf("team: %v", err)
		}
		inTeam := i < 3
		if inTeam {
			if err := db.Create(&models.TeamMembership{TeamID: team.ID, UserID: f.user.ID, Role: models.RoleViewer}).Error; err != nil {
				t.Fatalf("membership: %v", err)
			}
		}
		for p := 0; p < 2; p++ {
			proj := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "P", RepoPath: "/p"}
			if err := db.Create(proj).Error; err != nil {
				t.Fatalf("project: %v", err)
			}
			for k := 0; k < 2; k++ {
				tk := &models.Task{ID: common.NewID(), ProjectID: proj.ID, Title: "t", Prompt: "p", AssigneeID: "a",
					AssigneeType: models.AssigneeAgent, Status: models.TaskStatusQueued, Priority: models.PriorityNormal,
					Skills: models.StringArray{}, MCPServers: models.StringArray{},
					CreatedAt: now.Add(-time.Duration(seq) * time.Minute)}
				seq++
				if err := db.Create(tk).Error; err != nil {
					t.Fatalf("task: %v", err)
				}
				if inTeam {
					f.visible = append(f.visible, tk.ID)
				}
			}
		}
	}
	return f
}

func (f *listTasksFixture) service(t *testing.T, projects repository.ProjectRepository, tasks repository.TaskRepository) service.TaskService {
	t.Helper()
	return service.NewTaskService(tasks, repository.NewAttemptRepository(f.db), &enqueueSpy{}, nil,
		service.WithTaskSessionDependencies(nil, projects, repository.NewTeamRepository(f.db)))
}

func idsOfTasks(ts []models.Task) []string {
	out := make([]string, 0, len(ts))
	for i := range ts {
		out = append(out, ts[i].ID)
	}
	return out
}

// interfaceOnlyProjects and interfaceOnlyTasks hide the concrete batch methods, so ListTasks takes the per-team
// and per-project fallback.
type interfaceOnlyProjects struct{ repository.ProjectRepository }
type interfaceOnlyTasks struct{ repository.TaskRepository }

// TestListTasksAllTeamsQueryCount covers ListTasks without project_id: the visible tasks, their order, and a
// SELECT count that does not grow with the number of teams or projects.
func TestListTasksAllTeamsQueryCount(t *testing.T) {
	f := newListTasksFixture(t)
	ctx := context.Background()
	svc := f.service(t, repository.NewProjectRepository(f.db), repository.NewTaskRepository(f.db))

	queries := countQueries(t, f.db)
	got, err := svc.ListTasks(ctx, "", f.user.ID)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	batchQueries := *queries
	if want := f.visible; len(got) != len(want) {
		t.Fatalf("want %d tasks, got %d", len(want), len(got))
	} else {
		for i := range got {
			if got[i].ID != want[i] {
				t.Fatalf("position %d: want %s got %s", i, want[i], got[i].ID)
			}
		}
	}
	t.Logf("ListTasks(all teams) issued %d SELECT statements for 5 teams, 10 projects, 20 tasks", batchQueries)
	if batchQueries > 3 {
		t.Errorf("ListTasks(all teams) issued %d SELECTs, want at most 3 independent of team and project count", batchQueries)
	}

	// The fallback returns the same tasks in the same order, with one read per project.
	fallback := f.service(t, interfaceOnlyProjects{repository.NewProjectRepository(f.db)}, interfaceOnlyTasks{repository.NewTaskRepository(f.db)})
	fallbackQueries := countQueries(t, f.db)
	gotFallback, err := fallback.ListTasks(ctx, "", f.user.ID)
	if err != nil {
		t.Fatalf("ListTasks fallback: %v", err)
	}
	fallbackCount := *fallbackQueries
	if a, b := idsOfTasks(got), idsOfTasks(gotFallback); len(a) != len(b) {
		t.Fatalf("fallback returned %d tasks, batch returned %d", len(b), len(a))
	} else {
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("fallback position %d: batch %s fallback %s", i, a[i], b[i])
			}
		}
	}
	t.Logf("ListTasks(all teams) fallback issued %d SELECT statements", fallbackCount)
	if fallbackCount <= batchQueries {
		t.Errorf("fallback issued %d SELECTs, expected more than the batch path's %d", fallbackCount, batchQueries)
	}

	// A user without any membership gets an empty non-nil list.
	outsider := &models.User{ID: common.NewID(), Email: "o@example.com", DisplayName: "o", Subject: "sub-" + common.NewID(), Issuer: "test"}
	if err := f.db.Create(outsider).Error; err != nil {
		t.Fatalf("outsider: %v", err)
	}
	empty, err := svc.ListTasks(ctx, "", outsider.ID)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("outsider: want empty non-nil list, got %v / %v", empty, err)
	}

	// PAT team scope still restricts the all-teams list to the scoped team.
	scoped, err := svc.ListTasks(auth.WithTeamScope(ctx, "no-such-team"), "", f.user.ID)
	if err != nil || len(scoped) != 0 {
		t.Fatalf("scoped to unrelated team: want empty, got %d / %v", len(scoped), err)
	}
}
