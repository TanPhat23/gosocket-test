# Design Specification: Project-Scoped Runtime Image Configuration & Task-Scoped Container Server Architecture

**Status:** Written Specification Approved  
**Author:** Engineering Coordinator  
**Date:** 2026-10-10  
**Target:** Agent Task Manager (`agent-task-manager`)  
**Specification Document:** `docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md`

---

## 1. Problem Statement & Baseline Grounding

1. **Floating Baseline & Incomplete Runtimes:**  
   `deploy/sandbox/Dockerfile` currently combines floating `debian:bookworm-slim` apt packages with floating `golang:alpine` multi-stage copies. Live inspection yields Go 1.27.1, Node 18.20.4, and Python 3.11.2, while Java, Rust, and .NET are completely absent.
2. **Control Plane Host Execution Leakage:**  
   `internal/runner/dependencies.go` runs host-level `npm install` and `go mod download` directly on `worktreePath` and `baseRepoPath`. Furthermore, `internal/runner/task_runner.go` falls back to `runOnHost` when container creation fails.
3. **Execution Confinement Risk:**  
   Executing `opencode run` or issuing prompts against a host-bound OpenCode server risks routing tools, child agents, and commands outside container boundaries onto the control plane host.
4. **Requirement:**  
   Provide isolated, reproducible, content-addressed cached per-project runtime environments for Go, Node, Python, Java, Rust, and .NET on `linux/amd64` (matching current deployment), with task-scoped containerized OpenCode servers, fail-closed execution, zero host fallback, and no host dependency installation.

---

## 2. Configuration & Precedence Specification

### 2.1 Repository Manifest: `.atm-runtime.json`
Located at the root of the checked-out worktree, resolved once prior to task execution:
```json
{
  "schemaVersion": 1,
  "runtimes": {
    "go": "1.27.1",
    "node": "22.14.0",
    "python": false,
    "java": false,
    "rust": "1.85.0",
    "dotnet": false
  }
}
```
- `schemaVersion`: Must equal integer `1`.
- Runtime keys: Exactly `go`, `node`, `python`, `java`, `rust`, `dotnet`. Values must be exact catalog version strings or boolean `false`.
- **Baseline vs. Opt-In Defaults:** If omitted, baseline runtimes (`go`, `node`, `python`) default to their pinned catalog baseline versions. Opt-in runtimes (`java`, `rust`, `dotnet`) default to `false` (disabled) if omitted.
- **Sample Version Disclaimer:** All sample versions shown above (e.g. Go `"1.27.1"`, Node `"22.14.0"`, Rust `"1.85.0"`) are illustrative formatting examples only and do not constitute catalog support commitments.

### 2.2 Project UI/DB Overrides & Precedence
- Proposed addition: structured metadata field in `models.Project` (`runtime_config`).
- Per runtime: `inherit` (unset override), exact catalog version, or `disabled` (`false`).
- **Hierarchy:** UI Override > Manifest (`.atm-runtime.json`) > Pinned Catalog Baseline.
- **Fail-Closed Validation:** An invalid manifest (schema/syntax violation or uncataloged version) fails the task attempt immediately. A UI override does *not* mask or forgive an invalid manifest. Even when no configuration file exists, the task resolves to the pinned catalog baseline profile fail-closed.

---

## 3. Catalog & Deterministic Builder Architecture

### 3.1 Version Catalog & Qualification
- No unverified version lists are committed in this design. Only the observed baseline (Go 1.27.1, Node 18.20.4, Python 3.11.2) is known in the existing image.
- **Release Acceptance Requirement:** Prior to release, a strictly tested and maintained catalog must define explicitly qualified version entries for all 6 runtimes on `linux/amd64`, including upstream archive URLs, SHA256 checksums, and verification tests. Future architectures (such as `linux/arm64`) must be qualified separately.

### 3.2 Content-Addressed Caching & Deterministic Build Recipe
- Dynamic images are built only from control-plane trusted recipes. Repository source code and user credentials are never included in the image build context.
- **Deterministic Build Recipe:**
  1. Base OS image pinned by immutable container digest (`debian:bookworm-slim@sha256:...`, exact digest pinned at implementation).
  2. Operating system dependencies pinned via package snapshot or immutable version archives, avoiding floating upstream package indexes.
  3. Language runtimes extracted from pinned, checksum-verified distribution archives into isolated paths.
  4. Pinned OpenCode distribution binary and assets verified by cryptographic checksum.
  5. Worktree bind-mounted at `/workspace:rw` at container execution time.
- **Fingerprinting:** Tag format `atm-runtime:<full_sha256>`, where `<full_sha256>` is the 64-character lowercase SHA256 hash of the normalized JSON string containing: target platform (`linux/amd64`), pinned base image digest, control-plane template revision, checksums of native packages and pinned OpenCode distribution, and the resolved 6-runtime version/disabled map.
- **Build Coalescing & Cancellation:** Identical pending builds share a single in-flight build. Task cancellation detaches the task waiter. The shared build proceeds as long as at least one waiter remains; it terminates if the last waiter cancels.

---

## 4. Task-Scoped Container Server Architecture (Approved Design)

