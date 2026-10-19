package service_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/service"
	"gorm.io/gorm"
)

// writeSpyTaskRepo counts every write and every read that could expose task data.
type writeSpyTaskRepo struct {
	repository.TaskRepository
	creates, updates, statusUpdates, reads atomic.Int64
}

func (r *writeSpyTaskRepo) Create(ctx context.Context, t *models.Task) error {
	r.creates.Add(1)
	return r.TaskRepository.Create(ctx, t)
}
func (r *writeSpyTaskRepo) Update(ctx context.Context, t *models.Task) error {
	r.updates.Add(1)
	return r.TaskRepository.Update(ctx, t)
}
func (r *writeSpyTaskRepo) UpdateStatus(ctx context.Context, id string, s models.TaskStatus) error {
	r.statusUpdates.Add(1)
	return r.TaskRepository.UpdateStatus(ctx, id, s)
}
func (r *writeSpyTaskRepo) GetByID(ctx context.Context, id string) (*models.Task, error) {
	r.reads.Add(1)
	return r.TaskRepository.GetByID(ctx, id)
}
func (r *writeSpyTaskRepo) List(ctx context.Context, projectID string) ([]models.Task, error) {
	r.reads.Add(1)
	return r.TaskRepository.List(ctx, projectID)
}
func (r *writeSpyTaskRepo) ListByParentID(ctx context.Context, id string) ([]models.Task, error) {
	r.reads.Add(1)
	return r.TaskRepository.ListByParentID(ctx, id)
}
func (r *writeSpyTaskRepo) writes() int64 {
	return r.creates.Load() + r.updates.Load() + r.statusUpdates.Load()
}

type enqueueSpy struct{ enqueues atomic.Int64 }

func (q *enqueueSpy) EnqueueTask(ctx context.Context, task *models.Task) (string, error) {
	q.enqueues.Add(1)
	return "spy-id", nil
}
func (q *enqueueSpy) CancelTask(ctx context.Context, queueName, asynqTaskID string) error { return nil }
func (q *enqueueSpy) Close() error                                                        { return nil }

type authzFixture struct {
	db                                *gorm.DB
	spy                               *writeSpyTaskRepo
	queue                             *enqueueSpy
	svc                               service.TaskService
	svcNoRepos                        service.TaskService
	teamA, teamB                      *models.Team
	projA, projB                      *models.Project
	memberA, viewerA, memberB, outsid *models.User
	taskA, taskB                      *models.Task
}

func newAuthzFixture(t *testing.T) *authzFixture {
	t.Helper()
	db := setupTestDB(t)
	f := &authzFixture{db: db, queue: &enqueueSpy{}}
	f.spy = &writeSpyTaskRepo{TaskRepository: repository.NewTaskRepository(db)}

	mk := func(name string) *models.User {
		u := &models.User{ID: common.NewID(), Email: name + "@example.com", DisplayName: name, Subject: "sub-" + name + common.NewID(), Issuer: "test"}
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("user: %v", err)
		}
		return u
	}
	f.memberA, f.viewerA, f.memberB, f.outsid = mk("ma"), mk("va"), mk("mb"), mk("out")

	f.teamA = &models.Team{ID: common.NewID(), Name: "A", Slug: "a-" + common.NewID()}
	f.teamB = &models.Team{ID: common.NewID(), Name: "B", Slug: "b-" + common.NewID()}
	db.Create(f.teamA)
	db.Create(f.teamB)
	db.Create(&models.TeamMembership{TeamID: f.teamA.ID, UserID: f.memberA.ID, Role: models.RoleMember})
	db.Create(&models.TeamMembership{TeamID: f.teamA.ID, UserID: f.viewerA.ID, Role: models.RoleViewer})
	db.Create(&models.TeamMembership{TeamID: f.teamB.ID, UserID: f.memberB.ID, Role: models.RoleMember})

	f.projA = &models.Project{ID: common.NewID(), TeamID: f.teamA.ID, Name: "PA", RepoPath: "/a"}
	f.projB = &models.Project{ID: common.NewID(), TeamID: f.teamB.ID, Name: "PB", RepoPath: "/b"}
	db.Create(f.projA)
	db.Create(f.projB)

	now := time.Now().UTC()
	mkTask := func(p *models.Project, title string, age time.Duration) *models.Task {
		tk := &models.Task{ID: common.NewID(), ProjectID: p.ID, Title: title, Prompt: "p", AssigneeID: "a",
			AssigneeType: models.AssigneeAgent, Status: models.TaskStatusQueued, Priority: models.PriorityNormal,
			Skills: models.StringArray{}, MCPServers: models.StringArray{}, CreatedAt: now.Add(-age)}
		if err := db.Create(tk).Error; err != nil {
			t.Fatalf("task: %v", err)
		}
		return tk
	}
	f.taskA = mkTask(f.projA, "task-a", time.Minute)
	f.taskB = mkTask(f.projB, "task-b", 2*time.Minute)

	projectRepo, teamRepo := repository.NewProjectRepository(db), repository.NewTeamRepository(db)
	f.svc = service.NewTaskService(f.spy, repository.NewAttemptRepository(db), f.queue, nil,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo))
	f.svcNoRepos = service.NewTaskService(f.spy, repository.NewAttemptRepository(db), f.queue, nil)
	return f
}

