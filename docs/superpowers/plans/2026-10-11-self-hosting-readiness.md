# Master Self-Hosting Readiness & Qualification Roadmap

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to coordinate and track this roadmap task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish a verified, gated master implementation roadmap to reconcile qualification evidence, formally resolve architectural security blockers, obtain required sign-offs, and commission bounded downstream implementation plans before releasing Agent Task Manager for single-host Linux self-hosting.

**Architecture:** A gated, phase-structured roadmap that sequences durable documentation reconciliation and unresolved security decision gates (Git credential safety, egress default-deny, model credential protection, and production secrets/licensing) before permitting any product code implementation or runtime plan reconciliation. Once qualification gates pass, implementation is executed via separate, bounded subsystem plans (reusing and updating the existing project-scoped runtime plan rather than duplicating it), culminating in clean-host positive and negative verification.

**Tech Stack:** Go 1.27, Docker Engine CE 26+/27+/29+ (native `linux/amd64`), Docker Compose v2.20+, OpenCode Server v2.0.26, PostgreSQL 18, Redis 7, Dex OIDC, Traefik v3, React 19 / TypeScript.

**Spec:** `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md`

---

## Global Constraints

- **Platform Scope:** Strictly single-host `linux/amd64` running native systemd Docker Engine (CE 26+/27+/29+) and Docker Compose v2.20+. Snap Docker (`snap install docker`) is strictly unsupported due to verified AppArmor/NNP failure (`exit 255`).
- **No Early Implementation:** All product code modifications, Compose service additions, and environment mutations remain strictly blocked until pre-implementation architectural gates (0C, 0D, 0G, 0F design, scoped 0A/0B evidence reconciliation, plus dynamic password lifecycle, runtime catalog approval, Docker daemon privilege boundary, and secret key contract decisions) receive formal user and security sign-off at Checkpoint A, followed by explicit approval of reconciled downstream implementation plans at Checkpoint B. Implementation and hardening gates (Gate 0E, Gate 0F implementation, and runtime tasks) are executed under downstream plans and remain release-blocking until their qualification and acceptance pass, but do not circularly block commencing implementation after Checkpoints A and B pass.
- **Fail-Closed Privilege & Confinement:** Sandboxes must enforce `--security-opt no-new-privileges:true`, `--cap-drop=ALL`, `--read-only`, bounded memory/CPU/PIDs, and tmpfs scratch with `noexec,nosuid,nodev`. Bypassing, softening, or omitting NNP or AppArmor profiles is strictly forbidden.
- **Zero Host Execution Fallback:** Compilers, linters, package managers (`npm`, `go`, `pip`), and task scripts execute exclusively inside sandbox containers. `runOnHost` and host package execution in `internal/runner/dependencies.go` must be excised entirely.
- **No Unsafe Fallbacks or Bypasses:** Silent retries with `http.sslVerify=false` or `GIT_SSL_NO_VERIFY=true` are strictly prohibited; unverified TLS must fail closed.
- **No Shared Secrets or Default Credentials:** Hardcoded default credentials, dev secrets, developer host paths (`/home/ghug/...`), and Traefik Docker socket mounts are banned from production Compose assets.
- **Credential Protection Invariants:** Git tokens, database passwords, and model provider API keys (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) must never appear in container command-line arguments, persistent files, stdout logs, or shell environments inherited by untrusted task child processes.
- **Mutual Project Trust Boundary:** Repositories and tasks within the same project share a single container and volume trust boundary; inter-task multi-tenancy within the same project is an explicit non-guarantee.
- **Downstream Plan Reconciliation:** Do not duplicate the 9-task project-scoped runtime plan here. The existing plan (`docs/superpowers/plans/2026-10-10-runtime-image-configuration.md`) is maintained as the downstream runtime execution plan and must be reconciled after decision sign-off.

---

## Review Focus

