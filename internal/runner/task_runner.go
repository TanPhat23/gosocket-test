package runner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"agent-task-manager/internal/config"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/git"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/sandbox"
	"agent-task-manager/internal/ws"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// RunnerOption configures optional dependencies for AgentTaskRunner.
type RunnerOption func(*AgentTaskRunner)

// WithRunnerGitHubIntegration sets the integration repo, cred repo, cipher, and github provider for auto-PR creation.
func WithRunnerGitHubIntegration(
	integRepo repository.IntegrationRepository,
	credRepo repository.CredentialRepository,
	cipher *crypto.SecretCipher,
	ghProvider integrations.PullRequestProvider,
) RunnerOption {
	return func(r *AgentTaskRunner) {
		r.integRepo = integRepo
		r.credRepo = credRepo
		r.cipher = cipher
		r.ghProvider = ghProvider
	}
}
type AgentTaskRunner struct {
	cfg          *config.Config
	projectRepo  repository.ProjectRepository
	taskRepo     repository.TaskRepository
	logRepo      repository.LogRepository
	worktreeMgr  *git.WorktreeManager
	dockerRunner *sandbox.Runner
	dockerCli    sandbox.DockerClient
	hub          *ws.Hub
	integRepo    repository.IntegrationRepository
	credRepo     repository.CredentialRepository
	cipher       *crypto.SecretCipher
	ghProvider   integrations.PullRequestProvider
}

// NewAgentTaskRunner creates a new AgentTaskRunner.
func NewAgentTaskRunner(
	cfg *config.Config,
	projectRepo repository.ProjectRepository,
	taskRepo repository.TaskRepository,
	logRepo repository.LogRepository,
	worktreeMgr *git.WorktreeManager,
	dockerRunner *sandbox.Runner,
	dockerCli sandbox.DockerClient,
	hub *ws.Hub,
	opts ...RunnerOption,
) *AgentTaskRunner {
	runner := &AgentTaskRunner{
		cfg:          cfg,
		projectRepo:  projectRepo,
		taskRepo:     taskRepo,
		logRepo:      logRepo,
		worktreeMgr:  worktreeMgr,
		dockerRunner: dockerRunner,
		dockerCli:    dockerCli,
		hub:          hub,
	}
	for _, opt := range opts {
		opt(runner)
	}
	return runner
}

// EnsureGitRepo verifies that a valid Git repository exists at repoPath with an initial commit, creating one if missing.
func EnsureGitRepo(repoPath string) error {
	_ = exec.Command("git", "config", "--global", "--add", "safe.directory", "*").Run()

	if repoPath == "" {
		return errors.New("repoPath cannot be empty")
	}

	gitDir := filepath.Join(repoPath, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		return nil
	}

	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return fmt.Errorf("failed to create repo directory %s: %w", repoPath, err)
	}

	initCmd := exec.Command("git", "init", "-b", "main")
	initCmd.Dir = repoPath
	if out, err := initCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init failed: %w (output: %s)", err, string(out))
	}

	cfgName := exec.Command("git", "config", "user.name", "Agent Task Manager")
	cfgName.Dir = repoPath
	_ = cfgName.Run()

	cfgEmail := exec.Command("git", "config", "user.email", "agent@task-manager.local")
	cfgEmail.Dir = repoPath
	_ = cfgEmail.Run()

	commitCmd := exec.Command("git", "commit", "--allow-empty", "-m", "Initial repository commit")
	commitCmd.Dir = repoPath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("initial commit failed: %w (output: %s)", err, string(out))
	}

	return nil
}

