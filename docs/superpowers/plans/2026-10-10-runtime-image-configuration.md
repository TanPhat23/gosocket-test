# ⚠️ DO NOT EXECUTE / SUPERSEDED DRAFT — ARCHITECTURE CONFLICT & IMPLEMENTATION BLOCKED

> **CRITICAL NOTICE: DO NOT EXECUTE THIS IMPLEMENTATION PLAN.**
>
> 1. **Fundamental Architectural Conflict:**
>    This implementation plan assumes a **task-scoped container and task-scoped server architecture** (`atm-task-<task_id>`, `/workspace:rw` host worktree bind-mounts, task-scoped OpenCode background server instances). This model directly conflicts with the proposed project-scoped container runtime architecture specified in `docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md`, which defines hard security boundaries strictly at the project level (`atm-project-<project_id>`) with in-container cloning and zero host bind-mounts.
> 2. **Unapproved Spec & Open Design Decisions:**
>    The governing specification (`docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md`) remains in status `PROPOSED Revision (Awaiting User Review & Approval — Not Implemented)`. Multiple major architectural decisions (multi-repo manifest reconciliation, OpenCode server process topology inside project containers, safe git clone credential delivery, bounded writable tmpfs layout, base OS musl/glibc ABI qualification) and implementation blockers remain open and unresolved.
> 3. **Execution Prohibition:**
>    This historical draft must **not** be executed. An aligned implementation plan will be authored only after the proposed project-scoped specification is reviewed, updated, and approved by the user. This addendum is an informational status record and **is not an aligned or executable implementation plan**.
> 4. **Runtime Security Blocker Status (`no-new-privileges`):**
>    - **Default Host Daemon (Failure):** Testing on the target Linux host running Docker Engine 29.8.0 via Snap (rev 3613, containerd 2.3.4, runc 1.5.1) confirmed that minimal isolated containers (`--network none --read-only --cap-drop=ALL --user 65534:65534 --pids-limit 16 --memory 64m --cpus 0.5`) fail execution when `--security-opt no-new-privileges:true` is enabled: `exec /bin/sh: operation not permitted` (exit status 255). An otherwise identical control probe with only NNP omitted succeeded (`NoNewPrivs: 0`, exit status 0).
>    - **Docker Desktop Daemon (Informational VM Pass):** Comparative testing on Docker Desktop 29.2.1 (runc 1.3.4) succeeded with NNP enabled (`NoNewPrivs: 1`, exit status 0). However, Docker Desktop runs inside a separate Linux VM guest kernel; this does **not** prove compatibility or qualify a native Linux Docker Engine deployment or ATM OpenCode runtime.
>    - **Qualification Limits:** The probe localized failure to NNP interaction on the Snap-backed daemon, but did not test workspace mounts, OpenCode server execution, authentication handshakes, provider credentials, or network egress filtering. Dropping NNP, weakening AppArmor, or disabling Seccomp is strictly prohibited. ATM security enforcement requires failing closed.
>
> **Canonical Specification Reference:** `docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md`

---

# Project-Scoped Runtime Image Configuration & Task-Scoped Container Server Architecture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide isolated, reproducible, content-addressed cached per-project runtime environments for Go, Node, Python, Java, Rust, and .NET on `linux/amd64`, paired with task-scoped containerized OpenCode servers, fail-closed execution, zero host fallback, immutable static snapshot persistence (versioned per task with unique monotonically increasing `SnapshotVersion`) for stopped/completed/canceled tasks, and no host dependency installation.

**Architecture:**
1. Task runtime configuration resolves deterministically via `RuntimeResolver` using the worktree manifest (`.atm-runtime.json`), project DB override (`models.Project.RuntimeConfig`), and a strictly vetted 6-runtime catalog qualified from official upstream distribution archives.
2. Images are built dynamically from a control-plane Dockerfile template and cached with content-addressed tags (`atm-runtime:<full_sha256>`) via `RuntimeImageBuilder` using in-flight build coalescing.
3. `TaskSandboxManager` manages container lifecycles on a dedicated user-defined Docker bridge network with `CapDrop: ALL`, `no-new-privileges: true`, resource/PID limits, single-writer state volumes (`atm-task-data-<task_id>`), and task-scoped OpenCode background server instances.
4. An explicit task session broker `TaskSessionBroker` manages active task-scoped `OpenCodeClient` instances (created via `NewTaskOpenCodeClient(endpointURL, token, httpClient)` without host Unix socket interception). All active task interactions—including active transcript reads in `GetTaskSession`, `PromptTaskSession`, `CompactTaskSession`, `CancelTask`, `watchSteeringTurn`, child session polling, and PR/status reconciliation—acquire context-bound leases via `AcquireActiveClient(ctx, taskID)` or `EnsureServer(ctx, taskID)`. No unleased raw client pointers and no global host `openCodeClient` remain.
5. Member follow-up steering or compaction on a stopped task re-resolves the project runtime profile and starts a container server attached to the task worktree and retained volume (`atm-task-data-<task_id>`). A client lease is held during the steering turn; upon lease release, the container stops only when the session turn has completed and no active task execution or other leases remain.
6. Viewer inspection of tasks without an active container (completed, failed, cancelled, review, backlog) reads from immutable snapshots (`models.TaskSessionSnapshot` with unique `(TaskID, SnapshotVersion)`, optional nullable `AttemptID`, and bounded JSON object map `{ "messages": [...] }`) without starting containers or triggering models.
7. Authorization enforces RBAC before reaching `TaskSessionBroker` or configuration APIs: catalog reads require authentication (`RequireAuth`, no team role required as it returns public catalog metadata); snapshot reads require `RoleViewer` (10)+; prompt steering, follow-ups, and cancellations require `RoleMember` (20)+; project runtime overrides require `RoleAdmin` (30) or `RoleOwner` (40).
8. Cancellation initiates a bounded best-effort interrupt of the root session and active child subagents via the task's leased client from `TaskSessionBroker`, captures an immediate transcript snapshot, and bounds force stop (`ContainerStop` timeout fallback to `ContainerRemove`). Retained volumes and latest captured transcripts are preserved, but uncommitted in-container state is not guaranteed to flush if processes hang. Cancellation of one task never interrupts another.

**Tech Stack:** Go 1.27, Docker Engine SDK (`github.com/docker/docker v27.5.1+incompatible`), GORM v2, PostgreSQL/SQLite, Fiber v3, TypeScript, React 19, Tailwind CSS v4, Vitest.

**Spec Reference:** `docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md` (Commit: `d15b9e0`)

---

## Global Constraints

