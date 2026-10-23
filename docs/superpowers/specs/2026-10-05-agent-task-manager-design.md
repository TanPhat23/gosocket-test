# Agent Task Manager with Container Sandbox, smcp Sync, Teams & GitHub/Jira Board Sync
## Architectural Design Specification

**Date:** 2026-10-05
**Status:** Revised Draft — pending implementation approval
**Target Project:** `agent-task-manager` (`/home/ghug/Code/Go/agent-task-manager`)

---

## 1. Executive Summary & Goals

### 1.1 Overview
The **Agent Task Manager** is a high-performance, container-sandboxed control plane designed for collaborative engineering teams delegating tasks to AI coding agents (OpenCode, Claude Code, OpenAI Codex).

It combines six core operational pillars:
1. **Team & Workspace Management**: Multiple teams, projects, and human/agent assignees on unified boards.
2. **Bidirectional Issue Tracker Sync (GitHub & Jira)**: Ingests issues from GitHub and Jira, delegates them to AI agents, and automatically opens Pull Requests and transitions ticket statuses upon completion.
3. **Concurrency Control & Redis Distributed Task Queue (Asynq)**: Implements an asynchronous task queue powered by Redis 7 and `github.com/hibiken/asynq`, supporting priority scheduling (`critical`, `default`, `low`), automatic retries, task timeouts, and a concurrency-bounded worker pool (`MAX_CONCURRENT_TASKS`) to prevent host CPU/OOM exhaustion.
4. **Host-Level Git Worktree Isolation**: Spawns isolated Git worktrees per task so agents can work in parallel on the same repository without local file conflicts or mounting `.git` internals into execution sandboxes.
5. **Docker Container Sandboxing**: Traps agent tool executions and MCP servers inside an unprivileged Docker container (`CapDrop: ALL`, `no-new-privileges`), protecting the host machine from unintended edits, malicious commands, or credential tampering.
6. **API-First Engine & Self-Hosting Architecture (Go 1.27 + Fiber v3)**: Built with Go 1.27 using **Fiber v3 (`github.com/gofiber/fiber/v3`)** with built-in request validation (`c.Bind().Body()`) and the official Docker Go SDK. Exposes first-class REST APIs (`/api/v1`), WebSockets, and OpenAPI 3.1 documentation for external CLIs, CI/CD, and agent callers. Packaged as a clean 4-container stack orchestrated via Docker Compose: Traefik reverse proxy/ingress (automatic ACME/TLS, WebSocket passthrough), PostgreSQL 18 relational persistence managed via GORM v2, Redis 7 for queue management via Asynq, and a single Go 1.27 control plane container embedding the React/shadcn/ui frontend via `embed.FS`.
7. **Agent Ecosystem & `smcp` Synchronization**: Optionally interoperates with `@tanphat/smcp-core` or a configured `smcp` service/CLI to discover, sanitize, and inject skills and MCP servers dynamically into task environments. Production deployments must configure this dependency explicitly; the implementation must not rely on a hard-coded home-directory path.

### 1.2 Normative decisions

The following decisions are part of this specification and are not implementation suggestions:

* **Authentication** uses OAuth 2.1 Authorization Code with PKCE through an OpenID Connect (OIDC) provider. The deployment may use Google, GitHub, Microsoft Entra ID, Keycloak, Auth0, or another standards-compliant provider.
* **Database & Persistence** uses PostgreSQL 18 managed through GORM v2 (`gorm.io/gorm` and `gorm.io/driver/postgres` backed by pgx/database/sql). Database schema updates are executed via structured migrations and connection pooling is managed via `*sql.DB`.
* **API Engine & Validation** is built on Fiber v3 (`github.com/gofiber/fiber/v3`) exposing RESTful `/api/v1` endpoints and WebSockets. Incoming requests use Fiber v3's built-in `StructValidator` powered by `go-playground/validator/v10` via `c.Bind().Body()`, validating payload schemas and Go enum types before handler execution. Programmatic clients (CLI, CI/CD, agent runners) authenticate via hashed Personal Access Tokens (PATs) in `Authorization: Bearer` headers.
* **Reverse Proxy & Ingress** is fronted by Traefik, handling TLS termination via Let's Encrypt (ACME), HTTP/2, and persistent WebSocket connections required for xterm.js terminal streaming.
* **Sessions** are created by the Go control plane after validating the OIDC ID token. The browser receives only a Secure, HttpOnly, SameSite cookie containing an opaque session identifier. Access tokens are not stored in PostgreSQL unless a specific provider integration requires one.
* **Concurrency & Queueing** uses a distributed task queue powered by Redis 7 and Asynq (`github.com/hibiken/asynq`). Concurrency is bounded by a fixed worker pool (`MAX_CONCURRENT_TASKS`), scheduling jobs across priority queues (`critical`, `default`, `low`), supporting automatic exponential retries, task timeouts, and cancellations. Stalled workers and orphaned tasks are reclaimed automatically by Asynq's built-in heartbeat and lease management.
* **Authorization** is enforced locally through users, team memberships, project scope, and roles. Successful identity authentication alone never grants access to a project or task.
* **Git operations** are controller-owned. The sandbox edits the mounted task worktree, but does not require access to the host repository's `.git` metadata. The controller captures, reviews, commits, merges, and removes worktrees on the host.
* **External synchronization** is idempotent. Every inbound event is keyed by provider, project, external issue ID, and provider event ID where available.
* **Secrets Management & Cryptographic Defense**:
  - **External Provider Secrets (GitHub PAT, Jira API Token, OIDC client secrets)** are encrypted at rest using **AES-256-GCM** (Authenticated Encryption with Associated Data). Ciphertexts are sealed with unique 96-bit nonces and 128-bit integrity tags using a host-managed 32-byte master key (`SECRET_ENCRYPTION_KEY`). Raw secret values are never persisted in plaintext.
  - **Internal Personal Access Tokens (PATs)** for CLI, CI/CD, and agent callers are **one-way hashed using SHA-256** before storage in `api_tokens`. The raw token is displayed to the user only once upon creation and cannot be recovered from the database.
  - Runtime secrets are injected only into the specific containers or processes requiring them and are redacted from all task logs, WebSocket streams, and API error bodies.

---

## 2. System Architecture

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                                External Integrations                                   │
│            ┌────────────────────────────┐       ┌────────────────────────────┐         │
│            │   GitHub Issues & PRs      │       │      Jira Cloud Board      │         │
│            └─────────────┬──────────────┘       └─────────────┬──────────────┘         │
└──────────────────────────┼────────────────────────────────────┼────────────────────────┘
                           │ Webhook / REST                     │ Webhook / REST
                           ▼                                    ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                    Traefik Reverse Proxy & Edge Ingress (Ports 80/443)                 │
│         - Auto ACME / Let's Encrypt TLS Termination, HTTP/2, WebSocket Passthrough     │
└──────────────────────────────────────────┬─────────────────────────────────────────────┘
                                           │ Public Ingress Bridge Network
                                           ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                        Go Control Plane Container (Single Binary)                      │
│                                                                                        │
│  ┌───────────────────────┐   ┌──────────────────────────────────────────────────────┐  │
│  │   Embedded Web UI     │   │               Go Control Plane Logic                 │  │
│  │ (React 19 + shadcn/ui │ ◀─┤ - OIDC Authentication & Team RBAC                    │  │
│  │  Tailwind v4 + Vite)  │   │ - Team & Project Management Layer                    │  │
│  └───────────▲───────────┘   │ - GitHub & Jira Bi-directional Sync                  │  │
│              │               │ - Task State Machine & Worktree Engine               │  │
│              │               │ - WebSocket Hub (xterm.js streaming)                 │  │
│              │               │ - Dynamic smcp Skill/MCP Bridge                      │  │
│              │               ├──────────────────────────────────────────────────────┤  │
│              │               │   Asynq Queue Engine & Bounded Worker Pool           │  │
│              │               │ - Redis-backed Distributed Task Dispatcher           │  │
│              │               │ - Priority Queues (critical > default > low)         │  │
│              │               │ - Automatic Retries, Timeouts & Dead-Letter Handling │  │
│              │               └──────────┬────────────────────────────┬──────────────┘  │
└──────────────┼──────────────────────────┼────────────────────────────┼─────────────────┘
               │                          │ Internal Backend Network   │
               │                          ▼                            ▼
               │             ┌────────────────────────────┐  ┌────────────────────────────┐
               │             │ PostgreSQL 18 DB Store     │  │ Redis 7 Task Queue Store   │
               │             │ - Relational Data & JSONB  │  │ - Asynq Task Queues & AOF  │
               │             │ - Volume: postgres_data    │  │ - Volume: redis_data       │
               │             └────────────────────────────┘  └────────────────────────────┘
               │                                                       │
               │ Host Docker Socket (/var/run/docker.sock)             │
               └──────────────────────────┬────────────────────────────┘
                                          ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                 Host Docker Daemon -> Task Sandbox Containers                          │
