package service_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
)

const hardeningCipherKey = "01234567890123456789012345678901"

// hardeningFixture wires a real webhook service over an in-memory sqlite DB.
type hardeningFixture struct {
	ctx       context.Context
	cipher    *crypto.SecretCipher
	integRepo repository.IntegrationRepository
	credRepo  repository.CredentialRepository
	taskRepo  repository.TaskRepository
	svc       service.WebhookService
}

func newHardeningFixture(t *testing.T) *hardeningFixture {
	t.Helper()
	db := setupTestDB(t)
	cipher, err := crypto.NewSecretCipher([]byte(hardeningCipherKey))
	if err != nil {
		t.Fatalf("failed to init cipher: %v", err)
	}
	f := &hardeningFixture{
		ctx:       context.Background(),
		cipher:    cipher,
		integRepo: repository.NewIntegrationRepository(db),
		credRepo:  repository.NewCredentialRepository(db),
		taskRepo:  repository.NewTaskRepository(db),
	}
	f.svc = service.NewWebhookService(f.integRepo, f.credRepo, f.taskRepo,
		integrations.NewMemoryIdempotencyStore(), cipher, nil)
	return f
}

func (f *hardeningFixture) addCredential(t *testing.T, id, plain string) {
	t.Helper()
	enc, err := f.cipher.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	if err := f.credRepo.Create(f.ctx, &models.Credential{
		ID:            id,
		TeamID:        "team-1",
		Provider:      models.ProviderGitHub,
		Label:         "PAT",
		EncryptedData: enc,
	}); err != nil {
		t.Fatalf("create credential: %v", err)
	}
}

func (f *hardeningFixture) addIntegration(t *testing.T, integ models.ProjectIntegration) {
	t.Helper()
	// The enabled column has a DB default of 1, so a zero-value Enabled=false is omitted on insert.
	// Persist disabled integrations through an explicit update instead.
	disabled := !integ.Enabled
	integ.Enabled = true
	if err := f.integRepo.Create(f.ctx, &integ); err != nil {
		t.Fatalf("create integration %s: %v", integ.ID, err)
	}
	if disabled {
		integ.Enabled = false
		if err := f.integRepo.Update(f.ctx, &integ); err != nil {
			t.Fatalf("disable integration %s: %v", integ.ID, err)
		}
	}
}

// addGitHub seeds an enabled GitHub integration whose HMAC secret is stored in config.
func (f *hardeningFixture) addGitHub(t *testing.T, projectID, integID, repo, secret string) {
	t.Helper()
	f.addIntegration(t, models.ProjectIntegration{
		ID:           integID,
		ProjectID:    projectID,
		CredentialID: "cred-" + integID,
		Provider:     models.IntegrationGitHub,
		ExternalID:   repo,
		Enabled:      true,
		Config:       models.JSONMap{"webhook_secret": secret},
	})
}

func (f *hardeningFixture) addTask(t *testing.T, task models.Task) {
	t.Helper()
	if err := f.taskRepo.Create(f.ctx, &task); err != nil {
		t.Fatalf("create task %s: %v", task.ID, err)
	}
}

func (f *hardeningFixture) mustTask(t *testing.T, id string) *models.Task {
	t.Helper()
	task, err := f.taskRepo.GetByID(f.ctx, id)
	if err != nil || task == nil {
		t.Fatalf("reload task %s: task=%v err=%v", id, task, err)
	}
	return task
}

func (f *hardeningFixture) countTasks(t *testing.T, projectID string) int {
	t.Helper()
	tasks, err := f.taskRepo.List(f.ctx, projectID)
	if err != nil {
		t.Fatalf("list tasks for %s: %v", projectID, err)
	}
	return len(tasks)
}

// assertTaskUntouched fails if the seeded task's observable fields changed.
func (f *hardeningFixture) assertTaskUntouched(t *testing.T, before models.Task) {
	t.Helper()
	after := f.mustTask(t, before.ID)
	if after.Status != before.Status || after.Title != before.Title || after.Prompt != before.Prompt {
		t.Errorf("task %s mutated: status %q->%q title %q->%q prompt %q->%q",
			before.ID, before.Status, after.Status, before.Title, after.Title, before.Prompt, after.Prompt)
	}
}

func hardeningStr(s string) *string { return &s }

