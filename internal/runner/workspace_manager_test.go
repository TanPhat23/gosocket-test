package runner_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/runner"
)

func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v, output: %s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func initTestGitRepo(t *testing.T, rootPath string) (string, string) {
	t.Helper()
	repoPath := filepath.Join(rootPath, "repo")
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	runGitCmd(t, repoPath, "init", "-b", "main")
	runGitCmd(t, repoPath, "config", "user.name", "Test Runner")
	runGitCmd(t, repoPath, "config", "user.email", "test@example.com")

	readme := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readme, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runGitCmd(t, repoPath, "add", "README.md")
	runGitCmd(t, repoPath, "commit", "-m", "Initial commit")
	baseCommit := strings.TrimSpace(runGitCmd(t, repoPath, "rev-parse", "HEAD"))

	return repoPath, baseCommit
}

// assertWorkspaceRefused proves EnsureTaskWorktree refused with the quarantine sentinel, returned an
// empty path, and that nothing was created under the workspace or scratch roots.
func assertWorkspaceRefused(t *testing.T, path string, err error) {
	t.Helper()
	if path != "" {
		t.Errorf("expected empty path, got %q", path)
	}
	if !errors.Is(err, runner.ErrHostWorkspaceProvisioningRefused) {
		t.Fatalf("expected ErrHostWorkspaceProvisioningRefused, got: %v", err)
	}
	if !errors.Is(err, common.ErrRuntimeQuarantined) {
		t.Errorf("expected error to wrap common.ErrRuntimeQuarantined, got: %v", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "gate") {
		t.Errorf("refusal message must not mention internal gate names: %q", err.Error())
	}
}

func TestWorkspaceManager_QuarantineRemainsActive(t *testing.T) {
	wm := runner.NewWorkspaceManager("/workspaces", "/tmp/scratch")
	path, err := wm.EnsureTaskWorktree(context.Background(), "proj_1", &models.Task{ID: "task_1"}, "repo_1", "refs/heads/main")
	if path != "" {
		t.Fatalf("expected empty path under quarantine, got %q", path)
	}
	if !errors.Is(err, runner.ErrHostWorkspaceProvisioningRefused) {
		t.Fatalf("expected ErrHostWorkspaceProvisioningRefused, got %v", err)
	}
	if !errors.Is(err, common.ErrRuntimeQuarantined) {
		t.Fatalf("expected error to wrap common.ErrRuntimeQuarantined, got %v", err)
	}

	// Also verify with NewWorkspaceManagerWithHelper
	wmWithHelper := runner.NewWorkspaceManagerWithHelper("/workspaces", "/tmp/scratch", nil)
	path2, err2 := wmWithHelper.EnsureTaskWorktree(context.Background(), "proj_1", &models.Task{ID: "task_1"}, "repo_1", "refs/heads/main")
	if path2 != "" {
		t.Fatalf("expected empty path under quarantine, got %q", path2)
	}
	if !errors.Is(err2, runner.ErrHostWorkspaceProvisioningRefused) {
		t.Fatalf("expected ErrHostWorkspaceProvisioningRefused, got %v", err2)
	}
	if !errors.Is(err2, common.ErrRuntimeQuarantined) {
		t.Fatalf("expected error to wrap common.ErrRuntimeQuarantined, got %v", err2)
	}
}

func TestWorkspaceManager_EnsureTaskWorktree_ValidProvisioning(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	scratchRoot := t.TempDir()

	wm := runner.NewWorkspaceManager(workspaceRoot, scratchRoot)
	task := &models.Task{ID: "task_456"}

	path, err := wm.EnsureTaskWorktree(ctx, "proj_123", task, "myrepo", "abc123")
	assertWorkspaceRefused(t, path, err)

	// The workspace root stays completely empty and no scratch directory is created.
	if entries := snapshotTree(t, workspaceRoot); len(entries) != 1 {
		t.Errorf("workspace root must stay empty, got %v", entries)
	}
	if entries := snapshotTree(t, scratchRoot); len(entries) != 1 {
		t.Errorf("scratch root must stay empty, got %v", entries)
	}
}

func TestWorkspaceManager_EnsureTaskWorktree_LeavesExistingRepoUntouched(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	scratchRoot := t.TempDir()

	repoName := "myrepo"
	_, baseCommit := initTestGitRepo(t, filepath.Join(workspaceRoot, repoName))
	before := snapshotTree(t, workspaceRoot)

	wm := runner.NewWorkspaceManager(workspaceRoot, scratchRoot)
	task := &models.Task{ID: "task_existing"}
	path, err := wm.EnsureTaskWorktree(ctx, "proj_123", task, repoName, baseCommit)
	assertWorkspaceRefused(t, path, err)

	after := snapshotTree(t, workspaceRoot)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("workspace changed.\nbefore: %v\nafter:  %v", before, after)
	}
	if _, statErr := os.Stat(filepath.Join(workspaceRoot, repoName, "tasks")); !os.IsNotExist(statErr) {
		t.Errorf("tasks directory must not be created")
	}
	if entries := snapshotTree(t, scratchRoot); len(entries) != 1 {
		t.Errorf("scratch root must stay empty, got %v", entries)
	}
}

