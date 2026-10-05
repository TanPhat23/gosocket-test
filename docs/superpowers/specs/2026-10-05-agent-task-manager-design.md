# Agent Task Manager with Container Sandbox, smcp Sync, Teams & GitHub/Jira Board Sync
## Architectural Design Specification

**Date:** 2026-10-05  
**Status:** Validated / Draft  
**Target Project:** `agent-task-manager` (`/home/ghug/Code/Go/agent-task-manager`)

---

## 1. Executive Summary & Goals

### 1.1 Overview
The **Agent Task Manager** is a high-performance, container-sandboxed control plane designed for collaborative engineering teams delegating tasks to AI coding agents (OpenCode, Claude Code, OpenAI Codex).

It combines four pillars:
1. **Team & Workspace Management**: Multiple teams, projects, and human/agent assignees on unified boards.
2. **Bidirectional Issue Tracker Sync (GitHub & Jira)**: Ingests issues from GitHub and Jira, delegates them to AI agents, and automatically opens Pull Requests and transitions ticket statuses upon completion.
3. **Host-Level Git Worktree Isolation**: Spawns isolated Git worktrees per task so agents can work in parallel on the same repository without local file conflicts.
4. **Docker Container Sandboxing**: Traps agent tool executions and MCP servers inside an unprivileged Docker container, protecting the user's host machine from unintended edits, malicious commands, or credential tampering.
5. **Single Go Binary Architecture**: A Go backend using the official Docker Go SDK (`github.com/docker/docker/client`) and embedding a modern React/shadcn/ui frontend via `embed.FS`.
6. **Agent Ecosystem & `smcp` Synchronization**: Interoperates with `@tanphat/smcp-core` (located at `~/Code/JS/smcp`) to discover, sanitize, and inject skills and MCP servers dynamically into task environments.

---

## 2. System Architecture

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                                External Integrations                                   │
│            ┌────────────────────────────┐       ┌────────────────────────────┐         │
│            │   GitHub Issues & PRs      │       │      Jira Cloud Board      │         │
│            └─────────────┬──────────────┘       └─────────────┬──────────────┘         │
└──────────────────────────┼────────────────────────────────────┼────────────────────────┘
                           │ Webhook / REST                     │ Webhook / REST
                           ▼                                    ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                          Single Go Executable Control Plane                            │
│                                                                                        │
│  ┌───────────────────────┐             ┌────────────────────────────────────────────┐  │
│  │   Embedded Web UI     │             │            Go Control Plane                │  │
│  │ (React 19 + shadcn/ui │ ◀──embed.FS──│  - Team & Project Management Layer         │  │
│  │  Tailwind v4 + Vite)  │             │  - GitHub & Jira Bi-directional Sync       │  │
│  └───────────▲───────────┘             │  - Task State Machine & Worktree Engine    │  │
│              │                         │  - SQLite Database Store                   │  │
│              │                         │  - WebSocket Event Hub & smcp Bridge       │  │
│              │                         └─────────────────────┬──────────────────────┘  │
└──────────────┼───────────────────────────────────────────────┼─────────────────────────┘
               │ HTTP / WebSockets                             │ Docker Go SDK
               ▼                                               ▼