### 4.1 Architecture Decision: Task-Scoped Container OpenCode Server
Rather than executing against a shared host OpenCode daemon, each task executes against a **task-scoped OpenCode server running inside its resolved runtime container sandbox**.

```
┌────────────────────────────────────────────────────────────────────────┐
│               Control Plane Runner (AgentTaskRunner)                   │
│                                                                        │
│   Reusable OpenCodeClient ───(Daemon-Validated Task Route)─────────┐   │
│   (Auth, Session Lifecycle, Compact, Steer, Interrupt)             │   │
└────────────────────────────────────────────────────────────────────┼───┘
                                                                     │ HTTP (Container IP/Net)
                                                                     ▼
┌────────────────────────────────────────────────────────────────────────┐
│               Task Sandbox Container (atm-runtime:<sha256>)            │
│                                                                        │
│   Dedicated User-Defined Bridge Network (Outbound NAT, No Host Ports)  │
│   HostConfig: CapDrop=ALL, no-new-privileges=true, CPU/Mem/PID limits  │
│                                                                        │
│   ┌────────────────────────────────────────────────────────────────┐   │
│   │  OpenCode Server (Task-Scoped Instance, Local Only)            │   │
│   │  - Config: Minimal vetted plugins, local tools only            │   │
│   │  - External MCP execution disabled; no host credentials/plugins│   │
│   │  - Model Provider Auth: Injected per-task env, never in image  │   │
│   └───────────────┬────────────────────────────────┬───────────────┘   │
│                   │ tool execution                 │ child subagents   │
│                   ▼                                ▼                   │
│         /workspace (Worktree)            In-Container Shell / Tools    │
│                                                                        │
│   Volume Mounts:                                                       │
│   - /workspace:rw (Host Task Worktree)                                 │
│   - /root/.local/share/opencode:rw (Task-Dedicated State Volume)       │
└────────────────────────────────────────────────────────────────────────┘
```

### 4.2 Verified Execution & Tool Confinement Contract
1. **Full Sandbox Tool Confinement:**  
   Every agent turn, child subagent, bash/exec process, dependency preparation, and preflight/repair verification command runs strictly inside the resolved container sandbox via the task-scoped server or container `docker exec`. Host-level execution (`runOnHost`, host `InstallDependencies`, host test runners) is strictly eliminated.
2. **Reusable `OpenCodeClient` Routing & Existing RBAC Authorization:**  
   - Control plane communicates with the task container using a server-owned, daemon-validated internal endpoint.
   - User requests route through existing ATM authorization before reaching `OpenCodeClient`:
     - **Snapshot Reads:** Allowed for users satisfying `RoleViewer` (10) or higher.
     - **Follow-up Turns, Steer Prompts, and Cancellations:** Require `RoleMember` (20) or higher.
     - **Project Runtime Overrides:** Require `RoleAdmin` (30) or `RoleOwner` (40).
   - Control plane routes prompt steering, message polling, compaction, child tracking, and interruption exclusively through the daemon-validated binding. Arbitrary external or user-supplied URLs are strictly prohibited to prevent Server-Side Request Forgery (SSRF).
3. **Session State, Persistence & Snapshot Reads:**  
   - Session data is stored in a task-dedicated Docker volume (`atm-task-data-<task_id>`).
   - Control plane enforces single-writer access per task session volume.
   - When a task stops or completes, the execution container is stopped and removed, but its task state volume is retained for bounded history queries and follow-up turns.
   - **Snapshot Reads for Stopped Servers:** Historical messages and transcripts are read as immutable static snapshots from the retained state. **A snapshot read never authorizes or triggers model invocations.**
   - **Follow-up Turns & Restarts:** Follow-up prompts start a fresh container attached to the existing task state volume. Starting or resuming a task re-resolves the project runtime profile; active task executions preserve their pinned profile until restarted.
   - Task deletion explicitly removes retained task state volumes.
4. **Cancellation Lifecycle Contract:**  
   - On cancellation request, the control plane initiates a bounded graceful interrupt directed at the root session and active child subagents, followed by an immediate transcript snapshot capture.
   - A bounded grace period is allowed for graceful shutdown and snapshot capture before the container is stopped and removed (`ContainerStop` with bounded timeout, followed by `ContainerRemove`).
   - If the container or server process becomes unresponsive, the control plane initiates a force stop (`kill`/forced removal), preserves the task state volume, records the last successfully captured transcript snapshot, and marks the task attempt as incomplete/canceled.
   - **Non-blocking Cancellation:** Cancellation is never delayed indefinitely, and flushed state is never falsely guaranteed if an agent hangs.