│                                                                                        │
│  ┌──────────────────────────────────────────────────────────────────────────────────┐  │
│  │ Task Container Sandbox (Per Active Worker / Claimed Task, Bounded Concurrency)   │  │
│  │  - Mount: /workspace (RW to host task worktree, no .git mounted)                 │  │
│  │  - Agent: OpenCode / Claude Code / Codex CLI                                     │  │
│  │  - Security: CapDrop ALL, no-new-privileges, unprivileged UID/GID                │  │
│  │  - Network: Isolated bridge network (no access to internal control plane/DB)    │  │
│  └──────────────────────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

### 2.1 Repository & Backend Code Structure

The project follows standard Go package layout conventions with strict separation of concerns:

```text
agent-task-manager/
├── cmd/
│   └── server/
│       ├── main.go                     # Entrypoint: config loading, DI, Fiber v3 startup, graceful shutdown
│       └── embed.go                    # Go embed.FS embedding compiled React dashboard assets
├── internal/
│   ├── api/                            # HTTP / REST Presentation Layer (Fiber v3)
│   │   ├── server.go                   # Fiber app initialization, middleware registration, route grouping
│   │   ├── validator.go                # Fiber v3 StructValidator integration with go-playground/validator/v10
│   │   ├── middleware.go               # Session cookie & SHA-256 Bearer PAT auth, RBAC enforcement, CORS, recovery
│   │   ├── auth_handlers.go            # /api/v1/auth/login, /callback (PKCE), /logout, /me
│   │   ├── project_handlers.go         # /api/v1/projects CRUD, repo path verification
│   │   ├── integration_handlers.go     # /api/v1/projects/:id/integrations (GitHub, Jira, Trello)
│   │   ├── credential_handlers.go      # /api/v1/credentials (encrypted storage, secret creation)
│   │   ├── task_handlers.go            # /api/v1/tasks CRUD, Kanban column moves, cancel, retry
│   │   ├── webhook_handlers.go         # /api/v1/webhooks/:provider (signature verification, event ingestion)
│   │   └── ws_handler.go               # /api/v1/ws/tasks/:id/logs (WebSocket upgrade & live streaming)
│   ├── auth/                           # Authentication & Authorization Domain
│   │   ├── oidc.go                     # OIDC discovery, code exchange, PKCE verification, ID token validation
│   │   ├── session.go                  # Secure, HttpOnly, SameSite server session manager
│   │   ├── tokens.go                   # Personal Access Token (PAT) generation and SHA-256 hashing
│   │   └── rbac.go                     # Team role hierarchy evaluator (owner > admin > member > viewer)
│   ├── config/                         # Configuration Management
│   │   └── config.go                   # Environment parsing, defaults, validation
│   ├── crypto/                         # Service-Level Cryptographic Defense
│   │   └── aes.go                      # AES-256-GCM cipher service (96-bit random nonce, 128-bit tag, base64)
│   ├── db/                             # Persistence & Relational Infrastructure
│   │   └── db.go                       # GORM v2 initialization, PostgreSQL 18 driver, sql.DB pool tuning
│   ├── models/                         # Domain Entities, Enums & Struct Alignment
│   │   ├── enums.go                    # Typed Go enums (TeamRole, TaskStatus, TaskPriority, IntegrationProvider)
│   │   ├── models.go                   # Core entities (Team, User, TeamMembership, APIToken, Credential)
│   │   ├── project.go                  # Project & ProjectIntegration entities
│   │   ├── task_models.go              # Task, TaskAttempt, TaskLog, WebhookEvent
│   │   ├── json_types.go               # StringArray and JSONMap (driver.Valuer & sql.Scanner)
│   │   └── validator.go                # Validator v10 registration of custom enum validation rules
│   ├── queue/                          # Distributed Queue Engine (Redis + Asynq)
│   │   ├── client.go                   # Asynq client wrapper: Enqueue, priority tiers (critical, default, low)
│   │   ├── worker.go                   # Asynq server & handler: task dispatch, lease heartbeats, cancellation
│   │   └── types.go                    # Asynq task types and JSON execution payloads
│   ├── git/                            # Host Git & Worktree Lifecycle
│   │   ├── worktree.go                 # Git worktree add/prune, branch isolation, DinD path translator
│   │   └── diff.go                     # Snapshot review engine: staged, unstaged, and untracked file diffs
│   ├── sandbox/                        # Container Sandboxing Engine
│   │   ├── docker.go                   # Docker Go SDK client: container create/start/wait, CapDrop ALL, memory limits
│   │   └── types.go                    # Container execution specifications and environment definitions
│   ├── runner/                         # AI Agent Runner Implementations
│   │   ├── runner.go                   # Common AgentRunner interface & factory
│   │   ├── opencode.go                 # OpenCode CLI sandbox runner & tool injection
│   │   ├── claude.go                   # Claude Code CLI runner & config generator
│   │   ├── codex.go                    # OpenAI Codex CLI runner
│   │   └── smcp.go                     # Model Context Protocol (MCP) tool & skill configuration synthesizer
│   ├── integrations/                   # External Issue Tracker & Board Synchronizers
│   │   ├── provider.go                 # Common SyncProvider, PullRequestProvider & registry
│   │   ├── github/                     # GitHub API client: issue pulling, label syncing, PR creation
│   │   ├── jira/                       # Jira Cloud client: JQL issue pulling, status transitions, comments
│   │   └── trello/                     # Trello API client: board card pulling, list transitions, comments
│   └── ws/                             # Real-Time Event & Terminal Hub
│       └── hub.go                      # WebSocket connection hub, multiplexed ANSI log broadcast, channel security
├── web/                                # Embedded Frontend (React 19 + TypeScript + Vite)
│   ├── src/
│   │   ├── components/                 # KanbanBoard, TerminalView (xterm.js), DiffViewer, TeamSwitcher
│   │   ├── lib/api.ts                  # Typed Fetch client for Fiber v3 /api/v1 endpoints
│   │   └── App.tsx                     # Main dashboard layout and routing
│   ├── package.json
│   └── vite.config.ts
├── deploy/                             # Ingress & Infrastructure Packaging
│   ├── traefik/
│   │   ├── traefik.yml                 # Traefik static configuration (entrypoints, ACME resolvers)
│   │   └── dynamic.yml                 # Dynamic routing rules and TLS options
│   └── docker-compose.yml              # Production Compose topology: traefik, postgres, redis, control-plane
├── docs/                               # Architecture Specifications & Implementation Plans
└── tests/                              # Integration & End-to-End Test Suites
    ├── e2e/                            # Full stack containerized tests
    └── testutils/                      # Testcontainers-go helpers for Postgres, Redis, and mock Docker daemon
```

---

## 3. Team & Workspace Management

### 3.1 Organization Hierarchy
* **Team**: Workspace grouping engineers, repositories, and settings (e.g. `Backend Squad`, `Frontend Core`).
* **Project**: A specific codebase associated with a local repo path and team. A project maintains zero or more **Project Integrations** binding external issue trackers (GitHub repositories, Jira projects, Trello boards) to the project via encrypted credentials and non-secret JSON configuration.
* **Project Integration**: An extensible binding connecting a project to an external tracker (`github`, `jira`, `trello`). Keeps the core `Project` model clean and migration-free when adding new providers.
* **Members & Assignees**:
  * Tasks can be assigned to **Human Engineers** or **AI Agents** (`OpenCode`, `Claude Code`, `Codex`).

### 3.2 Authentication and authorization

The control plane uses an external OIDC provider and does not implement password authentication.

#### Login flow

1. The browser requests `/auth/login`.
2. The server generates cryptographically random `state`, `nonce`, and PKCE values, stores the short-lived transaction state server-side, and redirects to the configured OIDC provider.
3. The callback validates the authorization code exchange, issuer, audience, nonce, PKCE verifier, ID-token signature, and token expiry.
4. The server maps the stable identity `(issuer, subject)` to a local user and creates a server-side session.
5. The server sets an opaque session cookie with `Secure`, `HttpOnly`, and `SameSite=Lax` attributes. State-changing requests also require CSRF protection.

The durable identity key is `(issuer, subject)`. Email address and display name are profile attributes only and must not be used as the identity key.

#### Local roles

Team membership is local and is not inferred solely from an OIDC group claim:

* `owner`: manage team membership, projects, credentials, and destructive operations.
* `admin`: manage projects, tasks, integrations, and team settings; cannot transfer ownership.
* `member`: create and operate tasks within authorized projects; cannot manage credentials or memberships.
* `viewer`: read authorized projects, task logs, and review data; cannot execute, merge, or discard tasks.

Every HTTP endpoint, WebSocket subscription, task action, merge/discard operation, credential lookup, and webhook administration endpoint must authorize the resolved team and project scope.

#### Required configuration

The deployment supplies `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_REDIRECT_URL`, and a server-side session key through the host environment or secret store. These values must not be stored in PostgreSQL or exposed to a container.

---

## 4. GitHub & Jira Synchronization Engine

The backend implements a pluggable `SyncProvider` interface:

```go
type SyncProvider interface {
    ID() string // "github" | "jira"
    FetchIssues(ctx context.Context, project *Project) ([]ExternalIssue, error)
    UpdateIssueStatus(ctx context.Context, externalID string, status string) error
    CreatePullRequest(ctx context.Context, project *Project, branch string, title string, body string) (string, error)
    AddComment(ctx context.Context, externalID string, comment string) error
}
```

### 4.1 GitHub Provider
* **Authentication**: GitHub Personal Access Token (PAT) or GitHub App.
* **Inbound Sync**: Polls or receives webhooks for open issues labeled with target tags (e.g. `agent-ready` or all open issues).
* **Webhook security**: Verifies the provider signature before parsing an event and records the provider event ID for idempotent processing.
* **Identity mapping**: An external issue is unique within a configured provider project by `(provider, project, external_id)`; repeated polls or webhook deliveries update the existing task rather than creating another task.
* **Outbound Transition**:
  * When agent starts: Labels issue as `in-progress:agent`.
   * When agent finishes: The host controller captures the worktree state, pushes the controller-created task branch to origin, and calls the GitHub API to create a Pull Request linking the issue (`Fixes #<number>`). Moves card to **Review**.

### 4.2 Jira Provider
* **Authentication**: Jira Cloud Domain, User Email, and API Token.
* **Inbound Sync**: Fetches issues via JQL (e.g., `project = KEY AND status in ("To Do", "Backlog")`).
* **Webhook security**: Verifies the configured webhook authentication mechanism and records the provider event ID for idempotent processing.
* **Identity mapping**: A Jira issue is unique within a configured Jira project by `(provider, project, external_id)`.
* **Outbound Transition**:
  * When agent starts: Transitions Jira issue to *"In Progress"*.
   * When agent finishes: The host controller captures the worktree state, transitions the issue to *"In Review"*, and posts an execution summary comment with diff stats and PR link.

### 4.3 Provider boundary

`SyncProvider` is limited to issue synchronization. Pull-request operations are a separate repository-host capability because Jira does not provide pull requests:

```go
type SyncProvider interface {
    ID() string
    FetchIssues(ctx context.Context, project *Project) ([]ExternalIssue, error)
    UpdateIssueStatus(ctx context.Context, externalID string, status string) error
    AddComment(ctx context.Context, externalID string, comment string) error
}

type PullRequestProvider interface {
    ID() string
    CreatePullRequest(ctx context.Context, project *Project, branch, title, body string) (string, error)
}
```

Provider implementations must define status mappings, pagination, rate-limit handling, bounded retries, idempotency keys, and conflict behavior. A failed external mutation is visible as a sync error and does not silently advance the local task state.

---

## 5. Sandboxing & Security Controls

### 5.1 Host Filesystem Protection
* The host home directory (`~`, `~/.ssh`, `~/.bashrc`, `~/.config`) is **never** mounted into containers.
* **Only** the dedicated task worktree path (`.worktrees/task-<id>`) is mounted to `/workspace`.
* The task worktree is a source-file workspace. Its `.git` file and the parent repository's Git metadata remain outside the container; all branch, commit, diff, push, merge, and worktree cleanup operations are performed by the host controller.
* **Docker-out-of-Docker Path Translation**: When the control plane runs containerized and communicates with the host Docker daemon via `/var/run/docker.sock`, worktree directories on the control plane's local filesystem cannot be bound directly into sibling task containers using container-internal paths. Instead, the Go control plane resolves task worktree bind mounts using `HOST_WORKTREE_ROOT` (e.g. mapping internal `/srv/agent-tasks/worktrees/task-<id>` to host path `${HOST_WORKTREE_ROOT}/task-<id>`). The host Docker daemon binds the host path, preventing sibling container mount failures.

### 5.2 Linux Security Hardening
Container options set via Docker SDK:
```go
container.HostConfig{
    Binds: []string{
        worktreeHostPath + ":/workspace:rw",
    },
    Resources: container.Resources{
        Memory:   4 * 1024 * 1024 * 1024, // 4GB RAM limit
        NanoCPUs: 2 * 1000000000,         // 2 CPU cores max
    },
    CapDrop:     []string{"ALL"},          // Drop all Linux capabilities
    SecurityOpt: []string{"no-new-privileges:true"},
    NetworkMode: "bridge",                 // Egress policy is configured separately
}
```

### 5.3 MCP & Tool Containment
* Tools executed by the agent (e.g. bash commands, compilers, MCP servers) run **strictly inside the container**.
* The configured network policy controls egress explicitly. Docker `bridge` mode alone is not treated as an allowlist or as complete isolation from external services.
* The controller does not mount the Docker socket into a task container. Any host-side Docker access remains limited to the Go control plane.
* Credentials are injected only when required by the selected integration, are redacted from logs and terminal output where possible, and are never written into the worktree.

The sandbox is a defense-in-depth boundary, not a guarantee against a compromised host kernel, Docker daemon, or intentionally granted external service access. Before production use, the deployment must define its threat model, allowed egress destinations, container UID/GID, read-only root filesystem policy, temporary filesystem mounts, PID/resource limits, seccomp/AppArmor or SELinux profile, and device policy.

### 5.4 Cryptographic Defense & Secrets at Rest

1. **Third-Party Credentials Encryption (AES-256-GCM)**:
   - External tokens (GitHub PATs, Jira API keys, OIDC client secrets, custom MCP secrets) are stored in PostgreSQL inside the `credentials` table using **AES-256-GCM** (Galois/Counter Mode, AEAD).
   - An operator-managed 32-byte master key is provided via `SECRET_ENCRYPTION_KEY` (or Docker Secret).
   - For every encrypted secret, a fresh 96-bit (12-byte) cryptographic nonce is generated via `crypto/rand`. The resulting ciphertext and 128-bit authentication tag are stored alongside a key fingerprint: `base64(nonce || ciphertext || tag)`.
   - Any tampering or bit-flipping of the database record causes decryption to fail with an authentication error.

2. **Personal Access Tokens (One-Way Hashing via SHA-256)**:
   - Internal API tokens for CLI (`atm`), CI/CD pipelines, and agent callers (`atm_pat_<random_32_bytes>`) are **never** stored in reversible form.
   - The token is displayed to the user exactly once upon generation.
   - PostgreSQL stores only `token_hash = hex(sha256(raw_token))` in the `api_tokens` table.
   - Inbound requests authenticate via `Authorization: Bearer atm_pat_...`. The server computes the SHA-256 hash in memory and queries `WHERE token_hash = $1`, guaranteeing that a database compromise yields zero usable API tokens.

3. **Runtime Ingestion & Log Redaction**:
   - Decrypted secrets are held exclusively in process memory and passed into sandbox container environments at launch via Docker SDK environment variables.
   - Secrets are **never** committed to Git repositories or written to persistent files inside task worktrees.
   - The Go control plane's WebSocket hub and log ingester pass all stdout/stderr streams and API error messages through a redaction filter replacing known tokens, bearer headers, and private keys with `[REDACTED]`.

---

## 6. Multi-Agent Runner Architecture

The backend implements a modular runner interface:

```go
type AgentRunner interface {
    ID() string
    Name() string
    IsAvailable(ctx context.Context) (bool, error)
    GenerateContainerConfig(task *Task, worktreeDir string) (*container.Config, *container.HostConfig, error)
}
```

### 6.1 Supported Agents
1. **OpenCode Runner**:
   * Runs `opencode run "<prompt>" --model <model>`.
   * Also supports remote endpoints via OpenCode Server API (`opencode serve`).
2. **Claude Code Runner**:
    * Runs with the minimum required permission mode inside the sandbox; `--dangerously-skip-permissions` is prohibited by default and requires an explicit deployment-level exception with a documented threat-model review.
    * Passes instructions via prompt payloads and `.claude/rules/`, treating external issue text as untrusted input.
3. **OpenAI Codex Runner**:
   * Runs `codex exec "<prompt>"` non-interactively.

---

## 7. `smcp` Integration & Skill / MCP Injection

The task manager optionally integrates with `@tanphat/smcp-core` or a configured `smcp` service/CLI. The integration is disabled unless an explicit executable, service endpoint, or library configuration is supplied by the deployment:

1. **Skill Discovery**:
    * The Go backend discovers installed skills through configured agent directories or by running the configured `smcp list --json` command.
   * In the task creation form, skills appear as selectable badges (e.g., `test-driven-development`, `systematic-debugging`).
2. **MCP Server Discovery & Presets**:
   * Reads configured MCP servers from `smcp`'s registry or config files.
   * Users can select which MCP tools to attach per task (e.g. `postgres`, `browser`, `github`).
3. **Sandboxed Config Injection**:
   * When provisioning the worktree prior to container launch, the backend writes a sandboxed configuration:
     * For OpenCode: writes `.worktrees/task-<id>/opencode.json` containing only the selected MCP tools.
     * For Claude: writes `.worktrees/task-<id>/.claude/config.json`.
   * Environment variables and API keys are passed directly into the container environment at runtime, never saved in git.

---

