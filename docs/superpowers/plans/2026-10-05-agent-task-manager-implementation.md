# Agent Task Manager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a production-ready, self-hostable, container-sandboxed agent control plane with PostgreSQL 18, GORM v2, Redis 7 (Asynq distributed task queue), Traefik edge ingress, host Git worktree isolation, multi-agent runners (OpenCode, Claude Code, Codex), external sync (GitHub, Jira, Trello), and a standalone React 19/shadcn web dashboard.

**Architecture:** A multi-container Docker Compose topology: Traefik handles edge TLS and routing, PostgreSQL 18 provides relational persistence, Redis 7 powers the Asynq priority queue (`critical`, `default`, `low`), the Go control plane manages task orchestration and Docker sandboxing with host Git worktree isolation (no `.git` exposed to sandboxes), and a decoupled React 19 SPA dashboard connects via REST and WebSockets.

**Tech Stack:** Go 1.27, Fiber v3 (`github.com/gofiber/fiber/v3`), GORM v2 (`gorm.io/gorm`, `gorm.io/driver/postgres`), PostgreSQL 18, Redis 7, Asynq (`github.com/hibiken/asynq`), Traefik v3, Docker Go SDK (`github.com/docker/docker/client`), Go Validator v10 (`github.com/go-playground/validator/v10`), React 19, TypeScript, Vite, Tailwind CSS v4, shadcn/ui, xterm.js, CoreOS OIDC (`github.com/coreos/go-oidc/v3`).

**Spec:** `docs/superpowers/specs/2026-10-05-agent-task-manager-design.md`

## Global Constraints

- **Language & Runtime:** Go 1.27 for the backend; Node.js 22+ with Bun/Vite for frontend asset compilation.
- **API Framework & Routing:** Fiber v3 (`github.com/gofiber/fiber/v3`) exposing RESTful `/api/v1` resources and WebSockets. Request payloads are validated via Fiber v3's built-in `c.Bind().Body()` powered by `validator/v10`.
- **Database & ORM:** PostgreSQL 18 with GORM v2 (`gorm.io/gorm` and `gorm.io/driver/postgres`); connection pooling managed via `*sql.DB`. Domain enumerations are strongly typed Go idiomatic enums implementing `driver.Valuer` and `sql.Scanner`.
- **Validation & Memory Optimization:** All API boundaries, DTOs, and domain mutations are validated using `github.com/go-playground/validator/v10` with custom enum validators. Structs are ordered for optimal 64-bit memory alignment to eliminate padding holes (verified via `fieldalignment`).
- **Secrets & Cryptographic Defense:**
  - Third-party secrets (GitHub PATs, Jira tokens, OIDC client secrets) are encrypted at rest using **AES-256-GCM** with a 32-byte host master key (`SECRET_ENCRYPTION_KEY`).
  - Personal Access Tokens (PATs) for CLI/CI/Agent calls are **one-way hashed using SHA-256** before storage in `api_tokens`.
- **Ingress & TLS:** Traefik handles edge routing, automatic ACME/TLS on ports 80/443, and WebSocket connection upgrades.
- **Sandbox Security:** Agent containers run with `CapDrop: ALL`, `no-new-privileges: true`, resource limits, unprivileged user, and isolated bridge networks. The host Docker socket is mounted strictly into the control plane, never into task sandboxes.
- **Git Isolation:** Task worktrees contain source files only; parent repository `.git` metadata remains outside sandbox containers. The host controller captures staged, unstaged, and untracked changes.
- **Queueing:** Distributed priority queue powered by Redis 7 and Asynq (`critical`, `default`, `low`) with worker heartbeats, task timeouts, and bounded concurrency (`MAX_CONCURRENT_TASKS`).

## Review Focus

1. **Docker-out-of-Docker path resolution:** Task worktree mounts must resolve against `HOST_WORKTREE_ROOT` on the host, not container-internal paths, preventing sibling container bind failures.
2. **Untracked/uncommitted agent changes:** The review snapshot must capture untracked and unstaged files created by the agent, ensuring work is never lost if an agent exits without committing.
3. **Queue worker crash & lease recovery:** When a worker crashes or restarts mid-task, the heartbeat supervisor must reclaim expired leases and prevent orphaned active containers.
4. **OIDC identity stability:** Local user lookup must strictly use `(issuer, subject)` rather than email addresses, preventing account takeover or broken associations on email change.
5. **Webhook replay & deduplication:** Inbound GitHub and Jira events must verify signatures and enforce idempotency using `webhook_events`, preventing duplicate task creations.

---

## Phase 1: Project Scaffolding & Database Infrastructure

### Task 1: Project Structure, Environment & Docker Compose Setup

**Files:**
- Create: `go.mod`
- Create: `cmd/server/main.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `docker-compose.yml`
- Create: `.env.example`
- Create: `.gitignore`

**Interfaces:**
- Produces: `config.Load() (*config.Config, error)` returning strongly typed application settings.

- [ ] **Step 1: Write the failing config test**

```go
// internal/config/config_test.go
package config_test

import (
	"os"
	"testing"
	"time"

	"agent-task-manager/internal/config"
)

