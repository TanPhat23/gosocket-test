package service_test

import (
	"context"
	"testing"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
)

func TestIntegrationService_CreateAndList(t *testing.T) {
	db := setupTestDB(t)

	integRepo := repository.NewIntegrationRepository(db)
	projRepo := repository.NewProjectRepository(db)
	credRepo := repository.NewCredentialRepository(db)

	svc := service.NewIntegrationService(integRepo, projRepo, credRepo)
	ctx := context.Background()

	// Seed project and credential
	_ = projRepo.Create(ctx, &models.Project{ID: "proj-123", Name: "Demo", TeamID: "team-1", RepoPath: "/tmp"})
	_ = credRepo.Create(ctx, &models.Credential{ID: "cred-123", TeamID: "team-1", Provider: models.ProviderGitHub, Label: "My PAT", EncryptedData: "encrypted"})

	webhookSec := "whsec_supersecret123"
	req := dto.CreateIntegrationRequest{
		CredentialID:  "cred-123",
		Provider:      "github",
		Repo:          "octocat/hello-world",
		WebhookSecret: webhookSec,
	}

	res, err := svc.CreateIntegration(ctx, "proj-123", req)
	if err != nil {
		t.Fatalf("failed to create integration: %v", err)
	}
	if res.ExternalID != "octocat/hello-world" {
		t.Errorf("expected external_id octocat/hello-world, got %s", res.ExternalID)
	}

	// Security Fix 2 verification: webhook_secret is masked
	if res.Config["webhook_secret"] != "********" {
		t.Errorf("expected masked webhook_secret '********', got '%v'", res.Config["webhook_secret"])
	}

	list, err := svc.ListIntegrations(ctx, "proj-123")
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 integration, got %d (err: %v)", len(list), err)
	}
	if list[0].Config["webhook_secret"] != "********" {
		t.Errorf("expected masked webhook_secret in list response")
	}
}

func TestIntegrationService_TenantIsolation(t *testing.T) {
	db := setupTestDB(t)

	integRepo := repository.NewIntegrationRepository(db)
	projRepo := repository.NewProjectRepository(db)
	credRepo := repository.NewCredentialRepository(db)

	svc := service.NewIntegrationService(integRepo, projRepo, credRepo)
	ctx := context.Background()

	// Team 1 project, but Team 2 credential
	_ = projRepo.Create(ctx, &models.Project{ID: "proj-1", Name: "Demo", TeamID: "team-1", RepoPath: "/tmp"})
	_ = credRepo.Create(ctx, &models.Credential{ID: "cred-attacker", TeamID: "team-2", Provider: models.ProviderGitHub, Label: "Other PAT", EncryptedData: "encrypted"})

	req := dto.CreateIntegrationRequest{
		CredentialID: "cred-attacker",
		Provider:     "github",
		Repo:         "attacker/private-repo",
	}

	_, err := svc.CreateIntegration(ctx, "proj-1", req)
	if err == nil || err != service.ErrUnauthorizedTenant {
		t.Fatalf("expected ErrUnauthorizedTenant, got %v", err)
	}
}

func TestIntegrationService_InputValidation(t *testing.T) {
	db := setupTestDB(t)

	integRepo := repository.NewIntegrationRepository(db)
	projRepo := repository.NewProjectRepository(db)
	credRepo := repository.NewCredentialRepository(db)

	svc := service.NewIntegrationService(integRepo, projRepo, credRepo)
	ctx := context.Background()

	_ = projRepo.Create(ctx, &models.Project{ID: "proj-valid", Name: "Demo", TeamID: "team-1", RepoPath: "/tmp"})
	_ = credRepo.Create(ctx, &models.Credential{ID: "cred-valid", TeamID: "team-1", Provider: models.ProviderGitHub, Label: "PAT", EncryptedData: "encrypted"})

	badRepos := []string{
		"../escaped/repo",
		"/leading/slash",
		"trailing/slash/",
		"invalid slug",
		"onlyonename",
		"-bad/repo",
	}

	for _, repo := range badRepos {
		_, err := svc.CreateIntegration(ctx, "proj-valid", dto.CreateIntegrationRequest{
			CredentialID: "cred-valid",
			Provider:     "github",
			Repo:         repo,
		})
		if err == nil || err != service.ErrInvalidRepoSlug {
			t.Errorf("expected ErrInvalidRepoSlug for '%s', got %v", repo, err)
		}
	}

	badBranches := []string{
		"-branch",
		"feat/..",
		"branch with spaces",
	}

	for _, branch := range badBranches {
		_, err := svc.CreateIntegration(ctx, "proj-valid", dto.CreateIntegrationRequest{
			CredentialID: "cred-valid",
			Provider:     "github",
			Repo:         "octocat/valid-repo",
			BaseBranch:   branch,
		})
		if err == nil || err != service.ErrInvalidBaseBranch {
			t.Errorf("expected ErrInvalidBaseBranch for branch '%s', got %v", branch, err)
		}
	}
}

