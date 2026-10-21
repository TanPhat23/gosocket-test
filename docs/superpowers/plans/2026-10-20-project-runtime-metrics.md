# Project Runtime Metrics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an authenticated, low-cardinality Prometheus endpoint for current-process project runtime activity and resource usage.

**Architecture:** Add an optional manager telemetry snapshot that copies route/activity state under the manager lock and samples Docker stats after releasing it. A sampler in `internal/metrics` periodically aggregates the snapshots into a private Prometheus registry; `/metrics` serves only the latest completed sample and is mounted only when a sufficiently long bearer token is configured.

**Tech Stack:** Go, Fiber v3, Docker Engine client, Prometheus `client_golang` and `promhttp`, `crypto/subtle` for bearer-token verification.

**Spec:** `docs/superpowers/specs/2026-10-20-project-runtime-metrics.md`

## Global Constraints

- `ATM_METRICS_BEARER_TOKEN` is optional; when empty the `/metrics` route is unavailable.
- A configured metrics bearer token must be at least 32 bytes.
- The sample period is 30 seconds; Docker stats concurrency is bounded to 4 requests and each request has a 2-second timeout.
- Metrics contain aggregate values only; do not add project-, task-, user-, or container-specific labels.
- Sampling runs outside manager locks and HTTP scrapes perform no Docker API calls.
- Metrics are limited to runtimes tracked by this process; restart-time discovery is out of scope.
- Do not alter MCP configuration, task concurrency, container resource limits, or live container state.
- Preserve all pre-existing user changes, especially the uncommitted idle-shutdown implementation and its spec/plan.
- Do not commit or restart/rebuild any Docker service/container.

## File Structure

- `internal/config/config.go`, `internal/config/config_test.go`: optional bearer token loading and length validation.
- `.env.example`, `docker-compose.yml`, `docker-compose.prod.yml`: pass the optional token to the application.
- `internal/runner/project_sandbox_manager.go`: implement the optional telemetry source with a race-safe aggregate snapshot of tracked runtime IDs/activity, then sample Docker stats without holding the manager lock.
- `internal/runner/project_runtime_telemetry_internal_test.go` (new): assert counters and activity ages, and prove stats work occurs outside the manager lock.
- `internal/metrics/project_runtime.go` (new): own a private Prometheus registry, aggregate gauges/counters, and a cancellable 30-second sampler with bounded Docker work.
- `internal/metrics/project_runtime_test.go` (new): fake telemetry source tests for initial sample, refresh, partial stats failure, cancellation, and bounded aggregation.
- `internal/api/server.go`: accept an optional `http.Handler` and mount it at `/metrics` only when configured.
- `internal/api/metrics_test.go` (new): verify route disabled by default, correct bearer token succeeds, missing/wrong token returns 401, and exposition contains no identifiers.
- `cmd/server/main.go`: start the sampler only when metrics are enabled and stop it on shutdown; pass its handler to `api.NewServer`.

## Review Focus

- Empty and too-short secrets must not accidentally expose an unauthenticated route; cover config and HTTP behavior.
- A wrong or absent bearer credential must not leak any exposition bytes; test both responses.
- Docker stats failures must not turn partial data into an apparently complete sample; test sampled-container coverage and failure counters.
- A slow stats call must not block route/task activity or HTTP scrapes; test the sampler timeout and manager lock availability.
- Cancellation during sampling must stop the ticker and workers without goroutine leaks; test cancellation with a blocked fake source.

---

### Task 1: Add Optional Metrics Secret Configuration

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Modify: `.env.example`, `docker-compose.yml`, `docker-compose.prod.yml`

**Interfaces:**
- Add `Config.MetricsBearerToken string` loaded from `ATM_METRICS_BEARER_TOKEN`.
- Empty value disables the metrics endpoint. Non-empty values shorter than 32 bytes make `config.Load` return an error.

- [ ] Add table-driven tests for empty, exactly 32-byte, and too-short values using `t.Setenv`.
- [ ] Run `go test ./internal/config -run 'TestLoad.*MetricsBearerToken' -count=1` and confirm the validation cases fail before implementation.
- [ ] Load and validate the optional environment value; do not include its contents in errors.
- [ ] Pass `ATM_METRICS_BEARER_TOKEN: ${ATM_METRICS_BEARER_TOKEN:-}` through both Compose application services and document blank-by-default configuration in `.env.example`.
- [ ] Rerun the targeted config test and `git diff --check`.

### Task 2: Add Manager Runtime Telemetry Snapshots

**Files:**
- Modify: `internal/runner/project_sandbox_manager.go`
- Test: `internal/runner/project_sandbox_idle_test.go` or a new internal-package manager test

**Interfaces:**
- Add `runner.ProjectRuntimeTelemetrySnapshot` with fields `TrackedContainers int`, `ActiveTaskLeases int`, `PendingStopRetries int`, `MemoryBytes uint64`, `MemoryLimitBytes uint64`, `CPUPercent float64`, `MaxIdleAge time.Duration`, `StatsSampledContainers int`, and `StatsSampleFailures int`.
- Add `runner.ProjectRuntimeTelemetrySource` with method `CollectProjectRuntimeTelemetry(ctx context.Context) ProjectRuntimeTelemetrySnapshot`.
- Snapshot manager-owned counters and copy the tracked container IDs while holding `m.mu`; perform all Docker stats requests after releasing `m.mu`.
- Sample `ContainerStatsOneShot` with at most four workers and a two-second per-container timeout; aggregate stats only for successful samples and record sampled/failure counts.

