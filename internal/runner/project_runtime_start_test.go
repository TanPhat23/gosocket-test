package runner

import (
	"context"
	"sync"
	"testing"
	"time"

	"agent-task-manager/internal/models"
)

// fakeSandboxManager records each EnsureProjectContainer call. When block is set, the call waits on it, so a test
// can hold a start in flight.
type fakeSandboxManager struct {
	mu        sync.Mutex
	imageTags []string
	block     chan struct{}
	finished  chan string
}

func (f *fakeSandboxManager) EnsureProjectContainer(_ context.Context, project *models.Project, imageTag string) (*ProjectRouteHandle, error) {
	f.mu.Lock()
	f.imageTags = append(f.imageTags, imageTag)
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	if f.finished != nil {
		f.finished <- project.ID
	}
	return &ProjectRouteHandle{ContainerID: "ctr-" + project.ID}, nil
}

func (f *fakeSandboxManager) StopProjectContainer(context.Context, string, time.Duration) error {
	return nil
}

func (f *fakeSandboxManager) GetProjectRoute(context.Context, string) (*ProjectRouteHandle, bool, error) {
	return nil, false, nil
}

func TestEnsureProjectRuntime_UsesTheRunImageAndReturnsTheRoute(t *testing.T) {
	mgr := &fakeSandboxManager{}
	r := &AgentTaskRunner{projectSandboxMgr: mgr, projectImage: "atm-project-runtime:test"}

	route, err := r.EnsureProjectRuntime(context.Background(), &models.Project{ID: "p1"})
	if err != nil {
		t.Fatalf("EnsureProjectRuntime: %v", err)
	}
	if route == nil || route.ContainerID != "ctr-p1" {
		t.Fatalf("route = %+v, want container ctr-p1", route)
	}
	if len(mgr.imageTags) != 1 || mgr.imageTags[0] != "atm-project-runtime:test" {
		t.Fatalf("image tags = %v, want the run image", mgr.imageTags)
	}
}

func TestEnsureProjectRuntime_RefusesWithoutASandboxManager(t *testing.T) {
	r := &AgentTaskRunner{}

	if _, err := r.EnsureProjectRuntime(context.Background(), &models.Project{ID: "p1"}); err != ErrRuntimeExecutionUnavailable {
		t.Fatalf("err = %v, want ErrRuntimeExecutionUnavailable", err)
	}
}

func TestWarmProjectRuntime_ReportsAnUnconfiguredRuntime(t *testing.T) {
	r := &AgentTaskRunner{}

	if err := r.WarmProjectRuntime(models.Project{ID: "p1"}); err != ErrRuntimeExecutionUnavailable {
		t.Fatalf("err = %v, want ErrRuntimeExecutionUnavailable", err)
	}
}

func TestWarmProjectRuntime_OneStartPerProjectInFlight(t *testing.T) {
	mgr := &fakeSandboxManager{block: make(chan struct{}), finished: make(chan string, 4)}
	r := &AgentTaskRunner{projectSandboxMgr: mgr, projectImage: "img"}
	project := models.Project{ID: "p1"}

	if err := r.WarmProjectRuntime(project); err != nil {
		t.Fatalf("WarmProjectRuntime: %v", err)
	}
	if err := r.WarmProjectRuntime(project); err != nil { // a second call while the first is in flight does nothing
		t.Fatalf("second WarmProjectRuntime: %v", err)
	}

	close(mgr.block)
	select {
	case <-mgr.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("warm start did not finish")
	}
	select {
	case id := <-mgr.finished:
		t.Fatalf("a second start ran for %s while one was in flight", id)
	case <-time.After(100 * time.Millisecond):
	}

	// Once the start has finished, the project can be warmed again.
	mgr.mu.Lock()
	mgr.block = nil
	mgr.mu.Unlock()
	if err := r.WarmProjectRuntime(project); err != nil {
		t.Fatalf("later WarmProjectRuntime: %v", err)
	}
	select {
	case <-mgr.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("a later warm start did not run")
	}
}