1. **Premature Implementation Attempt:** An executor attempts to modify `internal/runner/task_runner.go`, `docker-compose.yml`, or other product code before Gates 0C, 0D, 0F, and 0G are formally signed off; expected behavior: execution halts immediately at the phase gate.
2. **Credential Leakage to Task Child Processes:** OpenCode server runs with provider credentials in its process environment, but spawns bash/tool child processes that inherit those variables; expected behavior: Gate 0G design explicitly pins child-process environment scrubbing or control-plane proxy mediation before implementation.
3. **Overstating VM Qualification as Production Verification:** Documentation claims Gate 0A/0B passed for production when evidence was only collected in an isolated QEMU/KVM guest; expected behavior: reconciled documentation explicitly delineates VM-isolated probe scope from native production host prerequisites.
4. **Weakening Network Egress to Allow Model APIs:** Gate 0D egress filtering is relaxed with broad outbound internet access to permit LLM calls; expected behavior: egress rules remain default-deny, allowlisting only verified package registries and (if direct) explicitly pinned provider endpoints, tightly coupled with Gate 0G credential isolation.
5. **Incompatible Secret Encryption Key Format:** Operator supplies a 32-character hex or base64 key that fails length or decoding checks; expected behavior: Gate 0F design explicitly defines the configuration contract (raw vs hex/base64 decoding) and validation errors.

---

## Roadmap Phases and Stop Gates Overview

```text
┌──────────────────────────────────────────────────────────────────────────────────┐
│                   PHASE 1: Tracked Evidence Reconciliation                       │
│  - Task 1: Reconcile Gate 0A & 0B status and evidence in tracked specs/plans     │
└────────────────────────────────────────┬─────────────────────────────────────────┘
                                         ▼
┌──────────────────────────────────────────────────────────────────────────────────┐
│                 PHASE 2: Architecture & Qualification Gate Specs                 │
│  - Task 2: Gate 0C (Credential-Safe Git Transfer) Design & Qualification Spec    │
│  - Task 3: Gate 0D (Default-Deny Egress Confinement) Design & Qualification Spec │
│  - Task 4: Gate 0G (Model Provider Credential Isolation) Architectural Decision  │
│  - Task 5: Gate 0F (Compose, Secrets Bootstrap, Key Encoding & License) Spec     │
└────────────────────────────────────────┬─────────────────────────────────────────┘
                                         ▼
┌──────────────────────────────────────────────────────────────────────────────────┐
│              CHECKPOINT A: Formal Architecture & Security Sign-Off               │
│               *** HARD STOP: Implementation remains strictly locked ***          │
└────────────────────────────────────────┬─────────────────────────────────────────┘
                                         ▼
┌──────────────────────────────────────────────────────────────────────────────────┐
│             PHASE 3: Downstream Plan Commissioning & Code Implementation         │
│  - Task 6: Reconcile Runtime Plan & Commission Subsystem Implementation Plans    │
└────────────────────────────────────────┬─────────────────────────────────────────┘
                                         ▼
┌──────────────────────────────────────────────────────────────────────────────────┐
│              CHECKPOINT B: Downstream Implementation Plans Approval              │
│         *** HARD STOP: Product code blocked pending downstream plan approval *** │
└────────────────────────────────────────┬─────────────────────────────────────────┘
                                         ▼
┌──────────────────────────────────────────────────────────────────────────────────┐
│  - Task 7: Execute Hardening & De-coupling Implementation (Gate 0E, 0F, 0C, 0G)  │
│            and Runtime Plan Tasks (Tasks 1-9 including full verification suite)  │
└────────────────────────────────────────┬─────────────────────────────────────────┘
                                         ▼
┌──────────────────────────────────────────────────────────────────────────────────┐
│                  PHASE 4: Clean-Host Acceptance & Verification                   │
│  - Task 8: End-to-End Clean-Host Positive & Fail-Closed Negative Battery         │
└──────────────────────────────────────────────────────────────────────────────────┘
```

---

## Detailed Task Breakdown

### Task 1: Reconcile Tracked Qualification Evidence for Gates 0A and 0B

**Files:**
- Create: `docs/superpowers/verification/2026-10-11-runtime-qualification-gates-0a-0b.md`
- Modify: `docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md`
- Modify: `docs/superpowers/plans/2026-10-10-runtime-image-configuration.md`
- Reference: `.superpowers/sdd/2026-10-10-runtime-image-configuration/progress.md`
- Reference: `.superpowers/sdd/2026-10-10-runtime-image-configuration/gate-0a-report.md`
- Reference: `.superpowers/sdd/2026-10-10-runtime-image-configuration/gate-0b-report.md`

**Interfaces:**
- Consumes: Ignored local ledger and reports documenting the dedicated Ubuntu 24.04 QEMU/KVM guest probes.
- Produces: Durable tracked verification report (`docs/superpowers/verification/2026-10-11-runtime-qualification-gates-0a-0b.md`) capturing redacted, safe technical evidence, alongside formally reconciled tracked runtime spec and plan with precise technical scope (VM qualification scope vs. native production host requirements).

