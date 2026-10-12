# Project-Scoped Runtime Container & Sandboxed OpenCode Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide reproducible, content-addressed cached per-project runtime container environments on `linux/amd64` with sandboxed OpenCode server execution, fail-closed privilege controls (`no-new-privileges:true`), zero host fallback, in-container cloning without host bind mounts, union-based multi-repository runtime resolution, and strict inter-project isolation.

**Architecture:** One isolated Docker container per ATM project (`atm-project-<project_id>`) hosting a long-lived OpenCode server communicating via internal container IP with HTTP Basic authentication (`opencode:<OPENCODE_PASSWORD>`). Repositories and task-specific Git worktrees reside strictly in persistent project volumes (`atm-project-data-<project_id>:/workspaces` and `atm-project-state-<project_id>:/var/lib/opencode`) without host volume bind mounts; control-plane tasks execute against in-container worktree paths with manual data retention and an automated 24-hour idle container stop lifecycle.

**Tech Stack:** Go 1.27, Docker Engine SDK (`github.com/docker/docker v27.5.1+incompatible`), GORM v2, PostgreSQL / SQLite, Fiber v3, OpenCode Server v2.0.26, TypeScript, React 19, Tailwind CSS v4.

**Spec:** `docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md`

---

## Global Constraints

- **Platform & Host Baseline:** Target runtime is strictly `linux/amd64`. Production deployment targets a native non-Snap Linux Docker Engine (or dedicated native Linux VM) qualified via an explicit zero-mount, zero-credential NNP probe.
- **Fail-Closed Privilege Security (`no-new-privileges:true`):** Container execution requires `--security-opt no-new-privileges:true` and `CapDrop: ALL`. If the runtime rejects NNP, the control plane fails closed immediately; disabling, relaxing, or bypassing NNP or softening AppArmor/seccomp profiles is strictly forbidden.
- **Docker Socket Isolation:** Project containers never mount `/var/run/docker.sock` or any daemon socket. Only the trusted ATM control-plane runner communicates with the Docker daemon.
- **Host Mount Elimination:** Zero host filesystem bind mounts. Host source trees, control-plane directories, host Git caches, and host tool configurations (`~/.config/opencode`) are never mounted into containers.
- **Single Trust Boundary per Project (Shared Intra-Project Boundary):** Security isolation is container-enforced strictly between different ATM projects (`atm-project-<project_id>`). Repositories and tasks within the same project share a single security trust boundary and filesystem volume; worktrees provide branch operational concurrency only, not task security isolation.
- **Deterministic Multi-Repository Runtime Resolution:** Resolves project runtime profiles via the union of all declared repository `.atm-runtime.json` manifests. Exact conflicting versions across repos fail closed immediately unless resolved by an explicit project UI/DB override. UI/DB overrides take precedence over valid manifests, but invalid manifests (syntax error, schemaVersion != 1, or uncataloged versions) fail closed immediately and cannot be bypassed. Repo-level `false` indicates that repo does not require the runtime; positive requirements from other repos win in union; an explicit project-level `false` override disables the runtime project-wide.
- **Read-Only Root & Scratch Hygiene:** Container root filesystems are read-only (`--read-only`). Writable paths are restricted to persistent project Docker volumes (`atm-project-data-<project_id>` and `atm-project-state-<project_id>`) and bounded `tmpfs` mounts (`/tmp:rw,noexec,nosuid,size=512m`). Per-task scratch directories in `tmpfs` are cleared upon task completion. Task and worktree retention in persistent volumes is strictly manual; there is no task-delete API or automatic deletion.
- **Container Idle Lifecycle (24h Inactivity):** Containers stop after 24 consecutive hours with zero running tasks and zero active OpenCode requests, retaining persistent volumes and OpenCode state. Stop operations must be coordinated to prevent races with active tasks or in-flight requests.
- **Credential Separation & Safe Sync:** Raw GitHub tokens stay solely in the trusted ATM control plane and are never exposed into container environments, worktree Git configs, files, command arguments, or prompt contexts. Initial repo cloning and sync into container volumes must pass the credential-safe qualification gate.
- **Outbound Egress Default-Deny:** Containers execute on isolated user-defined Docker bridge networks without published host ports. Default-deny outbound egress must explicitly block private host/control-plane subnets, Docker bridges, IPv4/IPv6 link-local addresses, and cloud provider metadata (`169.254.169.254` and `[fd00:ec2::254]`).
- **Zero Host Execution Fallback:** All compilation, dependency installation, tool runs, and agent steps execute exclusively inside the sandbox container. `runOnHost` and host-level dependency installation in `dependencies.go` must be completely removed.
- **Preservation of Existing Work:** Preserve the existing task cancellation and session tree interruption logic (`InterruptSessionTree`, bounded timeout handling).
- **Isolated Build & Test Execution:** All repo dependency installation, Go tests, builds, and verification must run in an approved isolated container/CI environment, never on the host.