- **Platform Target:** Exclusively `linux/amd64` (per specification baseline).
- **Zero Host Execution:** All compilation, dependency installation, agent turns, child subagents, and tool runs must execute inside the sandbox. `runOnHost` and host-level `InstallDependencies` must be eliminated.
- **Fail-Closed Validation:** Invalid JSON syntax, unknown runtime keys, or uncataloged versions in `.atm-runtime.json` fail the task attempt immediately; UI overrides never forgive or mask an invalid manifest.
- **Strict Content-Addressing:** Image tags are `atm-runtime:<full_sha256>` computed from the canonical normalized JSON of target platform, base image digest, template revision, native package checksums, OpenCode binary checksum, and the 6 resolved runtimes.
- **Daemon-Validated Internal Routing Only & No Global Host Client:** `OpenCodeClient` endpoints for active tasks must be server-owned mappings (`taskID -> containerID, networkID, observed internal IP:port, generation`) derived exclusively from Docker daemon inspection on the task network. Instances must be created using `NewTaskOpenCodeClient(endpointURL, token, httpClient)` to guarantee no host Unix socket hijacking occurs. Arbitrary user-supplied `serverEndpoint` values are strictly prohibited. IP string validation alone is NOT declared sufficient for SSRF defense.
- **Package Layering & Acyclic Broker Architecture:** `internal/runner` defines `TaskSandboxManager`, `TaskSessionBroker`, `ClientLease`, and `NewTaskOpenCodeClient`. `internal/service` imports `internal/runner` to consume `TaskSessionBroker`. `internal/runner` does NOT import `internal/service`. Wiring is performed in `cmd/server/main.go`. All service callsites route through leases (`AcquireActiveClient` or `EnsureServer`); raw unleased client pointers are prohibited to prevent races with container stop or route teardown.
- **Container Network Topology & Host Privileges:**
  - Sandbox containers execute on a dedicated user-defined Docker bridge network without published host ports, with outbound NAT for LLM provider APIs and package downloads.
  - Sandbox containers run without `/var/run/docker.sock` and with dropped privileges (`CapDrop: ALL`, `no-new-privileges: true`).
  - Host control-plane runner still requires access to `/var/run/docker.sock` to manage containers.
  - Mount no host user config, shared state, or host credentials into the container (`no /root/.config/opencode` or `/root/.local/share/opencode` host mounts).
- **Deterministic Single-Writer Volumes & Immutable Snapshots:** Dedicated Docker volume `atm-task-data-<task_id>` per task session; retained after completion for follow-up turns. Historical transcript inspection for tasks without active containers must be served from persisted `TaskSessionSnapshot` records (keyed by unique composite `(task_id, snapshot_version)`), not by invoking dead container endpoints or triggering models. Because multiple steering turns can share an attempt, snapshots are immutable, versioned per task monotonically, serialized transactionally per task, never upserted or overwritten, and store an optional nullable `attempt_id` for attribution.
- **RBAC & Multi-Tenant Authorization:**
  - Catalog Metadata (`GET /api/v1/runtimes/catalog`): Protected by `RequireAuth` (authenticated session required, no team role needed; tests verify 401 unauthenticated and 200 authenticated).
  - Snapshot Reads: `RoleViewer` (10)+ (static snapshot only; never triggers model invocation).
  - Steer Prompts, Follow-ups, Cancellations: `RoleMember` (20)+.
  - Project Runtime Overrides (`GET/PUT /api/v1/projects/:id/runtime`): Read requires `RoleViewer` (10)+, update requires `RoleAdmin` (30)+ or `RoleOwner` (40).
  - All project operations must resolve project team membership and enforce cross-team IDOR isolation.
- **Preservation of Cancellation Work:** Preserve the existing 9 dirty working tree files from task cancellation (`internal/runner/task_runner.go`, `internal/runner/opencode_client.go`, etc.). Do not overwrite or discard their interruption and child session traversal logic.
- **Code Quality:** All Go code must pass `make check` (`go vet`, `staticcheck`, and unit tests). All TypeScript/React code must pass `npm test --prefix web -- --run` and `npx tsc -b`.

---

## Review Focus

1. **Host Fallback Elimination:** Verify that no code path in `task_runner.go` or `dependencies.go` invokes commands, tool runs, or package installations on the host. Container initialization failure must fail the attempt fail-closed.
2. **SSRF & Network Boundary Defense:** Verify `OpenCodeClient` routes only via server-managed endpoints belonging to the daemon-inspected task bridge network. Ensure the container has no access to host docker.sock or host control-plane loopback services, and ensure client instances for tasks do not fall back to host socket discovery (`opencode.sock`).
3. **Session Broker Leased Routing & Service Isolation:** Verify that `TaskService` routes all live operations (GetTaskSession active reads, Prompt, Compact, Cancel, watchSteeringTurn, child session polling, PR/status reconciliation) exclusively through leased broker calls (`AcquireActiveClient` or `EnsureServer`) for the specific task ID, completely removing the single global host `openCodeClient` in service options. Verify cancellation of Task A never interrupts Task B.
4. **Member Follow-up vs. Viewer Static Reads:** Verify that when a member issues a steer prompt/follow-up on a stopped task, the broker ensures a container server is started with the retained volume and active worktree. Verify that a viewer reading session transcripts never starts a container server or triggers model execution.
5. **Snapshot Persistence & Schema Type Safety:** Verify that `TaskSessionSnapshot` uses a composite unique constraint on `(TaskID, SnapshotVersion)` with monotonically increasing `SnapshotVersion`, an optional nullable `AttemptID` for attribution, embeds messages within a valid `JSONMap` object `{ "messages": [...] }`, enforces bounded byte limits (max 10MB; sets `IsComplete = false` if capped or truncated), prevents any update/upsert of prior snapshots, orders `GetLatestSnapshot` by version descending, and ensures capture failures during cancellation never block or delay container force stop.
6. **Cancellation Semantics:** Verify cancellation implements bounded best-effort interruption and transcript snapshotting with a bounded force-stop timeout fallback. Do not claim flushed state or graceful termination is guaranteed if an in-container process hangs.
7. **Volume Hygiene & Purging Lifecycle:** Verify volume retention and purge policy gates.
8. **Secret Defense:** Verify no host credentials, host configs, or raw API tokens are written into container images or logged. Verify the secret source gate before implementing runner container startup.
9. **Concurrent Build Coalescing:** Verify that concurrent tasks requesting the identical runtime hash share a single in-flight build channel without duplicate builds.

---

## Pre-Implementation Architecture Decision Gates

> **IMPORTANT:** Tasks 1 through 9 cannot proceed to production code until each gate below is explicitly qualified and resolved. Tasks depending on unresolved gates must remain blocked.