- [ ] **Step 1: Audit tracked files against local qualification evidence**
Review discrepancies between `progress.md` (where 0A/0B are marked passed in QEMU VM) and `2026-10-10-runtime-image-configuration-design.md` (where 0A/0B remain marked open).

- [ ] **Step 2: Create durable tracked qualification evidence report**
Create `docs/superpowers/verification/2026-10-11-runtime-qualification-gates-0a-0b.md` synthesizing durable evidence from the local reports under strict redaction constraints:
  - Copy only safe technical evidence: exact NNP probe command and output/exit status (`NoNewPrivs: 1`, exit 0), guest OS distribution (Ubuntu 24.04 LTS), guest kernel version, Docker Engine version (`29.8`), candidate image digest (`sha256:ea6c5d68d8d230923d2627bc793e7fa6a03f8e47c253b50397eb92bbde6db255`), candidate ABI/binary architecture (`linux/amd64`, musl), and HTTP loopback authentication probe results (challenge status 401, authorized status 200).
  - Explicitly document strict technical limitations: qualification applies strictly to the dedicated Ubuntu 24.04 QEMU/KVM guest; Snap Docker is verified incompatible; arbitrary production hosts are NOT qualified; Gates 0C, 0D, 0E, 0F, 0G, dynamic OpenCode password lifecycle, and runtime catalog approval remain open release blockers.
  - Mandatory redaction exclusions: strictly exclude host/VM private SSH keys, pinned host key fingerprints, VM disk image paths/hashes, seed ISO data, developer personal host paths (`/home/...`), server-generated passwords, raw Basic-auth byte sequences/headers, and any credential or token material.
  - Preflight constraint: perform pure documentation synthesis; do NOT start/restart the QEMU VM, execute Docker commands, or run application builds/tests during evidence reconciliation.

- [ ] **Step 3: Update tracked runtime specification**
Update Section 7 of `docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md` to reference `docs/superpowers/verification/2026-10-11-runtime-qualification-gates-0a-0b.md` and record:
  - Gate 0A qualified in dedicated Ubuntu 24.04 QEMU/KVM guest (`docker-ce 29.8`, `NoNewPrivs: 1`, exit 0). Explicitly state that Snap Docker is verified incompatible.
  - Gate 0B qualified in dedicated guest with candidate digest `ghcr.io/anomalyco/opencode:2.0.26` (`sha256:ea6c5d68d8d230923d2627bc793e7fa6a03f8e47c253b50397eb92bbde6db255`). Note that loopback Basic auth succeeded, while production password lifecycle, publisher provenance verification, and log suppression remain open.

- [ ] **Step 4: Update tracked runtime implementation plan status headers**
Update Gate 0A and Gate 0B sections in `docs/superpowers/plans/2026-10-10-runtime-image-configuration.md` to reference the tracked verification report and reflect the VM-qualified status while keeping Tasks 1–9 blocked pending remaining gates.

- [ ] **Step 5: Verify text consistency and diff hygiene**
Run: `git diff docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md docs/superpowers/plans/2026-10-10-runtime-image-configuration.md`
Expected: Only status lines, qualification evidence references, and VM scope descriptions are modified.

- [ ] **Step 6: Commit reconciliation changes and tracked evidence artifact**
```bash
git add docs/superpowers/verification/2026-10-11-runtime-qualification-gates-0a-0b.md docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md docs/superpowers/plans/2026-10-10-runtime-image-configuration.md
git commit -m "docs: reconcile tracked qualification evidence for gates 0a and 0b"
```

---

### Task 2: Author Gate 0C Architecture & Qualification Design (Credential-Safe Git Transfer)

**Files:**
- Create: `docs/superpowers/specs/2026-10-11-gate-0c-git-transfer-design.md`
- Modify: `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md:363-366`

**Interfaces:**
- Consumes: Readiness spec Section 9.1 (Option A: Control-plane volume transfer vs Option B: Git credential helper Unix socket proxy).
- Produces: Approved architectural design doc and reproducible probe script specification for credential-safe Git synchronization into `atm-project-data-<id>`.

- [ ] **Step 1: Evaluate trade-offs between transfer options**
Analyze Option A (control plane executes Git clone/fetch into local staging or directly streams tar/packfile into project Docker volume via API) vs. Option B (sandboxed Git CLI connects to control-plane Unix credential helper socket). Evaluate impact on attack surface, memory/disk overhead, and Git LFS / submodules.