---

## Review Focus

1. **Gate Enforcement & Implementation Prohibition:** Verify that Tasks 1 through 9 remain strictly blocked and cannot be implemented until all qualification gates (Gates 0A through 0D) pass written sign-off and approval.
2. **Credential Leakage Prevention during Sync:** Verify that no Git credentials, HTTP authorization headers, or personal access tokens are stored in the persistent project volume, `.git/config`, environment variables, or container logs.
3. **Multi-Repo Manifest Conflict Fail-Closed:** Verify that when Repo A specifies `go: "1.27.2"` and Repo B specifies an alternate valid catalog version `go: "1.26.0"` (registered in test fixtures to isolate union conflicts from catalog validation), `RuntimeResolver` fails closed immediately with a descriptive conflict error unless an explicit project-level override exists in `models.Project.RuntimeConfig`. Ensure uncataloged invalid versions are separately rejected by schema/catalog validation.
4. **Host Fallback and Bind Mount Prohibition:** Verify that zero host bind mounts exist in container configurations, that `runOnHost` is deleted from `task_runner.go`, and that host package executions (`npm install`, `go mod download`) in `dependencies.go` are completely eradicated.
5. **Idle Container Stopping Concurrency & Leases:** Verify that the 24-hour idle stop coordinator checks both active task execution leases (`AcquireTaskLease`) and in-flight OpenCode HTTP request leases (`AcquireRequestLease`) before stopping, ensuring no in-flight turn or long-running task is interrupted.
6. **Egress Default-Deny Filtering:** Verify that user-defined project container bridge networks apply default-deny outbound filtering rules (blocking private subnets, host bridge interfaces, link-local, and cloud metadata `169.254.169.254` / `[fd00:ec2::254]`) qualified in Gate 0D.

---

## Pre-Implementation Qualification & Architecture Decision Gates

> **HARD STOP RULE:** Tasks 1 through 9 cannot proceed to production code until each gate below is explicitly qualified and resolved. Tasks depending on unresolved gates must remain blocked.

### Gate 0A: Native Non-Snap Linux Docker Host Qualification & Mandatory `no-new-privileges` Probe

- **Status:** QUALIFIED IN DEDICATED GUEST PROBE ONLY (Open for arbitrary production hosts; see `docs/superpowers/verification/2026-10-11-runtime-qualification-gates-0a-0b.md`). Tasks 1–9 remain blocked pending remaining gates.
- **Background & Context:** Testing on the host running Docker Engine 29.8.0 via Snap (rev 3613, containerd 2.3.4, runc 1.5.1) confirmed that running containers with mandatory `--security-opt no-new-privileges:true` fails execution during container launch (`operation not permitted`, exit status 255). An informational test on Docker Desktop 29.2.1 passed (`NoNewPrivs: 1`, exit status 0) inside its VM guest, but did not qualify a native Linux environment.
- **Probe Specification & Verification Result:**
  - Qualified in dedicated Ubuntu 24.04 QEMU/KVM guest (`docker-ce` 29.8.0, containerd 2.3.4, runc 1.5.1, Linux 6.8 kernel).
  - Executed under `--pull=never --network none --read-only --cap-drop=ALL --security-opt no-new-privileges:true --user 65534:65534 --pids-limit 16 --memory 64m --cpus 0.5`.
  - Output: `NoNewPrivs: 1`, exit status `0`.
- **Scope & Limitations:** Qualification applies strictly to the dedicated Ubuntu 24.04 guest probe. Snap Docker is confirmed incompatible. Arbitrary production hosts are NOT qualified. AppArmor/seccomp softening or NNP omission remains strictly prohibited.

### Gate 0B: OpenCode Candidate Image Provenance, Base OS ABI & Handshake Qualification

- **Status:** CANDIDATE QUALIFIED IN DEDICATED GUEST PROBE ONLY (Open for publisher provenance, production password lifecycle, control-plane routing, and egress; see `docs/superpowers/verification/2026-10-11-runtime-qualification-gates-0a-0b.md`). Tasks 1–9 remain blocked pending remaining gates.
- **Candidate Artifact:**
  - GHCR Tag: `ghcr.io/anomalyco/opencode:2.0.26`
  - Manifest Digest: `sha256:ea6c5d68d8d230923d2627bc793e7fa6a03f8e47c253b50397eb92bbde6db255`
