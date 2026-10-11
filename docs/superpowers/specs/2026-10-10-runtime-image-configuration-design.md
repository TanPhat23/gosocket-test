# Design Specification: Project-Scoped Runtime Container & Sandboxed OpenCode Server Architecture

**Status:** APPROVED FOR PLANNING (Not Implemented; implementation blocked pending qualification gates)
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

### 2.2 In-Container Repository Storage & Task Worktrees (Zero Host Bind-Mounts)
- Host worktrees and host bind-mounts (`/workspace:rw` pointing to host paths) are **strictly eliminated**.
- Repositories required by a project are stored, fetched, and checked out **inside the project container sandbox** in persistent project storage.
- The control plane never bind-mounts host source trees, host git caches, or control-plane directories into the container.
- Persistent project storage is managed via dedicated Docker volumes:
  - `atm-project-data-<project_id>:/workspaces:rw`: Retains repository bare/base trees, task worktrees, and branch artifacts across task executions.
  - `atm-project-state-<project_id>:/var/lib/opencode:rw`: Retains necessary durable OpenCode state across container restarts.
- **Task Worktree Layout & Operational Separation:**
  - Each task receives its own Git worktree inside the persistent project volume (e.g., `/workspaces/<repo>/tasks/<task_id>`).
  - By default, a task worktree includes only the task's selected repository; multi-repository access for a task must be explicitly declared.
  - Worktrees provide operational separation and concurrency management for branches and working directories; they do **NOT** establish security boundaries. All tasks and repositories within the same project share the security trust boundary and can access sibling files and processes.
  - Task and worktree retention is strictly manual; there is no automatic deletion semantics and no task-delete API.
- **Project Container Idle Lifecycle & Cold Starts:**
  - After 24 consecutive hours without any running task or active OpenCode request, the control plane stops the project container only.
  - Persistent project volumes (`atm-project-data-<project_id>` and `atm-project-state-<project_id>`), worktrees, and durable OpenCode state are retained intact; bounded tmpfs scratch directories are discarded on container stop.
  - The project container is restarted automatically on the next task or request (accepting the cold-start restart latency tradeoff).
  - Idle stopping must be coordinated so it never races active tasks or in-flight OpenCode requests and never deletes project data.
- **Credential Separation & Active Blocker:**
  - Raw GitHub tokens remain solely in the trusted ATM control plane; they are never passed into the project container, OpenCode/task process, task-visible environment variables, files, arguments, prompts, or worktree git configuration.
  - The exact credential-safe mechanism for clone, fetch, push, and data transfer between the trusted control plane and the project volume without host bind mounts remains unresolved and is an active blocker (see Section 7.1).

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
- **Multi-Repository Resolution & Union Semantics:**
  - In projects declaring multiple repositories, ATM resolves a single project runtime profile by computing the union of requirements across all declared project repository manifests (`.atm-runtime.json`).
  - **Union Behavior for Disablement:** Repo-level `false` means that specific repository does not require that runtime. If another repository in the project requires a positive runtime version, the positive requirement is included in the project runtime profile (consistent with union semantics). An explicit project-level UI/DB override set to `false` disables the runtime project-wide regardless of repository manifests.
  - **Conflicting Requirements Fail Closed:** If different repository manifests declare conflicting exact versions for the same runtime (i.e., two different exact versions that are both valid catalog entries), resolution **fails closed immediately**, refusing container startup and task attempts unless an explicit project-level UI/DB override resolves the conflict. An uncataloged version is separately invalid and fails manifest validation on its own.
  - Project UI/DB overrides remain the highest priority in the hierarchy.
  - Any invalid manifest (syntax/schema error or uncataloged version) fails closed immediately; a UI override does not forgive or bypass an invalid manifest.

### 3.2 Project UI/DB Overrides & Precedence
- Structured metadata field in `models.Project` (`runtime_config`).
- Per runtime: `inherit` (unset override), exact catalog version, or `disabled` (`false`).
- **Hierarchy:** UI Override > Union of Repo Manifests (`.atm-runtime.json`) > Pinned Catalog Baseline.
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
│   (Binds Task Sessions to Task Worktree Paths)                     │  │
└────────────────────────────────────────────────────────────────────┼───┘
                                                                     │ HTTP (Container IP/Net)
                                                                     ▼