// SyncRepoWithRemote checks if repoPath has a git remote configured (such as origin)
// and fetches the latest commits to ensure newly branched worktrees start from the freshest upstream code.
func SyncRepoWithRemote(ctx context.Context, repoPath string, logFn func(string)) error {
	if repoPath == "" {
		return nil
	}

	// 1. Check if any remote is configured
	checkCmd := exec.CommandContext(ctx, "git", "remote")
	checkCmd.Dir = repoPath
	out, err := checkCmd.Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return nil
	}

	remotes := strings.Fields(string(out))
	targetRemote := "origin"
	foundTarget := false
		if ok := slices.Contains(remotes, "origin"); ok {
			targetRemote = "origin"
			foundTarget = true
		}
	
	if !foundTarget && len(remotes) > 0 {
		targetRemote = remotes[0]
	}

	if logFn != nil {
		logFn(fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Syncing with remote %s upstream...\r\n", targetRemote))
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	fetchCmd := exec.CommandContext(fetchCtx, "git", "fetch", "--prune", targetRemote)
	fetchCmd.Dir = repoPath
	var fetchStderr bytes.Buffer
	fetchCmd.Stderr = &fetchStderr

	if err := fetchCmd.Run(); err != nil {
		errMsg := strings.TrimSpace(fetchStderr.String())
		if logFn != nil {
			logFn(fmt.Sprintf("\x1b[33m[Agent Control Plane] Remote fetch warning: %s. Continuing with local commits.\x1b[0m\r\n", errMsg))
		}
		return fmt.Errorf("git fetch %s failed: %w", targetRemote, err)
	}

	if logFn != nil {
		logFn(fmt.Sprintf("\x1b[32m[Agent Control Plane] ✔ Repository successfully synced with remote %s.\x1b[0m\r\n", targetRemote))
	}

	return nil
}

// RunTask executes an individual task attempt in an isolated worktree and runs OpenCode.
func (r *AgentTaskRunner) RunTask(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) error {
	if task == nil {
		return errors.New("task cannot be nil")
	}

	project, err := r.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to lookup project %s: %w", task.ProjectID, err)
	}

	repoPath := project.RepoPath
	if repoPath == "" {
		repoPath = filepath.Join(r.cfg.HostRepoRoot, "core")
	}

	if err := EnsureGitRepo(repoPath); err != nil {
		return fmt.Errorf("failed to ensure git repo at %s: %w", repoPath, err)
	}

	// Sync with remote GitHub repo if a remote is configured
	_ = SyncRepoWithRemote(ctx, repoPath, func(msg string) {
		r.broadcastLog(ctx, task.ID, attempt.ID, msg)
	})

	branchName := fmt.Sprintf("task/%s", task.ID)
	localWorktreePath, hostWorktreePath, _, err := r.worktreeMgr.CreateWorktree(ctx, repoPath, task.ID, branchName)
	if err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	// Guarantee full 0777 permissions across worktree so host OpenCode user (e.g. uid 1000) has write access
	_ = exec.CommandContext(ctx, "chmod", "-R", "0777", localWorktreePath).Run()

	task.WorktreePath = &hostWorktreePath
	task.BranchName = &branchName
	_ = r.taskRepo.Update(ctx, task)

	attempt.WorktreePath = &hostWorktreePath
	attempt.BranchName = &branchName

	if err := InjectSandboxedConfig(localWorktreePath, task.Skills, task.MCPServers); err != nil {
		log.Printf("warning: failed to inject sandboxed configs into worktree: %v", err)
	}

	selectedModel := "google/gemini-3.8-flash"
	if task.Model != nil && *task.Model != "" {
		selectedModel = *task.Model
	}

	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Starting autonomous task execution...\r\n"))
	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Model: %s\r\n", selectedModel))
	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Worktree: %s\r\n\r\n", hostWorktreePath))

	var execErr error
	if r.dockerRunner != nil && r.dockerCli != nil {
		execErr = r.runInDocker(ctx, task, attempt, hostWorktreePath, selectedModel)
	} else {
		execErr = r.runOnHost(ctx, task, attempt, localWorktreePath, selectedModel)
	}

	if execErr != nil {
		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\r\n\x1b[31m[Agent Control Plane] Execution failed: %v\x1b[0m\r\n", execErr))
		return execErr
	}

	diffRes, diffErr := git.CaptureSnapshot(ctx, localWorktreePath, "main")
	if diffErr == nil && diffRes != nil {
		if r.hub != nil {
			_ = r.hub.BroadcastTaskDiff(task.ID, diffRes.RawDiff)
		}
		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\r\n\x1b[32m✔ Snapshot captured: %d files changed (+%d/-%d)\x1b[0m\r\n",
			len(diffRes.Files), diffRes.TotalAdditions, diffRes.TotalDeletions))
	} else if diffErr != nil {
		log.Printf("warning: failed to capture snapshot diff: %v", diffErr)
	}

	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[32m✔ Autonomous task run finished. Ready for review.\x1b[0m\r\n"))

	// Auto-PR Creation
	r.maybeAutoCreatePR(ctx, task, attempt, localWorktreePath, diffRes)

	return nil
}

func (r *AgentTaskRunner) maybeAutoCreatePR(
	ctx context.Context,
	task *models.Task,
	attempt *models.TaskAttempt,
	localWorktreePath string,
	diffRes *git.Result,
) {
	if r.integRepo == nil || r.credRepo == nil || r.cipher == nil || r.ghProvider == nil {
		return
	}

	integrationsList, err := r.integRepo.ListByProjectID(ctx, task.ProjectID)
	if err != nil || len(integrationsList) == 0 {
		return
	}

	var ghInteg *models.ProjectIntegration
	for i := range integrationsList {
		if integrationsList[i].Provider == models.IntegrationGitHub && integrationsList[i].Enabled {
			ghInteg = &integrationsList[i]
			break
		}
	}
	if ghInteg == nil {
		return
	}

	// Check auto_pr flag
	autoPR := true
	if ghInteg.Config != nil {
		if v, ok := ghInteg.Config["auto_pr"].(bool); ok {
			autoPR = v
		}
	}
	if !autoPR {
		return
	}

	// Fetch & Decrypt PAT
	cred, err := r.credRepo.GetByID(ctx, ghInteg.CredentialID)
	if err != nil {
		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[33m[Agent Control Plane] Auto-PR skipped: credential lookup failed: %v\x1b[0m\r\n", err))
		return
	}

	token, err := r.cipher.Decrypt(cred.EncryptedData)
	if err != nil {
		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[33m[Agent Control Plane] Auto-PR skipped: failed to decrypt PAT: %v\x1b[0m\r\n", err))
		return
	}

	branchName := fmt.Sprintf("task/%s", task.ID)
	remoteURL := fmt.Sprintf("https://github.com/%s.git", ghInteg.ExternalID)

	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Pushing branch %s to remote %s...\r\n", branchName, ghInteg.ExternalID))
	if err := git.PushBranch(ctx, localWorktreePath, remoteURL, token, branchName); err != nil {
		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[31m[Agent Control Plane] Git push failed: %v\x1b[0m\r\n", err))
		return
	}
	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[32m✔ Branch pushed successfully to remote.\x1b[0m\r\n"))

	// Construct PR title & body
	prTitle := task.Title
	if prTitle == "" {
		prTitle = fmt.Sprintf("Task #%s", task.ID)
	}

	filesChanged := 0
	additions := 0
	deletions := 0
	if diffRes != nil {
		filesChanged = len(diffRes.Files)
		additions = diffRes.TotalAdditions
		deletions = diffRes.TotalDeletions
	}

	var prBody strings.Builder
	prBody.WriteString(fmt.Sprintf("## Summary\nAutonomous implementation generated by Agent Task Manager for task `#%s`.\n\n", task.ID))
	prBody.WriteString(fmt.Sprintf("**Prompt**:\n> %s\n\n", task.Prompt))
	prBody.WriteString(fmt.Sprintf("## Changes\n- %d files changed (+%d / -%d)\n\n", filesChanged, additions, deletions))
	if task.ExternalID != nil && *task.ExternalID != "" {
		prBody.WriteString(fmt.Sprintf("Resolves #%s\n", *task.ExternalID))
	}

	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Creating GitHub Pull Request...\r\n"))
	prURL, err := r.ghProvider.CreatePullRequest(ctx, ghInteg, token, branchName, prTitle, prBody.String())
	if err != nil {
		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[31m[Agent Control Plane] Failed to create Pull Request: %v\x1b[0m\r\n", err))
		return
	}

	task.PRURL = &prURL
	task.PullRequestURL = &prURL
	_ = r.taskRepo.Update(ctx, task)

	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[32m✔ Pull Request created: %s\x1b[0m\r\n", prURL))

	// If linked to an issue, post comment and update label
	if task.ExternalID != nil && *task.ExternalID != "" {
		if syncProv, ok := r.ghProvider.(integrations.SyncProvider); ok {
			commentMsg := fmt.Sprintf("Autonomous agent finished implementation. Pull Request: %s", prURL)
			_ = syncProv.AddComment(ctx, ghInteg, token, *task.ExternalID, commentMsg)
			_ = syncProv.UpdateIssueStatus(ctx, ghInteg, token, *task.ExternalID, "review:agent")
		}
	}
}

// BuildProcessEnv returns the process environment variables for an agent run.
func (r *AgentTaskRunner) BuildProcessEnv(task *models.Task, model string, homeDir string) []string {
	apiURL := os.Getenv("ATM_API_URL")
	if apiURL == "" {
		port := 8080
		if r.cfg != nil && r.cfg.Port > 0 {
			port = r.cfg.Port
		}
		apiURL = fmt.Sprintf("http://localhost:%d", port)
	}

	pw := ""
	if r.cfg != nil {
		pw = r.cfg.OpenCodePassword
	}

	return []string{
		"HOME=" + homeDir,
		"OPENCODE_PASSWORD=" + pw,
		"ATM_TASK_ID=" + task.ID,
		"ATM_PROJECT_ID=" + task.ProjectID,
		"ATM_API_URL=" + apiURL,
		"ATM_TASK_MODEL=" + model,
	}
}

func (r *AgentTaskRunner) runInDocker(ctx context.Context, task *models.Task, attempt *models.TaskAttempt, hostPath, model string) error {
	imageName := "debian:bookworm-slim"

	cmd := []string{
		"/root/.opencode/bin/opencode",
		"run",
		"--server", r.cfg.OpenCodeServerURL,
		"--model", model,
		"--auto",
		task.Prompt,
	}

	env := r.BuildProcessEnv(task, model, "/root")

	containerCfg := &container.Config{
		Image:        imageName,
		Cmd:          cmd,
		Env:          env,
		WorkingDir:   hostPath,
		AttachStdout: true,
		AttachStderr: true,
	}

	hostCfg := &container.HostConfig{
		NetworkMode: container.NetworkMode("host"),
		Binds: []string{
			fmt.Sprintf("%s:%s:rw", hostPath, hostPath),
			fmt.Sprintf("%s:/root/.opencode:ro", r.cfg.OpenCodeHomePath),
			fmt.Sprintf("%s:/root/.config/opencode:ro", r.cfg.OpenCodeConfigDir),
			fmt.Sprintf("%s:/root/.local/share/opencode:rw", r.cfg.OpenCodeDataDir),
		},
		Resources: container.Resources{
			Memory: 4 * 1024 * 1024 * 1024,
		},
	}

	resp, err := r.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, "")
	if err != nil {
		log.Printf("container create failed with debian:bookworm-slim (%v), falling back to host execution", err)
		return r.runOnHost(ctx, task, attempt, hostPath, model)
	}

	containerID := resp.ID
	defer func() {
		_ = r.dockerCli.ContainerRemove(context.Background(), containerID, container.RemoveOptions{Force: true})
	}()

	if err := r.dockerCli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start agent sandbox container: %w", err)
	}

	logsReader, err := r.dockerRunner.Logs(ctx, containerID)
	if err == nil {
		defer logsReader.Close()
		go r.streamDemuxedLogs(ctx, task.ID, attempt.ID, logsReader)
	}

	statusCh, errCh := r.dockerCli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case waitErr := <-errCh:
		if waitErr != nil {
			return fmt.Errorf("error waiting for container: %w", waitErr)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("agent container exited with status code %d", status.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (r *AgentTaskRunner) runOnHost(ctx context.Context, task *models.Task, attempt *models.TaskAttempt, worktreeDir, model string) error {
	binPath := r.cfg.OpenCodeBinPath
	if _, err := os.Stat(binPath); err != nil {
		binPath = "opencode"
	}

	args := []string{
		"run",
		"--server", r.cfg.OpenCodeServerURL,
		"--model", model,
		"--auto",
		task.Prompt,
	}

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = worktreeDir
	cmd.Env = append(cmd.Environ(), r.BuildProcessEnv(task, model, "/home/ghug")...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to capture stdout: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to capture stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start opencode on host: %w", err)
	}

	go r.streamPipe(ctx, task.ID, attempt.ID, stdoutPipe, "stdout")
	go r.streamPipe(ctx, task.ID, attempt.ID, stderrPipe, "stderr")

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("opencode run exited with error: %w", err)
	}

	return nil
}

func (r *AgentTaskRunner) streamDemuxedLogs(ctx context.Context, taskID, attemptID string, reader io.Reader) {
	outReader, outWriter := io.Pipe()
	errReader, errWriter := io.Pipe()

	go func() {
		defer outWriter.Close()
		defer errWriter.Close()
		_, _ = stdcopy.StdCopy(outWriter, errWriter, reader)
	}()

	go r.streamPipe(ctx, taskID, attemptID, outReader, "stdout")
	go r.streamPipe(ctx, taskID, attemptID, errReader, "stderr")
}

func (r *AgentTaskRunner) streamPipe(ctx context.Context, taskID, attemptID string, reader io.Reader, stream string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text() + "\r\n"
		r.broadcastLog(ctx, taskID, attemptID, line)
	}
}

func (r *AgentTaskRunner) broadcastLog(ctx context.Context, taskID, attemptID, content string) {
	if r.hub != nil {
		r.hub.BroadcastToTask(taskID, []byte(content))
	}
	if r.logRepo != nil {
		_ = r.logRepo.Append(ctx, &models.TaskLog{
			TaskID:    taskID,
			AttemptID: &attemptID,
			LogChunk:  content,
			Timestamp: time.Now().UTC(),
		})
	}
}
