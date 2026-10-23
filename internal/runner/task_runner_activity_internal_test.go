package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"agent-task-manager/internal/config"
	"agent-task-manager/internal/git"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/sandbox"
	"agent-task-manager/internal/ws"
)

type internalActivitySpyProjectSandboxManager struct {
	mu         sync.Mutex
	acquires   []string
	releases   []string
	releaseMap map[string]int
}

func newInternalActivitySpyProjectSandboxManager() *internalActivitySpyProjectSandboxManager {
	return &internalActivitySpyProjectSandboxManager{
		releaseMap: make(map[string]int),
	}
}

func (m *internalActivitySpyProjectSandboxManager) EnsureProjectContainer(ctx context.Context, project *models.Project, imageTag string) (*ProjectRouteHandle, error) {
	return &ProjectRouteHandle{
		ProjectID:    project.ID,
		ContainerID:  "c-" + project.ID,
		EndpointURL:  "http://127.0.0.1:4096",
		AuthPassword: "mock-password",
	}, nil
}

func (m *internalActivitySpyProjectSandboxManager) StopProjectContainer(ctx context.Context, projectID string, gracePeriod time.Duration) error {
	return nil
}

func (m *internalActivitySpyProjectSandboxManager) GetProjectRoute(ctx context.Context, projectID string) (*ProjectRouteHandle, bool, error) {
	return nil, false, nil
}

func (m *internalActivitySpyProjectSandboxManager) BeginProjectRuntimeActivity(projectID string) func() {
	m.mu.Lock()
	m.acquires = append(m.acquires, projectID)
	m.mu.Unlock()

	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.releases = append(m.releases, projectID)
		m.releaseMap[projectID]++
	}
}

func TestRunTask_RuntimeActivityLease_HeldThroughExecutionAndReleasedOnSuccess(t *testing.T) {
	spyMgr := newInternalActivitySpyProjectSandboxManager()
	cfg := &config.Config{HostWorktreeRoot: t.TempDir()}
	task := &models.Task{ID: "task-act-success", ProjectID: "proj-act-success"}
	attempt := &models.TaskAttempt{ID: "attempt-1", TaskID: task.ID}

	var leaseHeldDuringExecution bool
	executionRan := false

	ar := NewAgentTaskRunner(
		cfg, nil, nil, nil,
		git.NewWorktreeManager(cfg.HostWorktreeRoot),
		sandbox.NewRunner(nil), nil, ws.NewHub(),
		WithRunnerProjectSandboxManager(spyMgr),
	)
	ar.runTaskFn = func(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) error {
		executionRan = true
		spyMgr.mu.Lock()
		// While execution is running, lease must be acquired (1) but not yet released (0)
		leaseHeldDuringExecution = len(spyMgr.acquires) == 1 && len(spyMgr.releases) == 0
		spyMgr.mu.Unlock()
		return nil
	}

	err := ar.RunTask(context.Background(), task, attempt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !executionRan {
		t.Fatal("expected execution to run")
	}
	if !leaseHeldDuringExecution {
		t.Fatal("activity lease was not held during task execution")
	}

	if len(spyMgr.acquires) != 1 || spyMgr.acquires[0] != task.ProjectID {
		t.Fatalf("expected 1 acquire for %s, got %v", task.ProjectID, spyMgr.acquires)
	}
	if len(spyMgr.releases) != 1 || spyMgr.releases[0] != task.ProjectID {
		t.Fatalf("expected 1 release for %s, got %v", task.ProjectID, spyMgr.releases)
	}
}

func TestRunTask_RuntimeActivityLease_ReleasedOnError(t *testing.T) {
	spyMgr := newInternalActivitySpyProjectSandboxManager()
	cfg := &config.Config{HostWorktreeRoot: t.TempDir()}
	task := &models.Task{ID: "task-act-error", ProjectID: "proj-act-error"}
	attempt := &models.TaskAttempt{ID: "attempt-2", TaskID: task.ID}

	expectedErr := errors.New("simulated task execution failure")
	var leaseHeldDuringExecution bool

	ar := NewAgentTaskRunner(
		cfg, nil, nil, nil,
		git.NewWorktreeManager(cfg.HostWorktreeRoot),
		sandbox.NewRunner(nil), nil, ws.NewHub(),
		WithRunnerProjectSandboxManager(spyMgr),
	)
	ar.runTaskFn = func(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) error {
		spyMgr.mu.Lock()
		leaseHeldDuringExecution = len(spyMgr.acquires) == 1 && len(spyMgr.releases) == 0
		spyMgr.mu.Unlock()
		return expectedErr
	}

	err := ar.RunTask(context.Background(), task, attempt)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}

	if !leaseHeldDuringExecution {
		t.Fatal("activity lease was not held during task execution")
	}

	if len(spyMgr.acquires) != 1 || spyMgr.acquires[0] != task.ProjectID {
		t.Fatalf("expected 1 acquire for %s, got %v", task.ProjectID, spyMgr.acquires)
	}
	if len(spyMgr.releases) != 1 || spyMgr.releases[0] != task.ProjectID {
		t.Fatalf("expected 1 release for %s, got %v", task.ProjectID, spyMgr.releases)
	}
}

func TestRunTask_RuntimeActivityLease_ReleasedOnInFlightCancellation(t *testing.T) {
	spyMgr := newInternalActivitySpyProjectSandboxManager()
	cfg := &config.Config{HostWorktreeRoot: t.TempDir()}
	task := &models.Task{ID: "task-act-cancel", ProjectID: "proj-act-cancel"}
	attempt := &models.TaskAttempt{ID: "attempt-3", TaskID: task.ID}

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	unblock := make(chan struct{})

	ar := NewAgentTaskRunner(
		cfg, nil, nil, nil,
		git.NewWorktreeManager(cfg.HostWorktreeRoot),
		sandbox.NewRunner(nil), nil, ws.NewHub(),
		WithRunnerProjectSandboxManager(spyMgr),
	)
	ar.runTaskFn = func(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) error {
		close(started)
		<-ctx.Done() // Block until cancellation happens in-flight
		close(unblock)
		return ctx.Err()
	}

	var runErr error
	done := make(chan struct{})
	go func() {
		runErr = ar.RunTask(ctx, task, attempt)
		close(done)
	}()

	<-started
	spyMgr.mu.Lock()
	inFlightAcquired := len(spyMgr.acquires) == 1
	inFlightReleased := len(spyMgr.releases) == 0
	spyMgr.mu.Unlock()

	if !inFlightAcquired || !inFlightReleased {
		t.Fatalf("lease not held in-flight: acquires=%v releases=%v", inFlightAcquired, inFlightReleased)
	}

	cancel()
	<-unblock
	<-done

	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", runErr)
	}

	if len(spyMgr.acquires) != 1 || spyMgr.acquires[0] != task.ProjectID {
		t.Fatalf("expected 1 acquire for %s, got %v", task.ProjectID, spyMgr.acquires)
	}
	if len(spyMgr.releases) != 1 || spyMgr.releases[0] != task.ProjectID {
		t.Fatalf("expected 1 release for %s, got %v", task.ProjectID, spyMgr.releases)
	}
}
