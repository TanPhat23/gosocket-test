package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-task-manager/internal/git/transfer"
)

type fakeTransferMgr struct {
	ingest *transfer.IngestRequest
	push   *transfer.PushRequest
	err    error
}

func (f *fakeTransferMgr) IngestRemoteBranch(_ context.Context, r transfer.IngestRequest) (*transfer.TransferResult, error) {
	f.ingest = &r
	if f.err != nil {
		return nil, f.err
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