### Gate A: Runtime Catalog Sourcing & Candidate Review
- **Status:** Mandatory Pre-Implementation Gate (Open / Unresolved).
- **Current Baseline Fact:** Live inspection of the existing environment confirms Go 1.27.1, Node 18.20.4, and Python 3.11.2 on `linux/amd64`. These versions represent the observed baseline but are not yet a fully qualified distribution catalog.
- **Requirements:**
  1. For all six language runtimes (`go`, `node`, `python`, `java`, `rust`, `dotnet`), identify official vendor-maintained `linux/amd64` distribution archives, official release checksums, license, architecture, exact version, and EOL/security support status.
  2. All downloads and checksum sources must use official HTTPS endpoints; verify checksums cryptographically before extracting or executing any archive. No downloaded data or archives may be stored in the repository.
  3. Perform qualification discovery inside a disposable execution container staged with the verified archives. Run qualification with `--network none`, no host directory or socket mounts, `--security-opt=no-new-privileges`, dropped capabilities (`--cap-drop=ALL`), non-root user execution, and strict memory/CPU resource limits.
  4. Require actual toolchain build/test smoke verification for each verified archive, not just `--version`:
     - Go: hello-world compile and run (`go build` / `go run`).
     - Node: syntax validation with `node --check` and script execution (`node -e`).
     - Python: bytecode compilation with `python3 -m py_compile` and script execution (`python3 -c`).
     - Java: source compilation with `javac` and class execution with `java`.
     - Rust: single-file compilation with `rustc` and binary execution (or `cargo` offline test).
     - .NET: local console build and execution (`dotnet build --no-restore` / `dotnet run --no-restore`) without external package restore.
  5. EOL Baseline Governance: Check official EOL / security support status against release dates. If an observed baseline version (e.g., Node 18.20.4 or Python 3.11.2) is unsupported or past security EOL by the target release date, present recommended upgrade candidates and explicitly obtain user approval to change the baseline. Do NOT silently make EOL versions selectable or silently change the baseline.
  6. Explicitly distinguish between candidate versions (unverified/pending approval) and user-approved catalog entries. Maintain user approval gate after all discovery findings before codifying the catalog in Task 1.
  7. **Strict Ban:** No fabricated, placeholder, or estimated SHA256 hashes, digests, or archive URLs may be committed to code or tests.

### Gate B: OpenCode V2 Binary Auth & Container Handshake Qualification
- **Status:** Mandatory Pre-Implementation Gate (Open / Unresolved).
- **Current Baseline Fact:** Local binary is `opencode v2.0.23`. `opencode serve --help` exposes `--hostname string`, `--port integer`, `--cors string`, `--service`, and `--stdio`. No flags for credential authentication or handshake tokens are documented on CLI help.
- **Requirements:**
  1. Inspect the pinned OpenCode v2 binary and test `opencode serve` behavior locally to determine if it supports an HTTP authorization header, bearer token, API key, or internal authentication mechanism.
  2. If OpenCode v2 provides an authentication mechanism, specify the exact handshake contract between `OpenCodeClient` and the task container.
  3. If OpenCode v2 does NOT provide safe container-level authentication, document the confinement model: the server listens exclusively on the task-dedicated internal container network (with no published host port) and is accessible only to the control plane runner. If this is insufficient, halt and require coordinator/user approval before exposing the container network.
  4. Qualify task routing handles: the control-plane runner must maintain an internal mapping (`taskID -> {containerID, networkID, observedInternalIP, port, generation}`) strictly derived from Docker daemon inspection. Reject any arbitrary or user-supplied endpoints. IP string validation alone is NOT declared sufficient for SSRF defense.