- **Verification Results in Dedicated Guest:**
  - Standalone binary CLI verified under strict isolation (`opencode --version` -> `opencode v2.0.26`, exit status 0).
  - Dynamic ABI validated on Alpine Linux 3.24 musl libc (`/lib/ld-musl-x86_64.so.1`, `libstdc++.so.6`, `libgcc_s.so.1`).
  - Loopback listener bound to `127.0.0.1:4096` under `--network none`.
  - Inbound unauthenticated probes returned `401 Unauthorized` with `www-authenticate: Basic realm="Secure Area"`.
  - Authorized positive probe using server-generated password held only in tmpfs returned HTTP `200` (`positive_auth_status=200`, exit status 0).
- **Residual Open Blockers:**
  - Upstream publisher signature/provenance remains unverified.
  - Production password lifecycle (avoiding log scraping or stdout persistence) and control-plane integration remain open.
  - Gate 0C (credential-safe Git transfer), Gate 0D (default-deny egress), and Tasks 1–9 remain blocked.

### Gate 0C: Credential-Safe Repository Clone, Sync & Push Data Transfer Mechanism

- **Status:** Mandatory Pre-Implementation Gate (Open / Unresolved).
- **Problem Statement:** Host bind mounts are eliminated. Raw GitHub personal access tokens remain solely in the ATM control plane and must never be placed inside the project container, `/workspaces`, `.git/config`, or task-visible environment variables.
- **Deliverable:**
  - Design and qualify an exact mechanism to transfer repository data and perform authenticated fetch/push operations between the control plane and persistent volume `atm-project-data-<project_id>`.
  - Evaluate candidates: control-plane daemon-driven archive streaming (`docker cp` / tar stream over Docker SDK), dedicated transient control-plane Git credential helper pipe, or authenticated proxy sidecar on an internal network.
  - Require formal security review and coordinator sign-off before proceeding to implementation tasks (specifically blocking Task 5 `WorkspaceManager` and dependent tasks).

### Gate 0D: Container Egress Filtering Mechanism & Endpoint Allowlisting

- **Status:** Mandatory Pre-Implementation Gate (Open / Unresolved).
- **Problem Statement:** Project sandbox containers must enforce default-deny egress, explicitly blocking access to host/control-plane private networks (RFC 1918, RFC 6598), Docker bridge gateways, IPv4/IPv6 link-local addresses, and cloud provider metadata (`169.254.169.254`, `[fd00:ec2::254]`).
- **Deliverable:**
  - Qualify the filtering implementation: host-managed iptables/nftables rules applied to the project Docker bridge network vs. container egress proxy sidecar.
  - Document specific allowed external destinations: LLM model provider endpoints (Anthropic, OpenAI) and approved public package registries.
  - Require formal security review and coordinator sign-off before proceeding to implementation tasks (specifically blocking Task 4 `ProjectSandboxManager` and dependent tasks).

---

## Detailed Task Implementation Order

> **Note on Environment Execution:** All repository dependency installation, Go unit/integration tests, builds, and linting must be executed inside an approved isolated container/CI environment, never on the bare development host.

### Task 1: Runtime Model, Migration & Pinned Catalog Codification

**Goal:** Extend `models.Project` with `RuntimeConfig JSONMap`, update migrations, define catalog data structures, and codify the approved 6-language baseline runtime catalog.

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
// internal/runner/runtime_types.go
package runner

type RuntimeConfigMap map[string]string // runtime -> version string or "false"

type ResolvedRuntimeProfile struct {
    Platform       string            `json:"platform"`
    BaseImageRef   string            `json:"base_image_ref"`
    TemplateRev    string            `json:"template_rev"`
    Runtimes       map[string]string `json:"runtimes"` // "go", "node", "python", "java", "rust", "dotnet"
    OpenCodeDigest string            `json:"opencode_digest"`
}

type RuntimeCatalogEntry struct {
    Runtime     string `json:"runtime"`
    Version     string `json:"version"`
    ArchiveURL  string `json:"archive_url"`
    SHA256      string `json:"sha256"`
    ExtractPath string `json:"extract_path"`
    IsBaseline  bool   `json:"is_baseline"`
}

