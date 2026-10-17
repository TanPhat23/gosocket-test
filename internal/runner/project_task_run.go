package runner

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/sandbox"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// This file is the project-container task run: the successor to the host-based runTaskDisabledDraft.
// Everything the agent touches (checkout, edits, verification, commit, diff) happens inside the project
// container; the control plane only talks to Docker, the container's OpenCode API and the git-transfer
// helper. RunTask does not call it while the Phase 1A runtime quarantine is in force.

const (
	containerExecOutputCap = 256 << 10
	diffBroadcastCap       = 1 << 20
	defaultSessionPoll     = 2 * time.Second
)

// hardenedGit prefixes every in-container git call: no hooks and no fsmonitor from an agent-writable repo.
var hardenedGit = []string{"git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}

type containerExecResult struct {
	ExitCode int
	Output   string
}

// cappedBuffer keeps at most max bytes and silently drops the rest.
type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := c.max - c.buf.Len(); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		c.buf.Write(p)
	}
	return n, nil
}

// execInContainer runs argv in a running container as the OpenCode user and returns its exit code and
// capped combined output. Cancelling ctx closes the stream and returns ctx's error.
func execInContainer(ctx context.Context, cli sandbox.DockerClient, containerID, workdir string, argv []string) (*containerExecResult, error) {
	created, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		User:         fmt.Sprintf("%d:%d", opencodeUID, opencodeUID),
		WorkingDir:   workdir,
		Cmd:          argv,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}
	hijacked, err := cli.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach exec: %w", err)
	}
	defer hijacked.Close()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			hijacked.Close()
		case <-done:
		}
	}()

	out := &cappedBuffer{max: containerExecOutputCap}
	if _, err := stdcopy.StdCopy(out, out, hijacked.Reader); err != nil && ctx.Err() == nil {
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	res, err := cli.ContainerExecInspect(ctx, created.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect exec: %w", err)
	}
	return &containerExecResult{ExitCode: res.ExitCode, Output: out.buf.String()}, nil
}

// runGit runs a hardened git command in dir and fails on a non-zero exit.
func (r *AgentTaskRunner) runGit(ctx context.Context, containerID, dir string, args ...string) (string, error) {
	res, err := r.runGitStatus(ctx, containerID, dir, args...)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("git %s exited with code %d: %s", args[0], res.ExitCode, strings.TrimSpace(res.Output))
	}
	return res.Output, nil
}

func (r *AgentTaskRunner) runGitStatus(ctx context.Context, containerID, dir string, args ...string) (*containerExecResult, error) {
	argv := append(append([]string{}, hardenedGit...), args...)
	return execInContainer(ctx, r.dockerCli, containerID, dir, argv)
}

// detectContainerVerifyCommand mirrors DetectVerificationCommand but reads the in-container worktree.
// ATM_VERIFY_CMD overrides detection, as on the host.
func (r *AgentTaskRunner) detectContainerVerifyCommand(ctx context.Context, containerID, dir string) (string, error) {
	if override := strings.TrimSpace(os.Getenv("ATM_VERIFY_CMD")); override != "" {
		return override, nil
	}
	const script = `if [ -f go.mod ]; then echo "go test ./..."; ` +
		`elif [ -f package.json ] && grep -q '"test"[[:space:]]*:' package.json; then echo "npm test"; ` +
		`elif [ -f Cargo.toml ]; then echo "cargo test"; ` +
		`elif [ -f Makefile ]; then echo "make test"; fi`
	res, err := execInContainer(ctx, r.dockerCli, containerID, dir, []string{"sh", "-c", script})
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("verification detection exited with code %d", res.ExitCode)
	}
	return strings.TrimSpace(res.Output), nil
}

// waitForSessionOutcome polls the container's OpenCode session until it reports a terminal outcome that is
// newer than before (the session's UpdatedAt prior to the prompt). Only "succeeded" is success.
func (r *AgentTaskRunner) waitForSessionOutcome(ctx context.Context, oc OpenCodeClient, sessionID string, before *time.Time) error {
	poll := r.sessionPoll
	if poll <= 0 {
		poll = defaultSessionPoll
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		sess, err := oc.GetSession(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("failed to poll session: %w", err)
		}
		if before != nil {
			if sess.IdleAt != nil {
				if !sess.IdleAt.After(*before) {
					continue
				}
			} else if sess.UpdatedAt == nil || !sess.UpdatedAt.After(*before) {
				continue
			}
		}
		if sess.Status == "running" {
			continue
		}
		switch sess.Status {
		case "succeeded":
			return nil
		case "failed", "interrupted":
			return fmt.Errorf("agent session %s", sess.Status)
		}
	}
}

