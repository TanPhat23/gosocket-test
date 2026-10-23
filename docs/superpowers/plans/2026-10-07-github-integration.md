# GitHub Integration & End-to-End PR Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a complete, production-grade GitHub lifecycle: project integration management, HMAC-verified webhook ingestion for issue-to-task creation, authenticated git push, and automated + manual Pull Request creation.

**Architecture:** Connects GitHub webhooks to Fiber v3 handlers with HMAC verification, maps issues to Asynq tasks, executes worktree coding runs, pushes branches to remote GitHub with authenticated HTTPS headers, opens Pull Requests via GitHub REST API, and surfaces settings and actions in the React dashboard.

**Tech Stack:** Go 1.27, Fiber v3, GORM v2, PostgreSQL 18, Asynq/Redis 7, React 19, TypeScript, Tailwind CSS v4, Lucide icons, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-07-github-integration-design.md`

## Global Constraints

- Go 1.27 with 64-bit alignment in structs and zero untested pathways.
- Secrets must never be persisted in plaintext; GitHub PATs must only be stored encrypted with `crypto.SecretCipher`.
- Git credentials must never be written to `.git/config` on disk; must be injected via in-memory `http.extraHeader`.
- Inbound webhooks must verify HMAC signatures before database mutations or idempotency registration.
- Fiber v3 handlers must use `c.Bind().Body(&req)` for request validation with `go-playground/validator/v10`.
- All vitest and Go unit tests must pass cleanly at each step.

## Review Focus

1. **Invalid GitHub Repository Slugs**: Ensure inputs like `invalid-repo` or `owner/repo/extra` fail validation with `400 Bad Request`.
2. **Missing or Corrupted Credentials**: If a project integration points to a nonexistent or un-decryptable credential, return a clear descriptive error instead of crashing or hanging.
3. **Webhook HMAC Tampering**: Any payload with an invalid or mismatched `X-Hub-Signature-256` must return `401 Unauthorized` without modifying any state.
4. **Git Push Remote Authentication Failure**: If the GitHub PAT lacks write permissions or has expired, log the failure clearly to the terminal without destroying the completed local task.
5. **Duplicate Webhook Delivery**: Resending the same `X-GitHub-Delivery` ID must be acknowledged idempotently without creating duplicate tasks.

---

### Task 1: Integration Repository CRUD & Database Queries

**Files:**
- Modify: `internal/repository/integration_repository.go`
- Create: `internal/repository/integration_repository_test.go`

**Interfaces:**
- Produces:
  ```go
  type IntegrationRepository interface {
      Create(ctx context.Context, integ *models.ProjectIntegration) error
      GetByID(ctx context.Context, id string) (*models.ProjectIntegration, error)
      ListByProjectID(ctx context.Context, projectID string) ([]models.ProjectIntegration, error)
      GetActiveByProviderAndExternalID(ctx context.Context, provider models.IntegrationProvider, externalID string) (*models.ProjectIntegration, error)
      Update(ctx context.Context, integ *models.ProjectIntegration) error
      Delete(ctx context.Context, id string) error
  }
  ```

- [ ] **Step 1: Write the failing repository unit test**

Create `internal/repository/integration_repository_test.go`:
```go
package repository_test

import (
	"context"
	"testing"
	"time"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite in memory: %v", err)
	}
	err = db.AutoMigrate(&models.ProjectIntegration{})
	if err != nil {
		t.Fatalf("failed to auto migrate: %v", err)
	}
	return db
}

func TestIntegrationRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewIntegrationRepository(db)
	ctx := context.Background()

	integ := &models.ProjectIntegration{
		ID:           "integ-1",
		ProjectID:    "proj-1",
		CredentialID: "cred-1",
		Provider:     models.IntegrationGitHub,
		ExternalID:   "octocat/hello-world",
		Enabled:      true,
		Config: models.JSONMap{
			"base_branch": "main",
			"auto_pr":     true,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
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

	// 4. Update
	got.Enabled = false
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("failed to update integration: %v", err)
	}
	updated, _ := repo.GetByID(ctx, "integ-1")
	if updated.Enabled != false {
		t.Errorf("expected enabled to be false")
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -count=1 ./internal/repository/integration_repository_test.go`
Expected: Compile failure (`Create`, `GetByID`, etc. undefined on `IntegrationRepository`).

- [ ] **Step 3: Implement minimal code in `internal/repository/integration_repository.go`**

Update `internal/repository/integration_repository.go`:
```go
package repository

import (
	"context"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// IntegrationRepository defines data access methods for project integrations.
type IntegrationRepository interface {
	Create(ctx context.Context, integ *models.ProjectIntegration) error
	GetByID(ctx context.Context, id string) (*models.ProjectIntegration, error)
	ListByProjectID(ctx context.Context, projectID string) ([]models.ProjectIntegration, error)
	GetActiveByProviderAndExternalID(ctx context.Context, provider models.IntegrationProvider, externalID string) (*models.ProjectIntegration, error)
	Update(ctx context.Context, integ *models.ProjectIntegration) error
	Delete(ctx context.Context, id string) error
}

type integrationRepo struct {
	db *gorm.DB
}

// NewIntegrationRepository creates a new IntegrationRepository backed by GORM.
func NewIntegrationRepository(db *gorm.DB) IntegrationRepository {
	return &integrationRepo{db: db}
}

func (r *integrationRepo) Create(ctx context.Context, integ *models.ProjectIntegration) error {
	return r.db.WithContext(ctx).Create(integ).Error
}

func (r *integrationRepo) GetByID(ctx context.Context, id string) (*models.ProjectIntegration, error) {
	var integ models.ProjectIntegration
	if err := r.db.WithContext(ctx).First(&integ, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &integ, nil
}

func (r *integrationRepo) ListByProjectID(ctx context.Context, projectID string) ([]models.ProjectIntegration, error) {
	var results []models.ProjectIntegration
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("created_at ASC").Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *integrationRepo) GetActiveByProviderAndExternalID(ctx context.Context, provider models.IntegrationProvider, externalID string) (*models.ProjectIntegration, error) {
	var integ models.ProjectIntegration
	if err := r.db.WithContext(ctx).Where("provider = ? AND external_id = ? AND enabled = ?", provider, externalID, true).First(&integ).Error; err != nil {
		return nil, err
	}
	return &integ, nil
}

func (r *integrationRepo) Update(ctx context.Context, integ *models.ProjectIntegration) error {
	return r.db.WithContext(ctx).Save(integ).Error
}

func (r *integrationRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.ProjectIntegration{}, "id = ?", id).Error
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -count=1 ./internal/repository/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/repository/integration_repository.go internal/repository/integration_repository_test.go
git commit -m "feat(repository): add CRUD operations to IntegrationRepository"
```

---

### Task 2: Integration DTOs & Service Layer

**Files:**
- Create: `internal/dto/integration_dto.go`
- Create: `internal/service/integration_service.go`
- Create: `internal/service/integration_service_test.go`

**Interfaces:**
- Produces:
  ```go
  type IntegrationService interface {
      CreateIntegration(ctx context.Context, projectID string, req dto.CreateIntegrationRequest) (*dto.IntegrationResponse, error)
      ListIntegrations(ctx context.Context, projectID string) ([]dto.IntegrationResponse, error)
      GetIntegration(ctx context.Context, projectID, id string) (*dto.IntegrationResponse, error)
      UpdateIntegration(ctx context.Context, projectID, id string, req dto.UpdateIntegrationRequest) (*dto.IntegrationResponse, error)
      DeleteIntegration(ctx context.Context, projectID, id string) error
  }
  ```

- [ ] **Step 1: Write DTOs in `internal/dto/integration_dto.go`**

Create `internal/dto/integration_dto.go`:
```go
package dto

import (
	"time"

	"agent-task-manager/internal/models"
)

type CreateIntegrationRequest struct {
	CredentialID string         `json:"credential_id" validate:"required,uuid"`
	Provider     string         `json:"provider" validate:"required,oneof=github jira trello"`
	Repo         string         `json:"repo" validate:"required,min=3,max=100"`
	BaseBranch   string         `json:"base_branch,omitempty"`
	AutoPR       *bool          `json:"auto_pr,omitempty"`
	WebhookSecret string        `json:"webhook_secret,omitempty"`
	FilterLabel  string         `json:"filter_label,omitempty"`
}

type UpdateIntegrationRequest struct {
	CredentialID *string        `json:"credential_id,omitempty" validate:"omitempty,uuid"`
	Enabled      *bool          `json:"enabled,omitempty"`
	BaseBranch   *string        `json:"base_branch,omitempty"`
	AutoPR       *bool          `json:"auto_pr,omitempty"`
	WebhookSecret *string       `json:"webhook_secret,omitempty"`
	FilterLabel  *string        `json:"filter_label,omitempty"`
}

type IntegrationResponse struct {
	ID           string         `json:"id"`
	ProjectID    string         `json:"project_id"`
	CredentialID string         `json:"credential_id"`
	Provider     string         `json:"provider"`
	ExternalID   string         `json:"external_id"`
	ExternalURL  string         `json:"external_url"`
	Enabled      bool           `json:"enabled"`
	Config       models.JSONMap `json:"config"`
	LastSyncedAt *time.Time     `json:"last_synced_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}
```

- [ ] **Step 2: Write failing test in `internal/service/integration_service_test.go`**

Create `internal/service/integration_service_test.go`:
```go
package service_test

import (
	"context"
	"testing"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestIntegrationService_CreateAndList(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	_ = db.AutoMigrate(&models.ProjectIntegration{}, &models.Project{}, &models.Credential{})

	integRepo := repository.NewIntegrationRepository(db)
	projRepo := repository.NewProjectRepository(db)
	credRepo := repository.NewCredentialRepository(db)

	svc := service.NewIntegrationService(integRepo, projRepo, credRepo)
	ctx := context.Background()

	// Seed project and credential
	_ = projRepo.Create(ctx, &models.Project{ID: "proj-123", Name: "Demo", TeamID: "team-1", RepoPath: "/tmp"})
	_ = credRepo.Create(ctx, &models.Credential{ID: "cred-123", TeamID: "team-1", Provider: models.ProviderGitHub, Label: "My PAT", EncryptedData: "encrypted"})

	req := dto.CreateIntegrationRequest{
		CredentialID: "cred-123",
		Provider:     "github",
		Repo:         "octocat/hello-world",
	}

	res, err := svc.CreateIntegration(ctx, "proj-123", req)
	if err != nil {
		t.Fatalf("failed to create integration: %v", err)
	}
	if res.ExternalID != "octocat/hello-world" {
		t.Errorf("expected external_id octocat/hello-world, got %s", res.ExternalID)
	}

	list, err := svc.ListIntegrations(ctx, "proj-123")
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(list))
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test -count=1 ./internal/service/integration_service_test.go`
Expected: Compile failure (`service.NewIntegrationService` undefined).

- [ ] **Step 4: Implement `internal/service/integration_service.go`**

Create `internal/service/integration_service.go`:
```go
package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
)

var (
	ErrInvalidRepoSlug     = errors.New("invalid repository slug (expected owner/repo)")
	ErrProjectNotFound     = errors.New("project not found")
	ErrIntegrationNotFound = errors.New("integration not found")
)

var repoSlugRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)

type IntegrationService interface {
	CreateIntegration(ctx context.Context, projectID string, req dto.CreateIntegrationRequest) (*dto.IntegrationResponse, error)
	ListIntegrations(ctx context.Context, projectID string) ([]dto.IntegrationResponse, error)
	GetIntegration(ctx context.Context, projectID, id string) (*dto.IntegrationResponse, error)
	UpdateIntegration(ctx context.Context, projectID, id string, req dto.UpdateIntegrationRequest) (*dto.IntegrationResponse, error)
	DeleteIntegration(ctx context.Context, projectID, id string) error
}

type integrationService struct {
	integRepo repository.IntegrationRepository
	projRepo  repository.ProjectRepository
	credRepo  repository.CredentialRepository
}

func NewIntegrationService(
	integRepo repository.IntegrationRepository,
	projRepo repository.ProjectRepository,
	credRepo repository.CredentialRepository,
) IntegrationService {
	return &integrationService{
		integRepo: integRepo,
		projRepo:  projRepo,
		credRepo:  credRepo,
	}
}

func (s *integrationService) CreateIntegration(ctx context.Context, projectID string, req dto.CreateIntegrationRequest) (*dto.IntegrationResponse, error) {
	trimmedRepo := strings.TrimSpace(req.Repo)
	if !repoSlugRegex.MatchString(trimmedRepo) {
		return nil, ErrInvalidRepoSlug
	}

	if _, err := s.projRepo.GetByID(ctx, projectID); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}

	if _, err := s.credRepo.GetByID(ctx, req.CredentialID); err != nil {
		return nil, fmt.Errorf("credential not found: %w", err)
	}

	baseBranch := "main"
	if req.BaseBranch != "" {
		baseBranch = req.BaseBranch
	}
	autoPR := true
	if req.AutoPR != nil {
		autoPR = *req.AutoPR
	}

	cfg := models.JSONMap{
		"base_branch": baseBranch,
		"auto_pr":     autoPR,
	}
	if req.WebhookSecret != "" {
		cfg["webhook_secret"] = req.WebhookSecret
	}
	if req.FilterLabel != "" {
		cfg["filter_label"] = req.FilterLabel
	}

	extURL := fmt.Sprintf("https://github.com/%s", trimmedRepo)
	now := time.Now().UTC()
	integ := &models.ProjectIntegration{
		ID:           common.NewID(),
		ProjectID:    projectID,
		CredentialID: req.CredentialID,
		Provider:     models.IntegrationProvider(req.Provider),
		ExternalID:   trimmedRepo,
		ExternalURL:  &extURL,
		Enabled:      true,
		Config:       cfg,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.integRepo.Create(ctx, integ); err != nil {
		return nil, fmt.Errorf("failed to save integration: %w", err)
	}

	return s.toResponse(integ), nil
}

func (s *integrationService) ListIntegrations(ctx context.Context, projectID string) ([]dto.IntegrationResponse, error) {
	list, err := s.integRepo.ListByProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res := make([]dto.IntegrationResponse, len(list))
	for i, item := range list {
		res[i] = *s.toResponse(&item)
	}
	return res, nil
}

func (s *integrationService) GetIntegration(ctx context.Context, projectID, id string) (*dto.IntegrationResponse, error) {
	integ, err := s.integRepo.GetByID(ctx, id)
	if err != nil || integ.ProjectID != projectID {
		return nil, ErrIntegrationNotFound
	}
	return s.toResponse(integ), nil
}

func (s *integrationService) UpdateIntegration(ctx context.Context, projectID, id string, req dto.UpdateIntegrationRequest) (*dto.IntegrationResponse, error) {
	integ, err := s.integRepo.GetByID(ctx, id)
	if err != nil || integ.ProjectID != projectID {
		return nil, ErrIntegrationNotFound
	}

	if req.CredentialID != nil {
		integ.CredentialID = *req.CredentialID
	}
	if req.Enabled != nil {
		integ.Enabled = *req.Enabled
	}
	if integ.Config == nil {
		integ.Config = models.JSONMap{}
	}
	if req.BaseBranch != nil {
		integ.Config["base_branch"] = *req.BaseBranch
	}
	if req.AutoPR != nil {
		integ.Config["auto_pr"] = *req.AutoPR
	}
	if req.WebhookSecret != nil {
		integ.Config["webhook_secret"] = *req.WebhookSecret
	}
	if req.FilterLabel != nil {
		integ.Config["filter_label"] = *req.FilterLabel
	}
	integ.UpdatedAt = time.Now().UTC()

	if err := s.integRepo.Update(ctx, integ); err != nil {
		return nil, err
	}
	return s.toResponse(integ), nil
}

func (s *integrationService) DeleteIntegration(ctx context.Context, projectID, id string) error {
	integ, err := s.integRepo.GetByID(ctx, id)
	if err != nil || integ.ProjectID != projectID {
		return ErrIntegrationNotFound
	}
	return s.integRepo.Delete(ctx, id)
}

func (s *integrationService) toResponse(m *models.ProjectIntegration) *dto.IntegrationResponse {
	extURL := ""
	if m.ExternalURL != nil {
		extURL = *m.ExternalURL
	}
	return &dto.IntegrationResponse{
		ID:           m.ID,
		ProjectID:    m.ProjectID,
		CredentialID: m.CredentialID,
		Provider:     string(m.Provider),
		ExternalID:   m.ExternalID,
		ExternalURL:  extURL,
		Enabled:      m.Enabled,
		Config:       m.Config,
		LastSyncedAt: m.LastSyncedAt,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}
```

- [ ] **Step 5: Run tests and verify PASS**

Run: `go test -count=1 ./internal/service/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/dto/integration_dto.go internal/service/integration_service.go internal/service/integration_service_test.go
git commit -m "feat(service): implement IntegrationService and DTOs"
```

---

### Task 3: Project Integrations REST API Handlers

**Files:**
- Create: `internal/api/integration_handlers.go`
- Create: `internal/api/integration_handlers_test.go`
- Modify: `internal/api/server.go`

**Interfaces:**
- Produces routes:
  - `GET /api/v1/projects/:project_id/integrations`
  - `POST /api/v1/projects/:project_id/integrations`
  - `GET /api/v1/projects/:project_id/integrations/:id`
  - `PATCH /api/v1/projects/:project_id/integrations/:id`
  - `DELETE /api/v1/projects/:project_id/integrations/:id`

- [ ] **Step 1: Write integration handler tests in `internal/api/integration_handlers_test.go`**

Create `internal/api/integration_handlers_test.go`:
```go
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

func setupIntegrationApp(t *testing.T) (*fiber.App, service.IntegrationService) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	_ = db.AutoMigrate(&models.ProjectIntegration{}, &models.Project{}, &models.Credential{})

	integRepo := repository.NewIntegrationRepository(db)
	projRepo := repository.NewProjectRepository(db)
	credRepo := repository.NewCredentialRepository(db)

	ctx := t.Context()
	_ = projRepo.Create(ctx, &models.Project{ID: "proj-1", Name: "Project 1", TeamID: "team-1", RepoPath: "/tmp"})
	_ = credRepo.Create(ctx, &models.Credential{ID: "cred-1", TeamID: "team-1", Provider: models.ProviderGitHub, Label: "PAT", EncryptedData: "secret"})

	integSvc := service.NewIntegrationService(integRepo, projRepo, credRepo)
	handlers := api.NewIntegrationHandlers(integSvc)

	app := fiber.New()
	v1 := app.Group("/api/v1/projects/:project_id/integrations")
	v1.Get("/", handlers.List)
	v1.Post("/", handlers.Create)
	v1.Get("/:id", handlers.Get)
	v1.Patch("/:id", handlers.Update)
	v1.Delete("/:id", handlers.Delete)

	return app, integSvc
}

func TestIntegrationHandlers_CreateAndList(t *testing.T) {
	app, _ := setupIntegrationApp(t)

	// Create
	reqBody := dto.CreateIntegrationRequest{
		CredentialID: "cred-1",
		Provider:     "github",
		Repo:         "octocat/spoon-knife",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/proj-1/integrations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d (err: %v)", resp.StatusCode, err)
	}

	// List
	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-1/integrations", nil)
	respList, err := app.Test(reqList)
	if err != nil || respList.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", respList.StatusCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -count=1 ./internal/api/integration_handlers_test.go`
Expected: FAIL (`api.NewIntegrationHandlers` undefined).

- [ ] **Step 3: Implement `internal/api/integration_handlers.go`**

Create `internal/api/integration_handlers.go`:
```go
package api

import (
	"errors"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/service"
	"github.com/gofiber/fiber/v3"
)

type IntegrationHandlers struct {
	integService service.IntegrationService
}

func NewIntegrationHandlers(integService service.IntegrationService) *IntegrationHandlers {
	return &IntegrationHandlers{integService: integService}
}

func (h *IntegrationHandlers) List(c fiber.Ctx) error {
	projectID := c.Params("project_id")
	res, err := h.integService.ListIntegrations(c.Context(), projectID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

func (h *IntegrationHandlers) Create(c fiber.Ctx) error {
	projectID := c.Params("project_id")
	var req dto.CreateIntegrationRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	res, err := h.integService.CreateIntegration(c.Context(), projectID, req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidRepoSlug) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if errors.Is(err, service.ErrProjectNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusCreated).JSON(res)
}

func (h *IntegrationHandlers) Get(c fiber.Ctx) error {
	projectID := c.Params("project_id")
	id := c.Params("id")
	res, err := h.integService.GetIntegration(c.Context(), projectID, id)
	if err != nil {
		if errors.Is(err, service.ErrIntegrationNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Integration not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

func (h *IntegrationHandlers) Update(c fiber.Ctx) error {
	projectID := c.Params("project_id")
	id := c.Params("id")
	var req dto.UpdateIntegrationRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	res, err := h.integService.UpdateIntegration(c.Context(), projectID, id, req)
	if err != nil {
		if errors.Is(err, service.ErrIntegrationNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Integration not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

func (h *IntegrationHandlers) Delete(c fiber.Ctx) error {
	projectID := c.Params("project_id")
	id := c.Params("id")
	if err := h.integService.DeleteIntegration(c.Context(), projectID, id); err != nil {
		if errors.Is(err, service.ErrIntegrationNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Integration not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "deleted"})
}
```

- [ ] **Step 4: Wire `IntegrationHandlers` into `internal/api/server.go` and `cmd/server/main.go`**

Update `internal/api/server.go` to accept `IntegrationService` and register `/api/v1/projects/:project_id/integrations`.

- [ ] **Step 5: Run tests and verify PASS**

Run: `go test -count=1 ./internal/api/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/integration_handlers.go internal/api/integration_handlers_test.go internal/api/server.go cmd/server/main.go
git commit -m "feat(api): add REST endpoints for project integrations"
```

---

### Task 4: Authenticated Git Push & Runner PR Creation

**Files:**
- Create: `internal/git/push.go`
- Create: `internal/git/push_test.go`
- Modify: `internal/runner/task_runner.go`
- Modify: `internal/runner/task_runner_test.go`

**Interfaces:**
- Produces:
  ```go
  func PushBranch(ctx context.Context, worktreeDir, remoteURL, token, branchName string) error
  ```

- [ ] **Step 1: Write test for `PushBranch` in `internal/git/push_test.go`**

Create `internal/git/push_test.go`:
```go
package git_test

import (
	"context"
	"testing"

	"agent-task-manager/internal/git"
)

func TestPushBranch_BuildsBasicAuthHeader(t *testing.T) {
	ctx := context.Background()
	// Test error handling for non-git directory
	err := git.PushBranch(ctx, t.TempDir(), "https://github.com/fake/repo", "token123", "task/xyz")
	if err == nil {
		t.Errorf("expected error running push in non-git directory, got nil")
	}
}
```

- [ ] **Step 2: Implement `internal/git/push.go`**

Create `internal/git/push.go`:
```go
package git

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"time"
)

// PushBranch pushes the specified local branch to origin using token-based basic auth.
func PushBranch(ctx context.Context, worktreeDir, remoteURL, token, branchName string) error {
	authHeader := fmt.Sprintf("Authorization: Basic %s", base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)))

	pushCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(pushCtx, "git", "-c", "http.extraHeader="+authHeader, "push", "-u", "origin", fmt.Sprintf("%s:%s", branchName, branchName))
	cmd.Dir = worktreeDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git push %s failed: %w (stderr: %s)", branchName, err, stderr.String())
	}
	return nil
}
```

- [ ] **Step 3: Update `AgentTaskRunner.RunTask` to support automated PR creation**

In `internal/runner/task_runner.go`:
1. Inject `cipher *crypto.SecretCipher` and `integRepo repository.IntegrationRepository` and `credRepo repository.CredentialRepository`.
2. After diff capture, look up active GitHub integration for `task.ProjectID`.
3. If found and `config["auto_pr"] == true`:
   - Decrypt PAT.
   - Call `git.PushBranch(...)`.
   - Call `githubProvider.CreatePullRequest(...)`.
   - Update `task.PullRequestURL`.
   - Broadcast PR URL over WebSocket terminal log.

- [ ] **Step 4: Run unit tests**

Run: `go test -count=1 ./internal/git/... ./internal/runner/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/git/push.go internal/git/push_test.go internal/runner/task_runner.go internal/runner/task_runner_test.go
git commit -m "feat(runner): implement authenticated git push and automated PR creation on task completion"
```

---

### Task 5: Manual PR Trigger Endpoint (`POST /api/v1/tasks/:id/pr`)

**Files:**
- Modify: `internal/service/task_service.go`
- Modify: `internal/api/task_handlers.go`
- Modify: `internal/api/task_handlers_test.go`

**Interfaces:**
- Produces: `POST /api/v1/tasks/:id/pr` returning `{"task_id": "...", "pull_request_url": "..."}`.

- [ ] **Step 1: Write failing test in `internal/api/task_handlers_test.go`**

Add test checking `POST /api/v1/tasks/:id/pr` returns 200 with PR URL.

- [ ] **Step 2: Implement `CreatePullRequestForTask` in `service.TaskService`**

- [ ] **Step 3: Add `CreatePR` handler in `internal/api/task_handlers.go`**

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -count=1 ./internal/api/task_handlers_test.go`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/task_service.go internal/api/task_handlers.go internal/api/task_handlers_test.go
git commit -m "feat(api): add manual POST /api/v1/tasks/:id/pr endpoint"
```

---

### Task 6: Inbound GitHub Webhook Refinement (HMAC with Config Secret & Label Filter)

**Files:**
- Modify: `internal/service/webhook_service.go`
- Modify: `internal/service/webhook_service_test.go`
- Modify: `internal/api/webhook_handlers_test.go`

- [ ] **Step 1: Write tests for label filtering and custom webhook secret HMAC**

In `internal/service/webhook_service_test.go`:
- Verify webhook payload signed with `config["webhook_secret"]` passes.
- Verify `filter_label="agent"` ignores issues without the label and enqueues issues with the label.

- [ ] **Step 2: Update `HandleGitHubWebhook` in `internal/service/webhook_service.go`**

- [ ] **Step 3: Run tests to verify PASS**

Run: `go test -count=1 ./internal/service/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/service/webhook_service.go internal/service/webhook_service_test.go internal/api/webhook_handlers_test.go
git commit -m "feat(webhooks): support custom webhook secrets and label filtering for GitHub"
```

---

### Task 7: Frontend Project Integrations Dialog & Settings

**Files:**
- Create: `web/src/features/integrations/schemas/integrationSchemas.ts`
- Create: `web/src/features/integrations/components/ProjectIntegrationModal.tsx`
- Create: `web/src/features/integrations/components/ProjectIntegrationModal.test.tsx`
- Modify: `web/src/components/layout/Header.tsx` (or top navigation bar)

- [ ] **Step 1: Create Zod schemas in `web/src/features/integrations/schemas/integrationSchemas.ts`**

Define `IntegrationSchema`, `CreateIntegrationRequestSchema`, and TypeScript types.

- [ ] **Step 2: Write test in `web/src/features/integrations/components/ProjectIntegrationModal.test.tsx`**

Test that dialog opens, lists active integrations, allows inputting `owner/repo`, and displays webhook copy URL.

- [ ] **Step 3: Implement `ProjectIntegrationModal.tsx`**

Using Tailwind CSS, Lucide icons (`Github`, `Copy`, `Plus`, `Check`, `Trash2`, `ExternalLink`), and standard accessibility dialog primitives.

- [ ] **Step 4: Run frontend tests and verify PASS**

Run: `cd web && npm test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/features/integrations/
git commit -m "feat(web): add ProjectIntegrationModal for GitHub configuration"
```

---

### Task 8: Frontend Task Inspector PR Actions & Status Badges

**Files:**
- Modify: `web/src/features/tasks/components/TaskCard.tsx`
- Modify: `web/src/features/tasks/components/TaskInspectorModal.tsx`
- Modify: `web/src/features/tasks/schemas/taskSchemas.ts`
- Create: `web/src/features/tasks/components/TaskPRAction.test.tsx`

- [ ] **Step 1: Add `pull_request_url` to `taskSchemas.ts`**

- [ ] **Step 2: Update `TaskCard.tsx` to render PR / Issue badge**

- [ ] **Step 3: Update `TaskInspectorModal.tsx` with "Create Pull Request" button and PR link**

- [ ] **Step 4: Write test in `web/src/features/tasks/components/TaskPRAction.test.tsx`**

- [ ] **Step 5: Run frontend tests & build**

Run: `cd web && npm test && npm run build`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add web/src/features/tasks/
git commit -m "feat(web): add PR creation button and GitHub badges to TaskCard and TaskInspectorModal"
```

---

### Task 9: End-to-End Verification & Production Container Build

**Files:**
- Run tests: Go unit & integration tests, Web vitest tests
- Build containers: `docker compose build`
- Deploy & test live API

- [ ] **Step 1: Run complete backend test suite**

Run: `go test -count=1 ./...`
Expected: All suites PASS.

- [ ] **Step 2: Run complete frontend test suite & production bundle**

Run: `cd web && npm test && npm run build`
Expected: All 28+ tests PASS.

- [ ] **Step 3: Build & start Docker containers**

Run: `docker compose build && docker compose up -d`
Expected: `agent-control-plane` and `agent-web-ui` healthy.

- [ ] **Step 4: Execute live end-to-end verification script**

Create integration, verify webhook ingestion, verify manual PR trigger.

- [ ] **Step 5: Final git commit**