- [ ] **Step 2: Draft Gate 0C specification**
Write `docs/superpowers/specs/2026-10-11-gate-0c-git-transfer-design.md` defining:
  - Selected data transfer architecture.
  - Invariant: Zero Git personal access tokens or HTTP Authorization headers stored in `.git/config`, volume files, environment variables, or container logs.
  - Exact Docker volume path layout (`/workspaces/<repo>/...`).
  - Isolated qualification probe specification (commands to execute in VM/isolated test container to prove transfer succeeds and secrets are absent).

- [ ] **Step 3: Review spec against credential leakage invariants**
Verify that the probe inspects `git remote -v`, `.git/config`, `env`, and container history for token leakage.

- [ ] **Step 4: Commit Gate 0C specification**
```bash
git add docs/superpowers/specs/2026-10-11-gate-0c-git-transfer-design.md
git commit -m "docs: specify gate 0c credential-safe git transfer architecture"
```

---

### Task 3: Author Gate 0D Architecture & Qualification Design (Default-Deny Sandbox Egress)

**Files:**
- Create: `docs/superpowers/specs/2026-10-11-gate-0d-egress-confinement-design.md`
- Modify: `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md:367-370`

**Interfaces:**
- Consumes: Readiness spec Section 3.3, 7.2, and 9.2 (Option A: nftables/iptables integration vs Option B: Forward proxy container).
- Produces: Approved egress control design and test harness specification for default-deny network enforcement.

- [ ] **Step 1: Evaluate egress implementation mechanisms**
Analyze Option A (host nftables orchestration on Docker bridge) vs. Option B (isolated bridge with forward proxy container like Envoy/Squid enforcing strict CONNECT allowlists). Consider portability across standard Linux distributions, privilege requirements of control plane, and container environment variables (`HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`).

- [ ] **Step 2: Draft Gate 0D specification**
Write `docs/superpowers/specs/2026-10-11-gate-0d-egress-confinement-design.md` defining:
  - Exact network mechanism and rule topology.
  - Prohibited CIDRs: `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.169.254`, `fe80::/10`, `fc00::/7`, Docker bridge gateway IPs, control plane internal ports (`5432`, `6379`, `5556`, `8080`).
  - Approved allowlist destinations: npm, PyPI, Go proxy, Crates.io, NuGet, Maven Central.
  - Isolated qualification probe specification: matrix of positive curl commands to registries and negative curl commands to internal IPs/metadata with timeout assertions.

- [ ] **Step 3: Verify no weakening of host security**
Confirm design does not require granting Docker daemon host network access or root privileges to the sandbox.

- [ ] **Step 4: Commit Gate 0D specification**
```bash
git add docs/superpowers/specs/2026-10-11-gate-0d-egress-confinement-design.md
git commit -m "docs: specify gate 0d default-deny sandbox egress architecture"
```

---

### Task 4: Author Gate 0G & OpenCode Password Architecture Decision Record

**Files:**
- Create: `docs/superpowers/specs/2026-10-11-gate-0g-model-credential-isolation-design.md`
- Modify: `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md:371-379`

**Interfaces:**
- Consumes: Readiness spec Section 4 (Discrepancy 159), 5.2.2, 7.2 (Gate 0G criteria), and Section 9.3/9.4.
- Produces: Architectural decision defining model provider credential isolation, child process environment sanitization, and the dynamic OpenCode password lifecycle.

- [ ] **Step 1: Evaluate model credential protection options**
Evaluate:
  - Option A: Control-plane LLM reverse proxy (control plane injects API keys outside container; container has zero provider secrets).
  - Option B: Scoped OpenCode process environment with child process stripping (OpenCode daemon holds keys in memory/private env; all tool/shell subprocesses run with stripped environment).
  - Option C: Local secret broker daemon.
Assess each option against the risk of arbitrary prompt injection, malicious task dependencies (`node_modules`), or build script inspection.

- [ ] **Step 2: Define dynamic OpenCode password lifecycle**
Specify how the control plane generates or retrieves the OpenCode server authentication token without persisting it to inspectable container stdout or Docker log drivers (e.g. `--log-driver=none`, tmpfs file exchange, or container start API handshake).

