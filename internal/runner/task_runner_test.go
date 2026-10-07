package runner_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-task-manager/internal/config"
	"agent-task-manager/internal/git"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/sandbox"
	"agent-task-manager/internal/ws"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type mockProjectRepo struct {
	project *models.Project
}

func (m *mockProjectRepo) Create(ctx context.Context, p *models.Project) error { return nil }
func (m *mockProjectRepo) GetByID(ctx context.Context, id string) (*models.Project, error) {
	return m.project, nil
}
func (m *mockProjectRepo) List(ctx context.Context, teamID string) ([]models.Project, error) {
	return []models.Project{*m.project}, nil
}

type mockTaskRepo struct {
	task *models.Task
}

func (m *mockTaskRepo) Create(ctx context.Context, t *models.Task) error { return nil }
func (m *mockTaskRepo) GetByID(ctx context.Context, id string) (*models.Task, error) {
	return m.task, nil
}
func (m *mockTaskRepo) GetByProjectAndExternalID(ctx context.Context, projectID, externalID string) (*models.Task, error) {
	return m.task, nil
}
func (m *mockTaskRepo) List(ctx context.Context, projectID string) ([]models.Task, error) {
	return []models.Task{*m.task}, nil
}
func (m *mockTaskRepo) ListByParentID(ctx context.Context, parentTaskID string) ([]models.Task, error) {
	return []models.Task{*m.task}, nil
}
func (m *mockTaskRepo) Update(ctx context.Context, t *models.Task) error {
	m.task = t
	return nil
}
func (m *mockTaskRepo) UpdateStatus(ctx context.Context, id string, status models.TaskStatus) error {
	if m.task != nil {
		m.task.Status = status
	}
	return nil
}

type mockLogRepo struct {
	logs []*models.TaskLog
}

func (m *mockLogRepo) Append(ctx context.Context, l *models.TaskLog) error {
	m.logs = append(m.logs, l)
	return nil
}
func (m *mockLogRepo) ListByTaskID(ctx context.Context, taskID string) ([]models.TaskLog, error) {
	var result []models.TaskLog
	for _, l := range m.logs {
		result = append(result, *l)
	}
	return result, nil
}

func TestAgentTaskRunner_Initialization(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		HostWorktreeRoot: filepath.Join(tmpDir, "worktrees"),
		HostRepoRoot:     filepath.Join(tmpDir, "repos"),
	}

	pRepo := &mockProjectRepo{
		project: &models.Project{
			ID:       "proj-1",
			RepoPath: filepath.Join(tmpDir, "repos", "core"),
		},
	}
	tRepo := &mockTaskRepo{
		task: &models.Task{
			ID:        "task-1",
			ProjectID: "proj-1",
			Prompt:    "hello world",
		},
	}
	lRepo := &mockLogRepo{}
	wm := git.NewWorktreeManager(cfg.HostWorktreeRoot)
	hub := ws.NewHub()

	ar := runner.NewAgentTaskRunner(cfg, pRepo, tRepo, lRepo, wm, nil, nil, hub)
	if ar == nil {
		t.Fatalf("expected non-nil AgentTaskRunner")
	}

	// Verify EnsureGitRepo works on new directory
	err := runner.EnsureGitRepo(pRepo.project.RepoPath)
	if err != nil {
		t.Fatalf("EnsureGitRepo failed: %v", err)
	}

	gitDir := filepath.Join(pRepo.project.RepoPath, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		t.Errorf("expected .git directory to exist after EnsureGitRepo")
	}
}