func (f *authzFixture) createReq(projectID string) *dto.CreateTaskRequest {
	return &dto.CreateTaskRequest{ProjectID: projectID, Title: "t", Prompt: "p", AssigneeID: "a",
		AssigneeType: models.AssigneeAgent, Priority: int(models.PriorityNormal)}
}

func (f *authzFixture) taskCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := f.db.Model(&models.Task{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestTaskAuthz_GetTask(t *testing.T) {
	f := newAuthzFixture(t)
	ctx := context.Background()

	got, err := f.svc.GetTask(ctx, f.taskA.ID, f.viewerA.ID)
	if err != nil || got == nil || got.ID != f.taskA.ID {
		t.Fatalf("viewer must read own team's task, got %+v / %v", got, err)
	}
	for name, uid := range map[string]string{"empty user": "", "outsider": f.outsid.ID, "other team member": f.memberB.ID} {
		got, err := f.svc.GetTask(ctx, f.taskA.ID, uid)
		if got != nil || !errors.Is(err, common.ErrForbidden) {
			t.Errorf("%s: expected nil + ErrForbidden, got %+v / %v", name, got, err)
		}
	}
	if _, err := f.svc.GetTask(ctx, "does-not-exist", f.viewerA.ID); err == nil || errors.Is(err, common.ErrForbidden) {
		t.Errorf("unknown task keeps existing not-found behaviour, got %v", err)
	}
}

func TestTaskAuthz_QuarantinedRoutesAuthorizeBeforeRefusing(t *testing.T) {
	f := newAuthzFixture(t)
	ctx := context.Background()
	calls := map[string]func(taskID, userID string) error{
		"CreatePullRequestForTask": func(id, u string) error { _, err := f.svc.CreatePullRequestForTask(ctx, id, u); return err },
		"GetTaskDiff":              func(id, u string) error { _, err := f.svc.GetTaskDiff(ctx, id, u); return err },
		"SyncTaskPRStatus":         func(id, u string) error { _, err := f.svc.SyncTaskPRStatus(ctx, id, u); return err },
	}
	for name, call := range calls {
		if err := call(f.taskA.ID, f.memberA.ID); !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
			t.Errorf("%s member: expected quarantine refusal after authz, got %v", name, err)
		}
		// Opening a PR and syncing its status need RoleMember; the diff stays readable by viewers.
		viewerErr := call(f.taskA.ID, f.viewerA.ID)
		if name == "GetTaskDiff" {
			if !errors.Is(viewerErr, runner.ErrRuntimeExecutionUnavailable) {
				t.Errorf("%s viewer: expected quarantine refusal after authz, got %v", name, viewerErr)
			}
		} else if !errors.Is(viewerErr, common.ErrForbidden) || errors.Is(viewerErr, common.ErrRuntimeQuarantined) {
			t.Errorf("%s viewer: expected ErrForbidden, got %v", name, viewerErr)
		}
		for who, uid := range map[string]string{"empty": "", "outsider": f.outsid.ID, "other team": f.memberB.ID} {
			err := call(f.taskA.ID, uid)
			if !errors.Is(err, common.ErrForbidden) || errors.Is(err, common.ErrRuntimeQuarantined) {
				t.Errorf("%s %s: expected ErrForbidden (never quarantine 503), got %v", name, who, err)
			}
		}
		if err := call("does-not-exist", f.viewerA.ID); err == nil || errors.Is(err, common.ErrRuntimeQuarantined) || errors.Is(err, common.ErrForbidden) {
			t.Errorf("%s unknown task: expected plain lookup error, got %v", name, err)
		}
	}
	if f.spy.writes() != 0 || f.queue.enqueues.Load() != 0 {
		t.Errorf("no writes/enqueues expected, got writes=%d enqueues=%d", f.spy.writes(), f.queue.enqueues.Load())
	}
}

func TestTaskAuthz_ListTasks(t *testing.T) {
	f := newAuthzFixture(t)
	ctx := context.Background()

	idsOf := func(ts []models.Task) []string {
		out := []string{}
		for _, x := range ts {
			out = append(out, x.ID)
		}
		return out
	}

	// Without project_id: only the caller's teams' tasks.
	for name, c := range map[string]struct {
		user string
		want []string
	}{
		"member of A": {f.memberA.ID, []string{f.taskA.ID}},
		"viewer of A": {f.viewerA.ID, []string{f.taskA.ID}},
		"member of B": {f.memberB.ID, []string{f.taskB.ID}},
		"outsider":    {f.outsid.ID, []string{}},
	} {
		got, err := f.svc.ListTasks(ctx, "", c.user)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		g := idsOf(got)
		if len(g) != len(c.want) || (len(g) == 1 && g[0] != c.want[0]) {
			t.Errorf("%s: want %v got %v", name, c.want, g)
		}
	}

	// A user in both teams sees both, newest first.
	f.db.Create(&models.TeamMembership{TeamID: f.teamB.ID, UserID: f.viewerA.ID, Role: models.RoleViewer})
	got, err := f.svc.ListTasks(ctx, "", f.viewerA.ID)
	if err != nil || len(got) != 2 || got[0].ID != f.taskA.ID || got[1].ID != f.taskB.ID {
		t.Errorf("expected [taskA taskB] newest first, got %v / %v", idsOf(got), err)
	}

	// With project_id: viewer ok, non-member 403 (not an empty list), unknown project 403.
	if got, err := f.svc.ListTasks(ctx, f.projA.ID, f.memberA.ID); err != nil || len(got) != 1 {
		t.Errorf("member list own project: %v / %v", idsOf(got), err)
	}
	for name, c := range map[string]struct{ project, user string }{
		"other team":      {f.projA.ID, f.memberB.ID},
		"outsider":        {f.projB.ID, f.outsid.ID},
		"unknown project": {common.NewID(), f.memberA.ID},
	} {
		got, err := f.svc.ListTasks(ctx, c.project, c.user)
		if got != nil || !errors.Is(err, common.ErrForbidden) {
			t.Errorf("%s: expected nil + ErrForbidden, got %v / %v", name, idsOf(got), err)
		}
	}

	// Empty user and missing wiring fail closed.
	if _, err := f.svc.ListTasks(ctx, "", ""); !errors.Is(err, common.ErrForbidden) {
		t.Errorf("empty user (all): %v", err)
	}
	if _, err := f.svc.ListTasks(ctx, f.projA.ID, ""); !errors.Is(err, common.ErrForbidden) {
		t.Errorf("empty user (project): %v", err)
	}
	if _, err := f.svcNoRepos.ListTasks(ctx, "", f.memberA.ID); !errors.Is(err, common.ErrServiceUnavailable) {
		t.Errorf("no repos (all): %v", err)
	}
	if _, err := f.svcNoRepos.ListTasks(ctx, f.projA.ID, f.memberA.ID); !errors.Is(err, common.ErrServiceUnavailable) {
		t.Errorf("no repos (project): %v", err)
	}
}

func TestTaskAuthz_ListSubtasks(t *testing.T) {
	f := newAuthzFixture(t)
	ctx := context.Background()
	child := &models.Task{ID: common.NewID(), ProjectID: f.projA.ID, ParentTaskID: &f.taskA.ID, Title: "c", Prompt: "p",
		AssigneeID: "a", AssigneeType: models.AssigneeAgent, Status: models.TaskStatusQueued, Priority: models.PriorityNormal,
		Skills: models.StringArray{}, MCPServers: models.StringArray{}}
	foreign := &models.Task{ID: common.NewID(), ProjectID: f.projB.ID, ParentTaskID: &f.taskA.ID, Title: "x", Prompt: "p",
		AssigneeID: "a", AssigneeType: models.AssigneeAgent, Status: models.TaskStatusQueued, Priority: models.PriorityNormal,
		Skills: models.StringArray{}, MCPServers: models.StringArray{}}
	f.db.Create(child)
	f.db.Create(foreign)

	got, err := f.svc.ListSubtasks(ctx, f.taskA.ID, f.viewerA.ID)
	if err != nil || len(got) != 1 || got[0].ID != child.ID {
		t.Fatalf("viewer: expected only same-project child, got %+v / %v", got, err)
	}
	for name, uid := range map[string]string{"empty": "", "outsider": f.outsid.ID, "other team": f.memberB.ID} {
		got, err := f.svc.ListSubtasks(ctx, f.taskA.ID, uid)
		if got != nil || !errors.Is(err, common.ErrForbidden) {
			t.Errorf("%s: expected nil + ErrForbidden, got %+v / %v", name, got, err)
		}
	}
}

func TestTaskAuthz_CreateTask(t *testing.T) {
	f := newAuthzFixture(t)
	ctx := context.Background()
	before := f.taskCount(t)

	denied := map[string]struct {
		svc     service.TaskService
		project string
		user    string
		want    error
	}{
		"empty user":      {f.svc, f.projA.ID, "", common.ErrForbidden},
		"viewer":          {f.svc, f.projA.ID, f.viewerA.ID, common.ErrForbidden},
		"other team":      {f.svc, f.projA.ID, f.memberB.ID, common.ErrForbidden},
		"outsider":        {f.svc, f.projA.ID, f.outsid.ID, common.ErrForbidden},
		"unknown project": {f.svc, common.NewID(), f.memberA.ID, common.ErrForbidden},
		"no repos wired":  {f.svcNoRepos, f.projA.ID, f.memberA.ID, common.ErrServiceUnavailable},
	}
	for name, c := range denied {
		got, err := c.svc.CreateTask(ctx, f.createReq(c.project), c.user)
		if got != nil || !errors.Is(err, c.want) {
			t.Errorf("%s: expected nil + %v, got %+v / %v", name, c.want, got, err)
		}
	}
	if f.spy.writes() != 0 || f.queue.enqueues.Load() != 0 || f.taskCount(t) != before {
		t.Errorf("denied create must not write or enqueue: writes=%d enqueues=%d rows %d->%d",
			f.spy.writes(), f.queue.enqueues.Load(), before, f.taskCount(t))
	}

	created, err := f.svc.CreateTask(ctx, f.createReq(f.projA.ID), f.memberA.ID)
	if err != nil || created == nil || f.taskCount(t) != before+1 || f.queue.enqueues.Load() != 1 {
		t.Fatalf("member create failed: %+v / %v", created, err)
	}
	if created.CreatedByID == nil || *created.CreatedByID != f.memberA.ID {
		t.Errorf("created_by_id = %v, want the creating user %s", created.CreatedByID, f.memberA.ID)
	}
}

func TestTaskAuthz_CreateTask_ParentAndIntegrationMustMatchProject(t *testing.T) {
	f := newAuthzFixture(t)
	ctx := context.Background()
	before := f.taskCount(t)

	// Parent in another project (member of A creating in A with B's task as parent).
	req := f.createReq(f.projA.ID)
	req.ParentTaskID = &f.taskB.ID
	if _, err := f.svc.CreateTask(ctx, req, f.memberA.ID); err == nil {
		t.Errorf("cross-project parent must be rejected")
	}
	// Unknown parent.
	unknown := common.NewID()
	req = f.createReq(f.projA.ID)
	req.ParentTaskID = &unknown
	if _, err := f.svc.CreateTask(ctx, req, f.memberA.ID); err == nil {
		t.Errorf("unknown parent must be rejected")
	}
	// Same-project parent is fine.
	req = f.createReq(f.projA.ID)
	req.ParentTaskID = &f.taskA.ID
	if _, err := f.svc.CreateTask(ctx, req, f.memberA.ID); err != nil {
		t.Errorf("same-project parent must be accepted: %v", err)
	}

	// Integration from another project / unwired integration repo.
	cred := &models.Credential{ID: common.NewID(), TeamID: f.teamB.ID, Provider: models.ProviderGitHub, Label: "l", EncryptedData: "x"}
	f.db.Create(cred)
	integB := &models.ProjectIntegration{ID: common.NewID(), ProjectID: f.projB.ID, CredentialID: cred.ID,
		Provider: models.IntegrationGitHub, ExternalID: "o/r", Enabled: true}
	f.db.Create(integB)
	integRepo := repository.NewIntegrationRepository(f.db)
	svcInteg := service.NewTaskService(f.spy, repository.NewAttemptRepository(f.db), f.queue, nil,
		service.WithTaskSessionDependencies(nil, repository.NewProjectRepository(f.db), repository.NewTeamRepository(f.db)),
		service.WithTaskGitHubIntegration(integRepo, nil, nil, nil))
	req = f.createReq(f.projA.ID)
	req.IntegrationID = &integB.ID
	if _, err := svcInteg.CreateTask(ctx, req, f.memberA.ID); err == nil {
		t.Errorf("cross-project integration must be rejected")
	}
	if _, err := f.svc.CreateTask(ctx, req, f.memberA.ID); !errors.Is(err, common.ErrServiceUnavailable) {
		t.Errorf("integration id without integration repo must fail closed, got %v", err)
	}
	if got := f.taskCount(t); got != before+1 {
		t.Errorf("only the same-project-parent create may persist: rows %d -> %d", before, got)
	}
}

// Without project/team repositories task authorization cannot be decided, so task-addressed
// operations fail closed instead of skipping the membership check.
func TestTaskAuthz_TaskOperationsFailClosedWithoutRepos(t *testing.T) {
	f := newAuthzFixture(t)
	ctx := context.Background()
	if got, err := f.svcNoRepos.GetTask(ctx, f.taskA.ID, f.outsid.ID); got != nil || !errors.Is(err, common.ErrServiceUnavailable) {
		t.Errorf("GetTask: expected nil + ErrServiceUnavailable, got %+v / %v", got, err)
	}
	if got, err := f.svcNoRepos.ListSubtasks(ctx, f.taskA.ID, f.outsid.ID); got != nil || !errors.Is(err, common.ErrServiceUnavailable) {
		t.Errorf("ListSubtasks: expected nil + ErrServiceUnavailable, got %+v / %v", got, err)
	}
	if err := f.svcNoRepos.CancelTask(ctx, f.taskA.ID, f.outsid.ID); !errors.Is(err, common.ErrServiceUnavailable) {
		t.Errorf("CancelTask: expected ErrServiceUnavailable, got %v", err)
	}
	if f.spy.writes() != 0 {
		t.Errorf("fail-closed authorization must not write, got %d writes", f.spy.writes())
	}
}

func TestTaskAuthz_GetTaskExposesLatestFailureReasonOnlyWhenFailed(t *testing.T) {
	f := newAuthzFixture(t)
	ctx := context.Background()
	reason := "runtime execution is temporarily disabled"
	if err := f.db.Create(&models.TaskAttempt{ID: common.NewID(), TaskID: f.taskA.ID, AttemptNumber: 1, Status: models.AttemptFailed, FailureReason: &reason}).Error; err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.GetTask(ctx, f.taskA.ID, f.viewerA.ID)
	if err != nil || got.FailureReason != nil {
		t.Fatalf("non-failed task must not expose a reason, got %v / %v", got.FailureReason, err)
	}
	if err := f.db.Model(&models.Task{}).Where("id = ?", f.taskA.ID).Update("status", models.TaskStatusFailed).Error; err != nil {
		t.Fatal(err)
	}
	got, err = f.svc.GetTask(ctx, f.taskA.ID, f.viewerA.ID)
	if err != nil || got.FailureReason == nil || *got.FailureReason != reason {
		t.Fatalf("failed task must expose the latest attempt's reason, got %v / %v", got.FailureReason, err)
	}
	if _, err := f.svc.GetTask(ctx, f.taskA.ID, f.outsid.ID); !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("outsider must stay forbidden, got %v", err)
	}
}