┌─────────────────────────────┐                 ┌─────────────────────────────┐
│    Browser Dashboard        │                 │   Docker Container Sandbox  │
│  - Team & Project Switcher  │                 │                             │
│  - 4-Lane Kanban Board      │                 │  - Mount: /workspace (RW)   │
│  - Live xterm.js Terminal   │                 │  - Agent: OpenCode / Claude │
│  - GitHub/Jira Issue Badges │                 │  - Tool/MCP Execution       │
│  - Unified Git Diff Viewer  │                 │  - CapDrop: ALL, Non-Root   │
│  - One-Click Merge / Ship   │                 │                             │
└─────────────────────────────┘                 └─────────────────────────────┘
```

---

## 3. Team & Workspace Management

### 3.1 Organization Hierarchy
* **Team**: Workspace grouping engineers, repositories, and settings (e.g. `Backend Squad`, `Frontend Core`).
* **Project**: A specific codebase associated with a local repo path, a GitHub repo (`owner/repo`), and optionally a Jira project key (`PROJ`).
* **Members & Assignees**:
  * Tasks can be assigned to **Human Engineers** or **AI Agents** (`OpenCode`, `Claude Code`, `Codex`).

---

## 4. GitHub & Jira Synchronization Engine

The backend implements a pluggable `SyncProvider` interface:

```go
type SyncProvider interface {
    ID() string // "github" | "jira"
    FetchIssues(ctx context.Context, project *Project) ([]ExternalIssue, error)
    UpdateIssueStatus(ctx context.Context, externalID string, status string) error
    CreatePullRequest(ctx context.Context, project *Project, branch string, title string, body string) (string, error)
    AddComment(ctx context.Context, externalID string, comment string) error
}
```

### 4.1 GitHub Provider
* **Authentication**: GitHub Personal Access Token (PAT) or GitHub App.
* **Inbound Sync**: Polls or receives webhooks for open issues labeled with target tags (e.g. `agent-ready` or all open issues).
* **Outbound Transition**:
  * When agent starts: Labels issue as `in-progress:agent`.
  * When agent finishes: Pushes `task/<id>` branch to origin and calls GitHub API to create a Pull Request linking the issue (`Fixes #<number>`). Moves card to **Review**.

### 4.2 Jira Provider
* **Authentication**: Jira Cloud Domain, User Email, and API Token.
* **Inbound Sync**: Fetches issues via JQL (e.g., `project = KEY AND status in ("To Do", "Backlog")`).
* **Outbound Transition**:
  * When agent starts: Transitions Jira issue to *"In Progress"*.
  * When agent finishes: Transitions issue to *"In Review"* and posts an execution summary comment with diff stats and PR link.

---

## 5. Sandboxing & Security Controls

### 5.1 Host Filesystem Protection
* The host home directory (`~`, `~/.ssh`, `~/.bashrc`, `~/.config`) is **never** mounted into containers.
* **Only** the dedicated task worktree path (`.worktrees/task-<id>`) is mounted to `/workspace`.

### 5.2 Linux Security Hardening
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

### 5.3 MCP & Tool Containment
* Tools executed by the agent (e.g. bash commands, compilers, MCP servers) run **strictly inside the container**.
* Network isolation prevents containers from reaching host-only loopback services unless explicitly white-listed.

---

## 6. Multi-Agent Runner Architecture

The backend implements a modular runner interface:

```go
type AgentRunner interface {
    ID() string
    Name() string
    IsAvailable(ctx context.Context) (bool, error)
    GenerateContainerConfig(task *Task, worktreeDir string) (*container.Config, *container.HostConfig, error)
}
```

### 6.1 Supported Agents
1. **OpenCode Runner**:
   * Runs `opencode run "<prompt>" --model <model>`.
   * Also supports remote endpoints via OpenCode Server API (`opencode serve`).
2. **Claude Code Runner**:
   * Runs `claude -p "<prompt>" --dangerously-skip-permissions`.
   * Passes instructions via prompt payloads and `.claude/rules/`.
3. **OpenAI Codex Runner**:
   * Runs `codex exec "<prompt>"` non-interactively.

---

## 7. `smcp` Integration & Skill / MCP Injection

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

