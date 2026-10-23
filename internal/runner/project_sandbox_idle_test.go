package runner_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/runner"
)

type idleMockDockerClient struct {
	mockDockerClientForSandboxManager
	mu           sync.Mutex
	stopCalls    []string
	stopHook     func(containerID string) error
	inspectCalls int
	runningState bool
}

func (m *idleMockDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalls = append(m.stopCalls, containerID)
	if m.stopHook != nil {
		return m.stopHook(containerID)
	}
	m.runningState = false
	return nil
}

func (m *idleMockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inspectCalls++
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			ID: containerID,
			State: &types.ContainerState{
				Running: m.runningState,
			},
		},
		NetworkSettings: &types.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"test-net": {IPAddress: "172.28.0.50"},
			},
		},
	}, nil
}

func (m *idleMockDockerClient) getStopCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]string, len(m.stopCalls))
	copy(copied, m.stopCalls)
	return copied
}

func TestProjectSandboxManager_Idle_ExpiresAndStops(t *testing.T) {
	cli := &idleMockDockerClient{
		mockDockerClientForSandboxManager: mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "c-idle-1"},
		},
		runningState: true,
	}

	opts := runner.DevHostAuthOptions{
		Enabled:       true,
		SkipReadiness: true,
		IdleTimeout:   50 * time.Millisecond,
	}
	mgr := runner.NewProjectSandboxManagerWithDevHostAuth(cli, opts)

	project := &models.Project{ID: "p-idle-1"}
	route, err := mgr.EnsureProjectContainer(context.Background(), project, "img")
	if err != nil || route == nil {
		t.Fatalf("EnsureProjectContainer failed: %v", err)
	}

	// Verify route is initially active
	r, ok, err := mgr.GetProjectRoute(context.Background(), project.ID)
	if err != nil || !ok || r == nil {
		t.Fatalf("expected active route, got %v, %v, %v", r, ok, err)
	}

	// Wait for idle stop
	deadline := time.Now().Add(500 * time.Millisecond)
	stopped := false
	for time.Now().Before(deadline) {
		calls := cli.getStopCalls()
		if len(calls) > 0 {
			stopped = true
			if calls[0] != "c-idle-1" {
				t.Fatalf("expected container c-idle-1 stopped, got %v", calls)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !stopped {
		t.Fatal("expected container to be stopped after idle timeout")
	}

	// Route should be invalidated
	r, ok, err = mgr.GetProjectRoute(context.Background(), project.ID)
	if err != nil || ok || r != nil {
		t.Fatalf("expected route invalidated after idle stop, got ok=%v, route=%v", ok, r)
	}
}

func TestProjectSandboxManager_Idle_RouteAccessRefreshesTimeout(t *testing.T) {
	cli := &idleMockDockerClient{
		mockDockerClientForSandboxManager: mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "c-idle-refresh"},
		},
		runningState: true,
	}

	opts := runner.DevHostAuthOptions{
		Enabled:       true,
		SkipReadiness: true,
		IdleTimeout:   80 * time.Millisecond,
	}
	mgr := runner.NewProjectSandboxManagerWithDevHostAuth(cli, opts)

	project := &models.Project{ID: "p-refresh"}
	route, err := mgr.EnsureProjectContainer(context.Background(), project, "img")
	if err != nil || route == nil {
		t.Fatalf("EnsureProjectContainer failed: %v", err)
	}

	// At 40ms, access route to refresh activity
	time.Sleep(40 * time.Millisecond)
	r, ok, err := mgr.GetProjectRoute(context.Background(), project.ID)
	if err != nil || !ok || r == nil {
		t.Fatalf("expected route present at 40ms")
	}

	// At 70ms from start (30ms after access), container should NOT be stopped yet
	time.Sleep(30 * time.Millisecond)
	if len(cli.getStopCalls()) > 0 {
		t.Fatal("container stopped prematurely; route access should have refreshed the idle timer")
	}

	// Wait past the extended deadline (80ms after 40ms access = 120ms from start)
	deadline := time.Now().Add(200 * time.Millisecond)
	stopped := false
	for time.Now().Before(deadline) {
		if len(cli.getStopCalls()) > 0 {
			stopped = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !stopped {
		t.Fatal("expected container to stop after extended idle deadline")
	}
}

func TestProjectSandboxManager_Idle_DisabledWhenZero(t *testing.T) {
	cli := &idleMockDockerClient{
		mockDockerClientForSandboxManager: mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "c-zero"},
		},
		runningState: true,
	}

	opts := runner.DevHostAuthOptions{
		Enabled:       true,
		SkipReadiness: true,
		IdleTimeout:   0, // disabled
	}
	mgr := runner.NewProjectSandboxManagerWithDevHostAuth(cli, opts)

	project := &models.Project{ID: "p-zero"}
	_, err := mgr.EnsureProjectContainer(context.Background(), project, "img")
	if err != nil {
		t.Fatalf("EnsureProjectContainer failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if len(cli.getStopCalls()) > 0 {
		t.Fatal("idle stop called even though IdleTimeout is 0 (disabled)")
	}
}

func TestProjectSandboxManager_Idle_TaskLeaseBlocksStop(t *testing.T) {
	cli := &idleMockDockerClient{
		mockDockerClientForSandboxManager: mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "c-lease"},
		},
		runningState: true,
	}

	opts := runner.DevHostAuthOptions{
		Enabled:       true,
		SkipReadiness: true,
		IdleTimeout:   50 * time.Millisecond,
	}
	mgr := runner.NewProjectSandboxManagerWithDevHostAuth(cli, opts)

	project := &models.Project{ID: "p-lease"}
	_, err := mgr.EnsureProjectContainer(context.Background(), project, "img")
	if err != nil {
		t.Fatalf("EnsureProjectContainer failed: %v", err)
	}

	actMgr, ok := mgr.(runner.ProjectRuntimeActivityManager)
	if !ok {
		t.Fatalf("expected manager to implement ProjectRuntimeActivityManager")
	}

	release := actMgr.BeginProjectRuntimeActivity(project.ID)

	// Wait beyond the 50ms timeout; lease is active so it must not stop
	time.Sleep(100 * time.Millisecond)
	if len(cli.getStopCalls()) > 0 {
		t.Fatal("container stopped while task activity lease was active")
	}

	// Release lease; a new idle timeout starts from release
	release()

	// Should still not stop immediately after release
	if len(cli.getStopCalls()) > 0 {
		t.Fatal("container stopped immediately after lease release without waiting for idle timeout")
	}

	// Should stop after the fresh 50ms idle timeout
	deadline := time.Now().Add(250 * time.Millisecond)
	stopped := false
	for time.Now().Before(deadline) {
		if len(cli.getStopCalls()) > 0 {
			stopped = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !stopped {
		t.Fatal("expected container to stop after idle timeout following lease release")
	}
}

func TestProjectSandboxManager_Idle_StopFailurePreservesRouteAndRetries(t *testing.T) {
	stopAttempt := 0
	cli := &idleMockDockerClient{
		mockDockerClientForSandboxManager: mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "c-fail-retry"},
		},
		runningState: true,
		stopHook: func(containerID string) error {
			stopAttempt++
			if stopAttempt == 1 {
				return errors.New("transient docker daemon error")
			}
			return nil
		},
	}

	opts := runner.DevHostAuthOptions{
		Enabled:       true,
		SkipReadiness: true,
		IdleTimeout:   80 * time.Millisecond,
	}
	mgr := runner.NewProjectSandboxManagerWithDevHostAuth(cli, opts)

	// Configure retry delay to 30ms (distinct from and significantly shorter than IdleTimeout)
	peeker, ok := mgr.(interface {
		SetIdleStopRetryDelay(time.Duration)
		PeekProjectRoute(string) (*runner.ProjectRouteHandle, bool)
	})
	if !ok {
		t.Fatalf("expected manager to implement SetIdleStopRetryDelay and PeekProjectRoute")
	}
	peeker.SetIdleStopRetryDelay(30 * time.Millisecond)

	project := &models.Project{ID: "p-fail-retry"}
	_, err := mgr.EnsureProjectContainer(context.Background(), project, "img")
	if err != nil {
		t.Fatalf("EnsureProjectContainer failed: %v", err)
	}

	start := time.Now()

	// Wait for first attempt (at ~80ms) which fails
	time.Sleep(95 * time.Millisecond)
	if len(cli.getStopCalls()) != 1 {
		t.Fatalf("expected 1 failed stop attempt, got %d", len(cli.getStopCalls()))
	}

	// Verify route is preserved using non-activity-touching PeekProjectRoute
	peekedRoute, ok := peeker.PeekProjectRoute(project.ID)
	if !ok || peekedRoute == nil {
		t.Fatalf("route must be preserved when docker stop fails, got ok=%v, route=%v", ok, peekedRoute)
	}

	// Wait for the retry to occur at ~30ms after the first stop (i.e. around 80ms + 30ms = 110ms from start)
	// crucially, before a second full 80ms idle timeout (160ms) could have elapsed!
	retryDone := false
	deadline := time.Now().Add(60 * time.Millisecond) // up to ~155ms from start
	for time.Now().Before(deadline) {
		cli.mu.Lock()
		calls := len(cli.stopCalls)
		cli.mu.Unlock()
		if calls >= 2 {
			retryDone = true
			elapsedSinceStart := time.Since(start)
			// Must have triggered via retry timer before full idle interval (160ms) could expire
			if elapsedSinceStart >= 160*time.Millisecond {
				t.Fatalf("second stop happened at %v, expected retry at ~110ms (<160ms)", elapsedSinceStart)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if !retryDone {
		t.Fatalf("expected retry stop call, got calls: %v", cli.getStopCalls())
	}

	// Route should now be invalidated after second successful stop
	peekedRoute, ok = peeker.PeekProjectRoute(project.ID)
	if ok || peekedRoute != nil {
		t.Fatalf("route must be invalidated after successful retry stop, got ok=%v", ok)
	}
}

func TestProjectSandboxManager_ManualStop_FailurePreservesRouteAndRetries(t *testing.T) {
	stopAttempt := 0
	cli := &idleMockDockerClient{
		mockDockerClientForSandboxManager: mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "c-manual-fail-retry"},
		},
		runningState: true,
		stopHook: func(containerID string) error {
			stopAttempt++
			if stopAttempt == 1 {
				return errors.New("transient daemon stop failure on manual stop")
			}
			return nil
		},
	}

	opts := runner.DevHostAuthOptions{
		Enabled:       true,
		SkipReadiness: true,
		IdleTimeout:   10 * time.Second, // Long idle timeout so idle stop doesn't trigger
	}
	mgr := runner.NewProjectSandboxManagerWithDevHostAuth(cli, opts)

	if setter, ok := mgr.(interface{ SetIdleStopRetryDelay(time.Duration) }); ok {
		setter.SetIdleStopRetryDelay(50 * time.Millisecond)
	}

	project := &models.Project{ID: "p-manual-retry"}
	_, err := mgr.EnsureProjectContainer(context.Background(), project, "img")
	if err != nil {
		t.Fatalf("EnsureProjectContainer failed: %v", err)
	}

	// Manual stop: first attempt fails
	err = mgr.StopProjectContainer(context.Background(), project.ID, 0)
	if err == nil {
		t.Fatal("expected error on failed manual stop")
	}

	// Wait for retry to succeed after short delay (~50ms)
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		cli.mu.Lock()
		calls := len(cli.stopCalls)
		cli.mu.Unlock()
		if calls >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(cli.getStopCalls()) < 2 {
		t.Fatalf("expected retry after failed manual stop, got calls: %v", cli.getStopCalls())
	}

	// Verify route is preserved right after manual stop failure, before/after retry
	route, ok, err := mgr.GetProjectRoute(context.Background(), project.ID)
	if err != nil || ok || route != nil {
		t.Fatalf("route must be invalidated after retry succeeds, got ok=%v", ok)
	}
}

