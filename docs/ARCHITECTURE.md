# Architecture & Design Specifications

This document outlines the system architecture, component topology, Git worktree isolation mechanisms, and real-time streaming infrastructure of the **Agent Task Manager (ATM)**.

---

## 1. System Topology & Component Interactions

ATM is architected as an event-driven, distributed control plane designed for high isolation and real-time observability during autonomous coding operations.

```
                              [ Browser Client (React 19) ]
                                             │
                         HTTPS / WSS (:443)  │  HTTP / WS (:80)
                                             ▼
                 ┌────────────────────────────────────────────────────────┐
                 │                Traefik Reverse Proxy                   │
                 │   - Edge TLS & security-headers middleware            │
                 │   - PathPrefix `/api`, `/ws` ──> Control Plane :8080   │
                 │   - PathPrefix `/dex`        ──> Dex OIDC :5556        │
                 │   - Host (`/`, `/assets`)    ──> Web UI :80            │
                 └───────────────────────────┬────────────────────────────┘
                                             │
                                             ▼
                 ┌────────────────────────────────────────────────────────┐
                 │            Control Plane (Fiber v3 / Go 1.27)          │
                 │  - REST API & WebSocket Endpoints                      │
                 │  - Multi-tenant RBAC (Viewer, Member, Admin, Owner)    │
                 │  - AES-256-GCM Credential Cipher                       │
                 │  - WebSocket Hub (Global broadcast & Task streams)     │
                 └───────┬───────────────────┬────────────────────┬───────┘
                         │                   │                    │
            SQL Statements│                   │ Task Enqueue       │ Realtime Events
                         ▼                   ▼                    ▼
             ┌──────────────────────┐  ┌───────────────────┐  ┌─────────────────────┐
             │ PostgreSQL 18 Alpine │  │  Redis 7 Alpine   │  │   WebSocket Hub     │
             │ - Tasks & Attempts   │  │  - Asynq Queues   │  │ - Global /ws/events │
             │ - Users & Teams      │  │    * Critical (6) │  │ - Task ANSI Stream  │
             │ - Logs & Credentials │  │    * Default  (3) │  │   /ws/terminal/:id  │
             └──────────────────────┘  │    * Low      (1) │  └─────────────────────┘
                                       └─────────┬─────────┘
                                                 │
                                                 ▼
                                       ┌───────────────────┐
                                       │Asynq Worker Server│
                                       │(Bounded Concurrency)│
                                       └─────────┬─────────┘
                                                 │
                                                 ▼
                                       ┌───────────────────┐
                                       │  AgentTaskRunner  │
                                       └─────────┬─────────┘
                                                 │
                        ┌────────────────────────┴────────────────────────┐
                        ▼                                                 ▼
        ┌───────────────────────────────┐                 ┌───────────────────────────────┐
        │     Host Git Worktree Root    │                 │   Container / Host Sandbox    │
        │ - origin fetch & base resolve │                 │ - debian:bookworm-slim        │
        │ - Branch `task/<task-id>`     │                 │ - OpenCode Engine Execution   │
        │ - 0777 Permission Enforce     │                 │ - Live Demuxed Stdout/Stderr  │
        │ - Pre-commit diff snapshots   │                 │ - Environment Injection       │
        └───────────────────────────────┘                 └───────────────────────────────┘
```

### Component Roles

1. **Ingress (Traefik v3)**:
   - Terminate public HTTP/HTTPS traffic.
   - Inject security headers (`nosniff`, `DENY`, `strict-origin-when-cross-origin`).
   - Route `/dex` to the Dex identity provider, `/api` and `/ws` to the Go control plane, and remaining frontend paths to Nginx.
2. **Control Plane (Go Fiber v3)**:
   - Manages task lifecycle state transitions.
   - Enforces RBAC permissions based on team membership and authenticated OIDC identity.
   - Coordinates Asynq background task queues.
   - Operates the in-memory WebSocket event hub.
3. **Database (PostgreSQL 18)**:
   - Stores durable relational entities (`teams`, `users`, `projects`, `tasks`, `task_attempts`, `task_logs`, `credentials`).
   - Uses auto-migration on launch for zero-touch schema setup.