func TestWorkspaceManager_EnsureTaskWorktree_NilTaskRefused(t *testing.T) {
	wm := runner.NewWorkspaceManager(t.TempDir(), t.TempDir())
	path, err := wm.EnsureTaskWorktree(context.Background(), "proj", nil, "repo", "abc123")
	assertWorkspaceRefused(t, path, err)
}

func TestWorkspaceManager_EnsureTaskWorktree_NoCheckout(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	scratchRoot := t.TempDir()

	repoName := "testrepo"
	_, baseCommit := initTestGitRepo(t, filepath.Join(workspaceRoot, repoName))

	wm := runner.NewWorkspaceManager(workspaceRoot, scratchRoot)
	task := &models.Task{ID: "task_nc"}
	path, err := wm.EnsureTaskWorktree(ctx, "proj_nc", task, repoName, baseCommit)
	assertWorkspaceRefused(t, path, err)

	worktree := filepath.Join(workspaceRoot, repoName, "tasks", task.ID)
	if _, statErr := os.Stat(worktree); !os.IsNotExist(statErr) {
		t.Errorf("no worktree may be created at %s", worktree)
	}
}

func TestWorkspaceManager_SubmoduleFailClosed(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	scratchRoot := t.TempDir()

	repoName := "submodulerepo"
	repoPath, _ := initTestGitRepo(t, filepath.Join(workspaceRoot, repoName))
	gitmodules := filepath.Join(repoPath, ".gitmodules")
	if err := os.WriteFile(gitmodules, []byte("[submodule \"test\"]\n\tpath = test\n\turl = https://example.com/test.git\n"), 0644); err != nil {
		t.Fatalf("failed to write .gitmodules: %v", err)
	}
	runGitCmd(t, repoPath, "add", ".gitmodules")
	runGitCmd(t, repoPath, "commit", "-m", "Add submodule config")
	baseCommit := strings.TrimSpace(runGitCmd(t, repoPath, "rev-parse", "HEAD"))

	wm := runner.NewWorkspaceManager(workspaceRoot, scratchRoot)
	path, err := wm.EnsureTaskWorktree(ctx, "proj_1", &models.Task{ID: "task_sub"}, repoName, baseCommit)
	assertWorkspaceRefused(t, path, err)
	if strings.Contains(err.Error(), "submodule") {
		t.Errorf("refusal must precede submodule inspection, got %q", err.Error())
	}
}

