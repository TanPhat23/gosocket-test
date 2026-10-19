package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/service"
)

// subagentClient lists ten child sessions and gives the newest one a long transcript with oversized tool output.
type subagentClient struct {
	tunnelClient
}

func (c *subagentClient) ListChildSessions(_ context.Context, _ string) ([]runner.OpenCodeSessionDTO, error) {
	kids := make([]runner.OpenCodeSessionDTO, 0, 10)
	for i := 1; i <= 10; i++ {
		kids = append(kids, runner.OpenCodeSessionDTO{ID: fmt.Sprintf("child-%d", i), Title: fmt.Sprintf("child %d", i), Status: "running"})
	}
	return kids, nil
}

func (c *subagentClient) GetSessionMessages(_ context.Context, sessionID string) ([]dto.OpenCodeMessageDTO, error) {
	if !strings.HasPrefix(sessionID, "child-") {
		return []dto.OpenCodeMessageDTO{{ID: "root"}}, nil
	}
	msgs := make([]dto.OpenCodeMessageDTO, 0, 45)
	for i := 0; i < 45; i++ {
		msgs = append(msgs, dto.OpenCodeMessageDTO{ID: fmt.Sprintf("%s-m%02d", sessionID, i)})
	}
	if sessionID == "child-10" {
		huge := strings.Repeat("x", 5000)
		msgs[len(msgs)-1].Content = []any{
			map[string]any{"type": "tool", "tool": "shell", "state": map[string]any{"status": "completed", "output": huge}},
		}
	}
	return msgs, nil
}

func TestTaskService_GetTaskSession_IncludesRecentSubagentActivity(t *testing.T) {
	f := newTunnelFixture(t, true)
	client := &subagentClient{}
	resolve := func(_ context.Context, projectID string) (runner.OpenCodeClient, error) {
		if projectID == f.projA.ID {
			return client, nil
		}
		return nil, errors.New("unexpected project")
	}
	svc := service.NewTaskService(f.spy, repository.NewAttemptRepository(f.db), f.queue, nil,
		service.WithTaskSessionDependencies(nil, repository.NewProjectRepository(f.db), repository.NewTeamRepository(f.db)),
		service.WithTaskSessionClientResolver(resolve))

	sess, err := svc.GetTaskSession(context.Background(), f.taskA.ID, f.memberA.ID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}

	if len(sess.Children) != 8 {
		t.Fatalf("children = %d, want the newest 8", len(sess.Children))
	}
	if sess.Children[0].ID != "child-3" || sess.Children[7].ID != "child-10" {
		t.Fatalf("children = %s..%s, want child-3..child-10", sess.Children[0].ID, sess.Children[7].ID)
	}
	if got := len(sess.Children[0].Messages); got != 40 {
		t.Fatalf("messages for child-3 = %d, want the newest 40", got)
	}
	last := sess.Children[7].Messages
	if got := last[len(last)-1].ID; got != "child-10-m44" {
		t.Fatalf("newest message = %s, want child-10-m44", got)
	}
	parts := last[len(last)-1].Content.([]any)
	state := parts[0].(map[string]any)["state"].(map[string]any)
	if got := len([]rune(state["output"].(string))); got != 2001 {
		t.Fatalf("tool output kept %d runes, want 2000 plus an ellipsis", got)
	}
}
