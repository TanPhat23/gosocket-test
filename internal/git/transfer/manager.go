package transfer

import (
	"context"
	"fmt"
	"io"
	"sync"
)

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