┌────────────────────────────────────────────────────────────────────────┐
│             Project Sandbox Container (atm-runtime:<sha256>)           │
│             Container Name: atm-project-<project_id>                   │
│                                                                        │
│   Dedicated User-Defined Bridge Network (Egress-Filtered NAT, No Ports)│
│   HostConfig: CapDrop=ALL, no-new-privileges=true, CPU/Mem/PID limits  │
│   Read-Only Root Filesystem, Bounded Writable tmpfs Scratch            │
│                                                                        │
│   ┌────────────────────────────────────────────────────────────────┐   │
│   │  Long-Lived OpenCode Server Process (1 Per Project Sandbox)   │   │
│   │  - Auth: OPENCODE_PASSWORD set per project                     │   │
│   │  - Config: Minimal vetted plugins, local tools only            │   │
│   │  - External MCP execution disabled; no host credentials/plugins│   │
│   │  - Model Provider Auth: Injected per attempt/project env       │   │
│   │  - Task sessions bound to dedicated worktree paths             │   │
│   └───────────────┬────────────────────────────────┬───────────────┘   │
│                   │ tool execution                 │ child subagents   │
│                   ▼                                ▼                   │
│         /workspaces (In-Container Worktrees) In-Container Shell / Tools│
│                                                                        │
│   Volume Mounts & Temp Scratch:                                        │
│   - atm-project-data-<project_id>:/workspaces:rw (Bare/Worktrees)      │
│   - atm-project-state-<project_id>:/var/lib/opencode:rw (Durable State)│
│   - tmpfs:/tmp:rw,noexec,nosuid,size=512m (Per-task dirs cleared on end)
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
   The container must be executed with `--security-opt no-new-privileges:true`. If the underlying container runtime rejects this option, the control plane **must fail closed immediately**. It is strictly forbidden to silently relax, create exceptions for, or bypass this control; weakening AppArmor, relaxing seccomp, or omitting NNP is completely prohibited. Initial deployment targets native non-Snap Linux Docker Engine (or a dedicated native Linux VM) qualified via exact probe.
2. **Docker Socket Isolation:**
   No container may ever have access to `/var/run/docker.sock` or any Docker daemon interface.
3. **Host Mount Elimination:**
   Zero host bind mounts. No host user configs (`~/.config/opencode`), no host cache directories, and no host source trees are mounted into containers.
4. **Least Privilege Execution:**
   - Dropped Linux capabilities: `CapDrop: ALL`.
   - Execution as non-root user (`USER opencode:opencode`, UID 1000) compatible with language toolchains.
5. **Read-Only Root Filesystem & Bounded Writable Scratch:**
   Container root filesystem is mounted read-only (`--read-only`). Writable storage is restricted to persistent project volumes (for repository worktrees and durable OpenCode state) and bounded `tmpfs` mounts (e.g. `/tmp:rw,noexec,nosuid,size=512m`). Per-task scratch directories in `tmpfs` are cleared upon task completion. Task and worktree retention in persistent volumes is strictly manual (no auto-deletion semantics or task-delete API).
6. **Resource Bounds:**
   Strict memory quotas, CPU limits, and PID limits enforced on the project container.
7. **Control-Plane Network Isolation & Egress Filtering:**
   Project containers run on isolated user-defined Docker bridge networks without published host ports. Control plane communicates via daemon-inspected internal container IPs. Project containers enforce a **default-deny outbound egress policy** with explicit destination allowlists:
   - Must explicitly block host and control-plane private IP ranges, Docker daemon bridges, and IPv4/IPv6 link-local and cloud provider metadata endpoints (specifically including `169.254.169.254` and `[fd00:ec2::254]`).
   - Outbound access is restricted strictly to permitted external destinations (such as verified model provider API endpoints and package registries). Exact endpoints and filter mechanisms (iptables/nftables/proxy) remain an active qualification item (see Section 7.1).
   - Arbitrary user-supplied URLs are strictly prohibited to prevent SSRF.
8. **Credential Defense-in-Depth:**
   Raw GitHub tokens remain solely in the trusted ATM control plane and are never exposed into the project container, task-visible files, environment variables, command arguments, or prompt contexts.

---

## 7. Unresolved Design Questions & Implementation Blockers

The following items are unresolved architectural decisions and active implementation blockers that must be formally resolved and qualified before proceeding to production implementation:

### 7.1 Unresolved Design Questions & Architecture Decisions

1. **Accepted Architecture Decisions (Pending Qualification & Implementation):**
   - *Process Topology:* A single long-lived OpenCode server process runs per project sandbox container. Task sessions are bound to their respective Git worktree paths.
   - *Workspace Layout:* Each task receives its own dedicated Git worktree inside persistent project storage (`atm-project-data-<project_id>:/workspaces`). Tasks default to including only their selected repository; multi-repository task access must be explicitly configured. Worktrees are an operational separation only and share the project security trust boundary.
   - *Multi-Repository Runtime Resolution:* Resolved via unioning requirements across all declared project repository manifests (`.atm-runtime.json`). Conflicting exact versions for the same runtime fail closed unless resolved by an explicit project-level override. Project UI/DB overrides remain highest priority, and invalid manifests fail closed. Repo-level `false` means that repo does not require the runtime; positive requirements from other repos win in union; an explicit project-level `false` override disables the runtime project-wide.
   - *Container Storage & Idle Lifecycle:* Read-only container root; persistent project Docker volumes for repository trees and durable OpenCode state; bounded tmpfs scratch with per-task directories cleared upon task completion. Task and worktree retention is manual (no auto-deletion semantics or task-delete API). Containers stop after 24 hours of inactivity (no running tasks or active OpenCode requests), preserving persistent volumes and discarding tmpfs, with automatic restart on the next task/request.
   - *Egress Architecture:* Outbound container egress follows a default-deny policy with explicit allowlists, blocking host/control-plane private networks, daemon bridges, IPv4/IPv6 link-local addresses, and cloud provider metadata (`169.254.169.254`).
   - *Deployment Target & Privilege Mandate:* The initial production target is a native non-Snap Linux Docker Engine (or dedicated native Linux VM). Mandatory `no-new-privileges:true` is non-negotiable; AppArmor/seccomp softening and NNP omission are strictly banned.

