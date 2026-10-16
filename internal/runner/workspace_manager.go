package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/git/transfer"
	"agent-task-manager/internal/models"
)

// ErrHostWorkspaceProvisioningRefused is returned by EnsureTaskWorktree while host-side
// worktree provisioning is disabled. It wraps common.ErrRuntimeQuarantined.
var ErrHostWorkspaceProvisioningRefused = fmt.Errorf("%w", common.ErrRuntimeQuarantined)

var safeIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

// WorkspaceManager defines the operations for provisioning in-container task worktrees
// and managing scratch storage.
type WorkspaceManager interface {
	EnsureTaskWorktree(ctx context.Context, projectID string, task *models.Task, repoName string, baseCommit string) (containerWorktreePath string, err error)
	CleanupTaskScratch(ctx context.Context, projectID string, taskID string) error
}

type workspaceManager struct {
	workspaceRoot string
	scratchRoot   string
	volumeHelper  transfer.VolumeHelper
}

// NewWorkspaceManager creates a WorkspaceManager instance with configurable or default roots.
// Defaults: workspaceRoot = "/workspaces", scratchRoot = "/tmp/scratch".
func NewWorkspaceManager(workspaceRoot string, scratchRoot string) WorkspaceManager {
	return NewWorkspaceManagerWithHelper(workspaceRoot, scratchRoot, nil)
}

// NewWorkspaceManagerWithHelper creates a WorkspaceManager instance with configurable or default roots
// and a containerized volume helper.
func NewWorkspaceManagerWithHelper(workspaceRoot string, scratchRoot string, helper transfer.VolumeHelper) WorkspaceManager {
	if workspaceRoot == "" {
		workspaceRoot = "/workspaces"
	}
	if scratchRoot == "" {
		scratchRoot = "/tmp/scratch"
	}
	return &workspaceManager{
		workspaceRoot: workspaceRoot,
		scratchRoot:   scratchRoot,
		volumeHelper:  helper,
	}
}

// EnsureTaskWorktree provisions a task worktree at filepath.Join(workspaceRoot, repoName, "tasks", task.ID)
// without checking out files (--no-checkout), enforces fail-closed checks against submodules and Git LFS,
// and ensures an ephemeral scratch directory is created.
//
// Provisioning is disabled pending Phase 1B and Gates 0C/0D/0G. The refusal is unconditional and
// is the first statement: no sanitization, stat, git, or filesystem call precedes it.
func (m *workspaceManager) EnsureTaskWorktree(ctx context.Context, projectID string, task *models.Task, repoName string, baseCommit string) (string, error) {
	return "", ErrHostWorkspaceProvisioningRefused
}

// ensureTaskWorktreeDisabledDraft is the original provisioning draft, intentionally unreferenced.
// It is disabled pending Phase 1B/Gates 0C/0D/0G.
func (m *workspaceManager) ensureTaskWorktreeDisabledDraft(ctx context.Context, projectID string, task *models.Task, repoName string, baseCommit string) (string, error) {
	if task == nil {
		return "", fmt.Errorf("task cannot be nil")
	}

	// 1. Path & Identifier Sanitization
	if err := validateSafeIdentifier("projectID", projectID); err != nil {
		return "", err
	}
	if err := validateSafeIdentifier("taskID", task.ID); err != nil {
		return "", err
	}
	if err := validateSafeIdentifier("repoName", repoName); err != nil {
		return "", err
	}
	if err := validateSafeIdentifier("baseCommit", baseCommit); err != nil {
		return "", err
	}

	targetRepoPath := filepath.Join(m.workspaceRoot, repoName, "repo")
	targetWorktreePath := filepath.Join(m.workspaceRoot, repoName, "tasks", task.ID)
	targetScratchPath := filepath.Join(m.scratchRoot, task.ID)

	// Verify target repository exists
	if _, err := os.Stat(targetRepoPath); err != nil {
		return "", fmt.Errorf("repository directory does not exist at %q: %w", targetRepoPath, err)
	}

	// 2. Preflight Security Inspection on baseCommit: Submodules
	if err := inspectForSubmodules(ctx, targetRepoPath, baseCommit); err != nil {
		return "", err
	}

	// 3. Preflight Security Inspection on baseCommit: Git LFS
	if err := inspectForGitLFS(ctx, targetRepoPath, baseCommit); err != nil {
		return "", err
	}

	// 4. Ensure target task worktree parent directories exist
	tasksParentDir := filepath.Join(m.workspaceRoot, repoName, "tasks")
	if err := os.MkdirAll(tasksParentDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create tasks directory %q: %w", tasksParentDir, err)
	}

	// 5. Worktree Provisioning with Git execution hygiene
	// Check if worktree already exists and is valid
	if info, statErr := os.Stat(targetWorktreePath); statErr == nil && info.IsDir() {
		checkCmd := exec.CommandContext(ctx, "git", "-C", targetWorktreePath, "rev-parse", "--is-inside-work-tree")
		if checkOut, checkErr := checkCmd.Output(); checkErr == nil && strings.TrimSpace(string(checkOut)) == "true" {
			// Worktree already exists and is healthy; ensure scratch dir exists
			if err := os.MkdirAll(targetScratchPath, 0777); err != nil {
				return "", fmt.Errorf("failed to create scratch directory %q: %w", targetScratchPath, err)
			}
			return targetWorktreePath, nil
		}
		// If exists but invalid or dirty, clean it up before recreating
		_ = exec.CommandContext(ctx, "git", "-C", targetRepoPath,
			"-c", "core.hooksPath=/dev/null",
			"-c", "core.fsmonitor=false",
			"-c", "submodule.recurse=false",
			"worktree", "remove", "--force", targetWorktreePath).Run()
		_ = os.RemoveAll(targetWorktreePath)
		_ = exec.CommandContext(ctx, "git", "-C", targetRepoPath,
			"-c", "core.hooksPath=/dev/null",
			"-c", "core.fsmonitor=false",
			"-c", "submodule.recurse=false",
			"worktree", "prune").Run()
	}

	branchName := fmt.Sprintf("task/%s", task.ID)
	// git -c core.hooksPath=/dev/null -c core.fsmonitor=false -c submodule.recurse=false worktree add --no-checkout -b task/<task-id> <targetWorktreePath> <baseCommit>
	addArgs := []string{
		"-C", targetRepoPath,
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "submodule.recurse=false",
		"worktree", "add", "--no-checkout",
		"-b", branchName,
		targetWorktreePath,
		baseCommit,
	}

	cmd := exec.CommandContext(ctx, "git", addArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errStr := stderr.String()
		// If branch already exists, attach to existing branch without -b
		if strings.Contains(errStr, "already exists") {
			cmdRetry := exec.CommandContext(ctx, "git",
				"-C", targetRepoPath,
				"-c", "core.hooksPath=/dev/null",
				"-c", "core.fsmonitor=false",
				"-c", "submodule.recurse=false",
				"worktree", "add", "--no-checkout",
				targetWorktreePath,
				branchName,
			)
			var retryStderr bytes.Buffer
			cmdRetry.Stderr = &retryStderr
			if retryErr := cmdRetry.Run(); retryErr != nil {
				return "", fmt.Errorf("git worktree add (existing branch %q) failed: %w (stderr: %s)", branchName, retryErr, retryStderr.String())
			}
		} else {
			return "", fmt.Errorf("git worktree add failed: %w (stderr: %s)", err, errStr)
		}
	}

	// 6. Scratch Directory Provisioning
	if err := os.MkdirAll(targetScratchPath, 0777); err != nil {
		return "", fmt.Errorf("failed to create scratch directory %q: %w", targetScratchPath, err)
	}

	return targetWorktreePath, nil
}

