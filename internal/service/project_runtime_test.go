package service_test

import (
	"context"
	"errors"
	"testing"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/service"
)

type runtimeFixture struct {
	svc      service.TaskService
	project  *models.Project
	member   *models.User
	viewer   *models.User
	outsider *models.User
}

func newRuntimeFixture(t *testing.T, warm service.ProjectRuntimeWarmer) runtimeFixture {
	t.Helper()
	db := setupTestDB(t)
	team := &models.Team{ID: common.NewID(), Name: "Runtime Team"}
	db.Create(team)
	newUser := func(name string, role models.TeamRole) *models.User {
		u := &models.User{ID: common.NewID(), Email: name + "@runtime.test", DisplayName: name, Subject: "sub-" + name, Issuer: "test"}
		db.Create(u)
		if role != 0 {
			db.Create(&models.TeamMembership{TeamID: team.ID, UserID: u.ID, Role: role})
		}
		return u
	}
	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Runtime Project"}
	db.Create(project)

	opts := []service.TaskServiceOption{
		service.WithTaskSessionDependencies(nil, repository.NewProjectRepository(db), repository.NewTeamRepository(db)),
	}
	if warm != nil {
		opts = append(opts, service.WithTaskProjectRuntimeWarmer(warm))
	}
	svc := service.NewTaskService(repository.NewTaskRepository(db), repository.NewAttemptRepository(db), &mockQueueClient{}, nil, opts...)
	return runtimeFixture{
		svc:      svc,
		project:  project,
		member:   newUser("member", models.RoleMember),
		viewer:   newUser("viewer", models.RoleViewer),
		outsider: newUser("outsider", 0),
	}
}

func TestTaskService_StartProjectRuntime_MemberWarmsTheProject(t *testing.T) {
	var warmed []models.Project
	fx := newRuntimeFixture(t, func(p models.Project) error {
		warmed = append(warmed, p)
		return nil
	})

	if err := fx.svc.StartProjectRuntime(context.Background(), fx.project.ID, fx.member.ID); err != nil {
		t.Fatalf("StartProjectRuntime for a member: %v", err)
	}
	if len(warmed) != 1 || warmed[0].ID != fx.project.ID {
		t.Fatalf("warmer calls = %+v, want one call for project %s", warmed, fx.project.ID)
	}
}

func TestTaskService_StartProjectRuntime_RefusesViewersAndOutsiders(t *testing.T) {
	var calls int
	fx := newRuntimeFixture(t, func(models.Project) error {
		calls++
		return nil
	})

	for name, userID := range map[string]string{"viewer": fx.viewer.ID, "outsider": fx.outsider.ID, "empty": ""} {
		err := fx.svc.StartProjectRuntime(context.Background(), fx.project.ID, userID)
		if !errors.Is(err, common.ErrForbidden) {
			t.Fatalf("%s: err = %v, want ErrForbidden", name, err)
		}
	}
	if calls != 0 {
		t.Fatalf("warmer ran %d times for refused callers, want 0", calls)
	}
}

func TestTaskService_StartProjectRuntime_UnavailableWithoutWarmer(t *testing.T) {
	fx := newRuntimeFixture(t, nil)

	err := fx.svc.StartProjectRuntime(context.Background(), fx.project.ID, fx.member.ID)
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("err = %v, want ErrRuntimeExecutionUnavailable", err)
	}
}
