# Design Specification: Self-Hosting Readiness & Production Operating Model

**Status:** APPROVED FOR PLANNING (Implementation remains blocked pending required gates)
**Author:** Documentation Implementation Specialist
**Date:** 2026-10-11
**Target Platform:** Single-Host Linux (`linux/amd64`), Docker Compose, Full Task Execution
**Specification File:** `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md`

---

## 1. Executive Summary & Objective

This specification defines the readiness baseline, trust model, operational requirements, and release-blocking criteria for self-hosting **Agent Task Manager (ATM)**.

The user-approved target topology is a **supported single-host Linux (`linux/amd64`) installation using Docker Compose with full, safe task execution**. Full task execution requires running autonomous AI coding tasks to completion inside containerized sandboxes, without weakening host security or leaking credentials.

ATM is **not currently self-hosting ready** for untrusted workloads or general production environments. The existing codebase and deployment assets contain development shortcuts, hardcoded test secrets, host-execution fallbacks, insecure TLS bypass defaults, and unresolved sandbox qualification gates.

This document bridges the gap between current development convenience and a supportable production operating model by establishing:
1. Strict platform and operational support boundaries.
2. A formal threat and trust model, explicitly documenting the shared project boundary and Docker daemon privilege boundary.
3. An audit of current baseline discrepancies against the target.
4. Non-negotiable product, deployment, secret, backup, and security requirements.
5. Release-blocking qualification gates (including reconciling local test evidence against tracked documentation).
6. Clean-host acceptance criteria and unresolved architectural decisions.

---

## 2. Target Scope, Platform Support & Non-Goals

### 2.1 Supported Deployment Target (Single-Host Linux)
- **Host Architecture:** `linux/amd64` exclusively.
- **Host Operating System:** Native Linux distributions with standard upstream packages (e.g., Ubuntu 24.04 LTS, Debian 12) running a standard Linux kernel with standard cgroups v2, seccomp, AppArmor, and user namespaces support.
- **Container Engine:** Native upstream Docker Engine (Docker CE 26+ / 27+ / 29+) with Docker Compose v2.20+.
  - *Explicit Requirement:* Must run under standard systemd service management. Snap-confined Docker (`docker` installed via Snap) is **unsupported** due to verified AppArmor/containment failures when executing with `no-new-privileges:true`.
- **Workload Confinement:** Containerized project sandboxes with zero host bind mounts for repository code, task worktrees, or credentials.

### 2.2 Explicit Exclusions (Non-Goals for Target v1)
- **Multi-Node & Kubernetes:** Clustering, Kubernetes Helm charts, Nomad, and distributed worker nodes are out of scope for this release.
- **ARM64 / macOS / Windows Hosts:** `linux/arm64`, macOS (Docker Desktop), and Windows (WSL2 / Docker Desktop) are excluded from the supported production tier.
- **Arbitrary Container Runtimes:** Podman, rootless Docker, containerd-direct, and microVM managers (Firecracker, Kata) are out of scope.
- **Intra-Project Task Multi-Tenancy:** Task-to-task isolation within the same project is explicitly not supported (see Section 3.2).
- **Public-Internet Multi-Tenant SaaS:** Multi-tenant shared instances hosting mutually hostile untrusted organizations are strictly out of scope. ATM is designed for single-organization or trusted-team internal deployments.

---

## 3. Trust Model, Threat Boundary & Isolation Architecture

### 3.1 Overall Architectural Diagram & Deployment Boundary

