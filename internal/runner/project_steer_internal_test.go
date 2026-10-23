package runner

import (
	"context"
	"strings"
	"testing"
	"time"
)

func (s *ptrSandbox) GetProjectRoute(context.Context, string) (*ProjectRouteHandle, bool, error) {
	if s.noRoute {
		return nil, false, nil
	}
	return &ProjectRouteHandle{ProjectID: "p1", ContainerID: "c1", EndpointURL: "http://127.0.0.1:4096", AuthPassword: "pw"}, true, nil
}

// steerOC reports the task worktree as the session directory.
type steerOC struct{ *ptrOC }

func (o *steerOC) GetSession(ctx context.Context, id string) (*OpenCodeSessionDTO, error) {
	s, _ := o.ptrOC.GetSession(ctx, id)
	s.Directory = "/workspaces/p1/tasks/t1"
	return s, nil
}

// steer prompts the session as the service would and then finalizes the turn.
func steer(t *testing.T, f *ptrFixture) (*SteerTurnResult, error, []string) {
	t.Helper()
	oc := &steerOC{f.oc}
	f.r.projectClientFor = func(string, string) (OpenCodeClient, error) { return oc, nil }
	sid := "ses_1"
	f.task.SessionID = &sid
	before := f.oc.updated
	if err := oc.PromptSession(context.Background(), sid, "steer"); err != nil {
		t.Fatal(err)
	}
	var logs []string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := f.r.FinalizeSteeredTurn(ctx, f.task, &before, func(s string) { logs = append(logs, s) })
	return res, err, logs
}

func TestFinalizeSteeredTurn_CommitsAndPushes(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, nil, true)
	res, err, _ := steer(t, f)
	if err != nil || !res.Committed || !res.Pushed || res.Files != 2 || res.Additions != 3 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if !f.dk.ran("commit -q -m Add thing (steered)") || f.tm.push == nil {
		t.Fatalf("expected commit and push, got %v", f.dk.argvs())
	}
	for _, c := range f.dk.argvs() {
		if strings.HasPrefix(c, "git ") && !strings.Contains(c, "core.hooksPath=/dev/null") {
			t.Fatalf("git call without hardening: %s", c)
		}
	}
	for _, w := range f.dk.workdir {
		if w != "/workspaces/p1/tasks/t1" {
			t.Fatalf("exec ran outside the task worktree: %q", w)
		}
	}
	if f.sb.called != 0 {
		t.Fatal("a steer must never provision a container")
	}
}

func TestFinalizeSteeredTurn_NoChangesCommitsNothing(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, nil, false)
	res, err, _ := steer(t, f)
	if err != nil || res.Committed || f.dk.ran("commit") || f.tm.push != nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestFinalizeSteeredTurn_AutoPushOffCommitsLocallyOnly(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, nil, true)
	f.r.integRepo = &ptrIntegRepo{list: f.r.integRepo.(*ptrIntegRepo).list}
	f.r.integRepo.(*ptrIntegRepo).list[0].Config = map[string]any{"auto_pr": false}
	res, err, _ := steer(t, f)
	if err != nil || !res.Committed || res.Pushed || f.tm.push != nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestFinalizeSteeredTurn_FailedTurnDoesNotCommit(t *testing.T) {
	f := newPtrFixture(t, []string{"failed"}, nil, true)
	_, err, _ := steer(t, f)
	if err == nil || f.dk.ran("add -A") || f.tm.push != nil {
		t.Fatalf("err=%v calls=%v", err, f.dk.argvs())
	}
}

func TestFinalizeSteeredTurn_NoRunningContainer(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, nil, true)
	f.sb.noRoute = true
	_, err, _ := steer(t, f)
	if err == nil || f.sb.called != 0 || f.dk.ran("add -A") {
		t.Fatalf("err=%v", err)
	}
}
