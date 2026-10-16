package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-task-manager/internal/git/transfer"
	"agent-task-manager/internal/sandbox"
)

type fakeTransferMgr struct {
	ingest *transfer.IngestRequest
	push   *transfer.PushRequest
	err    error
	// worktree overrides the returned task worktree path (live probes).
	worktree string
}

func (f *fakeTransferMgr) IngestRemoteBranch(_ context.Context, r transfer.IngestRequest) (*transfer.TransferResult, error) {
	f.ingest = &r
	if f.err != nil {
		return nil, f.err
	}
	if f.worktree != "" {
		return &transfer.TransferResult{TaskWorktreePath: f.worktree}, nil
	}
	return &transfer.TransferResult{TaskWorktreePath: "/workspaces/" + r.RepoID + "/tasks/" + r.TaskID}, nil
}

func (f *fakeTransferMgr) ExportAndPushTask(_ context.Context, r transfer.PushRequest) (*transfer.TransferResult, error) {
	f.push = &r
	return &transfer.TransferResult{}, f.err
}

func TestTaskRepoTransfer_IngestUsesProjectIDAsVolumeKey(t *testing.T) {
	f := &fakeTransferMgr{}
	path, err := NewTaskRepoTransfer(f).IngestTaskBranch(context.Background(), "proj-1", "task-9", "org/repo", "main", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if f.ingest.RepoID != "proj-1" || f.ingest.ProjectID != "proj-1" {
		t.Fatalf("RepoID must be the project ID so the sandbox mounts the same volume, got %+v", f.ingest)
	}
	if f.ingest.RemoteURL != "https://github.com/org/repo.git" || f.ingest.BaseRef != "refs/heads/main" || f.ingest.AuthToken != "tok" {
		t.Fatalf("unexpected request %+v", f.ingest)
	}
	if path != "/workspaces/proj-1/tasks/task-9" {
		t.Fatalf("path %q", path)
	}
}

func TestTaskRepoTransfer_RejectsSlugsThatChangeTheHost(t *testing.T) {
	for _, slug := range []string{"", "repo", "org/", "/repo", "../x/y", "org/repo/extra", "evil.com@org/repo", "org/repo?x=1", "org/repo#f", "org\\repo", "a b/c", "org/repo\n", "https://evil.com/x", "org/..", "org/."} {
		f := &fakeTransferMgr{}
		if _, err := NewTaskRepoTransfer(f).IngestTaskBranch(context.Background(), "p", "t", slug, "main", "tok"); err == nil {
			t.Errorf("slug %q accepted", slug)
		}
		if err := NewTaskRepoTransfer(f).PushTaskBranch(context.Background(), "p", "t", slug, "tok"); err == nil {
			t.Errorf("push slug %q accepted", slug)
		}
		if f.ingest != nil || f.push != nil {
			t.Errorf("slug %q reached the transfer manager", slug)
		}
	}
}

func TestTaskRepoTransfer_PushAndErrorsDoNotLeakToken(t *testing.T) {
	f := &fakeTransferMgr{}
	if err := NewTaskRepoTransfer(f).PushTaskBranch(context.Background(), "p", "t1", "org/repo", "tok"); err != nil {
		t.Fatal(err)
	}
	if f.push.RepoID != "p" || f.push.RemoteURL != "https://github.com/org/repo.git" {
		t.Fatalf("unexpected push %+v", f.push)
	}
	f.err = transfer.ErrPushRejected
	err := NewTaskRepoTransfer(f).PushTaskBranch(context.Background(), "p", "t1", "org/repo", "SECRETTOKEN")
	if !errors.Is(err, transfer.ErrPushRejected) || strings.Contains(err.Error(), "SECRETTOKEN") {
		t.Fatalf("want wrapped ErrPushRejected without the token, got %v", err)
	}
}

func TestTaskRepoTransferFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	pinned := "alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6"
	cli := sandbox.NewMockDockerClient()
	for _, off := range []string{"", "false"} {
		if got, err := TaskRepoTransferFromEnv(env(map[string]string{"ATM_GIT_TRANSFER_ENABLED": off}), cli); got != nil || err != nil {
			t.Errorf("flag %q: want nil,nil got %v,%v", off, got, err)
		}
	}
	bad := []map[string]string{
		{"ATM_GIT_TRANSFER_ENABLED": "yes", "ATM_GIT_TRANSFER_IMAGE": pinned},
		{"ATM_GIT_TRANSFER_ENABLED": "true"},
		{"ATM_GIT_TRANSFER_ENABLED": "true", "ATM_GIT_TRANSFER_IMAGE": "atm-helper:latest"},
		{"ATM_GIT_TRANSFER_ENABLED": "true", "ATM_GIT_TRANSFER_IMAGE": "alpine:3.24@sha256:" + PendingSHA256},
	}
	for _, m := range bad {
		if got, err := TaskRepoTransferFromEnv(env(m), cli); got != nil || err == nil {
			t.Errorf("%v: want error, got %v,%v", m, got, err)
		}
	}
	if got, err := TaskRepoTransferFromEnv(env(map[string]string{"ATM_GIT_TRANSFER_ENABLED": "true", "ATM_GIT_TRANSFER_IMAGE": pinned}), nil); got != nil || err == nil {
		t.Errorf("nil client: want error, got %v,%v", got, err)
	}
	if got, err := TaskRepoTransferFromEnv(env(map[string]string{"ATM_GIT_TRANSFER_ENABLED": "true", "ATM_GIT_TRANSFER_IMAGE": pinned}), cli); got == nil || err != nil {
		t.Errorf("pinned image: want adapter, got %v,%v", got, err)
	}
}
