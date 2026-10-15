package runner_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/git"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/sandbox"
	"agent-task-manager/internal/ws"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type mockProjectRepo struct {
	project *models.Project
}

func (m *mockProjectRepo) Create(ctx context.Context, p *models.Project) error { return nil }
func (m *mockProjectRepo) Update(ctx context.Context, p *models.Project) error {
	m.project = p
	return nil
}
func (m *mockProjectRepo) GetByID(ctx context.Context, id string) (*models.Project, error) {
	return m.project, nil
}
func (m *mockProjectRepo) List(ctx context.Context, teamID string) ([]models.Project, error) {
	return []models.Project{*m.project}, nil
}
func (m *mockProjectRepo) Delete(ctx context.Context, id string) error { return nil }

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
	createErr      error
	createdConfig  *container.Config
	createdConfigs []*container.Config
	waitStatusCh   chan container.WaitResponse
	waitErrCh      chan error
	stopCalled     bool
	removeCalled   bool
	calls          int // every DockerClient method invocation
}

func (m *mockDockerClientForRunner) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	m.calls++
	if m.createErr != nil {
		return container.CreateResponse{}, m.createErr
	}
	m.createdConfig = config
	m.createdConfigs = append(m.createdConfigs, config)
	return container.CreateResponse{ID: fmt.Sprintf("test-cid-%d", len(m.createdConfigs))}, nil
}

func (m *mockDockerClientForRunner) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	m.calls++
	return nil
}

func (m *mockDockerClientForRunner) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	m.calls++
	statusCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)
	if m.waitStatusCh != nil {
		go func() {
			select {
			case s := <-m.waitStatusCh:
				statusCh <- s
			case <-ctx.Done():
			}
		}()
	} else {
		statusCh <- container.WaitResponse{StatusCode: 0}
	}
	return statusCh, errCh
}

func (m *mockDockerClientForRunner) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	m.calls++
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockDockerClientForRunner) ContainerAttach(ctx context.Context, containerID string, options container.AttachOptions) (types.HijackedResponse, error) {
	return types.HijackedResponse{}, nil
}

func (m *mockDockerClientForRunner) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	m.calls++
	m.stopCalled = true
	return nil
}

func (m *mockDockerClientForRunner) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	m.calls++
	m.removeCalled = true
	return nil
}

func (m *mockDockerClientForRunner) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	m.calls++
	return types.ContainerJSON{}, nil
}

func (m *mockDockerClientForRunner) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	m.calls++
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockDockerClientForRunner) ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	m.calls++
	return types.ImageBuildResponse{Body: io.NopCloser(strings.NewReader(""))}, nil
}

func (m *mockDockerClientForRunner) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	m.calls++
	return nil, nil
}

func (m *mockDockerClientForRunner) ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error) {
	m.calls++
	return types.ImageInspect{}, nil, nil
}

func (m *mockDockerClientForRunner) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	m.calls++
	return network.CreateResponse{}, nil
}

func (m *mockDockerClientForRunner) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	m.calls++
	return network.Inspect{}, nil
}

func (m *mockDockerClientForRunner) NetworkRemove(ctx context.Context, networkID string) error {
	m.calls++
	return nil
}

func (m *mockDockerClientForRunner) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	m.calls++
	return volume.Volume{}, nil
}

func (m *mockDockerClientForRunner) VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error) {
	m.calls++
	return volume.Volume{}, nil
}

func (m *mockDockerClientForRunner) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	m.calls++
	return nil
}