// CleanupTaskScratch validates projectID and taskID, then purges /tmp/scratch/<task_id>,
// while strictly retaining the worktree in /workspaces/<repo>/tasks/<task_id>.
func (m *workspaceManager) CleanupTaskScratch(ctx context.Context, projectID string, taskID string) error {
	if err := validateSafeIdentifier("projectID", projectID); err != nil {
		return err
	}
	if err := validateSafeIdentifier("taskID", taskID); err != nil {
		return err
	}

	targetScratchPath := filepath.Join(m.scratchRoot, taskID)
	if err := os.RemoveAll(targetScratchPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to cleanup task scratch directory %q: %w", targetScratchPath, err)
	}
	return nil
}

func validateSafeIdentifier(field, val string) error {
	if val == "" {
		return fmt.Errorf("%s cannot be empty", field)
	}
	if strings.Contains(val, "..") || strings.Contains(val, "/") || strings.Contains(val, "\\") || strings.Contains(val, "\x00") {
		return fmt.Errorf("%s contains invalid path traversal or separator characters: %q", field, val)
	}
	if !safeIDRegex.MatchString(val) {
		return fmt.Errorf("%s contains invalid characters (must match ^[a-zA-Z0-9_\\-]+$): %q", field, val)
	}
	return nil
}

func inspectForSubmodules(ctx context.Context, repoPath, baseCommit string) error {
	// 1. Check if .gitmodules file exists in tree of baseCommit
	cmdCat := exec.CommandContext(ctx, "git", "-C", repoPath,
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "submodule.recurse=false",
		"cat-file", "-e", fmt.Sprintf("%s:.gitmodules", baseCommit))
	if err := cmdCat.Run(); err == nil {
		return fmt.Errorf("repository contains submodules, which are not supported in v1")
	}

	// 2. Check for gitlink entries (mode 160000) in git ls-tree
	cmdLs := exec.CommandContext(ctx, "git", "-C", repoPath,
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "submodule.recurse=false",
		"ls-tree", "-r", baseCommit)
	out, err := cmdLs.Output()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "160000 commit ") {
				return fmt.Errorf("repository contains submodules, which are not supported in v1")
			}
		}
	}

	return nil
}

func inspectForGitLFS(ctx context.Context, repoPath, baseCommit string) error {
	// 1. Check if .gitattributes mentions filter=lfs
	cmdCat := exec.CommandContext(ctx, "git", "-C", repoPath,
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "submodule.recurse=false",
		"show", fmt.Sprintf("%s:.gitattributes", baseCommit))
	if out, err := cmdCat.Output(); err == nil {
		if strings.Contains(string(out), "filter=lfs") {
			return fmt.Errorf("repository contains Git LFS pointers, which are not supported in v1")
		}
	}

	// 2. Check all blobs in tree to see if any start with LFS pointer signature
	// We run git grep at baseCommit for the exact spec prefix: ^version https://git-lfs.github.com/spec/v1
	cmdGrep := exec.CommandContext(ctx, "git", "-C", repoPath,
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "submodule.recurse=false",
		"grep", "-q", "-E", "^version https://git-lfs.github.com/spec/v1", baseCommit)
	if err := cmdGrep.Run(); err == nil {
		// Exit code 0 means match found!
		return fmt.Errorf("repository contains Git LFS pointers, which are not supported in v1")
	}

	return nil
}