## 8. End-to-End Task Lifecycle

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Backlog    │ ──▶ │  In Progress │ ──▶ │    Review    │ ──▶ │     Done     │
│ (Create/Sync)│     │(Worktree+Run)│     │ (Diff & QA)  │     │(Merge & Prune│
└──────────────┘     └──────────────┘     └──────────────┘     └──────────────┘
```

### Phase 1: Creation & Sync (`Backlog`)
* Tasks can be created manually or ingested from GitHub Issues / Jira Tickets.
* Task attributes:
  * Team ID & Project ID
  * Title & Prompt
  * Assignee (Human or Agent: OpenCode / Claude / Codex)
  * External Sync Link (GitHub Issue # or Jira Key)
  * Selected Skills & MCP servers

### Phase 2: Execution (`In Progress`)
1. **Worktree Creation**:
   * Go runs `git worktree add .worktrees/task-<id> -b task/<id>-<slug>` in the project repo.
2. **Status Notification**:
   * Updates GitHub Issue (label `in-progress:agent`) and Jira (transition to "In Progress").
3. **Config Prep**:
   * Injects selected skills and sanitized MCP configs into the worktree.
4. **Docker Launch**:
   * Creates and starts the container mounting the worktree to `/workspace`.
5. **Real-Time Streaming**:
   * Go attaches to stdout/stderr via Docker SDK and streams frames to the WebSocket hub.
   * UI displays live ANSI output in `xterm.js`.
6. **Abort Handling**:
   * Clicking "Stop" sends `docker stop` & `docker rm -f`, gracefully halting execution.

### Phase 3: Review (`Review`)
* On container exit:
  * Container is destroyed.
  * Go executes `git diff main...task/<id>-<slug>` in the repository.
  * If connected to GitHub/Jira: creates Draft PR or updates Jira to "In Review".
  * Web UI displays changed files with diff syntax highlighting.
* **Actions Available**:
  * **Approve & Merge**: Merges branch into `main` (or merges GitHub PR) and cleans up worktree.
  * **Follow-up Prompt**: Spawns a new container in the *same* worktree to make revisions.
  * **Discard**: Deletes worktree and branches.

### Phase 4: Archival (`Done`)
* Go executes `git worktree remove .worktrees/task-<id>`.
* Task marked completed with duration, exit code, commit hash, and closed PR/Jira ticket.

---

## 9. Data Storage & Schema (SQLite)

```sql
CREATE TABLE IF NOT EXISTS teams (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    team_id TEXT NOT NULL,
    name TEXT NOT NULL,
    repo_path TEXT NOT NULL,
    github_repo TEXT, -- e.g. 'owner/repo'
    github_token TEXT,
    jira_host TEXT,   -- e.g. 'company.atlassian.net'
    jira_project_key TEXT,
    jira_email TEXT,
    jira_api_token TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(team_id) REFERENCES teams(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    team_id TEXT NOT NULL,
    title TEXT NOT NULL,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL, -- 'backlog', 'in_progress', 'review', 'done', 'failed'
    assignee_type TEXT DEFAULT 'agent', -- 'agent' | 'human'
    assignee_id TEXT, -- agent name or user name
    external_provider TEXT DEFAULT 'none', -- 'github', 'jira', 'none'
    external_id TEXT, -- e.g. '#42' or 'PROJ-102'
    external_url TEXT,
    pr_url TEXT,
    worktree_path TEXT,
    branch_name TEXT,
    agent_id TEXT NOT NULL,
    model TEXT,
    skills TEXT, -- JSON array of skill IDs
    mcp_servers TEXT, -- JSON array of MCP IDs
    container_id TEXT,
    exit_code INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY(team_id) REFERENCES teams(id) ON DELETE CASCADE
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

## 10. Verification & Testing Strategy

1. **Unit Tests (Go)**:
   * Worktree lifecycle: verify creation, branch isolation, and cleanup.
   * Docker SDK wrapper: mock container creation, volume binding, and capability dropping.
   * Sync provider mocks: verify GitHub and Jira issue parsing, status updates, and PR creation calls.
   * Task state machine: verify valid transitions (`backlog` &rarr; `in_progress` &rarr; `review` &rarr; `done`).
2. **Integration Tests**:
   * Launch a test Docker container executing a mock script, stream logs over WebSocket, and verify zero file leaks to the host.
   * Mock GitHub & Jira endpoints to verify synchronization and error handling.
3. **Frontend Component Tests**:
   * Kanban drag-and-drop state updates and team switcher.
   * Terminal connection and rendering via `xterm.js`.
   * Git diff rendering with shadcn components.

---

## 11. Next Steps

Upon review and approval of this specification:
1. Proceed to implementation planning using the `writing-plans` workflow.
2. Structure the codebase in `/home/ghug/Code/Go/agent-task-manager`.
3. Scaffold Go backend, Docker sandbox runner, and React/shadcn frontend.
