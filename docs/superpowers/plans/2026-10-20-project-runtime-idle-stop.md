# Project Runtime Idle Shutdown Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically stop project runtime containers after 15 minutes of inactivity while protecting active task runs and preserving persistent project state.

**Architecture:** Parse one validated timeout setting in application config and pass it into both project-runtime manager construction paths. The sandbox manager owns per-project timers, recent activity, and task activity leases; task execution acquires/releases a lease around `RunTask`. Automatic stop reuses the existing graceful stop and gateway cleanup path.

**Tech Stack:** Go, Docker Engine client, Docker Compose, Go `time.Timer`.

**Spec:** `docs/superpowers/specs/2026-10-20-project-runtime-idle-stop.md`

## Global Constraints

- Default `ATM_PROJECT_RUNTIME_IDLE_TIMEOUT` is `15m`; `0s` disables it; invalid, negative, and positive values below `30s` fail configuration loading.
- Never automatically stop a runtime while its active task lease count is nonzero.
- Do not remove containers, volumes, worktrees, task data, or OpenCode state.
- Preserve existing MCP settings and `MAX_CONCURRENT_TASKS` behavior.
- Do not change host processes or unrelated user data.

## Review Focus

- Timer expiry racing with task start: manager lock and active-lease tests must show the container is not stopped.
- Task error/cancellation paths: `RunTask` must release its lease via `defer`.
- Activity arriving near expiry: a route access must postpone stop by a full timeout interval.
- Explicit stop followed by timer callback: the container must not receive a duplicate stop.
- Docker stop failure: preserve route state and retry rather than reporting a stopped runtime.
- Readiness probe still running: do not arm the idle timer until route registration after readiness succeeds.

---

### Task 1: Configure and wire the idle timeout

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Modify: `internal/runner/project_runtime.go`
- Modify: `internal/runner/project_sandbox_manager.go`
- Modify: `internal/server/execution/runtime.go`
- Test: `internal/runner/project_runtime_internal_test.go`
- Test: `internal/runner/project_runtime_live_test.go`
- Modify: `docker-compose.yml`
- Modify: `docker-compose.prod.yml`
- Modify: `.env.example`

**Interfaces:**
- `config.Config.ProjectRuntimeIdleTimeout` is a `time.Duration` loaded from `ATM_PROJECT_RUNTIME_IDLE_TIMEOUT`.
- `DevHostAuthOptions.IdleTimeout` carries the same duration into the host-auth manager.
- `ProjectRuntimeFromEnv` receives the validated duration and applies it to the gateway-backed manager; its internal and live test callers pass an explicit duration.
- Existing public manager constructors used by isolated tests retain idle shutdown disabled unless an idle timeout is explicitly supplied through the application construction path.

- [ ] **Step 1: Add configuration tests for default, disabled, valid, and invalid values**

Add tests in `internal/config/config_test.go` that assert the default is `15*time.Minute`, `ATM_PROJECT_RUNTIME_IDLE_TIMEOUT=0s` produces zero, `45s` parses to 45 seconds, and `29s`, `-1s`, or `not-a-duration` returns an error.

Run: `go test ./internal/config -run 'TestLoad.*ProjectRuntimeIdleTimeout' -count=1`
Expected: FAIL because the setting is not yet parsed.

- [ ] **Step 2: Parse the setting and wire it into both runtime construction paths**

Add the `time.Duration` field and parse it with `time.ParseDuration`; accept zero as disabled, reject errors/negative values, and reject positive durations below 30 seconds. Add `ATM_PROJECT_RUNTIME_IDLE_TIMEOUT: ${ATM_PROJECT_RUNTIME_IDLE_TIMEOUT:-15m}` to the control-plane environment in both `docker-compose.yml` and `docker-compose.prod.yml`, and document it in `.env.example`. Pass the loaded duration to `ProjectRuntimeFromEnv` and `NewProjectSandboxManagerWithDevHostAuth` via `DevHostAuthOptions.IdleTimeout`. Update the internal and live test call sites of `ProjectRuntimeFromEnv` with explicit timeout values.

Use this parse contract:

```go
idleTimeout, err := time.ParseDuration(getEnv("ATM_PROJECT_RUNTIME_IDLE_TIMEOUT", "15m"))
if err != nil || idleTimeout < 0 || (idleTimeout > 0 && idleTimeout < 30*time.Second) {
    return nil, fmt.Errorf("invalid ATM_PROJECT_RUNTIME_IDLE_TIMEOUT: expected 0s or at least 30s")
}
```

- [ ] **Step 3: Verify configuration and runtime wiring**

Run: `go test ./internal/config ./internal/runner -run 'ProjectRuntimeFromEnv|ProjectRuntimeIdleTimeout' -count=1`
Expected: PASS; `ProjectRuntimeFromEnv` constructs a manager with the supplied duration and existing default constructors remain disabled.

Run: `docker compose config --quiet`
Expected: exit status 0 with no Compose parse errors.

Run: `docker compose -f docker-compose.yml -f docker-compose.prod.yml config --quiet`
Expected: exit status 0 with no production Compose parse errors.

