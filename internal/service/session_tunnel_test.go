package service_test

import (
	"context"
	"errors"
	"testing"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/service"
)

// tunnelClient records which session each call targeted.
type tunnelClient struct {
	mockOpenCodeClient
	prompts, compacts, interrupts []string
	promptText                    string
}

func (c *tunnelClient) PromptSession(_ context.Context, sessionID, prompt string) error {
	c.prompts = append(c.prompts, sessionID)
	c.promptText = prompt
	return nil
}

func (c *tunnelClient) CompactSession(_ context.Context, sessionID string) error {
	c.compacts = append(c.compacts, sessionID)
	return nil
}

func (c *tunnelClient) InterruptSessionTree(_ context.Context, sessionID string) error {
	c.interrupts = append(c.interrupts, sessionID)
	return nil
}

type tunnelFixture struct {
	*authzFixture
	clients  map[string]*tunnelClient // by project ID
	resolved []string                 // project IDs the resolver was asked for
	resolveE error
	svc      service.TaskService
}

func newTunnelFixture(t *testing.T, withSession bool) *tunnelFixture {
	t.Helper()
	a := newAuthzFixture(t)
	if withSession {
		for _, tk := range []*models.Task{a.taskA, a.taskB} {
			sid := "ses_" + tk.ID
			if err := a.db.Model(&models.Task{}).Where("id = ?", tk.ID).Update("session_id", sid).Error; err != nil {
				t.Fatalf("set session id: %v", err)
			}
		}
	}
	f := &tunnelFixture{authzFixture: a, clients: map[string]*tunnelClient{
		a.projA.ID: {}, a.projB.ID: {},
	}}
	resolve := func(_ context.Context, projectID string) (runner.OpenCodeClient, error) {
		f.resolved = append(f.resolved, projectID)
		if f.resolveE != nil {
			return nil, f.resolveE
		}
		return f.clients[projectID], nil
	}
	f.svc = service.NewTaskService(a.spy, repository.NewAttemptRepository(a.db), a.queue, nil,
		service.WithTaskSessionDependencies(nil, repository.NewProjectRepository(a.db), repository.NewTeamRepository(a.db)),
		service.WithTaskSessionClientResolver(resolve))
	return f
}

func (f *tunnelFixture) sessionID(t *testing.T, taskID string) string {
	t.Helper()
	tk, err := f.spy.GetByID(context.Background(), taskID)
	if err != nil || tk.SessionID == nil {
		t.Fatalf("task %s has no session: %v", taskID, err)
	}
	return *tk.SessionID
}

