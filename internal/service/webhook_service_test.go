package service_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
)

func TestHandleGitHubWebhook_CustomSecretAndFilterLabel(t *testing.T) {
	db := setupTestDB(t)

	cipher, _ := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	idempotencyStore := integrations.NewMemoryIdempotencyStore()

	webhookSvc := service.NewWebhookService(integRepo, credRepo, taskRepo, idempotencyStore, cipher, nil)
	ctx := context.Background()

	credEnc, _ := cipher.Encrypt("pat-token")
	_ = credRepo.Create(ctx, &models.Credential{
		ID:            "cred-1",
		TeamID:        "team-1",
		Provider:      models.ProviderGitHub,
		Label:         "PAT",
		EncryptedData: credEnc,
	})

	customSecret := "custom-wh-secret-xyz"
	integ := models.ProjectIntegration{
		ID:           "integ-1",
		ProjectID:    "proj-1",
		CredentialID: "cred-1",
		Provider:     models.IntegrationGitHub,
		ExternalID:   "owner/custom-repo",
		Enabled:      true,
		Config: models.JSONMap{
			"webhook_secret": customSecret,
			"filter_label":   "agent-run",
		},
	}
	_ = integRepo.Create(ctx, &integ)

	// 1. Missing delivery ID -> ErrMissingDeliveryID
	payload1 := []byte(`{"action":"opened","repository":{"full_name":"owner/custom-repo"},"issue":{"number":1,"title":"No label issue"}}`)
	_, err := webhookSvc.HandleGitHubWebhook(ctx, payload1, "", "sig")
	if err == nil || err != service.ErrMissingDeliveryID {
		t.Fatalf("expected ErrMissingDeliveryID, got %v", err)
	}

	// 2. Payload without filter_label -> processed, but task is not created
	mac := hmac.New(sha256.New, []byte(customSecret))
	mac.Write(payload1)
	sig1 := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	res1, err := webhookSvc.HandleGitHubWebhook(ctx, payload1, "del-101", sig1)
	if err != nil {
		t.Fatalf("webhook without label failed: %v", err)
	}
	if res1.TaskID != "" {
		t.Errorf("expected no task created when label missing, got task ID %s", res1.TaskID)
	}

	// 3. Payload with matching filter_label -> task created!
	payload2 := []byte(`{"action":"opened","repository":{"full_name":"owner/custom-repo"},"issue":{"number":2,"title":"Labeled issue","body":"Please fix","labels":[{"name":"agent-run"}]}}`)
	mac2 := hmac.New(sha256.New, []byte(customSecret))
	mac2.Write(payload2)
	sig2 := "sha256=" + hex.EncodeToString(mac2.Sum(nil))

	res2, err := webhookSvc.HandleGitHubWebhook(ctx, payload2, "del-102", sig2)
	if err != nil {
		t.Fatalf("webhook with label failed: %v", err)
	}
	if res2.TaskID == "" {
		t.Errorf("expected task created when label matches, got empty task ID")
	}

	created, err := taskRepo.GetByID(ctx, res2.TaskID)
	if err != nil || created.Title != "Labeled issue" {
		t.Fatalf("expected task 'Labeled issue', got %v", created)
	}
}