func hardeningGitHubSig(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func hardeningIssuePayload(repo string, number int, title string) []byte {
	return []byte(fmt.Sprintf(
		`{"action":"opened","repository":{"full_name":%q},"issue":{"number":%d,"title":%q,"body":"details","html_url":"https://github.com/%s/issues/%d"}}`,
		repo, number, title, repo, number))
}

func TestHandleGitHubWebhook_Hardening_ValidSignatureAccepted(t *testing.T) {
	f := newHardeningFixture(t)
	const secret = "valid-secret"
	const repo = "owner/accept-repo"
	f.addGitHub(t, "proj-accept", "integ-accept", repo, secret)

	payload := hardeningIssuePayload(repo, 11, "Accepted issue")
	res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-accept-1", hardeningGitHubSig(secret, payload))
	if err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if res.Status != "processed" || res.DeliveryID != "del-accept-1" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.TaskID == "" {
		t.Fatal("expected a task to be created for a valid opened issue")
	}
	task := f.mustTask(t, res.TaskID)
	if task.ProjectID != "proj-accept" || task.Title != "Accepted issue" {
		t.Errorf("unexpected task: project=%q title=%q", task.ProjectID, task.Title)
	}
}

func TestHandleGitHubWebhook_Hardening_SignatureGateRejectsWithoutSideEffects(t *testing.T) {
	const secret = "gate-secret"
	const repo = "owner/gate-repo"
	payload := hardeningIssuePayload(repo, 7, "Attacker title")
	tampered := hardeningIssuePayload(repo, 7, "Tampered title")
	validSig := hardeningGitHubSig(secret, payload)

	tests := []struct {
		name      string
		signature string
		body      []byte
	}{
		{name: "missing signature header", signature: "", body: payload},
		{name: "signature from wrong secret", signature: hardeningGitHubSig("other-secret", payload), body: payload},
		{name: "signature computed over tampered body", signature: hardeningGitHubSig(secret, tampered), body: payload},
		{name: "signature without sha256 prefix", signature: strings.TrimPrefix(validSig, "sha256="), body: payload},
		{name: "signature with wrong algorithm prefix", signature: "sha1=" + strings.TrimPrefix(validSig, "sha256="), body: payload},
		{name: "non-hex signature", signature: "sha256=zznothex", body: payload},
		{name: "truncated signature", signature: validSig[:len(validSig)-4], body: payload},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.addGitHub(t, "proj-gate", "integ-gate", repo, secret)
			f.addTask(t, models.Task{
				ID:         "task-gate-orig",
				ProjectID:  "proj-gate",
				ExternalID: hardeningStr("7"),
				Title:      "Original title",
				Prompt:     "original prompt",
				Status:     models.TaskStatusReview,
			})
			before := *f.mustTask(t, "task-gate-orig")

			res, err := f.svc.HandleGitHubWebhook(f.ctx, tc.body, "del-gate-1", tc.signature)
			if !errors.Is(err, service.ErrInvalidSignature) {
				t.Fatalf("expected ErrInvalidSignature, got res=%+v err=%v", res, err)
			}
			if res != nil {
				t.Errorf("expected nil result on rejection, got %+v", res)
			}
			if n := f.countTasks(t, "proj-gate"); n != 1 {
				t.Errorf("expected only the seeded task, found %d tasks", n)
			}
			f.assertTaskUntouched(t, before)
		})
	}
}

func TestHandleGitHubWebhook_Hardening_RejectedDeliveryIDIsNotConsumed(t *testing.T) {
	f := newHardeningFixture(t)
	const secret = "consume-secret"
	const repo = "owner/consume-repo"
	f.addGitHub(t, "proj-consume", "integ-consume", repo, secret)

	payload := hardeningIssuePayload(repo, 21, "Retry after bad sig")
	if _, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-consume-1", "sha256=deadbeef"); !errors.Is(err, service.ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature for bad signature, got %v", err)
	}

	// The rejected attempt must not have recorded the delivery ID as seen.
	res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-consume-1", hardeningGitHubSig(secret, payload))
	if err != nil {
		t.Fatalf("valid retry with same delivery ID failed: %v", err)
	}
	if res.Status != "processed" || res.TaskID == "" {
		t.Errorf("expected processed with a task after valid retry, got %+v", res)
	}
}

func TestHandleGitHubWebhook_Hardening_SecretResolution(t *testing.T) {
	const repo = "owner/secret-repo"
	const cfgSecret = "cfg-secret"
	const patSecret = "pat-secret"

	tests := []struct {
		name    string
		config  models.JSONMap
		signKey string
		wantErr error
	}{
		{name: "config secret accepts its own signature", config: models.JSONMap{"webhook_secret": cfgSecret}, signKey: cfgSecret},
		{name: "config secret overrides PAT fallback", config: models.JSONMap{"webhook_secret": cfgSecret}, signKey: patSecret, wantErr: service.ErrInvalidSignature},
		{name: "empty config secret falls back to PAT", config: models.JSONMap{"webhook_secret": ""}, signKey: patSecret},
		{name: "non-string config secret falls back to PAT", config: models.JSONMap{"webhook_secret": 42}, signKey: patSecret},
		{name: "no config falls back to PAT", config: nil, signKey: patSecret},
		{name: "no config rejects signature from unrelated key", config: nil, signKey: cfgSecret, wantErr: service.ErrInvalidSignature},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.addCredential(t, "cred-secret", patSecret)
			f.addIntegration(t, models.ProjectIntegration{
				ID:           "integ-secret",
				ProjectID:    "proj-secret",
				CredentialID: "cred-secret",
				Provider:     models.IntegrationGitHub,
				ExternalID:   repo,
				Enabled:      true,
				Config:       tc.config,
			})

			payload := hardeningIssuePayload(repo, 31, "Secret resolution")
			res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-secret-1", hardeningGitHubSig(tc.signKey, payload))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || res != nil {
					t.Fatalf("expected %v with nil result, got res=%+v err=%v", tc.wantErr, res, err)
				}
				if n := f.countTasks(t, "proj-secret"); n != 0 {
					t.Errorf("rejected signature created %d tasks", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected acceptance, got %v", err)
			}
			if res.Status != "processed" {
				t.Errorf("expected processed, got %+v", res)
			}
		})
	}
}