4. **Queue Engine (Redis 7 & Asynq)**:
   - Provides distributed task scheduling across 3 weighted tiers (`critical`, `default`, `low`).
   - Guarantees at-least-once execution with automatic retry backoff.
5. **Agent Task Runner (`AgentTaskRunner`)**:
   - The concrete executor running within the Asynq worker process.
   - Clones and prepares Git worktrees on the host.
   - Executes OpenCode either inside a sandboxed Docker container or directly on the host using the local binary.
   - Captures real-time output line-by-line and streams it to the WebSocket hub and PostgreSQL log store.
6. **Frontend (React 19 & Vite 6)**:
   - Single-page application using Zustand store for task state synchronization.
   - Displays real-time Kanban board updates and dynamic metrics.
   - Incorporates `@xterm/xterm` with `@xterm/addon-fit` for ANSI terminal rendering.
   - Features unified Git diff inspection with hunk parsing.

---

## 2. Git Worktree Isolation Lifecycle

### The Problem with Snap Docker & Shared Workspaces
Running autonomous coding agents in traditional Docker setups introduces two major failure modes:
1. **Snap Docker `/tmp` Traps**: On Ubuntu and distributions where Docker or microk8s is packaged via Canonical Snap, the daemon runs in a strict AppArmor confinement. Mounts referencing `/tmp` are mapped to `/tmp/snap.docker/...`, rendering container volume bind mounts invisible or non-functional.
2. **Dirty Tree Contamination**: Executing multiple concurrent coding agents inside a single shared repository directory leads to Git index lock conflicts (`.git/index.lock`), uncommitted file collisions, and branch corruption.

### ATM Worktree Lifecycle Pattern
ATM avoids these pitfalls by allocating an isolated Git worktree for every single task on the host filesystem:

```
[Target Repo: /data/repos/core]
         │
         ├── Fetch latest remote commits: git fetch --prune origin
         ├── Resolve base ref: origin/main -> main -> HEAD
         │
         ▼
[git worktree add /data/worktrees/<task-id> -b task/<task-id> origin/main]
         │
         ├── Permissive Permissions: chmod -R 0777 /data/worktrees/<task-id>
         │   (Ensures container or unprivileged host agent can write files)
         │
         ├── Inject Sandboxed Config (.opencode/skills, .opencode/mcp.json)
         │
         ├── Execution: OpenCode modifies code inside /data/worktrees/<task-id>
         │
         ▼
[git.CaptureSnapshot: git diff origin/main...HEAD]
         │
         ├── Parse file additions, deletions, modified files
         ├── Broadcast unified diff over WebSocket (/api/v1/ws/terminal/:task_id)
         │
         ▼
[Task Review / Cleanup: git worktree remove --force /data/worktrees/<task-id>]
```

#### Detailed Steps

1. **Remote Synchronization (`SyncRepoWithRemote`)**:
   - Before branching, the runner inspects if `origin` (or any remote) is configured in the repository (`internal/runner/task_runner.go`).
   - If present, it executes `git fetch --prune <remote>` with a 25-second timeout, emitting ANSI progress logs to the user's terminal stream.
2. **Base Ref Resolution (`resolveBaseRef`)**:
   - ATM queries Git candidate refs in strict priority order:
     1. `origin/main`
     2. `origin/master`
     3. `main`
     4. `master`
     5. `HEAD`
   - This ensures new tasks automatically branch from fresh upstream commits rather than stale local checkouts.
3. **Worktree Creation (`CreateWorktree`)**:
   - ATM invokes `git worktree add <localPath> -b <branchName> <baseRef>`.
   - If the task branch already exists from a prior attempt, it falls back to attaching to the existing branch (`git worktree add <localPath> <branchName>`).
4. **Permission Normalization**:
   - The runner executes `chmod -R 0777 <localPath>` to ensure that unprivileged agent user accounts (e.g. UID 1000 or container non-root users) have unhindered write access without requiring `sudo`.
