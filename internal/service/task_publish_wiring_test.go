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

type prWiringFixture struct {
	svc      service.TaskService
	task     *models.Task
	member   *models.User
	viewer   *models.User
	outsider *models.User
}

func newPRWiringFixture(t *testing.T, status models.TaskStatus, publish service.PullRequestPublisher, diff service.TaskDiffReader) prWiringFixture {
	t.Helper()
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	attemptRepo := repository.NewAttemptRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	teamRepo := repository.NewTeamRepository(db)

	team := &models.Team{ID: common.NewID(), Name: "PR Wiring Team"}
	db.Create(team)
	newUser := func(name string, role models.TeamRole) *models.User {
		u := &models.User{ID: common.NewID(), Email: name + "@pr-wiring.test", DisplayName: name, Subject: "sub-" + name, Issuer: "test"}
		db.Create(u)
		if role != 0 {
			db.Create(&models.TeamMembership{TeamID: team.ID, UserID: u.ID, Role: role})
		}
		return u
	}
	member := newUser("member", models.RoleMember)
	viewer := newUser("viewer", models.RoleViewer)
	outsider := newUser("outsider", 0)

	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "PR Wiring Project"}
	db.Create(project)
	sessionID := "sess-pr-wiring"
	task := &models.Task{
		ID:        common.NewID(),
		ProjectID: project.ID,
		Title:     "PR Wiring Task",
		Prompt:    "Change file",
		Status:    status,
		SessionID: &sessionID,
	}
	db.Create(task)

	svc := service.NewTaskService(
		taskRepo,
		attemptRepo,
		&mockQueueClient{},
		nil,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo),
		service.WithTaskPullRequestPublisher(publish, diff),
	)
	return prWiringFixture{svc: svc, task: task, member: member, viewer: viewer, outsider: outsider}
}

func TestTaskService_CreatePullRequestForTask_PublishesThroughRunner(t *testing.T) {
	ctx := context.Background()
	const prURL = "https://github.com/owner/repo/pull/7"
	var calls int
	var gotTaskID string
	publish := func(_ context.Context, task *models.Task, _ func(string)) (string, error) {
		calls++
		gotTaskID = task.ID
		return prURL, nil
	}
	f := newPRWiringFixture(t, models.TaskStatusReview, publish, nil)

	got, err := f.svc.CreatePullRequestForTask(ctx, f.task.ID, f.member.ID)
	if err != nil {
		t.Fatalf("member CreatePullRequestForTask: %v", err)
	}
	if got.PRURL == nil || *got.PRURL != prURL {
		t.Fatalf("expected PRURL %q, got %v", prURL, got.PRURL)
	}
	if calls != 1 || gotTaskID != f.task.ID {
		t.Fatalf("expected one publish for task %s, got %d calls for %q", f.task.ID, calls, gotTaskID)
	}

	if _, err := f.svc.CreatePullRequestForTask(ctx, f.task.ID, f.viewer.ID); !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("viewer: expected ErrForbidden, got %v", err)
	}
	if _, err := f.svc.CreatePullRequestForTask(ctx, f.task.ID, f.outsider.ID); !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("outsider: expected ErrForbidden, got %v", err)
	}
	if _, err := f.svc.CreatePullRequestForTask(ctx, f.task.ID, ""); !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("empty user: expected ErrForbidden, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("unauthorized callers reached the publisher: %d calls", calls)
	}
}

func TestTaskService_CreatePullRequestForTask_RefusesUnfinishedTask(t *testing.T) {
	ctx := context.Background()
	var calls int
	publish := func(context.Context, *models.Task, func(string)) (string, error) {
		calls++
		return "", nil
	}
	f := newPRWiringFixture(t, models.TaskStatusInProgress, publish, nil)

	if _, err := f.svc.CreatePullRequestForTask(ctx, f.task.ID, f.member.ID); err == nil {
		t.Fatal("expected an error for an in-progress task")
	}
	if calls != 0 {
		t.Fatalf("an unfinished task must not be published: %d calls", calls)
	}
}

func TestTaskService_CreatePullRequestForTask_PropagatesPublishError(t *testing.T) {
	ctx := context.Background()
	pushErr := errors.New("push rejected")
	publish := func(context.Context, *models.Task, func(string)) (string, error) {
		return "", pushErr
	}
	f := newPRWiringFixture(t, models.TaskStatusDone, publish, nil)

	got, err := f.svc.CreatePullRequestForTask(ctx, f.task.ID, f.member.ID)
	if !errors.Is(err, pushErr) {
		t.Fatalf("expected the publish error, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected no task on failure, got %+v", got)
	}
}

func TestTaskService_GetTaskDiff_ReadsThroughRunner(t *testing.T) {
	ctx := context.Background()
	const diffText = "diff --git a/main.go b/main.go\n"
	var calls int
	read := func(_ context.Context, _ *models.Task) (string, error) {
		calls++
		return diffText, nil
	}
	f := newPRWiringFixture(t, models.TaskStatusReview, nil, read)

	diff, err := f.svc.GetTaskDiff(ctx, f.task.ID, f.viewer.ID)
	if err != nil {
		t.Fatalf("viewer GetTaskDiff: %v", err)
	}
	if diff != diffText {
		t.Fatalf("expected diff %q, got %q", diffText, diff)
	}

	if _, err := f.svc.GetTaskDiff(ctx, f.task.ID, f.outsider.ID); !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("outsider: expected ErrForbidden, got %v", err)
	}
	if _, err := f.svc.GetTaskDiff(ctx, f.task.ID, ""); !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("empty user: expected ErrForbidden, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("unauthorized callers reached the diff reader: %d calls", calls)
	}
}

func TestTaskService_PullRequestPaths_UnwiredStillRefuse(t *testing.T) {
	ctx := context.Background()
	f := newPRWiringFixture(t, models.TaskStatusReview, nil, nil)

	if _, err := f.svc.CreatePullRequestForTask(ctx, f.task.ID, f.member.ID); !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("unwired CreatePullRequestForTask: expected runtime-unavailable, got %v", err)
	}
	if _, err := f.svc.GetTaskDiff(ctx, f.task.ID, f.viewer.ID); !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("unwired GetTaskDiff: expected runtime-unavailable, got %v", err)
	}
	if _, err := f.svc.SyncTaskPRStatus(ctx, f.task.ID, f.member.ID); !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("unwired SyncTaskPRStatus: expected runtime-unavailable, got %v", err)
	}
}