type RuntimeCatalog interface {
    Get(runtime, version string) (*RuntimeCatalogEntry, bool)
    GetBaseline(runtime string) (string, bool)
    List() []RuntimeCatalogEntry
    ValidateVersion(runtime, version string) bool
}
```

- [ ] **Step 1: Write failing tests for models.Project RuntimeConfig and RuntimeCatalog**
  - Create `internal/runner/runtime_catalog_test.go` asserting baseline versions:
    - `go`: `"1.27.2"`, `node`: `"24.21.0"`, `python`: `"3.11.17"` (default enabled).
    - `java`: `"25.0.4.1+1"`, `rust`: `"1.99.0"`, `dotnet`: `"10.0.401"` (default `"false"`).
  - Add test asserting rejection of unknown versions or invalid keys.
  - Add test in `internal/models/models_test.go` verifying `Project.RuntimeConfig` JSON serialization and defaults.
- [ ] **Step 2: Run focused test to verify failure**
  - Command: `go test -v ./internal/runner -run TestRuntimeCatalog`
  - Expected: FAIL (`undefined: runner.NewDefaultRuntimeCatalog`)
- [ ] **Step 3: Implement Runtime types, catalog, and Project model update**
  - In `internal/models/models.go`, add `RuntimeConfig JSONMap gorm:"type:jsonb;not null;default:'{}'" json:"runtime_config"` to `Project`.
  - Update `cmd/server/main.go` and `cmd/migrate/main.go` to include updated model in `modelList`.
  - Implement `internal/runner/runtime_types.go` and `internal/runner/runtime_catalog.go`.
- [ ] **Step 4: Run focused tests to verify green**
  - Command: `go test -v ./internal/runner -run TestRuntimeCatalog`
  - Command: `go test -v ./internal/models -run TestProject`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add internal/models/ internal/runner/runtime_* cmd/ && git commit -m "feat(runtime): add project runtime configuration model and catalog"`

---

### Task 2: Multi-Repository Manifest & Union Resolver

**Goal:** Implement `RuntimeResolver` supporting repository manifest parsing (`.atm-runtime.json`), multi-repo requirement unioning, conflict fail-closed validation, project UI/DB override precedence, and SHA256 cache tag computation.

**Files:**
- Create: `internal/runner/runtime_resolver.go`
- Create: `internal/runner/runtime_resolver_test.go`

**Interfaces:**
```go
// internal/runner/runtime_resolver.go
package runner

import (
    "context"
    "agent-task-manager/internal/models"
)

type RepositoryManifest struct {
    SchemaVersion int                    `json:"schemaVersion"`
    Runtimes      map[string]interface{} `json:"runtimes"` // string version or bool false
}

type RuntimeResolver interface {
    ResolveMultiRepo(ctx context.Context, manifests map[string]*RepositoryManifest, project *models.Project) (*ResolvedRuntimeProfile, error)
    ComputeCacheTag(profile *ResolvedRuntimeProfile) (string, error)
}
```

- [ ] **Step 1: Write unit tests for multi-repo union, conflict fail-closed, and hashing**
  - In `internal/runner/runtime_resolver_test.go`:
    - Test 1: Single repo with no manifest resolves to catalog baseline.
    - Test 2: Multi-repo union: Repo A specifies `rust: "1.99.0"`, Repo B specifies `rust: false`; resolved profile includes `rust: "1.99.0"`.
    - Test 3: Multi-repo conflict: With a test fixture registering a secondary valid catalog version `go: "1.26.0"` alongside `go: "1.27.2"`, Repo A specifies `go: "1.27.2"` and Repo B specifies `go: "1.26.0"`; resolver fails closed with conflict error. This isolates union conflict handling from uncataloged version rejection.
    - Test 4: Project UI/DB override resolves multi-repo conflict: project override `go: "1.27.2"` allows resolution to succeed.
    - Test 5: Invalid manifest: schemaVersion != 1 or invalid runtime key fails closed immediately; UI override does NOT forgive invalid manifest.
    - Test 6: Uncataloged version: Repo specifying uncataloged version fails closed under catalog validation, distinguishing it from valid-version union conflicts.
    - Test 7: Project UI/DB override set to `false` disables runtime across entire project.
    - Test 8: `ComputeCacheTag` outputs canonical format `atm-runtime:<64_hex_chars>`.
- [ ] **Step 2: Run focused test to verify failure**
  - Command: `go test -v ./internal/runner -run TestRuntimeResolver`
  - Expected: FAIL (`undefined: runner.NewRuntimeResolver`)
- [ ] **Step 3: Implement RuntimeResolver**
  - Implement `internal/runner/runtime_resolver.go`.
  - Validate all manifest entries against `RuntimeCatalog`.
  - Encode canonical JSON with sorted runtime keys and compute SHA256 tag.
- [ ] **Step 4: Run focused tests to verify green**
  - Command: `go test -v ./internal/runner -run TestRuntimeResolver`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add internal/runner/runtime_resolver* && git commit -m "feat(runtime): implement multi-repo union runtime resolver and cache tag hashing"`

---

### Task 3: Docker Sandbox Client Extension & Deterministic Image Builder

**Goal:** Extend `sandbox.DockerClient` to support image, network, and volume operations matching `github.com/docker/docker v27.5.1+incompatible`, and implement `RuntimeImageBuilder` with in-flight build coalescing based on the Gate 0B base OS decision.

**Files:**
- Modify: `internal/sandbox/docker.go`
- Modify: `internal/sandbox/docker_test.go`
- Create: `internal/runner/image_builder.go`
- Create: `internal/runner/image_builder_test.go`
- Create: `deploy/sandbox/Dockerfile.project.template`

**Interfaces:**
```go
// internal/sandbox/docker.go
package sandbox