func TestBuildProcessEnv(t *testing.T) {
	cfg := &config.Config{
		Port:             9090,
		OpenCodePassword: "secret-password",
	}
	ar := runner.NewAgentTaskRunner(cfg, nil, nil, nil, nil, nil, nil, nil)
	task := &models.Task{
		ID:        "task-xyz",
		ProjectID: "proj-123",
	}

	env := ar.BuildProcessEnv(task, "gpt-4o", "/home/test")
	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	if envMap["ATM_TASK_ID"] != "task-xyz" {
		t.Errorf("expected ATM_TASK_ID=task-xyz, got %s", envMap["ATM_TASK_ID"])
	}
	if envMap["ATM_PROJECT_ID"] != "proj-123" {
		t.Errorf("expected ATM_PROJECT_ID=proj-123, got %s", envMap["ATM_PROJECT_ID"])
	}
	if envMap["ATM_API_URL"] != "http://localhost:9090" {
		t.Errorf("expected ATM_API_URL=http://localhost:9090, got %s", envMap["ATM_API_URL"])
	}
	if envMap["ATM_TASK_MODEL"] != "gpt-4o" {
		t.Errorf("expected ATM_TASK_MODEL=gpt-4o, got %s", envMap["ATM_TASK_MODEL"])
	}
	if envMap["ATM_SESSION_ID"] != "ses_atm_task-xyz" {
		t.Errorf("expected ATM_SESSION_ID=ses_atm_task-xyz, got %s", envMap["ATM_SESSION_ID"])
	}
	if envMap["OPENCODE_PASSWORD"] != "secret-password" {
		t.Errorf("expected OPENCODE_PASSWORD=secret-password, got %s", envMap["OPENCODE_PASSWORD"])
	}

	// Also verify with explicit task.SessionID set
	explicitSession := "ses_custom_override"
	taskWithSession := &models.Task{
		ID:        "task-abc",
		ProjectID: "proj-123",
		SessionID: &explicitSession,
	}
	envCustom := ar.BuildProcessEnv(taskWithSession, "gpt-4o", "/home/test")
	envCustomMap := make(map[string]string)
	for _, e := range envCustom {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envCustomMap[parts[0]] = parts[1]
		}
	}
	if envCustomMap["ATM_SESSION_ID"] != "ses_custom_override" {
		t.Errorf("expected ATM_SESSION_ID=ses_custom_override, got %s", envCustomMap["ATM_SESSION_ID"])
	}
}

func TestSyncRepoWithRemote_NoRemote(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "local-repo")

	if err := runner.EnsureGitRepo(repoPath); err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	var logs []string
	err := runner.SyncRepoWithRemote(context.Background(), repoPath, func(msg string) {
		logs = append(logs, msg)
	})
	if err != nil {
		t.Fatalf("expected no error on local repo without remotes: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("expected no logs for local repo without remotes, got %v", logs)
	}
}

func TestSyncRepoWithRemote_WithRemote(t *testing.T) {
	tmpDir := t.TempDir()
	upstreamPath := filepath.Join(tmpDir, "upstream")
	localPath := filepath.Join(tmpDir, "local")

	if err := runner.EnsureGitRepo(upstreamPath); err != nil {
		t.Fatalf("failed to create upstream: %v", err)
	}
	if err := runner.EnsureGitRepo(localPath); err != nil {
		t.Fatalf("failed to create local: %v", err)
	}

	// Add upstream as origin remote in local repo
	addRemoteCmd := exec.Command("git", "remote", "add", "origin", upstreamPath)
	addRemoteCmd.Dir = localPath
	if err := addRemoteCmd.Run(); err != nil {
		t.Fatalf("failed to add remote: %v", err)
	}

	var logs []string
	err := runner.SyncRepoWithRemote(context.Background(), localPath, func(msg string) {
		logs = append(logs, msg)
	})
	if err != nil {
		t.Fatalf("expected sync to succeed: %v", err)
	}
	if len(logs) < 2 {
		t.Errorf("expected sync logs, got %v", logs)
	}
}

type mockDockerClientForRunner struct {
	createdConfig  *container.Config
	createdConfigs []*container.Config
	waitStatusCh   chan container.WaitResponse
	waitErrCh      chan error
	logsReader     io.ReadCloser
}

func (m *mockDockerClientForRunner) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	m.createdConfig = config
	m.createdConfigs = append(m.createdConfigs, config)
	return container.CreateResponse{ID: fmt.Sprintf("test-cid-%d", len(m.createdConfigs))}, nil
}

func (m *mockDockerClientForRunner) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	return nil
}

func (m *mockDockerClientForRunner) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	statusCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)
	if m.waitStatusCh != nil {
		select {
		case s := <-m.waitStatusCh:
			statusCh <- s
		default:
			statusCh <- container.WaitResponse{StatusCode: 0}
		}
	} else {
		statusCh <- container.WaitResponse{StatusCode: 0}
	}
	return statusCh, errCh
}