- [ ] **Step 3: Draft Gate 0G design specification**
Write `docs/superpowers/specs/2026-10-11-gate-0g-model-credential-isolation-design.md` documenting:
  - Selected model credential isolation architecture.
  - Contract for child process execution (guarantee that `ps aux`, `/proc/<pid>/environ`, and `env` inside task commands cannot view model API keys).
  - Reconciled network egress dependency (how LLM requests egress under the Gate 0D model).
  - Dynamic OpenCode password generation, transmission, and memory-only storage.
  - Qualification test specification.

- [ ] **Step 4: Commit Gate 0G specification**
```bash
git add docs/superpowers/specs/2026-10-11-gate-0g-model-credential-isolation-design.md
git commit -m "docs: specify gate 0g model credential isolation and opencode password lifecycle"
```

---

### Task 5: Author Gate 0F Production Compose, Secret Bootstrap & Licensing Specification

**Files:**
- Create: `docs/superpowers/specs/2026-10-11-gate-0f-production-deployment-design.md`
- Modify: `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md:380-384`

**Interfaces:**
- Consumes: Readiness spec Section 4 (Discrepancies 155, 161, 162, 163), Section 5.3, Section 6.1, 6.2, and Section 9.5.
- Produces: Approved specification for clean-host Compose topology, secret validation, encryption key encoding contract, Docker socket protection, and license resolution.

- [x] **Step 1: Define production Compose and secret contract**
Specify:
  - Traefik Docker socket decoupling: use file-based dynamic configuration or an isolated, read-only Docker socket filter proxy restricted strictly to label reading, eliminating direct `/var/run/docker.sock` mount.
  - Secret initialization and encoding: formally evaluate and specify the `SECRET_ENCRYPTION_KEY` representation and configuration contract. Assess whether to maintain the existing literal 32-byte string requirement or implement support for decoded representations (such as hex or base64) in `internal/config`, defining objective length and formatting validation rules alongside rejection of known default/example strings (avoiding subjective runtime entropy measurement).
  - Production bootstrap tool/script to generate high-entropy secrets and write mode `0600` `.env`.
  - Startup validation: control plane and Compose pre-flight checks fail immediately if default/test passwords (`postgres`, `secretredis`, `0123456789...`) are detected.
  - Repository `LICENSE` resolution: maintainer/user must explicitly confirm the intended licensing terms or obtain legal guidance before adding a tracked license file; do not assume MIT based solely on README badges.
  - Runtime catalog approval: pin baseline compiler/language toolchain versions for Go, Node.js, Python, Java, Rust, and .NET.

- [x] **Step 2: Draft Gate 0F design specification**
Write `docs/superpowers/specs/2026-10-11-gate-0f-production-deployment-design.md` with complete YAML schemas, config structs, error messages, and qualification probes.

- [x] **Step 3: Commit Gate 0F specification**
```bash
git add docs/superpowers/specs/2026-10-11-gate-0f-production-deployment-design.md
git commit -m "docs: specify gate 0f production compose, secret bootstrap and licensing"
```

---

### Checkpoint A: Architecture & Qualification Gate Sign-Off (STATUS: PENDING SECURITY REVIEW & QUALIFICATION)

> **CRITICAL HARD STOP:** Product code implementation, Compose modifications, and host operations remain strictly locked. The user has provided directional planning decisions on architecture direction, catalog versions, license choice, and Dex memory storage for v1; however, Checkpoint A remains **PENDING** required independent security review, isolated qualification probe execution, and explicit plan authorization. Checkpoint A is NOT fully cleared.

- [ ] **Review Deliverables & Security Sign-Off:**
  1. Reconciled Gate 0A and 0B tracked documentation and durable evidence artifact (`docs/superpowers/verification/2026-10-11-runtime-qualification-gates-0a-0b.md`).
  2. Gate 0C specification (`docs/superpowers/specs/2026-10-11-gate-0c-git-transfer-design.md`).
  3. Gate 0D specification (`docs/superpowers/specs/2026-10-11-gate-0d-egress-confinement-design.md`).
  4. Gate 0G specification (`docs/superpowers/specs/2026-10-11-gate-0g-model-credential-isolation-design.md`).
  5. Gate 0F specification (`docs/superpowers/specs/2026-10-11-gate-0f-production-deployment-design.md`).
