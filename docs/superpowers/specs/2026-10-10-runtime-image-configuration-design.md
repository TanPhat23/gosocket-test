# Design Specification: Project-Scoped Runtime Container & Sandboxed OpenCode Server Architecture

**Status:** PROPOSED Revision (Awaiting User Review & Approval — Not Implemented)
**Author:** Engineering Coordinator
**Date:** 2026-10-11
**Target:** Agent Task Manager (`agent-task-manager`)
**Specification Document:** `docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md`

---

## 1. Problem Statement & Baseline Grounding

1. **Floating Baseline & Incomplete Runtimes:**
   `deploy/sandbox/Dockerfile` currently combines floating `debian:bookworm-slim` apt packages with floating `golang:alpine` multi-stage copies. Live inspection of unpinned containers yields outdated baseline tools, while Java, Rust, and .NET are completely absent.
2. **Control Plane Host Execution Leakage:**
   `internal/runner/dependencies.go` runs host-level `npm install` and `go mod download` directly on `worktreePath` and `baseRepoPath`. Furthermore, `internal/runner/task_runner.go` falls back to `runOnHost` when container creation fails.
3. **Execution Confinement Risk:**
   Executing `opencode run` or issuing prompts against a host-bound OpenCode server risks routing tools, child agents, and commands outside container boundaries onto the control plane host.
4. **Project Confinement vs. Multi-Repo Workflow Requirement:**
   Users organize work by projects. A single ATM project can contain multiple git repositories and multiple concurrent or sequential tasks. Previous task-scoped container proposals imposed excessive container lifecycle churn and host volume bind mounts.
5. **Approved Direction & Architectural Scope:**
   Provide isolated, reproducible, content-addressed cached **per-project runtime container environments** on `linux/amd64` (matching current deployment), with sandboxed OpenCode server execution, fail-closed security controls, zero host fallback, in-container cloning (no host bind-mounts), and strict inter-project isolation.

---

## 2. Project Boundary, Trust Model & Workspace Lifecycle

### 2.1 Project Sandbox Boundary vs. Task/Repo Trust
- **Hard Security Boundary (Container-Enforced):**
  The Docker sandbox container is scoped **strictly per ATM project** (`atm-project-<project_id>`). Different projects are mutually isolated into separate Docker sandbox containers running on distinct Docker bridge networks without shared filesystems or shared daemon processes. Cross-project access is strictly prevented at the container boundary.
- **Shared Trust Boundary (Intra-Project):**
  Repositories and tasks belonging to the **same project share a single trust boundary**.
  - All tasks within Project A execute inside Project A's container sandbox.
  - All repositories cloned for Project A reside within Project A's container filesystem.
  - **Explicit Non-Guarantee:** There is **NO task-level or repository-level security isolation** within a project container. Tasks and repositories within the same project can see each other's filesystem contents and shared process environment. Task-scoped workspaces, worktrees, or OpenCode sessions inside the project container are operational conveniences for branch management and conversational routing; **they are NOT security boundaries**.
- **Historical Alternative (Informational Only):**
  The previous design specified task-scoped containers (`atm-task-<task_id>`) mounting host worktrees via `/workspace:rw`. This was abandoned because: (a) it required host-level bind-mounts exposing the host control-plane filesystem; (b) it could not cleanly support multi-repository project workflows; and (c) container setup latency degraded interactive prompt steering across rapid task turns.

### 2.2 In-Container Repository Cloning (Zero Host Bind-Mounts)
- Host worktrees and host bind-mounts (`/workspace:rw` pointing to host paths) are **strictly eliminated**.
- Repositories required by a project are cloned, fetched, and checked out **inside the project container sandbox** (e.g. into `/workspaces/<repo_name>` or `/workspaces/<task_id>`).
- The control plane never bind-mounts host source trees, host git caches, or control-plane directories into the container.
- Persistent project storage is managed via dedicated Docker volumes (`atm-project-data-<project_id>`) attached to the project container, retaining repository state and branch artifacts across task executions.

