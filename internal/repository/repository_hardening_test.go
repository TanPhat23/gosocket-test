package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"gorm.io/gorm"
)

// setupHardeningDB extends the shared in-memory schema with the projects and
// task_attempts tables that the base helper does not create.
func setupHardeningDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupTestDB(t)

	// A single connection keeps the ":memory:" database shared across queries.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	tables := []string{
		`CREATE TABLE projects (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			team_id TEXT NOT NULL,
			name TEXT NOT NULL,
			repo_path TEXT NOT NULL,
			runtime_config TEXT NOT NULL DEFAULT '{}',
			UNIQUE (team_id, name)
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
			status TEXT NOT NULL,
			UNIQUE (task_id, attempt_number)
		);`,
	}
	for _, stmt := range tables {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to create hardening table: %v", err)
		}
	}
	return db
}

func TestProjectRepositoryUpdatePersistsChanges(t *testing.T) {
	db := setupHardeningDB(t)
	repo := repository.NewProjectRepository(db)
	ctx := context.Background()

	project := &models.Project{
		ID:            "proj-1",
		TeamID:        "team-1",
		Name:          "Original",
		RepoPath:      "/repos/original",
		RuntimeConfig: models.JSONMap{"image": "old"},
	}
	if err := repo.Create(ctx, project); err != nil {
		t.Fatalf("Create project failed: %v", err)
	}

	loaded, err := repo.GetByID(ctx, "proj-1")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	loaded.Name = "Renamed"
	loaded.RuntimeConfig = models.JSONMap{"image": "new"}
	if err := repo.Update(ctx, loaded); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	reloaded, err := repo.GetByID(ctx, "proj-1")
	if err != nil {
		t.Fatalf("re-read after Update failed: %v", err)
	}
	if reloaded.Name != "Renamed" {
		t.Errorf("Name not persisted: got %q", reloaded.Name)
	}
	if reloaded.RuntimeConfig["image"] != "new" {
		t.Errorf("RuntimeConfig not persisted: got %v", reloaded.RuntimeConfig)
	}
	if reloaded.RepoPath != "/repos/original" || reloaded.TeamID != "team-1" {
		t.Errorf("untouched fields changed: repo=%q team=%q", reloaded.RepoPath, reloaded.TeamID)
	}
}

func TestProjectRepositoryUpdateRejectsDuplicateNameInTeam(t *testing.T) {
	db := setupHardeningDB(t)
	repo := repository.NewProjectRepository(db)
	ctx := context.Background()

	for _, p := range []*models.Project{
		{ID: "proj-a", TeamID: "team-1", Name: "Alpha", RepoPath: "/a"},
		{ID: "proj-b", TeamID: "team-1", Name: "Beta", RepoPath: "/b"},
		{ID: "proj-c", TeamID: "team-2", Name: "Alpha", RepoPath: "/c"},
	} {
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create %s failed: %v", p.ID, err)
		}
	}

	beta, err := repo.GetByID(ctx, "proj-b")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	beta.Name = "Alpha"
	if err := repo.Update(ctx, beta); err == nil {
		t.Fatalf("renaming into an existing team name must fail")
	}

	reloaded, err := repo.GetByID(ctx, "proj-b")
	if err != nil {
		t.Fatalf("re-read failed: %v", err)
	}
	if reloaded.Name != "Beta" {
		t.Errorf("failed Update must not persist, got name %q", reloaded.Name)
	}

	// The same name in a different team is allowed.
	if _, err := repo.GetByID(ctx, "proj-c"); err != nil {
		t.Fatalf("cross-team project should exist: %v", err)
	}
}

func TestProjectRepositoryGetByIDNotFound(t *testing.T) {
	db := setupHardeningDB(t)
	repo := repository.NewProjectRepository(db)

	if _, err := repo.GetByID(context.Background(), "missing"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected gorm.ErrRecordNotFound, got %v", err)
	}
}