// parseNumstat sums `git diff --numstat` output. Binary files ("-") count as a file with no line changes.
func parseNumstat(out string) (files, additions, deletions int) {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		files++
		if a, err := strconv.Atoi(parts[0]); err == nil {
			additions += a
		}
		if d, err := strconv.Atoi(parts[1]); err == nil {
			deletions += d
		}
	}
	return files, additions, deletions
}

func integrationBaseBranch(integ *models.ProjectIntegration) string {
	if integ.Config != nil {
		if b, ok := models.ConfigValue[string](integ.Config, "base_branch"); ok && b != "" {
			return b
		}
	}
	return "main"
}

func integrationAutoPR(integ *models.ProjectIntegration) bool {
	if integ.Config != nil {
		if v, ok := models.ConfigValue[bool](integ.Config, "auto_pr"); ok {
			return v
		}
	}
	return true
}

// resolveGitHubIntegration returns the task's enabled GitHub integration and its decrypted token. It is
// DB and crypto only, with no network, git or container access.
func (r *AgentTaskRunner) resolveGitHubIntegration(ctx context.Context, task *models.Task) (*models.ProjectIntegration, string, error) {
	integ, err := r.findGitHubIntegration(ctx, task)
	if err != nil {
		return nil, "", err
	}
	if r.credRepo == nil || r.cipher == nil {
		return nil, "", errors.New("GitHub integration is not configured")
	}
	cred, err := r.credRepo.GetByID(ctx, integ.CredentialID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load GitHub credential: %w", err)
	}
	token, err := r.cipher.Decrypt(cred.EncryptedData)
	if err != nil || token == "" {
		return nil, "", errors.New("failed to decrypt GitHub credential")
	}
	return integ, token, nil
}

// findGitHubIntegration returns the task's enabled GitHub integration without touching its credential.
func (r *AgentTaskRunner) findGitHubIntegration(ctx context.Context, task *models.Task) (*models.ProjectIntegration, error) {
	if r.integRepo == nil {
		return nil, errors.New("GitHub integration is not configured")
	}
	list, err := r.integRepo.ListByProjectID(ctx, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list integrations: %w", err)
	}
	var integ *models.ProjectIntegration
	if task.IntegrationID != nil && *task.IntegrationID != "" {
		for i := range list {
			if list[i].ID == *task.IntegrationID && list[i].Enabled {
				integ = &list[i]
				break
			}
		}
	}
	if integ == nil {
		for i := range list {
			if list[i].Provider == models.IntegrationGitHub && list[i].Enabled {
				integ = &list[i]
				break
			}
		}
	}
	if integ == nil {
		return nil, errors.New("project has no enabled GitHub integration")
	}
	return integ, nil
}

// projectRuntimeImage returns the image to run the project in: built or reused from the project's resolved
// runtime profile when a resolver and builder are set, otherwise the fixed projectImage. Repository manifests
// are not read yet, so the profile comes from the catalog baseline plus project.RuntimeConfig.
func (r *AgentTaskRunner) projectRuntimeImage(ctx context.Context, project *models.Project) (string, error) {
	if r.imageResolver == nil || r.imageBuilder == nil {
		return r.projectImage, nil
	}
	profile, err := r.imageResolver.ResolveMultiRepo(ctx, nil, project)
	if err != nil {
		return "", fmt.Errorf("failed to resolve runtime profile: %w", err)
	}
	tag, err := r.imageBuilder.BuildOrGetImage(ctx, profile)
	if err != nil {
		return "", fmt.Errorf("failed to build runtime image: %w", err)
	}
	return tag, nil
}

const injectConfigScript = `set -e
mkdir -p .claude
printf %s "$1" | base64 -d > opencode.json
printf %s "$2" | base64 -d > .claude/config.json
ex=$(git rev-parse --git-path info/exclude)
mkdir -p "$(dirname "$ex")"
printf '%s\n' /opencode.json /.claude/config.json >> "$ex"`

