package transfer_test

import (
	"context"
	"io"
	"testing"
	"time"

	"agent-task-manager/internal/git/transfer"
)

type mockVolumeHelper struct {
	applyCalled  bool
	exportCalled bool
	// syncAge, syncFound and syncErr answer LastSyncAge; provisioned records ProvisionTaskWorktree.
	syncAge     time.Duration
	syncFound   bool
	syncErr     error
	provisioned bool
}

func (m *mockVolumeHelper) LastSyncAge(ctx context.Context, repoID, baseRef string) (time.Duration, bool, error) {
	return m.syncAge, m.syncFound, m.syncErr
}

func (m *mockVolumeHelper) ProvisionTaskWorktree(ctx context.Context, repoID, taskID, refName string) error {
	m.provisioned = true
	return nil
}

func (m *mockVolumeHelper) ApplyBundleAndProvisionWorktree(ctx context.Context, repoID, taskID, refName string, bundleStream io.Reader) (string, error) {
	m.applyCalled = true
	_, _ = io.ReadAll(bundleStream)
	return "/workspaces/" + repoID + "/tasks/" + taskID, nil
}

func (m *mockVolumeHelper) ExportTaskBundle(ctx context.Context, repoID, taskID string, outStream io.Writer) error {
	m.exportCalled = true
	_, _ = outStream.Write([]byte("# v2 git bundle\n"))
	return nil
}

func (m *mockVolumeHelper) DiffTask(ctx context.Context, repoID, taskID, baseRef string, outStream io.Writer) error {
	return nil
}

type mockWorker struct {
	fetchCalled bool
	pushCalled  bool
}

func (m *mockWorker) FetchRemoteRefToBundle(ctx context.Context, remoteURL, refName, token string, outStream io.Writer) error {
	m.fetchCalled = true
	_, _ = outStream.Write([]byte("# v2 git bundle\n"))
	return nil
}

func (m *mockWorker) ImportBundleAndPush(ctx context.Context, remoteURL, taskID, token string, bundleStream io.Reader) error {
	m.pushCalled = true
	_, _ = io.ReadAll(bundleStream)
	return nil
}

func TestGitTransferManager_IngestAndPushRoundTrip(t *testing.T) {
	volHelper := &mockVolumeHelper{}
	worker := &mockWorker{}

	mgr := transfer.NewGitTransferManager(volHelper, worker)

	ctx := context.Background()
	// Test Ingestion
	ingestRes, err := mgr.IngestRemoteBranch(ctx, transfer.IngestRequest{
		ProjectID: "proj-1",
		RepoID:    "repo-1",
		TaskID:    "task-1",
		RemoteURL: "https://github.com/org/repo.git",
		BaseRef:   "refs/heads/main",
		AuthToken: "ghp_mock_token_123",
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	if !worker.fetchCalled || !volHelper.applyCalled {
		t.Errorf("expected worker fetch and helper apply to be called")
	}
	if ingestRes.TaskWorktreePath != "/workspaces/repo-1/tasks/task-1" {
		t.Errorf("unexpected worktree path: %s", ingestRes.TaskWorktreePath)
	}

	// Test Push
	_, err = mgr.ExportAndPushTask(ctx, transfer.PushRequest{
		ProjectID: "proj-1",
		RepoID:    "repo-1",
		TaskID:    "task-1",
		RemoteURL: "https://github.com/org/repo.git",
		AuthToken: "ghp_mock_token_123",
	})
	if err != nil {
		t.Fatalf("push failed: %v", err)
	}
	if !volHelper.exportCalled || !worker.pushCalled {
		t.Errorf("expected helper export and worker push to be called")
	}

	// Test Ingestion Validation Failures
	_, err = mgr.IngestRemoteBranch(ctx, transfer.IngestRequest{
		RepoID:    "bad/repo",
		TaskID:    "task-1",
		BaseRef:   "refs/heads/main",
		AuthToken: "token",
	})
	if err == nil {
		t.Errorf("expected error for invalid repo ID")
	}

	_, err = mgr.IngestRemoteBranch(ctx, transfer.IngestRequest{
		RepoID:    "repo-1",
		TaskID:    "../task",
		BaseRef:   "refs/heads/main",
		AuthToken: "token",
	})
	if err == nil {
		t.Errorf("expected error for invalid task ID")
	}

	_, err = mgr.IngestRemoteBranch(ctx, transfer.IngestRequest{
		RepoID:    "repo-1",
		TaskID:    "task-1",
		BaseRef:   "--upload-pack=evil",
		AuthToken: "token",
	})
	if err == nil {
		t.Errorf("expected error for invalid ref name")
	}

	_, err = mgr.IngestRemoteBranch(ctx, transfer.IngestRequest{
		RepoID:    "repo-1",
		TaskID:    "task-1",
		BaseRef:   "refs/heads/main",
		AuthToken: "",
	})
	if err == nil {
		t.Errorf("expected error for empty auth token")
	}

	// Test Push Validation Failures
	_, err = mgr.ExportAndPushTask(ctx, transfer.PushRequest{
		RepoID:    "bad/repo",
		TaskID:    "task-1",
		AuthToken: "token",
	})
	if err == nil {
		t.Errorf("expected error for invalid repo ID in push")
	}

	_, err = mgr.ExportAndPushTask(ctx, transfer.PushRequest{
		RepoID:    "repo-1",
		TaskID:    "../task",
		AuthToken: "token",
	})
	if err == nil {
		t.Errorf("expected error for invalid task ID in push")
	}

	_, err = mgr.ExportAndPushTask(ctx, transfer.PushRequest{
		RepoID:    "repo-1",
		TaskID:    "task-1",
		AuthToken: "",
	})
	if err == nil {
		t.Errorf("expected error for empty auth token in push")
	}
}

func (m *mockVolumeHelper) RemoveTaskWorkspace(ctx context.Context, repoID, taskID string) error {
	return nil
}

func (m *mockVolumeHelper) ListWorkspaceFiles(ctx context.Context, repoID, taskID string) (transfer.WorkspaceListing, error) {
	return transfer.WorkspaceListing{Source: "workspace"}, nil
}

func (m *mockVolumeHelper) ReadWorkspaceFile(ctx context.Context, repoID, taskID, relPath string, maxBytes int64) (*transfer.WorkspaceFile, error) {
	return &transfer.WorkspaceFile{Path: relPath, Missing: true}, nil
}