```
                     ┌────────────────────────────────────────────────────────┐
                     │                 External Traffic Ingress               │
                     │          (Browser, Webhook, CLI over HTTPS)            │
                     └───────────────────────────┬────────────────────────────┘
                                                 │ Port 443 (TLS Terminated)
                                                 ▼
┌─ Docker Engine Host ────────────────────────────────────────────────────────────────────────┐
│                                                                                            │
│   ┌────────────────────────────────────────────────────────────────────────────────────┐   │
│   │                         Traefik Reverse Proxy (Ingress)                            │   │
│   │         - Automated Let's Encrypt or Custom CA TLS Termination                     │   │
│   │         - Strict Security Headers (CSP, HSTS, X-Frame-Options)                     │   │
│   │         - Routes /api, /ws -> Control Plane; /dex -> Dex OIDC; / -> Web UI         │   │
│   │         - NO Docker socket mount (uses file provider or read-only socket proxy)    │   │
│   └───────────────┬────────────────────────────┬───────────────────────────┬───────────┘   │
│                   │                            │                           │               │
│                   ▼                            ▼                           ▼               │
│   ┌───────────────────────────────┐ ┌─────────────────────────┐ ┌──────────────────────┐   │
│   │          Web UI               │ │        Dex OIDC         │ │    PostgreSQL 18     │   │
│   │  - Static Nginx Delivery      │ │  - Identity Provider    │ │  - App State DB      │   │
│   │  - React 19 Client SPA        │ │  - Upstream IdP Relay   │ │  - AES-256-GCM Vault │   │
│   └───────────────────────────────┘ └─────────────────────────┘ └──────────────────────┘   │
│                                                                                            │
│   ┌────────────────────────────────────────────────────────────────────────────────────┐   │
│   │                       ATM Control Plane & Worker Process                           │   │
│   │  - Fiber v3 HTTP / WebSocket API Server & Asynq Worker Scheduler                   │   │
│   │  - Manages task queue, lifecycle, database models, and Git operations              │   │
│   │  - Privileged Docker Engine client (launches/terminates project sandboxes)         │   │
│   │  - SECRETS VAULT: Retains GitHub PATs, OAuth tokens, and Model API keys            │   │
│   └──────────────────────────────────────────┬─────────────────────────────────────────┘   │
│                                              │                                             │
│                      Docker Engine Socket    │ Privileged API Controls                     │
│                      (/var/run/docker.sock)  ▼ (Create container, attach networks, volumes)│
│                                                                                            │
│   ======================= HARD SYSTEM SECURITY BOUNDARY ================================   │
│                                                                                            │
│   ┌─ Isolated Project Sandbox: atm-project-<project_id> ───────────────────────────────┐   │
│   │                                                                                    │   │
│   │   - Security Options: no-new-privileges:true, cap-drop=ALL, read-only rootfs       │   │
│   │   - Dedicated Docker Volume: atm-project-data-<id> (Worktrees, Git bare repos)     │   │
│   │   - Dedicated Docker Volume: atm-project-state-<id> (OpenCode session cache)       │   │
│   │   - Bounded tmpfs: /tmp (noexec, nosuid, nodev, size=512m)                         │   │
│   │   - Network Confinement: Default-deny outbound egress via restricted bridge/NFT    │   │
│   │                                                                                    │   │
│   │   ┌────────────────────────────────────────────────────────────────────────────┐   │
│   │   │                    Sandboxed OpenCode Server Process                       │   │
│   │   │  - Listens on internal project loopback / bridge network                   │   │
│   │   │  - Authenticated via per-project dynamic credentials (never default password)│   │
│   │   │  - Runs agent prompts, code generators, and terminal tools                │   │
│   │   └─────────────────────────────────────┬──────────────────────────────────────┘   │
│   │                                         │ Spawns task commands                     │
│   │   ┌─────────────────────────────────────▼──────────────────────────────────────┐   │
│   │   │                 Shared Project Workspace (/workspaces)                     │   │
│   │   │  - Repo A Worktree (Task 1)     - Repo A Worktree (Task 2)                 │   │
│   │   │  - Repo B Worktree (Task 3)     - Local Project Compilers & Tools          │   │
│   │   │  *** MUTUAL TRUST BOUNDARY: Tasks inside same project share this volume ***│   │
│   │   └────────────────────────────────────────────────────────────────────────────┘   │
│   │                                                                                    │   │
│   └────────────────────────────────────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 3.2 Trust Boundaries and Isolation Semantics
1. **Host vs. Sandbox Boundary (Hard Boundary):**
   - Untrusted code generated by AI models, third-party dependency installation scripts (`npm install`, `go build`, `pip install`), and OpenCode subagent processes must **never** run directly on the host machine.
   - Project containers are strictly confined: root filesystem is read-only, all Linux capabilities are dropped (`--cap-drop=ALL`), privilege escalation is blocked (`--security-opt no-new-privileges:true`), and PIDs/memory/CPU are bounded.
   - Zero host filesystem directories may be bind-mounted into the sandbox.
2. **Project-to-Project Boundary (Hard Boundary):**
   - Different projects run in independent Docker containers (`atm-project-<project_id>`).
   - Project containers share no volumes, no network namespaces, no temporary scratch spaces, and no IPC mechanisms.
3. **Intra-Project Task & Repository Boundary (Shared Trust Boundary):**
   - **Explicit Non-Guarantee:** All repositories, tasks, and OpenCode sessions belonging to the **same project share a single security boundary**.
   - Tasks in the same project execute inside the same container and mount the same project storage volume (`atm-project-data-<project_id>`). Worktree subdirectories (`/workspaces/<repo>/tasks/<task_id>`) provide directory and branch segregation for concurrent development, **not** security confinement.
   - An agent working on Task A within Project 1 has read/write access to files modified by Task B within Project 1. Operators must not assign mutually hostile tasks or tenants to the same ATM project.
4. **Control Plane & Docker Daemon Privilege Boundary:**
   - The ATM control plane interacts directly with the Docker daemon API to orchestrate containers.
   - **Docker Socket Root Equivalence:** Access to `/var/run/docker.sock` provides root-equivalent authority over the host. The control plane is in the high-trust management plane. Compromise of the control plane container compromises the host.
   - The Traefik reverse proxy must **not** mount the Docker socket in production. Traefik must either use static/file providers or an isolated, read-only Docker socket filter (e.g., `docker-socket-proxy` restricted solely to container list/inspect events with zero access to exec, create, or volume endpoints).
   - In production, the control plane container must be secured as an administrative appliance, with strict network isolation preventing sandbox containers from reaching its management endpoints.

### 3.3 Network Threat Model & Egress Controls
1. **Control Plane & Infrastructure Networks:**
   - Control plane, PostgreSQL, Redis, and Dex communicate over an internal Docker bridge network (`agent_backend`) with `internal: true`. No external ingress is allowed directly to database or cache ports.
   - Ingress is mediated exclusively by Traefik over `agent_ingress`.
2. **Sandbox Network Egress (Default Deny):**
   - Sandboxes must not possess unconstrained outbound internet access.
   - **Prohibited Destinations:**
     - Host loopback (`127.0.0.1/8`) and host bridge IP addresses.
     - Internal control plane services (`postgres:5432`, `redis:6379`, `dex:5556`, `control-plane:8080`).
     - RFC 1918 private IPv4 subnets (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`) and ULA IPv6 subnets (`fc00::/7`).
     - Link-local and cloud provider metadata services (`169.254.169.254`, `fe80::/10`).
   - **Allowlisted Destinations (Egress Filtered):**
     - Allowed upstream package registries (e.g., `registry.npmjs.org`, `proxy.golang.org`, `pypi.org`, `crates.io`).
     - Direct sandbox egress to public LLM provider endpoints is **contingent on resolving Gate 0G / Decision 9.4**: if model provider requests are not reverse-proxied and authenticated by the control plane, direct sandbox egress requires allowlisting provider endpoints while maintaining the non-negotiable requirement that provider credentials remain inaccessible to untrusted task child processes. The network egress posture and the credential delivery mechanism must be resolved in tandem; unconstrained direct egress is prohibited.