---

## 3. Configuration & Precedence Specification

### 3.1 Repository Manifest: `.atm-runtime.json`
Optional manifest placed at repository root, defining desired runtime version bindings:
```json
{
  "schemaVersion": 1,
  "runtimes": {
    "go": "1.27.2",
    "node": "24.21.0",
    "python": "3.11.17",
    "java": false,
    "rust": "1.99.0",
    "dotnet": false
  }
}
```
- `schemaVersion`: Must equal integer `1`.
- Runtime keys: Exactly `go`, `node`, `python`, `java`, `rust`, `dotnet`. Values must be exact catalog version strings or boolean `false`.
- **Approved Runtime Catalog Decisions:**
  - **Baseline Runtimes (Default Enabled if omitted):**
    - `go`: `"1.27.2"`
    - `node`: `"24.21.0"`
    - `python`: `"3.11.17"`
  - **Opt-In Runtimes (Default Disabled `false` if omitted):**
    - `java`: `"25.0.4.1+1"`
    - `rust`: `"1.99.0"`
    - `dotnet`: `"10.0.401"`
- **Multi-Repository Reality & Manifest Scope:** In multi-repository projects, different repositories may each contain an `.atm-runtime.json`. Manifest reconciliation across repositories is an open decision tracked in Section 7.1; container image resolution must not arbitrarily assume an undeclared single "primary" repo.

### 3.2 Project UI/DB Overrides & Precedence
- Structured metadata field in `models.Project` (`runtime_config`).
- Per runtime: `inherit` (unset override), exact catalog version, or `disabled` (`false`).
- **Hierarchy:** UI Override > Manifest (`.atm-runtime.json`) > Pinned Catalog Baseline.
- **Fail-Closed Validation:** An invalid manifest (schema/syntax violation or uncataloged version) fails container startup and task attempts immediately. A UI override does *not* mask or forgive an invalid manifest. Even when no configuration file exists, the project resolves to the pinned catalog baseline profile fail-closed.

---

## 4. Catalog & Deterministic Builder Architecture

### 4.1 Content-Addressed Caching & Deterministic Build Recipe
- Dynamic images are built only from control-plane trusted recipes. Repository source code and user credentials are never included in the image build context.
- **Deterministic Build Recipe:**
  1. Base OS image pinned by immutable container digest (`debian:bookworm-slim@sha256:...`, exact digest pinned at implementation).
  2. Operating system dependencies pinned via package snapshot or immutable version archives, avoiding floating upstream package indexes.
  3. Language runtimes extracted from pinned, checksum-verified distribution archives into isolated paths (using the approved catalog versions: Go 1.27.2, Node 24.21.0, Python 3.11.17; Java 25.0.4.1+1, Rust 1.99.0, .NET 10.0.401).
  4. Pinned OpenCode distribution binary and assets verified by cryptographic checksum.
  5. Zero host volume bind mounts; in-container clone destinations initialized during runtime container setup.
- **Fingerprinting:** Tag format `atm-runtime:<full_sha256>`, where `<full_sha256>` is the 64-character lowercase SHA256 hash of the normalized JSON string containing: target platform (`linux/amd64`), pinned base image digest, control-plane template revision, checksums of native packages and pinned OpenCode distribution, and the resolved 6-runtime version/disabled map.
- **Build Coalescing & Cancellation:** Identical pending builds share a single in-flight build. Cancellation detaches the waiter; the shared build proceeds as long as at least one waiter remains.

---

## 5. Sandboxed OpenCode Architecture & Verified Auth Model

### 5.1 Architecture Overview: Project Sandbox Container