func TestConfigLoadDefaultsAndEnv(t *testing.T) {
	os.Setenv("PORT", "8080")
	os.Setenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/agent_tasks?sslmode=disable")
	os.Setenv("REDIS_ADDR", "localhost:6379")
	os.Setenv("REDIS_PASSWORD", "redispass123")
	os.Setenv("SECRET_ENCRYPTION_KEY", "01234567890123456789012345678901")
	os.Setenv("MAX_CONCURRENT_TASKS", "6")
	os.Setenv("HOST_WORKTREE_ROOT", "/srv/agent-tasks/worktrees")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.RedisAddr != "localhost:6379" {
		t.Errorf("expected redis addr localhost:6379, got %s", cfg.RedisAddr)
	}
	if cfg.MaxConcurrentTasks != 6 {
		t.Errorf("expected max concurrent tasks 6, got %d", cfg.MaxConcurrentTasks)
	}
	if cfg.HostWorktreeRoot != "/srv/agent-tasks/worktrees" {
		t.Errorf("expected worktree root /srv/agent-tasks/worktrees, got %s", cfg.HostWorktreeRoot)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/...`
Expected: FAIL with missing package or compilation error.

- [ ] **Step 3: Implement minimal configuration loader and base project files**

Initialize `go.mod` with module name `agent-task-manager`.
Create `internal/config/config.go` with parsing logic for database URL, Redis address and password, secret encryption key, OIDC credentials, server port, worker concurrency, and worktree paths.
Create `docker-compose.yml` defining `traefik:v3`, `postgres:18-alpine`, `redis:7-alpine`, `control-plane`, and `web-ui` services with `ingress` and `backend` networks.
Create `.env.example` documenting all configuration keys.
Create entrypoint in `cmd/server/main.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod cmd/ internal/config/ docker-compose.yml .env.example .gitignore
git commit -m "feat(scaffold): initialize project structure, config loader, and docker-compose"
```

---

### Task 2: GORM v2 PostgreSQL 18 Connection Pool, Core Relational Models & Enums

**Files:**
- Create: `internal/db/db.go`
- Create: `internal/db/db_test.go`
- Create: `internal/models/models.go`
- Create: `internal/models/enums.go`
- Create: `internal/models/validator.go`
- Create: `internal/models/models_test.go`
- Create: `internal/models/json_types.go`

**Interfaces:**
- Consumes: `config.Config` from Task 1.
- Produces: `db.Connect(cfg *config.Config) (*gorm.DB, error)`, `models.Team`, `models.User`, `models.TeamMembership`, `models.Project`, `models.ProjectIntegration`, `models.Credential`, `models.StringArray`, `models.JSONMap`, `models.Validate`, `models.TeamRole`, `models.CredentialProvider`, `models.IntegrationProvider`.

- [ ] **Step 1: Write the failing models, enums and validation test**

```go
// internal/models/models_test.go
package models_test

import (
	"testing"

	"agent-task-manager/internal/models"
)

func TestEnumsAndValidation(t *testing.T) {
	// Verify TeamRole enum
	if !models.RoleOwner.IsValid() {
		t.Errorf("expected RoleOwner to be valid")
	}
	if models.TeamRole(999).IsValid() {
		t.Errorf("expected 999 to be invalid TeamRole")
	}

	// Verify IntegrationProvider enum
	if !models.IntegrationTrello.IsValid() {
		t.Errorf("expected IntegrationTrello to be valid")
	}

	// Verify validator instance on valid team
	team := models.Team{
		ID:   "team-1",
		Name: "Core Team",
		Slug: "core-team",
	}
	if err := models.Validate.Struct(team); err != nil {
		t.Fatalf("expected valid team, got: %v", err)
	}

	// Verify validator catches invalid membership role
	mem := models.TeamMembership{
		TeamID: "team-1",
		UserID: "user-1",
		Role:   models.TeamRole(999),
	}
	if err := models.Validate.Struct(mem); err == nil {
		t.Errorf("expected validation error on invalid TeamRole, got nil")
	}
}

func TestJSONMapValueAndScan(t *testing.T) {
	config := models.JSONMap{"board_id": "trello-123", "list_id": "list-456"}
	val, err := config.Value()
	if err != nil {
		t.Fatalf("expected nil error on Value(), got %v", err)
	}

	var scanned models.JSONMap
	if err := scanned.Scan(val); err != nil {
		t.Fatalf("expected nil error on Scan(), got %v", err)
	}

	if scanned["board_id"] != "trello-123" {
		t.Errorf("unexpected scanned board_id: %v", scanned["board_id"])
	}
}

func TestStringArrayValueAndScan(t *testing.T) {
	arr := models.StringArray{"test-driven-development", "systematic-debugging"}
	val, err := arr.Value()
	if err != nil {
		t.Fatalf("expected nil error on Value(), got %v", err)
	}

	var scanned models.StringArray
	if err := scanned.Scan(val); err != nil {
		t.Fatalf("expected nil error on Scan(), got %v", err)
	}

	if len(scanned) != 2 || scanned[0] != "test-driven-development" || scanned[1] != "systematic-debugging" {
		t.Errorf("unexpected scanned value: %+v", scanned)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/models/...`
Expected: FAIL with undefined `models.RoleOwner`, `models.Validate`.

- [ ] **Step 3: Implement GORM models, Go enums, memory-optimized layout, and validator**

Implement `internal/models/enums.go` declaring `TeamRole`, `CredentialProvider`, `IntegrationProvider`, `TaskStatus`, `TaskPriority`, `AssigneeType`, `AttemptStatus`, `WebhookStatus` with typed constants, `IsValid()`, `Value()`, and `Scan()` methods.
Implement `internal/models/validator.go` initializing `github.com/go-playground/validator/v10` and registering custom enum rules (`team_role`, `task_status`, `assignee_type`, `task_priority`, `integration_provider`).
Implement `models.StringArray` and `models.JSONMap` with `driver.Valuer` and `sql.Scanner` supporting JSONB serialization.
Define `Team`, `User`, `TeamMembership`, `Project`, `ProjectIntegration`, and `Credential` structs with memory-optimized 64-bit alignment and `validate:"..."` tags.
Implement `db.Connect` in `internal/db/db.go` configuring GORM with `gorm.io/driver/postgres`, setting `MaxOpenConns`, `MaxIdleConns`, `ConnMaxLifetime`, and `ConnMaxIdleTime`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/models/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db/ internal/models/
git commit -m "feat(models): implement idiomatic Go enums, validator integration, and memory-aligned models"
```

---

## Phase 2: Authentication, Authorization & Project Scoping

### Task 3: Fiber v3 API Server, StructValidator & OIDC Authentication

**Files:**
- Create: `internal/api/server.go`
- Create: `internal/api/validator.go`
- Create: `internal/api/auth_handlers.go`
- Create: `internal/auth/oidc.go`
- Create: `internal/auth/session.go`
- Create: `internal/auth/session_test.go`

**Interfaces:**
- Consumes: `*gorm.DB`, `config.Config`, `models.Validate`.
- Produces: `api.NewServer(cfg, db) *fiber.App`, `auth.Authenticator`, `auth.SessionManager`, HTTP routes for `/api/v1/auth/login`, `/api/v1/auth/callback`, and `/api/v1/auth/logout`.

- [ ] **Step 1: Write failing session manager and Fiber v3 validation test**

```go
// internal/auth/session_test.go
package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agent-task-manager/internal/auth"
)

func TestSessionCreationAndValidation(t *testing.T) {
	sm := auth.NewSessionManager([]byte("super-secret-key-32-bytes-long!"), 24*time.Hour)
	rec := httptest.NewRecorder()

	token, err := sm.CreateSession(rec, "user-123", "github.com", "sub-456")
	if err != nil {
		t.Fatalf("unexpected error creating session: %v", err)
	}

	cookie := rec.Result().Cookies()[0]
	if cookie.Name != "atm_session" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie missing required security flags: %+v", cookie)
	}

	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.AddCookie(cookie)

	claims, err := sm.ValidateRequest(req)
	if err != nil {
		t.Fatalf("expected valid session, got: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("expected user-123, got %s", claims.UserID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/...`
Expected: FAIL.

- [ ] **Step 3: Implement Fiber v3 server with StructValidator and OIDC login handlers**

Configure `fiber.New` with `StructValidator` wrapping `validator/v10` so all `c.Bind().Body(&req)` calls automatically validate payloads.
Implement `auth.NewSessionManager` setting Secure, HttpOnly, SameSite cookies.
Implement PKCE verifier generation and CSRF state management for OIDC login flow.
Implement `/api/v1/auth/login`, `/api/v1/auth/callback`, and `/api/v1/auth/logout` Fiber v3 route handlers.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/ internal/api/
git commit -m "feat(api): implement Fiber v3 server with StructValidator and OIDC authentication"
```

---

### Task 4: Dual Auth Middleware (Session + Bearer PAT) & Team RBAC

**Files:**
- Create: `internal/auth/rbac.go`
- Create: `internal/auth/tokens.go`
- Create: `internal/auth/tokens_test.go`
- Create: `internal/api/middleware.go`
- Create: `internal/api/middleware_test.go`

**Interfaces:**
- Consumes: `auth.SessionManager`, `*gorm.DB`, `models.APIToken`.
- Produces: `middleware.RequireAuth()` supporting both session cookies and `Authorization: Bearer <token>` (SHA-256 lookup), `middleware.RequireTeamRole(minRole models.TeamRole)`.

- [ ] **Step 1: Write failing Personal Access Token (PAT) SHA-256 test**

```go
// internal/auth/tokens_test.go
package auth_test

import (
	"testing"

	"agent-task-manager/internal/auth"
)

func TestTokenGenerationAndHashing(t *testing.T) {
	rawToken, hash, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("expected nil error generating token: %v", err)
	}

	if len(rawToken) < 32 {
		t.Errorf("expected token length >= 32, got %d", len(rawToken))
	}

	expectedHash := auth.HashToken(rawToken)
	if hash != expectedHash {
		t.Errorf("expected hash %s, got %s", expectedHash, hash)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/...`
Expected: FAIL.

- [ ] **Step 3: Implement PAT hashing and Fiber v3 dual-authentication middleware**

Implement `auth.GenerateAPIToken` and `auth.HashToken` (SHA-256).
Implement Fiber v3 middleware in `internal/api/middleware.go`:
- Checks `atm_session` cookie; if absent, checks `Authorization: Bearer atm_pat_...` header.
- For Bearer tokens, hashes the incoming token with SHA-256 and queries `api_tokens` table in PostgreSQL.
- Attaches authenticated user and team context to `c.Locals()`.
- Implements `RequireTeamRole(minRole models.TeamRole)` enforcing `owner` > `admin` > `member` > `viewer`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/tokens* internal/api/middleware*
git commit -m "feat(auth): implement dual-auth middleware (session + SHA-256 PAT) and RBAC"
```

---

### Task 4B: AES-256-GCM Secret Encryption Service

**Files:**
- Create: `internal/crypto/aes.go`
- Create: `internal/crypto/aes_test.go`
- Create: `internal/api/credential_handlers.go`

**Interfaces:**
- Consumes: `config.Config.SecretEncryptionKey`.
- Produces: `crypto.SecretCipher`: `Encrypt(plaintext string) (ciphertext string, err error)`, `Decrypt(ciphertext string) (plaintext string, err error)`.

- [ ] **Step 1: Write failing AES-256-GCM encryption/decryption test**

```go
// internal/crypto/aes_test.go
package crypto_test

import (
	"testing"

	"agent-task-manager/internal/crypto"
)

func TestAES256GCMEncryptDecrypt(t *testing.T) {
	masterKey := []byte("01234567890123456789012345678901") // 32 bytes
	cipher, err := crypto.NewSecretCipher(masterKey)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	secret := "ghp_super_secret_github_personal_access_token_123"
	encrypted, err := cipher.Encrypt(secret)
	if err != nil {
		t.Fatalf("unexpected error encrypting: %v", err)
	}

	if encrypted == secret {
		t.Errorf("ciphertext must not equal plaintext")
	}

	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("unexpected error decrypting: %v", err)
	}

	if decrypted != secret {
		t.Errorf("expected decrypted %s, got %s", secret, decrypted)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/crypto/...`
Expected: FAIL.

- [ ] **Step 3: Implement AES-256-GCM authenticated cipher**

Implement `crypto.NewSecretCipher` using `crypto/aes` and `crypto/cipher.NewGCM`.
Implement `Encrypt` generating 96-bit random nonces with `crypto/rand` and returning `base64(nonce || ciphertext || tag)`.
Implement `Decrypt` unpacking the nonce, decrypting, and verifying the 128-bit authentication tag.
Wire into credential creation API handlers ensuring zero plaintext tokens in PostgreSQL.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/crypto/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/crypto/ internal/api/credential_handlers.go
git commit -m "feat(crypto): implement AES-256-GCM encryption service for credentials at rest"
```

---

## Phase 3: Transactional Task Queue & Concurrency Engine

### Task 5: Task Model & Database Schema Extensions

**Files:**
- Modify: `internal/models/models.go`
- Create: `internal/models/task_models.go`
- Create: `internal/models/task_models_test.go`

**Interfaces:**
- Produces: `models.Task`, `models.TaskAttempt`, `models.TaskLog`, `models.WebhookEvent` with priority, `AsynqTaskID`, `AttemptID` on `TaskLog`, and `ErrorMessage` on `WebhookEvent`.

- [ ] **Step 1: Write failing test verifying Task struct constraints**

```go
// internal/models/task_models_test.go
package models_test

import (
	"testing"
	"time"

	"agent-task-manager/internal/models"
)

func TestTaskInitialStateAndValidation(t *testing.T) {
	task := models.Task{
		ID:           "task-1",
		ProjectID:    "proj-1",
		Title:        "Implement feature",
		Prompt:       "Write tests and code",
		Status:       models.TaskStatusQueued,
		Priority:     models.PriorityHigh,
		AssigneeType: models.AssigneeAgent,
		AssigneeID:   "opencode",
	}

	if task.Priority != 75 {
		t.Errorf("expected PriorityHigh to be 75, got %d", task.Priority)
	}
	if task.Status != "queued" {
		t.Errorf("expected status queued, got %s", task.Status)
	}

	// Verify validator accepts valid task
	if err := models.Validate.Struct(task); err != nil {
		t.Fatalf("expected valid task, got error: %v", err)
	}

	// Verify validator catches invalid status
	invalidTask := task
	invalidTask.Status = models.TaskStatus("nonexistent_state")
	if err := models.Validate.Struct(invalidTask); err == nil {
		t.Errorf("expected validation failure on invalid TaskStatus, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/models/...`
Expected: FAIL with missing constants/fields.

- [ ] **Step 3: Implement task models with queue fields and GORM tags**

Add `Priority TaskPriority`, `AsynqTaskID *string`, `IntegrationID *string`, `HeartbeatAt *time.Time`, `Skills StringArray`, `MCPServers StringArray` to `models.Task`.
Add status constants: `TaskStatusBacklog`, `TaskStatusQueued`, `TaskStatusInProgress`, `TaskStatusReview`, `TaskStatusDone`, `TaskStatusFailed`, `TaskStatusCancelled`.
Add priority constants: `PriorityUrgent = 100`, `PriorityHigh = 75`, `PriorityNormal = 50`, `PriorityLow = 25`.
Add `models.TaskAttempt` with `CreatedAt *time.Time`.
Add `models.TaskLog` with `AttemptID *string`.
Add `models.WebhookEvent` with `ErrorMessage *string`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/models/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/models/task* internal/models/models.go
git commit -m "feat(models): define Task, TaskAttempt, TaskLog, and WebhookEvent models with queue metadata"
```

---

### Task 6: Distributed Task Queue Engine (Redis 7 & Asynq Client)

**Files:**
- Create: `internal/queue/types.go`
- Create: `internal/queue/client.go`
- Create: `internal/queue/client_test.go`

**Interfaces:**
- Consumes: `config.Config`, `models.Task`.
- Produces: `queue.Client`: `EnqueueTask(ctx, task *models.Task) (asynqTaskID string, err error)`, `CancelTask(ctx, queueName string, asynqTaskID string) error`.

- [ ] **Step 1: Write failing Asynq payload and priority tier test**

```go
// internal/queue/client_test.go
package queue_test

import (
	"testing"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"
)

func TestQueuePriorityTiers(t *testing.T) {
	cases := []struct {
		priority models.TaskPriority
		expected string
	}{
		{models.PriorityUrgent, "critical"},
		{models.PriorityHigh, "critical"},
		{models.PriorityNormal, "default"},
		{models.PriorityLow, "low"},
	}

	for _, tc := range cases {
		tier := queue.ResolveQueueTier(tc.priority)
		if tier != tc.expected {
			t.Errorf("for priority %d expected queue %s, got %s", tc.priority, tc.expected, tier)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/queue/...`
Expected: FAIL.

- [ ] **Step 3: Implement Asynq client and priority routing**

Implement `internal/queue/types.go` with `TypeExecuteAgentTask = "task:execute"` and payload `TaskExecutionPayload{TaskID string, ProjectID string}`.
Implement `queue.ResolveQueueTier`:
- `PriorityUrgent (100)` and `PriorityHigh (75)` &rarr; `"critical"`
- `PriorityNormal (50)` &rarr; `"default"`
- `PriorityLow (25)` &rarr; `"low"`
Implement `queue.NewClient` wrapping `*asynq.Client` and `*asynq.Inspector`.
Implement `EnqueueTask` returning the generated Asynq task ID and saving it to PostgreSQL `tasks.asynq_task_id`.
Implement `CancelTask` invoking `asynqInspector.DeleteTask` to prevent pending jobs from executing.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/queue/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/queue/
git commit -m "feat(queue): implement Redis 7 Asynq task dispatcher with priority tiers"
```

---

### Task 7: Concurrency-Bounded Asynq Worker Server & Execution Handler

**Files:**
- Create: `internal/queue/worker.go`
- Create: `internal/queue/worker_test.go`

**Interfaces:**
- Consumes: `*gorm.DB`, `config.Config`, `queue.Client`.
- Produces: `queue.WorkerServer`: `Start() error`, `Stop()`.

- [ ] **Step 1: Write failing Asynq worker server configuration test**

```go
// internal/queue/worker_test.go
package queue_test

import (
	"testing"

	"agent-task-manager/internal/config"
	"agent-task-manager/internal/queue"
)

func TestWorkerServerConfig(t *testing.T) {
	cfg := &config.Config{
		RedisAddr:          "localhost:6379",
		MaxConcurrentTasks: 4,
	}

	srv := queue.NewWorkerServer(cfg, nil, nil)
	if srv == nil {
		t.Fatalf("expected non-nil worker server")
	}
	if srv.Concurrency() != 4 {
		t.Errorf("expected concurrency 4, got %d", srv.Concurrency())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/queue/...`
Expected: FAIL.

- [ ] **Step 3: Implement Asynq worker server and execution dispatcher**

Configure `asynq.NewServer` with:
- `Concurrency: cfg.MaxConcurrentTasks`
- `Queues: map[string]int{"critical": 6, "default": 3, "low": 1}` (60/30/10 priority distribution)
Implement task execution handler:
- Decodes `TaskExecutionPayload`.
- Updates PostgreSQL task state to `in_progress`.
- Creates `task_attempts` record.
- Dispatches execution to the runner/sandbox engine.
- On completion or failure, updates task status and attempt record.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/queue/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/queue/worker*
git commit -m "feat(queue): implement concurrency-bounded Asynq worker server and task handler"
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/queue/worker_pool* internal/queue/supervisor*
git commit -m "feat(queue): implement bounded worker pool dispatcher and lease supervisor"
```

---

## Phase 4: Git Worktree Isolation & Diff Snapshot Engine

### Task 8: Host Git Worktree Manager & Host Path Translator

**Files:**
- Create: `internal/git/worktree.go`
- Create: `internal/git/worktree_test.go`

**Interfaces:**
- Consumes: `config.Config`.
- Produces: `worktree.Manager`: `CreateWorktree(repoPath, taskID, branch) (hostPath, containerBindPath string, err error)`, `RemoveWorktree(repoPath, taskID) error`.

- [ ] **Step 1: Write failing worktree path translation test**

```go
// internal/git/worktree_test.go
package git_test

import (
	"testing"

	"agent-task-manager/internal/git"
)

func TestPathTranslationForDockerSiblingBinds(t *testing.T) {
	hostWorktreeRoot := "/srv/agent-tasks/worktrees"
	wm := git.NewWorktreeManager(hostWorktreeRoot)

	hostPath, bindPath := wm.ResolvePaths("task-xyz-123")
	expectedHost := "/srv/agent-tasks/worktrees/task-xyz-123"

	if hostPath != expectedHost {
		t.Errorf("expected host path %s, got %s", expectedHost, hostPath)
	}
	if bindPath != expectedHost+":/workspace:rw" {
		t.Errorf("expected bind spec %s:/workspace:rw, got %s", expectedHost, bindPath)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/...`
Expected: FAIL.

- [ ] **Step 3: Implement Git worktree lifecycle and path resolver**

Implement `git worktree add <dir> -b task/<id>-<slug>` execution using `os/exec`.
Implement Docker-out-of-Docker host path translation using `HOST_WORKTREE_ROOT`.
Implement safe cleanup removing worktrees and pruning worktree metadata.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/worktree*
git commit -m "feat(git): implement host worktree manager with DinD path translation"
```

---

### Task 9: Review Snapshot Engine (Staged, Unstaged & Untracked Diffs)

**Files:**
- Create: `internal/git/diff.go`
- Create: `internal/git/diff_test.go`

**Interfaces:**
- Produces: `diff.CaptureSnapshot(worktreePath, baseBranch) (*diff.Result, error)`.

- [ ] **Step 1: Write failing test for diff capture**

```go
// internal/git/diff_test.go
package git_test

import (
	"testing"

	"agent-task-manager/internal/git"
)

func TestDiffParserWithUntrackedFiles(t *testing.T) {
	rawDiff := `diff --git a/newfile.go b/newfile.go
new file mode 100644
--- /dev/null
+++ b/newfile.go
@@ -0,0 +1,5 @@
+package main
+func Hello() string { return "world" }
`
	files, err := git.ParseDiffSummary(rawDiff)
	if err != nil {
		t.Fatalf("unexpected error parsing diff: %v", err)
	}
	if len(files) != 1 || files[0].Path != "newfile.go" || files[0].Additions != 2 {
		t.Errorf("unexpected diff summary: %+v", files)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/...`
Expected: FAIL.

- [ ] **Step 3: Implement diff capture engine**

Execute `git status --porcelain` in the worktree.
If untracked or unstaged files exist, stage them with `git add -A` and create a local review commit on the task branch.
Compute `git diff <baseBranch>...HEAD` and parse changed file statistics.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/diff*
git commit -m "feat(git): implement review snapshot engine capturing untracked and uncommitted diffs"
```

---

## Phase 5: Docker Container Sandbox & Agent Runners

### Task 10: Docker Sandbox Runner (Security Hardened)

**Files:**
- Create: `internal/sandbox/docker.go`
- Create: `internal/sandbox/docker_test.go`
- Create: `internal/sandbox/types.go`

**Interfaces:**
- Consumes: Docker client (`github.com/docker/docker/client`).
- Produces: `sandbox.Runner`: `Start(ctx, cfg SandboxConfig) (*ContainerInstance, error)`, `Stop(ctx, containerID) error`.

- [ ] **Step 1: Write failing container config security validation test**

```go
// internal/sandbox/docker_test.go
package sandbox_test

import (
	"testing"

	"agent-task-manager/internal/sandbox"
)

func TestSecurityHardeningDefaults(t *testing.T) {
	cfg := sandbox.NewDefaultHostConfig("/srv/agent-tasks/worktrees/task-1")

	if len(cfg.CapDrop) != 1 || cfg.CapDrop[0] != "ALL" {
		t.Errorf("expected CapDrop ALL, got: %v", cfg.CapDrop)
	}
	if len(cfg.SecurityOpt) != 1 || cfg.SecurityOpt[0] != "no-new-privileges:true" {
		t.Errorf("expected no-new-privileges:true, got: %v", cfg.SecurityOpt)
	}
	if cfg.NetworkMode != "bridge" {
		t.Errorf("expected isolated network bridge, got: %s", cfg.NetworkMode)
	}
	if cfg.Resources.Memory != 4*1024*1024*1024 {
		t.Errorf("expected 4GB memory limit, got: %d", cfg.Resources.Memory)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sandbox/...`
Expected: FAIL.

- [ ] **Step 3: Implement Docker SDK sandbox controller**

Implement `sandbox.NewDefaultHostConfig` setting `CapDrop: ALL`, `no-new-privileges:true`, 4GB RAM, 2 CPUs.
Implement `ContainerCreate`, `ContainerStart`, `ContainerWait`, and `ContainerLogs` streaming stdout/stderr frames.
Ensure `.git` metadata from the host repo is strictly omitted from bind mounts.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sandbox/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/
git commit -m "feat(sandbox): implement security-hardened Docker container runner"
```

---

### Task 11: Multi-Agent Runners & Configuration Injection

**Files:**
- Create: `internal/runner/runner.go`
- Create: `internal/runner/opencode.go`
- Create: `internal/runner/claude.go`
- Create: `internal/runner/codex.go`
- Create: `internal/runner/smcp.go`
- Create: `internal/runner/runner_test.go`

**Interfaces:**
- Consumes: `models.Task`, `sandbox.Runner`.
- Produces: `runner.AgentRunner`: `GenerateContainerConfig(task *models.Task) (*container.Config, error)`.

- [ ] **Step 1: Write failing runner command generation test**

```go
// internal/runner/runner_test.go
package runner_test

import (
	"testing"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/runner"
)

func TestOpenCodeRunnerCommand(t *testing.T) {
	r := runner.NewOpenCodeRunner()
	task := &models.Task{
		Prompt: "Implement unit tests",
		Model:  ptr("gemini-3.8-flash"),
	}

	cmd := r.BuildCommand(task)
	expected := []string{"opencode", "run", "Implement unit tests", "--model", "gemini-3.8-flash"}

	if len(cmd) != len(expected) || cmd[2] != expected[2] {
		t.Errorf("unexpected command: %v", cmd)
	}
}

func ptr(s string) *string { return &s }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runner/...`
Expected: FAIL.

- [ ] **Step 3: Implement OpenCode, Claude Code, Codex runners and smcp injector**

Implement OpenCode runner CLI flags.
Implement Claude Code runner flags (enforcing permission checks; blocking `--dangerously-skip-permissions` without explicit config).
Implement Codex non-interactive command generator.
Implement `smcp.InjectSandboxedConfig` writing sanitized `opencode.json` and `.claude/config.json` inside the task worktree before container launch.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runner/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runner/
git commit -m "feat(runner): implement OpenCode, Claude Code, Codex runners and smcp config injection"
```

---

## Phase 6: External Synchronization (GitHub, Jira & Trello)

### Task 12: SyncProvider Architecture, Provider Registry & Webhook Idempotency

**Files:**
- Create: `internal/integrations/provider.go`
- Create: `internal/integrations/registry.go`
- Create: `internal/integrations/idempotency.go`
- Create: `internal/integrations/idempotency_test.go`

**Interfaces:**
- Consumes: `*gorm.DB`, `models.ProjectIntegration`.
- Produces: `integrations.SyncProvider`, `integrations.PullRequestProvider`, `integrations.Registry`, `integrations.IdempotencyStore`.

- [ ] **Step 1: Write failing idempotency store test**

```go
// internal/integrations/idempotency_test.go
package integrations_test

import (
	"context"
	"testing"

	"agent-task-manager/internal/integrations"
)

func TestEventDeduplication(t *testing.T) {
	store := integrations.NewMemoryIdempotencyStore()
	ctx := context.Background()

	firstSeen, err := store.RecordEvent(ctx, "github", "proj-1", "evt-12345")
	if err != nil || !firstSeen {
		t.Fatalf("expected first event to be recorded, got seen=%v, err=%v", firstSeen, err)
	}

	secondSeen, err := store.RecordEvent(ctx, "github", "proj-1", "evt-12345")
	if err != nil || secondSeen {
		t.Fatalf("expected duplicate event to return seen=false, got seen=%v", secondSeen)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/integrations/...`
Expected: FAIL.

- [ ] **Step 3: Implement Sync interfaces, registry, and WebhookEvent deduplicator**

Define `SyncProvider` (`FetchIssues`, `UpdateIssueStatus`, `AddComment`), `PullRequestProvider` (`CreatePullRequest`), and `Registry` mapping `models.IntegrationProvider` strings to implementations.
Implement `RecordEvent` writing to PostgreSQL `webhook_events` table; duplicate `(provider, project_id, provider_event_id)` queries return duplicate error, halting redundant task processing.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/integrations/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/integrations/provider.go internal/integrations/registry.go internal/integrations/idempotency*
git commit -m "feat(integrations): implement SyncProvider contracts, provider registry, and webhook idempotency store"
```

---

### Task 13: GitHub, Jira & Trello Providers Implementation

**Files:**
- Create: `internal/integrations/github/provider.go`
- Create: `internal/integrations/github/provider_test.go`
- Create: `internal/integrations/jira/provider.go`
- Create: `internal/integrations/jira/provider_test.go`
- Create: `internal/integrations/trello/provider.go`
- Create: `internal/api/webhook_handlers.go`

**Interfaces:**
- Consumes: `integrations.SyncProvider`, `models.ProjectIntegration`, `models.Credential`, `crypto.SecretCipher`.
- Produces: GitHub, Jira, and Trello provider implementations, webhook route handlers for `/api/v1/webhooks/:provider`.

- [ ] **Step 1: Write failing GitHub webhook HMAC signature verification test**

```go
// internal/integrations/github/provider_test.go
package github_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"agent-task-manager/internal/integrations/github"
)

func TestGitHubHMACVerification(t *testing.T) {
	secret := "webhook-secret-token"
	body := []byte(`{"action":"opened","issue":{"number":42,"title":"Fix bug"}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !github.VerifySignature(secret, body, validSig) {
		t.Errorf("expected signature to verify successfully")
	}
	if github.VerifySignature(secret, body, "sha256=invalid") {
		t.Errorf("expected invalid signature to fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/integrations/...`
Expected: FAIL.

- [ ] **Step 3: Implement GitHub, Jira, and Trello providers with decrypted credentials**

Implement GitHub HMAC-SHA256 signature verification, issue polling, task creation/deduplication, PR creation, and label transitions (`in-progress:agent`).
Implement Jira Cloud REST API integration, JQL query parsing, and issue transition to In Progress and In Review with diff comments.
Implement Trello provider card fetching and list transitions using `project_integrations.config`.
Wire up HTTP webhook handlers in `internal/api/webhook_handlers.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/integrations/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/integrations/ internal/api/webhook_handlers.go
git commit -m "feat(integrations): implement GitHub, Jira, and Trello providers with HMAC verification"
```

---

## Phase 7: Real-Time Event Hub & WebSockets

### Task 14: WebSocket Hub & Live Log Streaming

**Files:**
- Create: `internal/ws/hub.go`
- Create: `internal/ws/hub_test.go`
- Create: `internal/api/ws_handler.go`

**Interfaces:**
- Consumes: `auth.SessionManager`.
- Produces: `ws.Hub`: `BroadcastTaskLog(taskID, chunk)`, `BroadcastTaskState(taskID, status)`, `/api/ws/terminal/:taskID` endpoint.

- [ ] **Step 1: Write failing WebSocket room subscription test**

```go
// internal/ws/hub_test.go
package ws_test

import (
	"testing"

	"agent-task-manager/internal/ws"
)

func TestHubSubscriptionAndBroadcast(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()

	client := ws.NewMockClient("client-1", "task-42")
	hub.Register(client)

	hub.BroadcastToTask("task-42", []byte("terminal-output-chunk"))

	select {
	case msg := <-client.Messages():
		if string(msg) != "terminal-output-chunk" {
			t.Errorf("expected terminal-output-chunk, got %s", string(msg))
		}
	default:
		t.Errorf("client did not receive broadcast message")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ws/...`
Expected: FAIL.

- [ ] **Step 3: Implement WebSocket hub and HTTP upgrader**

Implement `ws.Hub` with client registration, unregistration, and task-scoped channel broadcasting.
Implement `/api/ws/terminal/:taskID` verifying authenticated session and team permissions before upgrading to WebSocket.
Multiplex container stdout/stderr through the Hub and persist to `models.TaskLog`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ws/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ws/ internal/api/ws_handler.go
git commit -m "feat(ws): implement real-time WebSocket hub for xterm.js streaming and task events"
```

---

## Phase 8: Decoupled React 19 Frontend

### Task 15: Frontend Project Scaffolding, Typed API Client & Container Packaging

**Files:**
- Create: `web/package.json`
- Create: `web/vite.config.ts`
- Create: `web/src/App.tsx`
- Create: `web/src/index.css`
- Create: `web/src/lib/api.ts`
- Create: `web/Dockerfile`
- Create: `web/nginx.conf`

**Interfaces:**
- Consumes: REST endpoints (`/api/v1/auth/me`, `/api/v1/projects`).
- Produces: Standalone React 19 SPA build and Nginx-based production web container image.

- [ ] **Step 1: Write failing API client test**

```ts
// web/src/lib/api.test.ts
import { describe, it, expect, vi } from 'vitest';
import { apiClient } from './api';

describe('apiClient', () => {
  it('attaches credentials and parses json response', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [{ id: 'team-1', name: 'Core Team' }],
    } as Response);

    const teams = await apiClient.getTeams();
    expect(teams).toHaveLength(1);
    expect(teams[0].name).toBe('Core Team');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npm test`
Expected: FAIL.

- [ ] **Step 3: Initialize React 19 Vite project, typed client, and Dockerfile**

Initialize React 19, TypeScript, Tailwind v4 in `web/`.
Implement `web/src/lib/api.ts` exposing typed methods for all Fiber v3 `/api/v1` endpoints.
Create `web/Dockerfile` with multi-stage build: compiles Vite assets in Node/Bun stage and copies static files into `nginx:alpine`.
Create `web/nginx.conf` with SPA `try_files $uri /index.html` fallback.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npm test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/
git commit -m "feat(frontend): scaffold decoupled React 19 Vite application with typed API client and Dockerfile"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/server/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/ cmd/server/embed*
git commit -m "feat(frontend): scaffold Vite React 19 application and Go embed.FS asset server"
```

---

### Task 16: Kanban Board, Terminal & Diff Viewer UI Components

**Files:**
- Create: `web/src/components/KanbanBoard.tsx`
- Create: `web/src/components/TerminalView.tsx`
- Create: `web/src/components/DiffViewer.tsx`
- Create: `web/src/components/TaskModal.tsx`
- Create: `web/src/components/TeamSwitcher.tsx`
- Create: `web/src/lib/api.ts`

**Interfaces:**
- Consumes: REST endpoints (`/api/tasks`, `/api/projects`, `/api/teams`), WebSocket (`/api/ws/terminal/:id`).

- [ ] **Step 1: Write frontend component unit test for Kanban lane categorization**

```tsx
// web/src/components/KanbanBoard.test.tsx
import { render, screen } from '@testing-library/react';
import { KanbanBoard } from './KanbanBoard';

test('renders 4 board lanes: Backlog, Queued & In Progress, Review, Done', () => {
  render(<KanbanBoard tasks={[]} onMoveTask={() => {}} />);
  expect(screen.getByText(/Backlog/i)).toBeInTheDocument();
  expect(screen.getByText(/Queued & In Progress/i)).toBeInTheDocument();
  expect(screen.getByText(/Review/i)).toBeInTheDocument();
  expect(screen.getByText(/Done/i)).toBeInTheDocument();
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npm test` (or bun test)
Expected: FAIL.

- [ ] **Step 3: Implement dashboard UI components**

Implement `KanbanBoard` with 4 lanes and task card badges (GitHub issue #, Jira key, agent assignee, priority badge).
Implement `TerminalView` using `@xterm/xterm` and `@xterm/addon-fit` connecting to `/api/ws/terminal/:taskID`.
Implement `DiffViewer` rendering unified diffs with syntax highlighting.
Implement `TaskModal` for creating tasks with skill and MCP server selection badges.
Implement `TeamSwitcher` header component.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npm test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/
git commit -m "feat(frontend): implement Kanban board, live xterm.js terminal, and diff viewer"
```

---

## Phase 9: Self-Hosted Traefik Ingress & End-to-End Packaging

### Task 17: Traefik Ingress Configuration, Packaging & E2E Validation

**Files:**
- Create: `deploy/traefik/traefik.yml`
- Create: `deploy/traefik/dynamic.yml`
- Create: `Dockerfile`
- Create: `scripts/backup-db.sh`
- Create: `scripts/restore-db.sh`
- Create: `tests/e2e/e2e_test.go`

**Interfaces:**
- Consumes: Docker Compose topology from Task 1, full Go binary and web assets.
- Produces: Self-hosted production deployment artifact and end-to-end test suite.

- [ ] **Step 1: Write integration test verifying Traefik routing headers and WebSocket upgrade**

```go
// tests/e2e/e2e_test.go
package e2e_test

import (
	"net/http"
	"testing"
)

func TestSecurityHeadersConfig(t *testing.T) {
	// Verifies Traefik security header policy
	headers := http.Header{}
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("X-Frame-Options", "DENY")

	if headers.Get("X-Frame-Options") != "DENY" {
		t.Errorf("expected X-Frame-Options DENY")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tests/e2e/...`
Expected: FAIL if package doesn't exist.

- [ ] **Step 3: Configure Traefik ingress, server Dockerfile, and backup scripts**

Create `deploy/traefik/traefik.yml` configuring entrypoints (`web` on 80, `websecure` on 443), certificate resolvers, and Docker provider.
Create `deploy/traefik/dynamic.yml` with security headers and WebSocket rate limits.
Create production `Dockerfile` for Go control plane: Stage 1 builds Go binary with Go 1.27; Stage 2 minimal alpine runtime with Docker CLI and Git.
Create `scripts/backup-db.sh` (`pg_dump`) and `scripts/restore-db.sh` (`pg_restore`).
Implement `tests/e2e/e2e_test.go` running Docker Compose stack (Traefik, Postgres 18, Redis 7, Control Plane, Web UI) and testing task creation, Asynq queue dispatch, container sandbox execution, and WebSocket ANSI output streaming.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./tests/e2e/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add deploy/ Dockerfile scripts/ tests/e2e/
git commit -m "feat(deploy): configure Traefik ingress, production Dockerfile, and backup scripts"
```

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-10-05-agent-task-manager-implementation.md`. Please review the plan. Which execution approach would you prefer?

- **Subagent-driven** (Recommended) — A fresh subagent implements each task and a fresh reviewer checks it before the next one starts, then a whole-branch review at the end. Most thorough; catches interface and security defects at every task boundary.
- **Native** — I implement every task myself in this session, the way this harness runs work, then one fresh reviewer checks the whole branch at the end. Fastest and lowest token overhead.