func TestWorkspaceManager_GitLFSFailClosed(t *testing.T) {
	tests := []struct {
		name      string
		setupRepo func(t *testing.T, repoPath string)
	}{
		{
			name: "LFS pointer file content",
			setupRepo: func(t *testing.T, repoPath string) {
				lfsFile := filepath.Join(repoPath, "large_file.bin")
				content := "version https://git-lfs.github.com/spec/v1\noid sha256:4b4231a4\nsize 12345\n"
				if err := os.WriteFile(lfsFile, []byte(content), 0644); err != nil {
					t.Fatalf("failed to write lfs file: %v", err)
				}
				runGitCmd(t, repoPath, "add", "large_file.bin")
				runGitCmd(t, repoPath, "commit", "-m", "Add LFS pointer")
			},
		},
		{
			name: ".gitattributes filter=lfs",
			setupRepo: func(t *testing.T, repoPath string) {
				gitattrs := filepath.Join(repoPath, ".gitattributes")
				content := "*.bin filter=lfs diff=lfs merge=lfs -text\n"
				if err := os.WriteFile(gitattrs, []byte(content), 0644); err != nil {
					t.Fatalf("failed to write .gitattributes: %v", err)
				}
				runGitCmd(t, repoPath, "add", ".gitattributes")
				runGitCmd(t, repoPath, "commit", "-m", "Add LFS gitattributes")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			workspaceRoot := t.TempDir()
			scratchRoot := t.TempDir()

			repoName := "lfsrepo"
			repoPath, _ := initTestGitRepo(t, filepath.Join(workspaceRoot, repoName))
			tc.setupRepo(t, repoPath)
			baseCommit := strings.TrimSpace(runGitCmd(t, repoPath, "rev-parse", "HEAD"))

			wm := runner.NewWorkspaceManager(workspaceRoot, scratchRoot)
			path, err := wm.EnsureTaskWorktree(ctx, "proj_1", &models.Task{ID: "task_lfs"}, repoName, baseCommit)
			assertWorkspaceRefused(t, path, err)
			if strings.Contains(err.Error(), "LFS") {
				t.Errorf("refusal must precede LFS inspection, got %q", err.Error())
			}
		})
	}
}

func TestWorkspaceManager_PathSanitization(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	scratchRoot := t.TempDir()
	wm := runner.NewWorkspaceManager(workspaceRoot, scratchRoot)

	// Every input, including previously rejected malicious ones, is refused identically
	// before any sanitization, stat, git, or filesystem work.
	inputs := []struct {
		name       string
		projectID  string
		taskID     string
		repoName   string
		baseCommit string
	}{
		{"traversal in projectID", "../etc", "task1", "repo1", "abc123"},
		{"slash in projectID", "proj/sub", "task1", "repo1", "abc123"},
		{"traversal in taskID", "proj1", "../../../root", "repo1", "abc123"},
		{"backslash in taskID", "proj1", "task\\sub", "repo1", "abc123"},
		{"traversal in repoName", "proj1", "task1", "../../var", "abc123"},
		{"null byte in repoName", "proj1", "task1", "repo\x00bad", "abc123"},
		{"special char in baseCommit", "proj1", "task1", "repo1", "main; rm -rf /"},
		{"dash/dots in baseCommit traversal", "proj1", "task1", "repo1", "../main"},
		{"empty projectID", "", "task1", "repo1", "abc123"},
		{"empty taskID", "proj1", "", "repo1", "abc123"},
		{"empty repoName", "proj1", "task1", "", "abc123"},
		{"empty baseCommit", "proj1", "task1", "repo1", ""},
		{"valid identifiers", "proj1", "task1", "repo1", "abc123"},
	}
	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			path, err := wm.EnsureTaskWorktree(ctx, tc.projectID, &models.Task{ID: tc.taskID}, tc.repoName, tc.baseCommit)
			assertWorkspaceRefused(t, path, err)
		})
	}
	if entries := snapshotTree(t, workspaceRoot); len(entries) != 1 {
		t.Errorf("workspace root must stay empty, got %v", entries)
	}
	if entries := snapshotTree(t, scratchRoot); len(entries) != 1 {
		t.Errorf("scratch root must stay empty, got %v", entries)
	}

	// CleanupTaskScratch is unchanged and still validates its identifiers.
	badCleanupInputs := []struct {
		name      string
		projectID string
		taskID    string
	}{
		{"traversal in projectID", "../etc", "task1"},
		{"traversal in taskID", "proj1", "../../etc"},
		{"empty taskID", "proj1", ""},
		{"invalid char taskID", "proj1", "task$1"},
	}
	for _, tc := range badCleanupInputs {
		t.Run("cleanup_"+tc.name, func(t *testing.T) {
			if err := wm.CleanupTaskScratch(ctx, tc.projectID, tc.taskID); err == nil {
				t.Fatalf("expected error for malicious cleanup input %q, got nil", tc.name)
			}
		})
	}
}

