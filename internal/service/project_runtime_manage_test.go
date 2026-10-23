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

type runtimeManagerSpy struct {
	stopped, restarted int
}

func (m *runtimeManagerSpy) ProjectRuntimeStatus(context.Context, models.Project) (*runner.ProjectRuntimeInfo, error) {
	return &runner.ProjectRuntimeInfo{State: runner.RuntimeStateRunning}, nil
}
func (m *runtimeManagerSpy) StopProjectRuntime(context.Context, models.Project) error {
	m.stopped++
	return nil
}
func (m *runtimeManagerSpy) RestartProjectRuntime(context.Context, models.Project) error {
	m.restarted++
	return nil
}
func (m *runtimeManagerSpy) ProjectRuntimeLogs(context.Context, models.Project, int) (string, error) {
	return "log line", nil
}

type manageFixture struct {
	svc                    service.TaskService
	spy                    *runtimeManagerSpy
	project                *models.Project
	admin, member, outside *models.User
	addTask                func(models.TaskStatus)
}

func newManageFixture(t *testing.T, withManager bool) manageFixture {
	t.Helper()
	db := setupTestDB(t)
	team := &models.Team{ID: common.NewID(), Name: "Manage Team"}
	db.Create(team)
	newUser := func(name string, role models.TeamRole) *models.User {
		u := &models.User{ID: common.NewID(), Email: name + "@manage.test", DisplayName: name, Subject: "sub-" + name, Issuer: "test"}
		db.Create(u)
		if role != 0 {
			db.Create(&models.TeamMembership{TeamID: team.ID, UserID: u.ID, Role: role})
		}
		return u
	}
	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Manage Project"}
	db.Create(project)

	spy := &runtimeManagerSpy{}
	opts := []service.TaskServiceOption{
		service.WithTaskSessionDependencies(nil, repository.NewProjectRepository(db), repository.NewTeamRepository(db)),
	}
	if withManager {
		opts = append(opts, service.WithTaskProjectRuntimeManager(spy))
	}
	return manageFixture{
		svc:     service.NewTaskService(repository.NewTaskRepository(db), repository.NewAttemptRepository(db), &mockQueueClient{}, nil, opts...),
		spy:     spy,
		project: project,
		admin:   newUser("admin", models.RoleAdmin),
		member:  newUser("member", models.RoleMember),
		outside: newUser("outside", 0),
		addTask: func(status models.TaskStatus) {
			db.Create(&models.Task{
				ID: common.NewID(), ProjectID: project.ID, Title: "t", Prompt: "p", Status: status,
				Priority: models.PriorityNormal, Skills: models.StringArray{}, MCPServers: models.StringArray{},
			})
		},
	}
}

func TestProjectRuntimeManagement_MembersReadAndOutsidersAreRefused(t *testing.T) {
	fx := newManageFixture(t, true)
	ctx := context.Background()

	if info, err := fx.svc.GetProjectRuntime(ctx, fx.project.ID, fx.member.ID); err != nil || info.State != runner.RuntimeStateRunning {
		t.Fatalf("member status: %+v, %v", info, err)
	}
	if logs, err := fx.svc.GetProjectRuntimeLogs(ctx, fx.project.ID, fx.member.ID, 10); err != nil || logs != "log line" {
		t.Fatalf("member logs: %q, %v", logs, err)
	}
	if _, err := fx.svc.GetProjectRuntime(ctx, fx.project.ID, fx.outside.ID); !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("outsider status: %v, want ErrForbidden", err)
	}
}

func TestProjectRuntimeManagement_StopAndRestartNeedAnAdmin(t *testing.T) {
	fx := newManageFixture(t, true)
	ctx := context.Background()

	if err := fx.svc.StopProjectRuntime(ctx, fx.project.ID, fx.member.ID, false); !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("member stop: %v, want ErrForbidden", err)
	}
	if err := fx.svc.RestartProjectRuntime(ctx, fx.project.ID, fx.member.ID, false); !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("member restart: %v, want ErrForbidden", err)
	}
	if fx.spy.stopped+fx.spy.restarted != 0 {
		t.Fatal("a refused caller must not reach the container")
	}
	if err := fx.svc.StopProjectRuntime(ctx, fx.project.ID, fx.admin.ID, false); err != nil || fx.spy.stopped != 1 {
		t.Fatalf("admin stop: %v (stopped %d)", err, fx.spy.stopped)
	}
	if err := fx.svc.RestartProjectRuntime(ctx, fx.project.ID, fx.admin.ID, false); err != nil || fx.spy.restarted != 1 {
		t.Fatalf("admin restart: %v (restarted %d)", err, fx.spy.restarted)
	}
}

func TestProjectRuntimeManagement_BusyProjectNeedsForce(t *testing.T) {
	fx := newManageFixture(t, true)
	ctx := context.Background()
	fx.addTask(models.TaskStatusInProgress)

	if err := fx.svc.StopProjectRuntime(ctx, fx.project.ID, fx.admin.ID, false); !errors.Is(err, service.ErrRuntimeBusy) {
		t.Fatalf("stop with a running task: %v, want ErrRuntimeBusy", err)
	}
	if err := fx.svc.RestartProjectRuntime(ctx, fx.project.ID, fx.admin.ID, false); !errors.Is(err, service.ErrRuntimeBusy) {
		t.Fatalf("restart with a running task: %v, want ErrRuntimeBusy", err)
	}
	if fx.spy.stopped+fx.spy.restarted != 0 {
		t.Fatal("a busy project must not be stopped without force")
	}
	if err := fx.svc.StopProjectRuntime(ctx, fx.project.ID, fx.admin.ID, true); err != nil || fx.spy.stopped != 1 {
		t.Fatalf("forced stop: %v (stopped %d)", err, fx.spy.stopped)
	}
}

func TestProjectRuntimeManagement_UnavailableWithoutAManager(t *testing.T) {
	fx := newManageFixture(t, false)
	if _, err := fx.svc.GetProjectRuntime(context.Background(), fx.project.ID, fx.member.ID); !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("err = %v, want ErrRuntimeExecutionUnavailable", err)
	}
}
