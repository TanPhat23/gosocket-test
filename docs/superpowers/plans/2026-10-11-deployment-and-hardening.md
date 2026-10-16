# Deployment Hardening & Production Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement production self-hosting deployment hardening for Agent Task Manager (ATM) on single-host `linux/amd64` Docker Compose environments. Eliminate Traefik public Docker socket mounts via file-based dynamic configuration, remove host-level code execution fallbacks (`runOnHost`, `dependencies.go`), enforce fail-closed startup validation against weak/default secrets, add the approved MIT repository license, build an atomic secret bootstrap CLI writing restricted-mode `.env` and service configs, and qualify Gate 0F production deployment via objective probes.

**Architecture:** Segmented dual-bridge network topology (`agent_ingress` public-facing, `agent_backend` internal only). Traefik runs with zero Docker daemon socket access, routing traffic strictly through file-based dynamic configuration with explicit integer router priorities. PostgreSQL 18 mounts the official version-specific data volume (`/var/lib/postgresql`). Redis authentication is supplied via configuration file mounts and healthchecked via an isolated script without exposing passwords in inspectable Compose command lines. Dex runs with in-memory storage for v1 with development passwords disabled and upstream connectors configurable. The control plane loads credentials via a fail-closed preflight pipeline and decodes 256-bit AES encryption keys across 64-hex and 32-literal formats.

**Tech Stack:** Go 1.27, Docker Compose v2, Traefik v3.x, PostgreSQL 18 Alpine, Redis 7 Alpine, Dex IDP v2.41.x, Bash 5.x.

**Spec:** `docs/superpowers/specs/2026-10-11-gate-0f-production-deployment-design.md` and `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md`

---

## Global Constraints

- **Platform & Host Baseline:** Target platform is native Docker Engine on single-host Linux (`linux/amd64`). Host firewalling (ports 80/443 open to clients; database, Redis, and internal APIs dropped) is managed by the host administrator.
- **Root-Equivalent Socket Isolation:** Project sandbox containers and Traefik never mount `/var/run/docker.sock`. Only the trusted ATM control-plane container mounts the Docker socket for orchestration. Public ingress proxy compromise must never grant Docker daemon access.
- **Strict File Permissions & Non-World-Readable Modes:** The generated master `.env` file is restricted strictly to mode `0600` owned by the operator user. Service configurations (`deploy/dex/config.prod.yml`, `deploy/redis/redis.conf`) are restricted to mode `0640` inside parent directories restricted to mode `0750` with group ownership assigned to container service GIDs (`1001` for Dex, `999` for Redis). Master secrets must never be placed in world-readable files.
- **Atomic No-Clobber Publication & Set Rollback:** Generating configurations must pre-check destinations fail-closed, stage temporary files under an isolated private directory (`mode 0700`), verify parent directory ownership and permissions, use atomic no-replace (`ln`) or verified atomic replace (`mv`), and restore previous backups upon any publication failure.
- **Zero Secret Exposure Invariant:** Secrets must never appear in command-line arguments (`argv`), inspectable Compose `command` or `healthcheck` strings, process tables (`/proc`), container stdout/stderr logs, or error messages.
- **Fail-Closed Default Secret Rejection:** Control-plane startup halts immediately (exit code 1) in production or strict preflight mode if known insecure development credentials (e.g. `postgres`, `secretredis`, default encryption keys, dev session secrets) are detected.
- **Dual-Format Encryption Key Representation:** `SECRET_ENCRYPTION_KEY` accepts both standard 64-hex strings (decoded to 32 raw bytes) and legacy 32-byte literal ASCII strings, reporting non-secret diagnostic errors and normalizing to 32 bytes before evaluating weak patterns.
- **Zero Host Execution Fallback (Gate 0E):** All task execution occurs within container sandboxes. `runOnHost` in `internal/runner/task_runner.go`, host compilation calls in `internal/runner/dependencies.go`, and TLS-disabling fallback switches (`GIT_SSL_NO_VERIFY=true`, `http.sslVerify=false`) are completely eliminated.
- **Approved Licensing:** Standard MIT License is codified in a tracked repository `LICENSE` file.
- **Decoupled Implementation Boundaries:** This deployment and hardening plan governs infrastructure, configuration, and host-decoupling tasks. Container sandbox management, runtime image building, and in-container Git sync are governed independently by `docs/superpowers/plans/2026-10-10-runtime-image-configuration.md`.

---

## Review Focus

