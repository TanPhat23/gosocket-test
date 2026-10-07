package git_test

import (
	"context"
	"strings"
	"testing"

	"agent-task-manager/internal/git"
)

func TestSyncGitHubRepo_ValidationAndScrubbing(t *testing.T) {
	ctx := context.Background()

	// Empty repoPath
	err := git.SyncGitHubRepo(ctx, "", "owner/repo", "main", "secret", nil)
	if err == nil {
		t.Fatalf("expected error for empty repoPath")
	}

	// Empty repoSlug
	err = git.SyncGitHubRepo(ctx, t.TempDir(), "", "main", "secret", nil)
	if err == nil {
		t.Fatalf("expected error for empty repoSlug")
	}

	// Invalid clone URL scrubbing
	secretToken := "ghp_sensitive_secret_token_12345"
	targetDir := t.TempDir()
	err = git.SyncGitHubRepo(ctx, targetDir, "invalid-owner-xyz-999/non-existent-repo-999", "main", secretToken, nil)
	if err == nil {
		t.Fatalf("expected error cloning non-existent repo")
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, secretToken) {
		t.Fatalf("SyncGitHubRepo leaked secretToken in error: %s", errMsg)
	}
}
