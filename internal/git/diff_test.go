package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"agent-task-manager/internal/git"
)

func TestDiffParserWithUntrackedFiles(t *testing.T) {
	rawDiff := `diff --git a/newfile.go b/newfile.go
new file mode 100644
--- /dev/null
+++ b/newfile.go
@@ -0,0 +1,5 @@
+package main
+func Hello() string { return "world" }
`
	files, err := git.ParseDiffSummary(rawDiff)
	if err != nil {
		t.Fatalf("unexpected error parsing diff: %v", err)
	}
	if len(files) != 1 || files[0].Path != "newfile.go" || files[0].Additions != 2 {
		t.Errorf("unexpected diff summary: %+v", files)
	}
	if files[0].Status != "added" {
		t.Errorf("expected status 'added', got %q", files[0].Status)
	}
}

func TestDiffParserDeletedAndModifiedFiles(t *testing.T) {
	rawDiff := `diff --git a/deleted.txt b/deleted.txt
deleted file mode 100644
--- a/deleted.txt
+++ /dev/null
@@ -1,3 +0,0 @@
-line 1
-line 2
-line 3
diff --git a/modified.txt b/modified.txt
--- a/modified.txt
+++ b/modified.txt
@@ -1,2 +1,2 @@
-old
+new
`
	files, err := git.ParseDiffSummary(rawDiff)
	if err != nil {
		t.Fatalf("unexpected error parsing diff: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	if files[0].Path != "deleted.txt" || files[0].Status != "deleted" || files[0].Deletions != 3 {
		t.Errorf("unexpected deleted file stat: %+v", files[0])
	}

	if files[1].Path != "modified.txt" || files[1].Status != "modified" || files[1].Additions != 1 || files[1].Deletions != 1 {
		t.Errorf("unexpected modified file stat: %+v", files[1])
	}
}

func TestDiffParserEmptyDiff(t *testing.T) {
	files, err := git.ParseDiffSummary("")
	if err != nil {
		t.Fatalf("unexpected error for empty diff: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestCaptureSnapshotIntegration(t *testing.T) {
	ctx := context.Background()
	repoPath := setupTestGitRepo(t)
	localWorktreeRoot := filepath.Join(t.TempDir(), "worktrees")
	wm := git.NewWorktreeManager("/srv/agent-tasks/worktrees", localWorktreeRoot)

	taskID := "task-diff-snapshot"
	taskBranch := "task/diff-snapshot-branch"

	localWorktreePath, _, _, err := wm.CreateWorktree(ctx, repoPath, taskID, taskBranch)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}
	defer func() {
		_ = wm.RemoveWorktree(ctx, repoPath, taskID)
	}()

	// In worktree:
	// 1. Create a new untracked file
	newFilePath := filepath.Join(localWorktreePath, "feature.go")
	if err := os.WriteFile(newFilePath, []byte("package feature\n\nfunc Feat() int { return 42 }\n"), 0644); err != nil {
		t.Fatalf("failed to write untracked file: %v", err)
	}

	// 2. Modify an existing file (README.md)
	readmePath := filepath.Join(localWorktreePath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n\nUpdated documentation.\n"), 0644); err != nil {
		t.Fatalf("failed to modify readme: %v", err)
	}

	// Capture snapshot against base branch "main"
	result, err := git.CaptureSnapshot(ctx, localWorktreePath, "main")
	if err != nil {
		t.Fatalf("CaptureSnapshot failed: %v", err)
	}

	if !result.HasChanges {
		t.Errorf("expected HasChanges to be true")
	}
	if result.TotalAdditions <= 0 {
		t.Errorf("expected TotalAdditions > 0, got %d", result.TotalAdditions)
	}
	if len(result.Files) != 2 {
		t.Fatalf("expected 2 files in diff, got %d: %+v", len(result.Files), result.Files)
	}

	foundFeature := false
	foundReadme := false
	for _, f := range result.Files {
		if f.Path == "feature.go" {
			foundFeature = true
			if f.Status != "added" {
				t.Errorf("expected feature.go status 'added', got %s", f.Status)
			}
			if f.Additions != 3 {
				t.Errorf("expected feature.go additions 3, got %d", f.Additions)
			}
		}
		if f.Path == "README.md" {
			foundReadme = true
			if f.Status != "modified" {
				t.Errorf("expected README.md status 'modified', got %s", f.Status)
			}
		}
	}

	if !foundFeature {
		t.Errorf("feature.go not found in files summary: %+v", result.Files)
	}
	if !foundReadme {
		t.Errorf("README.md not found in files summary: %+v", result.Files)
	}

	// Capture snapshot when there are no new changes (clean state)
	result2, err := git.CaptureSnapshot(ctx, localWorktreePath, "main")
	if err != nil {
		t.Fatalf("CaptureSnapshot second run failed: %v", err)
	}
	if !result2.HasChanges || len(result2.Files) != 2 {
		t.Errorf("expected second snapshot to retain diff against base branch main, got: %+v", result2)
	}
}

func TestCaptureSnapshotDeletedFile(t *testing.T) {
	ctx := context.Background()
	repoPath := setupTestGitRepo(t)
	localWorktreeRoot := filepath.Join(t.TempDir(), "worktrees")
	wm := git.NewWorktreeManager("/srv/agent-tasks/worktrees", localWorktreeRoot)

	taskID := "task-diff-deleted"
	taskBranch := "task/diff-deleted-branch"

	localWorktreePath, _, _, err := wm.CreateWorktree(ctx, repoPath, taskID, taskBranch)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}
	defer func() {
		_ = wm.RemoveWorktree(ctx, repoPath, taskID)
	}()

	// Delete README.md without git rm
	readmePath := filepath.Join(localWorktreePath, "README.md")
	if err := os.Remove(readmePath); err != nil {
		t.Fatalf("failed to delete readme: %v", err)
	}

	result, err := git.CaptureSnapshot(ctx, localWorktreePath, "main")
	if err != nil {
		t.Fatalf("CaptureSnapshot failed: %v", err)
	}

	if !result.HasChanges {
		t.Errorf("expected HasChanges true")
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
	if result.Files[0].Path != "README.md" || result.Files[0].Status != "deleted" {
		t.Errorf("expected deleted README.md, got %+v", result.Files[0])
	}
	if result.TotalDeletions <= 0 {
		t.Errorf("expected deletions > 0, got %d", result.TotalDeletions)
	}
}
