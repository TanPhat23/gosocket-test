package service_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync/atomic"
	"testing"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/service"
	"gorm.io/gorm"
)

// countingCredRepo counts credential lookups; a lookup is the precondition for any PAT decrypt.
type countingCredRepo struct {
	repository.CredentialRepository
	getByID atomic.Int64
}

func (c *countingCredRepo) GetByID(ctx context.Context, id string) (*models.Credential, error) {
	c.getByID.Add(1)
	return c.CredentialRepository.GetByID(ctx, id)
}

// countingTaskRepo counts List calls made by the reconciler.
type countingTaskRepo struct {
	repository.TaskRepository
	listCalls atomic.Int64
}

func (c *countingTaskRepo) List(ctx context.Context, projectID string) ([]models.Task, error) {
	c.listCalls.Add(1)
	return c.TaskRepository.List(ctx, projectID)
}

// countingSyncProvider records every GitHub issue write the webhook could perform.
type countingSyncProvider struct {
	updateCalls  atomic.Int64
	commentCalls atomic.Int64
}

func (p *countingSyncProvider) Provider() models.IntegrationProvider { return models.IntegrationGitHub }
func (p *countingSyncProvider) FetchIssues(ctx context.Context, integration *models.ProjectIntegration, plainSecret string) ([]integrations.ExternalIssue, error) {
	return nil, nil
}
func (p *countingSyncProvider) UpdateIssueStatus(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, externalID string, status string) error {
	p.updateCalls.Add(1)
	return nil
}
func (p *countingSyncProvider) AddComment(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, externalID string, comment string) error {
	p.commentCalls.Add(1)
	return nil
}

type quarantineFixture struct {
	db        *gorm.DB
	taskRepo  repository.TaskRepository
	credRepo  *countingCredRepo
	integRepo repository.IntegrationRepository
	cipher    *crypto.SecretCipher
	owner     *models.User
	viewer    *models.User
	outsider  *models.User
	project   *models.Project
	integ     *models.ProjectIntegration
}

func newQuarantineFixture(t *testing.T) *quarantineFixture {
	t.Helper()
	db := setupTestDB(t)
	cipher, err := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	f := &quarantineFixture{
		db:        db,
		taskRepo:  repository.NewTaskRepository(db),
		credRepo:  &countingCredRepo{CredentialRepository: repository.NewCredentialRepository(db)},
		integRepo: repository.NewIntegrationRepository(db),
		cipher:    cipher,
	}
	team := &models.Team{ID: common.NewID(), Name: "RQ Team", Slug: "rq-team-" + common.NewID()}
	db.Create(team)
	f.owner = &models.User{ID: common.NewID(), Email: "rq-owner@example.com", DisplayName: "O", Subject: "sub-rq-o", Issuer: "test"}
	f.viewer = &models.User{ID: common.NewID(), Email: "rq-viewer@example.com", DisplayName: "V", Subject: "sub-rq-v", Issuer: "test"}
	f.outsider = &models.User{ID: common.NewID(), Email: "rq-out@example.com", DisplayName: "X", Subject: "sub-rq-x", Issuer: "test"}
	db.Create(f.owner)
	db.Create(f.viewer)
	db.Create(f.outsider)
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: f.owner.ID, Role: models.RoleOwner})
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: f.viewer.ID, Role: models.RoleViewer})
	f.project = &models.Project{ID: common.NewID(), TeamID: team.ID, Name: "RQ Project"}
	db.Create(f.project)

	credEnc, _ := cipher.Encrypt("pat-test")
	cred := &models.Credential{ID: common.NewID(), TeamID: team.ID, Provider: models.ProviderGitHub, Label: "PAT", EncryptedData: credEnc}
	db.Create(cred)
	f.integ = &models.ProjectIntegration{
		ID: common.NewID(), ProjectID: f.project.ID, CredentialID: cred.ID,
		Provider: models.IntegrationGitHub, ExternalID: "owner/rq-repo", Enabled: true,
		Config: models.JSONMap{"webhook_secret": "rq-wh-secret"},
	}
	db.Create(f.integ)
	return f
}

