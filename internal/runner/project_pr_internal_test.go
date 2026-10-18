package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agent-task-manager/internal/common"
)

// prFixture is a finished task in review: its session is bound to the task worktree and nothing is pushed yet.
func prFixture(t *testing.T) *ptrFixture {
	t.Helper()
	f := newPtrFixture(t, []string{"succeeded"}, nil, true)
	f.r.projectClientFor = func(string, string) (OpenCodeClient, error) { return &steerOC{f.oc}, nil }
	sid := "ses_1"
	f.task.SessionID = &sid
	return f
}

func prCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPublishTaskPullRequest_PushesTheBranchAndOpensThePR(t *testing.T) {
	f := prFixture(t)
	var logs []string
	url, err := f.r.PublishTaskPullRequest(prCtx(t), f.task, func(s string) { logs = append(logs, s) })
	if err != nil || url != "https://example.invalid/pr/1" {
		t.Fatalf("url=%q err=%v", url, err)
	}
	if f.tm.push == nil || f.tm.push.TaskID != "t1" || f.tm.push.AuthToken != "ghp_testtoken" ||
		f.tm.push.RemoteURL != "https://github.com/org/repo.git" {
		t.Fatalf("push %+v", f.tm.push)
	}
	if f.pr.n != 1 || f.pr.branch != "task/t1" {
		t.Fatalf("PR calls=%d branch=%q", f.pr.n, f.pr.branch)
	}
	if f.task.PRURL == nil || *f.task.PRURL != url || f.task.PullRequestURL == nil || *f.task.PullRequestURL != url {
		t.Fatalf("task was not given the PR url: %+v", f.task)
	}
	if !strings.Contains(strings.Join(logs, ""), url) {
		t.Fatalf("PR url missing from the log: %q", logs)
	}
}

func TestPublishTaskPullRequest_CountsChangesAgainstTheBaseBranchInTheWorktree(t *testing.T) {
	f := prFixture(t)
	if _, err := f.r.PublishTaskPullRequest(prCtx(t), f.task, nil); err != nil {
		t.Fatal(err)
	}
	if !f.dk.ran("diff --no-ext-diff --no-textconv --numstat main...HEAD") {
		t.Fatalf("expected a numstat against the base branch, got %v", f.dk.argvs())
	}
	for i, c := range f.dk.argvs() {
		if strings.Contains(c, "git ") && !strings.Contains(c, "core.hooksPath=/dev/null") {
			t.Fatalf("git call without hardening: %s", c)
		}
		if w := f.dk.workdir[i]; w != "/workspaces/p1/tasks/t1" {
			t.Fatalf("exec ran outside the task worktree: %q", w)
		}
	}
	if f.sb.called != 0 {
		t.Fatal("publishing must never provision a container")
	}
}

func TestPublishTaskPullRequest_ExistingPRIsReturnedWithoutPushing(t *testing.T) {
	f := prFixture(t)
	existing := "https://example.invalid/pr/9"
	f.task.PRURL = &existing
	url, err := f.r.PublishTaskPullRequest(prCtx(t), f.task, nil)
	if err != nil || url != existing || f.tm.push != nil || f.pr.n != 0 {
		t.Fatalf("url=%q err=%v push=%v prs=%d", url, err, f.tm.push, f.pr.n)
	}
}

func TestPublishTaskPullRequest_StillOpensThePRWhenTheContainerIsGone(t *testing.T) {
	f := prFixture(t)
	f.sb.noRoute = true
	if _, err := f.r.PublishTaskPullRequest(prCtx(t), f.task, nil); err != nil {
		t.Fatal(err)
	}
	if f.tm.push == nil || f.pr.n != 1 {
		t.Fatalf("push=%v prs=%d", f.tm.push, f.pr.n)
	}
}

func TestPublishTaskPullRequest_PushFailureOpensNoPR(t *testing.T) {
	f := prFixture(t)
	f.tm.err = errors.New("boom")
	_, err := f.r.PublishTaskPullRequest(prCtx(t), f.task, nil)
	if err == nil || f.pr.n != 0 || f.task.PRURL != nil {
		t.Fatalf("err=%v prs=%d", err, f.pr.n)
	}
}

func TestPublishTaskPullRequest_RefusesWithoutAGitHubIntegration(t *testing.T) {
	f := prFixture(t)
	f.r.integRepo = &ptrIntegRepo{}
	_, err := f.r.PublishTaskPullRequest(prCtx(t), f.task, nil)
	if err == nil || f.tm.push != nil || f.pr.n != 0 {
		t.Fatalf("err=%v push=%v prs=%d", err, f.tm.push, f.pr.n)
	}
}