### Gate C: Task State Volume Retention & Task Deletion Operation
- **Status:** Mandatory Pre-Implementation Decision Gate (Open / Unresolved).
- **Current Baseline Fact:** Codebase audit reveals NO task delete route, service method, or repository method currently exists (`grep DeleteTask` matches only the Asynq queue inspector).
- **Unresolved Decision Needed Before Implementation:**
  - **Option 1 (Recommended):** Add an authenticated, authorized task deletion operation (`DELETE /api/v1/tasks/:id`, requiring `RoleAdmin` or `RoleOwner` of the task's project team) that cancels any active task, removes its records, and purges its dedicated Docker volume `atm-task-data-<task_id>`.
  - **Option 2:** Defer task deletion endpoint to a future specification; retain `atm-task-data-<task_id>` volumes under a manual operator retention/cleanup CLI or script.
- **Constraint:** This is a blocking user decision gate. The plan MUST NOT claim to implement or execute a task deletion route until the user explicitly resolves Gate C. If Option 2 is selected, Task 6 will omit task deletion and rely on manual volume retention.

### Gate D: Model Provider Credential Source Qualification
- **Status:** Mandatory Pre-Implementation Gate (Open / Unresolved).
- **Current Baseline Fact:** `internal/models/enums.go` defines `CredentialProvider` only as `github`, `jira`, `trello`, `oidc`, and `generic`. The current runner injects environment variables by reading host environment / host configs (`/root/.config/opencode`). No team-level or project-level LLM provider credential storage currently exists in ATM.
- **Requirements:**
  1. Identify the approved LLM provider credential source for tasks (e.g., control-plane environment variables passed down via task execution context vs. dedicated secure secret store).
  2. Verify how the pinned OpenCode server inside the container consumes credentials (e.g., standard provider env vars `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) without persisting host secrets or mounting host configuration directories.
  3. Never claim encrypted team LLM credentials already exist in the database. If a new credential-management subsystem is required, stop and ask the user before writing code.
  4. Never print or log secrets in tests or runner output. Route security review for Docker configuration and secret isolation.

---

## Detailed Task Implementation Order

### Task 0: Catalog Qualification & Discovery (Architecture Prerequisite)

**Goal:** Qualify vendor distribution archives for all 6 language runtimes on `linux/amd64` with grounded smoke testing, and test OpenCode serve handshake with strict isolation and official endpoint probing.

**Files:**
- Discovery & Qualification only (no code edits, no code commits, no downloaded data in repo).
- Output: Candidate catalog specification with verified vendor URLs, verified SHA256 checksums, build/smoke test results, EOL status notes, and OpenCode auth probe results ready for user approval.

- [ ] **Step 1: Discover, stage, and smoke-test upstream archives for 6 runtimes**
  - Download official release artifacts and checksum manifests via official HTTPS endpoints for Go, Node.js, Python, Java (OpenJDK), Rust, and .NET on `linux/amd64`. Do not commit downloaded artifacts to the repo.
  - Verify sha256sum on downloaded archives strictly against vendor-published checksums before extracting or executing.
  - Stage verified archives into a disposable container running under strict isolation: `--network none`, no host mounts or Docker socket, `--security-opt=no-new-privileges`, `--cap-drop=ALL`, non-root user, and explicit CPU/memory limits.
  - Record architecture (`linux/amd64`), exact version, license, and EOL/security support status for each runtime candidate.
  - Execute concrete toolchain build and run smoke tests inside the disposable container (not just `--version`):
    - Go: hello-world source compile and run (`go build` / `go run`).
    - Node: syntax validation with `node --check` and execution (`node -e`).
    - Python: bytecode compilation with `python3 -m py_compile` and execution (`python3 -c`).
    - Java: source compilation with `javac` and execution with `java`.
    - Rust: single-source compilation with `rustc` and binary execution (or `cargo` offline).
    - .NET: local console build and run (`dotnet build --no-restore` / `dotnet run --no-restore`) without external network package restore.
  - If existing observed versions (e.g. Node 18.20.4 or Python 3.11.2) are past security EOL or unsupported by release date, prepare upgrade candidates and require explicit user approval to change baseline—do not silently make EOL selectable or silently modify the baseline.

- [ ] **Step 2: Grounded OpenCode serve probe and confinement verification**
  - Ground discovery in official OpenCode V2 OpenAPI specification (`/api/info` operation `server.info` and `/api/session` declare `security: []`). Do NOT use invented endpoints such as `/global/health`.
  - Check local CLI flags (`opencode serve --help` exposes `--hostname`, `--port`, `--cors`, `--service`, `--stdio`; no auth flags exist). Do NOT invent unsupported configuration such as `OPENCODE_SERVER_PASSWORD`.
  - Isolate probe environment completely: set temporary throwaway directories for `HOME`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, and `XDG_STATE_HOME`. Do not mount or expose host configuration, data, or credentials. Do not execute model prompts or model API calls.
  - Bind listener strictly to `127.0.0.1` for auth probe (e.g., `opencode serve --hostname 127.0.0.1 --port <port>`).
  - Probe endpoints: query `GET /api/info` and `GET /api/session`:
    1. Send request without Authorization header.
    2. Send request with dummy irrelevant Authorization header (e.g., `Authorization: Bearer test-dummy`).
    3. Redact all response payload tokens/credentials in probe logs.
  - If probe confirms API is unauthenticated (matching OpenAPI `security: []`), document outcome as unauthenticated and rely strictly on network isolation:
    - Container listener must bind only internal container interface on a task-dedicated Docker network with no published host port, accessible exclusively by the control-plane runner.
    - If task-dedicated network isolation is deemed insufficient, explicitly halt and ask user for guidance before exposing listener or proceeding.

- [ ] **Step 3: User Approval Gate (Gate A & Baseline Sign-Off)**
  - Present the qualified catalog candidate table (runtimes, versions, verified URLs, verified SHA256 checksums, smoke test results, EOL status, candidate vs user-approved status) and OpenCode auth probe results.
  - If EOL baseline upgrades are recommended (e.g. Node or Python), explicitly obtain user approval to change the baseline.
  - Keep Gate A approval strictly enforced after presenting findings before moving to Task 1 implementation.

---

### Task 1: Runtime Model, Migration & Catalog Interface

**Goal:** Extend `models.Project` with `RuntimeConfig JSONMap`, update migrations, define catalog data structures, and implement the approved catalog.

**Files:**
- Modify: `internal/models/models.go`
- Modify: `internal/models/models_test.go`
- Modify: `cmd/server/main.go`
- Modify: `cmd/migrate/main.go`
- Create: `internal/runner/runtime_types.go`
- Create: `internal/runner/runtime_catalog.go`
- Create: `internal/runner/runtime_catalog_test.go`

**Interfaces:**
```go
type RuntimeConfigMap map[string]string // runtime -> version or "false"

type ResolvedRuntimeProfile struct {
    Platform       string            `json:"platform"`
    BaseImageRef   string            `json:"base_image_ref"`
    TemplateRev    string            `json:"template_rev"`
    Runtimes       map[string]string `json:"runtimes"` // "go", "node", "python", "java", "rust", "dotnet"
    OpenCodeSHA256 string            `json:"opencode_sha256"`
}

type RuntimeCatalogEntry struct {
    Runtime     string `json:"runtime"`
    Version     string `json:"version"`
    ArchiveURL  string `json:"archive_url"`
    SHA256      string `json:"sha256"`
    ExtractPath string `json:"extract_path"`
}

type RuntimeCatalog interface {
    Get(runtime, version string) (*RuntimeCatalogEntry, bool)
    GetBaseline(runtime string) (string, bool)
    List() []RuntimeCatalogEntry
}
```

- [ ] **Step 1: Write failing tests for models.Project RuntimeConfig and RuntimeCatalog**
  - Create `internal/runner/runtime_catalog_test.go` testing baseline retrieval, specific version lookup, and rejection of uncataloged versions.
  - Add test in `internal/models/models_test.go` for `Project.RuntimeConfig` JSON serialization.
- [ ] **Step 2: Run focused test to verify failure**
  - Run: `go test -v ./internal/runner -run TestRuntimeCatalog`
  - Expected: FAIL (`undefined: runner.NewDefaultRuntimeCatalog`)
- [ ] **Step 3: Implement Runtime types, catalog, and Project model update**
  - Update `internal/models/models.go`: add `RuntimeConfig JSONMap gorm:"type:jsonb;not null;default:'{}'" json:"runtime_config"` to `Project`.
  - Verify GORM AutoMigrate paths in both `cmd/server/main.go` and `cmd/migrate/main.go`.
  - Implement `internal/runner/runtime_types.go` and `internal/runner/runtime_catalog.go` using user-approved catalog entries from Gate A.
- [ ] **Step 4: Run focused tests to verify green**
  - Run: `go test -v ./internal/runner -run TestRuntimeCatalog`
  - Run: `go test -v ./internal/models -run TestProject`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Scope: `internal/models/`, `internal/runner/runtime_*`, `cmd/`

---

### Task 2: Deterministic Manifest & Override Resolver

**Goal:** Parse `.atm-runtime.json`, merge with project DB overrides, enforce fail-closed validation, and compute content-addressed SHA256 cache tags.

**Files:**
- Create: `internal/runner/runtime_resolver.go`
- Create: `internal/runner/runtime_resolver_test.go`

**Interfaces:**
```go
type RuntimeResolver interface {
    Resolve(ctx context.Context, worktreePath string, project *models.Project) (*ResolvedRuntimeProfile, error)
    ComputeCacheTag(profile *ResolvedRuntimeProfile) (string, error)
}
```

- [ ] **Step 1: Write unit tests for Manifest parsing, precedence, fail-closed handling, and hashing**
  - Create `internal/runner/runtime_resolver_test.go`:
    - Test 1: Empty repo without manifest -> defaults to baseline (`go`, `node`, `python` baseline; others `"false"`).
    - Test 2: Valid `.atm-runtime.json` -> overrides baseline.
    - Test 3: Project DB override takes precedence over manifest.
    - Test 4: Malformed JSON or schemaVersion != 1 in `.atm-runtime.json` -> fails closed with error immediately (UI override does not forgive malformed manifest).
    - Test 5: Unknown runtime key or uncataloged version -> fails closed with error.
    - Test 6: Deterministic tag calculation produces `atm-runtime:<64_hex_chars>`.
- [ ] **Step 2: Run focused test to verify failure**
  - Run: `go test -v ./internal/runner -run TestRuntimeResolver`
  - Expected: FAIL (`undefined: runner.NewRuntimeResolver`)
- [ ] **Step 3: Implement RuntimeResolver**
  - Implement `internal/runner/runtime_resolver.go`.
  - Normalize keys in sorted order, encode to canonical JSON string, and compute SHA256.
- [ ] **Step 4: Run focused tests to verify green**
  - Run: `go test -v ./internal/runner -run TestRuntimeResolver`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Scope: `internal/runner/runtime_resolver*`

---

### Task 3: Docker Sandbox Client Extension & Deterministic Image Builder

**Goal:** Extend `sandbox.DockerClient` to support required image/network/volume operations strictly matching `github.com/docker/docker v27.5.1+incompatible` types, and implement `RuntimeImageBuilder` with in-flight build coalescing.

**Files:**
- Modify: `internal/sandbox/docker.go`
- Modify: `internal/sandbox/docker_test.go`
- Create: `internal/runner/image_builder.go`
- Create: `internal/runner/image_builder_test.go`
- Create: `deploy/sandbox/Dockerfile.template`

**Required DockerClient Operations (Strictly verified against pinned Docker SDK v27.5.1 packages):**
```go
type DockerClient interface {
    // Existing operations:
    ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
    ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
    ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error)
    ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)
    ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
    ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
    ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)

    // Extended operations strictly matched to github.com/docker/docker v27.5.1:
    // Package "github.com/docker/docker/api/types": types.ImageBuildOptions, types.ImageBuildResponse, types.ImageInspect, types.ContainerJSON
    // Package "github.com/docker/docker/api/types/image": image.ListOptions, image.Summary
    // Package "github.com/docker/docker/api/types/network": network.CreateOptions, network.CreateResponse, network.InspectOptions, network.Inspect
    // Package "github.com/docker/docker/api/types/volume": volume.CreateOptions, volume.Volume
    ImageBuild(ctx context.Context, context io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error)
    ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error)
    ImageInspectWithRaw(ctx context.Context, image string) (types.ImageInspect, []byte, error)
    NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
    NetworkInspect(ctx context.Context, network string, options network.InspectOptions) (network.Inspect, error)
    NetworkRemove(ctx context.Context, network string) error
    VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
    VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error)
    VolumeRemove(ctx context.Context, volumeID string, force bool) error
    ContainerInspect(ctx context.Context, container string) (types.ContainerJSON, error)
}
```

- [ ] **Step 1: Write unit tests for DockerClient extensions and ImageBuilder build coalescing**
  - Create `internal/runner/image_builder_test.go`:
    - Test 1: Cached image tag found via `ImageList` -> returns cached tag without building.
    - Test 2: Uncached image tag -> builds tar context from template, invokes `ImageBuild`, returns tag.
    - Test 3: Multiple concurrent requests for same profile share single in-flight build channel.
    - Test 4: Detaching 1 cancelled waiter does not cancel build if other waiters remain.
- [ ] **Step 2: Run focused test to verify failure**
  - Run: `go test -v ./internal/runner -run TestRuntimeImageBuilder`
  - Expected: FAIL
- [ ] **Step 3: Update sandbox.DockerClient and implement RuntimeImageBuilder**
  - Update `internal/sandbox/docker.go` to declare exact Docker SDK v27.5.1 method signatures as specified above.
  - Update `internal/sandbox/NewDefaultHostConfig` to accept network and ensure `CapDrop: ALL`, `no-new-privileges: true`, CPU/mem/PID limits are configured.
  - Implement `deploy/sandbox/Dockerfile.template` with pinned base digest and non-root user setup.
  - Implement `internal/runner/image_builder.go`.
- [ ] **Step 4: Run focused tests to verify green**
  - Run: `go test -v ./internal/sandbox -run TestDocker`
  - Run: `go test -v ./internal/runner -run TestRuntimeImageBuilder`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Scope: `internal/sandbox/`, `internal/runner/image_builder*`, `deploy/sandbox/`

---

### Task 4: Task Sandbox Lifecycle Manager & Task Session Broker

**Goal:** Implement `TaskSandboxManager` and `TaskSessionBroker` to manage task container lifecycles, daemon-derived route mapping, and strictly context-bound leased client access for both runner and service operations.

**Files:**
- Create: `internal/runner/task_sandbox_manager.go`
- Create: `internal/runner/task_sandbox_manager_test.go`
- Create: `internal/runner/task_session_broker.go`
- Create: `internal/runner/task_session_broker_test.go`

**Interfaces:**
```go
type TaskRouteHandle struct {
    TaskID       string `json:"task_id"`
    ContainerID  string `json:"container_id"`
    NetworkID    string `json:"network_id"`
    IPAddress    string `json:"ip_address"`
    Port         int    `json:"port"`
    EndpointURL  string `json:"endpoint_url"`
    Generation   int64  `json:"generation"`
}