func (m *mockDockerClientForRunner) CopyToContainer(ctx context.Context, containerID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error {
	m.calls++
	return nil
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

type mockOpenCodeClientForRunner struct {
	boundLocation  string
	boundSessionID string
	calls          int // every OpenCodeClient method invocation
}

func (m *mockOpenCodeClientForRunner) GetSession(ctx context.Context, sessionID string) (*runner.OpenCodeSessionDTO, error) {
	m.calls++
	return &runner.OpenCodeSessionDTO{ID: sessionID}, nil
}
func (m *mockOpenCodeClientForRunner) GetSessionMessages(ctx context.Context, sessionID string) ([]runner.OpenCodeMessageDTO, error) {
	m.calls++
	return nil, nil
}
func (m *mockOpenCodeClientForRunner) ListChildSessions(ctx context.Context, parentID string) ([]runner.OpenCodeSessionDTO, error) {
	m.calls++
	return nil, nil
}
func (m *mockOpenCodeClientForRunner) PromptSession(ctx context.Context, sessionID, prompt string) error {
	m.calls++
	return nil
}
func (m *mockOpenCodeClientForRunner) CompactSession(ctx context.Context, sessionID string) error {
	m.calls++
	return nil
}
func (m *mockOpenCodeClientForRunner) InterruptSession(ctx context.Context, sessionID string) error {
	m.calls++
	return nil
}
func (m *mockOpenCodeClientForRunner) InterruptSessionTree(ctx context.Context, rootSessionID string) error {
	m.calls++
	return nil
}
func (m *mockOpenCodeClientForRunner) EnsureSessionAtLocation(ctx context.Context, sessionID, title, directory string) error {
	m.calls++
	m.boundSessionID = sessionID
	m.boundLocation = directory
	return nil
}

type mockIntegRepoForRunner struct {
	integs []models.ProjectIntegration
}

func (m *mockIntegRepoForRunner) ListByProjectID(ctx context.Context, projectID string) ([]models.ProjectIntegration, error) {
	return m.integs, nil
}
func (m *mockIntegRepoForRunner) GetByID(ctx context.Context, id string) (*models.ProjectIntegration, error) {
	return nil, nil
}
func (m *mockIntegRepoForRunner) GetActiveByProviderAndExternalID(ctx context.Context, provider models.IntegrationProvider, externalID string) (*models.ProjectIntegration, error) {
	return nil, nil
}
func (m *mockIntegRepoForRunner) Create(ctx context.Context, integ *models.ProjectIntegration) error {
	return nil
}
func (m *mockIntegRepoForRunner) Update(ctx context.Context, integ *models.ProjectIntegration) error {
	return nil
}
func (m *mockIntegRepoForRunner) Delete(ctx context.Context, id string) error {
	return nil
}

type mockCredRepoForRunner struct {
	cred *models.Credential
}

func (m *mockCredRepoForRunner) GetByID(ctx context.Context, id string) (*models.Credential, error) {
	return m.cred, nil
}
func (m *mockCredRepoForRunner) GetByIDAndTeam(ctx context.Context, id, teamID string) (*models.Credential, error) {
	return m.cred, nil
}
func (m *mockCredRepoForRunner) Create(ctx context.Context, cred *models.Credential) error {
	return nil
}
func (m *mockCredRepoForRunner) Update(ctx context.Context, cred *models.Credential) error {
	m.cred = cred
	return nil
}
func (m *mockCredRepoForRunner) ListByTeamID(ctx context.Context, teamID string) ([]models.Credential, error) {
	return nil, nil
}
func (m *mockCredRepoForRunner) Delete(ctx context.Context, id, teamID string) error {
	return nil
}

type mockGHProviderForRunner struct {
	prCreated bool
	prURL     string
}

func (m *mockGHProviderForRunner) CreatePullRequest(ctx context.Context, integ *models.ProjectIntegration, token, headBranch, title, body string) (string, error) {
	m.prCreated = true
	return m.prURL, nil
}

func (m *mockGHProviderForRunner) GetPullRequestStatus(ctx context.Context, integ *models.ProjectIntegration, token, prNumberOrURL string) (*integrations.PullRequestStatus, error) {
	return &integrations.PullRequestStatus{
		Number:  1,
		State:   "open",
		Merged:  false,
		HTMLURL: m.prURL,
	}, nil
}

type mockProjectSandboxManagerForRunner struct {
	ensureFunc func(ctx context.Context, project *models.Project, imageTag string) (*runner.ProjectRouteHandle, error)
	called     bool
}

func (m *mockProjectSandboxManagerForRunner) EnsureProjectContainer(ctx context.Context, project *models.Project, imageTag string) (*runner.ProjectRouteHandle, error) {
	m.called = true
	if m.ensureFunc != nil {
		return m.ensureFunc(ctx, project, imageTag)
	}
	return &runner.ProjectRouteHandle{
		ProjectID:    project.ID,
		ContainerID:  "c-" + project.ID,
		EndpointURL:  "http://127.0.0.1:49374",
		AuthPassword: "mock-password",
	}, nil
}

func (m *mockProjectSandboxManagerForRunner) StopProjectContainer(ctx context.Context, projectID string, gracePeriod time.Duration) error {
	return nil
}

func (m *mockProjectSandboxManagerForRunner) GetProjectRoute(ctx context.Context, projectID string) (*runner.ProjectRouteHandle, bool, error) {
	return nil, false, nil
}

type mockWorkspaceManagerForRunner struct {
	ensureFunc  func(ctx context.Context, projectID string, task *models.Task, repoName string, baseCommit string) (string, error)
	cleanupFunc func(ctx context.Context, projectID string, taskID string) error
	called      bool
}

func (m *mockWorkspaceManagerForRunner) EnsureTaskWorktree(ctx context.Context, projectID string, task *models.Task, repoName string, baseCommit string) (string, error) {
	m.called = true
	if m.ensureFunc != nil {
		return m.ensureFunc(ctx, projectID, task, repoName, baseCommit)
	}
	return fmt.Sprintf("/workspaces/%s/tasks/%s", repoName, task.ID), nil
}

func (m *mockWorkspaceManagerForRunner) CleanupTaskScratch(ctx context.Context, projectID string, taskID string) error {
	if m.cleanupFunc != nil {
		return m.cleanupFunc(ctx, projectID, taskID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Containment (Phase 1A): RunTask refuses unconditionally and has no side effects.
// ---------------------------------------------------------------------------

type spyProjectRepo struct {
	mockProjectRepo
	calls int
}

func (s *spyProjectRepo) Create(ctx context.Context, p *models.Project) error {
	s.calls++
	return nil
}
func (s *spyProjectRepo) Update(ctx context.Context, p *models.Project) error {
	s.calls++
	return nil
}
func (s *spyProjectRepo) GetByID(ctx context.Context, id string) (*models.Project, error) {
	s.calls++
	return s.mockProjectRepo.project, nil
}
func (s *spyProjectRepo) List(ctx context.Context, teamID string) ([]models.Project, error) {
	s.calls++
	return nil, nil
}
func (s *spyProjectRepo) Delete(ctx context.Context, id string) error {
	s.calls++
	return nil
}

type spyTaskRepo struct {
	mockTaskRepo
	calls int
}

func (s *spyTaskRepo) Create(ctx context.Context, t *models.Task) error {
	s.calls++
	return nil
}
func (s *spyTaskRepo) GetByID(ctx context.Context, id string) (*models.Task, error) {
	s.calls++
	return s.mockTaskRepo.task, nil
}
func (s *spyTaskRepo) GetByProjectAndExternalID(ctx context.Context, projectID, externalID string) (*models.Task, error) {
	s.calls++
	return s.mockTaskRepo.task, nil
}
func (s *spyTaskRepo) List(ctx context.Context, projectID string) ([]models.Task, error) {
	s.calls++
	return nil, nil
}
func (s *spyTaskRepo) ListByParentID(ctx context.Context, parentTaskID string) ([]models.Task, error) {
	s.calls++
	return nil, nil
}
func (s *spyTaskRepo) Update(ctx context.Context, t *models.Task) error {
	s.calls++
	return nil
}
func (s *spyTaskRepo) UpdateStatus(ctx context.Context, id string, status models.TaskStatus) error {
	s.calls++
	return nil
}

type spyIntegRepo struct {
	mockIntegRepoForRunner
	calls int
}

func (s *spyIntegRepo) ListByProjectID(ctx context.Context, projectID string) ([]models.ProjectIntegration, error) {
	s.calls++
	return s.mockIntegRepoForRunner.integs, nil
}

type spyCredRepo struct {
	mockCredRepoForRunner
	calls int
}

func (s *spyCredRepo) GetByID(ctx context.Context, id string) (*models.Credential, error) {
	s.calls++
	return s.mockCredRepoForRunner.cred, nil
}
func (s *spyCredRepo) GetByIDAndTeam(ctx context.Context, id, teamID string) (*models.Credential, error) {
	s.calls++
	return s.mockCredRepoForRunner.cred, nil
}

type refusalFixture struct {
	tmpDir   string
	repoPath string
	marker   string
	cfg      *config.Config
	pRepo    *spyProjectRepo
	tRepo    *spyTaskRepo
	lRepo    *mockLogRepo
	cli      *mockDockerClientForRunner
	oc       *mockOpenCodeClientForRunner
	sbMgr    *mockProjectSandboxManagerForRunner
	wsMgr    *mockWorkspaceManagerForRunner
	integ    *spyIntegRepo
	cred     *spyCredRepo
	gh       *mockGHProviderForRunner
	task     *models.Task
	attempt  *models.TaskAttempt
	ar       *runner.AgentTaskRunner
	before   []string
	hub      *ws.Hub
	taskSub  *ws.MockClient
	globSub  *ws.MockClient
}

func snapshotTree(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		entries = append(entries, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to snapshot %s: %v", root, err)
	}
	return entries
}

// newRefusalFixture builds a fully populated runner (repos, docker, OpenCode, integration, managers).
// Every collaborator records calls so tests can prove RunTask touched none of them.
func newRefusalFixture(t *testing.T, id string, withManagers bool) *refusalFixture {
	t.Helper()
	f := &refusalFixture{tmpDir: t.TempDir()}
	f.repoPath = filepath.Join(f.tmpDir, "repo")
	if err := os.MkdirAll(f.repoPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(f.repoPath, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("failed to seed repo dir: %v", err)
	}

	// If the legacy verification step ever ran, it would create this marker.
	f.marker = filepath.Join(f.tmpDir, "verify-ran")
	t.Setenv("ATM_VERIFY_CMD", "touch "+f.marker)

	f.cfg = &config.Config{
		OpenCodeServerURL: "http://127.0.0.1:1",
		HostWorktreeRoot:  filepath.Join(f.tmpDir, "worktrees"),
		HostRepoRoot:      f.tmpDir,
	}
	f.pRepo = &spyProjectRepo{mockProjectRepo: mockProjectRepo{project: &models.Project{ID: id + "-proj", RepoPath: f.repoPath}}}
	f.task = &models.Task{ID: id, ProjectID: id + "-proj", Title: "Refused task", Prompt: "Prompt"}
	f.attempt = &models.TaskAttempt{ID: id + "-attempt", TaskID: f.task.ID}
	f.tRepo = &spyTaskRepo{mockTaskRepo: mockTaskRepo{task: f.task}}
	f.lRepo = &mockLogRepo{}
	f.cli = &mockDockerClientForRunner{}
	f.oc = &mockOpenCodeClientForRunner{}
	f.integ = &spyIntegRepo{mockIntegRepoForRunner: mockIntegRepoForRunner{integs: []models.ProjectIntegration{{
		ID: "integ-refused", ProjectID: f.task.ProjectID, Provider: models.IntegrationGitHub, Enabled: true,
	}}}}
	f.cred = &spyCredRepo{mockCredRepoForRunner: mockCredRepoForRunner{cred: &models.Credential{ID: "cred-refused"}}}
	f.gh = &mockGHProviderForRunner{prURL: "https://example.invalid/pr/1"}
	cipher, err := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}

	opts := []runner.RunnerOption{
		runner.WithRunnerOpenCodeClient(f.oc),
		runner.WithRunnerGitHubIntegration(f.integ, f.cred, cipher, f.gh),
	}
	if withManagers {
		f.sbMgr = &mockProjectSandboxManagerForRunner{}
		f.wsMgr = &mockWorkspaceManagerForRunner{}
		opts = append(opts,
			runner.WithRunnerProjectSandboxManager(f.sbMgr),
			runner.WithRunnerWorkspaceManager(f.wsMgr),
		)
	}
	// A live hub with subscribers on the task room and the global room: any broadcast is observable.
	f.hub = ws.NewHub()
	go f.hub.Run()
	t.Cleanup(f.hub.Stop)
	f.taskSub = ws.NewMockClient("sub-task-"+id, f.task.ID)
	f.globSub = ws.NewMockClient("sub-global-"+id, "global")
	f.hub.Register(f.taskSub)
	f.hub.Register(f.globSub)
	deadline := time.Now().Add(2 * time.Second)
	for !(f.hub.HasSubscribers(f.task.ID) && f.hub.HasSubscribers("global")) {
		if time.Now().After(deadline) {
			t.Fatalf("hub subscribers were not registered")
		}
		time.Sleep(time.Millisecond)
	}

	f.ar = runner.NewAgentTaskRunner(f.cfg, f.pRepo, f.tRepo, f.lRepo,
		git.NewWorktreeManager(f.cfg.HostWorktreeRoot), sandbox.NewRunner(f.cli), f.cli, f.hub, opts...)
	f.before = snapshotTree(t, f.tmpDir)
	return f
}

func assertRuntimeRefused(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected RunTask to be refused, got nil")
	}
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("expected ErrRuntimeExecutionUnavailable, got: %v", err)
	}
	if !errors.Is(err, common.ErrRuntimeQuarantined) {
		t.Fatalf("expected error to wrap common.ErrRuntimeQuarantined, got: %v", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "gate") {
		t.Errorf("refusal message must not mention internal gate names: %q", err.Error())
	}
}

func (f *refusalFixture) assertNoSideEffects(t *testing.T) {
	t.Helper()
	if f.pRepo.calls != 0 {
		t.Errorf("projectRepo must not be touched, got %d calls", f.pRepo.calls)
	}
	if f.tRepo.calls != 0 {
		t.Errorf("taskRepo must not be touched, got %d calls", f.tRepo.calls)
	}
	if f.integ.calls != 0 || f.cred.calls != 0 {
		t.Errorf("integration/credential repos must not be touched, got %d/%d calls", f.integ.calls, f.cred.calls)
	}
	if f.gh.prCreated {
		t.Errorf("pull request provider must not be invoked")
	}
	if len(f.lRepo.logs) != 0 {
		t.Errorf("no logs must be appended, got %d", len(f.lRepo.logs))
	}
	if f.cli.calls != 0 {
		t.Errorf("Docker client must not be called at all, got %d calls", f.cli.calls)
	}
	if f.oc.calls != 0 {
		t.Errorf("OpenCode client must not be called at all, got %d calls", f.oc.calls)
	}
	// Broadcasts are delivered from goroutines; allow them to land before checking.
	time.Sleep(50 * time.Millisecond)
	if n := len(f.taskSub.Messages()); n != 0 {
		t.Errorf("no task-room broadcast may be sent, got %d", n)
	}
	if n := len(f.globSub.Messages()); n != 0 {
		t.Errorf("no global broadcast may be sent, got %d", n)
	}
	if f.cli.createdConfig != nil || len(f.cli.createdConfigs) != 0 || f.cli.stopCalled || f.cli.removeCalled {
		t.Errorf("docker must not be touched: %+v", f.cli)
	}
	if f.oc.boundSessionID != "" || f.oc.boundLocation != "" {
		t.Errorf("OpenCode must not be touched: session=%q location=%q", f.oc.boundSessionID, f.oc.boundLocation)
	}
	if f.sbMgr != nil && f.sbMgr.called {
		t.Errorf("EnsureProjectContainer must never be called")
	}
	if f.wsMgr != nil && f.wsMgr.called {
		t.Errorf("EnsureTaskWorktree must never be called")
	}
	if _, err := os.Stat(f.marker); err == nil {
		t.Errorf("verification command must not run")
	}
	if _, err := os.Stat(f.cfg.HostWorktreeRoot); err == nil {
		t.Errorf("host worktree root must not be created")
	}
	after := snapshotTree(t, f.tmpDir)
	if strings.Join(after, "\n") != strings.Join(f.before, "\n") {
		t.Errorf("filesystem changed.\nbefore: %v\nafter:  %v", f.before, after)
	}
}

func TestRunTask_RefusesWithoutSideEffects(t *testing.T) {
	cases := []struct {
		name         string
		withManagers bool
	}{
		{"nil project sandbox and workspace managers", false},
		{"non-nil project sandbox and workspace managers", true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRefusalFixture(t, fmt.Sprintf("task-refused-%d", i), tc.withManagers)
			err := f.ar.RunTask(context.Background(), f.task, f.attempt)
			assertRuntimeRefused(t, err)
			f.assertNoSideEffects(t)
			if f.ar.IsRunning(f.task.ID) {
				t.Errorf("task must not remain marked as running")
			}
		})
	}
}

func TestRunTask_NilTaskStillRejected(t *testing.T) {
	f := newRefusalFixture(t, "task-nil", true)
	err := f.ar.RunTask(context.Background(), nil, f.attempt)
	if err == nil {
		t.Fatalf("expected error for nil task")
	}
	f.assertNoSideEffects(t)
}

func TestRunTask_RefusesWhenContextAlreadyCancelled(t *testing.T) {
	f := newRefusalFixture(t, "task-cancel", true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := f.ar.RunTask(ctx, f.task, f.attempt)
	assertRuntimeRefused(t, err)
	f.assertNoSideEffects(t)
}

// Legacy: TestRunInDocker_SessionAndTitleFlags
func TestRunInDocker_SessionAndTitleFlags(t *testing.T) {
	f := newRefusalFixture(t, "task-docker-1", false)
	sessionID := "ses_atm_task-docker-1"
	f.task.SessionID = &sessionID
	assertRuntimeRefused(t, f.ar.RunTask(context.Background(), f.task, f.attempt))
	f.assertNoSideEffects(t)
}

func TestRunTask_VerificationLoop_PassFirstTurn(t *testing.T) {
	f := newRefusalFixture(t, "task-verify-pass", false)
	assertRuntimeRefused(t, f.ar.RunTask(context.Background(), f.task, f.attempt))
	f.assertNoSideEffects(t)
}

func TestRunTask_VerificationLoop_FailThenPass(t *testing.T) {
	f := newRefusalFixture(t, "task-verify-fail-pass", false)
	f.cfg.MaxVerificationRetries = 3
	assertRuntimeRefused(t, f.ar.RunTask(context.Background(), f.task, f.attempt))
	f.assertNoSideEffects(t)
}

func TestRunTask_VerificationLoop_MaxRetriesExceeded(t *testing.T) {
	f := newRefusalFixture(t, "task-verify-max", false)
	f.cfg.MaxVerificationRetries = 1
	assertRuntimeRefused(t, f.ar.RunTask(context.Background(), f.task, f.attempt))
	f.assertNoSideEffects(t)
}

func TestAgentTaskRunner_Watch_Success(t *testing.T) {
	f := newRefusalFixture(t, "task-watch-1", false)
	doneCh := f.ar.Watch(f.task.ID)
	go func() {
		_ = f.ar.RunTask(context.Background(), f.task, f.attempt)
	}()

	select {
	case err := <-doneCh:
		assertRuntimeRefused(t, err)
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for task done channel")
	}
	select {
	case _, ok := <-doneCh:
		if ok {
			t.Errorf("expected done channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatalf("done channel was not closed")
	}
	f.assertNoSideEffects(t)
}

func TestAgentTaskRunner_Watch_Failure(t *testing.T) {
	f := newRefusalFixture(t, "task-watch-fail", false)
	f.cfg.MaxVerificationRetries = 1
	doneCh := f.ar.Watch(f.task.ID)
	go func() {
		_ = f.ar.RunTask(context.Background(), f.task, f.attempt)
	}()

	select {
	case err := <-doneCh:
		assertRuntimeRefused(t, err)
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for failing task done channel")
	}
	f.assertNoSideEffects(t)
}

func TestAgentTaskRunner_CancelExecution_StopsDocker(t *testing.T) {
	f := newRefusalFixture(t, "task-cancel-docker", false)
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- f.ar.RunTask(ctx, f.task, f.attempt)
	}()
	cancel()

	select {
	case err := <-doneCh:
		assertRuntimeRefused(t, err)
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for RunTask to return")
	}
	// No container was ever created, so there is nothing to stop or remove.
	f.assertNoSideEffects(t)
}

func TestAgentTaskRunner_Watch_IsRunning(t *testing.T) {
	ar := runner.NewAgentTaskRunner(nil, nil, nil, nil, nil, nil, nil, nil)
	if ar.IsRunning("non-existent-task") {
		t.Errorf("expected IsRunning to be false for non-existent-task")
	}

	// A refused run must also leave IsRunning false (nil collaborators prove nothing is dereferenced).
	task := &models.Task{ID: "task-isrunning"}
	assertRuntimeRefused(t, ar.RunTask(context.Background(), task, &models.TaskAttempt{ID: "a"}))
	if ar.IsRunning(task.ID) {
		t.Errorf("expected IsRunning to be false after refused run")
	}
}

func TestAgentTaskRunner_Watch_CompletedTask(t *testing.T) {
	f := newRefusalFixture(t, "task-watch-cached", false)
	assertRuntimeRefused(t, f.ar.RunTask(context.Background(), f.task, f.attempt))
	if f.ar.IsRunning(f.task.ID) {
		t.Errorf("expected IsRunning to be false after completion")
	}

	// Watch after a refused run delivers the cached sentinel and closes the channel.
	doneCh := f.ar.Watch(f.task.ID)
	select {
	case err, ok := <-doneCh:
		if !ok {
			t.Fatalf("expected cached error to be delivered before close")
		}
		assertRuntimeRefused(t, err)
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for cached watch channel")
	}
	select {
	case _, ok := <-doneCh:
		if ok {
			t.Errorf("expected channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatalf("watch channel was not closed")
	}
	f.assertNoSideEffects(t)
}

func TestAgentTaskRunner_RunTaskAsync(t *testing.T) {
	f := newRefusalFixture(t, "task-async", true)
	doneCh := f.ar.RunTaskAsync(context.Background(), f.task, f.attempt)
	select {
	case err := <-doneCh:
		assertRuntimeRefused(t, err)
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for RunTaskAsync done channel")
	}
	f.assertNoSideEffects(t)
}

func TestRunTask_EnsureSessionLocationBinding(t *testing.T) {
	f := newRefusalFixture(t, "task-bind-location", true)
	assertRuntimeRefused(t, f.ar.RunTask(context.Background(), f.task, f.attempt))
	if f.oc.boundSessionID != "" || f.oc.boundLocation != "" {
		t.Errorf("session must not be bound, got session=%q location=%q", f.oc.boundSessionID, f.oc.boundLocation)
	}
	f.assertNoSideEffects(t)
}

func TestRunTask_GitHubIntegrationSyncAndPushBranch(t *testing.T) {
	f := newRefusalFixture(t, "task-gh-sync-test", true)
	f.integ.integs[0].Config = models.JSONMap{"base_branch": "main", "auto_pr": true}
	assertRuntimeRefused(t, f.ar.RunTask(context.Background(), f.task, f.attempt))
	if f.gh.prCreated {
		t.Errorf("no pull request must be created")
	}
	f.assertNoSideEffects(t)
}

func TestTaskRunner_DockerUnavailable_FailsClosed(t *testing.T) {
	f := newRefusalFixture(t, "task-docker-unavailable", false)
	f.cli.createErr = errors.New("docker daemon unreachable")
	assertRuntimeRefused(t, f.ar.RunTask(context.Background(), f.task, f.attempt))
	f.assertNoSideEffects(t)
}

func TestTaskRunner_ContainerCreateError_FailsClosed(t *testing.T) {
	f := newRefusalFixture(t, "task-create-error", false)
	f.cli.createErr = errors.New("create failed")
	assertRuntimeRefused(t, f.ar.RunTask(context.Background(), f.task, f.attempt))
	if f.cli.removeCalled {
		t.Errorf("ContainerRemove must not be invoked")
	}
	f.assertNoSideEffects(t)
}

func TestAgentTaskRunner_InContainerWorktreePathBinding(t *testing.T) {
	f := newRefusalFixture(t, "task-sandbox-123", true)
	assertRuntimeRefused(t, f.ar.RunTask(context.Background(), f.task, f.attempt))
	if f.sbMgr.called {
		t.Errorf("EnsureProjectContainer must not be called")
	}
	if f.wsMgr.called {
		t.Errorf("EnsureTaskWorktree must not be called")
	}
	f.assertNoSideEffects(t)
}

func TestAgentTaskRunner_NoHostFallbackOnSandboxFailure(t *testing.T) {
	// Case A: the sandbox manager would fail; it must never be consulted and no host fallback may run.
	fa := newRefusalFixture(t, "task-sandbox-fail-a", true)
	fa.sbMgr.ensureFunc = func(ctx context.Context, project *models.Project, imageTag string) (*runner.ProjectRouteHandle, error) {
		return nil, errors.New("container engine exhausted")
	}
	errA := fa.ar.RunTask(context.Background(), fa.task, fa.attempt)
	assertRuntimeRefused(t, errA)
	if strings.Contains(errA.Error(), "container engine exhausted") {
		t.Errorf("sandbox manager must not be consulted")
	}
	fa.assertNoSideEffects(t)

	// Case B: the workspace manager would fail; it must never be consulted.
	fb := newRefusalFixture(t, "task-sandbox-fail-b", true)
	fb.wsMgr.ensureFunc = func(ctx context.Context, projectID string, task *models.Task, repoName string, baseCommit string) (string, error) {
		return "", errors.New("submodule rejected fail-closed")
	}
	errB := fb.ar.RunTask(context.Background(), fb.task, fb.attempt)
	assertRuntimeRefused(t, errB)
	if strings.Contains(errB.Error(), "submodule rejected") {
		t.Errorf("workspace manager must not be consulted")
	}
	fb.assertNoSideEffects(t)
}

func (m *mockDockerClientForRunner) ContainerExecCreate(ctx context.Context, containerID string, options container.ExecOptions) (types.IDResponse, error) {
	return types.IDResponse{ID: "exec-1"}, nil
}

func (m *mockDockerClientForRunner) ContainerExecAttach(ctx context.Context, execID string, config container.ExecAttachOptions) (types.HijackedResponse, error) {
	return types.HijackedResponse{}, nil
}

func (m *mockDockerClientForRunner) ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error) {
	return container.ExecInspect{}, nil
}