func TestHandleGitHubWebhook_CheckRunFailure(t *testing.T) {
	db := setupTestDB(t)
	cipher, _ := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	idempotencyStore := integrations.NewMemoryIdempotencyStore()

	webhookSvc := service.NewWebhookService(integRepo, credRepo, taskRepo, idempotencyStore, cipher, nil)
	ctx := context.Background()

	credEnc, _ := cipher.Encrypt("pat-token")
	_ = credRepo.Create(ctx, &models.Credential{
		ID:            "cred-ci-1",
		TeamID:        "team-1",
		Provider:      models.ProviderGitHub,
		Label:         "PAT",
		EncryptedData: credEnc,
	})

	webhookSecret := "ci-webhook-secret"
	integ := models.ProjectIntegration{
		ID:           "integ-ci-1",
		ProjectID:    "proj-1",
		CredentialID: "cred-ci-1",
		Provider:     models.IntegrationGitHub,
		ExternalID:   "owner/ci-repo",
		Enabled:      true,
		Config: models.JSONMap{
			"webhook_secret": webhookSecret,
		},
	}
	_ = integRepo.Create(ctx, &integ)

	branchName := "task/task-ci-1"
	task := models.Task{
		ID:         "task-ci-1",
		ProjectID:  "proj-1",
		Title:      "Original task",
		Prompt:     "Initial prompt",
		BranchName: &branchName,
		Status:     models.TaskStatusReview,
	}
	_ = taskRepo.Create(ctx, &task)

	// 1. check_run failure payload
	payload := []byte(`{
		"action": "completed",
		"repository": { "full_name": "owner/ci-repo" },
		"check_run": {
			"name": "build-and-test",
			"status": "completed",
			"conclusion": "failure",
			"head_sha": "abc1234",
			"check_suite": {
				"head_branch": "task/task-ci-1"
			},
			"output": {
				"title": "Unit tests failed",
				"summary": "1 test failed in auth_test.go",
				"text": "FAIL: TestTokenExpiry assertion failed at line 42"
			}
		}
	}`)

	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	res, err := webhookSvc.HandleGitHubWebhook(ctx, payload, "del-ci-101", sig)
	if err != nil {
		t.Fatalf("expected check_run failure to be handled cleanly: %v", err)
	}

	if res.Status != "processed" {
		t.Errorf("expected status 'processed', got %q", res.Status)
	}
	if res.TaskID != "task-ci-1" {
		t.Errorf("expected task_id 'task-ci-1', got %q", res.TaskID)
	}

	updated, err := taskRepo.GetByID(ctx, "task-ci-1")
	if err != nil {
		t.Fatalf("failed to reload task: %v", err)
	}

	if updated.Status != models.TaskStatusQueued {
		t.Errorf("expected task to be re-queued, got %s", updated.Status)
	}

	if !strings.Contains(updated.Prompt, "Remote CI check 'build-and-test' failed") {
		t.Errorf("expected prompt to contain failure diagnostics, got: %s", updated.Prompt)
	}
	if !strings.Contains(updated.Prompt, "FAIL: TestTokenExpiry") {
		t.Errorf("expected prompt to contain error logs, got: %s", updated.Prompt)
	}
}

func TestHandleGitHubWebhook_CheckRunSuccessIgnored(t *testing.T) {
	db := setupTestDB(t)
	cipher, _ := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	idempotencyStore := integrations.NewMemoryIdempotencyStore()

	webhookSvc := service.NewWebhookService(integRepo, credRepo, taskRepo, idempotencyStore, cipher, nil)
	ctx := context.Background()

	credEnc, _ := cipher.Encrypt("pat-token")
	_ = credRepo.Create(ctx, &models.Credential{
		ID:            "cred-ci-2",
		TeamID:        "team-1",
		Provider:      models.ProviderGitHub,
		Label:         "PAT",
		EncryptedData: credEnc,
	})

	webhookSecret := "ci-webhook-secret-2"
	integ := models.ProjectIntegration{
		ID:           "integ-ci-2",
		ProjectID:    "proj-1",
		CredentialID: "cred-ci-2",
		Provider:     models.IntegrationGitHub,
		ExternalID:   "owner/ci-repo-2",
		Enabled:      true,
		Config: models.JSONMap{
			"webhook_secret": webhookSecret,
		},
	}
	_ = integRepo.Create(ctx, &integ)

	branchName := "task/task-ci-2"
	task := models.Task{
		ID:         "task-ci-2",
		ProjectID:  "proj-1",
		Title:      "Original task",
		Prompt:     "Initial prompt",
		BranchName: &branchName,
		Status:     models.TaskStatusReview,
	}
	_ = taskRepo.Create(ctx, &task)

	// Successful check_run
	payload := []byte(`{
		"action": "completed",
		"repository": { "full_name": "owner/ci-repo-2" },
		"check_run": {
			"name": "build-and-test",
			"status": "completed",
			"conclusion": "success",
			"head_sha": "def5678",
			"check_suite": {
				"head_branch": "task/task-ci-2"
			}
		}
	}`)

	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	res, err := webhookSvc.HandleGitHubWebhook(ctx, payload, "del-ci-102", sig)
	if err != nil {
		t.Fatalf("expected check_run success to succeed without error: %v", err)
	}

	if res.Status != "ignored" {
		t.Errorf("expected status 'ignored' for successful CI run, got %q", res.Status)
	}

	// Task should not be touched
	unchanged, _ := taskRepo.GetByID(ctx, "task-ci-2")
	if unchanged.Status != models.TaskStatusReview {
		t.Errorf("expected task status to remain review, got %s", unchanged.Status)
	}
}