type TaskSandboxManager interface {
    StartTaskServer(ctx context.Context, task *models.Task, attempt *models.TaskAttempt, imageTag string, env []string) (*TaskRouteHandle, error)
    StopTaskServer(ctx context.Context, handle *TaskRouteHandle, gracePeriod time.Duration) error
    PurgeTaskVolume(ctx context.Context, taskID string) error
}

type ClientLease interface {
    Client() OpenCodeClient
    Release() error
}

type TaskSessionBroker interface {
    // AcquireActiveClient provides lease-counted access to an existing running container.
    // Returns (lease, true, nil) if active, (nil, false, nil) if no active server exists.
    AcquireActiveClient(ctx context.Context, taskID string) (ClientLease, bool, error)

    // EnsureServer acquires an existing running server or starts a new container server attached to the task worktree
    // and retained volume (re-resolving runtime profile), returning a leased client.
    EnsureServer(ctx context.Context, taskID string) (ClientLease, error)

    // RegisterActive registers an active container server started by the runner.
    RegisterActive(handle *TaskRouteHandle, client OpenCodeClient)

    // UnregisterActive unregisters and stops the active container server for the task once active turns conclude.
    UnregisterActive(ctx context.Context, taskID string, gracePeriod time.Duration) error
}
```

- [ ] **Step 1: Write unit tests for TaskSandboxManager and TaskSessionBroker**
  - Create `internal/runner/task_sandbox_manager_test.go`:
    - Test 1: Container configuration applies `CapDrop: ["ALL"]`, `SecurityOpt: ["no-new-privileges:true"]`, `Memory: 4GB`, `NanoCPUs: 2e9`, and dedicated bridge network.
    - Test 2: Binds host worktree at `/workspace:rw` and dedicated volume `atm-task-data-<task_id>` at `/root/.local/share/opencode:rw`. No host configs or host docker.sock mounted.
    - Test 3: Daemon inspection extracts internal IP; returns validated `TaskRouteHandle`.
    - Test 4: Bounded stop timeout initiates `ContainerStop` then falls back to `ContainerRemove`.
    - Test 5: `PurgeTaskVolume` calls `VolumeRemove(ctx, "atm-task-data-<task_id>", true)`.
  - Create `internal/runner/task_session_broker_test.go`:
    - Test 1: Two active tasks have distinct, isolated endpoints; operations on Task A route strictly to Task A container and never Task B.
    - Test 2: `AcquireActiveClient` returns `nil, false, nil` when task container is stopped.
    - Test 3: `EnsureServer` starts container attached to retained volume and worktree, returning lease.
    - Test 4: `Release` on lease decrements count; container stops only after session turn ends, no active task execution remains, and lease count reaches 0.
    - Test 5: Boundary test: concurrent live read / cancel versus container teardown does not panic or use stale handle.
- [ ] **Step 2: Run focused tests to verify failure**
  - Run: `go test -v ./internal/runner -run TestTaskSandboxManager`
  - Run: `go test -v ./internal/runner -run TestTaskSessionBroker`
  - Expected: FAIL
- [ ] **Step 3: Implement TaskSandboxManager and TaskSessionBroker**
  - Implement `internal/runner/task_sandbox_manager.go`.
  - Implement `internal/runner/task_session_broker.go` with mutex-protected active handles map `map[string]*activeTaskEntry`.
  - Ensure volume creation, network connectivity, and container configuration are applied.
  - Enforce daemon-validated routing handle and instantiate client using `NewTaskOpenCodeClient`.
- [ ] **Step 4: Run focused tests to verify green**
  - Run: `go test -v ./internal/runner -run TestTaskSandboxManager`
  - Run: `go test -v ./internal/runner -run TestTaskSessionBroker`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Scope: `internal/runner/task_sandbox_manager*`, `internal/runner/task_session_broker*`

---

### Task 5: AgentTaskRunner & TaskService Integration: Broker Leases & Versioned Immutable Snapshots

**Goal:** Eliminate `runOnHost` and host `InstallDependencies`. Add isolated `NewTaskOpenCodeClient`. Implement versioned immutable static snapshots `TaskSessionSnapshot` with composite unique `(TaskID, SnapshotVersion)` and optional nullable `AttemptID` attribution, ensuring multiple steering turns sharing an attempt create distinct versions without overwriting. Replace global host `openCodeClient` in `TaskService` with injected `TaskSessionBroker` using context-bound leases for all operations.

**Files:**
- Modify: `internal/runner/opencode_client.go`
- Modify: `internal/runner/opencode_client_test.go`
- Modify: `internal/models/task_models.go`
- Modify: `internal/models/task_models_test.go`
- Modify: `internal/repository/task_repository.go`
- Modify: `internal/repository/task_repository_test.go`
- Modify: `internal/service/task_service.go`
- Modify: `internal/service/task_service_test.go`
- Modify: `internal/runner/task_runner.go`
- Modify: `internal/runner/task_runner_test.go`
- Modify: `internal/runner/dependencies.go`
- Modify: `internal/runner/dependencies_test.go`
- Modify: `cmd/server/main.go`
- Modify: `cmd/migrate/main.go`

**Model Definition & Client Constructor:**
```go
// TaskSessionSnapshot stores immutable transcript snapshots versioned monotonically per task.
// Unique constraint on (task_id, snapshot_version) supports multiple follow-up turns sharing
// an attempt without collisions or overwrites. Prior snapshots are strictly immutable (no updates/upserts).
// AttemptID is an optional nullable UUID for attribution.
// Messages are stored inside a JSONMap object {"messages": [...]}.
type TaskSessionSnapshot struct {
    CreatedAt       time.Time `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
    UpdatedAt       time.Time `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
    ID              string    `gorm:"type:uuid;primaryKey" json:"id"`
    TaskID          string    `gorm:"type:uuid;not null;uniqueIndex:idx_session_snapshot_version,priority:1" json:"task_id"`
    SnapshotVersion int64     `gorm:"type:bigint;not null;uniqueIndex:idx_session_snapshot_version,priority:2" json:"snapshot_version"`
    AttemptID       *string   `gorm:"type:uuid;index:idx_session_snapshot_attempt" json:"attempt_id,omitempty"`
    SessionID       string    `gorm:"type:text;not null" json:"session_id"`
    Status          string    `gorm:"type:text;not null" json:"status"`
    Model           string    `gorm:"type:text;not null" json:"model"`
    Cost            float64   `gorm:"type:numeric" json:"cost"`
    Tokens          JSONMap   `gorm:"type:jsonb;not null;default:'{}'" json:"tokens"`
    Data            JSONMap   `gorm:"type:jsonb;not null;default:'{}'" json:"data"` // {"messages": [...]}
    IsComplete      bool      `gorm:"type:boolean;not null;default:true" json:"is_complete"` // false if truncated during forced stop or 10MB cap
    ByteSize        int64     `gorm:"type:bigint;not null;default:0" json:"byte_size"`
}

