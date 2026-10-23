package git_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

func TestGitTLSVerificationEnforced(t *testing.T) {
	// Intercept git commands by putting a mock git binary in PATH that records invocation arguments
	tmpBinDir := t.TempDir()
	logFile := filepath.Join(tmpBinDir, "git_args.log")

	// Wrapper script that logs all arguments passed to git and simulates an SSL error on clone
	gitWrapper := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %s
for arg in "$@"; do
    if [ "$arg" = "http.sslVerify=false" ]; then
        echo "SECURITY ERROR: http.sslVerify=false detected in arguments" >&2
        exit 88
    fi
done
# If cloning, output SSL error to simulate certificate problem
case "$*" in
    *clone*)
        echo "fatal: unable to access 'https://github.com/test/repo': server certificate verification failed" >&2
        exit 128
        ;;
    *)
        exec /usr/bin/git "$@"
        ;;
esac
`, logFile)

	if err := os.WriteFile(filepath.Join(tmpBinDir, "git"), []byte(gitWrapper), 0755); err != nil {
		t.Fatalf("failed to write git wrapper: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpBinDir+":"+origPath)
	t.Setenv("GIT_SSL_NO_VERIFY", "true")

	ctx := context.Background()
	secretToken := "ghp_sensitive_secret_token_12345"
	targetDir := filepath.Join(t.TempDir(), "target-repo")

	err := git.SyncGitHubRepo(ctx, targetDir, "test/repo", "main", secretToken, nil)
	if err == nil {
		t.Fatalf("expected error from simulated SSL certificate failure, got nil")
	}

	// Verify error was scrubbed
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("secret token was leaked in error: %s", err.Error())
	}

	// Read logFile to ensure http.sslVerify=false was never passed in any argument
	logData, readErr := os.ReadFile(logFile)
	if readErr == nil {
		if strings.Contains(string(logData), "http.sslVerify=false") {
			t.Fatalf("git was invoked with http.sslVerify=false despite hardening specification: %s", string(logData))
		}
	}
}

func TestGitSSLNoVerifyIgnored(t *testing.T) {
	tmpBinDir := t.TempDir()
	logFile := filepath.Join(tmpBinDir, "git_args.log")

	// Wrapper script that logs all arguments and simulates an SSL error on push
	gitWrapper := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %s
for arg in "$@"; do
    if [ "$arg" = "http.sslVerify=false" ]; then
        echo "SECURITY ERROR: http.sslVerify=false detected in arguments" >&2
        exit 88
    fi
done
case "$*" in
    *push*)
        echo "fatal: unable to access 'https://github.com/fake/repo': SSL certificate problem: self signed certificate" >&2
        exit 128
        ;;
    *)
        exec /usr/bin/git "$@"
        ;;
esac
`, logFile)

	if err := os.WriteFile(filepath.Join(tmpBinDir, "git"), []byte(gitWrapper), 0755); err != nil {
		t.Fatalf("failed to write git wrapper: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpBinDir+":"+origPath)
	t.Setenv("GIT_SSL_NO_VERIFY", "true")

	ctx := context.Background()
	secretToken := "ghp_super_secret_pat_9876543210"
	worktreeDir := t.TempDir()

	err := git.PushBranch(ctx, worktreeDir, "https://github.com/fake/repo", secretToken, "task/xyz")
	if err == nil {
		t.Fatalf("expected error from push failure, got nil")
	}

	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("secret token leaked in push error: %s", err.Error())
	}

	// Check git invocation log
	logData, readErr := os.ReadFile(logFile)
	if readErr == nil {
		if strings.Contains(string(logData), "http.sslVerify=false") {
			t.Fatalf("PushBranch invoked git with http.sslVerify=false: %s", string(logData))
		}
	}
}