- [ ] **User Planning Decisions (Recorded Separately from Qualification/Sign-Off):**
  - **User Directional Input:** The user confirmed that the architecture direction looks good for planning, chose the MIT license, selected catalog versions, selected Dex in-memory storage for v1, and selected the subagent execution approach.
  - **Explicit Non-Evidence:** These directional preferences do NOT constitute independent security sign-off, successful qualification, approved host changes, or resolution of the OpenCode dynamic password lifecycle.
  - **Catalog Verification Caveat:** Approval of catalog version numbers is not evidence of upstream package availability, checksum verification, or provenance.
  - **Unresolved OpenCode Password Lifecycle:** Gate 0G dynamic password provisioning remains unresolved; tmpfs delivery (`/run/secrets/opencode`) was not approved as fully secure and lacks an entrypoint consumption mechanism.
  - User preferences stand and are preserved:
    - Repository License Choice: MIT License chosen.
    - Runtime Catalog Baseline Selection: Go 1.27.2, Node 24.21.0, Python 3.11.17, Java 25.0.4.1+1, Rust 1.99.0, .NET 10.0.401, OpenCode 2.0.26 selected.
    - Dex OIDC Storage Selection: In-memory storage chosen for v1 production baseline.
    - Architecture Directions: Gate 0C (Option A: git bundles & volume transfer helper), Gate 0D (Option C: hybrid host default-deny packet filtering + DMZ forward proxy), Gate 0G (Option A: control-plane LLM reverse proxy gateway & per-attempt capabilities), and Gate 0F (Traefik file provider socket decoupling, atomic bootstrap script, fail-closed preflight validation) chosen for planning.

---

### Task 6: Reconcile Runtime Plan & Commission Downstream Subsystem Plans

**Files:**
- Modify: `docs/superpowers/plans/2026-10-10-runtime-image-configuration.md`
- Create: `docs/superpowers/plans/2026-10-11-deployment-and-hardening.md`

**Interfaces:**
- Consumes: Approved specifications from Tasks 1–5 and Checkpoint A sign-off.
- Produces: Updated downstream runtime implementation plan aligned with Gate 0C/0D/0G decisions, plus a dedicated deployment/hardening implementation plan.

- [x] **Step 1: Reconcile existing runtime implementation plan**
Update `docs/superpowers/plans/2026-10-10-runtime-image-configuration.md`:
  - Reconcile Task 4 (OpenCode bootstrap) with approved Gate 0G process isolation and dynamic password lifecycle.
  - Reconcile Task 5 (Repository sync) with approved Gate 0C transfer architecture.
  - Reconcile Task 4/9 network configuration with approved Gate 0D egress controls.
  - Do NOT rewrite or duplicate the 9-task breakdown; update interface definitions, constraints, and test steps to match approved specs.

- [x] **Step 2: Commission bounded deployment and hardening plan**
Draft `docs/superpowers/plans/2026-10-11-deployment-and-hardening.md` to govern Gate 0E (codebase decoupling: removal of `runOnHost`, `dependencies.go` host calls, `http.sslVerify=false` fallbacks) and Gate 0F (Compose cleanup, secret generation script, startup validation, confirmed LICENSE file addition).

- [x] **Step 3: Verify plan decoupling and interfaces**
Confirm runtime plan and deployment plan have distinct file boundaries and no circular dependencies. Note: The plans share runtime runner and server files (`internal/runner/task_runner.go`, `cmd/server/main.go`) and require ordered sequential ownership rather than globally disjoint isolation.

- [ ] **Step 4: Commit reconciled implementation plans**
```bash
git add docs/superpowers/plans/2026-10-10-runtime-image-configuration.md docs/superpowers/plans/2026-10-11-deployment-and-hardening.md
git commit -m "docs: reconcile runtime plan and commission deployment hardening plan"
```

---

### Checkpoint B: Downstream Implementation Plans Approval (STATUS: PENDING AUTHORIZATION & RESOLUTION)

> **CRITICAL HARD STOP:** Product code and Compose implementation remains strictly locked. Approval of planning roadmaps or user selection of an execution approach ("use subagents") does not constitute authorization to execute product code or make host changes. Prior to starting Task 7, both reconciled downstream implementation plans (`2026-10-10-runtime-image-configuration.md` and `2026-10-11-deployment-and-hardening.md`) must be formally reviewed and approved by the user and security reviewer, and architectural ambiguities must be cleared. Checkpoint B remains **PENDING**.

- [ ] **Review Deliverables:**
  1. Reconciled downstream runtime plan (`docs/superpowers/plans/2026-10-10-runtime-image-configuration.md`).
  2. Commissioned deployment and hardening plan (`docs/superpowers/plans/2026-10-11-deployment-and-hardening.md`).