func (m *mockDockerClientForRunner) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockDockerClientForRunner) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	return nil
}

func (m *mockDockerClientForRunner) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	return nil
}

func (m *mockDockerClientForRunner) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func TestRunInDocker_SessionAndTitleFlags(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")
	if err := runner.EnsureGitRepo(repoPath); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	cfg := &config.Config{
		OpenCodeServerURL: "http://127.0.0.1:49374",
		HostWorktreeRoot:  filepath.Join(tmpDir, "worktrees"),
		HostRepoRoot:      tmpDir,
	}
	pRepo := &mockProjectRepo{
		project: &models.Project{
			ID:       "proj-docker-1",
			RepoPath: repoPath,
		},
	}
	sessionID := "ses_atm_task-docker-1"
	task := &models.Task{
		ID:        "task-docker-1",
		ProjectID: "proj-docker-1",
		Title:     "Add deterministic session binding",
		Prompt:    "Write tests and update runner",
		SessionID: &sessionID,
	}
	attempt := &models.TaskAttempt{
		ID:     "attempt-1",
		TaskID: task.ID,
	}
	tRepo := &mockTaskRepo{task: task}
	lRepo := &mockLogRepo{}
	wm := git.NewWorktreeManager(cfg.HostWorktreeRoot)

	statusCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)
	statusCh <- container.WaitResponse{StatusCode: 0}

	mockCli := &mockDockerClientForRunner{
		waitStatusCh: statusCh,
		waitErrCh:    errCh,
	}
	dockerRunner := sandbox.NewRunner(mockCli)

	ar := runner.NewAgentTaskRunner(cfg, pRepo, tRepo, lRepo, wm, dockerRunner, mockCli, nil)

	ctx := context.Background()
	err := ar.RunTask(ctx, task, attempt)
	if err != nil {
		t.Fatalf("RunTask failed: %v", err)
	}

	if mockCli.createdConfig == nil {
		t.Fatalf("expected ContainerCreate to have been called")
	}

	cmd := mockCli.createdConfig.Cmd
	// Assert --session and --title are present in cmd arguments
	sessionIdx := -1
	titleIdx := -1
	for i, arg := range cmd {
		if arg == "--session" && i+1 < len(cmd) {
			sessionIdx = i + 1
		}
		if arg == "--title" && i+1 < len(cmd) {
			titleIdx = i + 1
		}
	}

	if sessionIdx == -1 {
		t.Fatalf("expected --session flag in cmd: %v", cmd)
	}
	if cmd[sessionIdx] != "ses_atm_task-docker-1" {
		t.Errorf("expected session ID ses_atm_task-docker-1, got %s", cmd[sessionIdx])
	}

	if titleIdx == -1 {
		t.Fatalf("expected --title flag in cmd: %v", cmd)
	}
	if cmd[titleIdx] != task.Title {
		t.Errorf("expected title %q, got %q", task.Title, cmd[titleIdx])
	}

	// Verify ATM_SESSION_ID in environment
	envMap := make(map[string]string)
	for _, e := range mockCli.createdConfig.Env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	if envMap["ATM_SESSION_ID"] != "ses_atm_task-docker-1" {
		t.Errorf("expected ATM_SESSION_ID=ses_atm_task-docker-1 in container env, got %s", envMap["ATM_SESSION_ID"])
	}
}

