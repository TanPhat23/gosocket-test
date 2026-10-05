# Agent Task Manager with Container Sandbox & smcp Sync
## Architectural Design Specification

**Date:** 2026-10-05  
**Status:** Validated / Draft  
**Target Project:** `agent-task-manager`

---

## 1. Executive Summary & Goals

### 1.1 Overview
The **Agent Task Manager** is a local-first, container-sandboxed control plane designed to manage, delegate, and supervise coding tasks executed by AI agents (OpenCode, Claude Code, OpenAI Codex).

It combines:
1. **Host-Level Git Worktree Isolation**: Enables multiple parallel tasks in the same repository without working directory conflicts.
2. **Docker Container Sandboxing**: Traps agent tool executions and MCP servers inside an unprivileged Docker container, protecting the user's host machine from unintended edits, malicious commands, or credential tampering.
3. **Single Go Binary Architecture**: A Go backend using the official Docker Go SDK (`github.com/docker/docker/client`) and embedding a modern React/shadcn/ui frontend via `embed.FS`.
4. **Agent Ecosystem & `smcp` Synchronization**: Interoperates with `@tanphat/smcp-core` (located at `~/Code/JS/smcp`) to discover, sanitize, and inject skills and MCP servers dynamically into task environments.

---

## 2. System Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                      Single Go Executable Binary                        │
│                                                                         │
│  ┌───────────────────────┐             ┌─────────────────────────────┐  │
│  │   Embedded Web UI     │             │     Go Control Plane        │  │
│  │ (React 19 + shadcn/ui │ ◀──embed.FS──│  - Task State Machine       │  │
│  │  Tailwind v4 + Vite)  │             │  - Git Worktree Engine      │  │
│  └───────────▲───────────┘             │  - SQLite Database Store    │  │
│              │                         │  - WebSocket Event Hub      │  │
│              │                         │  - smcp Adapter Bridge      │  │
│              │                         └──────────────┬──────────────┘  │
└──────────────┼────────────────────────────────────────┼─────────────────┘
               │ HTTP / WebSockets                      │ Docker Go SDK
               ▼                                        ▼
┌─────────────────────────────┐          ┌─────────────────────────────┐
│    Browser Dashboard        │          │   Docker Container Sandbox  │
│  - 4-Lane Kanban Board      │          │                             │
│  - Live xterm.js Terminal   │          │  - Mount: /workspace (RW)   │
│  - Unified Git Diff Viewer  │          │  - Agent: OpenCode / Claude │
│  - Skill & MCP Selector     │          │  - Tool/MCP Execution       │
│  - One-Click Merge / Review │          │  - CapDrop: ALL, Non-Root   │
└─────────────────────────────┘          └─────────────────────────────┘
```

### 2.1 Backend Core (Go 1.23+)
* **HTTP & Routing**: Standard library `net/http` with `chi` router.
* **Container Orchestration**: `github.com/docker/docker/client` for low-level container creation, log streaming, and lifecycle management.
* **Database**: Embedded SQLite using pure Go (`modernc.org/sqlite`).
* **Real-time Streaming**: `github.com/gorilla/websocket` multiplexing container stdout/stderr to connected UI sessions.
* **Asset Embedding**: `//go:embed dist/*` to package the compiled UI into a single standalone binary.

### 2.2 Frontend UI (React + Latest shadcn/ui)
* **Framework**: React 19 + TypeScript + Vite.
* **Design System**: Latest **shadcn/ui** (Tailwind CSS v4, Base/Radix primitives, Lucide Icons).
* **Terminal Display**: `xterm.js` with ANSI color support, auto-scrolling, and resize handling.
* **Diff Preview**: Side-by-side and unified syntax-highlighted diff viewer.

---

## 3. Sandboxing & Security Controls

### 3.1 Host Filesystem Protection
To prevent agents or MCP servers from executing arbitrary host commands or leaking secrets:
* The host home directory (`~`, `~/.ssh`, `~/.bashrc`, `~/.config`) is **never** mounted.
* **Only** the dedicated task worktree path (`.worktrees/task-<id>`) is mounted to `/workspace`.

### 3.2 Linux Security Hardening
Container options set via Docker SDK:
```go
container.HostConfig{
    Binds: []string{
        worktreeHostPath + ":/workspace:rw",
    },
    Resources: container.Resources{
        Memory:   4 * 1024 * 1024 * 1024, // 4GB RAM limit
        NanoCPUs: 2 * 1000000000,         // 2 CPU cores max
    },
    CapDrop:     []string{"ALL"},          // Drop all Linux capabilities
    SecurityOpt: []string{"no-new-privileges:true"},
    NetworkMode: "bridge",                 // Isolated network bridge
}
```

### 3.3 MCP & Tool Containment
* Tools executed by the agent (e.g. bash scripts, package installations, MCP servers) run **strictly inside the container**.
* Network isolation prevents containers from reaching host-only loopback services unless explicitly white-listed.

---

## 4. Multi-Agent Runner Architecture

The backend implements a modular runner interface:

```go
type AgentRunner interface {
    ID() string
    Name() string
    IsAvailable(ctx context.Context) (bool, error)
    GenerateContainerConfig(task *Task, worktreeDir string) (*container.Config, *container.HostConfig, error)
}
```

