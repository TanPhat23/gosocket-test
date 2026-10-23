package transfer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agent-task-manager/internal/git/transfer"
)

func syncRequest() transfer.IngestRequest {
	return transfer.IngestRequest{
		ProjectID: "repo-1", RepoID: "repo-1", TaskID: "task-1",
		RemoteURL: "https://github.com/owner/repo.git", BaseRef: "refs/heads/main", AuthToken: "tok",
	}
}

// A base fetched moments ago is reused: the worker is not started and the task only gets its worktree.
func TestIngest_ReusesAFreshBaseWithoutFetching(t *testing.T) {
	vh := &mockVolumeHelper{syncAge: time.Minute, syncFound: true}
	w := &mockWorker{}
	mgr := transfer.NewGitTransferManager(vh, w)

	res, err := mgr.IngestRemoteBranch(context.Background(), syncRequest())
	if err != nil {
		t.Fatalf("IngestRemoteBranch: %v", err)
	}
	if w.fetchCalled {
		t.Errorf("a fresh base must not be fetched from the remote again")
	}
	if vh.applyCalled || !vh.provisioned {
		t.Errorf("expected only worktree provisioning, got apply=%v provision=%v", vh.applyCalled, vh.provisioned)
	}
	if res.TaskWorktreePath != "/workspaces/repo-1/tasks/task-1" {
		t.Errorf("unexpected worktree path %q", res.TaskWorktreePath)
	}
}

func TestIngest_FetchesWhenTheBaseIsStale(t *testing.T) {
	vh := &mockVolumeHelper{syncAge: 10 * time.Minute, syncFound: true}
	w := &mockWorker{}
	mgr := transfer.NewGitTransferManager(vh, w)

	if _, err := mgr.IngestRemoteBranch(context.Background(), syncRequest()); err != nil {
		t.Fatalf("IngestRemoteBranch: %v", err)
	}
	if !w.fetchCalled || !vh.applyCalled || vh.provisioned {
		t.Errorf("a stale base must be fetched: fetch=%v apply=%v provision=%v", w.fetchCalled, vh.applyCalled, vh.provisioned)
	}
}

func TestIngest_FetchesWhenTheBaseIsMissing(t *testing.T) {
	vh := &mockVolumeHelper{syncFound: false}
	w := &mockWorker{}
	mgr := transfer.NewGitTransferManager(vh, w)

	if _, err := mgr.IngestRemoteBranch(context.Background(), syncRequest()); err != nil {
		t.Fatalf("IngestRemoteBranch: %v", err)
	}
	if !w.fetchCalled || vh.provisioned {
		t.Errorf("a missing base must be fetched: fetch=%v provision=%v", w.fetchCalled, vh.provisioned)
	}
}

// A failed sync check is not trusted: the full fetch runs rather than the request failing.
func TestIngest_FetchesWhenTheSyncCheckFails(t *testing.T) {
	vh := &mockVolumeHelper{syncAge: time.Second, syncFound: true, syncErr: errors.New("docker unavailable")}
	w := &mockWorker{}
	mgr := transfer.NewGitTransferManager(vh, w)

	if _, err := mgr.IngestRemoteBranch(context.Background(), syncRequest()); err != nil {
		t.Fatalf("a failed sync check must fall back to the fetch: %v", err)
	}
	if !w.fetchCalled || vh.provisioned {
		t.Errorf("expected the fetch fallback: fetch=%v provision=%v", w.fetchCalled, vh.provisioned)
	}
}