func (f *quarantineFixture) createReviewTaskWithPR(t *testing.T) *models.Task {
	t.Helper()
	prURL := "https://github.com/owner/rq-repo/pull/7"
	externalID := "7"
	sessionID := "ses_atm_rq_" + common.NewID()
	asynqID := "asynq-rq-" + common.NewID()
	task := &models.Task{
		ID: common.NewID(), ProjectID: f.project.ID, Title: "RQ", Prompt: "p",
		PRURL: &prURL, ExternalID: &externalID, SessionID: &sessionID, AsynqTaskID: &asynqID,
		Status: models.TaskStatusReview, Priority: models.PriorityNormal,
		Skills: models.StringArray{}, MCPServers: models.StringArray{},
	}
	if err := f.db.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func assertQuarantineErr(t *testing.T, label string, err error) {
	t.Helper()
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) || !errors.Is(err, common.ErrRuntimeQuarantined) {
		t.Fatalf("%s: expected runtime quarantine error, got %v", label, err)
	}
}

// Item A: a nil runner must fail closed instead of recording a succeeded attempt.
func TestTaskService_ExecuteTask_NilRunnerFailsClosed(t *testing.T) {
	var typedNil *mockRunnerCapture
	cases := map[string]service.TaskRunner{
		"untyped nil": nil,
		"typed nil":   typedNil,
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			gormDB := setupTestDB(t)
			_, _, task := createTestUserAndTask(t, gormDB)
			taskRepo := repository.NewTaskRepository(gormDB)
			attemptRepo := repository.NewAttemptRepository(gormDB)
			svc := service.NewTaskService(taskRepo, attemptRepo, &mockQueueClient{}, nil)

			var err error
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Fatalf("ExecuteTask panicked: %v", p)
					}
				}()
				err = svc.ExecuteTask(context.Background(), task.ID, r)
			}()
			assertQuarantineErr(t, name, err)

			reloaded, getErr := taskRepo.GetByID(context.Background(), task.ID)
			if getErr != nil {
				t.Fatalf("reload: %v", getErr)
			}
			if reloaded.Status != models.TaskStatusFailed {
				t.Errorf("expected task status %s, got %s", models.TaskStatusFailed, reloaded.Status)
			}
			var attempts []models.TaskAttempt
			if dbErr := gormDB.Where("task_id = ?", task.ID).Find(&attempts).Error; dbErr != nil {
				t.Fatalf("list attempts: %v", dbErr)
			}
			if len(attempts) != 1 {
				t.Fatalf("expected exactly 1 attempt, got %d", len(attempts))
			}
			if attempts[0].Status != models.AttemptFailed {
				t.Errorf("expected attempt status %s, got %s", models.AttemptFailed, attempts[0].Status)
			}
			if attempts[0].FailureReason == nil || *attempts[0].FailureReason == "" {
				t.Errorf("expected a failure reason on the attempt")
			}
		})
	}
}

// Item B: SyncTaskPRStatus refuses after authz without credential lookup, decrypt, or GitHub call.
func TestTaskService_SyncTaskPRStatus_QuarantineHasNoSideEffects(t *testing.T) {
	f := newQuarantineFixture(t)
	task := f.createReviewTaskWithPR(t)
	provider := &countingPRProvider{}
	svc := service.NewTaskService(f.taskRepo, repository.NewAttemptRepository(f.db), &mockQueueClient{}, nil,
		service.WithTaskSessionDependencies(nil, repository.NewProjectRepository(f.db), repository.NewTeamRepository(f.db)),
		service.WithTaskGitHubIntegration(f.integRepo, f.credRepo, f.cipher, provider),
	)
	ctx := context.Background()

	for _, userID := range []string{f.owner.ID, f.viewer.ID} {
		got, err := svc.SyncTaskPRStatus(ctx, task.ID, userID)
		assertQuarantineErr(t, "userID="+userID, err)
		if got != nil {
			t.Errorf("userID=%q: expected nil task, got %+v", userID, got)
		}
	}
	if _, err := svc.SyncTaskPRStatus(ctx, task.ID, f.outsider.ID); !errors.Is(err, common.ErrForbidden) {
		t.Errorf("expected ErrForbidden for outsider, got %v", err)
	}
	if _, err := svc.SyncTaskPRStatus(ctx, task.ID, ""); !errors.Is(err, common.ErrForbidden) {
		t.Errorf("expected ErrForbidden for empty userID, got %v", err)
	}
	if _, err := svc.SyncTaskPRStatus(ctx, "does-not-exist", f.viewer.ID); err == nil || errors.Is(err, common.ErrRuntimeQuarantined) || errors.Is(err, common.ErrForbidden) {
		t.Errorf("expected lookup error for unknown task, got %v", err)
	}
	if n := f.credRepo.getByID.Load(); n != 0 {
		t.Errorf("expected zero credential lookups, got %d", n)
	}
	if provider.statusCalls != 0 || provider.createCalls != 0 {
		t.Errorf("expected zero GitHub calls, got status=%d create=%d", provider.statusCalls, provider.createCalls)
	}
	reloaded, _ := f.taskRepo.GetByID(ctx, task.ID)
	if reloaded.Status != models.TaskStatusReview {
		t.Errorf("task status must not change, got %s", reloaded.Status)
	}
}