### Task 2: Add race-safe manager timers and activity leases

**Files:**
- Modify: `internal/runner/project_sandbox_manager.go`
- Test: `internal/runner/project_sandbox_manager_test.go` or a focused new `internal/runner/project_sandbox_idle_test.go`
- Modify: `internal/runner/project_sandbox_quarantine_internal_test.go` only if the existing stop-path expectations require adjustment

**Interfaces:**
- Add an optional `ProjectRuntimeActivityManager` interface with `BeginProjectRuntimeActivity(projectID string) func()`; `AgentTaskRunner` uses it without forcing unrelated manager fakes to implement it.
- The concrete project sandbox manager stores per-project last activity, active lease counts, and idle timers.
- Starting a tracked runtime and successfully retrieving its route refreshes its timer.
- An idle timer calls the internal stop path under the manager lock after rechecking route identity, elapsed idle time, and active leases.

- [ ] **Step 1: Write manager tests for expiry, route activity refresh, disabled timeout, and stop failure**

Use the existing fake Docker client with short manager-injected timeouts (configuration's 30-second minimum does not apply to package test fixtures). Assert that an inactive runtime is stopped once, `GetProjectRoute` pushes the deadline out, timeout zero never stops, a task lease blocks stop, and a failed Docker stop keeps the route/gateway lease and schedules one delayed retry.

Run: `go test ./internal/runner -run 'TestProjectSandboxManager_Idle' -count=1`
Expected: FAIL because idle tracking is not implemented.

- [ ] **Step 2: Implement synchronized idle tracking and cleanup**

Add the timeout and per-project timer/activity state to `projectSandboxManager`. Use one lock for lease counts, route state, and timer callbacks. Arm the timer only after startup and readiness succeed. Capture the exact container ID and route generation in each timer callback; under the lock, abort unless the cached route still matches both and its active lease count is zero. On success, release the gateway lease, remove the cached route, and clear timer state. On Docker errors other than not-found, log the error, preserve route and gateway lease state, and retry after one minute with only one retry timer per project. Keep the retry delay in a manager field initialized to one minute so package tests can use a short delay. `StopProjectContainer` cancels timer state before attempting stop; only remove the route/release the lease on success or not-found.

Lease behavior must be idempotent:

```go
release := manager.BeginProjectRuntimeActivity(projectID)
defer release()
```

- [ ] **Step 3: Run manager tests including race detection**

Run: `go test ./internal/runner -run 'TestProjectSandboxManager_(Idle|Stop)' -count=1`
Expected: PASS.

Run: `go test -race ./internal/runner -run 'TestProjectSandboxManager_Idle' -count=1`
Expected: PASS with no timer/map races.

### Task 3: Protect task runs with leases and verify end-to-end behavior

**Files:**
- Modify: `internal/runner/task_runner.go`
- Test: `internal/runner/task_runner_test.go`
- Test: `internal/runner/project_task_run_internal_test.go` if lease behavior needs project-run-specific coverage
- Modify: `internal/server/execution/runtime.go` only if final constructor wiring from Task 1 requires adjustment

**Interfaces:**
- `RunTask` begins project-runtime activity before invoking project-container execution and defers lease release across success, failure, and context cancellation.
- A sandbox manager that does not implement the optional activity interface keeps prior behavior.

- [ ] **Step 1: Add task-runner tests for lease acquisition and release**

Use a fake manager implementing `ProjectRuntimeActivityManager` and assert one acquire/release for a successful run, an erroring run, and a canceled run. Also assert a manager without the optional interface still executes without panic.

Run: `go test ./internal/runner -run 'TestRunTask.*RuntimeActivity' -count=1`
Expected: FAIL because `RunTask` does not yet acquire the lease.

- [ ] **Step 2: Wrap task execution in a deferred activity lease**

Immediately after the nil-task check at the start of `RunTask`—before task setup, repository reads, git transfer, or runtime work—type-assert the optional interface, acquire the lease for `task.ProjectID`, and defer its release. Keep the existing `RunTask` result bookkeeping and error behavior unchanged.

- [ ] **Step 3: Run focused and full verification**

Run: `go test ./internal/config ./internal/runner ./internal/server/execution -count=1`
Expected: PASS.

Run: `go test -race ./internal/runner -count=1`
Expected: PASS with no data races.

Run: `go test ./...`
Expected: PASS.

Run: `docker compose config --quiet && docker compose -f docker-compose.yml -f docker-compose.prod.yml config --quiet`
Expected: exit status 0 for both Compose configurations.

- [ ] **Step 4: Perform a bounded local runtime check after approval**

Set `ATM_PROJECT_RUNTIME_IDLE_TIMEOUT=30s` for a local control-plane rebuild, start the Core Workspace runtime, and verify it stops after 30 idle seconds. Confirm project state volumes remain present and the runtime can be started again. Active-task protection is covered by the focused unit and race tests; do not create or delete tasks for this live check. Record `docker stats --no-stream` before and after; do not flush Redis or delete task/project data.