// injectTaskConfigInContainer writes the task's skills and MCP server names where the legacy draft wrote them
// (opencode.json, .claude/config.json), but inside the container worktree. It writes nothing for a task with
// neither, and keeps the files out of the commit via the repository's info/exclude. File content travels as a
// base64 argument, so nothing is interpolated into a shell command.
func (r *AgentTaskRunner) injectTaskConfigInContainer(ctx context.Context, containerID, worktree string, task *models.Task) error {
	if len(task.Skills) == 0 && len(task.MCPServers) == 0 {
		return nil
	}
	skills, mcp := task.Skills, task.MCPServers
	if skills == nil {
		skills = []string{}
	}
	if mcp == nil {
		mcp = []string{}
	}
	oc, err := json.Marshal(OpenCodeConfigFile{Skills: skills, MCPServers: mcp})
	if err != nil {
		return err
	}
	cl, err := json.Marshal(ClaudeConfigFile{Skills: skills, MCPServers: mcp})
	if err != nil {
		return err
	}
	res, err := execInContainer(ctx, r.dockerCli, containerID, worktree, []string{
		"sh", "-c", injectConfigScript, "sh",
		base64.StdEncoding.EncodeToString(oc), base64.StdEncoding.EncodeToString(cl),
	})
	if err != nil {
		return fmt.Errorf("failed to inject task config: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("task config injection exited with code %d: %s", res.ExitCode, strings.TrimSpace(res.Output))
	}
	return nil
}

// runTaskInProjectContainer runs one task end to end in the project container. It does not do the
// run bookkeeping (running/completed/done channels) that RunTask owns.
//
// Disabled pending Gate 0F and the explicit go-ahead to lift the quarantine; it is intentionally unreferenced.
func (r *AgentTaskRunner) runTaskInProjectContainer(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) error {
	if task == nil || attempt == nil {
		return errors.New("task and attempt are required")
	}
	if r.repoTransfer == nil || r.projectSandboxMgr == nil || r.dockerCli == nil || r.projectRepo == nil {
		return errors.New("project container runtime is not configured")
	}
	if r.projectImage == "" && (r.imageResolver == nil || r.imageBuilder == nil) {
		return errors.New("no project runtime image is configured")
	}
	project, err := r.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to lookup project %s: %w", task.ProjectID, err)
	}
	imageTag, err := r.projectRuntimeImage(ctx, project)
	if err != nil {
		return err
	}
	integ, token, err := r.resolveGitHubIntegration(ctx, task)
	if err != nil {
		return err
	}
	slug := integ.ExternalID
	branch := "task/" + task.ID

	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Fetching %s (%s) into the project volume...\r\n", slug, integrationBaseBranch(integ)))
	worktree, err := r.repoTransfer.IngestTaskBranch(ctx, project.ID, task.ID, slug, integrationBaseBranch(integ), token)
	if err != nil {
		return err
	}

	route, err := r.projectSandboxMgr.EnsureProjectContainer(ctx, project, imageTag)
	if err != nil {
		return fmt.Errorf("failed to ensure project container: %w", err)
	}
	newClient := r.projectClientFor
	if newClient == nil {
		newClient = func(url, pw string) (OpenCodeClient, error) { return NewProjectOpenCodeClient(url, pw, nil) }
	}
	oc, err := newClient(route.EndpointURL, route.AuthPassword)
	if err != nil {
		return fmt.Errorf("failed to initialize project opencode client: %w", err)
	}

	task.BranchName = &branch
	if r.taskRepo != nil {
		_ = r.taskRepo.Update(ctx, task)
	}
	sessionID := fmt.Sprintf("ses_atm_%s", task.ID)
	if task.SessionID != nil && *task.SessionID != "" {
		sessionID = *task.SessionID
	} else {
		task.SessionID = &sessionID
		if r.taskRepo != nil {
			_ = r.taskRepo.Update(ctx, task)
		}
	}
	if err := oc.EnsureSessionAtLocation(ctx, sessionID, task.Title, worktree); err != nil {
		return fmt.Errorf("failed to bind session to %s: %w", worktree, err)
	}

	if err := r.injectTaskConfigInContainer(ctx, route.ContainerID, worktree, task); err != nil {
		return err
	}

	baseSHA, err := r.runGit(ctx, route.ContainerID, worktree, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	baseSHA = strings.TrimSpace(baseSHA)

	maxIterations := 3
	timeoutSec := 120
	if r.cfg != nil {
		if r.cfg.MaxVerificationRetries > 0 {
			maxIterations = r.cfg.MaxVerificationRetries
		}
		if r.cfg.VerificationTimeoutSeconds > 0 {
			timeoutSec = r.cfg.VerificationTimeoutSeconds
		}
	}
	verifyCmd, err := r.detectContainerVerifyCommand(ctx, route.ContainerID, worktree)
	if err != nil {
		return err
	}

	r.broadcastLog(ctx, task.ID, attempt.ID, "\x1b[36m[Agent Control Plane]\x1b[0m Starting autonomous task execution in the project container...\r\n")
	prompt := task.Prompt
	streamSince := time.Now()
	streamSeen := map[string]bool{}
	for iteration := 1; iteration <= maxIterations; iteration++ {
		if iteration > 1 {
			r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\r\n\x1b[33m[Agent Control Plane] Starting self-repair turn (Iteration %d/%d)...\x1b[0m\r\n", iteration, maxIterations))
		}
		var before *time.Time
		if sess, err := oc.GetSession(ctx, sessionID); err == nil {
			before = sess.UpdatedAt
		}
		stopStream := r.startSessionStream(ctx, oc, task.ID, attempt.ID, sessionID, streamSince, streamSeen)
		if err := oc.PromptSession(ctx, sessionID, prompt); err != nil {
			stopStream()
			return fmt.Errorf("failed to prompt session: %w", err)
		}
		waitErr := r.waitForSessionOutcome(ctx, oc, sessionID, before)
		stopStream()
		if err := waitErr; err != nil {
			r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\r\n\x1b[31m[Agent Control Plane] Execution failed in iteration %d: %v\x1b[0m\r\n", iteration, err))
			return err
		}
		if verifyCmd == "" {
			r.broadcastLog(ctx, task.ID, attempt.ID, "\x1b[36m[Agent Control Plane]\x1b[0m No test suite detected. Skipping pre-flight verification.\r\n")
			break
		}
		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Running pre-flight verification: %s\r\n", verifyCmd))
		vCtx, vCancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		vRes, vErr := execInContainer(vCtx, r.dockerCli, route.ContainerID, worktree, []string{"sh", "-c", verifyCmd})
		vCancel()
		if vErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// A verification timeout counts as a failed run, not an infrastructure error.
			vRes = &containerExecResult{ExitCode: 1, Output: fmt.Sprintf("verification did not complete: %v", vErr)}
		}
		if vRes.ExitCode == 0 {
			r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[32m✔ Pre-flight verification passed on iteration %d.\x1b[0m\r\n", iteration))
			break
		}
		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[31m✖ Pre-flight verification failed on iteration %d/%d:\x1b[0m\r\n%s\r\n", iteration, maxIterations, vRes.Output))
		if iteration == maxIterations {
			failReason := fmt.Sprintf("pre-flight verification failed after %d iterations:\n%s", maxIterations, vRes.Output)
			attempt.FailureReason = &failReason
			return errors.New(failReason)
		}
		prompt = FormatVerificationFeedbackPrompt(iteration, maxIterations, vRes.Output)
	}

	// Commit whatever the agent left, so the transfer helper can export task/<id> from the volume.
	if _, err := r.runGit(ctx, route.ContainerID, worktree, "add", "-A"); err != nil {
		return err
	}
	staged, err := r.runGitStatus(ctx, route.ContainerID, worktree, "diff", "--cached", "--quiet")
	if err != nil {
		return err
	}
	switch staged.ExitCode {
	case 0:
		r.broadcastLog(ctx, task.ID, attempt.ID, "\x1b[33m[Agent Control Plane]\x1b[0m The agent made no changes; nothing to push.\r\n")
		return nil
	case 1:
	default:
		return fmt.Errorf("git diff --cached exited with code %d", staged.ExitCode)
	}
	msg := task.Title
	if msg == "" {
		msg = "Task " + task.ID
	}
	if _, err := r.runGit(ctx, route.ContainerID, worktree,
		"-c", "user.name=Agent Task Manager", "-c", "user.email=agent@task-manager.invalid",
		"commit", "-q", "-m", msg); err != nil {
		return err
	}

	numstat, err := r.runGit(ctx, route.ContainerID, worktree, "diff", "--no-ext-diff", "--no-textconv", "--numstat", baseSHA, "HEAD")
	if err != nil {
		return err
	}
	files, adds, dels := parseNumstat(numstat)
	if r.hub != nil {
		if raw, err := r.runGit(ctx, route.ContainerID, worktree, "diff", "--no-ext-diff", "--no-textconv", "--no-color", baseSHA, "HEAD"); err == nil {
			if len(raw) > diffBroadcastCap {
				raw = raw[:diffBroadcastCap]
			}
			_ = r.hub.BroadcastTaskDiff(task.ID, raw)
		}
	}
	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\r\n\x1b[32m✔ Snapshot captured: %d files changed (+%d/-%d)\x1b[0m\r\n", files, adds, dels))

	if !integrationAutoPR(integ) || r.ghProvider == nil {
		r.broadcastLog(ctx, task.ID, attempt.ID, "\x1b[32m✔ Autonomous task run finished. Ready for review.\x1b[0m\r\n")
		return nil
	}
	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Pushing branch %s to %s...\r\n", branch, slug))
	if err := r.repoTransfer.PushTaskBranch(ctx, project.ID, task.ID, slug, token); err != nil {
		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[31m[Agent Control Plane] Git push failed: %v\x1b[0m\r\n", err))
		return err
	}
	r.openTaskPullRequest(ctx, task, attempt, integ, token, branch, files, adds, dels)
	r.broadcastLog(ctx, task.ID, attempt.ID, "\x1b[32m✔ Autonomous task run finished. Ready for review.\x1b[0m\r\n")
	return nil
}