## 8. End-to-End Task Lifecycle

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Backlog    │ ──▶ │    Queued    │ ──▶ │  In Progress │ ──▶ │    Review    │ ──▶ │     Done     │
│ (Create/Sync)│     │(Prioritized) │     │(Worktree+Run)│     │ (Diff & QA)  │     │(Merge & Prune│
└──────────────┘     └──────────────┘     └──────────────┘     └──────────────┘     └──────────────┘
```

### Phase 1: Creation & Ingestion (`Backlog`)
* Tasks can be created manually or ingested from GitHub Issues / Jira Tickets.
* Task attributes:
  * Team ID & Project ID
  * Title & Prompt
  * Priority (Integer: `urgent: 100`, `high: 75`, `normal: 50`, `low: 25`; default: `50`)
  * Assignee (Human or Agent: OpenCode / Claude / Codex)
  * External Sync Link (GitHub Issue # or Jira Key)
  * Selected Skills & MCP servers

### Phase 2: Distributed Enqueueing & Asynq Worker Scheduling (`Queued`)
1. **Transition to Queue**:
   * A task in `backlog` enters `queued` when explicitly triggered by a user, webhook event, or automated policy.
   * The task record is updated in PostgreSQL with `status = 'queued'`. The Go control plane immediately enqueues the job into Redis via the Asynq client (`github.com/hibiken/asynq`):
     ```go
     payload, _ := json.Marshal(TaskExecutionPayload{TaskID: task.ID})
     asynqTask := asynq.NewTask(TypeExecuteAgentTask, payload)
     info, err := asynqClient.Enqueue(asynqTask,
         asynq.Queue(task.QueueTier()), // "critical" for urgent (100), "default" for normal (50), "low" (25)
         asynq.MaxRetry(3),
         asynq.Timeout(time.Duration(cfg.TaskTimeoutMinutes) * time.Minute),
     )
     ```
   * The returned Asynq task ID is saved to `tasks.asynq_task_id` in PostgreSQL for correlation and cancel operations.
2. **Asynq Worker Pool & Concurrency Limiter**:
   * The Go control plane runs an Asynq Server with capacity strictly capped by `MAX_CONCURRENT_TASKS` (default: `4`):
     ```go
     srv := asynq.NewServer(
         asynq.RedisClientOpt{Addr: cfg.RedisAddr, Password: cfg.RedisPassword},
         asynq.Config{
             Concurrency: cfg.MaxConcurrentTasks,
             Queues: map[string]int{
                 "critical": 6, // 60% priority weight
                 "default":  3, // 30% priority weight
                 "low":      1, // 10% priority weight
             },
         },
     )
     ```
   * Urgent human tasks (`critical`) are prioritized over background synced backlog issues (`default` and `low`).
3. **Execution Claim & Status Transition**:
   * When an Asynq worker dequeues a task, it updates `status = 'in_progress'` in PostgreSQL and starts the task execution lifecycle.
   * Asynq's built-in heartbeats monitor worker health. If a worker process crashes, Asynq automatically retries or transitions the job according to the retry policy.
4. **Immediate Cancellation in Queue**:
   * Queued tasks can be cancelled via the API or Web UI before worker dispatch. The control plane calls `asynqInspector.DeleteTask(queueName, asynqTaskID)` and transitions the task in PostgreSQL to `cancelled`, bypassing Docker container spin-up and Git worktree allocation.

### Phase 3: Execution (`In Progress`)
1. **Worktree Creation**:
   * Go runs `git worktree add .worktrees/task-<id> -b task/<id>-<slug>` in the project repo.
   * The worktree's Git metadata remains host-controlled; the container receives the task directory and does not need the parent repository's `.git` metadata.
2. **Status Notification**:
   * Updates GitHub Issue (label `in-progress:agent`) and Jira (transition to "In Progress").
3. **Config Prep**:
   * Injects selected skills and sanitized MCP configs into the worktree.
4. **Docker Launch**:
   * Creates and starts the container mounting the worktree to `/workspace`.
5. **Real-Time Streaming & Heartbeat Supervision**:
   * Go attaches to stdout/stderr via Docker SDK and streams frames to the WebSocket hub.
   * UI displays live ANSI output in `xterm.js`.
   * While running, the worker executes a periodic heartbeat ticker updating `heartbeat_at = CURRENT_TIMESTAMP` (e.g. every 15 seconds) to signal that the worker and container are healthy.
6. **Abort Handling**:
   * Clicking "Stop" sends `docker stop` & `docker rm -f`, gracefully halting execution.
   * The controller records cancellation, captures any remaining worktree changes, and performs cleanup only after the container is confirmed stopped.

### Phase 4: Review (`Review`)
* On container exit:
   * Container is destroyed.
   * The host controller captures staged, unstaged, and untracked changes before review. Agents are not required to commit; the controller creates the review snapshot and any subsequent commit on the host.
   * Review compares the captured task snapshot with the configured project base branch; `main` is not assumed to exist.
  * If connected to GitHub/Jira: creates Draft PR or updates Jira to "In Review".
  * Web UI displays changed files with diff syntax highlighting.
* **Actions Available**:
  * **Approve & Merge**: Merges branch into `main` (or merges GitHub PR) and cleans up worktree.
  * **Follow-up Prompt**: Enqueues a revision prompt in the *same* worktree, transitioning the task back to `queued` with elevated priority.
* **Discard**: Deletes worktree and branches.

### 8.1 Failure, restart, and recovery rules

* `failed` and `cancelled` are terminal task states for the current execution attempt; a retry creates a new attempt record while preserving prior logs and snapshots.
* **Heartbeat Lease Timeout & Stalled Worker Recovery**:
  * A supervisor goroutine runs at regular intervals checking for tasks in `in_progress` where `heartbeat_at < NOW() - INTERVAL 'TASK_LEASE_TIMEOUT_SECONDS seconds'` (default: 120 seconds).
  * If the associated Docker container is dead, unresponsive, or missing from the host daemon, the supervisor reclaims the stalled task: logs a lease expiry incident in `task_attempts`, cleans up leftover container resources, and re-enqueues the task as `queued` (or marks `failed` if the maximum retry threshold is exceeded).
* A controller restart reconciles persisted task records with Docker containers and Git worktrees:
  * Tasks left in `in_progress` during an ungraceful shutdown without an active container are reclaimed by the supervisor upon restart.
  * Containers without a live task record are stopped and removed after an operator-visible orphan warning.
* A task with a live container is resumed as `in_progress` only after the controller verifies the container identity, worktree path, and task ownership metadata.
* Provider updates and webhook handling are retried with bounded exponential backoff and an idempotency key. A provider failure does not silently advance the local task state; it creates an operator-visible sync error.
* Lifecycle actions are serialized per task. Stop, merge, discard, retry, and follow-up actions cannot run concurrently.
* Timeouts are explicit configuration values. A timeout records the reason, stops the container, captures the worktree, and transitions the task to `failed` unless the operator retries it.

### Phase 5: Archival (`Done`)
* Go executes `git worktree remove .worktrees/task-<id>`.
* Task marked completed with duration, exit code, commit hash, and closed PR/Jira ticket.

---

## 9. Data Storage & Schema (PostgreSQL 18 & GORM v2)

The application uses **PostgreSQL 18** as its relational backing store, accessed through **GORM v2** (`gorm.io/gorm`) with the Postgres driver (`gorm.io/driver/postgres`) backed by standard `database/sql` connection pooling.

### 9.1 Connection Management & Pool Lifecycle

Database connection initialization configures the standard `*sql.DB` connection pool under GORM:

```go
package db

import (
	"database/sql"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Config struct {
	Host            string
	Port            int
	User            string
	Password        string
	DBName          string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

func Connect(cfg Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Warn),
		SkipDefaultTransaction: false, // Ensures all create/update operations run within explicit transactions
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open gorm connection: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to extract sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	return db, nil
}
```

### 9.2 GORM v2 Model Structs, Go Idiomatic Enums & Struct Optimization

All domain enumerations are declared as strongly typed Go enums with typed constants, compile-time safety, and validation. Struct fields are optimized for memory alignment (64-bit timestamps, pointers, slices, and 16-byte strings grouped sequentially to eliminate internal padding holes on 64-bit architectures, verified via `fieldalignment`).

All API inputs, DTOs, and domain mutations are validated at system boundaries using **`github.com/go-playground/validator/v10`** with strict struct tags (`validate:"required,min=1,max=255"`) and registered custom enum validators.

```go
package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
)

var Validate = validator.New()

func init() {
	_ = Validate.RegisterValidation("team_role", func(fl validator.FieldLevel) bool {
		return TeamRole(fl.Field().String()).IsValid()
	})
	_ = Validate.RegisterValidation("task_status", func(fl validator.FieldLevel) bool {
		return TaskStatus(fl.Field().String()).IsValid()
	})
	_ = Validate.RegisterValidation("assignee_type", func(fl validator.FieldLevel) bool {
		t := AssigneeType(fl.Field().String())
		return t == AssigneeAgent || t == AssigneeHuman
	})
	_ = Validate.RegisterValidation("task_priority", func(fl validator.FieldLevel) bool {
		p := TaskPriority(fl.Field().Int())
		return p == PriorityUrgent || p == PriorityHigh || p == PriorityNormal || p == PriorityLow
	})
}

// TeamRole represents the team-level RBAC role ordered by ascending privilege.
type TeamRole int

const (
	RoleViewer TeamRole = 10
	RoleMember TeamRole = 20
	RoleAdmin  TeamRole = 30
	RoleOwner  TeamRole = 40
)

func (r TeamRole) IsValid() bool {
	switch r {
	case RoleViewer, RoleMember, RoleAdmin, RoleOwner:
		return true
	}
	return false
}

func (r TeamRole) Value() (driver.Value, error) {
	if !r.IsValid() {
		return nil, fmt.Errorf("invalid TeamRole: %d", r)
	}
	return int64(r), nil
}

func (r *TeamRole) Scan(value any) error {
	if value == nil {
		return errors.New("cannot scan nil into TeamRole")
	}
	switch v := value.(type) {
	case int64:
		*r = TeamRole(v)
	case int:
		*r = TeamRole(v)
	case int32:
		*r = TeamRole(v)
	case int16:
		*r = TeamRole(v)
	default:
		return fmt.Errorf("unsupported scan type %T for TeamRole", value)
	}
	if !r.IsValid() {
		return fmt.Errorf("invalid TeamRole in database: %d", *r)
	}
	return nil
}

// IntegrationProvider identifies an external issue tracker / project management provider.
type IntegrationProvider string

const (
	IntegrationGitHub IntegrationProvider = "github"
	IntegrationJira   IntegrationProvider = "jira"
	IntegrationTrello IntegrationProvider = "trello"
)

func (p IntegrationProvider) IsValid() bool {
	switch p {
	case IntegrationGitHub, IntegrationJira, IntegrationTrello:
		return true
	}
	return false
}

// CredentialProvider identifies an external credential target.
type CredentialProvider string

const (
	ProviderGitHub  CredentialProvider = "github"
	ProviderJira    CredentialProvider = "jira"
	ProviderTrello  CredentialProvider = "trello"
	ProviderOIDC    CredentialProvider = "oidc"
	ProviderGeneric CredentialProvider = "generic"
)

func (p CredentialProvider) IsValid() bool {
	switch p {
	case ProviderGitHub, ProviderJira, ProviderTrello, ProviderOIDC, ProviderGeneric:
		return true
	}
	return false
}

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusBacklog    TaskStatus = "backlog"
	TaskStatusQueued     TaskStatus = "queued"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusReview     TaskStatus = "review"
	TaskStatusDone       TaskStatus = "done"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusCancelled  TaskStatus = "cancelled"
)