2. **Active Unresolved Architecture Questions (Blockers):**
   - **Credential-Safe Clone / Fetch / Push & Data Transfer Mechanism:**
     Raw GitHub tokens remain solely in the trusted ATM control plane and are never passed into the project container, OpenCode/task process, environment variables, files, arguments, prompts, or worktree config. Repository contents and worktrees must reside in the project volume without host bind mounts. The exact credential-safe mechanism for initial cloning, fetching, pushing, and data transfer between the trusted control plane and the project volume remains an **active unresolved blocker**. No proxy, helper, or sidecar is yet qualified or assumed to be inherently secure.
   - **Egress Filter Implementation & Endpoint Qualification:**
     While the default-deny policy and cloud metadata blocking requirements are accepted, the exact filter mechanism (iptables/nftables vs. egress proxy) and the specific allowlisted destinations (exact LLM API endpoints and package registry domains) remain to be qualified and documented.
   - **Runtime Base OS and Image Composition (Alpine/musl vs. Debian/glibc multi-stage):**
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
2. **Native Non-Snap Docker Host Qualification & Mandatory `no-new-privileges` Probe:**
   On the target Linux host running the default container daemon (Docker Engine 29.8.0 installed via Snap revision 3613, containerd 2.3.4, runc 1.5.1), running containers with mandatory `--security-opt no-new-privileges:true` fails during execution.
   - **Target Qualification Requirement:** Initial deployment targets a native non-Snap Linux Docker Engine (or a dedicated native Linux VM). Before implementation can begin, the native non-Snap target host must pass the exact qualification probe (`NoNewPrivs: 1`, exit status 0).
   - **Reproducible A/B Probe Facts (Snap Docker Failure):**
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
   - **Alternate Daemon Comparison (Docker Desktop Linux VM — Informational Only):**
     - Docker Desktop daemon (Docker Engine 29.2.1, runc 1.3.4) was started temporarily for comparative testing. Only the pre-existing Alpine test image was copied from the default daemon into Desktop without pulling from any external registry.
     - Running the exact same restricted probe with NNP enabled succeeded:
       ```text
       docker --context desktop-linux run --rm --pull=never --network none --read-only --cap-drop=ALL --security-opt no-new-privileges:true --user 65534:65534 --pids-limit 16 --memory 64m --cpus 0.5 --entrypoint /bin/sh alpine:latest -c 'grep "^NoNewPrivs:" /proc/self/status; grep -q "^NoNewPrivs:[[:space:]]*1$" /proc/self/status; exec /bin/true'
       NoNewPrivs: 1 (exit status 0)
       ```
     - Four pre-existing Desktop containers showed `Exited (255)` upon Desktop startup and were completely untouched; none were running. Following the probe, Docker Desktop was stopped and the Docker context was restored to `default`.
     - *Scope & Qualification Limitations:* Docker Desktop executes containers inside a dedicated Linux virtual machine, representing an alternate guest kernel and runtime stack. This pass serves solely as an informational VM comparison and development test candidate; it does **not** qualify a native Docker Engine host deployment or ATM's containerized OpenCode runtime.
   - **Root Cause & Workaround Prohibitions:**
     - The captured evidence localizes the execution failure specifically to the `no-new-privileges` interaction on this Snap-backed daemon installation. However, Snap confinement or AppArmor profile conflicts remain an unproven hypothesis, not an established root cause.
     - Workarounds that disable AppArmor, relax Seccomp profiles, or silently omit/bypass `no-new-privileges` are **strictly prohibited**.
     - In accordance with ATM's security specification, `no-new-privileges:true` is **strictly mandatory and fail-closed**. The Docker Desktop pass does not clear overall sandbox qualification, and implementation remains fully blocked until this runtime incompatibility is resolved on supported native hosts alongside other unapproved architectural decisions.
   - **Untested Surface:**
     - Prior probes evaluated only the Linux kernel execution barrier. They did **not** involve provider credentials, host config/data mounts, repository code, workspace volume mounts, OpenCode server runtime processes, authentication handshakes, or network egress filtering.
3. **Gate D Provider Credential and Egress Validation Blocked:**
   Validating and testing model provider credential injection and network egress allowlisting for containerized OpenCode servers remains blocked until the candidate OpenCode container artifact is qualified, dependency/ABI composition is resolved, the safe credential transfer mechanism is defined, and the container is runnable on a qualified native host with mandatory security options.

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
