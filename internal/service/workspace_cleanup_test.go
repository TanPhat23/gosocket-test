package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
)

type cleanerRecorder struct {
	calls []string
	err   error
}

func (c *cleanerRecorder) clean(_ context.Context, task *models.Task) error {
	c.calls = append(c.calls, task.ID)
	return c.err
}

func (c *cleanerRecorder) cleaned(id string) bool {
	for _, got := range c.calls {
		if got == id {
			return true
		}
	}
	return false
}

func TestCleanupFinishedWorkspaces_CleansOnlyIdleFinishedTasks(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	team := &models.Team{ID: common.NewID(), Name: "Cleanup Team"}
	db.Create(team)
	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Cleanup Project"}
	db.Create(project)

	old := time.Now().UTC().Add(-time.Hour)
	mk := func(status models.TaskStatus, at time.Time) *models.Task {
		task := &models.Task{
			ID: common.NewID(), ProjectID: project.ID, Title: "Cleanup", Prompt: "p",
			Status: status, Priority: models.PriorityNormal,
			Skills: models.StringArray{}, MCPServers: models.StringArray{},
		}
		db.Create(task)
		db.Model(task).Update("updated_at", at)
		return task
	}
	idleDone := mk(models.TaskStatusDone, old)
	idleFailed := mk(models.TaskStatusFailed, old)
	idleCancelled := mk(models.TaskStatusCancelled, old)
	inReview := mk(models.TaskStatusReview, old)
	justFinished := mk(models.TaskStatusDone, time.Now().UTC())
	running := mk(models.TaskStatusInProgress, old)

	rec := &cleanerRecorder{}
	svc := service.NewTaskService(taskRepo, repository.NewAttemptRepository(db), &mockQueueClient{}, nil,
		service.WithTaskWorkspaceCleaner(rec.clean))

	if err := svc.CleanupFinishedWorkspaces(ctx); err != nil {
		t.Fatalf("CleanupFinishedWorkspaces: %v", err)
	}
	for _, want := range []*models.Task{idleDone, idleFailed, idleCancelled} {
		if !rec.cleaned(want.ID) {
			t.Errorf("idle %s task %s was not cleaned", want.Status, want.ID)
		}
	}
	for _, skip := range []*models.Task{inReview, justFinished, running} {
		if rec.cleaned(skip.ID) {
			t.Errorf("%s task %s must keep its worktree", skip.Status, skip.ID)
		}
	}

	before := len(rec.calls)
	if err := svc.CleanupFinishedWorkspaces(ctx); err != nil {
		t.Fatalf("second CleanupFinishedWorkspaces: %v", err)
	}
	if len(rec.calls) != before {
		t.Errorf("a finished task must be cleaned once, got %d more calls", len(rec.calls)-before)
	}
}

func TestCleanupFinishedWorkspaces_CleansAgainAfterARetry(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	team := &models.Team{ID: common.NewID(), Name: "Retry Team"}
	db.Create(team)
	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Retry Project"}
	db.Create(project)
	task := &models.Task{
		ID: common.NewID(), ProjectID: project.ID, Title: "Retry", Prompt: "p",
		Status: models.TaskStatusFailed, Priority: models.PriorityNormal,
		Skills: models.StringArray{}, MCPServers: models.StringArray{},
	}
	db.Create(task)
	db.Model(task).Update("updated_at", time.Now().UTC().Add(-time.Hour))

	rec := &cleanerRecorder{}
	svc := service.NewTaskService(taskRepo, repository.NewAttemptRepository(db), &mockQueueClient{}, nil,
		service.WithTaskWorkspaceCleaner(rec.clean))
	if err := svc.CleanupFinishedWorkspaces(ctx); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("expected the failed task to be cleaned once, got %d", len(rec.calls))
	}

	// A retry runs again and fails later: its new finish time makes the state new, so it is cleaned again.
	db.Model(task).Update("updated_at", time.Now().UTC().Add(-30*time.Minute))
	if err := svc.CleanupFinishedWorkspaces(ctx); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("expected the retried finish to be cleaned again, got %d calls", len(rec.calls))
	}
}

func TestCleanupFinishedWorkspaces_FailedRemovalIsRetried(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	team := &models.Team{ID: common.NewID(), Name: "Retry Team"}
	db.Create(team)
	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Retry Project"}
	db.Create(project)
	task := &models.Task{
		ID: common.NewID(), ProjectID: project.ID, Title: "Retry", Prompt: "p",
		Status: models.TaskStatusDone, Priority: models.PriorityNormal,
		Skills: models.StringArray{}, MCPServers: models.StringArray{},
	}
	db.Create(task)
	db.Model(task).Update("updated_at", time.Now().UTC().Add(-time.Hour))

	rec := &cleanerRecorder{err: errors.New("docker unavailable")}
	svc := service.NewTaskService(taskRepo, repository.NewAttemptRepository(db), &mockQueueClient{}, nil,
		service.WithTaskWorkspaceCleaner(rec.clean))
	if err := svc.CleanupFinishedWorkspaces(ctx); err != nil {
		t.Fatalf("a failed removal must not fail the sweep: %v", err)
	}

	rec.err = nil
	if err := svc.CleanupFinishedWorkspaces(ctx); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("expected the failed removal to be retried, got %d calls", len(rec.calls))
	}
}

func TestCleanupFinishedWorkspaces_UnwiredRemovesNothing(t *testing.T) {
	db := setupTestDB(t)
	svc := service.NewTaskService(repository.NewTaskRepository(db), repository.NewAttemptRepository(db), &mockQueueClient{}, nil)
	if err := svc.CleanupFinishedWorkspaces(context.Background()); err != nil {
		t.Fatalf("unwired sweep: %v", err)
	}
}