// NewTaskOpenCodeClient creates an isolated client strictly bound to the container TCP endpoint.
// It bypasses host Unix socket auto-discovery (/home/ghug/.local/share/opencode/opencode.sock, /tmp/opencode.sock)
// and rejects loopback fallbacks to eliminate host interception and SSRF escape.
func NewTaskOpenCodeClient(endpointURL, token string, httpClient *http.Client) OpenCodeClient
```

- [ ] **Step 1: Write tests for NewTaskOpenCodeClient, Snapshot storage, and Broker-based TaskService**
  - In `internal/runner/opencode_client_test.go`:
    - Test `NewTaskOpenCodeClient` strictly uses provided `endpointURL`, disables redirects, and NEVER attempts unix socket dial even if socket files exist on host filesystem.
  - In `internal/models/task_models_test.go`:
    - Test `TaskSessionSnapshot` composite unique index `(task_id, snapshot_version)`.
    - Test message encapsulation inside object map `{ "messages": [...] }` conforming to `JSONMap`.
    - Test byte size truncation check (limit to 10MB per snapshot; marks `IsComplete = false` and flags truncated if capped).
    - Test nullable optional `AttemptID` field validation and attribution.
  - In `internal/repository/task_repository_test.go`:
    - Test `SaveSnapshot`: multiple steering turn snapshots within the same attempt allocate separate monotonically increasing versions (e.g. 1, 2) without overwriting or updating prior versions.
    - Test immutability: attempts to update/upsert existing `(task_id, snapshot_version)` fail or are rejected.
    - Test concurrent captures for the same task serialize transactionally and allocate unique strictly increasing versions without collisions.
    - Test `GetLatestSnapshot(taskID)` returns the snapshot with the highest `snapshot_version`.
  - In `internal/service/task_service_test.go`:
    - Test `GetTaskSession` on a task with NO active container (completed, failed, review, cancelled): reads latest persisted `TaskSessionSnapshot` via `GetLatestSnapshot` without invoking broker or container. Verify zero model calls and zero container startup.
    - Test `GetTaskSession` on active task: calls `broker.AcquireActiveClient(ctx, taskID)`, uses client to fetch live messages, releases lease.
    - Test `PromptTaskSession` on stopped task: calls `broker.EnsureServer`, issues prompt, passes lease to `watchSteeringTurn`, and releases lease when turn finishes (triggering container stop).
    - Test multiple steering turns under `PromptTaskSession` within the same attempt: verify each completed turn captures a separate new snapshot version and prior snapshot versions remain unchanged.
    - Test `CancelTask`: calls `broker.AcquireActiveClient(ctx, taskID)`; if active, calls `InterruptSessionTree` and releases lease. Verify canceling Task A does not interrupt Task B.
    - Test `CompactTaskSession`: calls `broker.AcquireActiveClient(ctx, taskID)` (or `EnsureServer`) to compact session and releases lease.
    - Test PR/status/session reconciliation paths: all callsites (`GetSessionMessages`, `ListChildSessions`, `GetSession`) acquire task lease via broker and release immediately.
    - Test that `TaskService` option `WithTaskSessionBroker` replaces `WithTaskSessionDependencies(openCodeClient, ...)`, removing any global host client.
  - In `internal/runner/task_runner_test.go`:
    - Test task completion and cancellation capture static snapshot and store in database before container shutdown.
    - Test that snapshot failure or timeout during cancellation does NOT block container force-stop; sets `IsComplete = false`, preserves partial transcript, and proceeds.
    - Test `TestAgentTaskRunner_NoHostFallbackOnContainerFailure`: verifies zero host fallback.
- [ ] **Step 2: Run focused tests to verify failures**
  - Run: `go test -v ./internal/runner -run TestTaskOpenCodeClient`
  - Run: `go test -v ./internal/service -run TestTaskService_BrokerRouting`
  - Expected: FAIL
- [ ] **Step 3: Implement Isolated Client, Snapshot Repository & Wire Broker in Service**
  - Implement `NewTaskOpenCodeClient` in `internal/runner/opencode_client.go`.
  - Add `TaskSessionSnapshot` to `internal/models/task_models.go`, update `cmd/server/main.go` and `cmd/migrate/main.go` `AutoMigrate` lists.
  - Add `SaveSnapshot(ctx, snapshot)` and `GetLatestSnapshot(ctx, taskID)` to `TaskRepository`:
    - `SaveSnapshot` executes inside a database transaction using dialect-safe row-level serialization to guarantee strictly increasing, collision-free versions without invalid aggregate locks (rejecting updates/upserts of prior snapshots):
      - Under PostgreSQL: Lock the parent task row first (`SELECT id FROM tasks WHERE id = ? FOR UPDATE`), then query `SELECT COALESCE(MAX(snapshot_version), 0) + 1 FROM task_session_snapshots WHERE task_id = ?` (without `FOR UPDATE` on the aggregate, preventing `SQLSTATE 42809`), and insert the new snapshot row with the allocated `snapshot_version`.
      - Under SQLite (tests): Execute inside an immediate transaction (`BEGIN IMMEDIATE`), select `COALESCE(MAX(snapshot_version), 0) + 1`, and insert the snapshot row; unique composite index `(task_id, snapshot_version)` guarantees conflict rejection and retry safety if serialized concurrently.
      - Add a dedicated repository test with concurrent captures against the same task ID to prove unique monotonically increasing versions without collisions.
    - `GetLatestSnapshot` executes `SELECT * FROM task_session_snapshots WHERE task_id = ? ORDER BY snapshot_version DESC LIMIT 1`.
  - In `internal/service/task_service.go`:
    - Replace `s.openCodeClient` with `s.sessionBroker runner.TaskSessionBroker`.
    - Update option: `WithTaskSessionBroker(broker runner.TaskSessionBroker, projectRepo repository.ProjectRepository, teamRepo repository.TeamRepository)`.
    - In `GetTaskSession`: attempt `broker.AcquireActiveClient(ctx, taskID)`. If active lease acquired, defer `lease.Release()`, query live client, and return. If not active (stopped, completed, failed, review, cancelled), retrieve latest persisted `TaskSessionSnapshot` from `taskRepo.GetLatestSnapshot(ctx, taskID)` and map to `*dto.TaskSessionResponseDTO`.
    - In `PromptTaskSession`: call `broker.EnsureServer(ctx, taskID)` to obtain a `ClientLease`. Use leased client for `PromptSession`. Pass lease to `watchSteeringTurn`.
    - In `watchSteeringTurn`: defer `lease.Release()`. Poll messages and child sessions via `lease.Client()`. When turn completes or times out, capture session snapshot via transactional `SaveSnapshot` (with optional `attemptID` attribution), finalize turn, and release lease.
    - In `CancelTask`: call `broker.AcquireActiveClient(ctx, taskID)`. If active, defer `lease.Release()`, invoke `InterruptSessionTree`.
    - In `CompactTaskSession`: call `broker.AcquireActiveClient(ctx, taskID)`. If active, defer `lease.Release()`, invoke `CompactSession`.
    - In all session reconciliation callsites: acquire task-specific lease and release after query.
  - In `internal/runner/task_runner.go`:
    - Register active handle with broker on startup: `r.sessionBroker.RegisterActive(handle, client)`.
    - Before stopping container in normal completion or cancellation: capture session metadata and messages via in-container GET with bounded timeout (3-5s). Enforce max 10MB byte size limit; if exceeded or error, record `IsComplete = false` and mark incomplete/truncated. Save immutable `TaskSessionSnapshot` transactionally with `task.ID`, allocated next `SnapshotVersion`, and optional `attempt.ID`.
    - Snapshot capture failure during cancellation must NOT block container stop; proceed immediately with `UnregisterActive`.
    - Delete `runOnHost` function in `task_runner.go`.
    - Delete host execs in `dependencies.go`.
    - Preserve the 9 dirty cancellation files (`InterruptSession`, `InterruptSessionTree`, context timeout checks, snapshot diff handling).
  - In `cmd/server/main.go`:
    - Instantiate single `TaskSessionBroker` backed by `TaskSandboxManager`.
    - Inject `sessionBroker` into both `NewAgentTaskRunner` and `NewTaskService(..., service.WithTaskSessionBroker(sessionBroker, ...))`.
    - Remove the single host `openCodeClient` initialization for tasks.
- [ ] **Step 4: Run focused tests to verify green**
  - Run: `go test -v ./internal/runner -run TestTaskOpenCodeClient`
  - Run: `go test -v ./internal/service -run TestTaskService_BrokerRouting`
  - Run: `go test -v ./internal/runner -run TestAgentTaskRunner`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Scope: `internal/models/`, `internal/repository/`, `internal/service/`, `internal/runner/`, `cmd/`

---

### Task 6: API Endpoints, RBAC Authorization & Gate C Decision

**Goal:** Implement runtime catalog and project runtime endpoints with RBAC enforcement (`RequireAuth` for catalog read; `RoleViewer` for project runtime read, `RoleAdmin`/`RoleOwner` for update, cross-team IDOR defense). Resolve Gate C decision before implementing task deletion.

**Files:**
- Create: `internal/api/runtime_handlers.go`
- Create: `internal/api/runtime_handlers_test.go`
- Modify: `internal/api/server.go`
- Modify: `internal/repository/project_repository.go`
- Modify (Conditional on Gate C): `internal/api/task_handlers.go`

**Endpoints:**
- `GET /api/v1/runtimes/catalog` (Auth-required via `RequireAuth`, no team role required) -> Returns 6-runtime catalog.
- `GET /api/v1/projects/:id/runtime` (Viewer+) -> Looks up project, verifies user is team `RoleViewer`+, returns overrides and catalog metadata.
- `PUT /api/v1/projects/:id/runtime` (Admin/Owner) -> Looks up project, verifies user is team `RoleAdmin`+ or `RoleOwner`, validates versions against catalog, updates DB.
- Task Deletion (`DELETE /api/v1/tasks/:id`) -> **BLOCKED ON GATE C.** If Option 1 approved: validates team `RoleAdmin`+, purges task volume via `TaskSandboxManager.PurgeTaskVolume`, deletes task records. If Option 2 approved: do not implement task deletion endpoint; retain volumes under manual policy.

- [ ] **Step 1: Write handler tests for RBAC, cross-team IDOR, and input validation**
  - Create `internal/api/runtime_handlers_test.go`:
    - Test 1: `GET /api/v1/runtimes/catalog` returns 401 Unauthorized when unauthenticated.
    - Test 2: `GET /api/v1/runtimes/catalog` returns 200 OK when authenticated (any valid user session, no team role required).
    - Test 3: `GET /api/v1/projects/:id/runtime` permits `RoleViewer` (10), denies non-team member (403 IDOR check).
    - Test 4: `PUT /api/v1/projects/:id/runtime` denies `RoleMember` (20) and `RoleViewer` (10) with 403.
    - Test 5: `PUT /api/v1/projects/:id/runtime` permits `RoleAdmin` (30) and `RoleOwner` (40).
    - Test 6: `PUT /api/v1/projects/:id/runtime` returns 400 for unknown runtime key or uncataloged version.
- [ ] **Step 2: Run focused test to verify failure**
  - Run: `go test -v ./internal/api -run TestRuntimeHandlers`
  - Expected: FAIL
- [ ] **Step 3: Implement RuntimeHandlers and Register Routes**
  - Implement `internal/api/runtime_handlers.go`.
  - In handlers, look up project by ID to retrieve `TeamID`, then call `authService.AuthorizeTeamRole(ctx, project.TeamID, user.ID, minRole)`.
  - Register routes in `internal/api/server.go`:
    - `v1.Get("/runtimes/catalog", RequireAuth(s.AuthService, s.SessionManager), runtimeHandlers.GetCatalog)`
    - Project runtime endpoints with `RequireAuth` and project team RBAC.
  - If and only if Gate C Option 1 was selected by user, implement authorized task deletion.
- [ ] **Step 4: Run focused tests to verify green**
  - Run: `go test -v ./internal/api -run TestRuntimeHandlers`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Scope: `internal/api/`, `internal/repository/`

---

### Task 7: Web Frontend Project Runtime Settings UI & Verification

**Goal:** Provide project runtime configuration in the web frontend using the existing active project/team store (`useTeamStore`), with RBAC permission disabling and accessibility checks.

**Files:**
- Create: `web/src/features/projects/schemas/runtimeSchemas.ts`
- Create: `web/src/features/projects/api/runtimeApi.ts`
- Create: `web/src/features/projects/components/ProjectRuntimeSettingsModal.tsx`
- Create: `web/src/features/projects/components/ProjectRuntimeSettingsModal.test.tsx`
- Modify: `web/src/features/teams/components/ProjectSwitcher.tsx` (or control bar project settings trigger)

- [ ] **Step 1: Write Vitest tests for runtime API and settings modal**
  - Create `web/src/features/projects/components/ProjectRuntimeSettingsModal.test.tsx`:
    - Test 1: Renders runtime table with all 6 runtimes (Go, Node, Python, Java, Rust, .NET).
    - Test 2: Shows catalog options and allows selecting "Inherit", "Disabled", or catalog versions.
    - Test 3: Disables editing controls when user role is less than Admin.
    - Test 4: Submits valid payload on Save.
    - Test 5: Accessibility: labels, keyboard tab navigation, ARIA attributes.
- [ ] **Step 2: Run focused Vitest to verify failure**
  - Run: `npm test --prefix web -- ProjectRuntimeSettingsModal`
  - Expected: FAIL
- [ ] **Step 3: Implement Frontend Schemas, API Client, and Component**
  - Create Zod schemas in `web/src/features/projects/schemas/runtimeSchemas.ts`.
  - Implement API client in `web/src/features/projects/api/runtimeApi.ts`.
  - Implement `ProjectRuntimeSettingsModal.tsx` using `@base-ui/react` / shadcn primitives, integrating with active project from `useTeamStore`.
  - Wire trigger into project interface.
- [ ] **Step 4: Run focused Vitest and typecheck to verify green**
  - Run: `npm test --prefix web -- ProjectRuntimeSettingsModal`
  - Run: `cd web && npx tsc -b`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Scope: `web/src/features/projects/`, `web/src/features/teams/`

---

### Task 8: Concrete 6-Language Docker Runtime Smoke Tests

**Goal:** Execute end-to-end sandbox verification on `linux/amd64` testing real compilation and execution across all 6 runtimes in generated containers.

**Files:**
- Create: `internal/runner/smoke_test.go` (guarded by `//go:build integration`)

