# ADR-004: OpenCode Session Persistence and Pipeline Failure Feedback

## Status
Accepted

## Date
2026-10-07

## Context
In Agent Task Manager (ATM), the autonomous agent runner (`AgentTaskRunner` in `internal/runner/task_runner.go`) coordinates worktrees and launches the OpenCode execution engine. Currently, both Docker container execution (`runInDocker`) and host fallback execution (`runOnHost`) invoke the OpenCode CLI with arguments structured as follows:

```bash
opencode run --server "http://127.0.0.1:49374" --model "<model>" --auto "<prompt>"
```

### Problem Statement
1. **Ephemeral Execution**: Omitting `--session <session_id>` forces OpenCode to generate a brand-new random session ID for every execution invocation.
2. **Loss of Context Between Attempts**: When a task fails or requires subsequent refinement across attempts (e.g., attempt 1 fails compilation or test suites), attempt 2 starts completely blind to what attempt 1 planned, read, modified, or failed on.
3. **Absence of Pipeline Failure Feedback Loops**: Neither local pre-flight checks (test suite, linter, typecheck) nor external CI/CD pipelines (e.g., GitHub Actions `check_run` failures via webhook) can re-engage the agent within its existing conversation context. Instead, errors are either abandoned or require manual human intervention to formulate a new prompt.
4. **No Session Correlation**: Operators and developers cannot correlate an ATM task entity in PostgreSQL (`tasks.id`) with the session history stored in OpenCode's background server database (`~/.local/share/opencode`).

## Decision

We establish persistent OpenCode session binding and an automated failure feedback loop architecture across ATM:

### 1. Deterministic Task-to-Session Binding
- Every task is deterministically bound to an OpenCode session ID formatted as:
  ```
  ses_atm_<sanitized_task_uuid>
  ```
- Store `session_id` in the ATM database schema (`tasks.session_id`) and propagate it across all lifecycle events.
- CLI invocations in `internal/runner/task_runner.go` must pass `--session` and `--title`:
  ```bash
  opencode run \
    --server "$OPENCODE_SERVER_URL" \
    --session "ses_atm_${TASK_ID}" \
    --title "ATM Task: ${TASK_TITLE}" \
    --model "$MODEL" \
    --auto \
    "$PROMPT"
  ```
  OpenCode creates the session if it does not yet exist, and re-attaches to it if it already exists, preserving message history and tool execution traces.

### 2. Dual-Interface Access (CLI & OpenCode REST API)
- **Initial Prompting & Heavy Batches**: Continued use of `opencode run` CLI via Docker sandbox or host process for full terminal stdout/stderr streaming via ATM's WebSocket log hubs.
- **Incremental Feedback & Background Control**: Leverage OpenCode's background server REST API (`http://127.0.0.1:49374`) authenticated via HTTP Basic Auth (`opencode:<service_password>` loaded from `~/.config/opencode/service.json` or `OPENCODE_PASSWORD`):
  - `POST /api/session`: Inspect or create explicitly configured session configurations.
  - `POST /api/session/{sessionID}/prompt`: Deliver targeted failure context, compiler output, or linter errors directly into the active session without tearing down or reconstructing container wrappers.
  - `GET /api/session/{sessionID}/message`: Audit session history, intermediate thoughts, and agent responses.

### 3. Automated Dual-Loop Failure Feedback
- **Loop 1: In-Worktree Pre-Flight Verification Loop (Local)**:
  - Immediately following agent execution in the task worktree, ATM executes project verification commands (e.g., `go test ./...`, `npm test`, `cargo test`).
  - If verification exits with a non-zero exit status, ATM captures standard output and standard error and submits a structured repair prompt back into session `ses_atm_<task.id>`.
  - The loop retries up to a bounded threshold (default: 3 iterations) before escalating to task failure.
- **Loop 2: Remote CI/CD Webhook Feedback Loop (GitHub Actions / External)**:
  - When ATM receives a GitHub webhook indicating a failed `check_run` or `workflow_run` associated with the task's branch or Pull Request, ATM fetches the failure log annotations.
  - ATM pushes a corrective prompt into `ses_atm_<task.id>`, reactivates the task runner on the existing worktree branch, commits fixes, and pushes updates back to the PR branch.

## Alternatives Considered

### 1. Ephemeral Sessions Per Attempt
- **Approach**: Generate a fresh session ID on every attempt (status quo) and concatenate previous error logs into the new initial prompt.
- **Pros**: Stateless; no risk of session context window overflow or contaminated agent scratchpads.
- **Cons**: Substantial token waste re-reading files already explored; loses tool execution histories, mental models, and partial patch rationale; agent often repeats the exact same failing hypothesis.
- **Rejected**: Prevents genuine iterative self-correction.

### 2. Pure REST API Execution (Eliminating CLI `opencode run`)
- **Approach**: Replace all CLI invocations with pure HTTP REST API calls (`/api/session/{sessionID}/prompt`) and WebSocket event streaming directly to the OpenCode daemon.
- **Pros**: Clean API architecture; no process spawning overhead.
- **Cons**: Loses the containerization sandbox wrapper (`runInDocker` in `internal/runner/task_runner.go`), which isolates agent bash commands and filesystem modifications; requires reimplementing terminal stream demuxing.
- **Rejected**: We keep Docker/CLI sandboxing for full execution isolation while using REST API endpoints for lightweight asynchronous notifications and session inspection.

## Consequences

### Positive
- **Autonomous Self-Healing**: Agents can see their own compiler/test errors in context and immediately patch syntax errors, missing imports, or failing unit test assertions.
- **Token Efficiency**: Agents retain context of existing file locations and previous reasoning, eliminating repetitive directory scans and read operations.
- **Bidirectional Observability**: Engineers inspecting OpenCode Web UI (`http://127.0.0.1:49374`) can see tasks cleanly labeled with ATM task IDs and titles.
- **Continuous Integration Cohesion**: PRs with failing CI checks trigger automatic self-repair without requiring manual human context switching.

### Negative / Trade-Offs
- **Context Window Growth**: Repeated failure-feedback iterations enlarge the session context history, increasing token consumption per turn. (Mitigated by OpenCode's native context compaction: `/api/session/{sessionID}/compact`).
- **Loop Bounds**: Infinite failure loops must be strictly bounded with an iteration budget (max 3-5 iterations) and total attempt limits.
- **State Cleanup**: Task deletion or worktree pruning must manage OpenCode session cleanup or archival if unbounded growth occurs on disk.
