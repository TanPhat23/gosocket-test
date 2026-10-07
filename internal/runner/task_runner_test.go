package runner_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	createdConfig *container.Config
	waitStatusCh  chan container.WaitResponse
	waitErrCh     chan error
	logsReader    io.ReadCloser
}

func (m *mockDockerClientForRunner) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	m.createdConfig = config
	return container.CreateResponse{ID: "test-cid-123"}, nil
}

func (m *mockDockerClientForRunner) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	return nil
}

func (m *mockDockerClientForRunner) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	return m.waitStatusCh, m.waitErrCh
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
