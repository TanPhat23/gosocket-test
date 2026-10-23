package service_test

import (
	"context"
	"errors"
	"testing"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/service"
)

type fileFixture struct {
	svc      service.TaskService
	task     *models.Task
	member   *models.User
	viewer   *models.User
	outsider *models.User
}

func newFileFixture(t *testing.T, list service.TaskFileLister, read service.TaskFileReader) fileFixture {
	t.Helper()
	db := setupTestDB(t)
	team := &models.Team{ID: common.NewID(), Name: "Files Team"}
	db.Create(team)
	newUser := func(name string, role models.TeamRole) *models.User {
		u := &models.User{ID: common.NewID(), Email: name + "@files.test", DisplayName: name, Subject: "sub-" + name, Issuer: "test"}
		db.Create(u)
		if role != 0 {
			db.Create(&models.TeamMembership{TeamID: team.ID, UserID: u.ID, Role: role})
		}
		return u
	}
	member := newUser("member", models.RoleMember)
	viewer := newUser("viewer", models.RoleViewer)
	outsider := newUser("outsider", 0)
	project := &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "Files Project"}
	db.Create(project)
	task := &models.Task{
		ID: common.NewID(), ProjectID: project.ID, Title: "Files", Prompt: "p",
		Status: models.TaskStatusReview, Priority: models.PriorityNormal,
		Skills: models.StringArray{}, MCPServers: models.StringArray{},
	}
	db.Create(task)
	svc := service.NewTaskService(repository.NewTaskRepository(db), repository.NewAttemptRepository(db), &mockQueueClient{}, nil,
		service.WithTaskSessionDependencies(nil, repository.NewProjectRepository(db), repository.NewTeamRepository(db)),
		service.WithTaskFileBrowser(list, read),
	)
	return fileFixture{svc: svc, task: task, member: member, viewer: viewer, outsider: outsider}
}

func TestTaskService_ListTaskFiles_AuthorizesAndReturnsTheListing(t *testing.T) {
	ctx := context.Background()
	var calls int
	list := func(_ context.Context, task *models.Task) (string, []string, error) {
		calls++
		return "workspace", []string{"README.md", "src/main.go"}, nil
	}
	f := newFileFixture(t, list, nil)

	resp, err := f.svc.ListTaskFiles(ctx, f.task.ID, f.viewer.ID)
	if err != nil {
		t.Fatalf("viewer ListTaskFiles: %v", err)
	}
	if resp.Source != "workspace" || len(resp.Files) != 2 || resp.TaskID != f.task.ID {
		t.Fatalf("unexpected listing %+v", resp)
	}
	for _, userID := range []string{f.outsider.ID, ""} {
		if _, err := f.svc.ListTaskFiles(ctx, f.task.ID, userID); !errors.Is(err, common.ErrForbidden) {
			t.Errorf("user %q: expected ErrForbidden, got %v", userID, err)
		}
	}
	if calls != 1 {
		t.Errorf("unauthorized callers reached the workspace: %d calls", calls)
	}
}

func TestTaskService_ReadTaskFile_ReportsMissingAndDeniedAsNotFound(t *testing.T) {
	ctx := context.Background()
	read := func(_ context.Context, _ *models.Task, path string, _ int64) (*runner.TaskFileContent, error) {
		return &runner.TaskFileContent{Path: path, Missing: path == "gone.go", Denied: path == "link.go"}, nil
	}
	f := newFileFixture(t, nil, read)

	for _, path := range []string{"gone.go", "link.go"} {
		if _, err := f.svc.ReadTaskFile(ctx, f.task.ID, f.viewer.ID, path); !errors.Is(err, common.ErrNotFound) {
			t.Errorf("%s: expected not found, got %v", path, err)
		}
	}
}

func TestTaskService_ReadTaskFile_ReturnsContentAndFlags(t *testing.T) {
	ctx := context.Background()
	read := func(_ context.Context, _ *models.Task, path string, max int64) (*runner.TaskFileContent, error) {
		if max != taskFileCap() {
			t.Errorf("expected the explorer cap, got %d", max)
		}
		return &runner.TaskFileContent{Path: path, Source: "workspace", Size: 9, Content: []byte("package x"), Truncated: true}, nil
	}
	f := newFileFixture(t, nil, read)

	resp, err := f.svc.ReadTaskFile(ctx, f.task.ID, f.member.ID, "main.go")
	if err != nil {
		t.Fatalf("ReadTaskFile: %v", err)
	}
	if resp.Content != "package x" || !resp.Truncated || resp.Size != 9 || resp.Source != "workspace" {
		t.Fatalf("unexpected file %+v", resp)
	}
	if _, err := f.svc.ReadTaskFile(ctx, f.task.ID, f.outsider.ID, "main.go"); !errors.Is(err, common.ErrForbidden) {
		t.Errorf("outsider: expected ErrForbidden, got %v", err)
	}
}

func TestTaskService_FileBrowser_UnwiredRefuses(t *testing.T) {
	f := newFileFixture(t, nil, nil)
	if _, err := f.svc.ListTaskFiles(context.Background(), f.task.ID, f.viewer.ID); !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Errorf("unwired listing: expected runtime-unavailable, got %v", err)
	}
	if _, err := f.svc.ReadTaskFile(context.Background(), f.task.ID, f.viewer.ID, "a.go"); !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Errorf("unwired read: expected runtime-unavailable, got %v", err)
	}
}

// taskFileCap mirrors the explorer's per-file cap, so the test can check what the service asks for.
func taskFileCap() int64 { return 512 * 1024 }
