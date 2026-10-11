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
  4. Pinned OpenCode distribution artifact:
     - Following user review and approval, the candidate OpenCode artifact pin is the official OpenCode GHCR OCI image manifest digest rather than requiring a standalone binary checksum:
       - GHCR Tag: `ghcr.io/anomalyco/opencode:2.0.26`
       - OCI Index Digest: `sha256:15b62c742c702b9750b7636cf3979823c653ef212e66ab217376ca0f32c1ca71`
       - Candidate Linux/amd64 Manifest Pin: `sha256:ea6c5d68d8d230923d2627bc793e7fa6a03f8e47c253b50397eb92bbde6db255`
     - *Qualification Status:* This digest is adopted strictly as a candidate content pin. Full image composition and provenance are not yet qualified (see Section 7.2).
  5. Zero host volume bind mounts; in-container clone destinations initialized during runtime container setup.
- **Fingerprinting:** Tag format `atm-runtime:<full_sha256>`, where `<full_sha256>` is the 64-character lowercase SHA256 hash of the normalized JSON string containing: target platform (`linux/amd64`), pinned base image digest, control-plane template revision, checksums of native packages and pinned candidate OpenCode distribution artifact digest, and the resolved 6-runtime version/disabled map.
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

### 5.2 Verified OpenCode Auth Contract (Official v2.0.26 Source)