5. **Snapshot & Diff Generation (`CaptureSnapshot`)**:
   - Upon task completion, ATM runs `git diff origin/main...HEAD` (or diffs against the empty tree SHA `4b825dc642cb6eb9a060e54bf8d69288fbee4904` if no commits exist).
   - The unified diff output is parsed into additions, deletions, and file lists, and immediately broadcast to connected clients for code review.

---

## 3. Real-Time Event & Streaming Architecture

ATM features a dual-channel WebSocket architecture separating global task coordination events from high-frequency ANSI terminal streams.

### Channel 1: Global Kanban Updates (`/api/v1/ws/events`)

- **Route**: `GET /api/v1/ws/events` (upgraded via fasthttp/websocket)
- **Scope**: Broadcast to all connected web clients globally.
- **Payload Structure**:
  ```json
  {
    "task_id": "1d803621-e374-4ec5-b220-d3a95d5fe038",
    "type": "status",
    "payload": "in_progress",
    "timestamp": 1775555620123
  }
  ```
- **Lifecycle Integration**:
  - Whenever a task changes status (`queued` ➔ `in_progress` ➔ `review` / `failed` / `done`), `taskService.UpdateStatus` pushes an event through `ws.Hub`.
  - The React frontend hook `useTaskSubscription` receives the payload and updates the Zustand store in memory without requiring full page refetches.
  - An adaptive fallback polling mechanism operates every 3 seconds while active tasks remain in flight.

### Channel 2: Terminal ANSI Log Stream (`/api/v1/ws/terminal/:task_id`)

- **Route**: `GET /api/v1/ws/terminal/:task_id`
- **Scope**: Scoped strictly to subscribers inspecting a specific task ID.
- **Payload Types**:
  1. **Historical Log Replay**: Upon client connection, all historical `TaskLog` chunks from PostgreSQL are replayed chronologically:
     ```json
     {
       "task_id": "1d803621-e374-4ec5-b220-d3a95d5fe038",
       "type": "log",
       "payload": "\u001b[36m[Agent Control Plane]\u001b[0m Starting autonomous task execution...\r\n",
       "timestamp": 1775555620150
     }
     ```
  2. **Live Demuxed Output**: As OpenCode runs (via Docker `stdcopy.StdCopy` or host stdout/stderr pipes), lines are parsed and broadcast in real-time.
  3. **Diff Snapshots**: When execution completes, diff updates are dispatched with `"type": "diff"`:
     ```json
     {
       "task_id": "1d803621-e374-4ec5-b220-d3a95d5fe038",
       "type": "diff",
       "payload": "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n...",
       "timestamp": 1775555620890
     }
     ```

### Terminal Frontend Rendering & Dynamic Themes

The web client integrates `@xterm/xterm` with `@xterm/addon-fit` within `TerminalView.tsx`. The terminal instance dynamically detects and hot-reloads its color palette when the user alters the application theme:

- **Dark Theme (`#090d16` background)**: Optimized for dark IDE layouts.
- **Light Theme (`#ffffff` background)**: High contrast for day viewing.
- **Matrix Theme (`#050a05` background, `#00ff66` foreground)**: Cyberpunk green phosphor aesthetic.

---

## 4. 4-Tier Asynq Queue Scheduling

ATM schedules background execution through Redis 7 using [Asynq](https://github.com/hibiken/asynq).

### Priority Tiers and Weights

Tasks are assigned an integer priority between 1 and 100, which maps directly to one of three queue tiers:

| Task Priority | Numeric Value | Queue Tier | Weighted Dispatch Ratio |
|---|---|---|---|
| **Urgent** | `100` | `critical` | **6** (60% worker share) |
| **High** | `75` | `critical` | **6** (60% worker share) |
| **Normal** | `50` | `default` | **3** (30% worker share) |
| **Low** | `25` | `low` | **1** (10% worker share) |

```go
asynqCfg := asynq.Config{
    Concurrency: concurrency, // Configured via MAX_CONCURRENT_TASKS (default 4)
    Queues: map[string]int{
        "critical": 6,
        "default":  3,
        "low":      1,
    },
}
```

Under heavy queue load, the Asynq worker server processes jobs from the `critical` queue twice as frequently as `default`, and six times as frequently as `low`, preventing lower-priority autonomous exploratory subtasks from starving critical bugfixes.
