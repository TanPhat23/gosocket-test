# Subagents & Autonomous Delegation Guide

This guide details the hierarchical subagent task delegation pattern in **Agent Task Manager (ATM)**. It explains how running AI agents detect their environment, autonomously dispatch child subtasks, track progress, and how engineers navigate parent-child hierarchies in the user interface.

---

## 1. The Autonomous Delegation Pattern

Complex software engineering tasks often require parallel workstreams or specialized sub-investigations:
- A high-level feature task needs a dedicated database migration, an API handler update, and a frontend component.
- An agent encountering failing test suites delegates isolated reproduction test cases to a subordinate agent.
- A long-running refactoring task forks independent documentation or linting jobs without blocking primary implementation.

ATM enables **hierarchical subagent delegation**, wherein an executing agent can act as an orchestrator, spawning asynchronous child tasks with explicit parent linkage.

```
                              ┌────────────────────────────────────────┐
                              │  Parent Task (ATM_TASK_ID: task-root)  │
                              │  "Implement Multi-tenant API Keys"     │
                              └───────────────────┬────────────────────┘
                                                  │
                                                  │ POST /api/v1/tasks
                                                  │ (parent_task_id: "task-root")
                                                  ▼
                        ┌──────────────────────────────────────────────────┐
                        │             ATM Distributed Queue                │
                        │        Asynq Priority Schedule & Worktrees       │
                        └─────────┬──────────────────────────────┬─────────┘
                                  │                              │
                                  ▼                              ▼
            ┌───────────────────────────────┐  ┌────────────────────────────────┐
            │  Subtask 1 (task-sub-01)      │  │  Subtask 2 (task-sub-02)       │
            │  "Write Postgres migration"   │  │  "Generate Fiber v3 endpoints" │
            │  Worktree: worktrees/task-... │  │  Worktree: worktrees/task-...  │
            └───────────────────────────────┘  └────────────────────────────────┘
```

---

## 2. Environment Auto-Discovery

When ATM launches an agent attempt (either within the Docker container sandbox or via the host process runner), `AgentTaskRunner` injects dedicated environment variables into the agent's execution context:

| Environment Variable | Description | Example Value |
|---|---|---|
| `ATM_TASK_ID` | The unique UUID of the currently executing parent task. | `1d803621-e374-4ec5-b220-d3a95d5fe038` |
| `ATM_PROJECT_ID` | The project identifier the task belongs to. | `01946761-0000-7000-8000-000000000001` |
| `ATM_API_URL` | Base HTTP endpoint for the ATM control plane API. | `http://localhost:8080` |
| `ATM_TASK_MODEL` | The AI model driving the current execution. | `google/gemini-3.8-flash` |
| `OPENCODE_PASSWORD` | Access token for the local OpenCode server runtime. | `4YXQ3udRl8k...` |

Agents can verify their operating context immediately:

```bash
echo "Parent Task ID: $ATM_TASK_ID"
echo "Project ID: $ATM_PROJECT_ID"
echo "Control Plane API: $ATM_API_URL"
```

---

## 3. Spawning Child Subtasks

Because `ATM_TASK_ID` and `ATM_API_URL` are present in the environment, any agent (or script run by an agent) can file child subtasks directly using `curl` or language SDKs without hardcoded configurations.

### Autonomous Delegation via `curl`

```bash
#!/usr/bin/env bash
set -euo pipefail

# Confirm runtime environment
if [ -z "${ATM_TASK_ID:-}" ] || [ -z "${ATM_API_URL:-}" ]; then
  echo "Error: Not running within an ATM agent execution context." >&2
  exit 1
fi

echo "Delegating child subtask under parent $ATM_TASK_ID..."

# Dispatch subtask
SUBTASK_RESPONSE=$(curl -s -X POST "${ATM_API_URL}/api/v1/tasks" \
  -H "Content-Type: application/json" \
  -d "{
    \"project_id\": \"${ATM_PROJECT_ID}\",
    \"parent_task_id\": \"${ATM_TASK_ID}\",
    \"title\": \"Implement unit tests for session revokation\",
    \"prompt\": \"Add comprehensive table-driven tests in internal/auth/session_test.go covering expired and revoked sessions.\",
    \"assignee_id\": \"subagent-tester\",
    \"assignee_type\": \"agent\",
    \"priority\": 50,
    \"model\": \"google/gemini-3.8-flash\"
  }")

SUBTASK_ID=$(echo "$SUBTASK_RESPONSE" | grep -o '"id":"[^"]*' | cut -d'"' -f4)
echo "✔ Successfully dispatched subtask: $SUBTASK_ID"
```

### Monitoring Delegated Subtasks

The parent agent can query the control plane to list all subtasks associated with its task ID:

```bash
curl -s "${ATM_API_URL}/api/v1/tasks?parent_task_id=${ATM_TASK_ID}"
```

This returns an array of child task objects, including their current `status` (`queued`, `in_progress`, `review`, `done`, `failed`). The parent agent can poll until child subtasks reach `review` or `done` before proceeding.

---

## 4. UI Navigation & Hierarchy Tracking

ATM's web console provides native visual tracking for task parentage and delegated subtasks in `TaskInspectorModal.tsx`:

### 1. Delegated Subtasks List
When viewing a parent task in the **Task Inspector Modal** under the **Overview** tab:
- The **Delegated Subtasks** section displays all child tasks where `parent_task_id == task.id`.
- Each subtask item displays its short UUID (`#1d803621`), title, and live status badge (`in_progress`, `review`, `done`).
- Clicking any subtask immediately shifts modal focus to that child task, allowing inspection of its prompt, terminal logs, and worktree diffs.

### 2. Parent Task Breadcrumb & Backlink
When viewing a child subtask:
- Under **Execution Runtime**, a **Parent Task** link is displayed with icon `CornerDownRight` and the parent ID (e.g. `#{parent_id}`).
- Clicking the parent link returns navigation back to the orchestrator task.

### 3. Isolated Worktree Safety
Each delegated subtask runs in its own separate worktree:
- Parent task: `worktrees/<parent-id>` on branch `task/<parent-id>`
- Subtask 1: `worktrees/<subtask-id-1>` on branch `task/<subtask-id-1>`
- Subtask 2: `worktrees/<subtask-id-2>` on branch `task/<subtask-id-2>`

Because every task operates on a dedicated Git branch and worktree directory, subagents can modify code, compile binaries, and commit changes completely in parallel without file collisions or Git lock conflicts.