func TestHandleGitHubWebhook_WorkflowRunFailure(t *testing.T) {
	db := setupTestDB(t)
	cipher, _ := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	idempotencyStore := integrations.NewMemoryIdempotencyStore()

	webhookSvc := service.NewWebhookService(integRepo, credRepo, taskRepo, idempotencyStore, cipher, nil)
	ctx := context.Background()

	credEnc, _ := cipher.Encrypt("pat-token")
	_ = credRepo.Create(ctx, &models.Credential{
		ID:            "cred-ci-3",
		TeamID:        "team-1",
		Provider:      models.ProviderGitHub,
		Label:         "PAT",
		EncryptedData: credEnc,
	})

	webhookSecret := "ci-webhook-secret-3"
	integ := models.ProjectIntegration{
		ID:           "integ-ci-3",
		ProjectID:    "proj-1",
		CredentialID: "cred-ci-3",
		Provider:     models.IntegrationGitHub,
		ExternalID:   "owner/ci-repo-3",
		Enabled:      true,
		Config: models.JSONMap{
			"webhook_secret": webhookSecret,
		},
	}
	_ = integRepo.Create(ctx, &integ)

	branchName := "task/task-wf-1"
	task := models.Task{
		ID:         "task-wf-1",
		ProjectID:  "proj-1",
		Title:      "Original task",
		Prompt:     "Initial prompt",
		BranchName: &branchName,
		Status:     models.TaskStatusReview,
	}
	_ = taskRepo.Create(ctx, &task)

	payload := []byte(`{
		"action": "completed",
		"repository": { "full_name": "owner/ci-repo-3" },
		"workflow_run": {
			"name": "Integration CI",
			"status": "completed",
			"conclusion": "failure",
			"head_branch": "task/task-wf-1",
			"head_sha": "9998887",
			"html_url": "https://github.com/owner/ci-repo-3/actions/runs/12345"
		}
	}`)

	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	res, err := webhookSvc.HandleGitHubWebhook(ctx, payload, "del-ci-103", sig)
	if err != nil {
		t.Fatalf("expected workflow_run failure to be handled cleanly: %v", err)
	}

	if res.Status != "processed" {
		t.Errorf("expected status 'processed', got %q", res.Status)
	}
	if res.TaskID != "task-wf-1" {
		t.Errorf("expected task_id 'task-wf-1', got %q", res.TaskID)
	}

	updated, err := taskRepo.GetByID(ctx, "task-wf-1")
	if err != nil {
		t.Fatalf("failed to reload task: %v", err)
	}

	if updated.Status != models.TaskStatusQueued {
		t.Errorf("expected task to be re-queued, got %s", updated.Status)
	}

	if !strings.Contains(updated.Prompt, "Remote CI workflow 'Integration CI' failed") {
		t.Errorf("expected prompt to contain workflow failure diagnostics, got: %s", updated.Prompt)
	}
}