func TestProjectRepositoryListFiltersByTeam(t *testing.T) {
	db := setupHardeningDB(t)
	repo := repository.NewProjectRepository(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seed := []*models.Project{
		{ID: "proj-old", TeamID: "team-1", Name: "Old", RepoPath: "/old", CreatedAt: base},
		{ID: "proj-new", TeamID: "team-1", Name: "New", RepoPath: "/new", CreatedAt: base.Add(time.Hour)},
		{ID: "proj-other", TeamID: "team-2", Name: "Other", RepoPath: "/other", CreatedAt: base.Add(2 * time.Hour)},
	}
	for _, p := range seed {
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create %s failed: %v", p.ID, err)
		}
	}

	team1, err := repo.List(ctx, "team-1")
	if err != nil {
		t.Fatalf("List(team-1) failed: %v", err)
	}
	if len(team1) != 2 || team1[0].ID != "proj-new" || team1[1].ID != "proj-old" {
		t.Fatalf("List(team-1) = %+v, want [proj-new proj-old] (newest first)", team1)
	}

	all, err := repo.List(ctx, "")
	if err != nil {
		t.Fatalf("List(all) failed: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List(all) returned %d projects, want 3", len(all))
	}

	none, err := repo.List(ctx, "team-unknown")
	if err != nil || len(none) != 0 {
		t.Fatalf("List(unknown team) = %+v, err %v; want empty without error", none, err)
	}
}

func TestTeamRepositoryAddMembershipPersistsRole(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewTeamRepository(db)
	ctx := context.Background()

	if err := repo.AddMembership(ctx, &models.TeamMembership{
		TeamID: "team-1", UserID: "usr-viewer", Role: models.RoleViewer,
	}); err != nil {
		t.Fatalf("AddMembership viewer failed: %v", err)
	}
	if err := repo.AddMembership(ctx, &models.TeamMembership{
		TeamID: "team-1", UserID: "usr-owner", Role: models.RoleOwner,
	}); err != nil {
		t.Fatalf("AddMembership owner failed: %v", err)
	}

	viewer, err := repo.GetMembership(ctx, "team-1", "usr-viewer")
	if err != nil {
		t.Fatalf("GetMembership viewer failed: %v", err)
	}
	if viewer.Role != models.RoleViewer {
		t.Errorf("viewer role = %v, want %v", viewer.Role, models.RoleViewer)
	}

	owner, err := repo.GetMembership(ctx, "team-1", "usr-owner")
	if err != nil {
		t.Fatalf("GetMembership owner failed: %v", err)
	}
	if owner.Role != models.RoleOwner {
		t.Errorf("owner role = %v, want %v", owner.Role, models.RoleOwner)
	}
}

func TestTeamRepositoryAddMembershipRejectsDuplicate(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewTeamRepository(db)
	ctx := context.Background()

	if err := repo.AddMembership(ctx, &models.TeamMembership{
		TeamID: "team-1", UserID: "usr-1", Role: models.RoleMember,
	}); err != nil {
		t.Fatalf("first AddMembership failed: %v", err)
	}

	err := repo.AddMembership(ctx, &models.TeamMembership{
		TeamID: "team-1", UserID: "usr-1", Role: models.RoleAdmin,
	})
	if err == nil {
		t.Fatalf("duplicate membership must be rejected")
	}

	existing, err := repo.GetMembership(ctx, "team-1", "usr-1")
	if err != nil {
		t.Fatalf("GetMembership after duplicate failed: %v", err)
	}
	if existing.Role != models.RoleMember {
		t.Errorf("rejected duplicate must not overwrite role, got %v", existing.Role)
	}
}

func TestTeamRepositoryGetMembershipNotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewTeamRepository(db)

	_, err := repo.GetMembership(context.Background(), "team-1", "nobody")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected gorm.ErrRecordNotFound, got %v", err)
	}
}

func TestTeamRepositoryListOrdersNewestFirst(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewTeamRepository(db)
	ctx := context.Background()

	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	for _, team := range []models.Team{
		{ID: "team-old", Name: "Old", Slug: "old", CreatedAt: base},
		{ID: "team-new", Name: "New", Slug: "new", CreatedAt: base.Add(time.Hour)},
	} {
		if err := db.Create(&team).Error; err != nil {
			t.Fatalf("seed team failed: %v", err)
		}
	}

	teams, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(teams) != 2 || teams[0].ID != "team-new" || teams[1].ID != "team-old" {
		t.Fatalf("List = %+v, want newest first", teams)
	}
}

func TestTaskRepositoryListFiltersByProject(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewTaskRepository(db)
	ctx := context.Background()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for i, projectID := range []string{"proj-1", "proj-1", "proj-2"} {
		task := &models.Task{
			ID:           "task-" + string(rune('a'+i)),
			ProjectID:    projectID,
			Title:        "Title",
			Prompt:       "Prompt",
			AssigneeID:   "agent",
			Status:       models.TaskStatusQueued,
			AssigneeType: models.AssigneeAgent,
			Skills:       models.StringArray{},
			MCPServers:   models.StringArray{},
			CreatedAt:    base.Add(time.Duration(i) * time.Hour),
		}
		if err := repo.Create(ctx, task); err != nil {
			t.Fatalf("Create task %d failed: %v", i, err)
		}
	}

	proj1, err := repo.List(ctx, "proj-1")
	if err != nil {
		t.Fatalf("List(proj-1) failed: %v", err)
	}
	if len(proj1) != 2 || proj1[0].ID != "task-b" || proj1[1].ID != "task-a" {
		t.Fatalf("List(proj-1) = %+v, want [task-b task-a]", proj1)
	}

	all, err := repo.List(ctx, "")
	if err != nil {
		t.Fatalf("List(all) failed: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List(all) returned %d tasks, want 3", len(all))
	}
}