import (
    "context"
    "io"
    "github.com/docker/docker/api/types"
    "github.com/docker/docker/api/types/image"
    "github.com/docker/docker/api/types/network"
    "github.com/docker/docker/api/types/volume"
)

type DockerClient interface {
    // Existing container lifecycle operations
    ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
    ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
    ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
    ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
    ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)

    // Image, network, and volume operations
    ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error)
    ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error)
    ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error)
    NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
    NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)
    NetworkRemove(ctx context.Context, networkID string) error
    VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
    VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error)
    VolumeRemove(ctx context.Context, volumeID string, force bool) error
}
```

- [ ] **Step 1: Write unit tests for DockerClient extensions and ImageBuilder build coalescing**
  - Create `internal/runner/image_builder_test.go`:
    - Test 1: Cached image tag found via `ImageList` returns immediately without building.
    - Test 2: Uncached image generates tar context and calls `ImageBuild`.
    - Test 3: Multiple concurrent calls for the same profile coalesce onto a single build channel.
    - Test 4: Detaching a cancelled context leaves the build running if another caller is waiting.
- [ ] **Step 2: Run focused test to verify failure**
  - Command: `go test -v ./internal/runner -run TestRuntimeImageBuilder`
  - Expected: FAIL
- [ ] **Step 3: Implement DockerClient extensions and RuntimeImageBuilder**
  - Update `internal/sandbox/docker.go` with Docker SDK v27.5.1 signatures.
  - Implement `internal/runner/image_builder.go`.
  - Create `deploy/sandbox/Dockerfile.project.template` reflecting the approved base OS from Gate 0B.
- [ ] **Step 4: Run focused tests to verify green**
  - Command: `go test -v ./internal/sandbox -run TestDocker`
  - Command: `go test -v ./internal/runner -run TestRuntimeImageBuilder`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add internal/sandbox/ internal/runner/image_builder* deploy/sandbox/ && git commit -m "feat(sandbox): extend docker client and implement deterministic image builder"`

---

### Task 4: Project Sandbox Container Lifecycle Manager & OpenCode Server Bootstrap

**Goal:** Implement `ProjectSandboxManager` to manage `atm-project-<project_id>` containers on dedicated bridge networks with outbound default-deny egress filtering (blocking private subnets, host bridge interfaces, link-local, and cloud metadata endpoints `169.254.169.254` / `[fd00:ec2::254]`) per Gate 0D, volume mounts (`atm-project-data-<project_id>` and `atm-project-state-<project_id>`), read-only root, tmpfs scratch, mandatory `no-new-privileges:true`, and `OPENCODE_PASSWORD` HTTP Basic authentication.

**Files:**
- Create: `internal/runner/project_sandbox_manager.go`
- Create: `internal/runner/project_sandbox_manager_test.go`
- Create: `internal/sandbox/egress_filter.go`
- Create: `internal/sandbox/egress_filter_test.go`

**Interfaces:**
```go
// internal/runner/project_sandbox_manager.go
package runner

import (
    "context"
    "time"
    "agent-task-manager/internal/models"
)

type ProjectRouteHandle struct {
    ProjectID    string `json:"project_id"`
    ContainerID  string `json:"container_id"`
    NetworkID    string `json:"network_id"`
    IPAddress    string `json:"ip_address"`
    Port         int    `json:"port"`
    EndpointURL  string `json:"endpoint_url"`
    AuthPassword string `json:"-"`
    Generation   int64  `json:"generation"`
}

type ProjectSandboxManager interface {
    EnsureProjectContainer(ctx context.Context, project *models.Project, imageTag string) (*ProjectRouteHandle, error)
    StopProjectContainer(ctx context.Context, projectID string, gracePeriod time.Duration) error
    GetProjectRoute(ctx context.Context, projectID string) (*ProjectRouteHandle, bool, error)
}
```

- [ ] **Step 1: Write unit tests for ProjectSandboxManager and Network Egress Filtering**
  - Create `internal/runner/project_sandbox_manager_test.go`:
    - Test 1: Container configuration applies `CapDrop: ["ALL"]`, `SecurityOpt: ["no-new-privileges:true"]`, `ReadonlyRootfs: true`.
    - Test 2: Mounts persistent volumes `atm-project-data-<project_id>:/workspaces:rw` and `atm-project-state-<project_id>:/var/lib/opencode:rw`.
    - Test 3: Mounts bounded tmpfs scratch at `/tmp:rw,noexec,nosuid,size=512m`.
    - Test 4: Zero host bind mounts configured.
    - Test 5: Injects `OPENCODE_PASSWORD` environment variable.
    - Test 6: Daemon inspection retrieves container IP on dedicated bridge; returns verified `ProjectRouteHandle`.
  - Create `internal/sandbox/egress_filter_test.go`:
    - Test 7: Bridge network configuration creates isolated user-defined Docker bridge without published host ports.
    - Test 8: Egress filter enforces default-deny outbound policy: explicitly drops packets to private host/control-plane subnets (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16), Docker bridge gateways, IPv4/IPv6 link-local addresses, and cloud provider metadata endpoints (`169.254.169.254` and `[fd00:ec2::254]`), while allowing traffic to qualified model provider endpoints.