func TestHandleGitHubWebhook_Hardening_NoSecretSourceFailsClosed(t *testing.T) {
	db := setupTestDB(t)
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	ctx := context.Background()

	if err := integRepo.Create(ctx, &models.ProjectIntegration{
		ID: "integ-nocipher", ProjectID: "proj-nocipher", CredentialID: "cred-none",
		Provider: models.IntegrationGitHub, ExternalID: "owner/nocipher", Enabled: true,
	}); err != nil {
		t.Fatalf("create integration: %v", err)
	}
	// Nil cipher and no config secret: the service must refuse rather than accept unverified input.
	svc := service.NewWebhookService(integRepo, credRepo, taskRepo, integrations.NewMemoryIdempotencyStore(), nil, nil)

	payload := hardeningIssuePayload("owner/nocipher", 1, "No cipher")
	res, err := svc.HandleGitHubWebhook(ctx, payload, "del-nocipher-1", hardeningGitHubSig("anything", payload))
	if err == nil || res != nil {
		t.Fatalf("expected failure without any secret source, got res=%+v err=%v", res, err)
	}
	if !strings.Contains(err.Error(), "cipher not configured") {
		t.Errorf("expected fail-closed cipher error, got %v", err)
	}
	if tasks, _ := taskRepo.List(ctx, "proj-nocipher"); len(tasks) != 0 {
		t.Errorf("expected no tasks, found %d", len(tasks))
	}
}

func TestHandleGitHubWebhook_Hardening_MissingDeliveryIDRejected(t *testing.T) {
	f := newHardeningFixture(t)
	const secret = "delivery-secret"
	const repo = "owner/delivery-repo"
	f.addGitHub(t, "proj-delivery", "integ-delivery", repo, secret)

	payload := hardeningIssuePayload(repo, 5, "No delivery id")
	res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "", hardeningGitHubSig(secret, payload))
	if !errors.Is(err, service.ErrMissingDeliveryID) || res != nil {
		t.Fatalf("expected ErrMissingDeliveryID with nil result, got res=%+v err=%v", res, err)
	}
	if n := f.countTasks(t, "proj-delivery"); n != 0 {
		t.Errorf("missing delivery ID created %d tasks", n)
	}
}

func TestHandleGitHubWebhook_Hardening_MalformedPayloadRejectedWithoutPanic(t *testing.T) {
	const secret = "malformed-secret"
	const repo = "owner/malformed-repo"

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "truncated json", body: `{"action":"opened"`},
		{name: "json array instead of object", body: `[1,2,3]`},
		{name: "wrong type for issue", body: `{"repository":{"full_name":"owner/malformed-repo"},"issue":"not-an-object"}`},
		{name: "missing repository full_name", body: `{"action":"opened","issue":{"number":1,"title":"x"}}`},
		{name: "null document", body: `null`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.addGitHub(t, "proj-malformed", "integ-malformed", repo, secret)

			payload := []byte(tc.body)
			res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-malformed-1", hardeningGitHubSig(secret, payload))
			if !errors.Is(err, service.ErrInvalidWebhookPayload) {
				t.Fatalf("expected ErrInvalidWebhookPayload, got res=%+v err=%v", res, err)
			}
			if res != nil {
				t.Errorf("expected nil result, got %+v", res)
			}
			if n := f.countTasks(t, "proj-malformed"); n != 0 {
				t.Errorf("malformed payload created %d tasks", n)
			}
		})
	}
}

func TestHandleGitHubWebhook_Hardening_MalformedPayloadDoesNotConsumeDelivery(t *testing.T) {
	f := newHardeningFixture(t)
	const secret = "consume-malformed"
	const repo = "owner/consume-malformed"
	f.addGitHub(t, "proj-consume-m", "integ-consume-m", repo, secret)

	bad := []byte(`{"repository":`)
	if _, err := f.svc.HandleGitHubWebhook(f.ctx, bad, "del-cm-1", hardeningGitHubSig(secret, bad)); !errors.Is(err, service.ErrInvalidWebhookPayload) {
		t.Fatalf("expected ErrInvalidWebhookPayload, got %v", err)
	}

	good := hardeningIssuePayload(repo, 3, "Good after bad")
	res, err := f.svc.HandleGitHubWebhook(f.ctx, good, "del-cm-1", hardeningGitHubSig(secret, good))
	if err != nil {
		t.Fatalf("valid payload with same delivery ID failed: %v", err)
	}
	if res.Status != "processed" {
		t.Errorf("expected processed (not duplicate), got %+v", res)
	}
}

func TestHandleGitHubWebhook_Hardening_UnknownOrDisabledRepoRejected(t *testing.T) {
	const secret = "unknown-secret"
	f := newHardeningFixture(t)
	f.addGitHub(t, "proj-known", "integ-known", "owner/known", secret)
	f.addIntegration(t, models.ProjectIntegration{
		ID: "integ-disabled", ProjectID: "proj-disabled", CredentialID: "cred-x",
		Provider: models.IntegrationGitHub, ExternalID: "owner/disabled", Enabled: false,
		Config: models.JSONMap{"webhook_secret": secret},
	})

	for _, repo := range []string{"owner/unknown", "owner/disabled", "OWNER/KNOWN"} {
		t.Run(repo, func(t *testing.T) {
			payload := hardeningIssuePayload(repo, 2, "Unknown repo")
			res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-unknown-"+repo, hardeningGitHubSig(secret, payload))
			if !errors.Is(err, service.ErrIntegrationNotFound) || res != nil {
				t.Fatalf("expected ErrIntegrationNotFound with nil result, got res=%+v err=%v", res, err)
			}
		})
	}
	if n := f.countTasks(t, "proj-known") + f.countTasks(t, "proj-disabled"); n != 0 {
		t.Errorf("unknown repo created %d tasks", n)
	}
}

