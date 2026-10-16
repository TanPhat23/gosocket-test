# Specification: Gate 0C Credential-Safe Git Transfer Architecture & Qualification Design

**Status:** APPROVED for Gate 0C at Checkpoint A by the user, 2026-10-16 ("I approve this"); see `verification/2026-10-15-quarantine-gate-status.md`
**Gate Status:** QUALIFIED 2026-10-16 on VM evidence by explicit user sign-off. Does not lift the quarantine.
**Author:** Documentation Implementation Specialist
**Date:** 2026-10-11
**Target Platform:** Single-Host Linux (`linux/amd64`), Docker Compose, Native Docker Engine
**Specification File:** `docs/superpowers/specs/2026-10-11-gate-0c-git-transfer-design.md`

---

## 1. Executive Summary & Objective

This specification details the architecture, isolation boundaries, data transfer protocols, and reproducible qualification criteria for **Gate 0C: Credential-Safe Git Data Transfer**.

Under the self-hosting production operating model defined in `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md`, all task execution occurs inside isolated, unprivileged Docker containers mounting project-scoped persistent data volumes (`atm-project-data-<project_id>`). Host bind mounts are prohibited. Furthermore, untrusted AI agents, arbitrary build scripts, and task child processes run inside the project sandbox.

The primary objective of Gate 0C is to guarantee that **Git personal access tokens (PATs) and Authorization headers remain strictly confined to the trusted control-plane security domain**. No secret material may ever reach the project container environment, process arguments, container logs, project data volumes, `.git/config` files, or network streams accessible by the sandbox.

This document formalizes the user-approved **Option A** direction (trusted control-plane operations and credential-free `git bundle` transfer through a constrained volume helper) and establishes an adversarial, reproducible qualification probe specification that must pass before any implementation work begins.

---

## 2. Direction Analysis: Option A vs. Option B

During architectural exploration, two potential synchronization models were analyzed:

| Dimension | Option A: Trusted Control Plane & Credential-Free Volume Helper (PROPOSED) | Option B: In-Sandbox Unix Socket Credential Helper / Proxy (REJECTED) |
|---|---|---|
| **Token Boundary** | Secrets never cross into the project container or persistent volume. Decrypted PAT is exposed only to an unprivileged, short-lived control-plane worker via in-memory IPC. | Credential helper socket is mounted or exposed via local IPC into the project container, reachable by untrusted task code. |
| **Confused-Deputy Attack Surface** | Zero sandbox exposure to credentials or Git authentication endpoints. Sandbox has no access to Git credentials or helper sockets. | Untrusted task code can connect to the Unix socket and craft arbitrary helper protocol requests (`get`, `store`, `erase`) or proxy requests. |
| **Standard Git Credential Helper Contract** | N/A (Standard helper not run in sandbox). | Per official Git documentation (`gitcredentials(7)`), credential helpers receive attributes on `stdin` and output `username=...` and `password=...` on `stdout`. Returning the token to sandbox Git immediately hands the plaintext secret to untrusted code. |
| **HTTP Proxy Mediation Alternative** | Not required. Task code does not communicate with GitHub. | If implemented as an authenticating HTTP proxy, task code can abuse the proxy to push arbitrary commits, exfiltrate data to unauthorized branches, or alter other repositories accessible to the token. |
| **Git LFS & Submodules** | Explicitly disabled and rejected in v1. Control plane and helper detect LFS pointers and submodules and fail closed immediately. | In-sandbox Git would attempt recursive submodules or LFS smudges, triggering unconstrained external network connections or secondary credential requests. |
| **Gate 0D Egress Confinement Dependency** | Clean decoupling. Project sandbox requires zero outbound internet access to GitHub (`github.com:443`). | Strong coupling. Project sandbox must be granted outbound network egress to `github.com:443` or an internal proxy socket. |
| **Memory / Disk / Transfer Cost** | Moderate. Bundles are transferred via pipe/streaming between ephemeral containers; bare repositories in volume retain packfiles efficiently. | Low disk transfer overhead, but high security liability. |
| **Evaluation Verdict** | **PROPOSED FOR ADOPTION** | **REJECTED FOR V1** |

---

## 3. Git LFS and Submodules Decision: Explicit V1 Rejection