func (s TaskStatus) IsValid() bool {
	switch s {
	case TaskStatusBacklog, TaskStatusQueued, TaskStatusInProgress,
		TaskStatusReview, TaskStatusDone, TaskStatusFailed, TaskStatusCancelled:
		return true
	}
	return false
}

// TaskPriority represents queue scheduling priority tiers.
type TaskPriority int

const (
	PriorityUrgent TaskPriority = 100
	PriorityHigh   TaskPriority = 75
	PriorityNormal TaskPriority = 50
	PriorityLow    TaskPriority = 25
)

// AssigneeType designates whether an assignee is an AI agent or human engineer.
type AssigneeType string

const (
	AssigneeAgent AssigneeType = "agent"
	AssigneeHuman AssigneeType = "human"
)

// AttemptStatus represents the execution state of an individual task run.
type AttemptStatus string

const (
	AttemptQueued    AttemptStatus = "queued"
	AttemptRunning   AttemptStatus = "running"
	AttemptSucceeded AttemptStatus = "succeeded"
	AttemptFailed    AttemptStatus = "failed"
	AttemptCancelled AttemptStatus = "cancelled"
)

// WebhookStatus represents the ingestion state of an external webhook payload.
type WebhookStatus string

const (
	WebhookReceived  WebhookStatus = "received"
	WebhookProcessed WebhookStatus = "processed"
	WebhookFailed    WebhookStatus = "failed"
)

// StringArray represents a JSONB-serializable array of strings.
type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "[]", nil
	}
	return json.Marshal(a)
}