func TestIntegrationService_UpdateAndDelete(t *testing.T) {
	db := setupTestDB(t)

	integRepo := repository.NewIntegrationRepository(db)
	projRepo := repository.NewProjectRepository(db)
	credRepo := repository.NewCredentialRepository(db)

	svc := service.NewIntegrationService(integRepo, projRepo, credRepo)
	ctx := context.Background()

	_ = projRepo.Create(ctx, &models.Project{ID: "proj-1", Name: "Demo", TeamID: "team-1", RepoPath: "/tmp"})
	_ = credRepo.Create(ctx, &models.Credential{ID: "cred-1", TeamID: "team-1", Provider: models.ProviderGitHub, Label: "PAT 1", EncryptedData: "encrypted"})
	_ = credRepo.Create(ctx, &models.Credential{ID: "cred-2", TeamID: "team-1", Provider: models.ProviderGitHub, Label: "PAT 2", EncryptedData: "encrypted"})

	created, err := svc.CreateIntegration(ctx, "proj-1", dto.CreateIntegrationRequest{
		CredentialID: "cred-1",
		Provider:     "github",
		Repo:         "octocat/hello-world",
	})
	if err != nil {
		t.Fatalf("failed to create: %v", err)
	}

	newBranch := "develop"
	disabled := false
	newCred := "cred-2"
	updated, err := svc.UpdateIntegration(ctx, "proj-1", created.ID, dto.UpdateIntegrationRequest{
		BaseBranch:   &newBranch,
		Enabled:      &disabled,
		CredentialID: &newCred,
	})
	if err != nil {
		t.Fatalf("failed to update: %v", err)
	}
	if updated.Enabled != false || updated.CredentialID != "cred-2" || updated.Config["base_branch"] != "develop" {
		t.Errorf("unexpected updated state: %+v", updated)
	}

	if err := svc.DeleteIntegration(ctx, "proj-1", created.ID); err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	_, err = svc.GetIntegration(ctx, "proj-1", created.ID)
	if err == nil || err != service.ErrIntegrationNotFound {
		t.Errorf("expected ErrIntegrationNotFound, got %v", err)
	}
}

func TestIntegrationService_WebhookCallbackURL(t *testing.T) {
	db := setupTestDB(t)
	integRepo := repository.NewIntegrationRepository(db)
	projRepo := repository.NewProjectRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	svc := service.NewIntegrationService(integRepo, projRepo, credRepo)
	ctx := context.Background()

	_ = projRepo.Create(ctx, &models.Project{ID: "proj-cb", Name: "Demo", TeamID: "team-1", RepoPath: "/tmp"})
	_ = credRepo.Create(ctx, &models.Credential{ID: "cred-cb", TeamID: "team-1", Provider: models.ProviderGitHub, Label: "PAT", EncryptedData: "encrypted"})

	const cb = "https://atm.example.com/api/v1/webhooks/trello"
	created, err := svc.CreateIntegration(ctx, "proj-cb", dto.CreateIntegrationRequest{
		CredentialID: "cred-cb", Provider: "github", Repo: "octocat/hello-world",
		WebhookCallbackURL: cb,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Config["webhook_callback_url"] != cb {
		t.Errorf("create did not store callback url: %v", created.Config)
	}

	newCB := "https://atm.example.com/other"
	updated, err := svc.UpdateIntegration(ctx, "proj-cb", created.ID, dto.UpdateIntegrationRequest{WebhookCallbackURL: &newCB})
	if err != nil || updated.Config["webhook_callback_url"] != newCB {
		t.Errorf("update did not store callback url: %v err=%v", updated, err)
	}

	for _, bad := range []string{"ftp://x.example/a", "not a url", "javascript:alert(1)", "https://", "/relative", "https://u:p@host/x"} {
		if _, err := svc.CreateIntegration(ctx, "proj-cb", dto.CreateIntegrationRequest{
			CredentialID: "cred-cb", Provider: "github", Repo: "octocat/hello-world", WebhookCallbackURL: bad,
		}); err != service.ErrInvalidWebhookCallbackURL {
			t.Errorf("create %q: want ErrInvalidWebhookCallbackURL, got %v", bad, err)
		}
		b := bad
		if _, err := svc.UpdateIntegration(ctx, "proj-cb", created.ID, dto.UpdateIntegrationRequest{WebhookCallbackURL: &b}); err != service.ErrInvalidWebhookCallbackURL {
			t.Errorf("update %q: want ErrInvalidWebhookCallbackURL, got %v", bad, err)
		}
	}
}
