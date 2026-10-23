# Project Runtime Metrics Specification

## Goal

Expose safe, low-cardinality Prometheus metrics that show whether project-runtime idle shutdown is reducing resource usage and how tracked runtimes are being used.

## Behavior

- Expose `GET /metrics` only when `ATM_METRICS_BEARER_TOKEN` is configured.
- Require a bearer token of at least 32 bytes. Reject a configured shorter token during config loading. Compare credentials without logging or returning the token.
- Refresh runtime samples in a background loop every 30 seconds. A scrape must serve the latest completed sample and must not trigger Docker API calls.
- Collect only project runtimes tracked by the current control-plane process. A control-plane restart may leave containers untracked; discovering those is out of scope, consistent with the idle-shutdown specification.
- Publish aggregate metrics only, without project IDs, names, task IDs, container IDs, user identifiers, or other high-cardinality labels:
  - `atm_project_runtime_tracked_containers` gauge;
  - `atm_project_runtime_active_task_leases` gauge;
  - `atm_project_runtime_pending_stop_retries` gauge;
  - `atm_project_runtime_memory_bytes` and `atm_project_runtime_memory_limit_bytes` gauges, summed across successfully sampled runtimes;
  - `atm_project_runtime_cpu_percent` gauge, summed across successfully sampled runtimes;
  - `atm_project_runtime_max_idle_seconds` gauge;
  - `atm_project_runtime_stats_sampled_containers` gauge;
  - `atm_project_runtime_stats_sample_failures_total` counter;
  - `atm_project_runtime_last_sample_timestamp_seconds` gauge.
- Calculate memory and CPU values using the same Docker formulas as `AgentTaskRunner.ProjectRuntimeStatus` in `internal/runner/project_runtime_manage.go`, so metrics agree with the existing management API.
- Use a bounded number of concurrent Docker stats requests and a per-container timeout. A failed stats request must not block task execution or HTTP scrapes; expose sample coverage/failure metrics so partial data is visible.
- Use a private Prometheus registry. Do not expose default Go/process collectors as part of this change.
- Keep existing project runtime APIs, task concurrency, MCP configuration, and container lifecycle behavior unchanged.

## Security and Operations

- The metrics endpoint is disabled when the bearer token is empty; it must not be anonymously scrapeable by default.
- The token is supplied through environment configuration and must not appear in logs, metric labels, or error responses.
- Metrics are aggregate only. Do not add project-specific labels.
- Sampling work is bounded and performed outside manager locks. Do not change, restart, or stop live containers as part of implementation or verification.
- These metrics describe in-process tracked runtimes only; they do not discover or reap containers after a control-plane restart.

## Acceptance Criteria

1. Empty `ATM_METRICS_BEARER_TOKEN` leaves `/metrics` unregistered/unavailable; a configured token shorter than 32 bytes fails config loading.
2. A correct bearer token receives Prometheus exposition; missing or incorrect credentials receive 401 without metric data.
3. The endpoint emits only the specified aggregate runtime metrics and contains no project/container/task/user identifiers.
4. Scraping does not call Docker; background sampling refreshes on a 30-second cadence and is canceled during shutdown.
5. Manager telemetry snapshots are race-safe and do not hold manager locks while performing Docker stats requests.
6. Docker stats sampling has bounded concurrency and per-container timeouts; partial failures remain observable and do not erase the last valid sample.
7. Existing runtime/task behavior remains unchanged, including active-task protection and idle-stop behavior.

## Out of Scope

- Runtime discovery/reaping across control-plane restarts.
- Per-project or per-container Prometheus labels.
- A dashboard, alert rules, Prometheus server deployment, or memory-limit tuning.
- Changing task concurrency, MCP settings, Docker resource limits, or live runtime state.
