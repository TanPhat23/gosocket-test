package repository_test

import (
	"context"
	"testing"
	"time"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
)

func TestIntegrationRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewIntegrationRepository(db)
	ctx := context.Background()

	extURL := "https://github.com/octocat/hello-world"
	integ := &models.ProjectIntegration{
		ID:           "integ-1",
		ProjectID:    "proj-1",
		CredentialID: "cred-1",
		Provider:     models.IntegrationGitHub,
		ExternalID:   "octocat/hello-world",
		ExternalURL:  &extURL,
		Enabled:      true,
		Config: models.JSONMap{
			"base_branch": "main",
			"auto_pr":     true,
		},
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}

	// 1. Create
	if err := repo.Create(ctx, integ); err != nil {
		t.Fatalf("failed to create integration: %v", err)
	}

	// 2. GetByID
	got, err := repo.GetByID(ctx, "integ-1")
	if err != nil {
		t.Fatalf("failed to get integration by ID: %v", err)
	}
	if got.ExternalID != "octocat/hello-world" {
		t.Errorf("expected external_id octocat/hello-world, got %s", got.ExternalID)
	}

	// 3. ListByProjectID
	list, err := repo.ListByProjectID(ctx, "proj-1")
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 integration in list, got %d (err: %v)", len(list), err)
	}

	// 3.1 GetActiveByProviderAndExternalID
	active, err := repo.GetActiveByProviderAndExternalID(ctx, models.IntegrationGitHub, "octocat/hello-world")
	if err != nil || active.ID != "integ-1" {
		t.Fatalf("expected active integration integ-1, got %v (err: %v)", active, err)
	}

	// 4. Update
	got.Enabled = false
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("failed to update integration: %v", err)
	}
	updated, _ := repo.GetByID(ctx, "integ-1")
	if updated.Enabled != false {
		t.Errorf("expected enabled to be false")
	}

	// Active lookup should now fail
	if _, err := repo.GetActiveByProviderAndExternalID(ctx, models.IntegrationGitHub, "octocat/hello-world"); err == nil {
		t.Errorf("expected error getting inactive integration, got nil")
	}

	// 5. Delete
	if err := repo.Delete(ctx, "integ-1"); err != nil {
		t.Fatalf("failed to delete integration: %v", err)
	}
	deleted, err := repo.GetByID(ctx, "integ-1")
	if err == nil || deleted != nil {
		t.Errorf("expected error or nil for deleted integration")
	}
}