func TestTaskRepositoryListByParentIDReturnsChildrenOldestFirst(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewTaskRepository(db)
	ctx := context.Background()

	parent := "parent-1"
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	seed := []struct {
		id     string
		parent *string
		at     time.Time
	}{
		{"child-late", &parent, base.Add(2 * time.Hour)},
		{"child-early", &parent, base},
		{"root", nil, base.Add(time.Hour)},
	}
	for _, s := range seed {
		task := &models.Task{
			ID:           s.id,
			ProjectID:    "proj-1",
			ParentTaskID: s.parent,
			Title:        "Title",
			Prompt:       "Prompt",
			AssigneeID:   "agent",
			Status:       models.TaskStatusQueued,
			AssigneeType: models.AssigneeAgent,
			Skills:       models.StringArray{},
			MCPServers:   models.StringArray{},
			CreatedAt:    s.at,
		}
		if err := repo.Create(ctx, task); err != nil {
			t.Fatalf("Create %s failed: %v", s.id, err)
		}
	}

	children, err := repo.ListByParentID(ctx, "parent-1")
	if err != nil {
		t.Fatalf("ListByParentID failed: %v", err)
	}
	if len(children) != 2 || children[0].ID != "child-early" || children[1].ID != "child-late" {
		t.Fatalf("ListByParentID = %+v, want [child-early child-late]", children)
	}

	none, err := repo.ListByParentID(ctx, "no-such-parent")
	if err != nil || len(none) != 0 {
		t.Fatalf("ListByParentID(unknown) = %+v, err %v; want empty without error", none, err)
	}
}

func TestAttemptRepositoryCreateCountAndUpdate(t *testing.T) {
	db := setupHardeningDB(t)
	repo := repository.NewAttemptRepository(db)
	ctx := context.Background()

	count, err := repo.CountByTaskID(ctx, "task-1")
	if err != nil || count != 0 {
		t.Fatalf("CountByTaskID(empty) = %d, err %v; want 0", count, err)
	}

	for _, n := range []int{1, 2} {
		attempt := &models.TaskAttempt{
			ID:            "att-" + string(rune('0'+n)),
			TaskID:        "task-1",
			AttemptNumber: n,
			Status:        models.AttemptQueued,
		}
		if err := repo.Create(ctx, attempt); err != nil {
			t.Fatalf("Create attempt %d failed: %v", n, err)
		}
	}
	if err := repo.Create(ctx, &models.TaskAttempt{
		ID: "att-other", TaskID: "task-2", AttemptNumber: 1, Status: models.AttemptQueued,
	}); err != nil {
		t.Fatalf("Create other-task attempt failed: %v", err)
	}

	count, err = repo.CountByTaskID(ctx, "task-1")
	if err != nil || count != 2 {
		t.Fatalf("CountByTaskID(task-1) = %d, err %v; want 2", count, err)
	}

	duplicate := &models.TaskAttempt{
		ID: "att-dup", TaskID: "task-1", AttemptNumber: 1, Status: models.AttemptQueued,
	}
	if err := repo.Create(ctx, duplicate); err == nil {
		t.Fatalf("duplicate attempt number for a task must be rejected")
	}

	first := &models.TaskAttempt{}
	if err := db.Where("id = ?", "att-1").First(first).Error; err != nil {
		t.Fatalf("load attempt failed: %v", err)
	}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	code := 1
	first.Status = models.AttemptFailed
	first.FinishedAt = &now
	first.ExitCode = &code
	if err := repo.Update(ctx, first); err != nil {
		t.Fatalf("Update attempt failed: %v", err)
	}

	reloaded := &models.TaskAttempt{}
	if err := db.Where("id = ?", "att-1").First(reloaded).Error; err != nil {
		t.Fatalf("reload attempt failed: %v", err)
	}
	if reloaded.Status != models.AttemptFailed {
		t.Errorf("status not persisted: got %q", reloaded.Status)
	}
	if reloaded.ExitCode == nil || *reloaded.ExitCode != 1 {
		t.Errorf("exit code not persisted: %v", reloaded.ExitCode)
	}
	if reloaded.FinishedAt == nil || !reloaded.FinishedAt.Equal(now) {
		t.Errorf("finished_at not persisted: %v", reloaded.FinishedAt)
	}
}

