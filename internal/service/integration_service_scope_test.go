package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
)

func TestIntegrationService_ScopeAndLookupErrors(t *testing.T) {
	db := setupTestDB(t)
	integRepo := repository.NewIntegrationRepository(db)
	projRepo := repository.NewProjectRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	svc := service.NewIntegrationService(integRepo, projRepo, credRepo)
	ctx := context.Background()

	_ = projRepo.Create(ctx, &models.Project{ID: "p1", Name: "A", TeamID: "t1", RepoPath: "/tmp"})
	_ = projRepo.Create(ctx, &models.Project{ID: "p2", Name: "B", TeamID: "t2", RepoPath: "/tmp"})
	_ = credRepo.Create(ctx, &models.Credential{ID: "c1", TeamID: "t1", Provider: models.ProviderGitHub, Label: "x", EncryptedData: "e"})
	_ = credRepo.Create(ctx, &models.Credential{ID: "c2", TeamID: "t2", Provider: models.ProviderGitHub, Label: "y", EncryptedData: "e"})
	created, err := svc.CreateIntegration(ctx, "p1", dto.CreateIntegrationRequest{CredentialID: "c1", Provider: "github", Repo: "o/r"})
	if err != nil {
		t.Fatal(err)
	}

	// project not found (prefix of wrapped message + sentinel)
	_, err = svc.CreateIntegration(ctx, "nope", dto.CreateIntegrationRequest{CredentialID: "c1", Provider: "github", Repo: "o/r"})
	if !errors.Is(err, service.ErrProjectNotFound) || !strings.HasPrefix(err.Error(), "project not found: ") {
		t.Fatalf("create: %v", err)
	}
	if _, err = svc.ListIntegrations(ctx, "nope"); !errors.Is(err, service.ErrProjectNotFound) || !strings.HasPrefix(err.Error(), "project not found: ") {
		t.Fatalf("list: %v", err)
	}
	if _, err = svc.UpdateIntegration(ctx, "nope", created.ID, dto.UpdateIntegrationRequest{}); !errors.Is(err, service.ErrProjectNotFound) {
		t.Fatalf("update proj: %v", err)
	}

	// credential not found
	_, err = svc.CreateIntegration(ctx, "p1", dto.CreateIntegrationRequest{CredentialID: "zz", Provider: "github", Repo: "o/r"})
	if !errors.Is(err, service.ErrCredentialNotFound) {
		t.Fatalf("create cred: %v", err)
	}
	missing := "zz"
	if _, err = svc.UpdateIntegration(ctx, "p1", created.ID, dto.UpdateIntegrationRequest{CredentialID: &missing}); !errors.Is(err, service.ErrCredentialNotFound) {
		t.Fatalf("update cred: %v", err)
	}
	// tenant mismatch
	other := "c2"
	if _, err = svc.UpdateIntegration(ctx, "p1", created.ID, dto.UpdateIntegrationRequest{CredentialID: &other}); err != service.ErrUnauthorizedTenant {
		t.Fatalf("update tenant: %v", err)
	}
	if _, err = svc.CreateIntegration(ctx, "p1", dto.CreateIntegrationRequest{CredentialID: "c2", Provider: "github", Repo: "o/r"}); err != service.ErrUnauthorizedTenant {
		t.Fatalf("create tenant: %v", err)
	}

	// integration scope mismatch / missing => ErrIntegrationNotFound exactly
	for name, f := range map[string]func() error{
		"get-missing":  func() error { _, e := svc.GetIntegration(ctx, "p1", "zz"); return e },
		"get-wrong":    func() error { _, e := svc.GetIntegration(ctx, "p2", created.ID); return e },
		"upd-missing":  func() error { _, e := svc.UpdateIntegration(ctx, "p1", "zz", dto.UpdateIntegrationRequest{}); return e },
		"upd-wrong":    func() error { _, e := svc.UpdateIntegration(ctx, "p2", created.ID, dto.UpdateIntegrationRequest{}); return e },
		"del-missing":  func() error { return svc.DeleteIntegration(ctx, "p1", "zz") },
		"del-wrong":    func() error { return svc.DeleteIntegration(ctx, "p2", created.ID) },
	} {
		if e := f(); e != service.ErrIntegrationNotFound {
			t.Errorf("%s: %v", name, e)
		}
	}
}