---

## 4. Current Readiness Baseline vs. Target Operating Model

An audit of the current repository reveals critical architectural and operational discrepancies between the existing implementation and the target operating model:

| Area | Current Implementation State | Target Self-Hosting Operating Model | Status / Delta |
|---|---|---|---|
| **Task Execution Confinement** | `internal/runner/task_runner.go:548,866,914` falls back to `runOnHost` when container startup fails. `internal/runner/dependencies.go:50,98` executes host `npm` and `go` commands. | Hard fail-closed containerization. Host fallback and host package managers are strictly deleted. If container startup fails, task fails. | **Violates Target** (Host leakage) |
| **Filesystem Bind Mounts** | `docker-compose.yml:134-139` mounts host directories (`/home/ghug/...`, `${HOST_WORKTREE_ROOT}`) and writable `/var/run/docker.sock` into control plane. `opencode-bridge` uses `network_mode: host`. | In-container project volumes (`atm-project-data-*`). Zero host worktree bind mounts. Socat bridge container eliminated. | **Violates Target** (Host mounts & path leakage) |
| **Git Transport & TLS** | `internal/git/sync.go:43,69,87` and `push.go:31,44` retry with `-c http.sslVerify=false`. `docker-compose.yml:132` sets `GIT_SSL_NO_VERIFY=true`. | Strict TLS certificate validation on all Git operations. Silent fallback to unverified TLS is completely prohibited. | **Violates Target** (Insecure TLS bypass) |
| **Git Credential Transfer** | Tokens passed in CLI flags and HTTP extra headers (`internal/git/sync.go`). Control plane handles local disk worktrees. | Credential-safe Git data transfer into project volume; raw tokens never enter sandbox arguments, environment, files, or logs. | **Unresolved Architecture** (Gate 0C blocker) |
| **Model Provider Credentials & LLM Access** | Accepted runtime spec proposes per-attempt/project env injection, but does not specify process visibility controls. | Model provider credentials (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) must never be accessible to untrusted task child processes or task inspection tools. | **Unresolved Architecture** (Gate 0G blocker) |
| **OpenCode Credentials & Server** | `docker-compose.yml:131` hardcodes static `OPENCODE_PASSWORD`. Bridge connects to developer host port `49374`. | Per-project containerized OpenCode server. Dynamic credentials generated at runtime, stored in memory/tmpfs, never logged to stdout/docker logs. | **Violates Target** (Static secrets / host port) |
| **Default Credentials & Logins** | `docker-compose.yml` defaults to `postgres`, `secretredis`, fixed encryption key `0123...01`, and hardcoded OIDC secrets. `README.md` documents `password123`. | Zero default secrets. Compose setup refuses to start if placeholder passwords or keys are detected. Sealed production Dex configuration. | **Violates Target** (Insecure defaults) |
| **Docker Socket Ingress Mount** | `docker-compose.yml:34` mounts `/var/run/docker.sock:ro` into Traefik. `:ro` socket mount still permits full Docker API reads (inspect secrets, read envs). | Traefik decoupled from Docker socket using file-based routing or a hardened filtering socket proxy restricted to container labels. | **Violates Target** (Privilege exposure) |
| **Legal & Licensing** | `README.md:8` displays an MIT badge linking to `LICENSE`, but no tracked `LICENSE` file exists in the repository. | Valid tracked `LICENSE` file committed to repository matching declared terms. | **Violates Target** (Licensing ambiguity) |
| **Host Docker Qualification** | Snap-confined Docker Engine fails `no-new-privileges:true` probes (`exit 255`). Tracked specs mark Gate 0A/0B as open. | Formal documentation of supported native Docker Engine prerequisites. Reconciliation of 0A/0B qualification evidence. | **Blocked** (Host runtime constraint) |

