package transfer

import (
	"context"
	"errors"
	"io"
)

var (
	ErrInvalidIdentifier     = errors.New("invalid identifier: must be alphanumeric, hyphen, or underscore")
	ErrInvalidRefName        = errors.New("invalid ref name: ref contains illegal git characters or flags")
	ErrSubmodulesNotSupported = errors.New("repository contains submodules, which are not supported in v1")
	ErrGitLFSNotSupported     = errors.New("repository contains Git LFS pointers, which are not supported in v1")
	ErrVolumeNotWritable      = errors.New("project volume is not writable by the helper (image must ship /workspaces owned by 1000:1000)")
	ErrGitTransferFailed      = errors.New("git transfer operation failed")
	ErrCanaryLeakDetected     = errors.New("security violation: canary credential leak detected")
	ErrPushRejected           = errors.New("upstream git push was rejected")
)

// GitTransferManager orchestrates credential-safe Git data transfer between remote and volume.
type GitTransferManager interface {
	// IngestRemoteBranch clones or fetches a remote ref into the project volume bare repository,
	// validates absence of submodules and LFS, and creates a no-checkout task worktree.
	IngestRemoteBranch(ctx context.Context, req IngestRequest) (*TransferResult, error)

	// ExportAndPushTask exports a task branch from the project volume as a bundle,
	// relays it to the credentialed worker, and pushes upstream to GitHub.
	ExportAndPushTask(ctx context.Context, req PushRequest) (*TransferResult, error)
}

// IngestRequest contains parameters for cloning/fetching into a project data volume.
type IngestRequest struct {
	ProjectID  string
	RepoID     string
	TaskID     string
	RemoteURL  string
	BaseRef    string // e.g. "refs/heads/main"
	AuthToken  string // Plaintext PAT decrypted in memory; never committed to disk
}

// PushRequest contains parameters for exporting a task branch bundle and pushing upstream.
type PushRequest struct {
	ProjectID string
	RepoID    string
	TaskID    string
	RemoteURL string
	AuthToken string
}

// TransferResult summarizes the completed transfer operation.
type TransferResult struct {
	TaskWorktreePath string
	HeadCommit       string
	BytesTransferred int64
}

// VolumeHelper defines operations executed within the non-networked volume helper container.
type VolumeHelper interface {
	ApplyBundleAndProvisionWorktree(ctx context.Context, repoID, taskID, refName string, bundleStream io.Reader) (string, error)
	ExportTaskBundle(ctx context.Context, repoID, taskID string, outStream io.Writer) error
}

// CredentialedWorker defines operations executed within the ephemeral networked worker container.
type CredentialedWorker interface {
	FetchRemoteRefToBundle(ctx context.Context, remoteURL, refName, token string, outStream io.Writer) error
	ImportBundleAndPush(ctx context.Context, remoteURL, taskID, token string, bundleStream io.Reader) error
}
