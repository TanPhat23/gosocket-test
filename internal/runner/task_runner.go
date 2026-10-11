package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
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

// WithRunnerOpenCodeClient sets the OpenCode client for session lifecycle management.
func WithRunnerOpenCodeClient(client OpenCodeClient) RunnerOption {
	return func(r *AgentTaskRunner) {
		r.openCodeClient = client
	}
}

type AgentTaskRunner struct {
	cfg            *config.Config
	projectRepo    repository.ProjectRepository
	taskRepo       repository.TaskRepository
	logRepo        repository.LogRepository
	worktreeMgr    *git.WorktreeManager
	dockerRunner   *sandbox.Runner
	dockerCli      sandbox.DockerClient
	hub            *ws.Hub
	integRepo      repository.IntegrationRepository
	credRepo       repository.CredentialRepository
	cipher         *crypto.SecretCipher
	ghProvider     integrations.PullRequestProvider
	openCodeClient OpenCodeClient

	mu        sync.RWMutex
	doneChans map[string][]chan error
	running   map[string]bool
	completed map[string]error
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
		doneChans:    make(map[string][]chan error),
		running:      make(map[string]bool),
		completed:    make(map[string]error),
	}
	for _, opt := range opts {
		opt(runner)
	}
	if runner.openCodeClient == nil && cfg != nil && cfg.OpenCodeServerURL != "" {
		runner.openCodeClient = NewOpenCodeClient(cfg.OpenCodeServerURL, cfg.OpenCodePassword, nil)
	}
	return runner
}

// VerificationResult encapsulates the result of pre-flight verification tests.
type VerificationResult struct {
	Passed bool
	Output string
}

// DetectVerificationCommand inspects the worktree root to determine the appropriate test command.
// If ATM_VERIFY_CMD is set, its value takes precedence.
func DetectVerificationCommand(worktreePath string) string {
	if override := os.Getenv("ATM_VERIFY_CMD"); strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}

	if worktreePath == "" {
		return ""
	}

	// 1. Go project
	if _, err := os.Stat(filepath.Join(worktreePath, "go.mod")); err == nil {
		return "go test ./..."
	}

	// 2. Node / JS / TS project
	pkgPath := filepath.Join(worktreePath, "package.json")
	if pkgBytes, err := os.ReadFile(pkgPath); err == nil {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(pkgBytes, &pkg) == nil && pkg.Scripts != nil {
			if _, ok := pkg.Scripts["test"]; ok {
				return "npm test"
			}
		}
	}

	// 3. Rust project
	if _, err := os.Stat(filepath.Join(worktreePath, "Cargo.toml")); err == nil {
		return "cargo test"
	}

	// 4. Makefile
	if _, err := os.Stat(filepath.Join(worktreePath, "Makefile")); err == nil {
		return "make test"
	}

	return ""
}

