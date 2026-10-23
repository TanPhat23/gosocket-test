package repository_test

import (
	"context"
	"testing"
	"time"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
)

// The batch reads are concrete-type methods (not part of TaskRepository and ProjectRepository), so the tests
// reach them through these interfaces.
type taskBatchReader interface {
	ListByProjectIDs(ctx context.Context, projectIDs []string) ([]models.Task, error)
}

type projectBatchReader interface {
	ListByTeamIDs(ctx context.Context, teamIDs []string) ([]models.Project, error)
}

func TestTaskRepositoryListByProjectIDs(t *testing.T) {
	db := setupHardeningDB(t)
	repo := repository.NewTaskRepository(db)
	ctx := context.Background()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	seed := []struct{ id, project string }{
		{"task-a", "proj-1"}, {"task-b", "proj-2"}, {"task-c", "proj-1"}, {"task-d", "proj-3"},
	}
	for i, s := range seed {
		task := &models.Task{
			ID: s.id, ProjectID: s.project, Title: "Title", Prompt: "Prompt", AssigneeID: "agent",
			Status: models.TaskStatusQueued, AssigneeType: models.AssigneeAgent,
			Skills: models.StringArray{}, MCPServers: models.StringArray{},
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
		}
		if err := repo.Create(ctx, task); err != nil {
			t.Fatalf("Create %s failed: %v", s.id, err)
		}
	}

	batch := repo.(taskBatchReader)
	got, err := batch.ListByProjectIDs(ctx, []string{"proj-1", "proj-2"})
	if err != nil {
		t.Fatalf("ListByProjectIDs failed: %v", err)
	}
	// Newest first across both projects: task-c (2h), task-b (1h), task-a (0h). proj-3 is excluded.
	if len(got) != 3 || got[0].ID != "task-c" || got[1].ID != "task-b" || got[2].ID != "task-a" {
		t.Fatalf("ListByProjectIDs = %+v, want [task-c task-b task-a]", got)
	}

	for name, ids := range map[string][]string{"empty": {}, "nil": nil, "unknown": {"proj-missing"}} {
		res, err := batch.ListByProjectIDs(ctx, ids)
		if err != nil || res == nil || len(res) != 0 {
			t.Fatalf("ListByProjectIDs(%s) = %+v, err %v; want empty non-nil without error", name, res, err)
		}
	}
}

func TestProjectRepositoryListByTeamIDs(t *testing.T) {
	db := setupHardeningDB(t)
	repo := repository.NewProjectRepository(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seed := []*models.Project{
		{ID: "proj-old", TeamID: "team-1", Name: "Old", RepoPath: "/old", CreatedAt: base},
		{ID: "proj-new", TeamID: "team-2", Name: "New", RepoPath: "/new", CreatedAt: base.Add(time.Hour)},
		{ID: "proj-other", TeamID: "team-3", Name: "Other", RepoPath: "/other", CreatedAt: base.Add(2 * time.Hour)},
	}
	for _, p := range seed {
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create %s failed: %v", p.ID, err)
		}
	}

	batch := repo.(projectBatchReader)
	got, err := batch.ListByTeamIDs(ctx, []string{"team-1", "team-2"})
	if err != nil {
		t.Fatalf("ListByTeamIDs failed: %v", err)
	}
	if len(got) != 2 || got[0].ID != "proj-new" || got[1].ID != "proj-old" {
		t.Fatalf("ListByTeamIDs = %+v, want [proj-new proj-old]", got)
	}

	for name, ids := range map[string][]string{"empty": {}, "nil": nil, "unknown": {"team-unknown"}} {
		res, err := batch.ListByTeamIDs(ctx, ids)
		if err != nil || res == nil || len(res) != 0 {
			t.Fatalf("ListByTeamIDs(%s) = %+v, err %v; want empty non-nil without error", name, res, err)
		}
	}
}

func TestTeamRepositoryListMembershipsByUserID(t *testing.T) {
	db := setupHardeningDB(t)
	repo := repository.NewTeamRepository(db)
	ctx := context.Background()

	seed := []models.TeamMembership{
		{TeamID: "team-1", UserID: "usr-1", Role: models.RoleViewer},
		{TeamID: "team-2", UserID: "usr-1", Role: models.RoleOwner},
		{TeamID: "team-1", UserID: "usr-2", Role: models.RoleMember},
	}
	for i := range seed {
		if err := repo.AddMembership(ctx, &seed[i]); err != nil {
			t.Fatalf("AddMembership %d failed: %v", i, err)
		}
	}

	got, err := repo.ListMembershipsByUserID(ctx, "usr-1")
	if err != nil {
		t.Fatalf("ListMembershipsByUserID failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListMembershipsByUserID(usr-1) returned %d rows, want 2: %+v", len(got), got)
	}
	roles := map[string]models.TeamRole{}
	for _, m := range got {
		if m.UserID != "usr-1" {
			t.Fatalf("membership of another user returned: %+v", m)
		}
		roles[m.TeamID] = m.Role
	}
	if roles["team-1"] != models.RoleViewer || roles["team-2"] != models.RoleOwner {
		t.Fatalf("roles = %v, want team-1 viewer and team-2 owner", roles)
	}

	none, err := repo.ListMembershipsByUserID(ctx, "usr-nobody")
	if err != nil || len(none) != 0 {
		t.Fatalf("ListMembershipsByUserID(unknown) = %+v, err %v; want empty without error", none, err)
	}
}
