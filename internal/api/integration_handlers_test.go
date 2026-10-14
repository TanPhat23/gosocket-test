package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func setupIntegrationTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("failed to connect sqlite: %v", err)
	}

	tables := []string{
		`CREATE TABLE projects (
			id TEXT PRIMARY KEY,
			team_id TEXT NOT NULL,
			name TEXT NOT NULL,
			repo_path TEXT NOT NULL,
			runtime_config TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE credentials (
			id TEXT PRIMARY KEY,
			team_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			label TEXT NOT NULL,
			encrypted_data TEXT NOT NULL,
			key_fingerprint TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE project_integrations (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			credential_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			external_id TEXT NOT NULL,
			external_url TEXT,
			config TEXT NOT NULL DEFAULT '{}',
			enabled BOOLEAN NOT NULL DEFAULT 1,
			last_synced_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, stmt := range tables {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to create table: %v", err)
		}
	}
	return db
}

func setupIntegrationApp(t *testing.T) (*fiber.App, service.IntegrationService) {
	db := setupIntegrationTestDB(t)

	integRepo := repository.NewIntegrationRepository(db)
	projRepo := repository.NewProjectRepository(db)
	credRepo := repository.NewCredentialRepository(db)

	ctx := t.Context()
	projID := "11111111-1111-1111-1111-111111111111"
	cred1ID := "22222222-2222-2222-2222-222222222222"
	credOtherID := "33333333-3333-3333-3333-333333333333"

	_ = projRepo.Create(ctx, &models.Project{ID: projID, Name: "Project 1", TeamID: "team-1", RepoPath: "/tmp"})
	_ = credRepo.Create(ctx, &models.Credential{ID: cred1ID, TeamID: "team-1", Provider: models.ProviderGitHub, Label: "PAT", EncryptedData: "secret"})
	_ = credRepo.Create(ctx, &models.Credential{ID: credOtherID, TeamID: "team-other", Provider: models.ProviderGitHub, Label: "PAT Other", EncryptedData: "secret"})

	integSvc := service.NewIntegrationService(integRepo, projRepo, credRepo)
	handlers := api.NewIntegrationHandlers(integSvc)

	app := fiber.New(fiber.Config{
		StructValidator: api.NewStructValidator(),
	})
	v1 := app.Group("/api/v1/projects/:project_id/integrations")
	v1.Get("", handlers.List)
	v1.Post("", handlers.Create)
	v1.Get("/:id", handlers.Get)
	v1.Patch("/:id", handlers.Update)
	v1.Delete("/:id", handlers.Delete)

	return app, integSvc
}

func TestIntegrationHandlers_CRUD(t *testing.T) {
	app, _ := setupIntegrationApp(t)

	projID := "11111111-1111-1111-1111-111111111111"
	cred1ID := "22222222-2222-2222-2222-222222222222"
	credOtherID := "33333333-3333-3333-3333-333333333333"

	// 1. Create with invalid slug -> 400
	badReq := dto.CreateIntegrationRequest{
		CredentialID: cred1ID,
		Provider:     "github",
		Repo:         "invalid-slug",
	}
	badBody, _ := json.Marshal(badReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projID+"/integrations", bytes.NewReader(badBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for invalid slug, got %d (err: %v)", resp.StatusCode, err)
	}

	// 2. Create with cross-tenant credential -> 403 Forbidden
	crossReq := dto.CreateIntegrationRequest{
		CredentialID: credOtherID,
		Provider:     "github",
		Repo:         "octocat/spoon-knife",
	}
	crossBody, _ := json.Marshal(crossReq)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projID+"/integrations", bytes.NewReader(crossBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for cross tenant credential, got %d (err: %v)", resp.StatusCode, err)
	}

	// 3. Create valid integration -> 201 Created
	validReq := dto.CreateIntegrationRequest{
		CredentialID:  cred1ID,
		Provider:      "github",
		Repo:          "octocat/spoon-knife",
		WebhookSecret: "secret123",
	}
	validBody, _ := json.Marshal(validReq)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projID+"/integrations", bytes.NewReader(validBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d (err: %v)", resp.StatusCode, err)
	}
	var created dto.IntegrationResponse
	_ = json.NewDecoder(resp.Body).Decode(&created)
	if created.Config["webhook_secret"] != "********" {
		t.Errorf("expected masked webhook_secret in response, got %v", created.Config["webhook_secret"])
	}

	// 4. List integrations -> 200 OK
	req = httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projID+"/integrations", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on list, got %d", resp.StatusCode)
	}

	// 5. Get integration -> 200 OK
	req = httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projID+"/integrations/"+created.ID, nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on get, got %d", resp.StatusCode)
	}

	// 6. Update integration -> 200 OK
	newBranch := "develop"
	updateReq := dto.UpdateIntegrationRequest{BaseBranch: &newBranch}
	upBody, _ := json.Marshal(updateReq)
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/projects/"+projID+"/integrations/"+created.ID, bytes.NewReader(upBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on patch, got %d", resp.StatusCode)
	}

	// 7. Delete integration -> 200 OK
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+projID+"/integrations/"+created.ID, nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on delete, got %d", resp.StatusCode)
	}

	// 8. Get deleted -> 404
	req = httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projID+"/integrations/"+created.ID, nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on get after delete, got %d", resp.StatusCode)
	}
}
