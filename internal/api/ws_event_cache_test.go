package api

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/service"
)

type countingTaskSvc struct {
	service.TaskService
	calls atomic.Int64
	err   error
}

func (s *countingTaskSvc) GetTask(ctx context.Context, taskID, userID string) (*models.Task, error) {
	s.calls.Add(1)
	return &models.Task{ID: taskID}, s.err
}

type sinkClient struct{ sent atomic.Int64 }

func (c *sinkClient) ID() string        { return "c" }
func (c *sinkClient) TaskID() string    { return "global" }
func (c *sinkClient) Send([]byte) error { c.sent.Add(1); return nil }
func (c *sinkClient) Close() error      { return nil }

func TestVisibleTaskEventsClientCachesAuthorizationPerTask(t *testing.T) {
	svc, sink := &countingTaskSvc{}, &sinkClient{}
	now := time.Unix(1_000_000, 0)
	c := &visibleTaskEventsClient{Client: sink, userID: "u", taskSvc: svc, now: func() time.Time { return now }}
	msg := []byte(`{"task_id":"t1"}`)

	for i := 0; i < 5; i++ {
		_ = c.Send(msg)
	}
	if got := svc.calls.Load(); got != 1 {
		t.Fatalf("5 events for one task made %d lookups, want 1", got)
	}
	if sink.sent.Load() != 5 {
		t.Fatalf("delivered %d, want 5", sink.sent.Load())
	}
	_ = c.Send([]byte(`{"task_id":"t2"}`))
	if got := svc.calls.Load(); got != 2 {
		t.Fatalf("a different task must be looked up separately, calls = %d", got)
	}

	now = now.Add(eventAuthzCacheTTL + time.Second)
	_ = c.Send(msg)
	if got := svc.calls.Load(); got != 3 {
		t.Fatalf("expired entry must be re-checked, calls = %d", got)
	}
}

func TestVisibleTaskEventsClientCachesDenialsAndDropsThem(t *testing.T) {
	svc, sink := &countingTaskSvc{err: common.ErrForbidden}, &sinkClient{}
	c := &visibleTaskEventsClient{Client: sink, userID: "u", taskSvc: svc, now: time.Now}
	for i := 0; i < 3; i++ {
		_ = c.Send([]byte(`{"task_id":"t1"}`))
	}
	if sink.sent.Load() != 0 {
		t.Fatalf("forbidden task events delivered: %d", sink.sent.Load())
	}
	if got := svc.calls.Load(); got != 1 {
		t.Fatalf("denial must be cached too, calls = %d", got)
	}

	// A transient lookup failure drops the event but is not cached as a denial.
	svc2 := &countingTaskSvc{err: errors.New("db down")}
	c2 := &visibleTaskEventsClient{Client: sink, userID: "u", taskSvc: svc2, now: time.Now}
	_ = c2.Send([]byte(`{"task_id":"t1"}`))
	_ = c2.Send([]byte(`{"task_id":"t1"}`))
	if got := svc2.calls.Load(); got != 2 {
		t.Fatalf("transient errors must not be cached, calls = %d", got)
	}
}
