package git

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
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

	cmd := exec.CommandContext(
		pushCtx,
		"git",
		"-c",
		"http.extraHeader="+authHeader,
		"push",
		"-u",
		targetRemote,
		"--",
		fmt.Sprintf("%s:%s", branchName, branchName),
	)
	cmd.Dir = worktreeDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
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