- [ ] **Step 2: Run focused test to verify failure**
  - Command: `go test -v ./internal/runner -run TestProjectSandboxManager`
  - Command: `go test -v ./internal/sandbox -run TestEgressFilter`
  - Expected: FAIL
- [ ] **Step 3: Implement ProjectSandboxManager and Egress Filtering**
  - Implement `internal/sandbox/egress_filter.go` according to the mechanism qualified in Gate 0D.
  - Implement `internal/runner/project_sandbox_manager.go`.
  - Wire network creation to apply egress filter rules prior to container attachment.
  - Validate fail-closed behavior if container creation, NNP, or egress setup fails.
- [ ] **Step 4: Run focused tests to verify green**
  - Command: `go test -v ./internal/runner -run TestProjectSandboxManager`
  - Command: `go test -v ./internal/sandbox -run TestEgressFilter`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add internal/runner/project_sandbox_manager* internal/sandbox/egress_filter* && git commit -m "feat(runner): implement project sandbox container manager with egress filtering"`

---

### Task 5: In-Container Worktree Management & Credential-Safe Sync Integration

**Goal:** Implement in-container task worktree provisioning inside `/workspaces/<repo>/tasks/<task_id>` within the persistent project volume, and integrate the credential-safe Git sync mechanism qualified in Gate 0C.

**Files:**
- Create: `internal/runner/workspace_manager.go`
- Create: `internal/runner/workspace_manager_test.go`
- Modify: `internal/git/worktree.go`
- Modify: `internal/git/worktree_test.go`

**Interfaces:**
```go
// internal/runner/workspace_manager.go
package runner

import (
    "context"
    "agent-task-manager/internal/models"
)

type WorkspaceManager interface {
    EnsureTaskWorktree(ctx context.Context, projectID string, task *models.Task, repoName string, baseCommit string) (containerWorktreePath string, err error)
    CleanupTaskScratch(ctx context.Context, projectID string, taskID string) error
}
```

- [ ] **Step 1: Write unit tests for WorkspaceManager**
  - Create `internal/runner/workspace_manager_test.go`:
    - Test 1: Provisions task worktree inside `/workspaces/<repo>/tasks/<task_id>`.
    - Test 2: Tasks default to single selected repository worktree.
    - Test 3: Bounded per-task scratch directory in `/tmp` is purged on `CleanupTaskScratch`.
    - Test 4: Persistent worktree and git data in `/workspaces` are retained intact across task ends (manual retention).
- [ ] **Step 2: Run focused test to verify failure**
  - Command: `go test -v ./internal/runner -run TestWorkspaceManager`
  - Expected: FAIL
- [ ] **Step 3: Implement WorkspaceManager using Gate 0C mechanism**
  - Implement `internal/runner/workspace_manager.go`.
  - Ensure zero raw tokens are passed into container Git configurations or command arguments.
- [ ] **Step 4: Run focused tests to verify green**
  - Command: `go test -v ./internal/runner -run TestWorkspaceManager`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add internal/runner/workspace_manager* internal/git/ && git commit -m "feat(runner): implement in-container workspace and task worktree manager"`

---

### Task 6: AgentTaskRunner & TaskService Integration: Basic Auth, Session Routing & Host Fallback Elimination

**Goal:** Refactor `OpenCodeClient` to use HTTP Basic authentication (`opencode:<OPENCODE_PASSWORD>`), update `AgentTaskRunner` and `TaskService` to route task sessions to in-container worktrees, eliminate `runOnHost`, and eradicate host-level dependency installation in `dependencies.go`.