- [ ] Add tests that populate fake routes/activity state and assert the snapshot counts and maximum idle age without exposing project IDs.
- [ ] Add a fake stats reader that blocks until timeout; concurrently call a manager route/activity method and prove it is not blocked on the telemetry lock.
- [ ] Add tests for unsupported stats readers and partial Docker stats errors; assert tracked counts remain accurate and failures are counted.
- [ ] Run `go test ./internal/runner -run 'Test.*RuntimeTelemetry' -count=1` before implementation and observe failures.
- [ ] Implement the optional telemetry source and bounded collection outside the lock; preserve all route/timer behavior.
- [ ] Rerun the focused test and `go test -race ./internal/runner -run 'Test.*RuntimeTelemetry|TestProjectSandboxManager_Idle' -count=1`.

### Task 3: Implement the Prometheus Sampler and Private Registry

**Files:**
- Create: `internal/metrics/project_runtime.go`
- Create: `internal/metrics/project_runtime_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- `NewProjectRuntimeMetrics(source runner.ProjectRuntimeTelemetrySource, interval time.Duration) *ProjectRuntimeMetrics` creates a private registry and aggregate metrics; a nil source produces zero runtime gauges.
- `(*ProjectRuntimeMetrics).Run(ctx context.Context)` samples once at startup, then every 30 seconds until canceled.
- `(*ProjectRuntimeMetrics).Handler(token string) http.Handler` serves the private registry and applies bearer authentication before exposition.
- Register exactly these aggregate metric names: `atm_project_runtime_tracked_containers`, `atm_project_runtime_active_task_leases`, `atm_project_runtime_pending_stop_retries`, `atm_project_runtime_memory_bytes`, `atm_project_runtime_memory_limit_bytes`, `atm_project_runtime_cpu_percent`, `atm_project_runtime_max_idle_seconds`, `atm_project_runtime_stats_sampled_containers`, `atm_project_runtime_stats_sample_failures_total`, and `atm_project_runtime_last_sample_timestamp_seconds`. Do not attach labels.

- [ ] Add fake-source tests for the initial sample, later refresh, stale-value retention on a failed collection, sample coverage/failure metrics, and cancellation.
- [ ] Add metric-content tests asserting names/types and absence of high-cardinality labels.
- [ ] Run `go test ./internal/metrics -count=1` before implementation and confirm the new tests fail.
- [ ] Add `github.com/prometheus/client_golang` at a version compatible with the repository's Go version; use a private `prometheus.NewRegistry` and `promhttp.HandlerFor`, not global default registration.
- [ ] Implement constant-time bearer verification before writing any exposition, returning 401 with no metrics for missing/invalid credentials.
- [ ] Rerun `go test ./internal/metrics -count=1` and `go test -race ./internal/metrics -count=1`.

### Task 4: Mount and Run the Optional Metrics Endpoint

**Files:**
- Modify: `internal/api/server.go`
- Create: `internal/api/metrics_test.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
- Add `api.WithMetricsHandler(handler http.Handler) api.Option`.
- Mount `GET /metrics` through Fiber's `middleware/adaptor.HTTPHandler` only when a handler is supplied.
- In `cmd/server/main.go`, if `cfg.MetricsBearerToken` is non-empty, build the runtime metrics service using `taskRt.Sandboxes`, start its sampler under a cancelable context, and pass its authenticated handler to `api.NewServer`.

- [ ] Add Fiber integration tests for route absence without a handler, successful exposition with the correct bearer token, and 401/no body for missing or wrong tokens.
- [ ] Assert the response contains aggregate metric names but no project/container/task identifiers.
- [ ] Run `go test ./internal/api -run 'TestMetrics' -count=1` before implementation and confirm it fails for the missing route behavior.
- [ ] Mount the handler only when configured, start the sampler only when enabled, and cancel the sampler on all normal shutdown/error return paths.
- [ ] Rerun API metrics tests and `go test -race ./internal/api ./internal/metrics ./internal/runner -count=1`.

### Task 5: Full Verification and Change Review

**Files:**
- Review all files in Tasks 1–4; no unrelated files may change.

- [ ] Run `go test ./internal/config ./internal/metrics ./internal/runner ./internal/api ./cmd/server -count=1`.
- [ ] Run `go test -race ./internal/api ./internal/metrics ./internal/runner -count=1`.
- [ ] Run `go test ./...`.
- [ ] Run `docker compose config --quiet && docker compose -f docker-compose.yml -f docker-compose.prod.yml config --quiet`.
- [ ] Run `git diff --check` and inspect the exact diff, including pre-existing idle-shutdown modifications.
- [ ] Do not scrape a live production endpoint, restart services, stop containers, or commit changes; report any deployment verification that remains pending.