---

## 5. Non-Negotiable Product and Security Requirements

To achieve self-hosting readiness, the deployment and runtime design must satisfy these non-negotiable requirements:

### 5.1 Confinement & Execution Invariants
1. **Zero Host Execution:** The control plane must never execute `exec.Command` for compilers, package managers (`npm`, `go`, `pip`), or task scripts on the host operating system. The `runOnHost` fallback in `internal/runner/task_runner.go` must be excised entirely.
2. **Mandatory Linux Security Options:** All task execution containers must launch with:
   - `--security-opt no-new-privileges:true` (Mandatory; prevents privilege escalation via setuid/setgid).
   - `--cap-drop=ALL` (Drops all kernel capabilities; no `NET_ADMIN`, `SYS_ADMIN`, `DAC_OVERRIDE`).
   - `--read-only` (Root filesystem is immutable; all writes redirected to explicit, bounded volumes or tmpfs).
   - Bounded container resource limits (`--pids-limit`, `--memory`, `--cpus`).
3. **No Dynamic Security Degradation:** Under no circumstances may ATM catch a runtime error and retry without `no-new-privileges`, without seccomp, with unconfined AppArmor, or with disabled SSL verification.

### 5.2 Network & Communication Invariants
1. **Strict Transport Security (TLS):**
   - All Git operations (`clone`, `fetch`, `push`) must validate certificates against system trust stores. `http.sslVerify=false` and `GIT_SSL_NO_VERIFY` are banned in production code and configuration.
   - Traefik ingress must serve traffic over HTTPS (port 443) using automated ACME certificates or operator-supplied certificates. Unencrypted HTTP (port 80) must redirect permanently to HTTPS.
2. **Credential Sanitization:**
   - Provider API keys (Anthropic, OpenAI, Google) and Git personal access tokens must never appear in command-line arguments (`ps` visible), shell history, container environment variables visible to user processes or task scripts, task logs, or ANSI terminal streams.
   - *Incompatibility & Architecture Blocker:* The accepted runtime design (`2026-10-10-runtime-image-configuration-design.md:152,195`) proposes injecting provider auth into the per-attempt/project environment for OpenCode, but leaves unresolved whether task child processes and build scripts inherit or can inspect those variables. Allowing untrusted task code to read raw provider API keys violates this non-negotiable security invariant. Resolving this architecture gap (via control-plane proxy mediation, scoped process-environment isolation, or a secure credential provider) is an explicit release-blocking requirement (Gate 0G) before self-hosting readiness can be certified.

### 5.3 Administrative & Production Invariants
1. **Zero Default Passwords:** The production Compose stack must refuse to start (`exit 1`) if `SECRET_ENCRYPTION_KEY`, `SESSION_SECRET`, `POSTGRES_PASSWORD`, `REDIS_PASSWORD`, or OIDC secrets match known default/example strings or are under minimum entropy thresholds.
2. **Deterministic Licensing:** A formal, tracked `LICENSE` file matching the project's intended open-source or proprietary terms must exist at repository root before making self-hostable claims.

---

## 6. Deployment, Secrets, Backup, and Operator Requirements

### 6.1 Clean-Host Deployment Architecture
A supported self-hosted instance must deploy cleanly on a fresh Linux server using a standardized directory layout:

```text
/opt/agent-task-manager/
├── docker-compose.yml          # Production Compose definition (no host bind mounts or dev credentials)
├── .env                        # Operator environment variables (mode 0600, root:root)
├── deploy/
│   ├── traefik/
│   │   ├── traefik.yml         # Static configuration
│   │   └── dynamic.yml         # Dynamic TLS and router definitions
│   └── dex/
│       └── config.yml          # Production OIDC configuration
└── data/                       # Docker volume storage root (or managed via named volumes)
```

### 6.2 Secret Generation & Management
1. **Mandatory Secret Bootstrap:**
   - Operators must run an automated initialization command or script to generate cryptographically secure secrets prior to `docker compose up`:
     - `SECRET_ENCRYPTION_KEY`: Currently enforced by `cmd/server/main.go:114-118` and `internal/config/config.go:68` as a raw string converted directly to bytes (`masterKey := []byte(cfg.SecretEncryptionKey)`), requiring exactly `len == 32` bytes/characters (not decoded from base64 or hex). Supplying a standard base64 (44 ASCII chars) or hex (64 ASCII chars) string currently causes the server to exit immediately (`log.Fatalf("SECRET_ENCRYPTION_KEY must be exactly 32 bytes...")`). Supporting safely printable encoded 256-bit values (such as hex or base64) requires an implementation change to decode the value in `internal/config` before cipher initialization and remains a target requirement/open configuration contract for self-hosting.
     - `SESSION_SECRET`: At least 32 cryptographically random bytes for cookie signing.
     - `POSTGRES_PASSWORD` and `REDIS_PASSWORD`: High-entropy random strings.