### 4.1 Supported Agents
1. **OpenCode Runner**:
   * Runs `opencode run "<prompt>" --model <model>`.
   * Also supports remote endpoints via OpenCode Server API (`opencode serve`).
2. **Claude Code Runner**:
   * Runs `claude -p "<prompt>" --dangerously-skip-permissions`.
   * Passes instructions via prompt payloads and `.claude/rules/`.
3. **OpenAI Codex Runner**:
   * Runs `codex exec "<prompt>"` non-interactively.

---

## 5. `smcp` Integration & Skill / MCP Injection

The task manager integrates with `@tanphat/smcp-core` (or the `smcp` CLI in `~/Code/JS/smcp`):

1. **Skill Discovery**:
   * The Go backend discovers installed skills by reading standard agent directories (`~/.agents/skills`, `.agents/skills`) or running `smcp list --json`.
   * In the task creation form, skills appear as selectable badges (e.g., `test-driven-development`, `systematic-debugging`).
2. **MCP Server Discovery & Presets**:
   * Reads configured MCP servers from `smcp`'s registry or config files.
   * Users can select which MCP tools to attach per task (e.g. `postgres`, `browser`, `github`).
3. **Sandboxed Config Injection**:
   * When provisioning the worktree prior to container launch, the backend writes a sandboxed configuration:
     * For OpenCode: writes `.worktrees/task-<id>/opencode.json` containing only the selected MCP tools.
     * For Claude: writes `.worktrees/task-<id>/.claude/config.json`.
   * Environment variables and API keys are passed directly into the container environment at runtime, never saved in git.

---

## 6. End-to-End Task Lifecycle

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Backlog    │ ──▶ │  In Progress │ ──▶ │    Review    │ ──▶ │     Done     │
│ (Create Task)│     │(Worktree+Run)│     │ (Diff & QA)  │     │(Merge & Prune│
└──────────────┘     └──────────────┘     └──────────────┘     └──────────────┘
```

### Phase 1: Creation (`Backlog`)
* User creates task:
  * Title & Prompt
  * Target repository (validated on host)
  * Agent runner & Model
  * Selected Skills & MCP servers
  * Resource limits (default: 2 CPUs, 4GB RAM)

### Phase 2: Execution (`In Progress`)
1. **Worktree Creation**:
   * Go runs `git worktree add .worktrees/task-<id> -b task/<id>-<slug>` in the target repo.
2. **Config Prep**:
   * Injects selected skills and sanitized MCP configs into the worktree.
3. **Docker Launch**:
   * Creates and starts the container mounting the worktree to `/workspace`.
4. **Real-Time Streaming**:
   * Go attaches to stdout/stderr via Docker SDK and streams frames to the WebSocket hub.
   * UI displays live ANSI output in `xterm.js`.
5. **Abort Handling**:
   * Clicking "Stop" sends `docker stop` & `docker rm -f`, gracefully halting execution.

### Phase 3: Review (`Review`)
* On container exit:
  * Container is destroyed.
  * Go executes `git diff main...task/<id>-<slug>` in the repository.
  * Web UI displays changed files with diff syntax highlighting.
* **Actions Available**:
  * **Approve & Merge**: Merges branch into `main` and cleans up worktree.
  * **Follow-up Prompt**: Spawns a new container in the *same* worktree to make revisions.
  * **Discard**: Deletes worktree and branches.

### Phase 4: Archival (`Done`)
* Go executes `git worktree remove .worktrees/task-<id>`.
* Task marked completed with duration, exit code, and commit hash.

---

## 7. Data Storage & Schema (SQLite)

```sql
CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL, -- 'backlog', 'in_progress', 'review', 'done', 'failed'
    repo_path TEXT NOT NULL,
    worktree_path TEXT,
    branch_name TEXT,
    agent_id TEXT NOT NULL,
    model TEXT,
    skills TEXT, -- JSON array of skill IDs
    mcp_servers TEXT, -- JSON array of MCP IDs
    container_id TEXT,
    exit_code INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS task_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    log_chunk TEXT NOT NULL,
    FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
```

---

## 8. Verification & Testing Strategy

1. **Unit Tests (Go)**:
   * Worktree lifecycle: verify creation, branch isolation, and cleanup.
   * Docker SDK wrapper: mock container creation, volume binding, and capability dropping.
   * Task state machine: verify valid transitions (`backlog` &rarr; `in_progress` &rarr; `review` &rarr; `done`).
2. **Integration Tests**:
   * Launch a test Docker container executing a mock script, stream logs over WebSocket, and verify zero file leaks to the host.
3. **Frontend Component Tests**:
   * Kanban drag-and-drop state updates.
   * Terminal connection and rendering via `xterm.js`.
   * Git diff rendering with shadcn components.

---

## 9. Next Steps

Upon review and approval of this specification:
1. Proceed to implementation planning using the `writing-plans` workflow.
2. Structure the codebase in `/home/ghug/Code/Go/agent-task-manager` or chosen directory.
3. Scaffold Go backend and React/shadcn frontend.
