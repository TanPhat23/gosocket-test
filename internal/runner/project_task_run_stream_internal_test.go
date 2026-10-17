package runner

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
)

func (o *ptrOC) GetSessionMessages(context.Context, string) ([]OpenCodeMessageDTO, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]OpenCodeMessageDTO(nil), o.msgs...), nil
}

type ptrLogRepo struct {
	repository.LogRepository
	mu    sync.Mutex
	chunk []string
}

func (l *ptrLogRepo) Append(_ context.Context, log *models.TaskLog) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.chunk = append(l.chunk, log.LogChunk)
	return nil
}

func (l *ptrLogRepo) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.chunk, "")
}

func assistantTurn(id string, at time.Time) OpenCodeMessageDTO {
	return OpenCodeMessageDTO{
		ID: id, Role: "assistant", Agent: "build", CreatedAt: &at,
		Content: []any{
			map[string]any{
				"type": "tool", "name": "bash", "id": id + "-tool",
				"state": map[string]any{
					"status": "completed",
					"input":  map[string]any{"command": "go build ./..."},
					"output": "build ok",
				},
			},
			map[string]any{"type": "text", "text": "I added the thing"},
		},
	}
}

func TestProjectTaskRun_StreamsTheRootSessionsToolCallsAndText(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, []int{0}, true)
	logs := &ptrLogRepo{}
	f.r.logRepo = logs
	f.oc.msgs = []OpenCodeMessageDTO{assistantTurn("m1", time.Now())}

	if err := f.run(t); err != nil {
		t.Fatal(err)
	}
	got := logs.all()
	for _, want := range []string{"[bash]", "go build ./...", "build ok", "I added the thing"} {
		if !strings.Contains(got, want) {
			t.Fatalf("task log is missing %q:\n%q", want, got)
		}
	}
	if n := strings.Count(got, "I added the thing"); n != 1 {
		t.Fatalf("assistant text relayed %d times, want exactly once", n)
	}
	if strings.Index(got, "I added the thing") > strings.Index(got, "Snapshot captured") {
		t.Fatalf("session output must be flushed before the turn is reported finished:\n%q", got)
	}
}

func TestProjectTaskRun_DoesNotReplayMessagesFromBeforeThisRun(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, []int{0}, true)
	logs := &ptrLogRepo{}
	f.r.logRepo = logs
	f.oc.msgs = []OpenCodeMessageDTO{assistantTurn("old", time.Now().Add(-time.Hour))}

	if err := f.run(t); err != nil {
		t.Fatal(err)
	}
	if got := logs.all(); strings.Contains(got, "I added the thing") {
		t.Fatalf("a previous attempt's output leaked into this attempt's log:\n%q", got)
	}
}