func TestPublishTaskPullRequest_RefusesWhenNothingCanPush(t *testing.T) {
	f := prFixture(t)
	f.r.repoTransfer = nil
	if _, err := f.r.PublishTaskPullRequest(prCtx(t), f.task, nil); err == nil || f.pr.n != 0 {
		t.Fatalf("err=%v prs=%d", err, f.pr.n)
	}
	f = prFixture(t)
	f.r.ghProvider = nil
	if _, err := f.r.PublishTaskPullRequest(prCtx(t), f.task, nil); err == nil || f.tm.push != nil {
		t.Fatalf("err=%v push=%v", err, f.tm.push)
	}
}

// Without a transfer manager the diff is read in the project container (the fallback path).
func TestTaskDiff_ReadsTheDiffAgainstTheBaseBranchInTheWorktree(t *testing.T) {
	f := prFixture(t)
	f.r.repoTransfer = nil
	f.dk.handler = func(argv []string) (int, string) {
		if strings.Contains(strings.Join(argv, " "), "--no-color main...HEAD") {
			return 0, "diff --git a/a.go b/a.go\n+x\n"
		}
		return 0, ""
	}
	got, err := f.r.TaskDiff(prCtx(t), f.task)
	if err != nil || got != "diff --git a/a.go b/a.go\n+x\n" {
		t.Fatalf("diff=%q err=%v", got, err)
	}
	for i, c := range f.dk.argvs() {
		if strings.Contains(c, "git ") && !strings.Contains(c, "core.hooksPath=/dev/null") {
			t.Fatalf("git call without hardening: %s", c)
		}
		if w := f.dk.workdir[i]; w != "/workspaces/p1/tasks/t1" {
			t.Fatalf("exec ran outside the task worktree: %q", w)
		}
	}
	if f.sb.called != 0 {
		t.Fatal("reading a diff must never provision a container")
	}
}

func TestTaskDiff_UnavailableWithoutARunningContainerOnTheFallbackPath(t *testing.T) {
	f := prFixture(t)
	f.r.repoTransfer = nil
	f.sb.noRoute = true
	_, err := f.r.TaskDiff(prCtx(t), f.task)
	if !errors.Is(err, common.ErrServiceUnavailable) || f.dk.ran("diff") {
		t.Fatalf("err=%v calls=%v", err, f.dk.argvs())
	}
}

// The diff is read from the project volume through the transfer helper, so it needs no project container.
func TestTaskDiff_ReadsFromTheVolumeWithTheBaseBranch(t *testing.T) {
	f := prFixture(t)
	f.tm.diffOut = "diff --git a/a.go b/a.go\n+x\n"
	got, err := f.r.TaskDiff(prCtx(t), f.task)
	if err != nil || got != "diff --git a/a.go b/a.go\n+x\n" {
		t.Fatalf("diff=%q err=%v", got, err)
	}
	if f.tm.diff == nil || f.tm.diff.TaskID != "t1" || f.tm.diff.RepoID != "p1" || f.tm.diff.BaseRef != "refs/heads/main" {
		t.Fatalf("diff request %+v", f.tm.diff)
	}
	if len(f.dk.argvs()) != 0 || f.sb.called != 0 {
		t.Fatalf("volume diff must not touch a container: exec=%v provisioned=%d", f.dk.argvs(), f.sb.called)
	}
}

// After a control-plane restart the project container is gone, but the diff still reads from the volume.
func TestTaskDiff_SurvivesAMissingProjectContainer(t *testing.T) {
	f := prFixture(t)
	f.tm.diffOut = "diff --git a/a.go b/a.go\n+x\n"
	f.sb.noRoute = true
	got, err := f.r.TaskDiff(prCtx(t), f.task)
	if err != nil || got != "diff --git a/a.go b/a.go\n+x\n" {
		t.Fatalf("diff=%q err=%v", got, err)
	}
}

func TestTaskDiff_TruncatesToTheBroadcastCap(t *testing.T) {
	f := prFixture(t)
	f.tm.diffOut = strings.Repeat("x", diffBroadcastCap+4096)
	got, err := f.r.TaskDiff(prCtx(t), f.task)
	if err != nil || len(got) != diffBroadcastCap {
		t.Fatalf("len=%d err=%v, want %d", len(got), err, diffBroadcastCap)
	}
}

func TestTaskDiff_EmptyForATaskThatNeverStarted(t *testing.T) {
	f := prFixture(t)
	f.task.SessionID = nil
	got, err := f.r.TaskDiff(prCtx(t), f.task)
	if err != nil || got != "" || len(f.dk.argvs()) != 0 {
		t.Fatalf("diff=%q err=%v", got, err)
	}
}