func TestHandleGitHubWebhook_Hardening_UnhandledEventsDoNotMutate(t *testing.T) {
	const secret = "unhandled-secret"
	const repo = "owner/unhandled"

	tests := []struct {
		name       string
		body       string
		wantStatus string
	}{
		{
			name:       "issue edited is not a creation trigger",
			body:       `{"action":"edited","repository":{"full_name":"owner/unhandled"},"issue":{"number":50,"title":"Edited title","body":"b"}}`,
			wantStatus: "processed",
		},
		{
			name:       "unknown action without any known section",
			body:       `{"action":"synchronize","repository":{"full_name":"owner/unhandled"}}`,
			wantStatus: "processed",
		},
		{
			name:       "check_run cancelled is ignored",
			body:       `{"action":"completed","repository":{"full_name":"owner/unhandled"},"check_run":{"name":"ci","conclusion":"cancelled","check_suite":{"head_branch":"task/task-unhandled"}}}`,
			wantStatus: "ignored",
		},
		{
			name:       "workflow_run success is ignored",
			body:       `{"action":"completed","repository":{"full_name":"owner/unhandled"},"workflow_run":{"name":"ci","conclusion":"success","head_branch":"task/task-unhandled"}}`,
			wantStatus: "ignored",
		},
		{
			name:       "merged flag on non-closing action is ignored",
			body:       `{"action":"opened","repository":{"full_name":"owner/unhandled"},"pull_request":{"number":9,"merged":true,"html_url":"https://github.com/owner/unhandled/pull/9","head":{"ref":"task/task-unhandled"}}}`,
			wantStatus: "processed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.addGitHub(t, "proj-unhandled", "integ-unhandled", repo, secret)
			f.addTask(t, models.Task{
				ID:         "task-unhandled",
				ProjectID:  "proj-unhandled",
				ExternalID: hardeningStr("50"),
				BranchName: hardeningStr("task/task-unhandled"),
				PRURL:      hardeningStr("https://github.com/owner/unhandled/pull/9"),
				Title:      "Stable title",
				Prompt:     "stable prompt",
				Status:     models.TaskStatusReview,
			})
			before := *f.mustTask(t, "task-unhandled")

			payload := []byte(tc.body)
			res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-unhandled-1", hardeningGitHubSig(secret, payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Status != tc.wantStatus {
				t.Errorf("expected status %q, got %q", tc.wantStatus, res.Status)
			}
			if res.TaskID != "" {
				t.Errorf("expected no task touched, got %q", res.TaskID)
			}
			if n := f.countTasks(t, "proj-unhandled"); n != 1 {
				t.Errorf("expected 1 task, found %d", n)
			}
			f.assertTaskUntouched(t, before)
		})
	}
}

func TestHandleGitHubWebhook_Hardening_DuplicateDeliveryIsNoOp(t *testing.T) {
	f := newHardeningFixture(t)
	const secret = "dup-secret"
	f.addGitHub(t, "proj-dup", "integ-dup", "owner/dup", secret)
	f.addGitHub(t, "proj-dup-b", "integ-dup-b", "owner/dup-b", secret)

	first := hardeningIssuePayload("owner/dup", 60, "First delivery")
	res1, err := f.svc.HandleGitHubWebhook(f.ctx, first, "del-dup-1", hardeningGitHubSig(secret, first))
	if err != nil || res1.Status != "processed" || res1.TaskID == "" {
		t.Fatalf("first delivery: res=%+v err=%v", res1, err)
	}

	// Replay the same delivery ID with a different (validly signed) body.
	replay := hardeningIssuePayload("owner/dup", 61, "Replay with new body")
	res2, err := f.svc.HandleGitHubWebhook(f.ctx, replay, "del-dup-1", hardeningGitHubSig(secret, replay))
	if err != nil {
		t.Fatalf("replay returned error: %v", err)
	}
	if res2.Status != "duplicate" || res2.TaskID != "" {
		t.Errorf("expected duplicate with no task, got %+v", res2)
	}
	if n := f.countTasks(t, "proj-dup"); n != 1 {
		t.Errorf("replay created tasks: count=%d", n)
	}

	// Idempotency is keyed per project: the same delivery ID for another project is independent.
	otherPayload := hardeningIssuePayload("owner/dup-b", 60, "Other project delivery")
	res3, err := f.svc.HandleGitHubWebhook(f.ctx, otherPayload, "del-dup-1", hardeningGitHubSig(secret, otherPayload))
	if err != nil || res3.Status != "processed" || res3.TaskID == "" {
		t.Errorf("same delivery ID for other project should be processed, got res=%+v err=%v", res3, err)
	}
}

func TestHandleGitHubWebhook_Hardening_FailedCreateIsNotRetried(t *testing.T) {
	// Pins current behaviour: a delivery whose task creation failed is recorded as failed,
	// and any redelivery with the same ID is answered as "duplicate" rather than retried.
	db := setupTestDB(t)
	cipher, _ := crypto.NewSecretCipher([]byte(hardeningCipherKey))
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	ctx := context.Background()
	const secret = "failcreate-secret"
	const repo = "owner/failcreate"
	if err := integRepo.Create(ctx, &models.ProjectIntegration{
		ID: "integ-failcreate", ProjectID: "proj-failcreate", CredentialID: "cred-failcreate",
		Provider: models.IntegrationGitHub, ExternalID: repo, Enabled: true,
		Config: models.JSONMap{"webhook_secret": secret},
	}); err != nil {
		t.Fatalf("create integration: %v", err)
	}

	svc := service.NewWebhookService(integRepo, credRepo, failingCreateTaskRepo{repository.NewTaskRepository(db)},
		integrations.NewMemoryIdempotencyStore(), cipher, nil)

	payload := hardeningIssuePayload(repo, 70, "Create fails")
	sig := hardeningGitHubSig(secret, payload)
	if _, err := svc.HandleGitHubWebhook(ctx, payload, "del-failcreate-1", sig); err == nil || !strings.Contains(err.Error(), "failed to create task") {
		t.Fatalf("expected task creation failure, got %v", err)
	}

	res, err := svc.HandleGitHubWebhook(ctx, payload, "del-failcreate-1", sig)
	if err != nil {
		t.Fatalf("redelivery returned error: %v", err)
	}
	if res.Status != "duplicate" {
		t.Errorf("expected redelivery of failed delivery to be reported as duplicate, got %+v", res)
	}
}

