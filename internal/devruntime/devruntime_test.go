package devruntime_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-task-manager/internal/devruntime"
	"agent-task-manager/internal/models"
)

type logSink struct {
	mu   sync.Mutex
	logs []*models.TaskLog
}

func (s *logSink) Append(_ context.Context, l *models.TaskLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, l)
	return nil
}

type hubSink struct {
	mu sync.Mutex
	n  int
}

func (h *hubSink) BroadcastTaskLog(string, string) error {
	h.mu.Lock()
	h.n++
	h.mu.Unlock()
	return nil
}

func TestRunnerCompletesAndLogsEveryStep(t *testing.T) {
	logs, hub := &logSink{}, &hubSink{}
	r := devruntime.NewRunner(logs, hub, time.Millisecond)
	task := &models.Task{ID: "t1", Title: "Improve things"}
	attempt := &models.TaskAttempt{ID: "a1"}

	if err := r.RunTask(context.Background(), task, attempt); err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(logs.logs) < 3 || hub.n != len(logs.logs) {
		t.Fatalf("logs=%d broadcasts=%d, want >=3 and equal", len(logs.logs), hub.n)
	}
	all := ""
	for _, l := range logs.logs {
		if l.TaskID != "t1" || l.AttemptID == nil || *l.AttemptID != "a1" {
			t.Fatalf("log not tied to task/attempt: %+v", l)
		}
		all += l.LogChunk
	}
	if !strings.Contains(strings.ToLower(all), "simulated") || !strings.Contains(all, "Improve things") {
		t.Fatalf("logs must say they are simulated and name the task: %q", all)
	}
}

func TestRunnerStopsOnCancel(t *testing.T) {
	r := devruntime.NewRunner(&logSink{}, &hubSink{}, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.RunTask(ctx, &models.Task{ID: "t"}, &models.TaskAttempt{ID: "a"}) }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled run must return an error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunTask ignored cancellation")
	}
}

func TestRunnerRejectsNilTask(t *testing.T) {
	if err := devruntime.NewRunner(nil, nil, 0).RunTask(context.Background(), nil, nil); err == nil {
		t.Fatal("nil task must error")
	}
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestEnabledFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    bool
		wantErr bool
	}{
		{"unset", map[string]string{"APP_ENV": "development"}, false, false},
		{"false", map[string]string{"APP_ENV": "development", "ATM_DEV_FAKE_RUNTIME": "false"}, false, false},
		{"on in development", map[string]string{"APP_ENV": "development", "ATM_DEV_FAKE_RUNTIME": "true"}, true, false},
		{"refused in production", map[string]string{"APP_ENV": "production", "ATM_DEV_FAKE_RUNTIME": "true"}, false, true},
		{"refused when env unset", map[string]string{"ATM_DEV_FAKE_RUNTIME": "true"}, false, true},
		{"refused in staging", map[string]string{"APP_ENV": "staging", "ATM_DEV_FAKE_RUNTIME": "true"}, false, true},
		{"garbage value", map[string]string{"APP_ENV": "development", "ATM_DEV_FAKE_RUNTIME": "yes"}, false, true},
	}
	for _, c := range cases {
		got, err := devruntime.EnabledFromEnv(env(c.env))
		if got != c.want || (err != nil) != c.wantErr {
			t.Errorf("%s: got (%v, %v), want (%v, err=%v)", c.name, got, err, c.want, c.wantErr)
		}
	}
}