Authentication mechanics have been verified directly against official OpenCode tag `v2.0.26` source code ([anomalyco/opencode](https://github.com/anomalyco/opencode)):

1. **Server Password Configuration (`OPENCODE_PASSWORD`):**
   The HTTP server password is read from the `OPENCODE_PASSWORD` environment variable (the legacy variable `OPENCODE_SERVER_PASSWORD` is also accepted as fallback) via [packages/cli/src/env.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/cli/src/env.ts).
2. **Server Password Initialization in `opencode serve`:**
   When `opencode serve` executes:
   - If `OPENCODE_PASSWORD` is supplied in the environment, the server adopts that explicit password.
   - If no password is provided in the environment or service configuration, `opencode serve` generates a random 32-byte cryptographic password via `randomBytes(32).toString("base64url")`.
   - Verified in [packages/cli/src/server-process.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/cli/src/server-process.ts).
3. **HTTP Basic Authentication Scheme (`opencode:<password>`):**
   The HTTP server authenticates inbound requests using HTTP Basic Auth.
   - The required username is strictly `opencode` (verified in [packages/server/src/auth.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/server/src/auth.ts)).
   - Inbound credentials are decoded from the `Authorization: Basic <base64>` header or the query parameter `?auth_token=<base64>` (verified in [packages/server/src/middleware/authorization.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/server/src/middleware/authorization.ts)).
4. **Correction of Pairing Mechanics (`/api/pair` vs `/auth/connect/:code`):**
   - Earlier claims that `/api/pair` is a public/unauthenticated bootstrap endpoint are **INCORRECT**.
   - `/api/pair` is part of `ServerGroup` and **requires prior authentication** under the `Authorization` middleware (verified in [packages/protocol/src/groups/server.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/protocol/src/groups/server.ts) and [packages/server/src/middleware/authorization.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/server/src/middleware/authorization.ts)).
   - Only the redemption route `/auth/connect/:code` (tested via `isPairingConnectURL`) skips credential checks in the authorization layer because the connect handler consumes and validates the single-use pairing code (verified in [packages/server/src/middleware/authorization.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/server/src/middleware/authorization.ts) and [packages/protocol/src/groups/server.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/protocol/src/groups/server.ts)).
   - Pairing codes are generated via `randomBytes(16).toString("base64url")`, cached in memory with a 5-minute TTL, and single-use (invalidated on consume) (verified in [packages/server/src/pairing.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/server/src/pairing.ts)).
   - Redeeming a code via `/auth/connect/:code` returns a signed stateless session token (`<expires>.<hmac>`) valid for 30 days (`SESSION_TTL_SECONDS = 30 * 24 * 60 * 60`). Rotating the server password immediately invalidates all active session tokens because the HMAC key is derived from the password (verified in [packages/server/src/auth.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/server/src/auth.ts)).
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
8. **Runtime Base OS and Image Composition (Alpine/musl vs. Debian/glibc multi-stage):**
   The upstream OpenCode image `ghcr.io/anomalyco/opencode:2.0.26` builds from an Alpine base (`packages/cli/Dockerfile`) and copies a `linux-x64-baseline-musl` OpenCode binary to `/usr/local/bin/opencode`, installing `libgcc`, `libstdc++`, and `ripgrep`. Meanwhile, the existing ATM runtime catalog and sandbox baseline are built on Debian/glibc (`debian:bookworm-slim`).
   - *Option A (Adopt Alpine Base for ATM Runtime):* Standardize ATM project sandboxes on Alpine, running the musl binary natively with musl-compatible toolchains.
   - *Option B (Multi-Stage Copy into Debian with ABI/Dependency Qualification):* Extract `/usr/local/bin/opencode` from the GHCR candidate image into the Debian base, but only after qualifying ABI compatibility, runtime C library/dynamic link dependencies (or musl compatibility layers such as `gcompat`/`musl`), and companion tools (e.g. `ripgrep`).
   - This architectural decision remains open pending ABI and dependency qualification.

### 7.2 Current Implementation Blockers

1. **Candidate OpenCode GHCR Artifact Discovered but Not Fully Qualified:**
   Following user approval, the official GHCR image digest (`ghcr.io/anomalyco/opencode:2.0.26`, linux/amd64 manifest `sha256:ea6c5d68d8d230923d2627bc793e7fa6a03f8e47c253b50397eb92bbde6db255`) has been selected as the candidate OpenCode content pin rather than requiring a standalone binary checksum. However, full qualification remains an active blocker due to:
   - *Provenance & Build Identity:* Although a registry-attached SLSA provenance attestation exists referencing source repository `https://github.com/anomalyco/opencode` and commit `5e73d5cb1f788637b1b3d5cf1298ea694cbad0c5` (the parent of release tag `v2.0.26` commit `9b4ec5714d481559990db0a816d5dec19541a814`, differing only by version metadata), its `resolvedDependencies` field is false and builder ID is empty. It cannot be characterized as fully verified provenance or signed publisher identity.
   - *Dependency Completeness & ABI Composition:* The binary is packaged in Alpine with `musl`, `libgcc`, `libstdc++`, and `ripgrep`. ABI compatibility and runtime dependencies for ATM container composition are not yet verified.
   - *Runtime Handshake & Auth Smoke:* Container startup handshake, `OPENCODE_PASSWORD` authentication smoke tests, and command-line execution validation have not yet been run and qualified.
2. **Docker/Snap Environment Incompatibility with Mandatory `no-new-privileges`:**
   On the target Linux host running the default container daemon (Docker Engine 29.8.0 installed via Snap revision 3613, containerd 2.3.4, runc 1.5.1), running containers with mandatory `--security-opt no-new-privileges:true` fails during execution.
   - **Reproducible A/B Probe Facts:**
     - Evaluated against existing local image `alpine:latest` (`--pull=never`). Minimal probe parameters applied: `--network none --read-only --cap-drop=ALL --user 65534:65534 --pids-limit 16 --memory 64m --cpus 0.5`, with zero host bind mounts, environment credentials, published ports, repository code, or registry download.
     - **With NNP enabled:**
       ```text
       docker run --rm --pull=never --network none --read-only --cap-drop=ALL --security-opt no-new-privileges:true --user 65534:65534 --pids-limit 16 --memory 64m --cpus 0.5 --entrypoint /bin/sh alpine:latest -c 'grep "^NoNewPrivs:" /proc/self/status; exec /bin/true'
       exec /bin/sh: operation not permitted (exit status 255)
       ```
     - **Control (otherwise identical probe, only NNP omitted):**
       ```text
       docker run --rm --pull=never --network none --read-only --cap-drop=ALL --user 65534:65534 --pids-limit 16 --memory 64m --cpus 0.5 --entrypoint /bin/sh alpine:latest -c 'grep "^NoNewPrivs:" /proc/self/status; exec /bin/true'
       NoNewPrivs: 0 (exit status 0)
       ```
   - **Alternate Daemon Comparison (Docker Desktop Linux VM):**
     - Docker Desktop daemon (Docker Engine 29.2.1, runc 1.3.4) was started temporarily for comparative testing. Only the pre-existing Alpine test image was copied from the default daemon into Desktop without pulling from any external registry.
     - Running the exact same restricted probe with NNP enabled succeeded:
       ```text
       docker --context desktop-linux run --rm --pull=never --network none --read-only --cap-drop=ALL --security-opt no-new-privileges:true --user 65534:65534 --pids-limit 16 --memory 64m --cpus 0.5 --entrypoint /bin/sh alpine:latest -c 'grep "^NoNewPrivs:" /proc/self/status; grep -q "^NoNewPrivs:[[:space:]]*1$" /proc/self/status; exec /bin/true'
       NoNewPrivs: 1 (exit status 0)
       ```
     - Four pre-existing Desktop containers showed `Exited (255)` upon Desktop startup and were completely untouched; none were running. Following the probe, Docker Desktop was stopped and the Docker context was restored to `default`.
     - *Scope & Qualification Limitations:* Docker Desktop executes containers inside a dedicated Linux virtual machine, representing an alternate guest kernel and runtime stack. This pass serves solely as an informational comparison and development test candidate; it does **not** prove compatibility for a native Docker Engine host deployment or ATM's containerized OpenCode runtime.
   - **Root Cause & Workaround Prohibitions:**
     - The captured evidence localizes the execution failure specifically to the `no-new-privileges` interaction on this Snap-backed daemon installation. However, Snap confinement or AppArmor profile conflicts remain an unproven hypothesis, not an established root cause.
     - Workarounds that disable AppArmor, relax Seccomp profiles, or silently omit/bypass `no-new-privileges` are **strictly prohibited**.
     - In accordance with ATM's security specification, `no-new-privileges:true` is **strictly mandatory and fail-closed**. The Docker Desktop pass does not clear overall sandbox qualification, and implementation remains fully blocked until this runtime incompatibility is resolved on supported native hosts alongside other unapproved architectural decisions.
   - **Untested Surface:**
     - This minimal probe evaluated only the Linux kernel execution barrier. It did **not** test workspace volume mounts, OpenCode server runtime processes, authentication handshakes, model provider credential delivery, or network egress filtering.
3. **Gate D Provider Credential Validation Blocked:**
   Validating and testing model provider credential injection into containerized OpenCode servers is blocked until the candidate OpenCode container artifact is qualified, dependency/ABI composition is resolved, and the container is runnable with the required security options.

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