func TestDetectVerificationCommand(t *testing.T) {
	t.Run("detects Go project", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644); err != nil {
			t.Fatal(err)
		}
		cmd := runner.DetectVerificationCommand(dir)
		if cmd != "go test ./..." {
			t.Errorf("expected 'go test ./...', got %q", cmd)
		}
	})

	t.Run("detects Node project with package.json", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"test":"jest"}}`), 0644); err != nil {
			t.Fatal(err)
		}
		cmd := runner.DetectVerificationCommand(dir)
		if cmd != "npm test" {
			t.Errorf("expected 'npm test', got %q", cmd)
		}
	})

	t.Run("detects Rust project with Cargo.toml", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]"), 0644); err != nil {
			t.Fatal(err)
		}
		cmd := runner.DetectVerificationCommand(dir)
		if cmd != "cargo test" {
			t.Errorf("expected 'cargo test', got %q", cmd)
		}
	})

	t.Run("detects Makefile", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\techo test"), 0644); err != nil {
			t.Fatal(err)
		}
		cmd := runner.DetectVerificationCommand(dir)
		if cmd != "make test" {
			t.Errorf("expected 'make test', got %q", cmd)
		}
	})

	t.Run("empty when no known test configs exist", func(t *testing.T) {
		dir := t.TempDir()
		cmd := runner.DetectVerificationCommand(dir)
		if cmd != "" {
			t.Errorf("expected empty string, got %q", cmd)
		}
	})

	t.Run("respects ATM_VERIFY_CMD override", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ATM_VERIFY_CMD", "pytest -v")
		cmd := runner.DetectVerificationCommand(dir)
		if cmd != "pytest -v" {
			t.Errorf("expected 'pytest -v', got %q", cmd)
		}
	})
}

func TestRunPreFlightVerification(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	t.Run("empty command passes immediately", func(t *testing.T) {
		res, err := runner.RunPreFlightVerification(ctx, dir, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Passed {
			t.Errorf("expected passed to be true")
		}
	})

	t.Run("successful command passes", func(t *testing.T) {
		res, err := runner.RunPreFlightVerification(ctx, dir, "echo 'all tests pass'")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Passed {
			t.Errorf("expected passed to be true")
		}
		if !strings.Contains(res.Output, "all tests pass") {
			t.Errorf("expected output to contain 'all tests pass', got %q", res.Output)
		}
	})

	t.Run("failing command returns Passed=false with output", func(t *testing.T) {
		res, err := runner.RunPreFlightVerification(ctx, dir, "echo 'assertion failed' && exit 1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Passed {
			t.Errorf("expected passed to be false")
		}
		if !strings.Contains(res.Output, "assertion failed") {
			t.Errorf("expected output to contain 'assertion failed', got %q", res.Output)
		}
	})
}

func TestFormatVerificationFeedbackPrompt(t *testing.T) {
	prompt := runner.FormatVerificationFeedbackPrompt(1, 3, "panic: index out of range")
	if !strings.Contains(prompt, "Iteration 1/3") {
		t.Errorf("expected 'Iteration 1/3' in prompt, got %s", prompt)
	}
	if !strings.Contains(prompt, "panic: index out of range") {
		t.Errorf("expected error log in prompt, got %s", prompt)
	}
	if !strings.Contains(prompt, "Please review the error output") {
		t.Errorf("expected corrective instruction, got %s", prompt)
	}
}

func TestRunTask_VerificationLoop_PassFirstTurn(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")
	if err := runner.EnsureGitRepo(repoPath); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	cfg := &config.Config{
		OpenCodeServerURL:      "http://127.0.0.1:49374",
		HostWorktreeRoot:       filepath.Join(tmpDir, "worktrees"),
		HostRepoRoot:           tmpDir,
		MaxVerificationRetries: 3,
	}
	pRepo := &mockProjectRepo{
		project: &models.Project{
			ID:       "proj-loop-pass",
			RepoPath: repoPath,
		},
	}
	task := &models.Task{
		ID:        "task-loop-pass",
		ProjectID: "proj-loop-pass",
		Title:     "Task that passes verification on first turn",
		Prompt:    "Implement feature cleanly",
	}
	attempt := &models.TaskAttempt{
		ID:     "attempt-1",
		TaskID: task.ID,
	}

	t.Setenv("ATM_VERIFY_CMD", "echo 'all tests pass'")

	mockCli := &mockDockerClientForRunner{}
	dockerRunner := sandbox.NewRunner(mockCli)
	ar := runner.NewAgentTaskRunner(cfg, pRepo, &mockTaskRepo{task: task}, &mockLogRepo{}, git.NewWorktreeManager(cfg.HostWorktreeRoot), dockerRunner, mockCli, nil)

	err := ar.RunTask(context.Background(), task, attempt)
	if err != nil {
		t.Fatalf("expected RunTask to succeed: %v", err)
	}

	if len(mockCli.createdConfigs) != 1 {
		t.Fatalf("expected 1 execution turn, got %d", len(mockCli.createdConfigs))
	}
	firstCmd := mockCli.createdConfigs[0].Cmd
	if firstCmd[len(firstCmd)-1] != task.Prompt {
		t.Errorf("expected initial prompt on first turn, got %q", firstCmd[len(firstCmd)-1])
	}
}

func TestRunTask_VerificationLoop_FailThenPass(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")
	if err := runner.EnsureGitRepo(repoPath); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	cfg := &config.Config{
		OpenCodeServerURL:      "http://127.0.0.1:49374",
		HostWorktreeRoot:       filepath.Join(tmpDir, "worktrees"),
		HostRepoRoot:           tmpDir,
		MaxVerificationRetries: 3,
	}
	pRepo := &mockProjectRepo{
		project: &models.Project{
			ID:       "proj-loop-repair",
			RepoPath: repoPath,
		},
	}
	task := &models.Task{
		ID:        "task-loop-repair",
		ProjectID: "proj-loop-repair",
		Title:     "Task that fails on turn 1 and passes on turn 2",
		Prompt:    "Fix the failing test",
	}
	attempt := &models.TaskAttempt{
		ID:     "attempt-repair-1",
		TaskID: task.ID,
	}

	flagFile := filepath.Join(tmpDir, "test_pass.flag")
	verifyScript := fmt.Sprintf("if [ -f %s ]; then echo 'all tests pass now'; exit 0; else touch %s; echo 'FAIL: broken assertion line 42'; exit 1; fi", flagFile, flagFile)
	t.Setenv("ATM_VERIFY_CMD", verifyScript)

	mockCli := &mockDockerClientForRunner{}
	dockerRunner := sandbox.NewRunner(mockCli)
	ar := runner.NewAgentTaskRunner(cfg, pRepo, &mockTaskRepo{task: task}, &mockLogRepo{}, git.NewWorktreeManager(cfg.HostWorktreeRoot), dockerRunner, mockCli, nil)

	err := ar.RunTask(context.Background(), task, attempt)
	if err != nil {
		t.Fatalf("expected RunTask to succeed after repair: %v", err)
	}

	if len(mockCli.createdConfigs) != 2 {
		t.Fatalf("expected exactly 2 execution turns, got %d", len(mockCli.createdConfigs))
	}

	// Turn 1 should be the user initial prompt
	turn1Cmd := mockCli.createdConfigs[0].Cmd
	if turn1Cmd[len(turn1Cmd)-1] != task.Prompt {
		t.Errorf("expected initial prompt on turn 1, got %q", turn1Cmd[len(turn1Cmd)-1])
	}

	// Turn 2 should be the diagnostic error prompt targeting the same session
	turn2Cmd := mockCli.createdConfigs[1].Cmd
	turn2Prompt := turn2Cmd[len(turn2Cmd)-1]
	if !strings.Contains(turn2Prompt, "Iteration 1/3") {
		t.Errorf("expected 'Iteration 1/3' in turn 2 prompt, got %s", turn2Prompt)
	}
	if !strings.Contains(turn2Prompt, "FAIL: broken assertion line 42") {
		t.Errorf("expected failure error log in turn 2 prompt, got %s", turn2Prompt)
	}
}

func TestRunTask_VerificationLoop_MaxRetriesExceeded(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")
	if err := runner.EnsureGitRepo(repoPath); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	cfg := &config.Config{
		OpenCodeServerURL:      "http://127.0.0.1:49374",
		HostWorktreeRoot:       filepath.Join(tmpDir, "worktrees"),
		HostRepoRoot:           tmpDir,
		MaxVerificationRetries: 2,
	}
	pRepo := &mockProjectRepo{
		project: &models.Project{
			ID:       "proj-loop-exceeded",
			RepoPath: repoPath,
		},
	}
	task := &models.Task{
		ID:        "task-loop-exceeded",
		ProjectID: "proj-loop-exceeded",
		Title:     "Task that never passes verification",
		Prompt:    "Impossible problem",
	}
	attempt := &models.TaskAttempt{
		ID:     "attempt-exceeded-1",
		TaskID: task.ID,
	}

	t.Setenv("ATM_VERIFY_CMD", "echo 'persistent compilation error' && exit 1")

	mockCli := &mockDockerClientForRunner{}
	dockerRunner := sandbox.NewRunner(mockCli)
	ar := runner.NewAgentTaskRunner(cfg, pRepo, &mockTaskRepo{task: task}, &mockLogRepo{}, git.NewWorktreeManager(cfg.HostWorktreeRoot), dockerRunner, mockCli, nil)

	err := ar.RunTask(context.Background(), task, attempt)
	if err == nil {
		t.Fatalf("expected RunTask to return error when retries exceeded")
	}

	if !strings.Contains(err.Error(), "pre-flight verification failed after 2 iterations") {
		t.Errorf("expected error message mentioning iterations exceeded, got: %v", err)
	}

	if attempt.FailureReason == nil || !strings.Contains(*attempt.FailureReason, "persistent compilation error") {
		t.Errorf("expected attempt.FailureReason to be populated with error log, got: %v", attempt.FailureReason)
	}

	if len(mockCli.createdConfigs) != 2 {
		t.Fatalf("expected exactly 2 execution turns before giving up, got %d", len(mockCli.createdConfigs))
	}
}

func TestAgentTaskRunner_Watch_Success(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")
	if err := runner.EnsureGitRepo(repoPath); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	cfg := &config.Config{
		OpenCodeServerURL: "http://127.0.0.1:49374",
		HostWorktreeRoot:  filepath.Join(tmpDir, "worktrees"),
		HostRepoRoot:      tmpDir,
	}
	pRepo := &mockProjectRepo{
		project: &models.Project{
			ID:       "proj-watch-1",
			RepoPath: repoPath,
		},
	}
	task := &models.Task{
		ID:        "task-watch-1",
		ProjectID: "proj-watch-1",
		Title:     "Watch task",
		Prompt:    "Prompt",
	}
	attempt := &models.TaskAttempt{
		ID:     "attempt-watch-1",
		TaskID: task.ID,
	}

	t.Setenv("ATM_VERIFY_CMD", "echo 'pass'")
	mockCli := &mockDockerClientForRunner{}
	dockerRunner := sandbox.NewRunner(mockCli)
	ar := runner.NewAgentTaskRunner(cfg, pRepo, &mockTaskRepo{task: task}, &mockLogRepo{}, git.NewWorktreeManager(cfg.HostWorktreeRoot), dockerRunner, mockCli, nil)

	// Watch before or during task execution
	doneCh := ar.Watch(task.ID)

	go func() {
		_ = ar.RunTask(context.Background(), task, attempt)
	}()

	select {
	case err, ok := <-doneCh:
		if err != nil {
			t.Errorf("expected nil error on success, got: %v", err)
		}
		if !ok {
			// Channel was closed, which is also valid completion
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for task done channel")
	}

	// Verify channel is closed
	select {
	case _, ok := <-doneCh:
		if ok {
			t.Errorf("expected done channel to be closed")
		}
	default:
	}
}

func TestAgentTaskRunner_Watch_Failure(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")
	if err := runner.EnsureGitRepo(repoPath); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	cfg := &config.Config{
		OpenCodeServerURL:      "http://127.0.0.1:49374",
		HostWorktreeRoot:       filepath.Join(tmpDir, "worktrees"),
		HostRepoRoot:           tmpDir,
		MaxVerificationRetries: 1,
	}
	pRepo := &mockProjectRepo{
		project: &models.Project{
			ID:       "proj-watch-fail",
			RepoPath: repoPath,
		},
	}
	task := &models.Task{
		ID:        "task-watch-fail",
		ProjectID: "proj-watch-fail",
		Title:     "Watch failing task",
		Prompt:    "Prompt",
	}
	attempt := &models.TaskAttempt{
		ID:     "attempt-watch-fail",
		TaskID: task.ID,
	}

	t.Setenv("ATM_VERIFY_CMD", "exit 1")
	mockCli := &mockDockerClientForRunner{}
	dockerRunner := sandbox.NewRunner(mockCli)
	ar := runner.NewAgentTaskRunner(cfg, pRepo, &mockTaskRepo{task: task}, &mockLogRepo{}, git.NewWorktreeManager(cfg.HostWorktreeRoot), dockerRunner, mockCli, nil)

	doneCh := ar.Watch(task.ID)

	go func() {
		_ = ar.RunTask(context.Background(), task, attempt)
	}()

	select {
	case err := <-doneCh:
		if err == nil {
			t.Errorf("expected non-nil error on failure, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for failing task done channel")
	}
}

func TestAgentTaskRunner_Watch_IsRunning(t *testing.T) {
	ar := runner.NewAgentTaskRunner(nil, nil, nil, nil, nil, nil, nil, nil)
	if ar.IsRunning("non-existent-task") {
		t.Errorf("expected IsRunning to be false for non-existent-task")
	}
}

func TestAgentTaskRunner_Watch_CompletedTask(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")
	if err := runner.EnsureGitRepo(repoPath); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	cfg := &config.Config{
		OpenCodeServerURL: "http://127.0.0.1:49374",
		HostWorktreeRoot:  filepath.Join(tmpDir, "worktrees"),
		HostRepoRoot:      tmpDir,
	}
	pRepo := &mockProjectRepo{
		project: &models.Project{
			ID:       "proj-watch-cached",
			RepoPath: repoPath,
		},
	}
	task := &models.Task{
		ID:        "task-watch-cached",
		ProjectID: "proj-watch-cached",
		Title:     "Watch cached task",
		Prompt:    "Prompt",
	}
	attempt := &models.TaskAttempt{
		ID:     "attempt-watch-cached",
		TaskID: task.ID,
	}

	t.Setenv("ATM_VERIFY_CMD", "echo 'pass'")
	mockCli := &mockDockerClientForRunner{}
	dockerRunner := sandbox.NewRunner(mockCli)
	ar := runner.NewAgentTaskRunner(cfg, pRepo, &mockTaskRepo{task: task}, &mockLogRepo{}, git.NewWorktreeManager(cfg.HostWorktreeRoot), dockerRunner, mockCli, nil)

	// Run task synchronously to completion
	err := ar.RunTask(context.Background(), task, attempt)
	if err != nil {
		t.Fatalf("expected RunTask to succeed: %v", err)
	}

	// Verify task is no longer running
	if ar.IsRunning(task.ID) {
		t.Errorf("expected IsRunning to be false after completion")
	}

	// Calling Watch on already completed task returns immediately with cached result
	doneCh := ar.Watch(task.ID)
	select {
	case err, ok := <-doneCh:
		if err != nil {
			t.Errorf("expected nil error on cached success, got: %v", err)
		}
		if ok {
			_, ok2 := <-doneCh
			if ok2 {
				t.Errorf("expected channel to be closed")
			}
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for cached watch channel")
	}
}

func TestAgentTaskRunner_RunTaskAsync(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "repo")
	if err := runner.EnsureGitRepo(repoPath); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	cfg := &config.Config{
		OpenCodeServerURL: "http://127.0.0.1:49374",
		HostWorktreeRoot:  filepath.Join(tmpDir, "worktrees"),
		HostRepoRoot:      tmpDir,
	}
	pRepo := &mockProjectRepo{
		project: &models.Project{
			ID:       "proj-async",
			RepoPath: repoPath,
		},
	}
	task := &models.Task{
		ID:        "task-async",
		ProjectID: "proj-async",
		Title:     "Async task",
		Prompt:    "Prompt",
	}
	attempt := &models.TaskAttempt{
		ID:     "attempt-async",
		TaskID: task.ID,
	}

	t.Setenv("ATM_VERIFY_CMD", "echo 'pass'")
	mockCli := &mockDockerClientForRunner{}
	dockerRunner := sandbox.NewRunner(mockCli)
	ar := runner.NewAgentTaskRunner(cfg, pRepo, &mockTaskRepo{task: task}, &mockLogRepo{}, git.NewWorktreeManager(cfg.HostWorktreeRoot), dockerRunner, mockCli, nil)

	doneCh := ar.RunTaskAsync(context.Background(), task, attempt)
	select {
	case err := <-doneCh:
		if err != nil {
			t.Errorf("expected nil error, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for RunTaskAsync done channel")
	}
}
