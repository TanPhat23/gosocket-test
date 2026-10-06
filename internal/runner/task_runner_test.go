package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"agent-task-manager/internal/config"
	"agent-task-manager/internal/git"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/ws"
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