- [ ] **Obtain Formal Downstream Plan Approval:** Formal security review and explicit authorization required before implementation execution. User selected subagent execution approach, but formal plan authorization remains pending.

---

### Task 7: Execute Implementation & Hardening via Downstream Plans (Gate 0E, 0F, 0C, 0G)

**Files:**
- Modify: Specified in downstream plans (`docs/superpowers/plans/2026-10-10-runtime-image-configuration.md` and `2026-10-11-deployment-and-hardening.md`).
- Scope: Implementation of containerized runtime, Git transfer, egress filtering, secret validation, and code decoupling.

**Interfaces:**
- Consumes: Reconciled and approved downstream plans from Task 6 and Checkpoint B.
- Produces: Fully implemented, hardened codebase and production deployment assets.

- [ ] **Step 1: Execute Gate 0E Codebase Decoupling (Deployment & Hardening Plan)**
  - Excise `runOnHost` from `internal/runner/task_runner.go`.
  - Remove host package execution (`npm`, `go`) from `internal/runner/dependencies.go`.
  - Eliminate all `-c http.sslVerify=false` and `GIT_SSL_NO_VERIFY` fallbacks from `internal/git/sync.go` and `internal/git/push.go`.
  - Verify unit tests pass in isolated container environment.

- [ ] **Step 2: Execute Gate 0F Production Compose & Secret Hardening**
  - Commit repository `LICENSE` matching the confirmed terms approved by user/maintainer during Gate 0F.
  - Decouple Traefik from Docker socket (file provider or proxy filter).
  - Implement `SECRET_ENCRYPTION_KEY` validation strictly in accordance with the contract approved in Gate 0F (verifying exact length/format and rejecting known defaults; avoiding subjective runtime entropy measurement).
  - Implement default password startup guard (`exit 1` on default secrets).
  - Create secret generation bootstrap script.

- [ ] **Step 3: Execute Runtime Plan Tasks (Tasks 1 through 9)**
  - Implement runtime data models and catalog (Task 1).
  - Implement multi-repository manifest union resolver (Task 2).
  - Implement deterministic Docker image builder (Task 3).
  - Implement sandbox lifecycle, egress enforcement, and OpenCode server runner (Task 4).
  - Implement in-container worktree layout and Gate 0C credential-safe sync (Task 5).
  - Route task runner through in-container session with zero host fallback (Task 6).
  - Implement 24-hour idle lifecycle coordinator (Task 7).
  - Implement runtime management API, RBAC, and web UI settings (Task 8).
  - **Execute Isolated Full Verification Suite (Task 9):** Execute the runtime plan's complete verification battery (`make check`, isolated backend unit/integration tests, and frontend TypeScript/lint/typecheck suites) inside the approved isolated build container before proceeding to clean-host acceptance.

- [ ] **Step 4: Commit implementation units per downstream plans**
Follow commit discipline defined in downstream plans.

---

### Task 8: Clean-Host End-to-End Acceptance Verification

**Files:**
- Create: `docs/superpowers/verification/2026-10-11-self-hosting-clean-host-acceptance-report.md`

**Interfaces:**
- Consumes: Hardened codebase, production Compose assets, and fresh Ubuntu 24.04 LTS host.
- Produces: Durable verification report validating all positive and negative acceptance criteria from Section 8 of the readiness spec.

- [ ] **Step 1: Obtain pre-provisioned host or request provisioning authorization**
**Mandatory Stop Rule:** Acceptance verification requires an isolated, clean native Linux `amd64` Docker environment (systemd Docker CE 26+/27+/29+ non-Snap, Docker Compose v2.20+).
- Use an existing pre-provisioned, user-authorized test host if one is already available.
- Provisioning a new VM, cloud instance, or hardware host, or modifying any host packages, system services, or the active Docker context, **requires explicit user authorization requested at execution time**.
- Approval of this implementation plan does **NOT** authorize provisioning environments or mutating host configurations.
- Confirm the target environment has zero preexisting ATM artifacts, worktrees, or data volumes.

- [ ] **Step 2: Execute Setup and Secret Bootstrap Acceptance**
Run secret generation script. Run `docker compose up` with default secrets; verify stack exits immediately with error code 1. Run `docker compose up -d` with generated secrets; verify all core containers become healthy within 60 seconds.

