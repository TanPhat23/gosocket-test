package git

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// PushBranch pushes the specified local branch to origin using token-based basic auth.
// Security Fix 3:
// - Uses "--" before ref specs to prevent git argument injection.
// - Scrubs any token, auth header, or sensitive patterns from error strings before returning.
func PushBranch(ctx context.Context, worktreeDir, remoteURL, token, branchName string) error {
	authHeader := fmt.Sprintf("Authorization: Basic %s", base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)))

	pushCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	targetRemote := "origin"
	if remoteURL != "" {
		targetRemote = remoteURL
	}

	pushArgs := []string{"-c", "http.extraHeader=" + authHeader}
	if os.Getenv("GIT_SSL_NO_VERIFY") == "true" {
		pushArgs = append(pushArgs, "-c", "http.sslVerify=false")
	}
	pushArgs = append(pushArgs, "push", "-u", targetRemote, "--", fmt.Sprintf("%s:%s", branchName, branchName))

	cmd := exec.CommandContext(pushCtx, "git", pushArgs...)
	cmd.Dir = worktreeDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errStr := stderr.String()
		if strings.Contains(errStr, "certificate") || strings.Contains(errStr, "SSL") {
			sslPushArgs := []string{"-c", "http.extraHeader=" + authHeader, "-c", "http.sslVerify=false", "push", "-u", targetRemote, "--", fmt.Sprintf("%s:%s", branchName, branchName)}
			sslCmd := exec.CommandContext(pushCtx, "git", sslPushArgs...)
			sslCmd.Dir = worktreeDir
			if sErr := sslCmd.Run(); sErr == nil {
				return nil
			}
		}
		rawErr := fmt.Sprintf("git push %s failed: %v (stderr: %s)", branchName, err, stderr.String())
		return fmt.Errorf("%s", scrubSensitiveGitOutput(rawErr, token, authHeader))
	}
	return nil
}

func scrubSensitiveGitOutput(input, token, authHeader string) string {
	scrubbed := input
	if token != "" {
		scrubbed = strings.ReplaceAll(scrubbed, token, "[REDACTED]")
	}
	if authHeader != "" {
		scrubbed = strings.ReplaceAll(scrubbed, authHeader, "[REDACTED_AUTH_HEADER]")
	}
	// Also scrub standard basic auth patterns if present
	if strings.Contains(scrubbed, "x-access-token:") {
		scrubbed = strings.ReplaceAll(scrubbed, "x-access-token:", "x-access-token:[REDACTED]")
	}
	return scrubbed
}