func TestProjectSandboxManager_Idle_RouteAccessCancelsPendingRetry(t *testing.T) {
	stopAttempt := 0
	cli := &idleMockDockerClient{
		mockDockerClientForSandboxManager: mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "c-retry-route-access"},
		},
		runningState: true,
		stopHook: func(containerID string) error {
			stopAttempt++
			if stopAttempt == 1 {
				return errors.New("transient daemon stop failure")
			}
			return nil
		},
	}

	opts := runner.DevHostAuthOptions{
		Enabled:       true,
		SkipReadiness: true,
		IdleTimeout:   50 * time.Millisecond,
	}
	mgr := runner.NewProjectSandboxManagerWithDevHostAuth(cli, opts)

	// Set retry delay to 70ms
	if setter, ok := mgr.(interface{ SetIdleStopRetryDelay(time.Duration) }); ok {
		setter.SetIdleStopRetryDelay(70 * time.Millisecond)
	}

	project := &models.Project{ID: "p-retry-route"}
	_, err := mgr.EnsureProjectContainer(context.Background(), project, "img")
	if err != nil {
		t.Fatalf("EnsureProjectContainer failed: %v", err)
	}

	// Wait for first attempt (at ~50ms) to fail and schedule retry
	time.Sleep(60 * time.Millisecond)
	if len(cli.getStopCalls()) != 1 {
		t.Fatalf("expected 1 failed stop attempt, got %d", len(cli.getStopCalls()))
	}

	// Now access the route at 70ms (before retry at 50+70 = 120ms)
	route, ok, err := mgr.GetProjectRoute(context.Background(), project.ID)
	if err != nil || !ok || route == nil {
		t.Fatalf("expected route to exist")
	}

	// Sleep 20ms: total elapsed since access at 70ms is 20ms (< 50ms idleTimeout)
	time.Sleep(20 * time.Millisecond)
	// Because route access refreshed activity and canceled the retry timer, no second stop should have happened yet
	if len(cli.getStopCalls()) > 1 {
		t.Fatalf("retry timer stopped container prematurely despite recent route access")
	}

	// Wait for the new idle timeout (50ms after the 70ms access = 120ms + margins)
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		cli.mu.Lock()
		calls := len(cli.stopCalls)
		cli.mu.Unlock()
		if calls >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(cli.getStopCalls()) < 2 {
		t.Fatalf("expected container to be stopped after new idle timeout")
	}
}