1. **Secret Non-Exposure Across Interfaces:** Verify that no secret values appear in CLI arguments, `docker inspect` outputs, logs, or error strings across Compose, Redis healthchecks, and key rotation CLI.
2. **Fail-Closed Startup Preflight:** Verify that `ValidateRawProductionConfig` deterministically rejects all specified banned passwords, weak encryption keys, and short session secrets.
3. **Ingress Decoupling & Router Priorities:** Verify that Traefik has zero socket mounts and that dynamic routing declares explicit integer priorities (`100` for path routers, `10` for root domain).
4. **Service Container UID/GID Compatibility:** Verify that non-root container users (Dex `1001`, Redis `999`) can traverse directories and read configuration files under mode `0640` / `0750` without permission errors.
5. **Atomic Publication & Rollback Safety:** Verify that bootstrap aborts fail-closed on symlinks or pre-existing targets without `--force`, and cleanly restores backups if an injected failure occurs during publication.

---

## Task Breakdown

### Task 1: Repository LICENSE File Addition & Licensing Resolution

**Goal:** Formally add the approved MIT License to the repository, resolving the licensing decision gate and aligning repository documentation with the README badge.

**Files:**
- Create: `LICENSE`
- Modify: `README.md` (verify badge link)

- [ ] **Step 1: Write test to verify LICENSE file presence and MIT content** (Draft test implemented, verification pending / isolated qualification required)
  - Create test script `tests/license_test.sh`:
    - Test 1: File `LICENSE` exists in repository root.
    - Test 2: File contains standard MIT copyright notice: `Permission is hereby granted, free of charge`.
- [ ] **Step 2: Run test to verify failure**
  - Command: `bash tests/license_test.sh`
  - Expected: FAIL (`LICENSE: No such file or directory`)
- [ ] **Step 3: Create LICENSE file** (Draft LICENSE created matching user choice, verification pending)
  - Create `LICENSE` with standard MIT License text and current year copyright notice.
- [ ] **Step 4: Run test to verify green** (Verification pending; prior bare host test run not accepted as isolated qualification)
  - Command: `bash tests/license_test.sh`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add LICENSE tests/license_test.sh && git commit -m "docs: add MIT license file"`

---

### Task 2: Codebase Decoupling & Host Execution Removal (Gate 0E)

**Goal:** Eradicate all control-plane host execution fallbacks, eliminate host dependency compilation from `dependencies.go`, and remove insecure TLS verification disablement.

**Files:**
- Modify: `internal/runner/task_runner.go`
- Modify: `internal/runner/task_runner_test.go`
- Modify: `internal/runner/dependencies.go`
- Modify: `internal/runner/dependencies_test.go`
- Modify: `internal/git/git.go`

- [ ] **Step 1: Write unit tests verifying zero host execution and TLS enforcement** (Draft tests implemented, verification pending)
  - In `internal/runner/task_runner_test.go`:
    - Test 1: When Docker sandbox container creation fails, `AgentTaskRunner` fails closed immediately with an error and NEVER falls back to host execution (`runOnHost`).
  - In `internal/runner/dependencies_test.go`:
    - Test 2: Verify host-level `exec.Command("npm", "install")` and `exec.Command("go", "mod", "download")` functions are removed from control-plane runner.
  - In `internal/git/git_test.go`:
    - Test 3: Verify Git clone operations reject `http.sslVerify=false` and do not accept `GIT_SSL_NO_VERIFY=true`.
- [ ] **Step 2: Run focused tests to verify failure**
  - Command: `go test -v ./internal/runner -run TestHostFallbackElimination`
  - Command: `go test -v ./internal/git -run TestTLSEnforcement`
  - Expected: FAIL
- [ ] **Step 3: Implement codebase decoupling** (Draft implemented, verification pending; Blocked: task_runner retains host git and runInDocker paths; extra blank lines reported by git diff --check in task_runner_test.go)
  - In `internal/runner/task_runner.go`: Delete `runOnHost` method and all fallback invocations.
  - In `internal/runner/dependencies.go`: Remove host dependency execution; route dependency inspection to container sandbox contracts.
  - In `internal/git/git.go`: Remove `-c http.sslVerify=false` flags and reject insecure environment variables.
- [ ] **Step 4: Run focused tests to verify green** (Verification pending; prior bare host run not accepted)
  - Command: `go test -v ./internal/runner -run TestHostFallbackElimination`
  - Command: `go test -v ./internal/git -run TestTLSEnforcement`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add internal/runner/ internal/git/ && git commit -m "feat(security): eliminate host execution fallbacks and enforce TLS verification"`

---