// failingCreateTaskRepo fails every Create while delegating reads to the real repository.
type failingCreateTaskRepo struct {
	repository.TaskRepository
}

func (failingCreateTaskRepo) Create(context.Context, *models.Task) error {
	return errors.New("simulated insert failure")
}

func TestHandleGitHubWebhook_Hardening_CrossProjectIsolation(t *testing.T) {
	const secret = "iso-secret"
	const repoA = "owner/iso-a"

	tests := []struct {
		name       string
		seedB      models.Task
		payload    string
		wantTaskID string // "" means no task may be touched
	}{
		{
			name:    "check_run branch convention points at project B task",
			seedB:   models.Task{ID: "iso-b-1", Title: "B1", Prompt: "b prompt", BranchName: hardeningStr("task/iso-b-1"), Status: models.TaskStatusReview},
			payload: `{"action":"completed","repository":{"full_name":"owner/iso-a"},"check_run":{"name":"ci","conclusion":"failure","head_sha":"x","check_suite":{"head_branch":"task/iso-b-1"},"output":{"text":"boom"}}}`,
		},
		{
			name:    "check_run branch column matches project B task",
			seedB:   models.Task{ID: "task-iso-b-2", Title: "B2", Prompt: "b prompt", BranchName: hardeningStr("feature/shared"), Status: models.TaskStatusReview},
			payload: `{"action":"completed","repository":{"full_name":"owner/iso-a"},"check_run":{"name":"ci","conclusion":"failure","check_suite":{"head_branch":"feature/shared"}}}`,
		},
		{
			name:    "workflow_run branch column matches project B task",
			seedB:   models.Task{ID: "task-iso-b-3", Title: "B3", Prompt: "b prompt", BranchName: hardeningStr("feature/wf-shared"), Status: models.TaskStatusReview},
			payload: `{"action":"completed","repository":{"full_name":"owner/iso-a"},"workflow_run":{"name":"wf","conclusion":"failure","head_branch":"feature/wf-shared"}}`,
		},
		{
			name:    "merged PR head ref matches project B task",
			seedB:   models.Task{ID: "task-iso-b-4", Title: "B4", Prompt: "b prompt", BranchName: hardeningStr("feature/pr-shared"), Status: models.TaskStatusReview},
			payload: `{"action":"closed","repository":{"full_name":"owner/iso-a"},"pull_request":{"number":4,"merged":true,"html_url":"https://github.com/owner/iso-a/pull/4","head":{"ref":"feature/pr-shared"}}}`,
		},
		{
			name:    "merged PR html_url matches project B task",
			seedB:   models.Task{ID: "task-iso-b-5", Title: "B5", Prompt: "b prompt", PRURL: hardeningStr("https://github.com/owner/shared/pull/5"), Status: models.TaskStatusReview},
			payload: `{"action":"closed","repository":{"full_name":"owner/iso-a"},"pull_request":{"number":5,"merged":true,"html_url":"https://github.com/owner/shared/pull/5","head":{"ref":"feature/none"}}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.addGitHub(t, "proj-iso-a", "integ-iso-a", repoA, secret)
			seedB := tc.seedB
			seedB.ProjectID = "proj-iso-b"
			f.addTask(t, seedB)
			before := *f.mustTask(t, seedB.ID)

			payload := []byte(tc.payload)
			res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-iso-"+seedB.ID, hardeningGitHubSig(secret, payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.TaskID != tc.wantTaskID {
				t.Errorf("expected TaskID %q, got %q", tc.wantTaskID, res.TaskID)
			}
			f.assertTaskUntouched(t, before)
			if after := f.mustTask(t, seedB.ID); after.ProjectID != "proj-iso-b" {
				t.Errorf("task moved to project %q", after.ProjectID)
			}
		})
	}
}

func TestHandleGitHubWebhook_Hardening_IssueExternalIDIsScopedPerProject(t *testing.T) {
	f := newHardeningFixture(t)
	const secret = "scope-secret"
	f.addGitHub(t, "proj-scope-a", "integ-scope-a", "owner/scope", secret)
	f.addTask(t, models.Task{
		ID: "task-scope-b", ProjectID: "proj-scope-b", ExternalID: hardeningStr("42"),
		Title: "Project B issue 42", Prompt: "b", Status: models.TaskStatusReview,
	})
	before := *f.mustTask(t, "task-scope-b")

	payload := hardeningIssuePayload("owner/scope", 42, "Project A issue 42")
	res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-scope-1", hardeningGitHubSig(secret, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TaskID == "" || res.TaskID == "task-scope-b" {
		t.Fatalf("expected a new task for project A, got TaskID %q", res.TaskID)
	}
	if created := f.mustTask(t, res.TaskID); created.ProjectID != "proj-scope-a" {
		t.Errorf("new task landed in project %q", created.ProjectID)
	}
	f.assertTaskUntouched(t, before)
}

func TestHandleGitHubWebhook_Hardening_PullRequestMergeLookup(t *testing.T) {
	const secret = "pr-lookup-secret"
	const repo = "owner/pr-lookup"
	const prURL = "https://github.com/owner/pr-lookup/pull/3"

	tests := []struct {
		name       string
		body       string
		wantTaskID bool // true: the seeded task in this project must be marked done
	}{
		{
			name:       "matched by PR URL when branch differs",
			body:       `{"action":"closed","repository":{"full_name":"owner/pr-lookup"},"pull_request":{"number":3,"merged":true,"html_url":"https://github.com/owner/pr-lookup/pull/3","head":{"ref":"feature/unrelated"}}}`,
			wantTaskID: true,
		},
		{
			name:       "action merged is accepted as a closing action",
			body:       `{"action":"merged","repository":{"full_name":"owner/pr-lookup"},"pull_request":{"number":3,"merged":true,"html_url":"https://github.com/owner/pr-lookup/pull/3","head":{"ref":"feature/unrelated"}}}`,
			wantTaskID: true,
		},
		{
			name:       "empty html_url skips URL lookup",
			body:       `{"action":"closed","repository":{"full_name":"owner/pr-lookup"},"pull_request":{"number":3,"merged":true,"html_url":"","head":{"ref":"feature/unrelated"}}}`,
			wantTaskID: false,
		},
		{
			name:       "different PR URL does not match",
			body:       `{"action":"closed","repository":{"full_name":"owner/pr-lookup"},"pull_request":{"number":8,"merged":true,"html_url":"https://github.com/owner/pr-lookup/pull/8","head":{"ref":"feature/unrelated"}}}`,
			wantTaskID: false,
		},
		{
			name:       "closed without merge does not complete task",
			body:       `{"action":"closed","repository":{"full_name":"owner/pr-lookup"},"pull_request":{"number":3,"merged":false,"html_url":"https://github.com/owner/pr-lookup/pull/3","head":{"ref":"feature/unrelated"}}}`,
			wantTaskID: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.addGitHub(t, "proj-pr-lookup", "integ-pr-lookup", repo, secret)
			f.addTask(t, models.Task{
				ID: "task-pr-lookup", ProjectID: "proj-pr-lookup", PRURL: hardeningStr(prURL),
				BranchName: hardeningStr("feature/other"), Title: "PR task", Prompt: "p",
				Status: models.TaskStatusReview,
			})

			payload := []byte(tc.body)
			res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-pr-"+tc.name, hardeningGitHubSig(secret, payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			status := f.mustTask(t, "task-pr-lookup").Status
			if tc.wantTaskID {
				if res.TaskID != "task-pr-lookup" || status != models.TaskStatusDone {
					t.Errorf("expected task done, got TaskID=%q status=%q", res.TaskID, status)
				}
				return
			}
			if res.TaskID != "" || status != models.TaskStatusReview {
				t.Errorf("expected no change, got TaskID=%q status=%q", res.TaskID, status)
			}
		})
	}
}

func TestHandleGitHubWebhook_Hardening_BranchLookupEdges(t *testing.T) {
	const secret = "branch-edge-secret"
	const repo = "owner/branch-edge"

	tests := []struct {
		name       string
		task       models.Task
		headBranch string
		wantTaskID string
	}{
		{
			name:       "empty head branch never matches a task with empty branch name",
			task:       models.Task{ID: "task-empty-branch", BranchName: hardeningStr(""), Status: models.TaskStatusReview},
			headBranch: "",
			wantTaskID: "",
		},
		{
			name:       "task/ prefix resolves by ID within project",
			task:       models.Task{ID: "task-prefix-ok", Status: models.TaskStatusReview},
			headBranch: "task/task-prefix-ok",
			wantTaskID: "task-prefix-ok",
		},
		{
			name:       "task/ prefix with unknown ID falls back to branch column scan",
			task:       models.Task{ID: "task-scan-fallback", BranchName: hardeningStr("task/unknown-id"), Status: models.TaskStatusReview},
			headBranch: "task/unknown-id",
			wantTaskID: "task-scan-fallback",
		},
		{
			name:       "branch with no matching task",
			task:       models.Task{ID: "task-no-match", BranchName: hardeningStr("feature/x"), Status: models.TaskStatusReview},
			headBranch: "feature/y",
			wantTaskID: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.addGitHub(t, "proj-branch-edge", "integ-branch-edge", repo, secret)
			task := tc.task
			task.ProjectID = "proj-branch-edge"
			task.Title = "Branch edge"
			task.Prompt = "original"
			f.addTask(t, task)

			body := fmt.Sprintf(`{"action":"completed","repository":{"full_name":%q},"check_run":{"name":"ci","conclusion":"failure","check_suite":{"head_branch":%q},"output":{"text":"x"}}}`, repo, tc.headBranch)
			payload := []byte(body)
			res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-branch-"+tc.name, hardeningGitHubSig(secret, payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Status != "processed" {
				t.Errorf("expected processed, got %+v", res)
			}
			if res.TaskID != tc.wantTaskID {
				t.Errorf("expected TaskID %q, got %q", tc.wantTaskID, res.TaskID)
			}
		})
	}
}

func TestHandleJiraWebhook_Hardening(t *testing.T) {
	f := newHardeningFixture(t)
	f.addIntegration(t, models.ProjectIntegration{
		ID: "integ-jira", ProjectID: "proj-jira", CredentialID: "cred-jira",
		Provider: models.IntegrationJira, ExternalID: "PROJ", Enabled: true,
	})
	f.addIntegration(t, models.ProjectIntegration{
		ID: "integ-jira-disabled", ProjectID: "proj-jira-off", CredentialID: "cred-jira",
		Provider: models.IntegrationJira, ExternalID: "OFF", Enabled: false,
	})
	f.addIntegration(t, models.ProjectIntegration{
		ID: "integ-jira-2", ProjectID: "proj-jira-2", CredentialID: "cred-jira",
		Provider: models.IntegrationJira, ExternalID: "OTHER", Enabled: true,
	})

	jiraBody := func(key string) []byte {
		return []byte(fmt.Sprintf(`{"webhookEvent":"jira:issue_created","issue":{"id":"10001","key":"%s-1","fields":{"summary":"s","description":"d","project":{"key":%q}}}}`, key, key))
	}

	tests := []struct {
		name       string
		eventID    string
		body       []byte
		wantStatus string
		wantReason string
	}{
		{name: "known project processed", eventID: "jira-evt-1", body: jiraBody("PROJ"), wantStatus: "processed"},
		{name: "missing project key ignored", eventID: "jira-evt-2", body: []byte(`{"webhookEvent":"jira:issue_created","issue":{"fields":{}}}`), wantStatus: "ignored", wantReason: "missing project key"},
		{name: "unknown project key ignored", eventID: "jira-evt-3", body: jiraBody("NOPE"), wantStatus: "ignored", wantReason: "no active integration"},
		{name: "disabled integration ignored", eventID: "jira-evt-4", body: jiraBody("OFF"), wantStatus: "ignored", wantReason: "no active integration"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := f.svc.HandleJiraWebhook(f.ctx, tc.body, tc.eventID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Status != tc.wantStatus || res.Reason != tc.wantReason {
				t.Errorf("expected status=%q reason=%q, got %+v", tc.wantStatus, tc.wantReason, res)
			}
			if res.EventID != tc.eventID {
				t.Errorf("expected EventID echo %q, got %q", tc.eventID, res.EventID)
			}
		})
	}

	if n := f.countTasks(t, "proj-jira"); n != 0 {
		t.Errorf("jira webhook must not create tasks in this implementation, found %d", n)
	}
}

func TestHandleJiraWebhook_Hardening_MalformedAndReplay(t *testing.T) {
	f := newHardeningFixture(t)
	f.addIntegration(t, models.ProjectIntegration{
		ID: "integ-jira-r", ProjectID: "proj-jira-r", CredentialID: "cred-jira",
		Provider: models.IntegrationJira, ExternalID: "RPL", Enabled: true,
	})
	valid := []byte(`{"webhookEvent":"jira:issue_updated","issue":{"fields":{"project":{"key":"RPL"}}}}`)

	t.Run("malformed json rejected", func(t *testing.T) {
		res, err := f.svc.HandleJiraWebhook(f.ctx, []byte(`{"issue":`), "jira-mal-1")
		if !errors.Is(err, service.ErrInvalidWebhookPayload) || res != nil {
			t.Fatalf("expected ErrInvalidWebhookPayload with nil result, got res=%+v err=%v", res, err)
		}
	})

	t.Run("malformed attempt does not consume event ID", func(t *testing.T) {
		res, err := f.svc.HandleJiraWebhook(f.ctx, valid, "jira-mal-1")
		if err != nil || res.Status != "processed" {
			t.Fatalf("expected processed after malformed attempt, got res=%+v err=%v", res, err)
		}
	})

	t.Run("replayed event ID is duplicate", func(t *testing.T) {
		res, err := f.svc.HandleJiraWebhook(f.ctx, valid, "jira-mal-1")
		if err != nil || res.Status != "duplicate" {
			t.Fatalf("expected duplicate, got res=%+v err=%v", res, err)
		}
	})

	t.Run("empty event ID generates a fresh ID per call", func(t *testing.T) {
		r1, err1 := f.svc.HandleJiraWebhook(f.ctx, valid, "")
		r2, err2 := f.svc.HandleJiraWebhook(f.ctx, valid, "")
		if err1 != nil || err2 != nil {
			t.Fatalf("unexpected errors: %v, %v", err1, err2)
		}
		if r1.EventID == "" || r2.EventID == "" || r1.EventID == r2.EventID {
			t.Errorf("expected distinct generated event IDs, got %q and %q", r1.EventID, r2.EventID)
		}
		if r1.Status != "processed" || r2.Status != "processed" {
			t.Errorf("expected both processed, got %q and %q", r1.Status, r2.Status)
		}
	})

	t.Run("empty body rejected", func(t *testing.T) {
		if _, err := f.svc.HandleJiraWebhook(f.ctx, nil, "jira-empty"); !errors.Is(err, service.ErrInvalidWebhookPayload) {
			t.Fatalf("expected ErrInvalidWebhookPayload, got %v", err)
		}
	})

	t.Run("same event ID for another project is independent", func(t *testing.T) {
		f.addIntegration(t, models.ProjectIntegration{
			ID: "integ-jira-r2", ProjectID: "proj-jira-r2", CredentialID: "cred-jira",
			Provider: models.IntegrationJira, ExternalID: "RP2", Enabled: true,
		})
		other := []byte(`{"webhookEvent":"jira:issue_updated","issue":{"fields":{"project":{"key":"RP2"}}}}`)
		res, err := f.svc.HandleJiraWebhook(f.ctx, other, "jira-mal-1")
		if err != nil || res.Status != "processed" {
			t.Errorf("expected processed for other project, got res=%+v err=%v", res, err)
		}
	})
}

// TestHandleJiraWebhook_Hardening_NoSignatureVerification documents the missing authentication
// on the Jira inbound path. The service signature has no secret or signature parameter.
func TestHandleJiraWebhook_Hardening_NoSignatureVerification(t *testing.T) {
	t.Skip("BUG: HandleJiraWebhook performs no signature or shared-secret verification; any caller that knows a project key can write idempotency records. Add a verified-secret check before enabling this test.")
}

func TestHandleTrelloWebhook_Hardening(t *testing.T) {
	f := newHardeningFixture(t)
	f.addIntegration(t, models.ProjectIntegration{
		ID: "integ-trello", ProjectID: "proj-trello", CredentialID: "cred-trello",
		Provider: models.IntegrationTrello, ExternalID: "board-A", Enabled: true,
	})
	f.addIntegration(t, models.ProjectIntegration{
		ID: "integ-trello-2", ProjectID: "proj-trello-2", CredentialID: "cred-trello",
		Provider: models.IntegrationTrello, ExternalID: "board-B", Enabled: true,
	})

	t.Run("HEAD probe succeeds without parsing body", func(t *testing.T) {
		res, err := f.svc.HandleTrelloWebhook(f.ctx, []byte("not json"), true)
		if err != nil || res.Status != "processed" || res.EventID != "" {
			t.Fatalf("expected processed probe, got res=%+v err=%v", res, err)
		}
	})

	t.Run("malformed json rejected", func(t *testing.T) {
		res, err := f.svc.HandleTrelloWebhook(f.ctx, []byte(`{"model":`), false)
		if !errors.Is(err, service.ErrInvalidWebhookPayload) || res != nil {
			t.Fatalf("expected ErrInvalidWebhookPayload, got res=%+v err=%v", res, err)
		}
	})

	t.Run("empty body rejected", func(t *testing.T) {
		if _, err := f.svc.HandleTrelloWebhook(f.ctx, nil, false); !errors.Is(err, service.ErrInvalidWebhookPayload) {
			t.Fatalf("expected ErrInvalidWebhookPayload, got %v", err)
		}
	})

	t.Run("missing board id ignored", func(t *testing.T) {
		res, err := f.svc.HandleTrelloWebhook(f.ctx, []byte(`{"model":{},"action":{"id":"act-x"}}`), false)
		if err != nil || res.Status != "ignored" || res.Reason != "missing board id" {
			t.Fatalf("expected ignored missing board id, got res=%+v err=%v", res, err)
		}
	})

	t.Run("unknown board ignored with event ID echoed", func(t *testing.T) {
		res, err := f.svc.HandleTrelloWebhook(f.ctx, []byte(`{"model":{"idBoard":"board-none"},"action":{"id":"act-u"}}`), false)
		if err != nil || res.Status != "ignored" || res.Reason != "no active integration" || res.EventID != "act-u" {
			t.Fatalf("expected ignored no integration, got res=%+v err=%v", res, err)
		}
	})

	t.Run("model id is used when idBoard is absent", func(t *testing.T) {
		res, err := f.svc.HandleTrelloWebhook(f.ctx, []byte(`{"model":{"id":"board-A"},"action":{"id":"act-fb"}}`), false)
		if err != nil || res.Status != "processed" || res.EventID != "act-fb" {
			t.Fatalf("expected processed via model id, got res=%+v err=%v", res, err)
		}
	})

	t.Run("replayed action ID is duplicate", func(t *testing.T) {
		body := []byte(`{"model":{"idBoard":"board-A"},"action":{"id":"act-dup"}}`)
		if res, err := f.svc.HandleTrelloWebhook(f.ctx, body, false); err != nil || res.Status != "processed" {
			t.Fatalf("first delivery: res=%+v err=%v", res, err)
		}
		res, err := f.svc.HandleTrelloWebhook(f.ctx, body, false)
		if err != nil || res.Status != "duplicate" {
			t.Fatalf("expected duplicate, got res=%+v err=%v", res, err)
		}
	})

	t.Run("missing action ID generates fresh event ID", func(t *testing.T) {
		body := []byte(`{"model":{"idBoard":"board-A"},"action":{}}`)
		r1, err1 := f.svc.HandleTrelloWebhook(f.ctx, body, false)
		r2, err2 := f.svc.HandleTrelloWebhook(f.ctx, body, false)
		if err1 != nil || err2 != nil {
			t.Fatalf("unexpected errors: %v, %v", err1, err2)
		}
		if r1.EventID == "" || r1.EventID == r2.EventID {
			t.Errorf("expected distinct generated IDs, got %q and %q", r1.EventID, r2.EventID)
		}
	})

	t.Run("same action ID on another board is independent", func(t *testing.T) {
		body := []byte(`{"model":{"idBoard":"board-B"},"action":{"id":"act-dup"}}`)
		res, err := f.svc.HandleTrelloWebhook(f.ctx, body, false)
		if err != nil || res.Status != "processed" {
			t.Errorf("expected processed for other project, got res=%+v err=%v", res, err)
		}
	})
}

// TestHandleTrelloWebhook_Hardening_NoSignatureVerification documents the missing authentication
// on the Trello inbound path. The service accepts no signature or callback secret.
func TestHandleTrelloWebhook_Hardening_NoSignatureVerification(t *testing.T) {
	t.Skip("BUG: HandleTrelloWebhook performs no callback signature verification (HMAC over body and callback URL); any caller that knows a board ID can write idempotency records. Add verification before enabling this test.")
}