func TestProjectSandboxManager_Idle_TaskLeaseCancelsPendingRetry(t *testing.T) {
	stopAttempt := 0
	cli := &idleMockDockerClient{
		mockDockerClientForSandboxManager: mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "c-retry-lease"},
		},
		runningState: true,
		stopHook: func(containerID string) error {
			stopAttempt++
			if stopAttempt == 1 {
				return errors.New("transient daemon stop failure")
			}
			return nil
		},
	}

	opts := runner.DevHostAuthOptions{
		Enabled:       true,
		SkipReadiness: true,
		IdleTimeout:   50 * time.Millisecond,
	}
	mgr := runner.NewProjectSandboxManagerWithDevHostAuth(cli, opts)

	if setter, ok := mgr.(interface{ SetIdleStopRetryDelay(time.Duration) }); ok {
		setter.SetIdleStopRetryDelay(70 * time.Millisecond)
	}

	project := &models.Project{ID: "p-retry-lease"}
	_, err := mgr.EnsureProjectContainer(context.Background(), project, "img")
	if err != nil {
		t.Fatalf("EnsureProjectContainer failed: %v", err)
	}

	// Wait for first attempt (at ~50ms) to fail and schedule retry
	time.Sleep(60 * time.Millisecond)
	if len(cli.getStopCalls()) != 1 {
		t.Fatalf("expected 1 failed stop attempt, got %d", len(cli.getStopCalls()))
	}

	// Acquire task lease at ~70ms
	actMgr := mgr.(runner.ProjectRuntimeActivityManager)
	release := actMgr.BeginProjectRuntimeActivity(project.ID)

	// Release lease after 20ms (at ~90ms)
	time.Sleep(20 * time.Millisecond)
	release()

	// Past the original 120ms mark (wait until 130ms from start)
	time.Sleep(30 * time.Millisecond)
	if len(cli.getStopCalls()) > 1 {
		t.Fatalf("retry timer stopped container prematurely; task lease should have canceled/reset retry")
	}

	// Wait for fresh idle timeout (50ms after release at 90ms = 140ms from start)
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		cli.mu.Lock()
		calls := len(cli.stopCalls)
		cli.mu.Unlock()
		if calls >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(cli.getStopCalls()) < 2 {
		t.Fatalf("expected container to be stopped after new idle timeout following task lease release")
	}
}