// Item B: the reconciler is a no-op while quarantined.
func TestTaskService_ReconcileActivePRs_QuarantineIsNoOp(t *testing.T) {
	f := newQuarantineFixture(t)
	task := f.createReviewTaskWithPR(t)
	taskRepo := &countingTaskRepo{TaskRepository: f.taskRepo}
	provider := &countingPRProvider{}
	svc := service.NewTaskService(taskRepo, repository.NewAttemptRepository(f.db), &mockQueueClient{}, nil,
		service.WithTaskGitHubIntegration(f.integRepo, f.credRepo, f.cipher, provider),
	)

	if err := svc.ReconcileActivePRs(context.Background()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if n := taskRepo.listCalls.Load(); n != 0 {
		t.Errorf("expected zero task List calls, got %d", n)
	}
	if n := f.credRepo.getByID.Load(); n != 0 {
		t.Errorf("expected zero credential lookups, got %d", n)
	}
	if provider.statusCalls != 0 {
		t.Errorf("expected zero GitHub calls, got %d", provider.statusCalls)
	}
	reloaded, _ := f.taskRepo.GetByID(context.Background(), task.ID)
	if reloaded.Status != models.TaskStatusReview {
		t.Errorf("task status must not change, got %s", reloaded.Status)
	}
}

// Item C: a merged-PR webhook still marks the task done but performs no GitHub writes.
func TestHandleGitHubWebhook_PullRequestMerged_QuarantineSkipsGitHubWrites(t *testing.T) {
	f := newQuarantineFixture(t)
	task := f.createReviewTaskWithPR(t)
	branch := "task/" + task.ID
	task.BranchName = &branch
	if err := f.taskRepo.Update(context.Background(), task); err != nil {
		t.Fatalf("update task: %v", err)
	}
	sync := &countingSyncProvider{}
	queueClient := &mockQueueClient{}
	svc := service.NewWebhookService(f.integRepo, f.credRepo, f.taskRepo, integrations.NewMemoryIdempotencyStore(), f.cipher, queueClient,
		service.WithWebhookSyncProvider(sync))

	payload := []byte(`{"action":"closed","repository":{"full_name":"owner/rq-repo"},` +
		`"pull_request":{"number":7,"state":"closed","merged":true,"html_url":"https://github.com/owner/rq-repo/pull/7",` +
		`"head":{"ref":"` + branch + `"}}}`)
	mac := hmac.New(sha256.New, []byte("rq-wh-secret"))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	res, err := svc.HandleGitHubWebhook(context.Background(), payload, "del-rq-merged", sig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "processed" || res.TaskID != task.ID {
		t.Errorf("unexpected result %+v", res)
	}
	reloaded, _ := f.taskRepo.GetByID(context.Background(), task.ID)
	if reloaded.Status != models.TaskStatusDone {
		t.Errorf("expected task status %s, got %s", models.TaskStatusDone, reloaded.Status)
	}
	if n := f.credRepo.getByID.Load(); n != 0 {
		t.Errorf("expected zero credential lookups (no PAT decrypt), got %d", n)
	}
	if u, c := sync.updateCalls.Load(), sync.commentCalls.Load(); u != 0 || c != 0 {
		t.Errorf("expected zero GitHub writes, got update=%d comment=%d", u, c)
	}

	// Idempotency is unchanged: a replay is reported as duplicate.
	dup, err := svc.HandleGitHubWebhook(context.Background(), payload, "del-rq-merged", sig)
	if err != nil || dup.Status != "duplicate" {
		t.Errorf("expected duplicate on replay, got %+v err=%v", dup, err)
	}
	// Signature verification is unchanged.
	if _, err := svc.HandleGitHubWebhook(context.Background(), payload, "del-rq-bad", "sha256=00"); !errors.Is(err, service.ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
}

// Item D: cancel still stops the task (DB + queue) but makes no OpenCode call.
func TestTaskService_CancelTask_NoOpenCodeInterrupt(t *testing.T) {
	f := newQuarantineFixture(t)
	task := f.createReviewTaskWithPR(t)
	spy := &spyOpenCodeClient{}
	queueClient := &mockQueueClient{}
	svc := service.NewTaskService(f.taskRepo, repository.NewAttemptRepository(f.db), queueClient, nil,
		service.WithTaskSessionDependencies(spy, repository.NewProjectRepository(f.db), repository.NewTeamRepository(f.db)),
	)
	ctx := context.Background()

	if err := svc.CancelTask(ctx, task.ID, f.viewer.ID); !errors.Is(err, common.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for viewer, got %v", err)
	}
	if err := svc.CancelTask(ctx, task.ID, f.owner.ID); err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}
	reloaded, _ := f.taskRepo.GetByID(ctx, task.ID)
	if reloaded.Status != models.TaskStatusCancelled {
		t.Errorf("expected task status %s, got %s", models.TaskStatusCancelled, reloaded.Status)
	}
	if queueClient.cancelledID != *task.AsynqTaskID {
		t.Errorf("expected queue cancel of %s, got %q", *task.AsynqTaskID, queueClient.cancelledID)
	}
	if n := spy.calls.Load(); n != 0 {
		t.Errorf("expected zero OpenCode client calls, got %d", n)
	}
}

// Item E: session inspection refuses after authz with no OpenCode call.
func TestTaskService_GetTaskSession_QuarantineHasNoSideEffects(t *testing.T) {
	f := newQuarantineFixture(t)
	task := f.createReviewTaskWithPR(t)
	spy := &spyOpenCodeClient{}
	projectRepo := repository.NewProjectRepository(f.db)
	teamRepo := repository.NewTeamRepository(f.db)
	svc := service.NewTaskService(f.taskRepo, repository.NewAttemptRepository(f.db), &mockQueueClient{}, nil,
		service.WithTaskSessionDependencies(spy, projectRepo, teamRepo))
	ctx := context.Background()

	for _, userID := range []string{f.viewer.ID, f.owner.ID} {
		got, err := svc.GetTaskSession(ctx, task.ID, userID)
		assertQuarantineErr(t, "userID="+userID, err)
		if got != nil {
			t.Errorf("userID=%q: expected nil session, got %+v", userID, got)
		}
	}
	if _, err := svc.GetTaskSession(ctx, task.ID, f.outsider.ID); !errors.Is(err, common.ErrForbidden) {
		t.Errorf("expected ErrForbidden for outsider, got %v", err)
	}
	if n := spy.calls.Load(); n != 0 {
		t.Errorf("expected zero OpenCode client calls, got %d", n)
	}

	// Without an OpenCode client the refusal is the same quarantine error, not ErrServiceUnavailable.
	noClient := service.NewTaskService(f.taskRepo, repository.NewAttemptRepository(f.db), &mockQueueClient{}, nil,
		service.WithTaskSessionDependencies(nil, projectRepo, teamRepo))
	_, err := noClient.GetTaskSession(ctx, task.ID, f.viewer.ID)
	assertQuarantineErr(t, "nil client", err)
}
