// internal/queue/worker_lifecycle_test.go
package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/queue"

	"github.com/hibiken/asynq"
)

// lifecycleUnreachableRedis points the asynq server at a closed local port so that
// the server lifecycle can be exercised without any real broker or network access.
const lifecycleUnreachableRedis = "127.0.0.1:1"

func newLifecycleWorker(t *testing.T, executor queue.TaskExecutor) *queue.WorkerServer {
	t.Helper()
	cfg := &config.Config{
		RedisAddr:          lifecycleUnreachableRedis,
		MaxConcurrentTasks: 1,
	}
	return queue.NewWorkerServer(cfg, executor, nil)
}

// lifecycleExecutor is a TaskExecutor whose behaviour is supplied per test.
type lifecycleExecutor struct {
	fn    func(ctx context.Context, taskID string) error
	calls atomic.Int32
}

func (e *lifecycleExecutor) ExecuteTask(ctx context.Context, taskID string, _ queue.TaskRunner) error {
	e.calls.Add(1)
	if e.fn != nil {
		return e.fn(ctx, taskID)
	}
	return nil
}

func lifecycleTask(t *testing.T, taskID string) *asynq.Task {
	t.Helper()
	payload, err := json.Marshal(queue.TaskExecutionPayload{TaskID: taskID})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return asynq.NewTask(queue.TypeExecuteAgentTask, payload)
}

func TestWorkerServerStartIsNotIdempotentButDoesNotPanic(t *testing.T) {
	ws := newLifecycleWorker(t, nil)
	t.Cleanup(ws.Shutdown)

	if err := ws.Start(); err != nil {
		t.Fatalf("first Start returned error: %v", err)
	}

	err := ws.Start()
	if err == nil {
		t.Fatalf("second Start on a running server must return an error")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second Start error = %q, want it to report the server is already running", err)
	}
}

func TestWorkerServerStopIsIdempotent(t *testing.T) {
	ws := newLifecycleWorker(t, nil)
	t.Cleanup(ws.Shutdown)

	if err := ws.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	ws.Stop()
	// A second Stop must be a harmless no-op, not a panic or a deadlock.
	ws.Stop()

	// Stop moves the server into the stopped state, so it cannot be restarted.
	err := ws.Start()
	if err == nil {
		t.Fatalf("Start after Stop must return an error")
	}
	if !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("Start after Stop error = %q, want it to report the stopped state", err)
	}
}

func TestWorkerServerShutdownIsIdempotentAndTerminal(t *testing.T) {
	ws := newLifecycleWorker(t, nil)

	if err := ws.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	shutdownDone := make(chan struct{})
	go func() {
		ws.Shutdown()
		ws.Shutdown()
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("Shutdown did not return with no active tasks")
	}

	err := ws.Start()
	if !errors.Is(err, asynq.ErrServerClosed) {
		t.Fatalf("Start after Shutdown error = %v, want asynq.ErrServerClosed", err)
	}
}

func TestWorkerServerStopThenShutdownCompletes(t *testing.T) {
	ws := newLifecycleWorker(t, nil)

	if err := ws.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	ws.Stop()

	done := make(chan struct{})
	go func() {
		ws.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Shutdown after Stop did not return")
	}
}

func TestWorkerServerRunRefusesWhenAlreadyStarted(t *testing.T) {
	ws := newLifecycleWorker(t, nil)
	t.Cleanup(ws.Shutdown)

	if err := ws.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Run must surface the start error and return without blocking on signals.
	done := make(chan error, 1)
	go func() { done <- ws.Run() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("Run on a running server must return an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Run blocked instead of returning the start error")
	}
}

func TestWorkerServerShutdownWithoutStartIsNoop(t *testing.T) {
	ws := newLifecycleWorker(t, nil)

	done := make(chan struct{})
	go func() {
		ws.Stop()
		ws.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Stop/Shutdown on a never-started server did not return")
	}
}

func TestWorkerServerLifecycleDoesNotLeakGoroutines(t *testing.T) {
	const tolerance = 2

	// Let goroutines left by earlier tests in this package settle first.
	baseline := runtime.NumGoroutine()

	ws := newLifecycleWorker(t, nil)
	if err := ws.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	ws.Stop()
	ws.Shutdown()

	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > baseline+tolerance {
		if time.Now().After(deadline) {
			t.Fatalf("goroutines did not settle: baseline %d, now %d (tolerance %d)",
				baseline, runtime.NumGoroutine(), tolerance)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWorkerHandlerRejectsInvalidPayloadWithoutExecuting(t *testing.T) {
	executor := &lifecycleExecutor{}
	ws := newLifecycleWorker(t, executor)

	cases := map[string]*asynq.Task{
		"malformed json": asynq.NewTask(queue.TypeExecuteAgentTask, []byte("{not-json")),
		"empty task id":  lifecycleTask(t, ""),
		"nil payload":    asynq.NewTask(queue.TypeExecuteAgentTask, nil),
	}
	for name, task := range cases {
		if err := ws.Handler().ProcessTask(context.Background(), task); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if got := executor.calls.Load(); got != 0 {
		t.Fatalf("executor must not run for invalid payloads, ran %d times", got)
	}
}

func TestWorkerHandlerNilExecutorAcknowledgesTask(t *testing.T) {
	ws := newLifecycleWorker(t, nil)
	if err := ws.Handler().ProcessTask(context.Background(), lifecycleTask(t, "task-1")); err != nil {
		t.Fatalf("nil executor should acknowledge task, got %v", err)
	}
}

func TestWorkerHandlerPropagatesDeadlineToRunningJob(t *testing.T) {
	executor := &lifecycleExecutor{fn: func(ctx context.Context, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	ws := newLifecycleWorker(t, executor)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- ws.Handler().ProcessTask(ctx, lifecycleTask(t, "task-deadline")) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("in-flight job ignored its context deadline")
	}
}

func TestWorkerHandlerQuarantineSkipsRetry(t *testing.T) {
	executor := &lifecycleExecutor{fn: func(context.Context, string) error {
		return fmt.Errorf("runner: %w", common.ErrRuntimeQuarantined)
	}}
	ws := newLifecycleWorker(t, executor)

	err := ws.Handler().ProcessTask(context.Background(), lifecycleTask(t, "task-q"))
	if !errors.Is(err, common.ErrRuntimeQuarantined) {
		t.Fatalf("expected quarantine error to be preserved, got %v", err)
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("quarantined job must be marked SkipRetry, got %v", err)
	}
}

func TestWorkerHandlerTransientErrorIsRetryable(t *testing.T) {
	transient := errors.New("temporary database outage")
	executor := &lifecycleExecutor{fn: func(context.Context, string) error { return transient }}
	ws := newLifecycleWorker(t, executor)

	err := ws.Handler().ProcessTask(context.Background(), lifecycleTask(t, "task-t"))
	if !errors.Is(err, transient) {
		t.Fatalf("expected transient error to pass through, got %v", err)
	}
	if errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("transient error must remain retryable")
	}
}