```
┌────────────────────────────────────────────────────────────────────────┐
│               Control Plane Runner (AgentTaskRunner)                   │
│                                                                        │
│   Project OpenCodeClient ───(Daemon-Validated Project Route)────────┐  │
│   (HTTP Basic: opencode:<OPENCODE_PASSWORD>, HTTP APIs)            │  │
└────────────────────────────────────────────────────────────────────┼───┘
                                                                     │ HTTP (Container IP/Net)
                                                                     ▼
┌────────────────────────────────────────────────────────────────────────┐
│             Project Sandbox Container (atm-runtime:<sha256>)           │
│             Container Name: atm-project-<project_id>                   │
│                                                                        │
│   Dedicated User-Defined Bridge Network (Outbound NAT, No Host Ports)  │
│   HostConfig: CapDrop=ALL, no-new-privileges=true, CPU/Mem/PID limits  │
│   Read-Only Root Filesystem, Writable tmpfs on approved paths          │
│                                                                        │
│   ┌────────────────────────────────────────────────────────────────┐   │
│   │  OpenCode Server Process (Container Sandboxed)                 │   │
│   │  - Auth: OPENCODE_PASSWORD set per project                     │   │
│   │  - Config: Minimal vetted plugins, local tools only            │   │
│   │  - External MCP execution disabled; no host credentials/plugins│   │
│   │  - Model Provider Auth: Injected per attempt/project env       │   │
│   └───────────────┬────────────────────────────────┬───────────────┘   │
│                   │ tool execution                 │ child subagents   │
│                   ▼                                ▼                   │
│         /workspaces (In-Container Repos)   In-Container Shell / Tools  │
│                                                                        │
│   Volume Mounts:                                                       │
│   - atm-project-data-<project_id>:/workspaces:rw (Cloned Repos)        │
│   - atm-project-state-<project_id>:/var/lib/opencode:rw                │
│   - tmpfs:/tmp:rw,noexec,nosuid,size=512m                              │
│   (NO HOST BIND MOUNTS)                                                │
└────────────────────────────────────────────────────────────────────────┘
```

### 5.2 Verified OpenCode Auth Contract (Official v2.0.23 Source)

