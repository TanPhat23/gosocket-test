package git_test

import (
	"context"
	"strings"
	"testing"

	"agent-task-manager/internal/git"
)

func TestPushBranch_NonGitDirAndScrubbing(t *testing.T) {
	ctx := context.Background()
	secretToken := "ghp_super_secret_pat_9876543210"
	err := git.PushBranch(ctx, t.TempDir(), "https://github.com/fake/repo", secretToken, "task/xyz")
	if err == nil {
		t.Fatalf("expected error running push in non-git directory, got nil")
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, secretToken) {
		t.Fatalf("error message leaked secret token! err: %s", errMsg)
	}
}