- [ ] **Step 1: Write integration smoke test**
  - In `internal/runner/smoke_test.go`, test container creation for:
    - Go: compiles and executes `main.go`
    - Node: executes `node index.js`
    - Python: executes `python3 script.py`
    - Java: compiles and executes `Main.java`
    - Rust: compiles with `cargo build` and runs binary
    - .NET: builds and runs with `dotnet run`
- [ ] **Step 2: Run integration smoke test with Docker daemon**
  - Run: `go test -v -tags=integration ./internal/runner -run TestSixRuntimeSmoke`
  - Expected: PASS for all 6 runtimes.
- [ ] **Step 3: Commit scoped changes**
  - Scope: `internal/runner/smoke_test.go`

---

### Task 9: Full Verification, Clean Build & Lint Gate

**Goal:** Run the complete test suite, staticcheck, lint, and typechecks across backend and frontend to verify no regressions.

- [ ] **Step 1: Run complete backend verification**
  - Run: `make check`
  - Expected: `staticcheck` passed with 0 findings, `go vet` clean, all unit tests PASS.
- [ ] **Step 2: Run complete frontend verification**
  - Run: `npm test --prefix web -- --run`
  - Run: `cd web && npx tsc -b`
  - Expected: All 26+ test files PASS, 0 TypeScript errors.
- [ ] **Step 3: Verify git status cleanliness**
  - Run: `git status`
  - Verify that the 9 original cancellation files were cleanly integrated and no untracked temporary files exist.

---

## Developer's Verification Commands

```bash
# Backend unit & static analysis
make check

# Focused Go package testing during tasks
go test -v ./internal/models -run TestProject
go test -v ./internal/runner -run TestRuntimeCatalog
go test -v ./internal/runner -run TestRuntimeResolver
go test -v ./internal/sandbox -run TestDocker
go test -v ./internal/runner -run TestRuntimeImageBuilder
go test -v ./internal/runner -run TestTaskSandboxManager
go test -v ./internal/runner -run TestTaskSessionBroker
go test -v ./internal/runner -run TestTaskOpenCodeClient
go test -v ./internal/service -run TestTaskService_BrokerRouting
go test -v ./internal/runner -run TestAgentTaskRunner
go test -v ./internal/api -run TestRuntimeHandlers

# Integration 6-language Docker smoke test
go test -v -tags=integration ./internal/runner -run TestSixRuntimeSmoke

# Frontend unit tests & TypeScript build check
npm test --prefix web -- --run
cd web && npx tsc -b
```
