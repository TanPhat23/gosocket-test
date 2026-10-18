package transfer

import (
	"context"
	"errors"
	"io"
	"os"
	"time"
)

func defaultSecurityOpt() []string {
	if os.Getenv("ATM_DISABLE_NO_NEW_PRIVS") == "true" || os.Getenv("ATM_DEV_HOST_AUTH") == "true" {
		return nil
	}
	return []string{"no-new-privileges:true"}
}

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

	// DiffTask writes the task's diff against its pinned base commit, read from the project volume, to outStream.
	DiffTask(ctx context.Context, req DiffRequest, outStream io.Writer) error

	// RemoveTaskWorkspace deletes the task's worktree (checkout and dependencies) from the project volume. The
	// task branch and its base pin stay in the shared repository.
	RemoveTaskWorkspace(ctx context.Context, req WorkspaceRequest) error

	// ListWorkspaceFiles lists the files of a task's workspace (its working tree, or its committed branch).
	ListWorkspaceFiles(ctx context.Context, req WorkspaceRequest) (WorkspaceListing, error)
	// ReadWorkspaceFile reads one file of a task's workspace, capped at MaxBytes.
	ReadWorkspaceFile(ctx context.Context, req WorkspaceFileRequest) (*WorkspaceFile, error)
}

// DiffRequest names a task whose diff is read from the project volume.
type DiffRequest struct {
	ProjectID string
	RepoID    string // names the project data volume: must be the sandbox project ID (sandbox.ProjectDataVolumeName)
	TaskID    string
	BaseRef   string // fallback base for tasks provisioned before the base pin, e.g. "refs/heads/main"
}

// WorkspaceRequest names a task whose worktree is removed from the project volume.
// WorkspaceListing is the file list of a task workspace. Source is "workspace" for a live worktree or "committed"
// for the task's branch after its worktree was removed.
type WorkspaceListing struct {
	Source string
	Files  []string
}

// WorkspaceFile is one file read from a task workspace. Missing and Denied report paths that do not name a file
// inside the workspace. Binary files carry no Content.
type WorkspaceFile struct {
	Path      string
	Source    string
	Size      int64
	Content   []byte
	Truncated bool
	Binary    bool
	Missing   bool
	Denied    bool
}

// WorkspaceFileRequest names one file of a task workspace to read.
type WorkspaceFileRequest struct {
	ProjectID string
	RepoID    string // names the project data volume: must be the sandbox project ID (sandbox.ProjectDataVolumeName)
	TaskID    string
	Path      string
	MaxBytes  int64
}

// WorkspaceRequest names a task whose worktree is removed from the project volume.
type WorkspaceRequest struct {
	ProjectID string
	RepoID    string // names the project data volume: must be the sandbox project ID (sandbox.ProjectDataVolumeName)
	TaskID    string
}

// IngestRequest contains parameters for cloning/fetching into a project data volume.
type IngestRequest struct {
	ProjectID  string
	RepoID     string // names the project data volume: must be the sandbox project ID (sandbox.ProjectDataVolumeName)
	TaskID     string
	RemoteURL  string
	BaseRef    string // e.g. "refs/heads/main"
	AuthToken  string // Plaintext PAT decrypted in memory; never committed to disk
}

// PushRequest contains parameters for exporting a task branch bundle and pushing upstream.
type PushRequest struct {
	ProjectID string
	RepoID    string // names the project data volume: must be the sandbox project ID (sandbox.ProjectDataVolumeName)
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
	DiffTask(ctx context.Context, repoID, taskID, baseRef string, outStream io.Writer) error
	RemoveTaskWorkspace(ctx context.Context, repoID, taskID string) error

	// LastSyncAge reports how long ago the project was last fetched; found is false when the base must be fetched.
	LastSyncAge(ctx context.Context, repoID, baseRef string) (age time.Duration, found bool, err error)
	// ProvisionTaskWorktree adds a task worktree from a base ref already in the project repository.
	ProvisionTaskWorktree(ctx context.Context, repoID, taskID, refName string) error
	// ListWorkspaceFiles and ReadWorkspaceFile are the read-only views of a task workspace.
	ListWorkspaceFiles(ctx context.Context, repoID, taskID string) (WorkspaceListing, error)
	ReadWorkspaceFile(ctx context.Context, repoID, taskID, relPath string, maxBytes int64) (*WorkspaceFile, error)
}

// CredentialedWorker defines operations executed within the ephemeral networked worker container.
type CredentialedWorker interface {
	FetchRemoteRefToBundle(ctx context.Context, remoteURL, refName, token string, outStream io.Writer) error
	ImportBundleAndPush(ctx context.Context, remoteURL, taskID, token string, bundleStream io.Reader) error
}
