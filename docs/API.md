# REST & WebSocket API Specification

This document provides the authoritative API specification for the **Agent Task Manager (ATM)** control plane. All endpoints are prefixed by `/api/v1` and served over HTTP/1.1 and WebSockets.

---

## Table of Contents

1. [Authentication & Headers](#1-authentication--headers)
2. [AI Models Discovery](#2-ai-models-discovery)
   - [`GET /api/v1/models`](#get-apiv1models)
3. [Task Management](#3-task-management)
   - [`POST /api/v1/tasks`](#post-apiv1tasks)
   - [`GET /api/v1/tasks`](#get-apiv1tasks)
   - [`GET /api/v1/tasks/:task_id`](#get-apiv1taskstask_id)
   - [`POST /api/v1/tasks/:task_id/cancel`](#post-apiv1taskstask_idcancel)
4. [Teams & Projects](#4-teams--projects)
   - [`GET /api/v1/teams`](#get-apiv1teams)
   - [`GET /api/v1/projects`](#get-apiv1projects)
5. [Real-Time WebSocket Streams](#5-real-time-websocket-streams)
   - [`WS /api/v1/ws/events`](#ws-apiv1wsevents)
   - [`WS /api/v1/ws/terminal/:task_id`](#ws-apiv1wsterminaltask_id)

---

## 1. Authentication & Headers

- Standard Content-Type: `application/json`
- Session Authentication: HttpOnly secure session cookie (`atm_session`) established via OIDC callback (`GET /api/v1/auth/callback`).
- Direct API calls from local agents or scripts within the container sandbox can hit `http://localhost:8080` (or `ATM_API_URL`).

---

## 2. AI Models Discovery

### `GET /api/v1/models`

Lists all AI models available to the agent runner engine (queried dynamically from the local OpenCode server runtime or falling back to built-in models).

#### Request Example

```bash
curl -s http://localhost:8080/api/v1/models
```

#### Response (`200 OK`)

```json
[
  {
    "id": "google/gemini-3.8-flash",
    "name": "Gemini 3.8 Flash (Active)",
    "provider": "google",
    "active": true
  },
  {
    "id": "google/gemini-2.5-pro",
    "name": "Gemini 2.5 Pro",
    "provider": "google"
  },
  {
    "id": "anthropic/claude-sonnet-4-5",
    "name": "Claude Sonnet 4 5",
    "provider": "anthropic"
  },
  {
    "id": "anthropic/claude-opus-4-6",
    "name": "Claude Opus 4 6",
    "provider": "anthropic"
  },
  {
    "id": "openai/gpt-5.5",
    "name": "Gpt 5.5",
    "provider": "openai"
  },
  {
    "id": "openai/gpt-5.6-luna",
    "name": "Gpt 5.6 Luna",
    "provider": "openai"
  },
  {
    "id": "opencode-go/qwen3.8-max",
    "name": "Qwen3.8 Max",
    "provider": "opencode-go"
  },
  {
    "id": "opencode-go/deepseek-v4-pro",
    "name": "Deepseek V4 Pro",
    "provider": "opencode-go"
  },
  {
    "id": "opencode/big-pickle",
    "name": "Big Pickle",
    "provider": "opencode"
  }
]
```

---

## 3. Task Management

### `POST /api/v1/tasks`

Creates a new task. The control plane saves the task to PostgreSQL, queues it in Asynq, and broadcasts the status to active WebSocket clients.

#### Request Schema (`dto.CreateTaskRequest`)

| Field | Type | Required | Description |
|---|---|---|---|
| `project_id` | string (UUID) | **Yes** | Target project identifier. |
| `title` | string | **Yes** | Short description (1–255 chars). |
| `prompt` | string | **Yes** | Detailed instructions for the agent. |
| `assignee_id` | string | **Yes** | Assignee identifier (e.g., `agent`, `developer@example.com`). |
| `assignee_type` | string | **Yes** | `"agent"` or `"human"`. |
| `priority` | integer | No | Integer priority: `25` (Low), `50` (Normal), `75` (High), `100` (Urgent). Default `50`. |
| `parent_task_id` | string (UUID) | No | Optional parent task UUID for subagent delegations. |
| `model` | string | No | Specific model ID (e.g. `"google/gemini-3.8-flash"`). |
| `skills` | array[string] | No | Optional list of agent skills to enable (e.g. `["code-review-and-quality"]`). |
| `mcp_servers` | array[string] | No | Optional list of MCP servers to activate. |

#### Request Example

```bash
curl -s -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "project_id": "01946761-0000-7000-8000-000000000001",
    "title": "Refactor token auth middleware",
    "prompt": "Inspect internal/auth and add unit tests covering expired JWT tokens.",
    "assignee_id": "gemini-agent",
    "assignee_type": "agent",
    "priority": 75,
    "model": "google/gemini-3.8-flash",
    "skills": ["code-review-and-quality"]
  }'
```

#### Response (`201 Created`)

```json
{
  "id": "1d803621-e374-4ec5-b220-d3a95d5fe038",
  "project_id": "01946761-0000-7000-8000-000000000001",
  "title": "Refactor token auth middleware",
  "prompt": "Inspect internal/auth and add unit tests covering expired JWT tokens.",
  "assignee_id": "gemini-agent",
  "assignee_type": "agent",
  "status": "queued",
  "priority": 75,
  "parent_task_id": null,
  "model": "google/gemini-3.8-flash",
  "skills": ["code-review-and-quality"],
  "mcp_servers": [],
  "asynq_task_id": "31ac4440-428e-4f05-89b9-d24823908db6",
  "external_id": null,
  "external_url": null,
  "pr_url": null,
  "worktree_path": null,
  "branch_name": null,
  "container_id": null,
  "exit_code": null,
  "heartbeat_at": null,
  "created_at": "2026-10-07T09:40:00Z",
  "updated_at": "2026-10-07T09:40:00Z"
}
```

---

### `GET /api/v1/tasks`

Retrieves tasks, optionally filtered by `project_id` or `parent_task_id`.

#### Query Parameters

- `project_id` *(optional)*: Filter tasks for a specific project.
- `parent_task_id` *(optional)*: Filter for child tasks delegated by a parent task.

#### Request Example

```bash
# Query all tasks for a project
curl -s "http://localhost:8080/api/v1/tasks?project_id=01946761-0000-7000-8000-000000000001"

# Query subtasks of a parent task
curl -s "http://localhost:8080/api/v1/tasks?parent_task_id=1d803621-e374-4ec5-b220-d3a95d5fe038"
```

#### Response (`200 OK`)

```json
[
  {
    "id": "1d803621-e374-4ec5-b220-d3a95d5fe038",
    "project_id": "01946761-0000-7000-8000-000000000001",
    "title": "Refactor token auth middleware",
    "prompt": "Inspect internal/auth and add unit tests covering expired JWT tokens.",
    "assignee_id": "gemini-agent",
    "assignee_type": "agent",
    "status": "in_progress",
    "priority": 75,
    "parent_task_id": null,
    "model": "google/gemini-3.8-flash",
    "skills": ["code-review-and-quality"],
    "mcp_servers": [],
    "asynq_task_id": "31ac4440-428e-4f05-89b9-d24823908db6",
    "worktree_path": "/data/worktrees/1d803621-e374-4ec5-b220-d3a95d5fe038",
    "branch_name": "task/1d803621-e374-4ec5-b220-d3a95d5fe038",
    "created_at": "2026-10-07T09:40:00Z",
    "updated_at": "2026-10-07T09:40:15Z"
  }
]
```

---

### `GET /api/v1/tasks/:task_id`

Fetches complete metadata, timestamps, and worktree information for a specific task.

#### Request Example

```bash
curl -s http://localhost:8080/api/v1/tasks/1d803621-e374-4ec5-b220-d3a95d5fe038
```

#### Response (`200 OK`)

```json
{
  "id": "1d803621-e374-4ec5-b220-d3a95d5fe038",
  "project_id": "01946761-0000-7000-8000-000000000001",
  "title": "Refactor token auth middleware",
  "prompt": "Inspect internal/auth and add unit tests covering expired JWT tokens.",
  "assignee_id": "gemini-agent",
  "assignee_type": "agent",
  "status": "review",
  "priority": 75,
  "parent_task_id": null,
  "model": "google/gemini-3.8-flash",
  "skills": ["code-review-and-quality"],
  "mcp_servers": [],
  "asynq_task_id": "31ac4440-428e-4f05-89b9-d24823908db6",
  "worktree_path": "/data/worktrees/1d803621-e374-4ec5-b220-d3a95d5fe038",
  "branch_name": "task/1d803621-e374-4ec5-b220-d3a95d5fe038",
  "exit_code": 0,
  "created_at": "2026-10-07T09:40:00Z",
  "updated_at": "2026-10-07T09:42:10Z"
}
```

---

### `POST /api/v1/tasks/:task_id/cancel`

Cancels an active or queued task. Deletes the job from the Redis queue if pending, and transitions database state to `cancelled`.

#### Request Example

```bash
curl -s -X POST http://localhost:8080/api/v1/tasks/1d803621-e374-4ec5-b220-d3a95d5fe038/cancel
```

#### Response (`200 OK`)

```json
{
  "status": "cancelled"
}
```

---

## 4. Teams & Projects

### `GET /api/v1/teams`

Lists all teams registered in the workspace.

#### Request Example

```bash
curl -s http://localhost:8080/api/v1/teams
```

#### Response (`200 OK`)

```json
[
  {
    "id": "01946760-ffff-7000-8000-000000000000",
    "name": "Core Team",
    "slug": "core-team",
    "created_at": "2026-10-07T09:00:00Z",
    "updated_at": "2026-10-07T09:00:00Z"
  }
]
```

---

### `GET /api/v1/projects`

Lists projects, optionally filtered by `team_id`.

#### Request Example

```bash
curl -s "http://localhost:8080/api/v1/projects?team_id=01946760-ffff-7000-8000-000000000000"
```

#### Response (`200 OK`)

```json
[
  {
    "id": "01946761-0000-7000-8000-000000000001",
    "team_id": "01946760-ffff-7000-8000-000000000000",
    "name": "Core Workspace",
    "repo_path": "/data/repos/core",
    "created_at": "2026-10-07T09:00:00Z",
    "updated_at": "2026-10-07T09:00:00Z"
  }
]
```

---

## 5. Real-Time WebSocket Streams

### `WS /api/v1/ws/events`

Global WebSocket endpoint streaming real-time status transitions for all tasks. Used by the Kanban board to dynamically update column placements without HTTP polling.

#### Connection URL

```
ws://localhost:8080/api/v1/ws/events
```

#### Inbound Frame Example

```json
{
  "task_id": "1d803621-e374-4ec5-b220-d3a95d5fe038",
  "type": "status",
  "payload": "in_progress",
  "timestamp": 1775555620123
}
```

---

### `WS /api/v1/ws/terminal/:task_id`

Dedicated task execution stream. Upon connection, replayed historical logs from the database are emitted, followed by live demuxed container stdout/stderr chunks and diff snapshots.

#### Connection URL

```
ws://localhost:8080/api/v1/ws/terminal/1d803621-e374-4ec5-b220-d3a95d5fe038
```

#### Frame Types

1. **Log Frame (`"type": "log"` or raw ANSI string)**:
   ```json
   {
     "task_id": "1d803621-e374-4ec5-b220-d3a95d5fe038",
     "type": "log",
     "payload": "\u001b[36m[Agent Control Plane]\u001b[0m Syncing with remote origin upstream...\r\n",
     "timestamp": 1775555620150
   }
   ```
2. **Status Notification (`"type": "status"` )**:
   ```json
   {
     "task_id": "1d803621-e374-4ec5-b220-d3a95d5fe038",
     "type": "status",
     "payload": "review",
     "timestamp": 1775555700000
   }
   ```
3. **Diff Snapshot (`"type": "diff"` )**:
   ```json
   {
     "task_id": "1d803621-e374-4ec5-b220-d3a95d5fe038",
     "type": "diff",
     "payload": "diff --git a/internal/auth/tokens.go b/internal/auth/tokens.go\nindex e69de29..4b825dc 100644\n--- a/internal/auth/tokens.go\n+++ b/internal/auth/tokens.go\n@@ -20,6 +20,11 @@\n...",
     "timestamp": 1775555701200
   }
   ```

Clients can inspect code diffs either via the live WebSocket `"diff"` frame or inside the Task Inspector Modal Diff tab.