### Task 3: Production Configuration Loader & Fail-Closed Startup Validation (Gate 0F)

**Goal:** Modernize application configuration loading with dual-format encryption key decoding (64-hex and 32-literal), strict non-secret error messages, and fail-closed preflight validation rejecting known default passwords in production.

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Write unit tests for configuration loader and validation pipeline** (Draft tests implemented, verification pending; git diff --check reports extra blank line at EOF in config_test.go)
  - In `internal/config/config_test.go`:
    - Test 1: Valid 64-character hex `SECRET_ENCRYPTION_KEY` decodes to exact 32 raw bytes.
    - Test 2: Valid 32-byte literal key converts to 32 raw bytes.
    - Test 3: Empty encryption key fails with exact message: `"SECRET_ENCRYPTION_KEY is required and cannot be empty"`.
    - Test 4: Invalid length/format fails with sanitized message without printing the key.
    - Test 5: Known default encryption keys (`0123...`, `0000...`, `1111...`, `1234...`) fail closed.
    - Test 6: Insecure `POSTGRES_PASSWORD` (`postgres`, empty, `password`, `admin`, `root`, `secret`) fails closed in production.
    - Test 7: Insecure `REDIS_PASSWORD` (`secretredis`, empty, `redis`, `password`, `admin`) fails closed in production.
    - Test 8: Short `SESSION_SECRET` (< 32 characters) or default secret fails closed.
    - Test 9: `PREFLIGHT_STRICT=true` enforces validation even when `APP_ENV != production`.
- [ ] **Step 2: Run focused tests to verify failure**
  - Command: `go test -v ./internal/config -run TestValidateProductionSecrets`
  - Expected: FAIL
- [ ] **Step 3: Implement validation and normalization pipeline** (Draft implemented, verification pending; cmd/server/main.go lacks project sandbox manager wiring)
  - Implement `RawEnvironmentConfig`, `ValidateRawProductionConfig`, `MasterEncryptionKey`, and `ProposedConfig` in `internal/config/config.go`.
  - Wire preflight validation into `cmd/server/main.go` immediately after configuration loading.
- [ ] **Step 4: Run focused tests to verify green** (Verification pending; prior bare host run not accepted)
  - Command: `go test -v ./internal/config -run TestValidateProductionSecrets`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add internal/config/ cmd/server/main.go && git commit -m "feat(config): implement dual-format key decoding and fail-closed secret validation"`

---

### Task 4: Production Traefik Static Configuration & Ingress Decoupling (Gate 0F)

**Goal:** Decouple Traefik ingress from the Docker daemon socket by configuring pure file-based dynamic routing with explicit router priorities.

**Files:**
- Modify: `deploy/traefik/traefik.yml`
- Modify: `deploy/traefik/dynamic.yml`

- [ ] **Step 1: Write validation test for Traefik configuration files** (Draft test implemented, verification pending)
  - Create test script `tests/traefik_config_test.sh`:
    - Test 1: `deploy/traefik/traefik.yml` completely omits `providers.docker`.
    - Test 2: `deploy/traefik/traefik.yml` declares `providers.file.filename: /etc/traefik/dynamic.yml`.
    - Test 3: `deploy/traefik/traefik.yml` defines named entrypoints `web` (:80 with HTTPS redirect) and `websecure` (:443).
    - Test 4: `deploy/traefik/dynamic.yml` declares explicit router priorities (`priority: 100` for control-plane and dex; `priority: 10` for web-ui).
    - Test 5: Verify both YAML files parse cleanly with PyYAML.
- [ ] **Step 2: Run test to verify failure**
  - Command: `bash tests/traefik_config_test.sh`
  - Expected: FAIL
- [ ] **Step 3: Implement Traefik configuration updates** (Draft implemented, verification pending)
  - Update `deploy/traefik/traefik.yml` according to spec §3.2.
  - Update `deploy/traefik/dynamic.yml` according to spec §3.4.
- [ ] **Step 4: Run test to verify green** (Verification pending; prior bare host run not accepted)
  - Command: `bash tests/traefik_config_test.sh`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add deploy/traefik/ tests/traefik_config_test.sh && git commit -m "feat(traefik): decouple ingress from docker socket and configure file provider"`

---

### Task 5: Production Service Hardening: Redis Auth & Dex Templates (Gate 0F)

**Goal:** Author production configuration templates for Redis and Dex, implementing fail-closed Redis healthchecks and disabling development passwords in Dex.

**Files:**
- Create: `deploy/redis/redis.conf.template`
- Create: `deploy/redis/redis-healthcheck.sh`
- Create: `deploy/dex/config.prod.yml.template`