Per user decision, **both Git LFS (Large File Storage) and Git submodules are strictly disabled and rejected in v1**:
1. **Rationale:** Supporting Git LFS requires invoking external smudge/clean filter binaries (`git-lfs filter-process`), issuing auxiliary HTTP requests to LFS batch API endpoints, and handling secondary token authentication. Git submodules introduce embedded repo URLs (including file/relative/arbitrary external protocols) and nested authentication contexts that dramatically expand the attack surface.
2. **Fail-Closed Enforcement:**
   - Any repository or branch containing `.gitmodules` or Gitlink tree entries (`mode 160000`) is rejected upon synchronization with an explicit, sanitized error: `repository contains submodules, which are not supported in v1`.
   - Any commit introducing Git LFS pointer files (detected via `.gitattributes` filter rules or Git LFS header signatures `version https://git-lfs.github.com/spec/v1`) is rejected: `repository contains Git LFS pointers, which are not supported in v1`.
   - ATM will not silently check out raw pointer text or clone incomplete submodule trees; synchronization must fail closed before task execution begins.

---

## 4. Architectural Components & Data Flow

```
                      ┌────────────────────────────────────────────────────────┐
                      │              ATM Control-Plane Controller              │
                      │  - Holds Docker Engine Socket (/var/run/docker.sock)   │
                      │  - Decrypts GitHub PAT in memory from PostgreSQL vault │
                      │  - Relays stdin/stdout bundle streams between helpers  │
                      └──────────────┬───────────────────────────┬─────────────┘
             Launches ephemeral      │                           │ Launches ephemeral
             worker (no Docker sock, │                           │ helper (no Docker sock,
             no project volume)      │                           │ no network, no secrets)
                                     ▼                           ▼
       ┌──────────────────────────────────────────┐  ┌──────────────────────────────────────────┐
       │   Credentialed Git Worker (Ephemeral)   │  │    Volume Transfer Helper (Ephemeral)    │
       │                                          │  │                                          │
       │  - Unprivileged container                │  │  - Unprivileged container                │
       │  - NO Docker socket, NO project volume   │  │  - NO Docker socket, NO network          │
       │  - Egress: GitHub HTTPS (443) ONLY       │  │  - Mounts: atm-project-data-<id> only    │
       │  - PAT provided via in-memory helper     │  │  - Read-only rootfs, drop ALL caps       │
       │  - Fixed controller Git configuration    │  │  - Target directory: /workspaces         │
       │  - NO hooks, NO filters, NO submodules   │  │  - Manages bare repos & task worktrees   │
       │  - Imports / Exports `git bundle` bytes  │  │  - Imports / Exports `git bundle` bytes  │
       └────────────────────┬─────────────────────┘  └─────────────────────┬────────────────────┘
                            │                                              │
                            │ Bundle Byte Stream Relay (Docker stdio)      │
                            └───────────────────◄►─────────────────────────┘
                                                │
                                                ▼
                               ┌──────────────────────────────────┐
                               │  Project Volume: /workspaces     │
                               │  - /workspaces/<repo-id>/...     │
                               │  - Zero secrets in .git/config   │
                               │  - Zero remote auth headers      │
                               └──────────────────────────────────┘
```

### 4.1 Component Responsibilities & Confinement Profiles

#### 1. ATM Control-Plane Orchestrator (Trusted Management Domain)
- Exists in the administrative tier; has access to the Docker Engine socket and application PostgreSQL database.
- Binds safe, canonical repository identifiers (`<repo-id>`) and task identifiers (`<task-id>`) to all operations.
- Decrypts the repository GitHub PAT in memory; never writes the token to disk, environment variables, command-line arguments, or inspection logs.
- Spawns short-lived worker and helper containers via Docker API and streams raw bundle bytes between their standard I/O pipes.

#### 2. Credentialed Git Worker (Ephemeral Management Worker)
- **Role:** Handles all remote Git communication (`git clone`, `git fetch`, `git push`) against the remote GitHub HTTPS repository.
- **Security Boundaries:**
  - Has **zero access to the Docker socket**.
  - Has **zero access to the project data volume** (`atm-project-data-<id>`).
  - Has **zero access to the database or encryption keys**.
  - Bounded container resources (memory, CPU, runtime timeout).
  - Outbound network access restricted strictly to GitHub HTTPS (`github.com:443`).