func TestHandleGitHubWebhook_CheckRun_TenantIsolation(t *testing.T) {
	db := setupTestDB(t)
	cipher, _ := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	idempotencyStore := integrations.NewMemoryIdempotencyStore()

	webhookSvc := service.NewWebhookService(integRepo, credRepo, taskRepo, idempotencyStore, cipher, nil)
	ctx := context.Background()

	credEnc, _ := cipher.Encrypt("pat-token")
	_ = credRepo.Create(ctx, &models.Credential{
		ID:            "cred-iso-1",
		TeamID:        "team-1",
		Provider:      models.ProviderGitHub,
		Label:         "PAT",
		EncryptedData: credEnc,
	})

	webhookSecret := "ci-secret-iso"
	integA := models.ProjectIntegration{
		ID:           "integ-iso-A",
		ProjectID:    "proj-A",
		CredentialID: "cred-iso-1",
		Provider:     models.IntegrationGitHub,
		ExternalID:   "owner/repo-A",
		Enabled:      true,
		Config: models.JSONMap{
			"webhook_secret": webhookSecret,
		},
	}
	_ = integRepo.Create(ctx, &integA)

	// Task belonging to project B
	branchB := "task/task-in-proj-B"
	taskB := models.Task{
		ID:         "task-in-proj-B",
		ProjectID:  "proj-B",
		Title:      "Project B task",
		Prompt:     "Initial prompt B",
		BranchName: &branchB,
		Status:     models.TaskStatusReview,
	}
	_ = taskRepo.Create(ctx, &taskB)

	// Inbound webhook from repo-A claiming branch for Project B task
	payload := []byte(`{
		"action": "completed",
		"repository": { "full_name": "owner/repo-A" },
		"check_run": {
			"name": "malicious-injection",
			"status": "completed",
			"conclusion": "failure",
			"head_sha": "bad123",
			"check_suite": {
				"head_branch": "task/task-in-proj-B"
			},
			"output": {
				"title": "Injected title",
				"summary": "Injected summary",
				"text": "malicious payload"
			}
		}
	}`)

	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	res, err := webhookSvc.HandleGitHubWebhook(ctx, payload, "del-iso-1", sig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.TaskID != "" {
		t.Errorf("expected no task touched across tenant boundary, got task %s", res.TaskID)
	}

	// Task B should NOT have been modified
	taskBCheck, _ := taskRepo.GetByID(ctx, "task-in-proj-B")
	if taskBCheck.Prompt != "Initial prompt B" {
		t.Errorf("tenant isolation violated: prompt was mutated to %s", taskBCheck.Prompt)
	}
	if taskBCheck.Status != models.TaskStatusReview {
		t.Errorf("tenant isolation violated: status was changed to %s", taskBCheck.Status)
	}
}

func TestHandleGitHubWebhook_PullRequestMerged(t *testing.T) {
	db := setupTestDB(t)
	cipher, _ := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	idempotencyStore := integrations.NewMemoryIdempotencyStore()

	webhookSvc := service.NewWebhookService(integRepo, credRepo, taskRepo, idempotencyStore, cipher, nil)
	ctx := context.Background()

	credEnc, _ := cipher.Encrypt("pat-token")
	_ = credRepo.Create(ctx, &models.Credential{
		ID:            "cred-pr-1",
		TeamID:        "team-1",
		Provider:      models.ProviderGitHub,
		Label:         "PAT",
		EncryptedData: credEnc,
	})

	webhookSecret := "pr-wh-secret"
	integ := models.ProjectIntegration{
		ID:           "integ-pr-1",
		ProjectID:    "proj-pr-1",
		CredentialID: "cred-pr-1",
		Provider:     models.IntegrationGitHub,
		ExternalID:   "owner/pr-repo",
		Enabled:      true,
		Config: models.JSONMap{
			"webhook_secret": webhookSecret,
		},
	}
	_ = integRepo.Create(ctx, &integ)

	prURL := "https://github.com/owner/pr-repo/pull/12"
	branch := "task/task-pr-merge-1"
	task := models.Task{
		ID:         "task-pr-merge-1",
		ProjectID:  "proj-pr-1",
		Title:      "PR Merge Task",
		Prompt:     "Implement feature",
		BranchName: &branch,
		PRURL:      &prURL,
		Status:     models.TaskStatusReview,
	}
	_ = taskRepo.Create(ctx, &task)

	// 1. Inbound webhook for closed PR but NOT merged
	notMergedPayload := []byte(`{
		"action": "closed",
		"repository": { "full_name": "owner/pr-repo" },
		"pull_request": {
			"number": 12,
			"state": "closed",
			"merged": false,
			"html_url": "https://github.com/owner/pr-repo/pull/12",
			"head": {
				"ref": "task/task-pr-merge-1"
			}
		}
	}`)
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(notMergedPayload)
	sigNotMerged := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	res1, err := webhookSvc.HandleGitHubWebhook(ctx, notMergedPayload, "del-pr-not-merged", sigNotMerged)
	if err != nil {
		t.Fatalf("unexpected error for unmerged PR webhook: %v", err)
	}
	if res1.TaskID != "" {
		t.Errorf("unmerged PR should not mark task done")
	}

	taskCheck1, _ := taskRepo.GetByID(ctx, "task-pr-merge-1")
	if taskCheck1.Status != models.TaskStatusReview {
		t.Errorf("expected task to remain in review, got %s", taskCheck1.Status)
	}

	// 2. Inbound webhook for closed PR WITH merged = true
	mergedPayload := []byte(`{
		"action": "closed",
		"repository": { "owner": "owner", "full_name": "owner/pr-repo" },
		"pull_request": {
			"number": 12,
			"state": "closed",
			"merged": true,
			"merged_at": "2026-10-08T03:00:00Z",
			"html_url": "https://github.com/owner/pr-repo/pull/12",
			"head": {
				"ref": "task/task-pr-merge-1"
			}
		}
	}`)
	mac2 := hmac.New(sha256.New, []byte(webhookSecret))
	mac2.Write(mergedPayload)
	sigMerged := "sha256=" + hex.EncodeToString(mac2.Sum(nil))

	res2, err := webhookSvc.HandleGitHubWebhook(ctx, mergedPayload, "del-pr-merged", sigMerged)
	if err != nil {
		t.Fatalf("unexpected error for merged PR webhook: %v", err)
	}
	if res2.TaskID != "task-pr-merge-1" {
		t.Errorf("expected task-pr-merge-1 to be marked done, got %s", res2.TaskID)
	}

	taskCheck2, _ := taskRepo.GetByID(ctx, "task-pr-merge-1")
	if taskCheck2.Status != models.TaskStatusDone {
		t.Fatalf("expected task status to be %s, got %s", models.TaskStatusDone, taskCheck2.Status)
	}
}