5. **Security Hardening, Egress Realities & Credential Scoping:**  
   - **Network Topology & Egress:** Containers run on a dedicated user-defined Docker bridge network without published host ports and without access to `/var/run/docker.sock`. This is *not* an isolated internal-only (`--internal`) network: outbound NAT is explicitly permitted to allow provider LLM API calls and package manager downloads. Fine-grained egress filtering is *not* implemented at the container level; SSRF and internal service boundary isolation remain recognized risks that require external network firewall policy qualification.
   - **Container Security Profile:** `CapDrop: ALL`, `no-new-privileges: true`, CPU quota, memory limits, and PID limits. Containers execute as non-root where compatible with the language toolchain.
   - **Plugin & MCP Restrictions:** Container configuration permits only minimal vetted local tools. External MCP servers and inheritance of host-side plugins or user tokens are strictly disabled.
   - **Credential Scoping & State Volume Reality:** Model provider credentials (API keys) are injected into the container environment per task attempt and are never written to container images, logged, or written to host control-plane disk. While the control plane aims to avoid credential persistence, OpenCode itself might write or cache environment or session artifacts into the task state volume (`/root/.local/share/opencode`). Therefore, release qualification must verify the behavior of the pinned OpenCode binary version, restrict access to the task-scoped volume, and purge the volume completely upon task deletion. No fictional volume-level encryption is claimed.
6. **Alternative Tradeoffs Evaluated:**
   - *Standalone CLI Export (`opencode run --standalone`):* Rejected for interactive tasks. While simple, standalone mode does not natively support interactive session steering, mid-turn compaction, live question answering, or subagent message resumption without spinning up independent processes that re-parse disk state.
   - *Shared Host OpenCode Dispatcher:* Rejected due to tool escape risks. Running a single multi-tenant server on the host exposes the host filesystem to untrusted repository tools and leaks environment variables across projects.
   - *Selected Task-Scoped Private Container Server (Adopted):* Provides complete process and filesystem isolation, supports full interactive API capabilities, guarantees in-container tool execution, and scopes state cleanly per task.

---

## 5. Proposed Interfaces & API Contracts (Proposed — Not Implemented)

### 5.1 Proposed Go Interfaces (`internal/runner/`)
- `type RuntimeResolver interface { Resolve(ctx context.Context, worktreePath string, project *models.Project) (*ResolvedRuntimeProfile, error) }`
- `type RuntimeImageBuilder interface { EnsureImage(ctx context.Context, profile *ResolvedRuntimeProfile, onProgress func(string)) (string, error) }`
- `type TaskSandboxManager interface { StartTaskServer(ctx context.Context, task *models.Task, imageTag string) (endpoint string, err error); StopTaskServer(ctx context.Context, taskID string) error }`
- Proposed addition to `internal/sandbox/DockerClient`: `ImageBuild(ctx context.Context, buildContext io.Reader, options image.BuildOptions) (image.BuildResponse, error)`.

### 5.2 Proposed HTTP Endpoints
- `GET /api/v1/projects/:id/runtime`:
  - Returns project overrides and supported catalog metadata.
  - Returns resolved effective values only when repository checkout evidence or an explicit commit ref is available; otherwise returns overrides and catalog metadata with an unresolved preview status.
  - Requires Team Role `RoleViewer` (10) or higher.
- `PUT /api/v1/projects/:id/runtime`:
  - Updates project runtime overrides.
  - Requires Team Role `RoleAdmin` (30) or `RoleOwner` (40) (matching `internal/models/enums.go` RBAC definitions).
  - Validates that pinned versions exist in the catalog; strictly rejects arbitrary images or build arguments.
- `GET /api/v1/runtimes/catalog`:
  - Returns maintained and qualified catalog entries for all 6 runtimes on `linux/amd64`.

### 5.3 Proposed Verification & Test Plan (Planned — Not Yet Run)
- **Unit & Resolution Tests:** Manifest parsing, fail-closed validation on malformed manifests or uncataloged versions, and full 64-character SHA256 cache key calculation.
- **API Routing & SSRF Adversarial Gates:**
  - Verify `RoleViewer` (10) can access snapshot reads but cannot submit prompt steering, trigger model invocations, or initiate cancellations.
  - Verify `RoleMember` (20) can submit follow-up prompts and cancellations but cannot update project runtime overrides.
  - Validate that `OpenCodeClient` routes strictly to daemon-assigned internal container IP/ports and rejects arbitrary user-specified or external URLs.
- **Cancellation & Unresponsive Container Lifecycle Tests:**
  - Verify graceful interrupt and snapshot capture on cancellation.
  - Adversarially simulate an unresponsive, hanging child process during cancellation: verify that bounded timeout triggers force-stop, preserves task state volume with last captured snapshot, marks attempt incomplete, and returns without deadlock.
- **Volume Isolation & Credential Cache Purge Tests:**
  - Verify single-writer volume locking prevents concurrent tasks from mounting the same task volume.
  - Verify that deleting a task purges its dedicated volume (`atm-task-data-<task_id>`), eliminating any residual OpenCode session caches.
- **Real Toolchain & Child Agent Smoke Tests:** Concrete verification on `linux/amd64` testing actual compilation and test execution inside generated sandboxes for all 6 language runtimes (`go`, `node`, `python`, `java`, `rust`, `dotnet`) and verifying child subagent shell commands execute inside the container boundary.

---

## 6. Out of Scope (Non-Goals)
- Automatic background garbage collection for cached runtime images (`atm-runtime:*`).
- Custom user-provided Docker images or external registry references.
- OS distribution selection or architecture switches in the UI.
- Arbitrary build arguments, environment injection, or custom build scripts.