Authentication mechanics have been verified directly against official OpenCode tag `v2.0.23` source code ([anomalyco/opencode](https://github.com/anomalyco/opencode)):

1. **Server Password Configuration (`OPENCODE_PASSWORD`):**
   The HTTP server password is read from the `OPENCODE_PASSWORD` environment variable (the legacy variable `OPENCODE_SERVER_PASSWORD` is also accepted as fallback) via [packages/cli/src/env.ts:10-13](https://github.com/anomalyco/opencode/blob/v2.0.23/packages/cli/src/env.ts#L10-L13).
2. **Server Password Initialization in `opencode serve`:**
   When `opencode serve` executes:
   - If `OPENCODE_PASSWORD` is supplied in the environment, the server adopts that explicit password.
   - If no password is provided in the environment or service configuration, `opencode serve` generates a random 32-byte cryptographic password via `randomBytes(32).toString("base64url")`.
   - Verified in [packages/cli/src/server-process.ts:82-88](https://github.com/anomalyco/opencode/blob/v2.0.23/packages/cli/src/server-process.ts#L82-L88).
3. **HTTP Basic Authentication Scheme (`opencode:<password>`):**
   The HTTP server authenticates inbound requests using HTTP Basic Auth.
   - The required username is strictly `opencode` (verified in [packages/server/src/auth.ts:20](https://github.com/anomalyco/opencode/blob/v2.0.23/packages/server/src/auth.ts#L20)).
   - Inbound credentials are decoded from the `Authorization: Basic <base64>` header or the query parameter `?auth_token=<base64>` (verified in [packages/server/src/middleware/authorization.ts:31-37](https://github.com/anomalyco/opencode/blob/v2.0.23/packages/server/src/middleware/authorization.ts#L31-L37)).
4. **Correction of Pairing Mechanics (`/api/pair` vs `/auth/connect/:code`):**
   - Earlier claims that `/api/pair` is a public/unauthenticated bootstrap endpoint are **INCORRECT**.
   - `/api/pair` is part of `ServerGroup` and **requires prior authentication** under the `Authorization` middleware (verified in [packages/protocol/src/groups/server.ts:53-62](https://github.com/anomalyco/opencode/blob/v2.0.23/packages/protocol/src/groups/server.ts#L53-L62) and [packages/server/src/middleware/authorization.ts:77-91](https://github.com/anomalyco/opencode/blob/v2.0.23/packages/server/src/middleware/authorization.ts#L77-L91)).
   - Only the redemption route `/auth/connect/:code` (tested via `isPairingConnectURL`) skips credential checks in the authorization layer because the connect handler consumes and validates the single-use pairing code (verified in [packages/server/src/middleware/authorization.ts:83](https://github.com/anomalyco/opencode/blob/v2.0.23/packages/server/src/middleware/authorization.ts#L83) and [packages/protocol/src/groups/server.ts:36-38](https://github.com/anomalyco/opencode/blob/v2.0.23/packages/protocol/src/groups/server.ts#L36-L38)).
   - Pairing codes are generated via `randomBytes(16).toString("base64url")`, cached in memory with a 5-minute TTL, and single-use (invalidated on consume) (verified in [packages/server/src/pairing.ts:7-32](https://github.com/anomalyco/opencode/blob/v2.0.23/packages/server/src/pairing.ts#L7-L32)).
   - Redeeming a code via `/auth/connect/:code` returns a signed stateless session token (`<expires>.<hmac>`) valid for 30 days (`SESSION_TTL_SECONDS = 30 * 24 * 60 * 60`). Rotating the server password immediately invalidates all active session tokens because the HMAC key is derived from the password (verified in [packages/server/src/auth.ts:16-56](https://github.com/anomalyco/opencode/blob/v2.0.23/packages/server/src/auth.ts#L16-L56)).
5. **OpenAPI `security: []` Annotations vs. Global Process Dispatch Middleware:**
   - In OpenCode's protocol schema definitions, individual endpoint declarations (such as those in `packages/protocol/src/groups/`) frequently omit security schemes or produce OpenAPI specs that display `security: []`.
   - However, the HTTP server process dispatch (`packages/server/src/process.ts` and `@opencode/protocol/src/api.ts`) mounts `Authorization` middleware globally across all endpoint groups (`HttpApiGroup.addMiddleware`).
   - Consequently, when `OPENCODE_PASSWORD` is configured, HTTP Basic authentication is enforced globally on every route except the explicitly ticketed/exempt handlers (`isPairingConnectURL`, `hasPtyConnectTicketURL`, and `hasPersistentPtyConnectTicketURL`). Tool and client integrators must not mistake OpenAPI `security: []` annotations for unauthenticated endpoints.
6. **Control Plane Integration:**
   The ATM control plane must configure an explicit `OPENCODE_PASSWORD` per project container when launching the sandbox, and ATM's `OpenCodeClient` must communicate using standard HTTP Basic authentication (`opencode:<project_password>`).
7. **Distinction from Model Provider Credentials:**
   `OPENCODE_PASSWORD` is an internal control-plane transport credential for communicating with the sandboxed OpenCode HTTP API. It is completely distinct from model provider credentials (such as `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`), which are injected separately to allow the agent to invoke LLMs.

---

## 6. Security Hardening & Confinement Guarantees

### 6.1 Preserved Strict Security Requirements
1. **Fail-Closed Privilege Enforcement (`no-new-privileges`):**
   The container must be executed with `--security-opt no-new-privileges:true`. If the underlying container runtime rejects this option, the control plane **must fail closed immediately**. It is strictly forbidden to silently relax or bypass this control.
2. **Docker Socket Isolation:**
   No container may ever have access to `/var/run/docker.sock` or any Docker daemon interface.
3. **Host Mount Elimination:**
   Zero host bind mounts. No host user configs (`~/.config/opencode`), no host cache directories, and no host source trees are mounted into containers.
4. **Least Privilege Execution:**
   - Dropped Linux capabilities: `CapDrop: ALL`.
   - Execution as non-root user (`USER opencode:opencode`, UID 1000) compatible with language toolchains.
5. **Read-Only Root Filesystem:**
   Container root filesystem is mounted read-only (`--read-only`), with explicitly declared, bounded, writable tmpfs mounts for necessary runtime scratch space.
6. **Resource Bounds:**
   Strict memory quotas, CPU limits, and PID limits enforced on the project container.
7. **Control-Plane Network Isolation & Routing:**
   Project containers run on isolated user-defined Docker bridge networks without published host ports. Control plane communicates via daemon-inspected internal container IPs. Arbitrary user-supplied URLs are strictly prohibited to prevent SSRF.

---

## 7. Unresolved Design Questions & Implementation Blockers

The following items are unresolved architectural decisions and active implementation blockers that must be formally resolved and qualified before proceeding to production implementation:

### 7.1 Unresolved Design Questions

1. **Multi-Repository Runtime Manifest & Profile Resolution:**
   Because a single ATM project can contain multiple git repositories, each repository might define its own `.atm-runtime.json` with differing or conflicting runtime version requirements. Since container execution is sandboxed at the project level, a single runtime image must satisfy the project's requirements. Several resolution strategies remain open:
   - *Option A (Project Manifest Precedence / Dedicated Primary Repo):* The user explicitly designates one repository as the primary runtime manifest source in project settings, or project UI/DB overrides take absolute precedence over repository manifests.
   - *Option B (Manifest Merging / Union):* ATM parses manifests from all declared project repositories and computes the union of required runtimes. If different repositories declare conflicting versions of the same runtime (e.g., Repo 1 specifies Node 24.21.0 while Repo 2 specifies Node 22.14.0), resolution fails closed immediately, requiring the user to set an explicit project-level override.
   - *Option C (UI/DB Override Only for Multi-Repo):* Multi-repo projects require explicit project-level runtime configurations in the ATM UI/DB, treating repo-level `.atm-runtime.json` files as informational hints or advisory checks.
2. **OpenCode Server Process Topology (Per-Project vs. Per-Task):**
   While container sandboxing is established at the **project boundary**, whether to run a single long-lived OpenCode server process per project container or to spawn separate OpenCode server processes per task inside that project container remains an open architectural decision:
   - *Option A (Single Project Server):* Low memory overhead; single OpenCode database per project. Requires validating how OpenCode handles concurrent multi-repo sessions and directory switching.
   - *Option B (Task-Scoped Server Processes inside Project Container):* Isolates agent crash domains and simplifies per-task session teardown, but consumes more memory per task.
3. **Safe GitHub Clone Credential Delivery:**
   Repositories must be cloned inside the project container without exposing host git credentials or persisting GitHub personal access tokens (PATs) / OAuth tokens on container disk or in bash history where untrusted project agents could leak them. Requires design of a secure in-container credential helper, short-lived installation token, or control-plane clone proxy.
4. **Task Workspace Layout & Concurrency inside Shared Project Boundary:**
   Because tasks within a project share the container filesystem, multiple tasks running concurrently could experience git lock contention or workspace collisions if operating in the same working tree. Requires deciding whether each task receives a git worktree (`/workspaces/<repo>/tasks/<task_id>`), separate cloned trees, or mutually exclusive task execution locks per repository.
5. **Outbound Egress & Cloud Metadata Blocking:**
   Containers need outbound internet NAT to reach LLM APIs and public package registries (npm, Go proxy, PyPI, Maven, crates.io, NuGet). However, open outbound NAT exposes risks of SSRF against cloud provider metadata endpoints (`169.254.169.254`) and internal control-plane services. A concrete iptables/firewall egress policy must be designed.
6. **Bounded Writable Tmpfs Locations for Read-Only Root Filesystem:**
   Running with `--read-only` rootfs requires identifying all directories that toolchains and OpenCode require for temporary scratch space (e.g. `/tmp`, `/var/tmp`, `/var/lib/opencode/tmp`, `/run`) and establishing bounded memory limits (`tmpfs:size=...`, `noexec`, `nosuid`) that do not exhaust host memory or bypass execution restrictions.
7. **Strict `no-new-privileges` vs. Explicitly Approved Exceptions:**
   Given the current runtime environment failure (detailed below), the policy must decide whether ATM strictly refuses to run on environments where `no-new-privileges` fails, or if an explicitly configured, user-approved platform exception can ever exist.

### 7.2 Current Implementation Blockers

1. **Checksum-Verifiable OpenCode V2 Distribution Artifact Unresolved:**
   ATM requires an official, checksum-verifiable distribution binary/archive for OpenCode V2 on `linux/amd64` to build deterministic container images. The exact pinned release artifact, upstream download URL, cryptographic SHA256 checksum, and container startup handshake test have not yet been satisfied and qualified.
2. **Docker/Snap Environment Incompatibility with `no-new-privileges`:**
   On the target Linux host running Docker Engine 29.8.0 via Snap, executing containers with `--security-opt no-new-privileges` currently fails with:
   ```text
   docker run --rm --security-opt no-new-privileges debian:bookworm-slim /bin/sh -c "echo hello"
   exec /bin/sh: operation not permitted (exit status 255)
   ```
   Because our security contract requires **failing closed** if `no-new-privileges` cannot be enforced, strict sandbox qualification cannot pass in this environment until Docker AppArmor/Snap confinement profiles are reconciled or a native Docker Engine is provided.
3. **Gate D Provider Credential Validation Blocked:**
   Validating and testing model provider credential injection into containerized OpenCode servers is blocked until the exact OpenCode V2 container binary is qualified and runnable with the required security options.

---

## 8. Proposed Interfaces & API Contracts (Proposed — Not Implemented)

### 8.1 Proposed Go Interfaces (`internal/runner/`)
- `type RuntimeResolver interface { Resolve(ctx context.Context, repoRoot string, project *models.Project) (*ResolvedRuntimeProfile, error) }`
- `type RuntimeImageBuilder interface { EnsureImage(ctx context.Context, profile *ResolvedRuntimeProfile, onProgress func(string)) (string, error) }`
- `type ProjectSandboxManager interface { EnsureProjectContainer(ctx context.Context, project *models.Project, imageTag string) (endpoint string, err error); StopProjectContainer(ctx context.Context, projectID string) error }`
- Proposed addition to `internal/sandbox/DockerClient`: `ImageBuild(ctx context.Context, buildContext io.Reader, options image.BuildOptions) (image.BuildResponse, error)`.

### 8.2 Proposed HTTP Endpoints
- `GET /api/v1/projects/:id/runtime`:
  - Returns project runtime overrides and supported catalog metadata.
  - Requires Team Role `RoleViewer` (10) or higher.
- `PUT /api/v1/projects/:id/runtime`:
  - Updates project runtime overrides.
  - Requires Team Role `RoleAdmin` (30) or `RoleOwner` (40).
  - Validates that pinned versions exist in the catalog; strictly rejects arbitrary images or build arguments.
- `GET /api/v1/runtimes/catalog`:
  - Returns maintained and qualified catalog entries for all 6 runtimes on `linux/amd64`.

---

## 9. Out of Scope (Non-Goals)

- Isolation between tasks or repositories within the same project (shared project trust boundary).
- Automatic background garbage collection for cached runtime images (`atm-runtime:*`).
- Custom user-provided Docker images or external registry references.
- OS distribution selection or architecture switches in the UI.
- Arbitrary build arguments, environment injection, or custom build scripts.
