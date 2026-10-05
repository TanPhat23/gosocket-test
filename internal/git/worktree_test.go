package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"agent-task-manager/internal/git"
)

func TestPathTranslationForDockerSiblingBinds(t *testing.T) {
	hostWorktreeRoot := "/srv/agent-tasks/worktrees"
	wm := git.NewWorktreeManager(hostWorktreeRoot)

	hostPath, bindPath := wm.ResolvePaths("task-xyz-123")
	expectedHost := "/srv/agent-tasks/worktrees/task-xyz-123"

	if hostPath != expectedHost {
		t.Errorf("expected host path %s, got %s", expectedHost, hostPath)
	}
	if bindPath != expectedHost+":/workspace:rw" {
		t.Errorf("expected bind spec %s:/workspace:rw, got %s", expectedHost, bindPath)
	}

	// Also verify default local path falls back to hostWorktreeRoot
	if wm.ResolveLocalPath("task-xyz-123") != expectedHost {
		t.Errorf("expected default localPath %s, got %s", expectedHost, wm.ResolveLocalPath("task-xyz-123"))
	}
}

func setupTestGitRepo(t *testing.T) (repoPath string) {
	t.Helper()
	tempDir := t.TempDir()
	repoPath = filepath.Join(tempDir, "repo")

	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %v, output: %s", strings.Join(args, " "), err, string(out))
		}
	}

	runGit(repoPath, "init", "-b", "main")
	runGit(repoPath, "config", "user.name", "Test Runner")
	runGit(repoPath, "config", "user.email", "test@example.com")

	initialFile := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(initialFile, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}
	runGit(repoPath, "add", "README.md")
	runGit(repoPath, "commit", "-m", "Initial commit")

	return repoPath
}

func TestWorktreeLifecycle(t *testing.T) {
	ctx := context.Background()

	repoPath := setupTestGitRepo(t)
	localWorktreeRoot := filepath.Join(t.TempDir(), "worktrees")
	hostWorktreeRoot := "/srv/agent-tasks/worktrees"

	// 1. Initialize WorktreeManager
	wm := git.NewWorktreeManager(hostWorktreeRoot, localWorktreeRoot)

	taskID := "task-abc-999"
	branch := "task/abc-999-feature"

	// 2. Create worktree (new branch)
	localPath, hostPath, bindPath, err := wm.CreateWorktree(ctx, repoPath, taskID, branch)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	expectedLocal := filepath.Join(localWorktreeRoot, taskID)
	expectedHost := filepath.Join(hostWorktreeRoot, taskID)
	expectedBind := expectedHost + ":/workspace:rw"

	if localPath != expectedLocal {
		t.Errorf("expected localPath %s, got %s", expectedLocal, localPath)
	}
	if hostPath != expectedHost {
		t.Errorf("expected hostPath %s, got %s", expectedHost, hostPath)
	}
	if bindPath != expectedBind {
		t.Errorf("expected bindPath %s, got %s", expectedBind, bindPath)
	}

	// Verify worktree file isolation and existence
	worktreeReadme := filepath.Join(localPath, "README.md")
	data, err := os.ReadFile(worktreeReadme)
	if err != nil {
		t.Fatalf("failed to read README.md in worktree: %v", err)
	}
	if string(data) != "# Test Repo\n" {
		t.Errorf("unexpected content in worktree README: %s", string(data))
	}

	// Verify .git in worktree is a gitdir reference file, not a full .git directory
	gitEntry := filepath.Join(localPath, ".git")
	fi, err := os.Stat(gitEntry)
	if err != nil {
		t.Fatalf(".git not found in worktree: %v", err)
	}
	if fi.IsDir() {
		t.Errorf(".git in worktree should be a gitdir pointer file, not a directory")
	}

	// 3. Modify worktree file and ensure main repo is not touched
	if err := os.WriteFile(worktreeReadme, []byte("# Modified in Worktree\n"), 0644); err != nil {
		t.Fatalf("failed to write modified file: %v", err)
	}
	repoReadme, err := os.ReadFile(filepath.Join(repoPath, "README.md"))
	if err != nil {
		t.Fatalf("failed to read repo README: %v", err)
	}
	if string(repoReadme) != "# Test Repo\n" {
		t.Errorf("main repo file was modified: %s", string(repoReadme))
	}

	// 4. Remove worktree
	if err := wm.RemoveWorktree(ctx, repoPath, taskID); err != nil {
		t.Fatalf("RemoveWorktree failed: %v", err)
	}

	// Verify localPath directory is gone
	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Errorf("expected worktree dir %s to be removed, stat err: %v", localPath, err)
	}

	// 5. Test recreate on already existing branch
	taskID2 := "task-abc-999-recreate"
	localPath2, _, _, err := wm.CreateWorktree(ctx, repoPath, taskID2, branch)
	if err != nil {
		t.Fatalf("CreateWorktree with existing branch failed: %v", err)
	}
	if _, err := os.Stat(localPath2); err != nil {
		t.Errorf("expected recreated worktree dir to exist: %v", err)
	}

	// Cleanup taskID2
	if err := wm.RemoveWorktree(ctx, repoPath, taskID2); err != nil {
		t.Fatalf("RemoveWorktree failed for recreated worktree: %v", err)
	}

	// 6. Test removing a worktree that is already removed (idempotency)
	if err := wm.RemoveWorktree(ctx, repoPath, "non-existent-task"); err != nil {
		t.Errorf("RemoveWorktree on non-existent worktree should succeed idempotently, got: %v", err)
	}
}

func TestWorktreeContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled context

	repoPath := setupTestGitRepo(t)
	localWorktreeRoot := filepath.Join(t.TempDir(), "worktrees")
	wm := git.NewWorktreeManager("/srv/agent-tasks/worktrees", localWorktreeRoot)

	_, _, _, err := wm.CreateWorktree(ctx, repoPath, "task-cancelled", "branch-cancelled")
	if err == nil {
		t.Fatal("expected error due to cancelled context, got nil")
	}
}