- **Git Execution Hardening:**
  - Fixed, controller-owned Git configuration. Does not load user-controlled configuration files.
  - All hooks, smudge/clean filters, and submodules disabled (`-c core.hooksPath=/dev/null`, `-c filter.lfs.smudge=`, `-c filter.lfs.clean=`, `-c filter.lfs.process=`, `-c submodule.recurse=false`).
  - Strict system TLS verification enabled (`http.sslVerify=true`).
  - Credential delivery: token passed exclusively via a transient in-memory credential helper feeding `git-credential(1)` via stdin or protected memory pipe. Never in argv (`-c http.extraHeader=...` is banned) and never in container `env`.
  - Never checks out files in its staging area (operates strictly on bare staging repositories).

#### 3. Volume Transfer Helper (Ephemeral Volume Worker)
- **Role:** Manages the persistent project volume filesystem: initializes bare repository mirrors, applies bundles, checks for forbidden files (LFS/submodules), and provisions task worktree directories.
- **Security Boundaries:**
  - Has **zero network access** (`network_mode: none`).
  - Has **zero access to the Docker socket**.
  - Has **zero credentials, tokens, or auth headers**.
  - Runs as an unprivileged user (`no-new-privileges:true`, `--cap-drop=ALL`, `--read-only` rootfs with ephemeral tmpfs scratch at `/tmp`).
  - Mounts **only** the target project's dedicated data volume at `/workspaces`.
- **Git Execution Hardening & Untrusted Config Isolation:**
  - Does not communicate with any external remote.
  - Acts solely on offline `git bundle` files spooled into transient tmpfs scratch (`/tmp/scratch.bundle`).
  - **Untrusted Volume Config Defenses:** Linked worktrees and repositories inside `/workspaces` may contain untrusted, attacker-controlled `.git/config` files written by sandbox task scripts (such as `filter.<name>.smudge` or `filter.<name>.process` paired with `.gitattributes`). To eliminate hostile code execution inside the helper:
    - **No-Checkout Invariant:** The Volume Transfer Helper invokes `git worktree add --no-checkout`. The helper **never checks out files** into the working directory. File checkout (`git checkout`) is delegated exclusively to the untrusted project sandbox container where task code already executes within its dropped-capability, network-confined boundary.
    - Configuration overrides on helper commands:
      - `-c core.hooksPath=/dev/null` (disables hook execution).
      - `-c core.fsmonitor=false` (disables filesystem monitor binary invocation).
    - Under `--no-checkout`, Git provisions only the worktree administrative directory and `.git` pointer file; no blob smudging or filter execution is ever triggered within the Volume Transfer Helper.

---

## 5. End-to-End Data Flow Protocols

### 5.1 Remote Ingestion & Initial Clone / Fetch Flow
Official Git documentation (`git-bundle(1)`) defines a Git bundle as an offline archive of Git references and objects:
- `git bundle create <file> <revisions>` writes a complete packfile bundle. `git bundle create - <revisions>` writes the bundle to standard output.
- `git clone <bundle-file> <path>` or `git fetch <bundle-file>` extracts objects and refs directly from a local bundle file.
- *Bundle Input Semantics:* Per `git-bundle(1)`, `git clone --bare -` and `git fetch -` are invalid syntax (`fatal: repository '-' does not exist`, `fatal: strange pathname '-' blocked`). Git bundle ingestion commands require a valid filesystem path. The Volume Transfer Helper therefore spools incoming bundle byte streams into its private ephemeral tmpfs scratch path (`/tmp/scratch.bundle`) before invoking `git clone` or `git fetch`.

**Step-by-Step Flow:**
1. **Control Plane Staging:** The control plane spawns the Credentialed Git Worker.
2. **Worker Fetch:** The worker performs `git fetch` against `https://github.com/<owner>/<repo>.git` using the in-memory PAT helper into a private bare staging repository.
3. **Worker Inspection:** The worker verifies refs. If `.gitmodules` or LFS pointers are present in target trees, the operation is aborted.
4. **Bundle Generation:** The worker executes `git bundle create - refs/heads/<base-branch>` writing the bundle to stdout.
5. **Stream Relay & Tmpfs Spooling:** The control plane reads the stdout stream from the worker and pipes it into the Volume Transfer Helper, which writes the stream directly to ephemeral tmpfs scratch (`/tmp/scratch.bundle`).
6. **Helper Ingestion:** The Volume Transfer Helper runs:
   - If `/workspaces/<repo-id>/repository.git` does not exist: creates a bare mirror repository using `git clone --bare /tmp/scratch.bundle /workspaces/<repo-id>/repository.git`.
   - If it exists: runs `git -C /workspaces/<repo-id>/repository.git -c core.hooksPath=/dev/null -c core.fsmonitor=false fetch /tmp/scratch.bundle refs/heads/<base-branch>:refs/heads/<base-branch>`.
   - Ephemeral scratch file `/tmp/scratch.bundle` is immediately deleted after ingestion.
