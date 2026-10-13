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

	sslFlags := []string{}
	if os.Getenv("GIT_SSL_NO_VERIFY") == "true" {
		sslFlags = append(sslFlags, "-c", "http.sslVerify=false")
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

		cloneArgs := append([]string{"-c", "http.extraHeader=" + authHeader}, sslFlags...)
		cloneArgs = append(cloneArgs, "clone", "--branch", baseBranch, "--single-branch", "--", remoteURL, repoPath)
		cmd := exec.CommandContext(cloneCtx, "git", cloneArgs...)

		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			errStr := stderr.String()
			// If SSL certificate error, retry with SSL verification disabled
			if strings.Contains(errStr, "certificate") || strings.Contains(errStr, "SSL") {
				retryArgs := []string{"-c", "http.extraHeader=" + authHeader, "-c", "http.sslVerify=false", "clone", "--branch", baseBranch, "--single-branch", "--", remoteURL, repoPath}
				retryCmd := exec.CommandContext(cloneCtx, "git", retryArgs...)
				var retryStderr bytes.Buffer
				retryCmd.Stderr = &retryStderr
				if rErr := retryCmd.Run(); rErr == nil {
					goto clonePost
				}
			}

			// If branch wasn't found or standard clone without branch needed
			fallbackArgs := append([]string{"-c", "http.extraHeader=" + authHeader}, sslFlags...)
			fallbackArgs = append(fallbackArgs, "clone", "--", remoteURL, repoPath)
			fallbackCmd := exec.CommandContext(cloneCtx, "git", fallbackArgs...)
			var fallbackStderr bytes.Buffer
			fallbackCmd.Stderr = &fallbackStderr
			if fbErr := fallbackCmd.Run(); fbErr != nil {
				// Retry fallback with sslVerify=false if SSL issue
				if strings.Contains(fallbackStderr.String(), "certificate") || strings.Contains(fallbackStderr.String(), "SSL") {
					sslFallbackArgs := []string{"-c", "http.extraHeader=" + authHeader, "-c", "http.sslVerify=false", "clone", "--", remoteURL, repoPath}
					sslFallbackCmd := exec.CommandContext(cloneCtx, "git", sslFallbackArgs...)
					var sslFallbackStderr bytes.Buffer
					sslFallbackCmd.Stderr = &sslFallbackStderr
					if sfErr := sslFallbackCmd.Run(); sfErr == nil {
						goto clonePost
					}
				}
				rawErr := fmt.Sprintf("git clone %s failed: %v (stderr: %s)", repoSlug, fbErr, fallbackStderr.String())
				return fmt.Errorf("%s", scrubSensitiveGitOutput(rawErr, token, authHeader))
			}
		}

	clonePost:

		// Configure local git user
		_ = exec.Command("git", "-C", repoPath, "config", "user.name", "Agent Task Manager").Run()
		_ = exec.Command("git", "-C", repoPath, "config", "user.email", "agent@task-manager.local").Run()
		_ = exec.Command("git", "-C", repoPath, "config", "--add", "safe.directory", "*").Run()
		_ = exec.Command("git", "-C", repoPath, "config", "core.fileMode", "false").Run()
		_ = exec.Command("chmod", "-R", "0777", repoPath).Run()

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

	fetchArgs := append([]string{"-C", repoPath, "-c", "http.extraHeader=" + authHeader}, sslFlags...)
	fetchArgs = append(fetchArgs, "fetch", "--prune", "origin", baseBranch)
	fetchCmd := exec.CommandContext(fetchCtx, "git", fetchArgs...)
	var fetchStderr bytes.Buffer
	fetchCmd.Stderr = &fetchStderr
	if err := fetchCmd.Run(); err != nil {
		errStr := fetchStderr.String()
		if strings.Contains(errStr, "certificate") || strings.Contains(errStr, "SSL") {
			sslFetchArgs := []string{"-C", repoPath, "-c", "http.extraHeader=" + authHeader, "-c", "http.sslVerify=false", "fetch", "--prune", "origin", baseBranch}
			sslFetchCmd := exec.CommandContext(fetchCtx, "git", sslFetchArgs...)
			if sfErr := sslFetchCmd.Run(); sfErr == nil {
				goto fetchPost
			}
		}
		rawErr := fmt.Sprintf("git fetch %s warning: %v (stderr: %s). Continuing with cached local repository.", repoSlug, err, fetchStderr.String())
		if logFn != nil {
			logFn(fmt.Sprintf("\x1b[33m[Agent Control Plane] %s\x1b[0m\r\n", scrubSensitiveGitOutput(rawErr, token, authHeader)))
		}
		return nil
	}

fetchPost:

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