func TestProjectRepositoryDeleteRemovesProject(t *testing.T) {
	db := setupHardeningDB(t)
	repo := repository.NewProjectRepository(db)
	ctx := context.Background()

	if err := repo.Create(ctx, &models.Project{
		ID: "proj-del", TeamID: "team-1", Name: "Doomed", RepoPath: "/doomed",
	}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := repo.Delete(ctx, "proj-del"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := repo.GetByID(ctx, "proj-del"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted project must not be found, got %v", err)
	}
	// Deleting an unknown project is not an error.
	if err := repo.Delete(ctx, "proj-del"); err != nil {
		t.Fatalf("second Delete returned error: %v", err)
	}
}

func TestProjectRepositoryUpdateDoesNotResurrectMissingProject(t *testing.T) {
	db := setupHardeningDB(t)
	repo := repository.NewProjectRepository(db)
	ctx := context.Background()

	ghost := &models.Project{ID: "never-created", TeamID: "team-1", Name: "Ghost", RepoPath: "/g"}
	if err := repo.Update(ctx, ghost); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("Update of unknown id: want gorm.ErrRecordNotFound, got %v", err)
	}
	if _, err := repo.GetByID(ctx, ghost.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("Update must not insert a row, GetByID err = %v", err)
	}

	// A project deleted between read and write (e.g. by the task runner's late save) stays deleted.
	p := &models.Project{ID: "proj-del", TeamID: "team-1", Name: "Doomed", RepoPath: "/d"}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, p); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("Update after delete: want ErrRecordNotFound, got %v", err)
	}
	if _, err := repo.GetByID(ctx, p.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted project was resurrected: %v", err)
	}
}

func TestProjectRepositoryUpdateWritesZeroValuesAndKeepsCreatedAt(t *testing.T) {
	db := setupHardeningDB(t)
	repo := repository.NewProjectRepository(db)
	ctx := context.Background()

	created := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	p := &models.Project{ID: "proj-z", TeamID: "team-1", Name: "Z", RepoPath: "/z", RuntimeConfig: models.JSONMap{"k": "v"}, CreatedAt: created}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	p.RuntimeConfig = nil // cleared config must be written, not skipped as a zero value
	p.RepoPath = "/z2"
	p.CreatedAt = time.Time{} // a caller holding a partially-populated struct must not clobber created_at
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := repo.GetByID(ctx, "proj-z")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.RuntimeConfig) != 0 {
		t.Errorf("empty RuntimeConfig was not written: %v", got.RuntimeConfig)
	}
	if got.RepoPath != "/z2" {
		t.Errorf("RepoPath = %q", got.RepoPath)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt changed: %v", got.CreatedAt)
	}
}

func TestAttemptRepositoryLatestFailureReason(t *testing.T) {
	db := setupHardeningDB(t)
	repo := repository.NewAttemptRepository(db)
	ctx := context.Background()

	if got, err := repo.LatestFailureReason(ctx, "task-1"); err != nil || got != nil {
		t.Fatalf("LatestFailureReason(no attempts) = %v, err %v; want nil", got, err)
	}
	old, newer := "first reason", "second reason"
	for _, a := range []*models.TaskAttempt{
		{ID: "att-a", TaskID: "task-1", AttemptNumber: 1, Status: models.AttemptFailed, FailureReason: &old},
		{ID: "att-b", TaskID: "task-1", AttemptNumber: 2, Status: models.AttemptFailed, FailureReason: &newer},
		{ID: "att-c", TaskID: "task-2", AttemptNumber: 1, Status: models.AttemptQueued},
	} {
		if err := repo.Create(ctx, a); err != nil {
			t.Fatalf("Create %s: %v", a.ID, err)
		}
	}
	got, err := repo.LatestFailureReason(ctx, "task-1")
	if err != nil || got == nil || *got != newer {
		t.Fatalf("LatestFailureReason(task-1) = %v, err %v; want %q", got, err, newer)
	}
	if got, err := repo.LatestFailureReason(ctx, "task-2"); err != nil || got != nil {
		t.Fatalf("LatestFailureReason(no reason) = %v, err %v; want nil", got, err)
	}
}