7. **Worktree Provisioning (No-Checkout):** The helper provisions the task worktree without checking out files, enforcing configuration isolation:
   ```bash
   git -C /workspaces/<repo-id>/repository.git \
     -c core.hooksPath=/dev/null \
     -c core.fsmonitor=false \
     worktree add --no-checkout \
     -b task/<task-id> \
     /workspaces/<repo-id>/tasks/<task-id> \
     <base-branch>
   ```
   *Sandbox Checkout Delegation:* When the task container starts, the unprivileged task sandbox process performs `git checkout` inside `/workspaces/<repo-id>/tasks/<task-id>`. Because untrusted task scripts already execute in the sandbox, filter or script execution there remains strictly within the untrusted project sandbox boundary and cannot compromise the helper or control plane.
8. **Origin Sanitization:** The repository's configured origin inside `/workspaces/<repo-id>/repository.git` is explicitly set to a local placeholder (or points to the local bare path). No remote URLs containing userinfo, tokens, or credential-helper directives exist.

### 5.2 Task Push & Upstream Synchronization Flow
Per `git-bundle(1)`, `git bundle` is an offline transport format; Git cannot execute `git push` into a bundle file. Furthermore, if an incremental bundle (`task/<task-id> ^<base-branch>`) is used, any upstream rebase, force-update, or concurrent branch movement causes `git fetch` to fail with `error: Repository lacks these prerequisite commits`.

To provide deterministic, race-free push synchronization in v1:
- The Volume Transfer Helper exports a **self-contained bundle of the task branch** (`refs/heads/task/<task-id>` without prerequisite exclusions).
- Self-contained bundles include all commit history required by the task branch, ensuring that upstream branch rewrites, force-pushes, or rebase events never cause missing prerequisite errors.
- Bounded bundle size and object count limits are enforced during export/import to prevent resource exhaustion.

**Step-by-Step Flow:**
1. **Helper Bundle Export:** When a task completes and commits code, the Volume Transfer Helper is invoked against the project volume. It verifies that commits on `task/<task-id>` contain no forbidden LFS pointers or submodules. It creates a self-contained bundle of the task branch into tmpfs scratch with strict config isolation:
   ```bash
   git -C /workspaces/<repo-id>/repository.git \
     -c core.hooksPath=/dev/null \
     -c core.fsmonitor=false \
     bundle create /tmp/export.bundle \
     refs/heads/task/<task-id>
   ```
   and streams `/tmp/export.bundle` to stdout (or pipes stdout directly using `git bundle create - refs/heads/task/<task-id>`), then wipes the tmpfs file.
2. **Stream Relay & Worker Spooling:** The control plane reads the bundle stream and pipes it into the Credentialed Git Worker, which spools the payload into its ephemeral tmpfs scratch (`/tmp/task.bundle`).
3. **Deterministic Worker Staging Import:**
   - The Credentialed Worker initializes an empty bare staging repository (`/worker/staging.git`).
   - The worker imports the self-contained task bundle:
     ```bash
     git -C /worker/staging.git \
       -c core.hooksPath=/dev/null \
       fetch /tmp/task.bundle \
       refs/heads/task/<task-id>:refs/heads/task/<task-id>
     ```
   - Because the task bundle is self-contained, import succeeds deterministically regardless of upstream branch changes.
   - The temporary file `/tmp/task.bundle` is immediately unlinked.
4. **Verified Upstream Push:** The worker executes a strictly validated push against the configured GitHub remote:
   ```bash
   git -C /worker/staging.git \
     -c core.hooksPath=/dev/null \
     push \
     --porcelain \
     --atomic \
     https://github.com/<owner>/<repo>.git \
     refs/heads/task/<task-id>:refs/heads/task/<task-id>
   ```
   **Security Rules for Push:**
   - Force-push (`--force`, `+ref`) is strictly prohibited.
   - Pushes to default/base branches (`main`, `master`, etc.) are prohibited; the target ref must match `refs/heads/task/<task-id>`.
   - The remote URL is strictly derived from the control plane's internal database record for the repository.

