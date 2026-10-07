package service_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