- [ ] **Step 1: Write validation test for Redis and Dex templates** (Draft test implemented, verification pending)
  - Create test script `tests/service_hardening_test.sh`:
    - Test 1: `deploy/redis/redis-healthcheck.sh` uses portable POSIX character classes (`[[:space:]]`), verifies exactly 1 non-empty `requirepass` line, and pipes `redis-cli ping >/dev/null`.
    - Test 2: `deploy/dex/config.prod.yml.template` has `enablePasswordDB: false` and zero static passwords (`staticPasswords` omitted).
    - Test 3: `deploy/dex/config.prod.yml.template` configures `issuer: https://${DOMAIN}/dex` and `storage: type: memory`.
- [ ] **Step 2: Run test to verify failure**
  - Command: `bash tests/service_hardening_test.sh`
  - Expected: FAIL
- [ ] **Step 3: Implement templates and healthcheck script** (Draft implemented, verification pending)
  - Create `deploy/redis/redis.conf.template` with `requirepass ${REDIS_PASSWORD}`.
  - Create `deploy/redis/redis-healthcheck.sh` per spec §3.5 with executable permissions (`0755`).
  - Create `deploy/dex/config.prod.yml.template` per spec §3.5.
- [ ] **Step 4: Run test to verify green** (Verification pending; prior bare host run not accepted)
  - Command: `bash tests/service_hardening_test.sh`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add deploy/redis/ deploy/dex/ tests/service_hardening_test.sh && git commit -m "feat(deploy): author production redis and dex hardening templates"`

---

### Task 6: Secret Bootstrap Script Implementation (Gate 0F)

**Goal:** Implement `scripts/bootstrap-secrets.sh` to generate high-entropy secrets, validate operator inputs, enforce ancestor path safety, stage files in an isolated directory, and publish `.env` and service configurations atomically with rollback protection.

**Files:**
- Create: `scripts/bootstrap-secrets.sh`

- [ ] **Step 1: Write integration test suite for bootstrap script** (Draft test implemented, verification pending)
  - Create test script `tests/bootstrap_secrets_test.sh`:
    - Test 1: Generates `.env` with mode `0600` containing high-entropy random secrets.
    - Test 2: Renders `deploy/dex/config.prod.yml` (mode `0640`) and `deploy/redis/redis.conf` (mode `0640`) in directories with mode `0750`.
    - Test 3: Rejects newline/control characters in `DOMAIN` and `ACME_EMAIL` with generic errors.
    - Test 4: Rejects direct and ancestor symlinks fail-closed.
    - Test 5: Rejects group-writable ancestor directory (`chmod 0770 deploy`).
    - Test 6: Fails closed on pre-existing targets without `--force`.
    - Test 7: Simulates injected publish failure and verifies complete rollback of new files and restoration of backups.
    - Test 8: Verifies zero secrets are printed to stdout or stderr.
- [ ] **Step 2: Run test to verify failure**
  - Command: `bash tests/bootstrap_secrets_test.sh`
  - Expected: FAIL (`scripts/bootstrap-secrets.sh: No such file or directory`)
- [ ] **Step 3: Implement bootstrap script** (Draft implemented, verification pending)
  - Implement `scripts/bootstrap-secrets.sh` according to spec §4.3 with mode `0755`.
- [ ] **Step 4: Run test to verify green** (Verification pending; prior bare host run not accepted)
  - Command: `bash tests/bootstrap_secrets_test.sh`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add scripts/bootstrap-secrets.sh tests/bootstrap_secrets_test.sh && git commit -m "feat(scripts): implement production secret bootstrap CLI with atomic rollback"`

---

### Task 7: Production Docker Compose Manifest (Gate 0F)

**Goal:** Author `docker-compose.prod.yml` codifying the hardened production topology with dual bridge networks, PostgreSQL 18 persistent volume mounts, Redis configuration file mounts, and zero Traefik Docker socket mounts.

**Files:**
- Create: `docker-compose.prod.yml`

- [ ] **Step 1: Write validation test for production Compose manifest** (Draft test implemented, verification pending)
  - Create test script `tests/compose_prod_test.sh`:
    - Test 1: Traefik service has zero mounts matching `docker.sock`.
    - Test 2: Networks include external `agent_ingress` and internal `agent_backend`.
    - Test 3: PostgreSQL 18 specifies `PGDATA: /var/lib/postgresql/18/docker` and volume `postgres_data:/var/lib/postgresql`.
    - Test 4: Redis mounts `./deploy/redis/redis.conf` and healthcheck script without inline passwords in `command` or `healthcheck`.
    - Test 5: Dex mounts `./deploy/dex/config.prod.yml`.
    - Test 6: Control plane mounts Docker socket for orchestration but connects to backend data stores on internal network.
    - Test 7: Validate YAML syntax using PyYAML.
