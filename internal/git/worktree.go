package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeManager manages Git worktrees on the host system and resolves container mount paths.
type WorktreeManager struct {
	hostWorktreeRoot  string
	localWorktreeRoot string
}

// NewWorktreeManager creates a new WorktreeManager.
// hostWorktreeRoot is the path on the host system where worktrees are stored (for Docker daemon bind mounts).
// localWorktreeRoot is the local filesystem path accessible to this process (defaults to hostWorktreeRoot if omitted).
func NewWorktreeManager(hostWorktreeRoot string, localWorktreeRoot ...string) *WorktreeManager {
	localRoot := hostWorktreeRoot
	if len(localWorktreeRoot) > 0 && localWorktreeRoot[0] != "" {
		localRoot = localWorktreeRoot[0]
	}
	return &WorktreeManager{
		hostWorktreeRoot:  hostWorktreeRoot,
		localWorktreeRoot: localRoot,
	}
}

// ResolvePaths returns the host-level worktree path and the Docker container bind mount string.
func (m *WorktreeManager) ResolvePaths(taskID string) (hostPath, containerBindPath string) {
	hostPath = filepath.Join(m.hostWorktreeRoot, taskID)
	containerBindPath = fmt.Sprintf("%s:/workspace:rw", hostPath)
	return hostPath, containerBindPath
}

// ResolveLocalPath returns the local filesystem path for a task's worktree.
func (m *WorktreeManager) ResolveLocalPath(taskID string) string {
	return filepath.Join(m.localWorktreeRoot, taskID)
}

// CreateWorktree creates a new Git worktree for the task at the resolved local path,
// checks out or creates the specified branch, and returns the local path, host path, and bind mount spec.
func (m *WorktreeManager) CreateWorktree(ctx context.Context, repoPath, taskID, branch string) (localPath, hostPath, bindPath string, err error) {
	localPath = m.ResolveLocalPath(taskID)
	hostPath, bindPath = m.ResolvePaths(taskID)

	// Ensure parent directory for worktrees exists
	if err := os.MkdirAll(m.localWorktreeRoot, 0755); err != nil {
		return "", "", "", fmt.Errorf("failed to create local worktree root %q: %w", m.localWorktreeRoot, err)
	}

	// Try adding worktree with a new branch: git worktree add <localPath> -b <branch>
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", localPath, "-b", branch)
	cmd.Dir = repoPath
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errStr := stderr.String()
		// If branch already exists, attach to existing branch: git worktree add <localPath> <branch>
		if strings.Contains(errStr, "already exists") {
			cmdRetry := exec.CommandContext(ctx, "git", "worktree", "add", localPath, branch)
			cmdRetry.Dir = repoPath
			var retryStderr bytes.Buffer
			cmdRetry.Stderr = &retryStderr
			if retryErr := cmdRetry.Run(); retryErr != nil {
				return "", "", "", fmt.Errorf("git worktree add (existing branch %q) failed: %w (stderr: %s)", branch, retryErr, retryStderr.String())
			}
		} else {
			return "", "", "", fmt.Errorf("git worktree add failed: %w (stderr: %s)", err, errStr)
		}
	}

	return localPath, hostPath, bindPath, nil
}

// RemoveWorktree forces removal of the worktree and prunes git worktree metadata.
func (m *WorktreeManager) RemoveWorktree(ctx context.Context, repoPath, taskID string) error {
	localPath := m.ResolveLocalPath(taskID)

	// git worktree remove --force <localPath>
	cmdRemove := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", localPath)
	cmdRemove.Dir = repoPath
	var removeStderr bytes.Buffer
	cmdRemove.Stderr = &removeStderr
	if err := cmdRemove.Run(); err != nil {
		// Log or retain error, but don't fail immediately if directory can still be cleaned or if worktree was already removed
		removeErrStr := strings.TrimSpace(removeStderr.String())
		// If it's not a known worktree or already removed, proceed to prune & os.RemoveAll
		if !strings.Contains(removeErrStr, "is not a working tree") && !strings.Contains(removeErrStr, "No such file or directory") {
			// Try pruning first before returning error
		}
	}

	// git worktree prune
	cmdPrune := exec.CommandContext(ctx, "git", "worktree", "prune")
	cmdPrune.Dir = repoPath
	_ = cmdPrune.Run()

	// Ensure filesystem directory is cleaned up
	if err := os.RemoveAll(localPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clean up worktree directory %q: %w", localPath, err)
	}

	return nil
}