---

## 6. Filesystem & Volume Layout

All paths inside the project data volume (`atm-project-data-<project_id>`) mount at `/workspaces` and use safe, server-generated canonical identifiers:

```text
/workspaces/
├── <repo-id>/
│   ├── repository.git/               # Bare shared repository mirror
│   │   ├── HEAD
│   │   ├── config                    # Clean, credential-free configuration
│   │   ├── objects/
│   │   ├── refs/
│   │   │   └── heads/
│   │   │       ├── main
│   │   │       └── task/<task-id>
│   │   └── worktrees/
│   │       └── <task-id>/            # Git metadata for linked worktree
│   └── tasks/
│       └── <task-id>/                # Linked worktree workspace
│           ├── .git                  # Worktree gitdir pointer to repository.git
│           ├── <project source code>
│           └── ...
```

### Path & Identifier Invariants:
1. **Identifier Safety:** `<repo-id>` and `<task-id>` are alphanumeric UUIDv4 or ULID strings generated by the ATM control plane. Raw repository slugs (e.g. `owner/repo`) and task-supplied titles are never used in volume directory paths, preventing path traversal attacks.
2. **Worktree Shared Configuration:** Per official Git documentation (`git-worktree(1)`), linked worktrees share the common repository configuration at `/workspaces/<repo-id>/repository.git/config`. Because untrusted task scripts running inside the sandbox could modify this config file (e.g. injecting malicious hooks or credential helper directives), the **Credentialed Git Worker must never read, reference, or trust the configuration in `/workspaces`**. The worker operates strictly within its own isolated, controller-owned staging environment.

---

## 7. Failure Semantics & Error Handling

To satisfy the non-negotiable security requirements in readiness spec §5.2 and §8.3:
1. **Fail-Closed on All Errors:** Any failure during clone, bundle generation, streaming, unpacking, worktree creation, or push terminates the operation immediately with a sanitized error. Under no circumstances does the system fall back to host Git execution or unverified TLS.
2. **Credential Sanitization in Errors:** All error messages returned to users, recorded in database attempt logs, or emitted over WebSocket streams must pass through an automated credential scrubber that strips all tokens, Authorization headers, and internal worker IDs.
3. **No Retries with Lowered Security:** If TLS validation fails (e.g. self-signed certificate, expired CA), the operation fails permanently. ATM will never retry with `-c http.sslVerify=false` or `GIT_SSL_NO_VERIFY=true`.
4. **Push Failure Semantics:** If an upstream push fails (e.g., branch conflict, network timeout, branch protection failure on GitHub), the task is marked as failed or manual-resolution required. No automatic Pull Request is generated, and force-pushes are blocked.
5. **Resource and Size Bounding:** Bounded limits on bundle sizes, maximum object counts, and transfer timeouts must be enforced to protect against denial-of-service packfile bombs.

---

## 8. Current Codebase Delta & Legacy Code Disclaimer

**IMPORTANT SECURITY DISCLAIMER:**
The existing codebase is **NOT made secure by the approval of this design document**.

As documented in `internal/git/sync.go` and `internal/git/push.go`:
- Lines 59 and 29 pass authorization tokens directly in command-line arguments: `-c http.extraHeader=Authorization: Basic ...`. This exposes secrets to any local process capable of inspecting process arguments (`ps aux` / `/proc`).
- Lines 68–73 and 43–49 explicitly catch TLS errors and automatically retry with `-c http.sslVerify=false`.
- `internal/runner/task_runner.go:442` uses sanitized repository slugs directly on the host filesystem (`r.cfg.HostRepoRoot`), with fallback to host Git and host package managers.

**Implementation of Gate 0C and removal of these legacy vulnerabilities remains strictly blocked** until this specification is reviewed and approved by the user, and an isolated qualification probe is executed.

---

## 9. Gate 0C Reproducible Qualification Probe Specification

This section defines the formal, reproducible qualification battery required to certify Gate 0C before merging any implementation code into main.

> **Execution Note:** This specification is a test plan and protocol definition. **It is NOT being executed during this documentation task.** Qualification must be performed in an isolated, dedicated test environment (e.g. a disposable Linux VM with native Docker Engine).