func TestProjectSandboxManager_Idle_OldGenerationCallbackIgnored(t *testing.T) {
	cli := &idleMockDockerClient{
		mockDockerClientForSandboxManager: mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "c-old"},
		},
		runningState: true,
	}

	opts := runner.DevHostAuthOptions{
		Enabled:       true,
		SkipReadiness: true,
		IdleTimeout:   50 * time.Millisecond,
	}
	mgr := runner.NewProjectSandboxManagerWithDevHostAuth(cli, opts)

	project := &models.Project{ID: "p-gen"}
	route1, err := mgr.EnsureProjectContainer(context.Background(), project, "img")
	if err != nil {
		t.Fatalf("EnsureProjectContainer 1 failed: %v", err)
	}

	// Now simulate container restart or replacement: stop and ensure new container
	if err := mgr.StopProjectContainer(context.Background(), project.ID, 0); err != nil {
		t.Fatalf("StopProjectContainer failed: %v", err)
	}

	cli.createResp = container.CreateResponse{ID: "c-new"}
	cli.runningState = true
	route2, err := mgr.EnsureProjectContainer(context.Background(), project, "img")
	if err != nil {
		t.Fatalf("EnsureProjectContainer 2 failed: %v", err)
	}

	if route2.ContainerID == route1.ContainerID {
		t.Fatalf("expected new container ID")
	}

	// Sleep 30ms (less than 50ms idle timeout for c-new): the old timer callback for c-old would have fired at 50ms from start
	time.Sleep(30 * time.Millisecond)

	// Ensure route2 (c-new) is still valid and not killed by c-old timer
	calls := cli.getStopCalls()
	for _, call := range calls {
		if call == "c-new" {
			t.Fatalf("c-new was prematurely stopped by stale timer callback: %v", calls)
		}
	}
}