2. **Secret Rotation Model:**
   - **Session Secret Rotation:** Invalidates active web user sessions; users re-authenticate via Dex.
   - **Database & Redis Password Rotation:** Requires synchronized `.env` update and container restart.
   - **Secret Encryption Key Rotation (Key Re-encryption):** ATM must document or provide a CLI re-encryption tool to decrypt existing database-stored credentials with the old key and re-encrypt them with the new key before cutting over.

### 6.3 Backup, Restore & Disaster Recovery
A valid self-hosted deployment requires a verified backup and restore procedure:
1. **Persistent State Inventory:**
   - **Relational Data:** PostgreSQL database (`postgres_data` volume) contains projects, task metadata, logs, RBAC assignments, and encrypted provider credentials.
   - **Queue State:** Redis (`redis_data` volume) contains active task scheduling state. Can be rebuilt or drained.
   - **Project Git State:** Project data volumes (`atm-project-data-<id>`) contain bare repository clones, worktrees, and working tree diffs.
   - **Configuration:** `.env`, custom TLS certificates, and Dex user store configurations.
2. **Backup Procedures:**
   - Consistent database snapshot via `pg_dump` executed inside the Postgres container:
     `docker compose exec -T postgres pg_dump -U postgres agent_tasks > backup_$(date +%F).sql`
   - Filesystem archive of project volumes while containers are quiescent.
3. **Restore Verification:**
   - Restoration must be tested on a clean host by restoring the SQL dump into a clean Postgres container and re-mounting the project volumes with identical `SECRET_ENCRYPTION_KEY`.

### 6.4 Update & Maintenance Lifecycle
1. **Container Upgrades:** Upgrades must follow explicit container image pull and recreation:
   `docker compose pull && docker compose up -d`
2. **Database Migrations:** GORM/database schema migrations must execute automatically and idempotently during control-plane container startup before traffic ingress begins.
3. **Rollback Strategy:** If an upgrade fails, operators revert the `docker-compose.yml` image tags and restore the database backup taken immediately prior to upgrade.

---

## 7. Release-Blocking Qualification Gates & Evidence Reconciliation

Release sign-off for self-hosting is strictly blocked by the following qualification gates.

### 7.1 Status Reconciliation: Tracked Documentation vs. Local Qualification Artifacts
There is a documented discrepancy between the tracked repository specification and local, ignored qualification ledgers:
- **Tracked Documentation (`docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md`):** States that Gate 0A (native Linux NNP qualification) and Gate 0B (OpenCode container qualification) are **open and blocked**.
- **Local Ignored Ledger (`.superpowers/sdd/2026-10-10-runtime-image-configuration/progress.md`):** Records that Gate 0A and Gate 0B **passed** under isolated probes in a dedicated Ubuntu 24.04 QEMU/KVM guest using Docker Engine 29.8.
- **Reconciliation Requirement:** Local, uncommitted ledger files do not constitute release sign-off. Before implementation is accepted into main, the tracked specifications and plans must be updated to reconcile this status, referencing durable, verifiable test reports while preserving the exact technical boundaries of what was proven.

