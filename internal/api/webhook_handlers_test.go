package api_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func setupWebhookTestApp(t *testing.T) (*fiber.App, *gorm.DB, integrations.IdempotencyStore, *crypto.SecretCipher) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("failed to connect sqlite: %v", err)
	}

	tables := []string{
		`CREATE TABLE teams (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			name TEXT NOT NULL,
			slug TEXT NOT NULL
		);`,
		`CREATE TABLE projects (
			id TEXT PRIMARY KEY,
			team_id TEXT NOT NULL,
			name TEXT NOT NULL,
			repo_path TEXT NOT NULL,
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
		`CREATE TABLE tasks (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			heartbeat_at DATETIME,
			integration_id TEXT,
			asynq_task_id TEXT,
			session_id TEXT,
			external_id TEXT,
			external_url TEXT,
			pr_url TEXT,
			worktree_path TEXT,
			branch_name TEXT,
			model TEXT,
			container_id TEXT,
			exit_code INTEGER,
			skills TEXT NOT NULL DEFAULT '[]',
			mcp_servers TEXT NOT NULL DEFAULT '[]',
			project_id TEXT NOT NULL,
			title TEXT NOT NULL,
			prompt TEXT NOT NULL,
			assignee_id TEXT NOT NULL,
			status TEXT NOT NULL,
			assignee_type TEXT NOT NULL DEFAULT 'agent',
			priority INTEGER NOT NULL DEFAULT 50,
			parent_task_id TEXT
		);`,
		`CREATE TABLE webhook_events (
			provider TEXT NOT NULL,
			project_id TEXT NOT NULL,
			provider_event_id TEXT NOT NULL,
			status TEXT NOT NULL,
			error_message TEXT,
			received_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			processed_at DATETIME,
			PRIMARY KEY (provider, project_id, provider_event_id)
		);`,
	}

	for _, stmt := range tables {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to create table: %v", err)
		}
	}

	key := []byte("01234567890123456789012345678901")
	cipher, err := crypto.NewSecretCipher(key)
	if err != nil {
		t.Fatalf("failed to init cipher: %v", err)
	}

	idempotencyStore := integrations.NewMemoryIdempotencyStore()

	app := fiber.New()
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	webhookSvc := service.NewWebhookService(integRepo, credRepo, taskRepo, idempotencyStore, cipher, nil)

	handler := api.NewWebhookHandlers(webhookSvc)
	app.Post("/api/v1/webhooks/:provider", handler.HandleWebhook)

	return app, db, idempotencyStore, cipher
}

func TestWebhook_GitHub_UnsupportedProvider(t *testing.T) {
	app, _, _, _ := setupWebhookTestApp(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/unknown", strings.NewReader(`{}`))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d", resp.StatusCode)
	}
}

func TestWebhook_GitHub_HMACAndIdempotency(t *testing.T) {
	app, db, _, cipher := setupWebhookTestApp(t)

	team := models.Team{ID: "team-1", Name: "Team 1"}
	db.Create(&team)
	project := models.Project{ID: "proj-1", TeamID: "team-1", Name: "Project 1", RepoPath: "/tmp/repo"}
	db.Create(&project)

	secret := "my-webhook-secret"
	encrypted, _ := cipher.Encrypt(secret)
	cred := models.Credential{
		ID:            "cred-1",
		TeamID:        "team-1",
		Provider:      models.ProviderGitHub,
		Label:         "GH Secret",
		EncryptedData: encrypted,
	}
	db.Create(&cred)

	integ := models.ProjectIntegration{
		ID:           "integ-1",
		ProjectID:    "proj-1",
		CredentialID: "cred-1",
		Provider:     models.IntegrationGitHub,
		ExternalID:   "owner/repo",
		Enabled:      true,
	}
	db.Create(&integ)

	body := `{"action":"opened","repository":{"full_name":"owner/repo"},"issue":{"number":42,"title":"Fix bug","body":"Bug details","html_url":"https://github.com/owner/repo/issues/42","state":"open"}}`

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// Invalid signature test
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Delivery", "delivery-001")
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for invalid signature, got %d", resp.StatusCode)
	}

	// Valid request test
	req = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Delivery", "delivery-001")
	req.Header.Set("X-Hub-Signature-256", sig)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("test request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}

	// Verify task created in database
	var task models.Task
	if err := db.Where("project_id = ? AND external_id = ?", "proj-1", "42").First(&task).Error; err != nil {
		t.Fatalf("expected task to be created in DB, got error: %v", err)
	}
	if task.Title != "Fix bug" {
		t.Errorf("expected title 'Fix bug', got %q", task.Title)
	}

	// Duplicate delivery ID (Idempotency replay test)
	reqDup := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(body))
	reqDup.Header.Set("X-GitHub-Delivery", "delivery-001")
	reqDup.Header.Set("X-Hub-Signature-256", sig)
	respDup, err := app.Test(reqDup)
	if err != nil {
		t.Fatalf("duplicate test request failed: %v", err)
	}
	if respDup.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK on duplicate delivery, got %d", respDup.StatusCode)
	}
}
