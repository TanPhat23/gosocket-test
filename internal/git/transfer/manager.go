package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// baseFreshWindow is how recently the project must have been fetched for a new task to reuse that base instead of
// fetching it again. A task started inside the window starts from a base up to this old.
const baseFreshWindow = 5 * time.Minute

type gitTransferManager struct {
	volumeHelper VolumeHelper
	worker       CredentialedWorker
}

// NewGitTransferManager creates a new GitTransferManager orchestrating the two containers.
func NewGitTransferManager(volHelper VolumeHelper, worker CredentialedWorker) GitTransferManager {
	return &gitTransferManager{
		volumeHelper: volHelper,
		worker:       worker,
	}
}

func (m *gitTransferManager) IngestRemoteBranch(ctx context.Context, req IngestRequest) (*TransferResult, error) {
	if err := ValidateRepoID(req.RepoID); err != nil {
		return nil, err
	}
	if err := ValidateTaskID(req.TaskID); err != nil {
		return nil, err
	}
	if err := ValidateRefName(req.BaseRef); err != nil {
		return nil, err
	}
	if req.AuthToken == "" {
		return nil, fmt.Errorf("auth token is required")
	}

	// A base fetched moments ago is already in the project repository: only the task worktree is needed. Any doubt
	// (a stale stamp, a missing ref, or a failed check) falls through to the full fetch below.
	if age, found, err := m.volumeHelper.LastSyncAge(ctx, req.RepoID, req.BaseRef); err == nil && found && age < baseFreshWindow {
		if err := m.volumeHelper.ProvisionTaskWorktree(ctx, req.RepoID, req.TaskID, req.BaseRef); err != nil {
			return nil, fmt.Errorf("provision worktree from synced base: %w", err)
		}
		return &TransferResult{
			TaskWorktreePath: fmt.Sprintf("/workspaces/%s/tasks/%s", req.RepoID, req.TaskID),
		}, nil
	}

	// Direct in-memory stream pipe between worker and helper
	pr, pw := io.Pipe()

	var (
		worktreePath string
		helperErr    error
		workerErr    error
		wg           sync.WaitGroup
	)

	wg.Add(2)

	// Worker fetches bundle to pipe writer
	go func() {
		defer wg.Done()
		defer pw.Close()
		workerErr = m.worker.FetchRemoteRefToBundle(ctx, req.RemoteURL, req.BaseRef, req.AuthToken, pw)
		if workerErr != nil {
			_ = pw.CloseWithError(workerErr)
		}
	}()

	// Volume helper reads bundle from pipe reader
	go func() {
		defer wg.Done()
		defer pr.Close()
		worktreePath, helperErr = m.volumeHelper.ApplyBundleAndProvisionWorktree(ctx, req.RepoID, req.TaskID, req.BaseRef, pr)
		if helperErr != nil {
			_ = pr.CloseWithError(helperErr)
		}
	}()

	wg.Wait()

	if helperErr != nil && workerErr != nil && errors.Is(workerErr, io.ErrClosedPipe) {
		return nil, fmt.Errorf("volume helper apply failed: %w", helperErr)
	}

	if workerErr != nil {
		return nil, fmt.Errorf("worker fetch failed: %w", workerErr)
	}
	if helperErr != nil {
		return nil, fmt.Errorf("volume helper apply failed: %w", helperErr)
	}

	return &TransferResult{
		TaskWorktreePath: worktreePath,
	}, nil
}

func (m *gitTransferManager) ExportAndPushTask(ctx context.Context, req PushRequest) (*TransferResult, error) {
	if err := ValidateRepoID(req.RepoID); err != nil {
		return nil, err
	}
	if err := ValidateTaskID(req.TaskID); err != nil {
		return nil, err
	}
	if req.AuthToken == "" {
		return nil, fmt.Errorf("auth token is required")
	}

	// Direct in-memory stream pipe from helper export to worker push
	pr, pw := io.Pipe()

	var (
		helperErr error
		workerErr error
		wg        sync.WaitGroup
	)

	wg.Add(2)

	go func() {
		defer wg.Done()
		defer pw.Close()
		helperErr = m.volumeHelper.ExportTaskBundle(ctx, req.RepoID, req.TaskID, pw)
		if helperErr != nil {
			_ = pw.CloseWithError(helperErr)
		}
	}()

	go func() {
		defer wg.Done()
		defer pr.Close()
		workerErr = m.worker.ImportBundleAndPush(ctx, req.RemoteURL, req.TaskID, req.AuthToken, pr)
		if workerErr != nil {
			_ = pr.CloseWithError(workerErr)
		}
	}()

	wg.Wait()

	if helperErr != nil {
		return nil, fmt.Errorf("helper bundle export failed: %w", helperErr)
	}
	if workerErr != nil {
		return nil, fmt.Errorf("worker push failed: %w", workerErr)
	}

	return &TransferResult{}, nil
}

// DiffTask validates the request and streams the task's diff from the volume helper.
func (m *gitTransferManager) DiffTask(ctx context.Context, req DiffRequest, outStream io.Writer) error {
	if err := ValidateRepoID(req.RepoID); err != nil {
		return err
	}
	if err := ValidateTaskID(req.TaskID); err != nil {
		return err
	}
	if err := ValidateRefName(req.BaseRef); err != nil {
		return err
	}
	if err := m.volumeHelper.DiffTask(ctx, req.RepoID, req.TaskID, req.BaseRef, outStream); err != nil {
		return fmt.Errorf("helper diff failed: %w", err)
	}
	return nil
}

// RemoveTaskDependencies validates the request and deletes the task's node_modules from the volume.
func (m *gitTransferManager) RemoveTaskDependencies(ctx context.Context, req WorkspaceRequest) error {
	if err := ValidateRepoID(req.RepoID); err != nil {
		return err
	}
	if err := ValidateTaskID(req.TaskID); err != nil {
		return err
	}
	if err := m.volumeHelper.RemoveTaskDependencies(ctx, req.RepoID, req.TaskID); err != nil {
		return fmt.Errorf("helper dependency removal failed: %w", err)
	}
	return nil
}

// RemoveTaskWorkspace validates the request and deletes the task's worktree from the volume.
func (m *gitTransferManager) RemoveTaskWorkspace(ctx context.Context, req WorkspaceRequest) error {
	if err := ValidateRepoID(req.RepoID); err != nil {
		return err
	}
	if err := ValidateTaskID(req.TaskID); err != nil {
		return err
	}
	if err := m.volumeHelper.RemoveTaskWorkspace(ctx, req.RepoID, req.TaskID); err != nil {
		return fmt.Errorf("helper workspace removal failed: %w", err)
	}
	return nil
}