func TestWorkspaceManager_ZeroCredentialsInGitConfig(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	scratchRoot := t.TempDir()

	repoName := "zerocred"
	repoDir := filepath.Join(workspaceRoot, repoName)
	_, baseCommit := initTestGitRepo(t, repoDir)
	repoGitConfig := filepath.Join(repoDir, "repo", ".git", "config")
	configBefore, err := os.ReadFile(repoGitConfig)
	if err != nil {
		t.Fatalf("failed to read repo git config: %v", err)
	}

	wm := runner.NewWorkspaceManager(workspaceRoot, scratchRoot)
	task := &models.Task{ID: "task_cred"}
	path, err := wm.EnsureTaskWorktree(ctx, "proj_cred", task, repoName, baseCommit)
	assertWorkspaceRefused(t, path, err)

	// The repo's git config is byte-identical (no credentials or headers added) and no worktree exists.
	configAfter, err := os.ReadFile(repoGitConfig)
	if err != nil {
		t.Fatalf("failed to re-read repo git config: %v", err)
	}
	if string(configBefore) != string(configAfter) {
		t.Errorf("repo git config changed")
	}
	for _, sensitive := range []string{"extraHeader", "token", "password", "Authorization", "ghp_"} {
		if strings.Contains(string(configAfter), sensitive) {
			t.Fatalf("sensitive indicator %q found in %s", sensitive, repoGitConfig)
		}
	}
	if _, statErr := os.Stat(filepath.Join(repoDir, "tasks", task.ID)); !os.IsNotExist(statErr) {
		t.Errorf("no worktree may be created")
	}
}

func TestWorkspaceManager_CleanupTaskScratch_Hygiene(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	scratchRoot := t.TempDir()

	repoName := "cleanuphygienerepo"
	wm := runner.NewWorkspaceManager(workspaceRoot, scratchRoot)
	task := &models.Task{ID: "task_hygiene"}

	// Provisioning is refused, so the directories this cleanup test needs are created directly.
	path, err := wm.EnsureTaskWorktree(ctx, "proj_hygiene", task, repoName, "abc123")
	assertWorkspaceRefused(t, path, err)

	containerPath := filepath.Join(workspaceRoot, repoName, "tasks", task.ID)
	scratchTaskDir := filepath.Join(scratchRoot, task.ID)
	if err := os.MkdirAll(containerPath, 0755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	if err := os.MkdirAll(scratchTaskDir, 0755); err != nil {
		t.Fatalf("failed to create scratch dir: %v", err)
	}

	scratchFile := filepath.Join(scratchTaskDir, "scratch_data.tmp")
	if err := os.WriteFile(scratchFile, []byte("ephemeral data"), 0644); err != nil {
		t.Fatalf("failed to write scratch file: %v", err)
	}
	worktreeFile := filepath.Join(containerPath, "agent_work.txt")
	if err := os.WriteFile(worktreeFile, []byte("agent output"), 0644); err != nil {
		t.Fatalf("failed to write worktree file: %v", err)
	}

	if err := wm.CleanupTaskScratch(ctx, "proj_hygiene", task.ID); err != nil {
		t.Fatalf("CleanupTaskScratch failed: %v", err)
	}
	if _, err := os.Stat(scratchTaskDir); !os.IsNotExist(err) {
		t.Fatalf("expected scratch task dir %s to be purged, but it still exists", scratchTaskDir)
	}
	if _, err := os.Stat(worktreeFile); err != nil {
		t.Fatalf("expected worktree file %s to be retained, but got error: %v", worktreeFile, err)
	}
}