func (a *StringArray) Scan(value any) error {
	if value == nil {
		*a = []string{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("unsupported scan type for StringArray")
	}
	return json.Unmarshal(bytes, a)
}

// JSONMap represents a JSONB-serializable map of key-value configurations.
type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(value any) error {
	if value == nil {
		*m = JSONMap{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("unsupported scan type for JSONMap")
	}
	return json.Unmarshal(bytes, m)
}

type Team struct {
	CreatedAt   time.Time        `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time        `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	Memberships []TeamMembership `gorm:"foreignKey:TeamID;constraint:OnDelete:CASCADE" json:"memberships,omitempty"`
	Projects    []Project        `gorm:"foreignKey:TeamID;constraint:OnDelete:CASCADE" json:"projects,omitempty"`
	Credentials []Credential     `gorm:"foreignKey:TeamID;constraint:OnDelete:CASCADE" json:"credentials,omitempty"`
	ID          string           `gorm:"type:uuid;primaryKey" json:"id" validate:"required"`
	Name        string           `gorm:"type:text;not null" json:"name" validate:"required,min=1,max=100"`
	Slug        string           `gorm:"type:text;not null;uniqueIndex:idx_teams_slug" json:"slug" validate:"required,min=1,max=50,alphanum"`
}

type User struct {
	CreatedAt   time.Time        `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time        `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	Memberships []TeamMembership `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"memberships,omitempty"`
	ID          string           `gorm:"type:uuid;primaryKey" json:"id" validate:"required"`
	Issuer      string           `gorm:"type:text;not null;uniqueIndex:idx_users_issuer_subject,priority:1" json:"issuer" validate:"required,url"`
	Subject     string           `gorm:"type:text;not null;uniqueIndex:idx_users_issuer_subject,priority:2" json:"subject" validate:"required"`
	Email       string           `gorm:"type:text" json:"email,omitempty" validate:"omitempty,email"`
	DisplayName string           `gorm:"type:text" json:"display_name,omitempty" validate:"omitempty,max=100"`
}

type TeamMembership struct {
	CreatedAt time.Time `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	Team      Team      `gorm:"foreignKey:TeamID" json:"team,omitempty"`
	User      User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
	TeamID    string    `gorm:"type:uuid;primaryKey" json:"team_id" validate:"required"`
	UserID    string    `gorm:"type:uuid;primaryKey;index:idx_team_memberships_user" json:"user_id" validate:"required"`
	Role      TeamRole  `gorm:"type:smallint;not null;check:role IN (10, 20, 30, 40)" json:"role" validate:"required,team_role"`
}

type APIToken struct {
	CreatedAt  time.Time  `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	ExpiresAt  *time.Time `gorm:"type:timestamptz" json:"expires_at,omitempty"`
	LastUsedAt *time.Time `gorm:"type:timestamptz" json:"last_used_at,omitempty"`
	User       User       `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Team       Team       `gorm:"foreignKey:TeamID" json:"team,omitempty"`
	ID         string     `gorm:"type:uuid;primaryKey" json:"id" validate:"required"`
	UserID     string     `gorm:"type:uuid;not null;index:idx_tokens_user" json:"user_id" validate:"required"`
	TeamID     string     `gorm:"type:uuid;not null;index:idx_tokens_team" json:"team_id" validate:"required"`
	Name       string     `gorm:"type:text;not null" json:"name" validate:"required,min=1,max=100"`
	TokenHash  string     `gorm:"type:text;not null;uniqueIndex:idx_tokens_hash" json:"-"`
}

type Project struct {
	CreatedAt    time.Time            `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt    time.Time            `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	Team         Team                 `gorm:"foreignKey:TeamID" json:"team,omitempty"`
	Integrations []ProjectIntegration `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE" json:"integrations,omitempty"`
	Tasks        []Task               `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE" json:"tasks,omitempty"`
	ID           string               `gorm:"type:uuid;primaryKey" json:"id" validate:"required"`
	TeamID       string               `gorm:"type:uuid;not null;index:idx_projects_team;uniqueIndex:idx_projects_team_name,priority:1" json:"team_id" validate:"required"`
	Name         string               `gorm:"type:text;not null;uniqueIndex:idx_projects_team_name,priority:2" json:"name" validate:"required,min=1,max=100"`
	RepoPath     string               `gorm:"type:text;not null" json:"repo_path" validate:"required"`
}

type ProjectIntegration struct {
	CreatedAt    time.Time           `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt    time.Time           `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	LastSyncedAt *time.Time          `gorm:"type:timestamptz" json:"last_synced_at,omitempty"`
	ExternalURL  *string             `gorm:"type:text" json:"external_url,omitempty"`
	Project      Project             `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
	Credential   Credential          `gorm:"foreignKey:CredentialID" json:"credential,omitempty"`
	Config       JSONMap             `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	ID           string              `gorm:"type:uuid;primaryKey" json:"id" validate:"required"`
	ProjectID    string              `gorm:"type:uuid;not null;index:idx_project_integrations_project;uniqueIndex:idx_proj_integ_ext,priority:1" json:"project_id" validate:"required"`
	CredentialID string              `gorm:"type:uuid;not null" json:"credential_id" validate:"required"`
	Provider     IntegrationProvider `gorm:"type:text;not null;uniqueIndex:idx_proj_integ_ext,priority:2;check:provider IN ('github', 'jira', 'trello')" json:"provider" validate:"required,oneof=github jira trello"`
	ExternalID   string              `gorm:"type:text;not null;uniqueIndex:idx_proj_integ_ext,priority:3" json:"external_id" validate:"required"`
	Enabled      bool                `gorm:"not null;default:true" json:"enabled"`
}

type Credential struct {
	CreatedAt      time.Time          `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt      time.Time          `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	Team           Team               `gorm:"foreignKey:TeamID" json:"team,omitempty"`
	ID             string             `gorm:"type:uuid;primaryKey" json:"id" validate:"required"`
	TeamID         string             `gorm:"type:uuid;not null;index:idx_credentials_team;uniqueIndex:idx_credentials_team_label,priority:1" json:"team_id" validate:"required"`
	Provider       CredentialProvider `gorm:"type:text;not null;check:provider IN ('github', 'jira', 'trello', 'oidc', 'generic')" json:"provider" validate:"required,oneof=github jira trello oidc generic"`
	Label          string             `gorm:"type:text;not null;uniqueIndex:idx_credentials_team_label,priority:2" json:"label" validate:"required,min=1,max=100"`
	EncryptedData  string             `gorm:"type:text;not null" json:"-"` // AES-256-GCM base64(nonce || ciphertext || tag)
	KeyFingerprint string             `gorm:"type:text;not null" json:"-"` // Master key fingerprint for key rotation
}

type Task struct {
	CreatedAt     time.Time           `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt     time.Time           `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	HeartbeatAt   *time.Time          `gorm:"type:timestamptz" json:"heartbeat_at,omitempty"`
	IntegrationID *string             `gorm:"type:uuid;index:idx_tasks_integration" json:"integration_id,omitempty"`
	AsynqTaskID   *string             `gorm:"type:text;index:idx_tasks_asynq_id" json:"asynq_task_id,omitempty"`
	ExternalID    *string             `gorm:"type:text" json:"external_id,omitempty"`
	ExternalURL   *string             `gorm:"type:text" json:"external_url,omitempty" validate:"omitempty,url"`
	PRURL         *string             `gorm:"type:text" json:"pr_url,omitempty" validate:"omitempty,url"`
	WorktreePath  *string             `gorm:"type:text" json:"worktree_path,omitempty"`
	BranchName    *string             `gorm:"type:text" json:"branch_name,omitempty"`
	Model         *string             `gorm:"type:text" json:"model,omitempty"`
	ContainerID   *string             `gorm:"type:text" json:"container_id,omitempty"`
	ExitCode      *int                `gorm:"type:integer" json:"exit_code,omitempty"`
	Skills        StringArray         `gorm:"type:jsonb;not null;default:'[]';index:idx_tasks_skills,type:gin" json:"skills"`
	MCPServers    StringArray         `gorm:"type:jsonb;not null;default:'[]';index:idx_tasks_mcp,type:gin" json:"mcp_servers"`
	Attempts      []TaskAttempt       `gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE" json:"attempts,omitempty"`
	Logs          []TaskLog           `gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE" json:"logs,omitempty"`
	Project       Project             `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
	Integration   *ProjectIntegration `gorm:"foreignKey:IntegrationID" json:"integration,omitempty"`
	ID            string              `gorm:"type:uuid;primaryKey" json:"id" validate:"required"`
	ProjectID     string              `gorm:"type:uuid;not null;index:idx_tasks_project" json:"project_id" validate:"required"`
	Title         string              `gorm:"type:text;not null" json:"title" validate:"required,min=1,max=255"`
	Prompt        string              `gorm:"type:text;not null" json:"prompt" validate:"required"`
	AssigneeID    string              `gorm:"type:text;not null" json:"assignee_id" validate:"required"`
	Status        TaskStatus          `gorm:"type:text;not null;index:idx_tasks_status;check:status IN ('backlog', 'queued', 'in_progress', 'review', 'done', 'failed', 'cancelled')" json:"status" validate:"required,task_status"`
	AssigneeType  AssigneeType        `gorm:"type:text;not null;default:'agent';check:assignee_type IN ('agent', 'human')" json:"assignee_type" validate:"required,assignee_type"`
	Priority      TaskPriority        `gorm:"type:integer;not null;default:50;index:idx_tasks_priority" json:"priority" validate:"task_priority"`
}

type TaskAttempt struct {
	CreatedAt     time.Time     `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	StartedAt     *time.Time    `gorm:"type:timestamptz" json:"started_at,omitempty"`
	FinishedAt    *time.Time    `gorm:"type:timestamptz" json:"finished_at,omitempty"`
	ContainerID   *string       `gorm:"type:text" json:"container_id,omitempty"`
	WorktreePath  *string       `gorm:"type:text" json:"worktree_path,omitempty"`
	BranchName    *string       `gorm:"type:text" json:"branch_name,omitempty"`
	ExitCode      *int          `gorm:"type:integer" json:"exit_code,omitempty"`
	FailureReason *string       `gorm:"type:text" json:"failure_reason,omitempty"`
	Task          Task          `gorm:"foreignKey:TaskID" json:"task,omitempty"`
	ID            string        `gorm:"type:uuid;primaryKey" json:"id" validate:"required"`
	TaskID        string        `gorm:"type:uuid;not null;uniqueIndex:idx_attempts_task_attempt,priority:1" json:"task_id" validate:"required"`
	AttemptNumber int           `gorm:"type:integer;not null;uniqueIndex:idx_attempts_task_attempt,priority:2" json:"attempt_number" validate:"required,min=1"`
	Status        AttemptStatus `gorm:"type:text;not null;check:status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')" json:"status" validate:"required"`
}

type WebhookEvent struct {
	ReceivedAt      time.Time     `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"received_at"`
	ProcessedAt     *time.Time    `gorm:"type:timestamptz" json:"processed_at,omitempty"`
	Project         Project       `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE" json:"project,omitempty"`
	ErrorMessage    *string       `gorm:"type:text" json:"error_message,omitempty"`
	Provider        string        `gorm:"type:text;primaryKey" json:"provider" validate:"required,oneof=github jira trello"`
	ProjectID       string        `gorm:"type:uuid;primaryKey;index:idx_webhook_project" json:"project_id" validate:"required"`
	ProviderEventID string        `gorm:"type:text;primaryKey" json:"provider_event_id" validate:"required"`
	Status          WebhookStatus `gorm:"type:text;not null;check:status IN ('received', 'processed', 'failed')" json:"status" validate:"required"`
}

type TaskLog struct {
	Timestamp time.Time    `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP;index:idx_task_logs_task_attempt,priority:3" json:"timestamp"`
	Task      Task         `gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE" json:"task,omitempty"`
	Attempt   *TaskAttempt `gorm:"foreignKey:AttemptID;constraint:OnDelete:CASCADE" json:"attempt,omitempty"`
	ID        int64        `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID    string       `gorm:"type:uuid;not null;index:idx_task_logs_task_attempt,priority:1" json:"task_id" validate:"required"`
	AttemptID *string      `gorm:"type:uuid;index:idx_task_logs_task_attempt,priority:2" json:"attempt_id,omitempty"`
	LogChunk  string       `gorm:"type:text;not null" json:"log_chunk" validate:"required"`
}
```

### 9.3 PostgreSQL 18 Relational Schema (DDL)

```sql
-- PostgreSQL 18 Relational Schema Specification

CREATE TABLE IF NOT EXISTS teams (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    email TEXT,
    display_name TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_users_issuer_subject UNIQUE (issuer, subject)
);

CREATE TABLE IF NOT EXISTS team_memberships (
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role SMALLINT NOT NULL CHECK (role IN (10, 20, 30, 40)),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (team_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_team_memberships_user_id ON team_memberships(user_id);

CREATE TABLE IF NOT EXISTS api_tokens (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_api_tokens_user_id ON api_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_api_tokens_team_id ON api_tokens(team_id);

-- Third-party credentials encrypted at rest using AES-256-GCM.
CREATE TABLE IF NOT EXISTS credentials (
    id UUID PRIMARY KEY,
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('github', 'jira', 'trello', 'oidc', 'generic')),
    label TEXT NOT NULL,
    encrypted_data TEXT NOT NULL, -- AES-256-GCM base64(nonce || ciphertext || tag)
    key_fingerprint TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_credentials_team_label UNIQUE (team_id, label)
);

CREATE INDEX IF NOT EXISTS idx_credentials_team_id ON credentials(team_id);

CREATE TABLE IF NOT EXISTS projects (
    id UUID PRIMARY KEY,
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    repo_path TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_projects_team_name UNIQUE (team_id, name)
);

CREATE INDEX IF NOT EXISTS idx_projects_team_id ON projects(team_id);

-- Extensible project-provider integrations (GitHub, Jira, Trello, etc.)
CREATE TABLE IF NOT EXISTS project_integrations (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    credential_id UUID NOT NULL REFERENCES credentials(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL CHECK (provider IN ('github', 'jira', 'trello')),
    external_id TEXT NOT NULL, -- e.g., 'owner/repo', Jira project key, or Trello board ID
    external_url TEXT,
    config JSONB NOT NULL DEFAULT '{}'::jsonb, -- Provider-specific non-secret settings
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    last_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_project_integrations_external UNIQUE (project_id, provider, external_id)
);

CREATE INDEX IF NOT EXISTS idx_project_integrations_project ON project_integrations(project_id);
CREATE INDEX IF NOT EXISTS idx_project_integrations_provider ON project_integrations(provider);

CREATE TABLE IF NOT EXISTS tasks (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    integration_id UUID REFERENCES project_integrations(id) ON DELETE SET NULL,
    asynq_task_id TEXT, -- Correlates with Redis / Asynq for queue cancellation
    title TEXT NOT NULL,
    prompt TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 50, -- urgent: 100, high: 75, normal: 50, low: 25
    status TEXT NOT NULL CHECK (status IN ('backlog', 'queued', 'in_progress', 'review', 'done', 'failed', 'cancelled')),
    assignee_type TEXT NOT NULL DEFAULT 'agent' CHECK (assignee_type IN ('agent', 'human')),
    assignee_id TEXT NOT NULL, -- registered agent ID or users.id
    external_id TEXT, -- e.g. '#42' or 'PROJ-102'
    external_url TEXT,
    pr_url TEXT,
    worktree_path TEXT,
    branch_name TEXT,
    model TEXT,
    skills JSONB NOT NULL DEFAULT '[]'::jsonb,
    mcp_servers JSONB NOT NULL DEFAULT '[]'::jsonb,
    container_id TEXT,
    exit_code INTEGER,
    heartbeat_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tasks_project_id ON tasks(project_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_project_status_priority ON tasks(project_id, status, priority DESC, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_skills ON tasks USING gin(skills);
CREATE INDEX IF NOT EXISTS idx_tasks_mcp ON tasks USING gin(mcp_servers);
CREATE INDEX IF NOT EXISTS idx_tasks_asynq_id ON tasks(asynq_task_id) WHERE asynq_task_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_tasks_external_identity
    ON tasks(project_id, integration_id, external_id)
    WHERE external_id IS NOT NULL AND integration_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS task_attempts (
    id UUID PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    attempt_number INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    container_id TEXT,
    worktree_path TEXT,
    branch_name TEXT,
    exit_code INTEGER,
    failure_reason TEXT,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_task_attempts_task_attempt UNIQUE (task_id, attempt_number)
);

CREATE INDEX IF NOT EXISTS idx_task_attempts_task_id ON task_attempts(task_id);

CREATE TABLE IF NOT EXISTS webhook_events (
    provider TEXT NOT NULL,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    provider_event_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('received', 'processed', 'failed')),
    error_message TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at TIMESTAMPTZ,
    PRIMARY KEY (provider, project_id, provider_event_id)
);

CREATE INDEX IF NOT EXISTS idx_webhook_events_project_id ON webhook_events(project_id);

CREATE TABLE IF NOT EXISTS task_logs (
    id BIGSERIAL PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    attempt_id UUID REFERENCES task_attempts(id) ON DELETE CASCADE,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    log_chunk TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_task_logs_task_attempt ON task_logs(task_id, attempt_id, id ASC);
```

---

## 10. Self-Hosting & Deployment Architecture

### 10.1 Architecture Principles
The system is built **self-hosting first**, running as a self-contained Docker Compose stack designed for straightforward on-premises or cloud VM operation.
The stack enforces zero internet exposure for sensitive storage, strictly segmenting traffic across Docker networks.

### 10.2 Service Topology
The reference deployment consists of three primary services:
1. **`traefik`**: Edge reverse proxy terminating HTTP/HTTPS, executing automatic ACME TLS issuance, and handling WebSocket connection persistence.
2. **`postgres`**: PostgreSQL 18 container storing relational data, configured with persistent storage and restricted to internal network traffic. The reference deployment specifies `image: postgres:18-alpine` (the official Alpine image for PostgreSQL 18). Operators requiring a glibc baseline may substitute `postgres:18`, or use `postgres:17-alpine` if targeting an earlier LTS release.
3. **`control-plane`**: Single Go binary serving REST APIs, executing Git worktree operations on mounted host repositories, communicating with Docker via the host Docker socket, and serving the embedded frontend.

### 10.3 Network Segmentation
1. **`ingress` network (`bridge`, external-facing)**:
   - Shared between `traefik` and `control-plane`.
   - Traefik exposes ports `80` (HTTP redirect to HTTPS) and `443` (HTTPS edge termination).
   - `control-plane` is reached only via Traefik reverse proxy; its application port is not published to host interfaces.
2. **`backend` network (`bridge`, `internal: true`)**:
   - Shared strictly between `control-plane` and `postgres`.
   - `postgres` port `5432` has no host port binding and cannot be routed from outside Docker.
3. **Task sandbox networks (`isolated bridge per container`)**:
   - Spawned on demand by the Go control plane for each task execution.
   - Task containers are not attached to `backend` or `ingress` networks, guaranteeing that untrusted sandboxes cannot connect to PostgreSQL or the control plane's internal management endpoints.

### 10.4 Volume Mounts & Permissions
* **`postgres_data` (Named Volume)**: Mounted to `/var/lib/postgresql/data` inside the `postgres` container.
* **Control Plane Repositories & Worktrees (`/srv/agent-tasks/repos` & `/srv/agent-tasks/worktrees`)**: Host directories mounted into the `control-plane` container. The control plane manages Git worktree lifecycle within this volume.
* **Host Docker Socket (`/var/run/docker.sock`)**: Mounted **only** into the `control-plane` container, granting it permissions to spawn task sandbox containers on the host Docker daemon. **Never mounted into task sandbox containers.**
* **Docker-out-of-Docker Worktree Path Resolution**: When the Go control plane runs containerized with `/var/run/docker.sock` mounted, its internal worktree directory (e.g. `/srv/agent-tasks/worktrees/task-<id>`) is not valid for sibling container bind mounts on the host daemon. The control plane must translate the worktree bind source using `HOST_WORKTREE_ROOT` (e.g. `${HOST_WORKTREE_ROOT}/task-<id>`), ensuring the host Docker daemon binds the correct host path and preventing sibling container mount failures.

### 10.5 Reference `docker-compose.yml`

```yaml
version: "3.8"

networks:
  ingress:
    name: agent_ingress
    driver: bridge
  backend:
    name: agent_backend
    driver: bridge
    internal: true

volumes:
  postgres_data:
    name: agent_postgres_data
  redis_data:
    name: agent_redis_data
  traefik_acme:
    name: agent_traefik_acme

services:
  traefik:
    image: traefik:v3.1
    container_name: agent-traefik
    restart: unless-stopped
    command:
      - "--providers.docker=true"
      - "--providers.docker.exposedbydefault=false"
      - "--entrypoints.web.address=:80"
      - "--entrypoints.web.http.redirections.entrypoint.to=websecure"
      - "--entrypoints.web.http.redirections.entrypoint.scheme=https"
      - "--entrypoints.websecure.address=:443"
      - "--certificatesresolvers.letsencrypt.acme.tlschallenge=true"
      - "--certificatesresolvers.letsencrypt.acme.email=${ACME_EMAIL}"
      - "--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json"
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - "/var/run/docker.sock:/var/run/docker.sock:ro"
      - "traefik_acme:/letsencrypt"
    networks:
      - ingress

  postgres:
    image: postgres:18-alpine # Official image for PostgreSQL 18. Fallback options: postgres:18 (glibc baseline) or postgres:17-alpine (prior LTS)
    container_name: agent-postgres
    restart: unless-stopped
    environment:
      POSTGRES_DB: ${POSTGRES_DB:-agent_tasks}
      POSTGRES_USER: ${POSTGRES_USER:-agent_user}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    networks:
      - backend
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-agent_user} -d ${POSTGRES_DB:-agent_tasks}"]
      interval: 5s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    container_name: agent-redis
    restart: unless-stopped
    command: ["redis-server", "--appendonly", "yes", "--requirepass", "${REDIS_PASSWORD}"]
    volumes:
      - redis_data:/data
    networks:
      - backend
    healthcheck:
      test: ["CMD", "redis-cli", "-a", "${REDIS_PASSWORD}", "ping"]
      interval: 5s
      timeout: 5s
      retries: 5

  control-plane:
    image: ghcr.io/agent-task-manager/control-plane:latest
    container_name: agent-control-plane
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    environment:
      APP_ENV: production
      PORT: "8080"
      DATABASE_HOST: postgres
      DATABASE_PORT: "5432"
      DATABASE_USER: ${POSTGRES_USER:-agent_user}
      DATABASE_PASSWORD: ${POSTGRES_PASSWORD}
      DATABASE_NAME: ${POSTGRES_DB:-agent_tasks}
      DATABASE_SSLMODE: disable
      REDIS_ADDR: redis:6379
      REDIS_PASSWORD: ${REDIS_PASSWORD}
      SECRET_ENCRYPTION_KEY: ${SECRET_ENCRYPTION_KEY}
      OIDC_ISSUER_URL: ${OIDC_ISSUER_URL}
      OIDC_CLIENT_ID: ${OIDC_CLIENT_ID}
      OIDC_CLIENT_SECRET: ${OIDC_CLIENT_SECRET}
      OIDC_REDIRECT_URL: https://${DOMAIN}/api/v1/auth/callback
      SESSION_SECRET: ${SESSION_SECRET}
      MAX_CONCURRENT_TASKS: ${MAX_CONCURRENT_TASKS:-4}
      TASK_TIMEOUT_MINUTES: ${TASK_TIMEOUT_MINUTES:-60}
      HOST_WORKTREE_ROOT: /srv/agent-tasks/worktrees
      HOST_REPO_ROOT: /srv/agent-tasks/repos
    volumes:
      - "/var/run/docker.sock:/var/run/docker.sock"
      - "/srv/agent-tasks/worktrees:/srv/agent-tasks/worktrees"
      - "/srv/agent-tasks/repos:/srv/agent-tasks/repos"
    networks:
      - ingress
      - backend
    labels:
      - "traefik.enable=true"
      - "traefik.docker.network=agent_ingress"
      - "traefik.http.routers.control-plane.rule=Host(`${DOMAIN}`) && (PathPrefix(`/api`) || PathPrefix(`/ws`))"
      - "traefik.http.routers.control-plane.entrypoints=websecure"
      - "traefik.http.routers.control-plane.tls.certresolver=letsencrypt"
      - "traefik.http.services.control-plane.loadbalancer.server.port=8080"

  web-ui:
    image: ghcr.io/agent-task-manager/web-ui:latest
    container_name: agent-web-ui
    restart: unless-stopped
    networks:
      - ingress
    labels:
      - "traefik.enable=true"
      - "traefik.docker.network=agent_ingress"
      - "traefik.http.routers.web-ui.rule=Host(`${DOMAIN}`)"
      - "traefik.http.routers.web-ui.entrypoints=websecure"
      - "traefik.http.routers.web-ui.tls.certresolver=letsencrypt"
      - "traefik.http.services.web-ui.loadbalancer.server.port=80"
```

### 10.6 Operator Prerequisites & Environment Configuration
Before starting the stack, the host operator must:
1. Ensure Docker Engine (v24.0+) and Docker Compose (v2.20+) are installed.
2. Create host directories with appropriate ownership:
   ```bash
   sudo mkdir -p /srv/agent-tasks/{repos,worktrees}
   sudo chown -R 1000:1000 /srv/agent-tasks
   ```
3. Prepare a root `.env` file containing required credentials:
   ```env
   DOMAIN=tasks.example.com
   ACME_EMAIL=ops@example.com
   POSTGRES_DB=agent_tasks
   POSTGRES_USER=agent_user
   POSTGRES_PASSWORD=generate_secure_random_db_password
   SESSION_SECRET=generate_32_byte_base64_session_key
   MAX_CONCURRENT_TASKS=4
   TASK_LEASE_TIMEOUT_SECONDS=120
   TASK_POLL_INTERVAL_MS=1000
   OIDC_ISSUER_URL=https://accounts.google.com
   OIDC_CLIENT_ID=your-oidc-client-id
   OIDC_CLIENT_SECRET=your-oidc-client-secret
   ```

### 10.7 Backup & Restore Procedures
- **Database Backup (`pg_dump`)**:
  ```bash
  docker compose exec -T postgres pg_dump -U agent_user -d agent_tasks -Fc > backup_agent_tasks_$(date +%Y%m%d_%H%M%S).dump
  ```
- **Database Restore**:
  ```bash
  docker compose stop control-plane
  docker compose exec -T postgres pg_restore -U agent_user -d agent_tasks --clean --if-exists < backup_agent_tasks_20261005_120000.dump
  docker compose start control-plane
  ```

---

## 11. Verification & Testing Strategy

1. **Unit Tests (Go)**:
   * Worktree lifecycle: verify creation, branch isolation, and cleanup.
   * Docker SDK wrapper: mock container creation, volume binding, and capability dropping.
   * Sync provider mocks: verify GitHub and Jira issue parsing, status updates, and PR creation calls.
   * Task state machine: verify valid and invalid transitions across `backlog`, `queued`, `in_progress`, `review`, `done`, `failed`, and `cancelled`.
   * Redis & Asynq task queue scheduling: verify priority queue routing across `critical`, `default`, and `low` queues, task timeouts, retry count bounds, and cancellation via `asynq.Inspector`.
   * Queue concurrency limiter: verify Asynq server strictly caps active concurrent worker executions at `MAX_CONCURRENT_TASKS`.
   * Authorization: verify OIDC identity mapping, team-role permissions, project scoping, CSRF rejection, and WebSocket authorization.
   * GORM model & schema constraints: verify unique indexes (`idx_teams_slug`, `idx_users_issuer_subject`, `uq_project_integrations_external`, `uq_tasks_external_identity`), foreign key cascading, enum CHECK constraints (`status IN ('backlog', 'queued', ...)`), composite index `idx_tasks_project_status_priority`, and JSONB serialization of skills, MCP tool arrays, and integration configs.
2. **Integration Tests**:
   * PostgreSQL 18 & Redis 7 test container lifecycle: execute automated GORM migrations against an ephemeral PostgreSQL 18 and Redis 7 instance via `testcontainers-go` to verify transaction rollbacks, pool connection limits (`sql.DB`), and schema constraints.
   * Distributed Asynq worker concurrency: spin up concurrent Asynq workers claiming jobs from Redis; verify no duplicate task executions, no deadlocks, and that high-priority (`critical`) tasks execute before lower-priority tasks.
   * Immediate queue cancellation: cancel a task in `queued` state; verify Asynq removes the task from Redis and updates PostgreSQL status to `cancelled` with zero container spin-up or worktree allocation.
   * Traefik routing & ingress verification: spin up Traefik in a containerized integration suite to verify TLS termination, HTTP-to-HTTPS redirect rules, reverse proxying to the control plane, and full duplex WebSocket upgrade stability for ANSI streaming.
   * Sandbox isolation: launch a test Docker container executing a mock agent script, stream logs over WebSocket, and verify zero file leaks outside the task worktree.
   * Worktree boundary: verify the container can edit task files without requiring or exposing the host repository's `.git` metadata.
   * Diff snapshotting: verify staged, unstaged, and untracked changes are captured in the review snapshot.
   * Provider sync: mock GitHub & Jira endpoints to verify signature validation, idempotent webhook handling, retries, rate-limit handling, and failure visibility.
   * Controller reconciliation: restart the controller with an active task and verify container/worktree reconciliation and orphan cleanup.
   * Security defense: verify credentials and authorization values do not appear in PostgreSQL, task logs, terminal streams, errors, or persisted worktree files.
3. **Frontend Component Tests**:
   * Kanban drag-and-drop state updates and team switcher.
   * Terminal connection and rendering via `xterm.js`.
   * Git diff rendering with shadcn components.
   * Login, logout, expired-session behavior, role-based action visibility, and unauthorized-response handling.

### 11.1 Acceptance criteria

Implementation is ready for review only when:

* Unauthenticated users cannot access project, task, log, WebSocket, merge, or credential operations.
* A user can access only teams and projects granted by `team_memberships`.
* The OIDC callback rejects invalid issuer, audience, nonce, state, PKCE, signature, and expiry values.
* Repeated provider events do not create duplicate tasks or duplicate comments/PRs.
* Active concurrent Docker containers never exceed configured `MAX_CONCURRENT_TASKS` regardless of inbound task volume.
* Concurrent Asynq queue workers safely dequeue jobs from Redis without race conditions or duplicate executions.
* Stalled or dead workers are detected and recovered via Asynq worker heartbeats and task timeouts within configured limits.
* A task's complete staged, unstaged, and untracked change set is reviewable after container exit.
* Controller restart does not silently lose an active task, worktree, container reference, or provider-sync failure.
* No raw credential or session secret is persisted in PostgreSQL, task logs, WebSocket output, or the task worktree.
* PostgreSQL 18 schema migrations, GORM transactions, and pool constraints execute cleanly with zero data corruption.
* Traefik routes public traffic over TLS and sustains WebSocket streaming connections to `xterm.js` without dropped frames.
* Every security-sensitive test and integration test has a reproducible command and records its exit status.

---

## 12. Next Steps

Upon review and approval of this specification:
1. Proceed to implementation planning using the `writing-plans` workflow.
2. Structure the codebase in `/home/ghug/Code/Go/agent-task-manager`.
3. Scaffold Go backend with GORM v2 and PostgreSQL 18 drivers, Docker sandbox runner, Traefik edge configuration, and React/shadcn frontend.