### 9.1 Qualification Environment Setup
1. **Host Environment:** Clean Ubuntu 24.04 LTS `linux/amd64` instance with native Docker CE 26+.
2. **Isolated Volume:** Dedicated Docker volume `probe-gate0c-data`.
3. **Fixture Git Repository & Network Accommodation:**
   - A fixture repository served over TLS by a mock HTTPS Git server running on the isolated test Docker network.
   - *Harness Egress Accommodation:* Production worker policy enforces strict GitHub HTTPS (`github.com:443`). To allow the qualification test harness to validate the worker without modifying production container networking logic, the test harness maps `github.com` via internal container DNS / `/etc/hosts` to the mock fixture server's isolated test network IP, or mounts a test CA bundle into the worker's trust store. Outbound internet access remains completely blocked; no public network access is granted.
4. **Synthetic Canary Credential (In-Memory Generation):**
   - The test harness generates a high-entropy canary token in test memory at runtime using a CSPRNG (`ghp_ATM_CANARY_` prefix followed by 32 cryptographically secure random hexadecimal characters).
   - *Strict Tracking & Environment Invariant:* The canary token is **never hardcoded or committed to git history**, and must **never appear in shell command arguments (`argv`), host/container environment variables (`env`), inspectable container configurations, or tracked documentation**. The token is delivered exclusively via secure in-memory IPC/pipe directly to the test credential helper stdin.

### 9.2 Probe Test Cases & Verification Assertions

#### Probe 1: Positive Clone & Worktree Round-Trip
- **Procedure:**
  1. Spawns Credentialed Worker with runtime CSPRNG canary token via in-memory stdin helper.
  2. Worker fetches from fixture HTTPS repository and streams `git bundle` to Volume Transfer Helper.
  3. Helper spools bundle to tmpfs and unpacks into `probe-gate0c-data` at `/workspaces/repo-001/repository.git`, then runs `git worktree add --no-checkout` to create worktree `task-001`.
  4. Task sandbox container attaches to volume and executes `git checkout`.
- **Assertions:**
  - Helper container exits with status `0`.
  - Directory `/workspaces/repo-001/tasks/task-001` exists and contains tracked repository files after sandbox checkout.
  - Worktree is on branch `task/task-001`.

#### Probe 2: Canary Leakage Scan (Volume, Config, History, Env, Args, /proc)
- **Procedure:**
  - Execute exhaustive redaction scan across the entire project data volume, Docker inspect metadata for all involved containers, image layer histories, runtime process command-line tables, and container log streams.
- **Assertions:**
  1. `git -C /workspaces/repo-001/repository.git remote -v`: Must NOT contain credentials or auth headers.
  2. `cat /workspaces/repo-001/repository.git/config`: Must NOT contain `extraHeader`, `token`, `password`, or canary string.
  3. Volume file inspection: Full grep of `/workspaces` must return ZERO occurrences of the canary token or `Authorization:`.
  4. Docker inspect across all containers:
     - `docker inspect <credentialed-worker-container>`: Environment variables (`Config.Env`) and command arguments (`Config.Cmd`, `Config.Entrypoint`, `Args`) must contain ZERO occurrences of the canary token or `Authorization:`.
     - `docker inspect <helper-container>`: Environment variables (`Config.Env`) and command arguments (`Args`) must contain ZERO occurrences of the canary token or `Authorization:`.
     - `docker inspect <sandbox-container>`: Environment variables (`Config.Env`) and command arguments (`Args`) must contain ZERO occurrences of the canary token or `Authorization:`.
  5. Container stdout/stderr logs across all three containers (`credentialed-worker`, `helper`, `sandbox`) must contain ZERO occurrences of the canary token or `Authorization:`.
  6. Docker image history inspection: `docker history <worker-image>` and `docker history <helper-image>` must show ZERO occurrences of canary credentials or auth headers baked into image layers.
  7. Runtime process argument inspection: During active operations, inspecting `/proc/$PID/cmdline` and `ps aux` across host and containers must verify that credentials never appear in command-line arguments.

#### Probe 3: Positive Commit, Bundle Export & Push Round-Trip (With Simulated Base Branch Drift)
- **Procedure:**
  1. Sandbox modifies a file in `/workspaces/repo-001/tasks/task-001`, stages, and commits it.
  2. Simulate upstream base branch rebase/amend or concurrent drift on the fixture remote.
  3. Helper exports self-contained bundle `refs/heads/task/task-001` to stdout without prereq exclusion.
  4. Worker imports bundle into empty bare staging repository and pushes to fixture HTTPS remote using canary token.