func TestProjectSandboxManager_Idle_ManualStopCancelsTimer(t *testing.T) {
	cli := &idleMockDockerClient{
		mockDockerClientForSandboxManager: mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "c-manual"},
		},
		runningState: true,
	}

	opts := runner.DevHostAuthOptions{
		Enabled:       true,
		SkipReadiness: true,
		IdleTimeout:   50 * time.Millisecond,
	}
	mgr := runner.NewProjectSandboxManagerWithDevHostAuth(cli, opts)

	project := &models.Project{ID: "p-manual"}
	_, err := mgr.EnsureProjectContainer(context.Background(), project, "img")
	if err != nil {
		t.Fatalf("EnsureProjectContainer failed: %v", err)
	}

	// Manual stop at 20ms
	time.Sleep(20 * time.Millisecond)
	if err := mgr.StopProjectContainer(context.Background(), project.ID, 0); err != nil {
		t.Fatalf("StopProjectContainer failed: %v", err)
	}

	initialCalls := len(cli.getStopCalls())
	if initialCalls != 1 {
		t.Fatalf("expected exactly 1 stop call, got %d", initialCalls)
	}

	// Sleep past original 50ms timeout
	time.Sleep(60 * time.Millisecond)
	afterCalls := len(cli.getStopCalls())
	if afterCalls != initialCalls {
		t.Fatalf("expected timer to be canceled by manual stop; got %d stop calls", afterCalls)
	}
}
