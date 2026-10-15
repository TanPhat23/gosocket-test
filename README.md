# Agent Task Manager (ATM)

> **Self-hostable autonomous coding control plane** orchestrating local [OpenCode](https://opencode.ai) engines, host-level Git worktree sandboxes, a 4-tier Asynq priority queue, live streaming ANSI terminals, and hierarchical subagent delegation.

[![Go Version](https://img.shields.io/badge/go-1.27-blue.svg)](https://golang.org)
[![React Version](https://img.shields.io/badge/react-19.0-61dafb.svg)](https://react.dev)
[![Docker](https://img.shields.io/badge/docker-compose-2496ed.svg)](https://www.docker.com)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

---

## Overview

**Agent Task Manager (ATM)** is an enterprise-grade control plane engineered to turn autonomous AI coding agents into reliable, reviewable engineering workflows. Rather than running code modifications directly inside shared environments or opaque cloud containers, ATM combines host-level Git worktree isolation with distributed worker queues and real-time streaming interfaces.

### Core Capabilities

- **Autonomous Coding Control Plane**: Full task lifecycle tracking (`backlog`, `queued`, `in_progress`, `review`, `done`, `failed`, `cancelled`) with automated multi-attempt execution and heartbeat monitoring.
- **Local OpenCode Engine Integration**: Seamlessly drives the local OpenCode binary or server runtime (`opencode run --server <url> --model <model> --auto <prompt>`) using active models such as Google Gemini 3.8 Flash, Claude Sonnet 4.5, GPT-5.5, and open weights.
- **Host Git Worktree Isolation**: Executes each task inside an isolated Git worktree (`git worktree add <dir> -b task/<id> origin/main`). Avoids Docker Snap `/tmp` mount traps, grants root/non-root file access via explicit permissions (`0777`), and isolates task diffs from the host repository.
- **Remote GitHub Auto-Sync**: Automatically checks and prunes/fetches configured remotes (`origin`) prior to branching, ensuring every agent workspace builds atop the freshest upstream commits.
- **4-Tier Asynq Priority Queue**: Distributed scheduling powered by Redis 7 and Asynq with priority-based queue distribution weights (`critical: 6`, `default: 3`, `low: 1`) mapped from task priorities (Urgent `100`, High `75`, Normal `50`, Low `25`).
- **Live Streaming xterm.js Terminal with Theme Matching**: Real-time ANSI terminal logs streamed directly from container stdout/stderr over WebSockets (`/api/v1/ws/terminal/:task_id`), supporting Dark, Light, and Matrix themes synchronized with application UI styling.
- **Hierarchical Subagent Delegation**: Running agents automatically receive environment markers (`ATM_TASK_ID`, `ATM_PROJECT_ID`, `ATM_API_URL`) allowing them to autonomously file, trigger, and inspect child subtasks via REST calls or scripts.
- **OIDC & Multi-Tenant RBAC**: Integrated out-of-the-box with Dex OIDC identity provider, AES-256-GCM secret encryption, and 4-tier role hierarchies (`Viewer`, `Member`, `Admin`, `Owner`).

---

## Architecture & Technology Stack

```
                                  [ Browser / Client ]
                                           │
                        ┌──────────────────┴──────────────────┐
                        │   Port 80 / 443 (HTTP & WebSocket)  │
                        ▼                                     ▼
                ┌─────────────────────────────────────────────────────┐
                │             Traefik Reverse Proxy Ingress           │
                │        (Security Headers, WebSocket Upgrades)       │
                └──────────────────┬─────────────────┬────────────────┘
                                   │                 │
                Path: / /assets    │                 │ Path: /api /ws
                                   ▼                 ▼
             ┌───────────────────────────────┐  ┌─────────────────────────────────┐
             │       Web UI (Nginx)          │  │    Control Plane (Fiber v3)     │
             │   React 19 + Vite 6 + Tailwind│  │   Go 1.27 + GORM + WebSocket    │
             │     xterm.js + Zustand Store  │  └──────────────┬──────────────────┘
             └───────────────────────────────┘                 │
                                                               │
                         ┌─────────────────────────────────────┼────────────────────────┐
                         │                                     │                        │
                         ▼                                     ▼                        ▼
         ┌───────────────────────────────┐     ┌────────────────────────┐     ┌───────────────────┐
         │     PostgreSQL 18 Alpine      │     │      Redis 7 Alpine    │     │   Dex OIDC v2.41  │
         │ (Tasks, Logs, RBAC, Projects) │     │ (Asynq 4-Tier Queue)   │     │ (Identity Auth)   │
         └───────────────────────────────┘     └───────────┬────────────┘     └───────────────────┘
                                                           │
                                                           ▼
                                               ┌────────────────────────┐
                                               │   Asynq Worker Server  │
                                               │ (Concurrency: Bounded) │
                                               └───────────┬────────────┘
                                                           │
                                                           ▼
                                               ┌────────────────────────┐
                                               │    AgentTaskRunner     │
                                               └───────────┬────────────┘
                                                           │
                                ┌──────────────────────────┴──────────────────────────┐
                                ▼                                                     ▼
                ┌───────────────────────────────┐                     ┌───────────────────────────────┐
                │   Host Worktree Manager       │                     │    Execution Sandbox          │
                │  - Clones / Branches off base │                     │  - Docker Container Sandbox   │
                │  - Generates git diff snapshots│                    │  - Local OpenCode Host Engine │
                └───────────────────────────────┘                     └───────────────────────────────┘
```

| Layer | Technologies |
|---|---|
| **Ingress** | Traefik v3, Let's Encrypt ACME, Custom Security Headers |
| **Frontend** | React 19, TypeScript 5.7, Vite 6, Tailwind CSS 4, xterm.js 6, Zustand 5, Base UI / shadcn |
| **Backend & API** | Go 1.27, Fiber v3 (`gofiber/fiber/v3`), fasthttp/websocket |
| **Database & ORM** | PostgreSQL 18 Alpine, GORM 1.31, AES-256-GCM Credential Encryption |
| **Queue & Cache** | Redis 7 Alpine, Hibiken Asynq 0.26 (Weighted Priority Queues) |
| **Auth & Identity** | Dex OIDC 2.41, CoreOS go-oidc v3, HttpOnly secure cookies |
| **Agent Engine** | OpenCode Local CLI & Server, Docker Engine API (`moby/moby`) |

---

## Prerequisites

Before starting the control plane, verify the following dependencies on the host machine:

- **Docker & Docker Compose**: Docker Engine 24+ and Docker Compose v2.20+
- **Git**: Git 2.38+ installed on the host
- **Go** (for local development): Go 1.27 or higher
- **Node.js & Package Manager** (for local UI development): Node.js 22 LTS with `bun` or `npm`

---

## Quickstart Guide

### 1. Clone & Configure

```bash
git clone https://github.com/ghug/agent-task-manager.git
cd agent-task-manager

# Copy the sample environment file
cp .env.example .env
```

Review `.env` and verify key configuration variables:
- `SECRET_ENCRYPTION_KEY`: A 32-character string used for AES-256-GCM credential encryption.
- `SESSION_SECRET`: Session cookie signing key (at least 32 characters).
- `HOST_WORKTREE_ROOT`: Absolute host directory where task worktrees will be created (e.g. `/home/user/agent-task-manager/data/worktrees`).
- `HOST_REPO_ROOT`: Absolute host directory where target repositories live (e.g. `/home/user/agent-task-manager/data/repos`).

### 2. Launch with Docker Compose

Run the entire stack in the background:

```bash
docker compose up -d
```

This starts:
- `agent-traefik` on ports `80` and `443`
- `agent-postgres` on internal port `5432`
- `agent-redis` on internal port `6379`
- `agent-dex` on internal port `5556` (mapped at `http://localhost/dex`)
- `agent-control-plane` on internal port `8080` (mapped at `http://localhost/api` and `http://localhost/ws`)
- `agent-web-ui` on internal port `80` (mapped at `http://localhost/`)

### 3. Access the Dashboard & Default Credentials

Open your browser to:
```
http://localhost
```

The system includes pre-seeded test accounts via Dex:
- **Admin**: `admin@example.com` / `password123`
- **Developer**: `developer@example.com` / `password123`

When launched for the first time, the control plane automatically provisions a default `Core Team` and `Core Workspace` project linked to your host repository root.

---

## Configuration & Environment Variables

The control plane is configured via environment variables defined in `.env` or passed to container runtimes:

| Variable | Default Value | Description |
|---|---|---|
| `APP_ENV` | `development` | Runtime environment (`development` or `production`). Enables secure cookie flags in production. |
| `PORT` | `8080` | Control plane HTTP/WebSocket server listening port. |
| `DOMAIN` | `localhost` | Host domain for Traefik routing rules and OIDC issuer resolution. |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/agent_tasks?sslmode=disable` | PostgreSQL connection string. |
| `DATABASE_STATEMENT_TIMEOUT_SECONDS` | `15` | Maximum execution time in seconds for SQL statements. |
| `DATABASE_CONNECT_TIMEOUT_SECONDS` | `5` | Maximum timeout for acquiring database connections. |
| `REDIS_ADDR` | `localhost:6379` | Host and port of Redis 7 instance for Asynq. |
| `REDIS_PASSWORD` | *(empty)* | Redis authentication password. |
| `SECRET_ENCRYPTION_KEY` | *(32-byte string)* | **Mandatory 32-byte key** for AES-256-GCM credential encryption. |
| `SESSION_SECRET` | *(string)* | Secret string used to sign session cookies (minimum 32 characters). |
| `MAX_CONCURRENT_TASKS` | `4` | Worker pool concurrency limit for simultaneous agent runs. |
| `TASK_TIMEOUT_MINUTES` | `60` | Maximum execution duration before an agent task attempt is timed out. |
| `HOST_WORKTREE_ROOT` | `/srv/agent-tasks/worktrees` | Host filesystem directory where task Git worktrees are created. |
| `HOST_REPO_ROOT` | `/srv/agent-tasks/repos` | Host directory containing base Git repositories. |
| `OPENCODE_SERVER_URL` | `http://127.0.0.1:49374` | Address of the local OpenCode server engine. |
| `OPENCODE_PASSWORD` | *(empty)* | Authentication token/password for the OpenCode server API. |
| `OPENCODE_BIN_PATH` | `/home/ghug/.opencode/bin/opencode` | Path to the local `opencode` binary executable. |
| `OPENCODE_HOME_PATH` | `/home/ghug/.opencode` | Host path mapped to agent sandbox container `/root/.opencode`. |
| `OPENCODE_CONFIG_DIR`| `/home/ghug/.config/opencode` | Host OpenCode config dir (read for `service.json`); no longer mounted into task containers. |
| `OIDC_ISSUER_URL` | `http://localhost/dex` | Public OIDC issuer URL for browser authentication redirects. |
| `OIDC_INTERNAL_ISSUER_URL` | `http://dex:5556/dex` | Internal OIDC issuer URL reachable by the backend container. |
| `OIDC_CLIENT_ID` | `agent-control-plane` | Registered OIDC client ID. |
| `OIDC_CLIENT_SECRET` | `dev-oidc-secret-change-in-prod-1234` | Registered OIDC client secret. |
| `OIDC_REDIRECT_URL` | `http://localhost/api/v1/auth/callback` | OAuth2 redirect callback endpoint. |

---

## Documentation Index

Detailed architectural and developer guides are available in the [`docs/`](./docs) directory:

- [**System Architecture & Lifecycle (`docs/ARCHITECTURE.md`)**](./docs/ARCHITECTURE.md): Component diagrams, Docker volume bind mounts, Git worktree isolation lifecycle, and real-time WebSocket protocol specifications.
- [**REST & WebSocket API Reference (`docs/API.md`)**](./docs/API.md): Complete API documentation with request schemas, responses, and verified `curl` examples.
- [**Subagents & Autonomous Delegation (`docs/SUBAGENTS_AND_DELEGATION.md`)**](./docs/SUBAGENTS_AND_DELEGATION.md): Guide to environment variables (`ATM_TASK_ID`), recursive subtask dispatching, and parent-child hierarchy tracking.