- [ ] **Step 3: Execute Positive End-to-End Task Execution**
Confirm that the downstream runtime plan's full verification suite (Task 9: `make check`, isolated backend tests, and frontend typecheck/lint) has completed and passed before/alongside the host deployment.
Configure a test project in the UI. Dispatch an end-to-end multi-turn coding task requiring dependency installation. Verify:
  - Task completes successfully inside `atm-project-<id>`.
  - Zero files created in host directories (`/srv`, `/tmp`, home directories).
  - ANSI terminal stream lives over WebSocket without credentials.
  - Code changes are committed to project volume.

- [ ] **Step 4: Execute Negative Fail-Closed Security Tests**
Run negative security battery:
  - **Host Fallback Denial:** Force container launch failure (e.g. invalid image tag); verify task status transitions to `failed` and zero host processes spawn.
  - **NNP Probe:** Run `docker exec atm-project-<id> grep "^NoNewPrivs:" /proc/self/status`; verify output is `NoNewPrivs: 1`.
  - **Egress Confinement:** From inside sandbox, probe `169.254.169.254`, `172.17.0.1:8080`, and host SSH; verify connection timed out or unreachable.
  - **Git TLS Verification:** Attempt clone from invalid/self-signed remote; verify operation fails without unverified SSL retry.
  - **Credential Absence:** Inspect sandbox `env` and `ps aux`; verify GitHub tokens, database passwords, and model provider API keys are completely absent from untrusted processes.

- [ ] **Step 5: Record acceptance report**
Document all commands, timestamps, outputs, and exit codes in `docs/superpowers/verification/2026-10-11-self-hosting-clean-host-acceptance-report.md`.

- [ ] **Step 6: Commit clean-host acceptance report**
```bash
git add docs/superpowers/verification/2026-10-11-self-hosting-clean-host-acceptance-report.md
git commit -m "docs: record clean-host self-hosting acceptance verification report"
```

---

## File Boundary Inventory

| File Path | Responsibility | Action |
|---|---|---|
| `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md` | Primary readiness target and release gate specification | Updated header status only (Planning phase) |
| `docs/superpowers/plans/2026-10-11-self-hosting-readiness.md` | Master gated implementation roadmap (this document) | Created |
| `docs/superpowers/verification/2026-10-11-runtime-qualification-gates-0a-0b.md` | Durable tracked qualification evidence report for Gates 0A and 0B | Created in Task 1 |
| `docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md` | Runtime container architecture and manifest schema | Modified in Task 1 (Reconciliation) |
| `docs/superpowers/plans/2026-10-10-runtime-image-configuration.md` | Downstream runtime implementation plan | Modified in Task 1 & Task 6 |
| `docs/superpowers/specs/2026-10-11-gate-0c-git-transfer-design.md` | Credential-safe Git data transfer specification | Created in Task 2 |
| `docs/superpowers/specs/2026-10-11-gate-0d-egress-confinement-design.md` | Default-deny sandbox egress specification | Created in Task 3 |
| `docs/superpowers/specs/2026-10-11-gate-0g-model-credential-isolation-design.md` | Model credential protection and OpenCode password design | Created in Task 4 |
| `docs/superpowers/specs/2026-10-11-gate-0f-production-deployment-design.md` | Production Compose, secrets bootstrap, and licensing spec | Created in Task 5 |
| `docs/superpowers/plans/2026-10-11-deployment-and-hardening.md` | Downstream deployment and codebase decoupling implementation plan | Created in Task 6 |
| `docs/superpowers/verification/2026-10-11-self-hosting-clean-host-acceptance-report.md` | Clean-host acceptance verification report | Created in Task 8 |

---

## Unresolved Risks and Decision Dependencies

1. **Gate 0C Data Transfer Complexity:** Transferring Git bare repositories into named Docker volumes without host bind mounts requires evaluating control-plane sidecar streaming vs Docker archive API vs in-container credential helper proxy.
2. **Gate 0D Container Egress vs Language Tooling:** Implementing default-deny egress via forward proxies requires ensuring all container language runtimes (Go proxy, npm, pip, maven) correctly respect proxy environment variables and corporate CA certs if TLS inspection is used.
3. **Gate 0G Subprocess Isolation vs OpenCode Architecture:** If OpenCode runs commands directly in subshells without environment stripping, model API keys could leak to untrusted task build scripts. If reverse-proxied through the control plane, streaming latency and WebSocket protocol compatibility must be verified.
4. **Environment Verification Deferral:** Acceptance testing requires a clean, non-Snap Linux Docker host. Probes cannot be completed inside developer machines running Snap Docker or restricted environments without dedicated VM isolation.