### 7.2 Gate Inventory & Exit Criteria

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                        Self-Hosting Release Qualification Gates                        │
├─────────┬───────────────────────────┬──────────────┬───────────────────────────────────┤
│ Gate    │ Description               │ Status       │ Required Sign-off Evidence        │
├─────────┼───────────────────────────┼──────────────┼───────────────────────────────────┤
│ Gate 0A │ Native Linux Host NNP     │ Qualified in │ Verify NoNewPrivs: 1 (exit 0) on  │
│         │ Qualification             │ QEMU VM      │ supported native Linux Docker CE. │
│         │                           │ (Doc Stale)  │ Formally update tracked spec.     │
├─────────┼───────────────────────────┼──────────────┼───────────────────────────────────┤
│ Gate 0B │ Sandboxed OpenCode        │ Qualified in │ Official digest verified. Probe   │
│         │ Artifact Qualification    │ QEMU VM      │ passes Basic auth on loopback.    │
│         │                           │ (Doc Stale)  │ Production password flow defined. │
├─────────┼───────────────────────────┼──────────────┼───────────────────────────────────┤
│ Gate 0C │ Credential-Safe Git Data  │ BLOCKED      │ Git clone/fetch/push works into   │
│         │ Transfer & Volume Layout  │              │ volume with ZERO tokens inside    │
│         │                           │              │ sandbox env, args, files, or logs.│
├─────────┼───────────────────────────┼──────────────┼───────────────────────────────────┤
│ Gate 0D │ Default-Deny Sandbox      │ BLOCKED      │ Sandbox outbound network blocks   │
│         │ Egress Enforcement        │              │ host/LAN/cloud metadata; allows   │
│         │                           │              │ only strict approved destinations.│
├─────────┼───────────────────────────┼──────────────┼───────────────────────────────────┤
│ Gate 0E │ Zero Host Execution &     │ BLOCKED      │ All runOnHost and host package    │
│         │ Codebase Hardening        │              │ execution excised. SSL verify     │
│         │                           │              │ fallback eliminated from git pkg. │
├─────────┼───────────────────────────┼──────────────┼───────────────────────────────────┤
│ Gate 0F │ Production Compose &      │ BLOCKED      │ Refuse start on default secrets.  │
│         │ Secret Bootstrap          │              │ Tracked LICENSE committed. Traefik│
│         │                           │              │ Docker socket decoupled.          │
├─────────┼───────────────────────────┼──────────────┼───────────────────────────────────┤
│ Gate 0G │ Model Provider Credential │ BLOCKED      │ Model provider keys inaccessible  │
│         │ & API Mediation           │              │ to untrusted task child processes;│
│         │                           │              │ egress & auth reconciled with spec│
└─────────┴───────────────────────────┴──────────────┴───────────────────────────────────┘
```

#### Detailed Gate Criteria:
- **Gate 0A (Native Linux NNP Probe):**
  - *Proven Scope:* Confirmed that native Docker Engine running on a clean Linux kernel properly enforces `--security-opt no-new-privileges:true` alongside `--cap-drop=ALL` and `--read-only`.
  - *Remaining Action:* Reconcile tracked documentation. Prohibit Snap Docker deployments in operator documentation.
- **Gate 0B (OpenCode v2.0.26 Container Digest & Startup):**
  - *Proven Scope:* Evaluated candidate image digest `ghcr.io/anomalyco/opencode:2.0.26` (`sha256:ea6c5d68d8d230923d2627bc793e7fa6a03f8e47c253b50397eb92bbde6db255`). Verified musl/ABI compatibility and authenticated loopback probe (`GET /api/info` returned 200).
  - *Remaining Action:* Resolve publisher signature/provenance verification; implement a production-safe dynamic password lifecycle that does not leak credentials into stdout or container log drivers.
- **Gate 0C (Credential-Safe Git Transfer - Active Blocker):**
  - *Criteria:* Design and prove a data transfer mechanism where the control plane synchronizes remote repositories into the project volume (`atm-project-data-<id>`) without bind-mounting host paths and without passing raw GitHub PATs into the sandbox container.
- **Gate 0D (Default-Deny Egress Filter - Active Blocker):**
  - *Criteria:* Prove that container egress filtering (via Docker custom networks, iptables, or nftables) reliably drops packets to host IP addresses, link-local addresses (`169.254.169.254`), and local RFC 1918 subnets while permitting required package registries.
- **Gate 0E (Codebase De-coupling):**
  - *Criteria:* Delete `runOnHost` from `task_runner.go`. Delete host package manager calls from `dependencies.go`. Remove all `-c http.sslVerify=false` options from `internal/git/`.
- **Gate 0F (Deployment & Licensing Hardening):**
  - *Criteria:* Commit a valid repository `LICENSE` file. Update `docker-compose.yml` to remove hardcoded default credentials, host user paths, and Traefik Docker socket mounts. Implement startup validation refusing execution if default secrets are detected.
- **Gate 0G (Model Provider Credential & API Mediation - Active Blocker):**
  - *Criteria:* Formally resolve the mechanism for OpenCode LLM API communication and credential protection. The implementation must guarantee that model provider credentials (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, etc.) cannot be accessed or extracted by arbitrary task code, build scripts, or inspection of child process environments, while reconciling whether network traffic is reverse-proxied through the control plane or routed via egress-filtered allowlists. This qualification gate must be formally signed off and reconciled with the runtime architecture before production release.

---

## 8. Clean-Host Acceptance Criteria

Before declaring self-hosting ready for production use, a clean, unprovisioned Linux host must pass the following end-to-end acceptance battery:

### 8.1 Setup & Secret Bootstrap Acceptance
1. Start on a clean Ubuntu 24.04 LTS host with only Docker Engine (native CE) and Docker Compose installed.
2. Clone repository, execute automated secret bootstrap generator. Confirm `.env` is populated with random 32-byte keys.
3. Attempt startup with default credentials (`docker compose up`). Verification requirement: **Stack must fail immediately with an explicit configuration error.**
4. Launch stack with generated production secrets (`docker compose up -d`). Verification requirement: All core containers (`traefik`, `postgres`, `redis`, `dex`, `control-plane`, `web-ui`) become healthy within 60 seconds.

### 8.2 Positive End-to-End Task Execution
1. Log into Web UI using administrative account provisioned during setup.
2. Configure a project pointing to a test repository.
3. Dispatch a real, multi-turn coding task requiring dependency installation (e.g., adding an endpoint in Go or a component in Node.js).
4. Verification requirements:
   - Task executes completely inside container `atm-project-<project_id>`.
   - Worktree and repository exist entirely within `atm-project-data-<project_id>`.
   - Host filesystem shows zero new files or worktrees in `/srv` or host home directories.
   - Task generates code, runs tests, commits, and reports success.
   - ANSI terminal streams live over WebSocket without exposing secrets.

### 8.3 Negative & Fail-Closed Security Acceptance
1. **Host Fallback Negative Test:**
   - Simulate a Docker daemon failure or invalid image tag for a task.
   - Verification requirement: **Task fails closed immediately with state `failed`.** The control plane must NOT attempt to execute the task on the host. Host `ps aux` must show zero compiler or task worker processes.
2. **NNP Probe Verification:**
   - Execute an inspection probe against the running project sandbox container:
     `docker exec atm-project-<id> grep "^NoNewPrivs:" /proc/self/status`
   - Verification requirement: Must output `NoNewPrivs: 1`.
3. **Egress Confinement Negative Test:**
   - From inside the task execution sandbox, attempt network requests to prohibited destinations:
     - `curl -m 2 http://169.254.169.254/latest/meta-data/`
     - `curl -m 2 http://172.17.0.1:8080/api/v1/health`
     - `curl -m 2 http://<host_internal_ip>:22`
   - Verification requirement: **All requests must time out or fail closed with network unreachable.**