func TestSessionTunnel_MemberActionsGoToTheTasksOwnProject(t *testing.T) {
	f := newTunnelFixture(t, true)
	ctx := context.Background()
	want := f.sessionID(t, f.taskA.ID)

	if err := f.svc.PromptTaskSession(ctx, f.taskA.ID, f.memberA.ID, &dto.TaskSessionPromptRequest{Prompt: "do it"}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if err := f.svc.CompactTaskSession(ctx, f.taskA.ID, f.memberA.ID); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if err := f.svc.InterruptTaskSession(ctx, f.taskA.ID, f.memberA.ID); err != nil {
		t.Fatalf("interrupt: %v", err)
	}

	a, b := f.clients[f.projA.ID], f.clients[f.projB.ID]
	if len(a.prompts) != 1 || a.prompts[0] != want || a.promptText != "do it" {
		t.Errorf("prompt not delivered to project A's session %q: %+v", want, a)
	}
	if len(a.compacts) != 1 || a.compacts[0] != want || len(a.interrupts) != 1 || a.interrupts[0] != want {
		t.Errorf("compact/interrupt not delivered to project A's session: %+v", a)
	}
	if len(b.prompts)+len(b.compacts)+len(b.interrupts) != 0 {
		t.Errorf("project B's sandbox must receive nothing, got %+v", b)
	}
	for _, id := range f.resolved {
		if id != f.projA.ID {
			t.Errorf("resolver asked for project %q, want only %q", id, f.projA.ID)
		}
	}
}

func TestSessionTunnel_ViewerMayReadButNotAct(t *testing.T) {
	f := newTunnelFixture(t, true)
	ctx := context.Background()

	got, err := f.svc.GetTaskSession(ctx, f.taskA.ID, f.viewerA.ID)
	if err != nil || got == nil || got.SessionID != f.sessionID(t, f.taskA.ID) || len(got.Messages) == 0 {
		t.Fatalf("viewer must read the session, got %+v / %v", got, err)
	}
	resolvedAfterRead := len(f.resolved)

	acts := map[string]error{
		"prompt":    f.svc.PromptTaskSession(ctx, f.taskA.ID, f.viewerA.ID, &dto.TaskSessionPromptRequest{Prompt: "x"}),
		"compact":   f.svc.CompactTaskSession(ctx, f.taskA.ID, f.viewerA.ID),
		"interrupt": f.svc.InterruptTaskSession(ctx, f.taskA.ID, f.viewerA.ID),
	}
	for name, err := range acts {
		if !errors.Is(err, common.ErrForbidden) {
			t.Errorf("viewer %s: expected ErrForbidden, got %v", name, err)
		}
	}
	if len(f.resolved) != resolvedAfterRead {
		t.Errorf("a forbidden action must not reach a sandbox (resolver calls %d -> %d)", resolvedAfterRead, len(f.resolved))
	}
}

func TestSessionTunnel_OtherTeamsAndOutsidersAreForbidden(t *testing.T) {
	f := newTunnelFixture(t, true)
	ctx := context.Background()
	for who, uid := range map[string]string{"outsider": f.outsid.ID, "other team": f.memberB.ID, "empty": ""} {
		if _, err := f.svc.GetTaskSession(ctx, f.taskA.ID, uid); !errors.Is(err, common.ErrForbidden) {
			t.Errorf("%s get: expected ErrForbidden, got %v", who, err)
		}
		if err := f.svc.PromptTaskSession(ctx, f.taskA.ID, uid, &dto.TaskSessionPromptRequest{Prompt: "x"}); !errors.Is(err, common.ErrForbidden) {
			t.Errorf("%s prompt: expected ErrForbidden, got %v", who, err)
		}
		if err := f.svc.InterruptTaskSession(ctx, f.taskA.ID, uid); !errors.Is(err, common.ErrForbidden) {
			t.Errorf("%s interrupt: expected ErrForbidden, got %v", who, err)
		}
	}
	if len(f.resolved) != 0 {
		t.Errorf("no sandbox may be contacted for unauthorized callers, resolver calls: %v", f.resolved)
	}
}

func TestSessionTunnel_SandboxNotRunningIsReported(t *testing.T) {
	f := newTunnelFixture(t, true)
	f.resolveE = common.ErrServiceUnavailable
	ctx := context.Background()
	if err := f.svc.InterruptTaskSession(ctx, f.taskA.ID, f.memberA.ID); !errors.Is(err, common.ErrServiceUnavailable) {
		t.Errorf("expected the resolver error, got %v", err)
	}
	if _, err := f.svc.GetTaskSession(ctx, f.taskA.ID, f.viewerA.ID); !errors.Is(err, common.ErrServiceUnavailable) {
		t.Errorf("expected the resolver error, got %v", err)
	}
}

func TestSessionTunnel_TaskWithoutSessionNeverContactsASandbox(t *testing.T) {
	f := newTunnelFixture(t, false)
	ctx := context.Background()
	if err := f.svc.InterruptTaskSession(ctx, f.taskA.ID, f.memberA.ID); err == nil {
		t.Error("expected an error for a task with no session")
	}
	if len(f.resolved) != 0 {
		t.Errorf("resolver must not run without a session, got %v", f.resolved)
	}
}

func TestSessionTunnel_EmptyPromptRejectedBeforeAnySandboxCall(t *testing.T) {
	f := newTunnelFixture(t, true)
	err := f.svc.PromptTaskSession(context.Background(), f.taskA.ID, f.memberA.ID, &dto.TaskSessionPromptRequest{Prompt: "   "})
	if err == nil || len(f.resolved) != 0 {
		t.Errorf("expected rejection with no resolver call, err=%v resolved=%v", err, f.resolved)
	}
}