- **Assertions:**
  - Upstream fixture repository receives branch `task/task-001` with new commit.
  - Worker push exits with status `0`, succeeding deterministically despite upstream base drift.
  - Neither force-push nor pushes to `main` are permitted.

#### Probe 4: TLS Insecurity Negative Test
- **Procedure:**
  - Direct Worker against an HTTPS fixture presenting an untrusted/self-signed certificate without configuring custom CA trust.
- **Assertions:**
  - Worker must fail closed immediately (non-zero exit).
  - Worker must NOT retry with `http.sslVerify=false` or `GIT_SSL_NO_VERIFY=true`.
  - Zero bundle data emitted.

#### Probe 5: Submodule Rejection Negative Test
- **Procedure:**
  - Supply a fixture repository containing `.gitmodules` and a Gitlink commit entry.
- **Assertions:**
  - Worker and Helper detect submodule configuration.
  - Operation terminates immediately with error code `ERR_SUBMODULES_NOT_SUPPORTED`.
  - No worktree created in `/workspaces`.

#### Probe 6: Git LFS Pointer Rejection Negative Test
- **Procedure:**
  - Supply a fixture repository containing a Git LFS pointer file (`version https://git-lfs.github.com/spec/v1`).
- **Assertions:**
  - Operation terminates immediately with error code `ERR_LFS_NOT_SUPPORTED`.
  - Task execution blocked.

#### Probe 7: Untrusted Volume Config & Malicious Filter Negative Test
- **Procedure:**
  - Supply a repository with a `.gitattributes` file mapping files to a malicious smudge filter (`* filter=malicious`), and inject `filter.malicious.smudge = "touch /tmp/PWNED_HELPER"` into `/workspaces/<repo-id>/repository.git/config`.
  - Execute Volume Transfer Helper worktree provisioning (`worktree add --no-checkout`).
- **Assertions:**
  - Volume Transfer Helper exits with status `0`.
  - Marker file `/tmp/PWNED_HELPER` is NOT created within the Volume Transfer Helper container.
  - Proves malicious filter was not executed by the helper.

#### Probe 8: Path Traversal & Identifier Escape Negative Test
- **Procedure:**
  - Attempt to invoke sync using malicious repo/task IDs containing relative paths (`../../etc`, `repo/sub`).
- **Assertions:**
  - Control plane rejects identifiers at input validation boundary.
  - Volume helper path validation prevents any directory creation outside `/workspaces/<safe-id>`.

#### Probe 9: Sandbox Network Confinement Verification (Gate 0D Synergy)
- **Procedure:**
  - From inside the task sandbox container, attempt network connections to GitHub HTTPS (`curl -m 2 https://github.com`) and local Docker socket (`curl --unix-socket /var/run/docker.sock`).
- **Assertions:**
  - All connections fail closed (network unreachable or timeout).
  - Sandbox proves complete independence from external Git network egress.

---

## 10. Official Git References Checked

The design and invariants specified above are grounded in official Git documentation:
1. **`git-bundle(1)` (https://git-scm.com/docs/git-bundle):**
   - Git bundles package Git objects and references into a single archive file or byte stream (`git bundle create - <git-rev-list-args>`).
   - Bundles carry only commits, trees, blobs, and tag/branch refs; they never carry repository config (`.git/config`), hooks, working trees, or credentials.
   - Bundles can be fetched or cloned directly (`git clone <bundle>`, `git fetch <bundle>`).
   - Git bundles do not support `git push` into a bundle file, establishing the necessity of the two-phase push architecture defined in §5.2.
2. **`gitcredentials(7)` (https://git-scm.com/docs/gitcredentials):**
   - Git credential helpers receive configuration queries on standard input and output attributes (`username=...`, `password=...`) on standard output.
   - Exposing a standard credential helper to sandbox Git would transmit plaintext credentials directly to untrusted task processes, justifying the rejection of Option B.
   - Helper scripts are invoked through a shell; helper commands must be fixed, immutable, and strictly controlled by the control plane.
3. **`git-worktree(1)` (https://git-scm.com/docs/git-worktree):**
   - Linked worktrees share common repository configuration (`$GIT_DIR/config`).
   - Because worktree processes could write to shared configuration, the control-plane credentialed worker must never use or load worktree configurations from the project volume.