4. **TLS Insecurity Negative Test:**
   - Configure a Git remote with a self-signed or invalid TLS certificate without installing the CA cert.
   - Verification requirement: **Git clone/fetch must fail.** Control plane must not retry with `http.sslVerify=false`.
5. **Secret Absence Negative Test:**
   - Inspect container environment variables and process list inside sandbox:
     `docker exec atm-project-<id> env` and `docker exec atm-project-<id> ps aux`
   - Verification requirement: Neither GitHub personal access tokens nor database passwords appear in the environment or process command lines. Furthermore, model provider API keys (`ANTHROPIC_API_KEY`, etc.) must not be visible to untrusted task child processes or build environments; enforcement of this test is contingent on the formal resolution and implementation of Gate 0G.

---

## 9. Unresolved Decisions & Open Architecture Questions

The following decisions remain unresolved and must be decided prior to drafting an implementation plan:

1. **Git Data Synchronization Mechanism (Gate 0C Resolution):**
   - *Option A (Trusted Control Plane & Credential-Free Volume Transfer Helper - PROPOSED):* The control plane performs authenticated Git operations against GitHub via an isolated, short-lived unprivileged Git worker holding repository-scoped credentials in memory/protected IPC only. Repository data is streamed via credential-free `git bundle` streams into a non-networked, capability-dropped transfer helper attached to the project volume (`atm-project-data-<project_id>`). Sandboxed tasks never receive Git credentials or tokens in volume files, `.git/config`, environment variables, or arguments. Git LFS and submodules are explicitly rejected and disabled in v1; their presence causes fail-closed termination.
   - *Option B (In-Sandbox Git Credential Helper Unix Socket Proxy - REJECTED):* Sandbox runs standard Git CLI, but Git credential helper communicates over an internal Unix socket to a restricted proxy in the control plane that signs/authenticates requests without exposing the raw token. Rejected for v1 due to confused-deputy risk: untrusted task code within the project container can reach the socket and induce malicious Git operations; standard Git credential helpers emit credentials to stdout/sandbox Git, directly violating token confidentiality invariants.
   - *Status:* PROPOSED architecture drafted in `docs/superpowers/specs/2026-10-11-gate-0c-git-transfer-design.md`. Remains unqualified; formal security review and isolated qualification probe execution (Checkpoint A) are mandatory before implementation begins.
2. **Sandbox Outbound Egress Implementation (Gate 0D Resolution):**
   - *Option A (Host Firewall / NFTables Integration Only):* Rely strictly on host-managed packet filtering (e.g. nftables/iptables) directly on container bridges. While strong at L3/L4, host firewalling alone cannot inspect HTTP CONNECT headers, making strict FQDN allowlisting brittle and vulnerable to IP churn and shared CDN hosting.
   - *Option B (Isolated Forward Proxy Container Only):* Route traffic solely via forward proxy environment variables (`HTTP_PROXY`, `HTTPS_PROXY`). Rejected as an independent security boundary because untrusted task code, custom binaries, and raw socket connections easily bypass proxy environment variables.
   - *Option C (Hybrid Host Packet Filtering + Constrained Egress Proxy DMZ - PROPOSED):* Sandbox containers attach to isolated per-project bridges with default-deny routing. Host-managed stateful firewalling strictly permits sandbox traffic only to a dedicated forward proxy (e.g., Squid/Envoy) residing on an isolated DMZ network (not attached to project bridges), blocking all direct internet egress, host gateway/INPUT, and inter-project forwarding. The proxy enforces strict FQDN allowlists for approved package registries.
   - *Status:* PROPOSED architecture drafted in `docs/superpowers/specs/2026-10-11-gate-0d-egress-confinement-design.md`. Remains unqualified; formal security review and isolated qualification probe execution (Checkpoint A) are mandatory before implementation begins.
3. **Dynamic OpenCode Password Lifecycle:**
   - *Decision Needed:* How does the control plane generate and provision the OpenCode server authentication secret (`OPENCODE_PASSWORD` / HTTP Basic auth) without leaking it into `docker inspect` (`container.Config.Env`), container startup CLI arguments, stdout/stderr captured by Docker log drivers, task worktrees, or persistent project volumes?
   - *Evaluated Candidates:* Private memory-backed `tmpfs` secret file exchange (`/run/secrets/opencode`), local Unix domain socket startup handshake, and one-time startup pairing code redemption (`/auth/connect/:code`).
   - *Status:* Unresolved pending prototype qualification. Candidate mechanisms aim to avoid those vectors; none are selected or qualified, and candidate mechanisms do not resolve same-UID process access. Processes sharing the same container and Linux user (`UID 1000`) share a process and filesystem boundary; the exact bootstrap and isolation mechanism remains open pending adversarial review and verification.