**Files:**
- Modify: `internal/runner/opencode_client.go`
- Modify: `internal/runner/opencode_client_test.go`
- Modify: `internal/runner/task_runner.go`
- Modify: `internal/runner/task_runner_test.go`
- Modify: `internal/runner/dependencies.go`
- Modify: `internal/runner/dependencies_test.go`
- Modify: `internal/service/task_service.go`
- Modify: `internal/service/task_service_test.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
```go
// internal/runner/opencode_client.go
func NewProjectOpenCodeClient(endpointURL string, password string, httpClient *http.Client) OpenCodeClient
```

- [ ] **Step 1: Write tests asserting HTTP Basic Auth, zero host fallback, and session worktree path binding**
  - In `internal/runner/opencode_client_test.go`:
    - Test client sends `Authorization: Basic <base64(opencode:password)>` on all API requests.
    - Test client rejects loopback redirects and host Unix socket dial paths.
  - In `internal/runner/task_runner_test.go`:
    - Test `TestAgentTaskRunner_NoHostFallbackOnContainerFailure`: container failure aborts task attempt fail-closed without host fallback.
    - Test task session creation binds directory strictly to in-container worktree path `/workspaces/<repo>/tasks/<task_id>`.
  - In `internal/runner/dependencies_test.go`:
    - Verify host execution functions are removed; dependency operations execute via container toolchains.
- [ ] **Step 2: Run focused tests to verify failure**
  - Command: `go test -v ./internal/runner -run TestProjectOpenCodeClient`
  - Command: `go test -v ./internal/runner -run TestAgentTaskRunner_NoHostFallback`
  - Expected: FAIL
- [ ] **Step 3: Implement Client updates, delete host fallback and host dependency installation**
  - Update `internal/runner/opencode_client.go` with HTTP Basic auth support.
  - In `internal/runner/task_runner.go`:
    - Delete `runOnHost` function.
    - Wire `ProjectSandboxManager` to ensure project container.
    - Acquire active task execution lease: `releaseTask, err := r.idleTracker.AcquireTaskLease(task.ProjectID, task.ID)` and defer `releaseTask()`.
    - Bind task sessions to in-container worktree paths.
  - In `internal/runner/dependencies.go`:
    - Delete host package manager executions (`exec.Command("npm", ...)` and `exec.Command("go", ...)`).
  - Preserve cancellation logic (`InterruptSessionTree`).
- [ ] **Step 4: Run focused tests to verify green**
  - Command: `go test -v ./internal/runner -run TestProjectOpenCodeClient`
  - Command: `go test -v ./internal/runner -run TestAgentTaskRunner`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add internal/runner/ internal/service/ cmd/server/ && git commit -m "feat(runner): wire project container routing and eliminate host execution fallback"`

---

### Task 7: 24-Hour Container Idle Lifecycle Coordinator

**Goal:** Implement a background coordinator that stops project containers after 24 consecutive hours without running tasks or active OpenCode requests, preserving persistent volumes and OpenCode state.

**Files:**
- Create: `internal/runner/idle_coordinator.go`
- Create: `internal/runner/idle_coordinator_test.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
```go
// internal/runner/idle_coordinator.go
package runner

import (
    "context"
    "time"
)

type IdleTracker interface {
    RecordActivity(projectID string)
    AcquireTaskLease(projectID, taskID string) (release func(), err error)
    AcquireRequestLease(projectID string) (release func(), err error)
    CheckIdleAndStop(ctx context.Context, maxIdleDuration time.Duration) error
}
```

- [ ] **Step 1: Write unit tests for idle timeout and race-free stopping**
  - In `internal/runner/idle_coordinator_test.go`:
    - Test 1: Container active within 24h is NOT stopped.
    - Test 2: Container with zero activity for >24h triggers `StopProjectContainer`.
    - Test 3: Active task running holds lease from `AcquireTaskLease`, preventing idle stop even if >24h since previous activity.
    - Test 4: In-flight OpenCode request holds lease from `AcquireRequestLease`, blocking idle stop until released.
    - Test 5: Stopping container preserves persistent Docker volumes (`atm-project-data-*` and `atm-project-state-*`).
- [ ] **Step 2: Run focused test to verify failure**
  - Command: `go test -v ./internal/runner -run TestIdleTracker`
  - Expected: FAIL
- [ ] **Step 3: Implement IdleTracker coordinator**
  - Implement `internal/runner/idle_coordinator.go`.
  - Wire periodic tick in `cmd/server/main.go`.
- [ ] **Step 4: Run focused tests to verify green**
  - Command: `go test -v ./internal/runner -run TestIdleTracker`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add internal/runner/idle_coordinator* cmd/server/ && git commit -m "feat(runner): implement 24-hour project container idle lifecycle coordinator"`

---

### Task 8: API Endpoints, RBAC Authorization & Web UI Settings

**Goal:** Implement REST API endpoints (`GET /api/v1/runtimes/catalog`, `GET/PUT /api/v1/projects/:id/runtime`) with RBAC enforcement, and build the project runtime settings modal in the web frontend.

**Files:**
- Create: `internal/api/runtime_handlers.go`
- Create: `internal/api/runtime_handlers_test.go`
- Modify: `internal/api/server.go`
- Create: `web/src/features/projects/schemas/runtimeSchemas.ts`
- Create: `web/src/features/projects/api/runtimeApi.ts`
- Create: `web/src/features/projects/components/ProjectRuntimeSettingsModal.tsx`
- Create: `web/src/features/projects/components/ProjectRuntimeSettingsModal.test.tsx`
- Modify: `web/src/features/teams/components/ProjectSwitcher.tsx`

