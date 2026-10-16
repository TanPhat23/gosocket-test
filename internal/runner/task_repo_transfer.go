package runner

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"agent-task-manager/internal/git/transfer"
)

// githubSlugRegex matches "owner/repo" as GitHub allows it. It rejects anything that could change the
// host or smuggle userinfo, a port, a path escape or a query into the remote URL.
var githubSlugRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+$`)

// TaskRepoTransfer moves a task's repository between GitHub and the project data volume through the
// Gate 0C transfer containers. It never runs git on the host and never sees the volume.
//
// It is NOT wired into RunTask or EnsureTaskWorktree: those still refuse unconditionally while the
// runtime is quarantined. Wiring it in is part of lifting the quarantine, which needs Gates 0C, 0D, 0F
// and 0G qualified and the owner's explicit go-ahead.
//
// The project ID is the transfer RepoID, so the volume is the one the sandbox mounts
// (sandbox.ProjectDataVolumeName). Credentials are approved for github.com only.
type TaskRepoTransfer struct {
	mgr transfer.GitTransferManager
}

func NewTaskRepoTransfer(mgr transfer.GitTransferManager) *TaskRepoTransfer {
	return &TaskRepoTransfer{mgr: mgr}
}

// githubRemoteURL returns the https github.com clone URL for an "owner/repo" slug, or an error for anything else.
func githubRemoteURL(slug string) (string, error) {
	if !githubSlugRegex.MatchString(slug) || strings.HasSuffix(slug, "/.") || strings.HasSuffix(slug, "/..") {
		return "", fmt.Errorf("invalid GitHub repository %q: want owner/repo", slug)
	}
	return "https://github.com/" + slug + ".git", nil
}

// IngestTaskBranch fetches baseBranch of slug into the project volume and creates the task worktree.
// It returns the in-container worktree path.
func (t *TaskRepoTransfer) IngestTaskBranch(ctx context.Context, projectID, taskID, slug, baseBranch, token string) (string, error) {
	remote, err := githubRemoteURL(slug)
	if err != nil {
		return "", err
	}
	res, err := t.mgr.IngestRemoteBranch(ctx, transfer.IngestRequest{
		ProjectID: projectID, RepoID: projectID, TaskID: taskID,
		RemoteURL: remote, BaseRef: "refs/heads/" + baseBranch, AuthToken: token,
	})
	if err != nil {
		return "", fmt.Errorf("ingest %s@%s for task %s: %w", slug, baseBranch, taskID, err)
	}
	return res.TaskWorktreePath, nil
}

// PushTaskBranch exports task/<taskID> from the project volume and pushes it to slug.
func (t *TaskRepoTransfer) PushTaskBranch(ctx context.Context, projectID, taskID, slug, token string) error {
	remote, err := githubRemoteURL(slug)
	if err != nil {
		return err
	}
	if _, err := t.mgr.ExportAndPushTask(ctx, transfer.PushRequest{
		ProjectID: projectID, RepoID: projectID, TaskID: taskID, RemoteURL: remote, AuthToken: token,
	}); err != nil {
		return fmt.Errorf("push task %s to %s: %w", taskID, slug, err)
	}
	return nil
}
