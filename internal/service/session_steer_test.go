package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/service"
)

func newSteerService(f *tunnelFixture, fin service.SteerFinalizer) service.TaskService {
	resolve := func(_ context.Context, projectID string) (runner.OpenCodeClient, error) {
		return f.clients[projectID], nil
	}
	return service.NewTaskService(f.spy, repository.NewAttemptRepository(f.db), f.queue, nil,
		service.WithTaskSessionDependencies(nil, repository.NewProjectRepository(f.db), repository.NewTeamRepository(f.db)),
		service.WithTaskSessionClientResolver(resolve),
		service.WithTaskSteerFinalizer(fin))
}

func taskStatus(t *testing.T, f *tunnelFixture, id string) models.TaskStatus {
	t.Helper()
	tk, err := f.spy.GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return tk.Status
}

func waitStatus(t *testing.T, f *tunnelFixture, id string, want models.TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if taskStatus(t, f, id) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("task never reached %s (now %s)", want, taskStatus(t, f, id))
}

func TestSteer_RunsFinalizerThenReturnsTaskToReview(t *testing.T) {
	f := newTunnelFixture(t, true)
	release := make(chan struct{})
	var mu sync.Mutex
	var seen []string
	svc := newSteerService(f, func(_ context.Context, task *models.Task, _ *time.Time, logf func(string)) error {
		mu.Lock()
		seen = append(seen, task.ID)
		mu.Unlock()
		<-release
		return nil
	})
	if err := svc.PromptTaskSession(context.Background(), f.taskA.ID, f.memberA.ID, &dto.TaskSessionPromptRequest{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, f, f.taskA.ID, models.TaskStatusInProgress)
	close(release)
	waitStatus(t, f, f.taskA.ID, models.TaskStatusReview)
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != f.taskA.ID {
		t.Fatalf("finalizer calls: %v", seen)
	}
}

func TestSteer_SecondSteerWhileInFlightConflicts(t *testing.T) {
	f := newTunnelFixture(t, true)
	release := make(chan struct{})
	svc := newSteerService(f, func(context.Context, *models.Task, *time.Time, func(string)) error {
		<-release
		return nil
	})
	defer close(release)
	req := &dto.TaskSessionPromptRequest{Prompt: "go"}
	if err := svc.PromptTaskSession(context.Background(), f.taskA.ID, f.memberA.ID, req); err != nil {
		t.Fatal(err)
	}
	err := svc.PromptTaskSession(context.Background(), f.taskA.ID, f.memberA.ID, req)
	if err == nil {
		t.Fatal("expected a conflict while a turn is in flight")
	}
	if n := len(f.clients[f.projA.ID].prompts); n != 1 {
		t.Fatalf("the rejected steer must not reach the sandbox, prompts=%d", n)
	}
}

func TestSteer_FinalizerErrorStillReturnsToReview(t *testing.T) {
	f := newTunnelFixture(t, true)
	svc := newSteerService(f, func(context.Context, *models.Task, *time.Time, func(string)) error {
		return errors.New("push failed")
	})
	if err := svc.PromptTaskSession(context.Background(), f.taskA.ID, f.memberA.ID, &dto.TaskSessionPromptRequest{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, f, f.taskA.ID, models.TaskStatusReview)
}

func TestSteer_CancelledTaskIsRejected(t *testing.T) {
	f := newTunnelFixture(t, true)
	if err := f.db.Model(&models.Task{}).Where("id = ?", f.taskA.ID).Update("status", models.TaskStatusCancelled).Error; err != nil {
		t.Fatal(err)
	}
	called := false
	svc := newSteerService(f, func(context.Context, *models.Task, *time.Time, func(string)) error { called = true; return nil })
	if err := svc.PromptTaskSession(context.Background(), f.taskA.ID, f.memberA.ID, &dto.TaskSessionPromptRequest{Prompt: "go"}); err == nil {
		t.Fatal("expected rejection")
	}
	if called || len(f.clients[f.projA.ID].prompts) != 0 {
		t.Fatal("a cancelled task must not be steered")
	}
}

func TestSteer_PromptFailureReleasesTheSlot(t *testing.T) {
	f := newTunnelFixture(t, true)
	svc := newSteerService(f, func(context.Context, *models.Task, *time.Time, func(string)) error { return nil })
	f.resolveE = nil
	f.clients[f.projA.ID] = &tunnelClient{}
	// first steer fails at the sandbox: make the client error by swapping in a failing one
	f.clients[f.projA.ID] = &tunnelClient{failPrompt: true}
	if err := svc.PromptTaskSession(context.Background(), f.taskA.ID, f.memberA.ID, &dto.TaskSessionPromptRequest{Prompt: "go"}); err == nil {
		t.Fatal("expected prompt error")
	}
	f.clients[f.projA.ID] = &tunnelClient{}
	if err := svc.PromptTaskSession(context.Background(), f.taskA.ID, f.memberA.ID, &dto.TaskSessionPromptRequest{Prompt: "again"}); err != nil {
		t.Fatalf("slot should be free after a failed prompt: %v", err)
	}
}