// RunPreFlightVerification executes the verification command inside the worktree directory.
func RunPreFlightVerification(ctx context.Context, worktreeDir, verifyCmd string) (*VerificationResult, error) {
	if strings.TrimSpace(verifyCmd) == "" {
		return &VerificationResult{Passed: true, Output: ""}, nil
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", verifyCmd)
	cmd.Dir = worktreeDir
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	err := cmd.Run()
	output := truncateLog(outBuf.String(), 200, 16*1024)

	if err != nil {
		return &VerificationResult{
			Passed: false,
			Output: output,
		}, nil
	}

	return &VerificationResult{
		Passed: true,
		Output: output,
	}, nil
}

// FormatVerificationFeedbackPrompt formats the diagnostic feedback prompt for the ongoing OpenCode session.
func FormatVerificationFeedbackPrompt(iteration, maxIterations int, failureOutput string) string {
	return fmt.Sprintf(
		"Pre-flight verification failed (Iteration %d/%d) with the following error output:\n\n"+
			"```\n%s\n```\n\n"+
			"Please review the error output, inspect the relevant files, fix the root cause, "+
			"and ensure all tests pass.",
		iteration, maxIterations, failureOutput,
	)
}

func truncateLog(s string, maxLines, maxBytes int) string {
	if len(s) > maxBytes {
		s = s[len(s)-maxBytes:]
	}
	lines := strings.Split(s, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
		s = strings.Join(lines, "\n")
	}
	return strings.TrimSpace(s)
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

	_ = exec.Command("git", "-C", repoPath, "config", "core.fileMode", "false").Run()

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

// Watch registers and returns a receive-only channel that receives the task completion error
// (or nil on success) and is closed when the task finishes.
// If the task has already completed, the cached completion error is delivered immediately and the channel is closed.
func (r *AgentTaskRunner) Watch(taskID string) <-chan error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ch := make(chan error, 1)

	// 1. If already completed, deliver cached completion error and close immediately
	if err, ok := r.completed[taskID]; ok {
		ch <- err
		close(ch)
		return ch
	}

	// 2. Otherwise register watcher for ongoing or upcoming run
	r.doneChans[taskID] = append(r.doneChans[taskID], ch)
	return ch
}

// IsRunning returns true if the specified task is actively executing in the runner.
func (r *AgentTaskRunner) IsRunning(taskID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.running == nil {
		return false
	}
	return r.running[taskID]
}

// RunTaskAsync executes RunTask in a background goroutine and returns a receive-only done channel.
func (r *AgentTaskRunner) RunTaskAsync(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) <-chan error {
	done := make(chan error, 1)
	go func() {
		err := r.RunTask(ctx, task, attempt)
		done <- err
		close(done)
	}()
	return done
}

// RunTask executes an individual task attempt in an isolated worktree and runs OpenCode.
func (r *AgentTaskRunner) RunTask(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) (retErr error) {
	if task == nil {
		return errors.New("task cannot be nil")
	}

	r.mu.Lock()
	if r.doneChans == nil {
		r.doneChans = make(map[string][]chan error)
	}
	if r.running == nil {
		r.running = make(map[string]bool)
	}
	if r.completed == nil {
		r.completed = make(map[string]error)
	}
	r.running[task.ID] = true
	delete(r.completed, task.ID)
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.running[task.ID] = false
		r.completed[task.ID] = retErr
		chans := r.doneChans[task.ID]
		delete(r.doneChans, task.ID)
		r.mu.Unlock()

		for _, ch := range chans {
			select {
			case ch <- retErr:
			default:
			}
			close(ch)
		}
	}()

	project, err := r.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to lookup project %s: %w", task.ProjectID, err)
	}

	repoPath := project.RepoPath
	if repoPath == "" {
		repoPath = filepath.Join(r.cfg.HostRepoRoot, "core")
	}

	// 1. Check for active GitHub integration on this project
	var ghInteg *models.ProjectIntegration
	var token string
	if r.integRepo != nil && r.credRepo != nil && r.cipher != nil {
		integrationsList, err := r.integRepo.ListByProjectID(ctx, task.ProjectID)
		if err == nil {
			// If task specifies a specific integration, look up that target repo
			if task.IntegrationID != nil && *task.IntegrationID != "" {
				for i := range integrationsList {
					if integrationsList[i].ID == *task.IntegrationID && integrationsList[i].Enabled {
						ghInteg = &integrationsList[i]
						break
					}
				}
			}
			// Fallback to first enabled GitHub integration if not specifically designated
			if ghInteg == nil {
				for i := range integrationsList {
					if integrationsList[i].Provider == models.IntegrationGitHub && integrationsList[i].Enabled {
						ghInteg = &integrationsList[i]
						break
					}
				}
			}
		}
		if ghInteg != nil {
			if cred, err := r.credRepo.GetByID(ctx, ghInteg.CredentialID); err == nil {
				if t, err := r.cipher.Decrypt(cred.EncryptedData); err == nil {
					token = t
				}
			}
		}
	}

	if ghInteg != nil && token != "" {
		repoSlug := ghInteg.ExternalID
		baseBranch := "main"
		if ghInteg.Config != nil {
			if b, ok := ghInteg.Config["base_branch"].(string); ok && b != "" {
				baseBranch = b
			}
		}

		// Ensure a dedicated repository directory for this specific GitHub repo
		cleanSlug := strings.ReplaceAll(repoSlug, "/", "_")
		repoPath = filepath.Join(r.cfg.HostRepoRoot, cleanSlug)
		project.RepoPath = repoPath
		_ = r.projectRepo.Update(ctx, project)

		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Syncing GitHub repository %s (%s)...\r\n", repoSlug, baseBranch))
		if err := git.SyncGitHubRepo(ctx, repoPath, repoSlug, baseBranch, token, func(msg string) {
			r.broadcastLog(ctx, task.ID, attempt.ID, msg)
		}); err != nil {
			r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[31m[Agent Control Plane] Failed to sync GitHub repository: %v\x1b[0m\r\n", err))
			return fmt.Errorf("failed to sync github repo %s: %w", repoSlug, err)
		}
	} else {
		if err := EnsureGitRepo(repoPath); err != nil {
			return fmt.Errorf("failed to ensure git repo at %s: %w", repoPath, err)
		}

		// Sync with remote GitHub repo if a remote is configured
		_ = SyncRepoWithRemote(ctx, repoPath, func(msg string) {
			r.broadcastLog(ctx, task.ID, attempt.ID, msg)
		})
	}

	branchName := fmt.Sprintf("task/%s", task.ID)
	localWorktreePath, hostWorktreePath, _, err := r.worktreeMgr.CreateWorktree(ctx, repoPath, task.ID, branchName)
	if err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	// Guarantee full 0777 permissions across worktree so agent user has write access
	_ = exec.CommandContext(ctx, "chmod", "-R", "0777", localWorktreePath).Run()

	task.WorktreePath = &hostWorktreePath
	task.BranchName = &branchName
	_ = r.taskRepo.Update(ctx, task)

	attempt.WorktreePath = &hostWorktreePath
	attempt.BranchName = &branchName

	if err := InjectSandboxedConfig(localWorktreePath, task.Skills, task.MCPServers); err != nil {
		log.Printf("warning: failed to inject sandboxed configs into worktree: %v", err)
	}

	// Install project dependencies if needed (e.g. npm install for Next.js/React/Node, with persistent cache from repoPath)
	if err := InstallDependencies(ctx, localWorktreePath, repoPath, func(msg string) {
		r.broadcastLog(ctx, task.ID, attempt.ID, msg)
	}); err != nil {
		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[33m[Agent Control Plane] Dependency installation warning: %v\x1b[0m\r\n", err))
		log.Printf("warning: dependency installation failed in %s: %v", localWorktreePath, err)
	}

	sessionID := fmt.Sprintf("ses_atm_%s", task.ID)
	if task.SessionID != nil && *task.SessionID != "" {
		sessionID = *task.SessionID
	} else {
		task.SessionID = &sessionID
		_ = r.taskRepo.Update(ctx, task)
	}

	// Ensure the OpenCode session is bound to the task worktree directory
	if r.openCodeClient != nil {
		if err := r.openCodeClient.EnsureSessionAtLocation(ctx, sessionID, task.Title, hostWorktreePath); err != nil {
			log.Printf("warning: failed to bind OpenCode session to worktree %s: %v", hostWorktreePath, err)
		}
	}

	selectedModel := "google/gemini-3.8-flash"
	if task.Model != nil && *task.Model != "" {
		selectedModel = *task.Model
	}

	r.broadcastLog(ctx, task.ID, attempt.ID, "\x1b[36m[Agent Control Plane]\x1b[0m Starting autonomous task execution...\r\n")
	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Model: %s\r\n", selectedModel))
	worktreeDisplay := hostWorktreePath
	if r.dockerRunner != nil && r.dockerCli != nil {
		worktreeDisplay = fmt.Sprintf("/workspace (sandboxed from %s)", hostWorktreePath)
	}
	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Worktree: %s\r\n\r\n", worktreeDisplay))

	maxIterations := 3
	if r.cfg != nil && r.cfg.MaxVerificationRetries > 0 {
		maxIterations = r.cfg.MaxVerificationRetries
	}

	timeoutSec := 120
	if r.cfg != nil && r.cfg.VerificationTimeoutSeconds > 0 {
		timeoutSec = r.cfg.VerificationTimeoutSeconds
	}

	verifyCmd := DetectVerificationCommand(localWorktreePath)
	currentPrompt := task.Prompt

	for iteration := 1; iteration <= maxIterations; iteration++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if iteration > 1 {
			r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\r\n\x1b[33m[Agent Control Plane] Starting autonomous self-repair turn (Iteration %d/%d)...\x1b[0m\r\n", iteration, maxIterations))
		}

		var execErr error
		subCtx, subCancel := context.WithCancel(ctx)
		go r.watchChildSubagents(subCtx, task.ID, attempt.ID, sessionID)

		if r.dockerRunner != nil && r.dockerCli != nil {
			execErr = r.runInDocker(ctx, task, attempt, hostWorktreePath, selectedModel, currentPrompt)
		} else {
			execErr = r.runOnHost(ctx, task, attempt, localWorktreePath, selectedModel, currentPrompt)
		}
		subCancel()

		if execErr != nil {
			r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\r\n\x1b[31m[Agent Control Plane] Execution failed in iteration %d: %v\x1b[0m\r\n", iteration, execErr))
			return execErr
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Run pre-flight verification if a test suite is present
		if verifyCmd == "" {
			r.broadcastLog(ctx, task.ID, attempt.ID, "\x1b[36m[Agent Control Plane]\x1b[0m No test suite detected. Skipping pre-flight verification.\r\n")
			break
		}

		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Running pre-flight verification: %s\r\n", verifyCmd))

		vCtx, vCancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		vRes, vErr := RunPreFlightVerification(vCtx, localWorktreePath, verifyCmd)
		vCancel()

		if vErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[31m[Agent Control Plane] Verification execution error: %v\x1b[0m\r\n", vErr))
			return fmt.Errorf("verification execution error: %w", vErr)
		}

		if vRes.Passed {
			r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[32m✔ Pre-flight verification passed on iteration %d.\x1b[0m\r\n", iteration))
			break
		}

		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[31m✖ Pre-flight verification failed on iteration %d/%d:\x1b[0m\r\n%s\r\n", iteration, maxIterations, vRes.Output))

		if iteration == maxIterations {
			failReason := fmt.Sprintf("pre-flight verification failed after %d iterations:\n%s", maxIterations, vRes.Output)
			if attempt != nil {
				attempt.FailureReason = &failReason
			}
			r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\r\n\x1b[31m[Agent Control Plane] %s\x1b[0m\r\n", failReason))
			return errors.New(failReason)
		}

		// Prepare diagnostic prompt targeting the existing OpenCode session
		currentPrompt = FormatVerificationFeedbackPrompt(iteration, maxIterations, vRes.Output)
	}

	if ctx.Err() != nil {
		return ctx.Err()
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

	if ctx.Err() != nil {
		return ctx.Err()
	}

	r.broadcastLog(ctx, task.ID, attempt.ID, "\x1b[32m✔ Autonomous task run finished. Ready for review.\x1b[0m\r\n")

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
	if ctx.Err() != nil {
		return
	}

	if r.integRepo == nil || r.credRepo == nil || r.cipher == nil || r.ghProvider == nil {
		return
	}

	integrationsList, err := r.integRepo.ListByProjectID(ctx, task.ProjectID)
	if err != nil || len(integrationsList) == 0 {
		return
	}

	var ghInteg *models.ProjectIntegration
	if task.IntegrationID != nil && *task.IntegrationID != "" {
		for i := range integrationsList {
			if integrationsList[i].ID == *task.IntegrationID && integrationsList[i].Enabled {
				ghInteg = &integrationsList[i]
				break
			}
		}
	}
	// If not matched, inspect worktree git remote to deduce matching repo slug
	if ghInteg == nil && localWorktreePath != "" {
		if out, err := exec.CommandContext(ctx, "git", "-C", localWorktreePath, "remote", "get-url", "origin").Output(); err == nil {
			remoteStr := strings.TrimSpace(string(out))
			for i := range integrationsList {
				if integrationsList[i].Enabled && strings.Contains(remoteStr, integrationsList[i].ExternalID) {
					ghInteg = &integrationsList[i]
					break
				}
			}
		}
	}
	// Fallback to first enabled GitHub integration
	if ghInteg == nil {
		for i := range integrationsList {
			if integrationsList[i].Provider == models.IntegrationGitHub && integrationsList[i].Enabled {
				ghInteg = &integrationsList[i]
				break
			}
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
	if strings.HasPrefix(ghInteg.ExternalID, "http://") || strings.HasPrefix(ghInteg.ExternalID, "https://") || strings.HasPrefix(ghInteg.ExternalID, "file://") {
		remoteURL = ghInteg.ExternalID
	}

	r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Pushing branch %s to remote %s...\r\n", branchName, ghInteg.ExternalID))
	if err := git.PushBranch(ctx, localWorktreePath, remoteURL, token, branchName); err != nil {
		r.broadcastLog(ctx, task.ID, attempt.ID, fmt.Sprintf("\x1b[31m[Agent Control Plane] Git push failed: %v\x1b[0m\r\n", err))
		return
	}
	r.broadcastLog(ctx, task.ID, attempt.ID, "\x1b[32m✔ Branch pushed successfully to remote.\x1b[0m\r\n")

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

	r.broadcastLog(ctx, task.ID, attempt.ID, "\x1b[36m[Agent Control Plane]\x1b[0m Creating GitHub Pull Request...\r\n")
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

	sessionID := ""
	if task != nil {
		if task.SessionID != nil && *task.SessionID != "" {
			sessionID = *task.SessionID
		} else if task.ID != "" {
			sessionID = fmt.Sprintf("ses_atm_%s", task.ID)
		}
	}

	taskID := ""
	projectID := ""
	if task != nil {
		taskID = task.ID
		projectID = task.ProjectID
	}

	return []string{
		"HOME=" + homeDir,
		"OPENCODE_PASSWORD=" + pw,
		"ATM_TASK_ID=" + taskID,
		"ATM_PROJECT_ID=" + projectID,
		"ATM_API_URL=" + apiURL,
		"ATM_TASK_MODEL=" + model,
		"ATM_SESSION_ID=" + sessionID,
	}
}

func (r *AgentTaskRunner) runInDocker(ctx context.Context, task *models.Task, attempt *models.TaskAttempt, hostPath, model, prompt string) error {
	imageName := "agent-sandbox:latest"
	if override := os.Getenv("ATM_SANDBOX_IMAGE"); override != "" {
		imageName = override
	}

	sessionID := fmt.Sprintf("ses_atm_%s", task.ID)
	if task.SessionID != nil && *task.SessionID != "" {
		sessionID = *task.SessionID
	}

	binPath := "/usr/local/bin/opencode"
	cmd := []string{
		binPath,
		"run",
		"--standalone",
		"--format", "json",
		"--session", sessionID,
		"--title", task.Title,
		"--model", model,
		"--auto",
		prompt,
	}

	env := r.BuildProcessEnv(task, model, "/root")

	containerCfg := &container.Config{
		Image:        imageName,
		Cmd:          cmd,
		Env:          env,
		WorkingDir:   "/workspace",
		AttachStdout: true,
		AttachStderr: true,
	}

	hostBinPath := r.cfg.OpenCodeBinPath
	if hostBinPath == "" {
		hostBinPath = "/home/ghug/.opencode/bin/opencode"
	}

	binds := []string{
		fmt.Sprintf("%s:/workspace:rw", hostPath),
		fmt.Sprintf("%s:/usr/local/bin/opencode:ro", hostBinPath),
		fmt.Sprintf("%s:/root/.config/opencode:ro", r.cfg.OpenCodeConfigDir),
		fmt.Sprintf("%s:/root/.local/share/opencode:rw", r.cfg.OpenCodeDataDir),
	}
	if hostPath != "/workspace" {
		binds = append(binds, fmt.Sprintf("%s:%s:rw", hostPath, hostPath))
	}

	hostCfg := &container.HostConfig{
		Binds:  binds,
		Memory: 4 * 1024 * 1024 * 1024,
	}

	resp, err := r.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, "")
	if err != nil {
		log.Printf("container create failed with %s (%v), falling back to host execution", imageName, err)
		return r.runOnHost(ctx, task, attempt, hostPath, model, prompt)
	}

	containerID := resp.ID
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = r.dockerCli.ContainerRemove(cleanupCtx, containerID, container.RemoveOptions{Force: true})
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
			if ctx.Err() != nil {
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer stopCancel()
				timeoutSec := 0
				_ = r.dockerCli.ContainerStop(stopCtx, containerID, container.StopOptions{Timeout: &timeoutSec})
				return ctx.Err()
			}
			return fmt.Errorf("error waiting for container: %w", waitErr)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("agent container exited with status code %d", status.StatusCode)
		}
	case <-ctx.Done():
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		timeoutSec := 0
		_ = r.dockerCli.ContainerStop(stopCtx, containerID, container.StopOptions{Timeout: &timeoutSec})
		return ctx.Err()
	}

	return nil
}

func (r *AgentTaskRunner) runOnHost(ctx context.Context, task *models.Task, attempt *models.TaskAttempt, worktreeDir, model, prompt string) error {
	binPath := r.cfg.OpenCodeBinPath
	if _, err := os.Stat(binPath); err != nil {
		binPath = "opencode"
	}

	sessionID := fmt.Sprintf("ses_atm_%s", task.ID)
	if task.SessionID != nil && *task.SessionID != "" {
		sessionID = *task.SessionID
	}

	args := []string{
		"run",
		"--format", "json",
		"--server", r.cfg.OpenCodeServerURL,
		"--session", sessionID,
		"--title", task.Title,
		"--model", model,
		"--auto",
		prompt,
	}

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = worktreeDir
	cmd.Env = append(cmd.Environ(), r.BuildProcessEnv(task, model, "/home/ghug")...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

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
	scanner := NewAgentStreamScanner(reader)
	defer scanner.Close()
	tracker := NewTerminalTracker()

	for scanner.Scan() {
		line := scanner.Bytes()
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}

		if evt, err := ParseOpenCodeWireEvent(trimmed); err == nil && evt.Type != "" {
			tracker.Process(evt)
			if r.hub != nil {
				_ = r.hub.BroadcastTaskEvent(taskID, evt)
			}
			if ansi, isLog := FormatWireEventANSI(evt); isLog {
				r.broadcastLog(ctx, taskID, attemptID, ansi)
			}
		} else {
			r.broadcastLog(ctx, taskID, attemptID, string(line)+"\r\n")
		}
	}
	if scanner.Err() != nil {
		log.Printf("error reading %s stream for task %s: %v", stream, taskID, scanner.Err())
	}
}

func (r *AgentTaskRunner) broadcastLog(ctx context.Context, taskID, attemptID, content string) {
	if r.hub != nil {
		_ = r.hub.BroadcastTaskLog(taskID, content)
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

func (r *AgentTaskRunner) watchChildSubagents(ctx context.Context, taskID, attemptID, sessionID string) {
	if r.openCodeClient == nil {
		return
	}

	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	seenParts := make(map[string]bool)

	poll := func() {
		childSessions, err := r.openCodeClient.ListChildSessions(ctx, sessionID)
		if err != nil || len(childSessions) == 0 {
			return
		}

		for _, child := range childSessions {
			cMsgs, cErr := r.openCodeClient.GetSessionMessages(ctx, child.ID)
			if cErr != nil || len(cMsgs) == 0 {
				continue
			}

			agentLabel := child.Model
			if agentLabel == "" {
				agentLabel = "subagent"
			}

			for _, cMsg := range cMsgs {
				cContentList, ok := cMsg.Content.([]any)
				if !ok {
					continue
				}

				for cPartIdx, cItem := range cContentList {
					cItemMap, ok := cItem.(map[string]any)
					if !ok {
						continue
					}
					cPartType, _ := cItemMap["type"].(string)
					cPartID := fmt.Sprintf("%s_%d", cMsg.ID, cPartIdx)

					if cPartType == "tool" {
						cToolName, _ := cItemMap["name"].(string)
						if cid, ok := cItemMap["id"].(string); ok && cid != "" {
							cPartID = cid
						}
						cState, _ := cItemMap["state"].(map[string]any)
						cStatus := ""
						if cState != nil {
							cStatus, _ = cState["status"].(string)
						}

						cStartKey := cPartID + "_start"
						if !seenParts[cStartKey] {
							seenParts[cStartKey] = true
							cCmd := ""
							if cState != nil {
								if input, ok := cState["input"].(map[string]any); ok {
									if cmd, ok := input["command"].(string); ok {
										cCmd = cmd
									} else if p, ok := input["path"].(string); ok {
										cCmd = p
									}
								}
							}

							if r.hub != nil {
								var cInput any
								if cState != nil {
									cInput = cState["input"]
								}
								_ = r.hub.BroadcastTaskEvent(taskID, map[string]any{
									"eventType":       "tool_use",
									"sessionID":       child.ID,
									"parentSessionID": sessionID,
									"subagent":        agentLabel,
									"toolName":        cToolName,
									"toolStatus":      "running",
									"toolInput":       cInput,
								})
							}

							if cCmd != "" {
								if len(cCmd) > 70 {
									cCmd = cCmd[:67] + "..."
								}
								r.broadcastLog(context.Background(), taskID, attemptID, fmt.Sprintf("\x1b[35m[subagent: %s]\x1b[0m \x1b[33m→ [%s]\x1b[0m \x1b[90m%s\x1b[0m\r\n", agentLabel, cToolName, cCmd))
							} else {
								r.broadcastLog(context.Background(), taskID, attemptID, fmt.Sprintf("\x1b[35m[subagent: %s]\x1b[0m \x1b[33m→ [%s]\x1b[0m\r\n", agentLabel, cToolName))
							}
						}

						cDoneKey := cPartID + "_done"
						if (cStatus == "completed" || cStatus == "failed") && !seenParts[cDoneKey] {
							seenParts[cDoneKey] = true
							var cOutput any
							if cState != nil {
								cOutput = cState["output"]
							}

							if r.hub != nil {
								_ = r.hub.BroadcastTaskEvent(taskID, map[string]any{
									"eventType":       "tool_use",
									"sessionID":       child.ID,
									"parentSessionID": sessionID,
									"subagent":        agentLabel,
									"toolName":        cToolName,
									"toolStatus":      cStatus,
									"toolOutput":      cOutput,
								})
							}

							if outStr, ok := cOutput.(string); ok && outStr != "" {
								lines := strings.Split(strings.TrimSpace(outStr), "\n")
								limit := 4
								if len(lines) < limit {
									limit = len(lines)
								}
								for lIdx := 0; lIdx < limit; lIdx++ {
									r.broadcastLog(context.Background(), taskID, attemptID, fmt.Sprintf("  \x1b[90m%s\x1b[0m\r\n", lines[lIdx]))
								}
								if len(lines) > 4 {
									r.broadcastLog(context.Background(), taskID, attemptID, fmt.Sprintf("  \x1b[90m... (%d lines)\x1b[0m\r\n", len(lines)))
								}
							}

							if cStatus == "completed" {
								r.broadcastLog(context.Background(), taskID, attemptID, fmt.Sprintf("\x1b[35m[subagent: %s]\x1b[0m \x1b[32m✔ Completed tool: %s\x1b[0m\r\n", agentLabel, cToolName))
							} else {
								r.broadcastLog(context.Background(), taskID, attemptID, fmt.Sprintf("\x1b[35m[subagent: %s]\x1b[0m \x1b[31m✖ Tool failed: %s\x1b[0m\r\n", agentLabel, cToolName))
							}
						}
					}
				}
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			poll()
			return
		case <-ticker.C:
			poll()
		}
	}
}