**API Contracts:**
- `GET /api/v1/runtimes/catalog`: Public catalog metadata (Requires `RequireAuth`, no team role required).
- `GET /api/v1/projects/:id/runtime`: Requires `RoleViewer` (10)+ on project team.
- `PUT /api/v1/projects/:id/runtime`: Requires `RoleAdmin` (30)+ or `RoleOwner` (40) on project team. Validates runtime versions against catalog.

- [ ] **Step 1: Write backend API and frontend modal tests**
  - In `internal/api/runtime_handlers_test.go`:
    - Test catalog endpoint 401 unauthenticated, 200 authenticated.
    - Test project runtime read: 200 for Viewer, 403 for non-team member (cross-team IDOR).
    - Test project runtime write: 200 for Admin/Owner, 403 for Member/Viewer, 400 for uncataloged version.
  - In `web/src/features/projects/components/ProjectRuntimeSettingsModal.test.tsx`:
    - Test renders 6 runtimes with baseline/catalog options.
    - Test disables form controls for non-admin roles.
- [ ] **Step 2: Run focused tests to verify failure**
  - Command: `go test -v ./internal/api -run TestRuntimeHandlers`
  - Command: `npm test --prefix web -- ProjectRuntimeSettingsModal`
  - Expected: FAIL
- [ ] **Step 3: Implement API endpoints and Web UI component**
  - Implement `internal/api/runtime_handlers.go` and register routes in `internal/api/server.go`.
  - Implement frontend schemas, API client, and `ProjectRuntimeSettingsModal.tsx` using `@base-ui/react` primitives.
- [ ] **Step 4: Run focused tests to verify green**
  - Command: `go test -v ./internal/api -run TestRuntimeHandlers`
  - Command: `npm test --prefix web -- ProjectRuntimeSettingsModal`
  - Command: `cd web && npx tsc -b`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add internal/api/ web/src/features/ && git commit -m "feat(api,web): add runtime catalog and project settings api and ui"`

---

### Task 9: Full Verification, Clean Build & Lint Gate

**Goal:** Run complete test suites, static analysis, linter checks, and typechecks across backend and frontend in an isolated environment to verify zero regressions.

- [ ] **Step 1: Run complete backend verification**
  - Command: `make check` (or `./scripts/staticcheck.sh && go vet ./... && go test -v -race ./...`)
  - Expected: All packages PASS, 0 staticcheck findings, 0 vet issues.
- [ ] **Step 2: Run complete frontend verification**
  - Command: `npm test --prefix web -- --run`
  - Command: `cd web && npx tsc -b`
  - Expected: All test suites PASS, 0 TypeScript errors.
- [ ] **Step 3: Verify working tree cleanliness**
  - Command: `git status`
  - Expected: Clean working tree, no untracked temporary files.

---

## Planned Execution & Verification Reference

```bash
# Backend unit & static analysis (Planned for execution phase)
make check
./scripts/staticcheck.sh
go vet ./...

# Package unit tests (Planned for execution phase)
go test -v ./internal/models -run TestProject
go test -v ./internal/runner -run TestRuntimeCatalog
go test -v ./internal/runner -run TestRuntimeResolver
go test -v ./internal/sandbox -run TestDocker
go test -v ./internal/runner -run TestRuntimeImageBuilder
go test -v ./internal/runner -run TestProjectSandboxManager
go test -v ./internal/runner -run TestWorkspaceManager
go test -v ./internal/runner -run TestProjectOpenCodeClient
go test -v ./internal/runner -run TestAgentTaskRunner
go test -v ./internal/runner -run TestIdleTracker
go test -v ./internal/api -run TestRuntimeHandlers

# Frontend unit tests & TypeScript verification (Planned for execution phase)
npm test --prefix web -- --run
cd web && npx tsc -b
```

> **Execution Status Note:** The commands above are documented for the future implementation phase. In accordance with the documentation specialist brief, no code tests, dependency installations, or build commands were run while authoring this plan.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-10-10-runtime-image-configuration.md`. Please review the plan. Which execution approach would you prefer?

- **Subagent-driven** - A fresh subagent implements each task and a fresh reviewer checks it before the next one starts, then a whole-branch review at the end. Most thorough; costs a fresh context per task and per review.
- **Native** - I implement every task myself in this session, the way this harness runs work, then one fresh reviewer on the most capable model checks the whole branch. Cheapest and fastest; no independent review until the end. Runs well with a mid-tier session model, since the plan carries the design.

For this plan I recommend **Subagent-driven**, because this refactoring crosses critical security boundaries, container lifecycle management, credential isolation, and database models where per-task fresh verification prevents subtle regressions. Does the plan capture what you want, and which approach should we use?
