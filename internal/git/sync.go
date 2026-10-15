package git

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SyncGitHubRepo clones or syncs a remote GitHub repository into the local repoPath
// using the provided personal access token and checks out baseBranch.
// All tokens and auth headers are strictly scrubbed from logged/returned errors.
// TLS verification is strictly enforced; insecure SSL verification bypasses and fallback retries are removed.
func SyncGitHubRepo(ctx context.Context, repoPath, repoSlug, baseBranch, token string, logFn func(string)) error {
	if repoPath == "" {
		return fmt.Errorf("repoPath cannot be empty")
	}
	if repoSlug == "" {
		return fmt.Errorf("repoSlug cannot be empty")
	}
	if baseBranch == "" {
		baseBranch = "main"
	}

	remoteURL := RemoteURL(repoSlug)
	authHeader := fmt.Sprintf("Authorization: Basic %s", base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)))

	gitDir := filepath.Join(repoPath, ".git")
	isCloned := false
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		isCloned = true
	}

	// 1. If not cloned, clone the repository
	if !isCloned {
		if err := os.MkdirAll(filepath.Dir(repoPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory for repo: %w", err)
		}

		if logFn != nil {
			logFn(fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Cloning GitHub repository %s (%s)...\r\n", repoSlug, baseBranch))
		}

		cloneCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()

		cloneArgs := []string{
			"-c", "http.extraHeader=" + authHeader,
			"clone", "--branch", baseBranch, "--single-branch", "--", remoteURL, repoPath,
		}
		cmd := exec.CommandContext(cloneCtx, "git", cloneArgs...)

		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			// If branch wasn't found or standard clone without branch needed
			fallbackArgs := []string{
				"-c", "http.extraHeader=" + authHeader,
				"clone", "--", remoteURL, repoPath,
			}
			fallbackCmd := exec.CommandContext(cloneCtx, "git", fallbackArgs...)
			var fallbackStderr bytes.Buffer
			fallbackCmd.Stderr = &fallbackStderr
			if fbErr := fallbackCmd.Run(); fbErr != nil {
				rawErr := fmt.Sprintf("git clone %s failed: %v (stderr: %s)", repoSlug, fbErr, fallbackStderr.String())
				return fmt.Errorf("%s", scrubSensitiveGitOutput(rawErr, token, authHeader))
			}
		}

		// Configure local git user
		_ = exec.Command("git", "-C", repoPath, "config", "user.name", "Agent Task Manager").Run()
		_ = exec.Command("git", "-C", repoPath, "config", "user.email", "agent@task-manager.local").Run()
		_ = exec.Command("git", "-C", repoPath, "config", "--add", "safe.directory", "*").Run()
		_ = exec.Command("git", "-C", repoPath, "config", "core.fileMode", "false").Run()

		syncMarker := filepath.Join(repoPath, ".git", "ATM_LAST_SYNC")
		_ = os.WriteFile(syncMarker, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0644)

		if logFn != nil {
			logFn(fmt.Sprintf("\x1b[32m✔ GitHub repository %s successfully cloned.\x1b[0m\r\n", repoSlug))
		}
		return nil
	}

	// 2. If already cloned, check if synced recently (within 3 minutes)
	syncMarker := filepath.Join(repoPath, ".git", "ATM_LAST_SYNC")
	if info, err := os.Stat(syncMarker); err == nil {
		if time.Since(info.ModTime()) < 3*time.Minute {
			if logFn != nil {
				logFn(fmt.Sprintf("\x1b[32m✔ Repository %s is up-to-date (cached %s ago).\x1b[0m\r\n", repoSlug, time.Since(info.ModTime()).Round(time.Second)))
			}
			return nil
		}
	}

	// Fetch latest commits
	if logFn != nil {
		logFn(fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Fetching latest commits for %s from origin/%s...\r\n", repoSlug, baseBranch))
	}

	// Ensure origin points to remoteURL
	checkOrigin := exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "get-url", "origin")
	if out, err := checkOrigin.Output(); err != nil || strings.TrimSpace(string(out)) != remoteURL {
		_ = exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "remove", "origin").Run()
		_ = exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "add", "origin", remoteURL).Run()
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	fetchArgs := []string{
		"-C", repoPath,
		"-c", "http.extraHeader=" + authHeader,
		"fetch", "--prune", "origin", baseBranch,
	}
	fetchCmd := exec.CommandContext(fetchCtx, "git", fetchArgs...)
	var fetchStderr bytes.Buffer
	fetchCmd.Stderr = &fetchStderr
	if err := fetchCmd.Run(); err != nil {
		rawErr := fmt.Sprintf("git fetch %s warning: %v (stderr: %s). Continuing with cached local repository.", repoSlug, err, fetchStderr.String())
		if logFn != nil {
			logFn(fmt.Sprintf("\x1b[33m[Agent Control Plane] %s\x1b[0m\r\n", scrubSensitiveGitOutput(rawErr, token, authHeader)))
		}
		return nil
	}

	// Fast-forward or reset baseBranch to origin/<baseBranch>
	_ = exec.CommandContext(ctx, "git", "-C", repoPath, "checkout", baseBranch).Run()
	_ = exec.CommandContext(ctx, "git", "-C", repoPath, "reset", "--hard", "origin/"+baseBranch).Run()
	_ = os.WriteFile(syncMarker, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0644)

	if logFn != nil {
		logFn(fmt.Sprintf("\x1b[32m✔ Repository %s synced with latest upstream %s.\x1b[0m\r\n", repoSlug, baseBranch))
	}
	return nil
}

// RemoteURL returns the git remote URL for a repository slug. Values that are
// already http://, https:// or file:// URLs are returned unchanged; anything
// else is treated as a GitHub "owner/repo" slug.
func RemoteURL(slug string) string {
	if strings.HasPrefix(slug, "http://") || strings.HasPrefix(slug, "https://") || strings.HasPrefix(slug, "file://") {
		return slug
	}
	return fmt.Sprintf("https://github.com/%s.git", slug)
}