- [ ] **Step 2: Run test to verify failure**
  - Command: `bash tests/compose_prod_test.sh`
  - Expected: FAIL (`docker-compose.prod.yml: No such file or directory`)
- [ ] **Step 3: Implement production Compose manifest** (Draft implemented, verification pending)
  - Create `docker-compose.prod.yml` according to spec §3.3.
- [ ] **Step 4: Run test to verify green** (Verification pending; prior bare host run not accepted)
  - Command: `bash tests/compose_prod_test.sh`
  - Expected: PASS
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add docker-compose.prod.yml tests/compose_prod_test.sh && git commit -m "feat(deploy): author hardened production docker compose manifest"`
- [ ] **Step 5: Commit scoped changes**
  - Command: `git add docker-compose.prod.yml tests/compose_prod_test.sh && git commit -m "feat(deploy): author hardened production docker compose manifest"`

---

### Task 8: End-to-End Verification & Qualification Probes Execution (Gate 0F)

**Goal:** Execute qualification probes 0F-1 through 0F-4 on a clean staging host, capture durable verification evidence, and qualify Gate 0F for self-hosting production readiness.

**Files:**
- Create: `docs/superpowers/verification/2026-10-13-gate-0f-qualification-evidence.md`

- [ ] **Step 1: Execute Probe 0F-1 (Traefik Docker Socket Decoupling)**
  - Launch staging Compose stack: `docker compose -f docker-compose.prod.yml up -d`
  - Verify Traefik mounts: confirm zero references to `/var/run/docker.sock`.
  - Verify container file absence: `docker exec agent-traefik ls -l /var/run/docker.sock` -> fails with `No such file or directory`.
  - Verify ingress routing: `curl -k https://localhost/` -> HTTP 200 from web-ui; `curl -k https://localhost/api/v1/health` -> HTTP 200 from control-plane.
- [ ] **Step 2: Execute Probe 0F-2 (Default Secret Rejection)**
  - Attempt startup with `SECRET_ENCRYPTION_KEY=01234567890123456789012345678901` and `APP_ENV=production`.
  - Verify process halts with exit code `1` and emits `"production preflight check failed: SECRET_ENCRYPTION_KEY uses an insecure default"`.
- [ ] **Step 3: Execute Probe 0F-3 (Bootstrap Script Atomic Creation, Symlink Defense & Rollback)**
  - Run full test suite in `tests/bootstrap_secrets_test.sh` covering clean run, no-clobber, symlink rejection, group-writable ancestor rejection, and rollback on injected failure.
- [ ] **Step 4: Execute Probe 0F-4 (Encryption Key Format & Normalization Validation)**
  - Execute unit tests for 64-hex decoding, 32-literal loading, invalid length rejection, and non-hex encoding failure.
- [ ] **Step 5: Record durable qualification evidence**
  - Compile command outputs and exit codes into `docs/superpowers/verification/2026-10-13-gate-0f-qualification-evidence.md`.
- [ ] **Step 6: Commit qualification evidence**
  - Command: `git add docs/superpowers/verification/2026-10-13-gate-0f-qualification-evidence.md && git commit -m "docs: record gate 0f qualification verification evidence"`

---

## Plan Self-Review Checklist

- [ ] **File Boundaries & Ownership (Clarified):** While this plan touches deployment, configuration, scripts, and documentation files, it shares runner and server files (`internal/runner/task_runner.go`, `cmd/server/main.go`) with the runtime plan. Rather than globally disjoint plans, execution requires coordinated, sequential ownership.
- [ ] **No Circular Dependencies:** Runtime plan and deployment plan interfaces are aligned; Checkpoints A and B remain pending before implementation execution.
- [x] **Explicit Error Contracts:** All failure paths specify non-secret error messages and fail-closed termination.
- [x] **Zero Secret Printing:** Every task respects the secret non-exposure invariant.
- [ ] **Test Steps Precede Implementation:** TDD structure defined in plan; implementation drafts exist in tree but isolated qualification remains pending.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-10-11-deployment-and-hardening.md`. Prior to executing tasks, both this plan and `docs/superpowers/plans/2026-10-10-runtime-image-configuration.md` must be reviewed and formally approved at **Checkpoint B**.