4. **Model Provider Credential Delivery & Egress Architecture (Gate 0G Resolution):**
   - *Problem Statement:* The existing runtime design proposes injecting model provider auth into the per-attempt/project environment, but leaves process visibility and child-process protection undefined. In a shared container environment under the same UID, arbitrary task scripts or dependency hooks (e.g. `npm postinstall`, `make`) can inspect `/proc/<pid>/environ` or parent process memory, defeating child-process stripping as a reliable security boundary.
   - *Option A (Control-Plane LLM Gateway - RECOMMENDED DESIGN DIRECTION):* Sandbox containers possess zero model provider API keys and zero direct internet egress to provider APIs. The control plane operates a narrow, authenticating, rate-limited, spend-capped LLM reverse proxy. The sandbox receives only a short-lived, revocable, per-attempt capability token. The gateway enforces server-side project and active-attempt authorization, strict provider/model allowlists, and request/spending caps, and injects upstream provider keys strictly in the control plane outside the sandbox.
   - *Option B (Isolated OpenCode Daemon Environment with Scoped Subprocess Stripping - REJECTED):* OpenCode server runs with provider credentials in its private process environment, attempting to strip keys before spawning task child processes. Rejected as an isolation boundary because same-container/same-UID processes can inspect parent environment via `/proc/<pid>/environ`, memory, or ptrace.
   - *Option C (Internal Credential Helper / Secret Broker - REJECTED):* Local broker daemon signs requests or issues temporary tokens inside the container. Rejected because same-UID task code can access the broker IPC without resolving the shared trust boundary, while adding local IPC complexity.
   - *Status:* PROPOSED architecture drafted in `docs/superpowers/specs/2026-10-11-gate-0g-model-credential-isolation-design.md`. Remains unqualified; formal security review, Gate 0D egress reconciliation, and isolated qualification probe execution are mandatory before Gate 0G can be unblocked or implementation can begin.
5. **Production Deployment Topology, Secret Bootstrap & Licensing Resolution (Gate 0F Resolution):**
   - *Problem Statement:* Current Compose manifests mount the Docker daemon socket (`/var/run/docker.sock:ro`) directly into the public-facing Traefik ingress container, embed insecure development default credentials for PostgreSQL, Redis, secret encryption, OIDC, sessions, and OpenCode, use host path volume bind mounts, lack atomic secret generation and preflight rejection of default passwords, lack a tracked `LICENSE` file, and contain an unresolved source-status conflict regarding runtime catalog baseline approval.
   - *Architecture Direction (PLANNING DIRECTION SELECTED):* Traefik is decoupled from Docker daemon socket access by migrating to a static/templated file-based dynamic configuration provider (`deploy/traefik/dynamic.yml`). Production secrets are generated via an atomic, no-clobber bootstrap CLI writing mode `0600` `.env` files and mode `0640` configs within `0750` directories. Control-plane startup implements fail-closed validation rejecting known development passwords. `SECRET_ENCRYPTION_KEY` maintains a strict representation contract supporting both 64-character hex strings and legacy 32-byte literal strings with non-secret error messages and explicit rotation procedures. Repository licensing is resolved by user choice: MIT License chosen by user. Runtime catalog baseline versions (Go 1.27.2, Node 24.21.0, Python 3.11.17, Java 25.0.4.1+1, Rust 1.99.0, .NET 10.0.401, OpenCode 2.0.26) chosen by user for planning baseline. Dex OIDC in-memory storage chosen for v1 baseline.
   - *Status:* PROPOSED FOR PLANNING (Directional user preferences recorded; independent security sign-off, qualification probes, and Checkpoints A/B remain pending). Implementation governed by downstream plans once unblocked.
6. **Runtime Image Catalog Version Approval:**
   - *Status: CANDIDATES SELECTED FOR PLANNING.* User explicitly selected candidate baseline catalog versions: Go 1.27.2, Node 24.21.0, Python 3.11.17, Java 25.0.4.1+1, Rust 1.99.0, .NET 10.0.401, and OpenCode 2.0.26 OCI candidate pin (`ghcr.io/anomalyco/opencode:2.0.26@sha256:ea6c5d68d8d230923d2627bc793e7fa6a03f8e47c253b50397eb92bbde6db255`). Selection represents planning direction; upstream availability, checksum verification, and provenance qualification remain open.

---

## 10. Document Relationship & Revision History

- **Relationship to `2026-10-10-runtime-image-configuration-design.md`:**
  The runtime image design defines the container internals, `.atm-runtime.json` manifest schema, and union version resolution. This readiness specification defines the end-to-end system operating model, host prerequisites, deployment posture, and release criteria required before that runtime can be safely offered to self-hosting operators.
- **Status of Implementation:**
  Implementation of self-hosting features and project-scoped runtimes remains **strictly blocked** until this specification is reviewed and approved by the user, and qualification gates 0C, 0D, and 0G are formally resolved.
