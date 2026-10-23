# GitHub Integration & End-to-End PR Automation Design Specification

- **Date**: 2026-10-07
- **Status**: Approved
- **Target Release**: Agent Task Manager v1.2.0

---

## 1. Executive Summary & Goals

Agent Task Manager (ATM) is an autonomous agent control plane designed to run coding tasks inside sandboxed Git worktrees with local OpenCode runtimes. This specification establishes a complete, production-grade GitHub lifecycle connecting GitHub issues, repositories, and pull requests directly into the ATM control plane:

1. **Project Integrations & Credential Management**: Secure storage of GitHub Personal Access Tokens (PATs) using AES-256-GCM encryption and project-level bindings (`owner/repo`) configured via REST API and a dashboard UI dialog.
2. **Inbound Webhook Automation**: Ingestion of GitHub webhook events (`issues.opened`, `issues.reopened`, `issues.labeled`) with HMAC-SHA256 signature verification, optional label filtering, deduplication, and automatic task creation.
3. **Outbound Git Push & Automated Pull Requests**: Capability for the Agent Task Runner to push worktree branches (`task/<task_id>`) directly to the remote GitHub repository and automatically create Pull Requests with diff summaries and issue links (`Resolves #<issue_number>`).
4. **On-Demand Manual PR Trigger**: A dedicated endpoint (`POST /api/v1/tasks/:id/pr`) and dashboard Task Inspector button allowing human developers to review worktree diffs and trigger PR creation on demand.
5. **Real-time Status Synchronization**: Live propagation of issue and PR links across the Kanban board, Task Inspector, and WebSocket event channels (`/api/v1/ws/events`).

---

## 2. Architecture & Data Flow

```
+-----------------------------------------------------------------------------------+
|                                     GitHub                                        |
|  +--------------------+                                   +--------------------+  |
|  |    GitHub Issue    |                                   |    Pull Request    |  |
|  |  (opened/labeled)  |                                   |     (created)      |  |
|  +---------+----------+                                   +---------^----------+  |
+------------|--------------------------------------------------------|-------------+
             | Webhook Payload                                        |
             | (X-Hub-Signature-256)                                  | REST API:
             v                                                        | POST /repos/.../pulls
+------------------------------------+              +-----------------+------------------+
| Ingress & Control Plane            |              | Agent Task Runner                  |
| POST /api/v1/webhooks/github       |              | - Local worktree execution         |
| - Verify HMAC (webhook_secret)     |              | - Unified diff snapshot            |
| - Deduplicate delivery ID          |              | - Authenticated git push           |
| - Filter by label (if configured)  |              |   (git -c http.extraHeader=...)    |
| - Create Task (status: queued)     |              | - Automated or Manual PR trigger   |
+-----------------+------------------+              +-----------------+------------------+
                  |                                                   ^
                  | Enqueue Asynq Job                                 | Dequeue Task
                  v                                                   |
+------------------------------------+              +-----------------+------------------+
| Redis 7 & Asynq Priority Queue     |------------->| Asynq Worker Pool                  |
+------------------------------------+              +------------------------------------+
```

---

## 3. Data Models & Database Schema

### 3.1 Existing Models Utilized
The system leverages existing GORM models in `internal/models/models.go` and `internal/models/task_models.go`:

1. **`models.Credential`**:
   - `ID` (UUID, PK)
   - `TeamID` (UUID, FK)
   - `Provider` = `models.ProviderGitHub` (`"github"`)
   - `Label` (string, e.g. `"Primary GitHub PAT"`)
   - `EncryptedData` (AES-256-GCM string containing PAT token)
   - `KeyFingerprint` (string)

2. **`models.ProjectIntegration`**:
   - `ID` (UUID, PK)
   - `ProjectID` (UUID, FK to `projects`)
   - `CredentialID` (UUID, FK to `credentials`)
   - `Provider` = `models.IntegrationGitHub` (`"github"`)
   - `ExternalID` (string, e.g. `"octocat/hello-world"`)
   - `ExternalURL` (string, e.g. `"https://github.com/octocat/hello-world"`)
   - `Enabled` (bool, default `true`)
   - `Config` (`models.JSONMap`):
     ```json
     {
       "base_branch": "main",
       "auto_pr": true,
       "webhook_secret": "whsec_1234567890abcdef",
       "filter_label": "agent"
     }
     ```

3. **`models.Task` Enhancements**:
   - `PullRequestURL` (`*string`, json: `pull_request_url,omitempty`): Persists the URL of the created GitHub PR (e.g. `https://github.com/owner/repo/pull/12`).
   - `ExternalID` (`*string`): Stores GitHub issue number (e.g. `"42"`).
   - `ExternalURL` (`*string`): Stores GitHub issue web URL.
   - `IntegrationID` (`*string`): Foreign key referencing the active `ProjectIntegration`.

---

## 4. Backend Specifications

### 4.1 Integration Repository & Service

#### Repository Interface (`internal/repository/integration_repository.go`)
```go
type IntegrationRepository interface {
    Create(ctx context.Context, integ *models.ProjectIntegration) error
    GetByID(ctx context.Context, id string) (*models.ProjectIntegration, error)
    ListByProjectID(ctx context.Context, projectID string) ([]models.ProjectIntegration, error)
    GetActiveByProviderAndExternalID(ctx context.Context, provider models.IntegrationProvider, externalID string) (*models.ProjectIntegration, error)
    Update(ctx context.Context, integ *models.ProjectIntegration) error
    Delete(ctx context.Context, id string) error
}
```

#### Service Layer (`internal/service/integration_service.go`)
- `CreateIntegration(ctx, projectID, req)`:
  - Validates repository slug format: `^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`.
  - Verifies project exists.
  - Verifies credential exists and decrypts PAT.
  - Tests GitHub API access (`GET /repos/{owner}/{repo}`) using PAT.
  - Persists `ProjectIntegration` record.
- `ListIntegrations(ctx, projectID)`: Returns all integrations for the project.
- `GetIntegration(ctx, projectID, id)`: Returns specific integration.
- `UpdateIntegration(ctx, projectID, id, req)`: Allows toggling `enabled`, updating `auto_pr`, `base_branch`, `filter_label`, or changing credential.
- `DeleteIntegration(ctx, projectID, id)`: Removes the integration binding.

### 4.2 REST API Endpoints

| Method | Path | Description | Access |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/projects/:project_id/integrations` | List integrations for a project | Authenticated |
| `POST` | `/api/v1/projects/:project_id/integrations` | Create new integration | Authenticated |
| `GET` | `/api/v1/projects/:project_id/integrations/:id` | Get integration details | Authenticated |
| `PATCH` | `/api/v1/projects/:project_id/integrations/:id` | Update integration settings | Authenticated |
| `DELETE` | `/api/v1/projects/:project_id/integrations/:id` | Delete integration | Authenticated |
| `POST` | `/api/v1/tasks/:id/pr` | Manually push branch and create GitHub PR | Authenticated |
| `POST` | `/api/v1/webhooks/github` | Inbound GitHub webhook endpoint | Public (HMAC verified) |

### 4.3 Outbound Authenticated Git Push (`internal/git/push.go`)
To push commits to remote GitHub without writing plaintext secrets to `.git/config` or logging them in shell process tables:
```go
func PushBranch(ctx context.Context, worktreeDir, remoteURL, token, branchName string) error {
    authHeader := fmt.Sprintf("Authorization: Basic %s", base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)))
    cmd := exec.CommandContext(ctx, "git", "-c", "http.extraHeader="+authHeader, "push", "-u", "origin", fmt.Sprintf("%s:%s", branchName, branchName))
    cmd.Dir = worktreeDir
    // execute with bounded timeout
}
```

### 4.4 PR Generation in `AgentTaskRunner`
When a task finishes in `RunTask`:
1. Check if `task.ProjectID` has an active GitHub integration.
2. If `auto_pr == true` in config:
   - Run `PushBranch`.
   - Prepare PR Markdown:
     ```markdown
     ## Summary
     Autonomous implementation generated by Agent Task Manager for task `#{{.TaskID}}`.
     
     **Prompt**:
     > {{.Prompt}}
     
     ## Changes
     - {{.FilesChangedCount}} files changed (+{{.Additions}} / -{{.Deletions}})
     
     {{if .ExternalID}}
     Resolves #{{.ExternalID}}
     {{end}}
     ```
   - Call `githubProvider.CreatePullRequest(...)`.
   - Update `task.PullRequestURL` in database.
   - Broadcast status and PR URL over WebSocket (`/api/v1/ws/events`).
   - If `task.ExternalID != nil`, comment on GitHub issue with PR link and update labels (`review:agent`).

---

## 5. Frontend UI Specifications

### 5.1 Project Integration Modal (`ProjectIntegrationModal.tsx`)
1. **Access Point**: An "Integrations" icon button with GitHub logo located on the top dashboard bar next to the Project selection.
2. **Components**:
   - **Integration List View**: Displays existing connections with status (`Active`/`Disabled`), target repo (`owner/repo`), base branch, and last synced timestamp.
   - **Connect Dialog Form**:
     - Repo input (`owner/repo`).
     - Credential selector (existing PAT or quick-add new PAT).
     - Webhook configuration guidance:
       - Displays `http://<host>/api/v1/webhooks/github` with copy button.
       - Displays or generates a random webhook secret with copy button.
     - Settings:
       - `Auto-create Pull Request` (Toggle, default ON).
       - `Base Branch` (Text input, default `"main"`).
       - `Filter Label` (Text input, optional, e.g. `"agent"`).

### 5.2 Task Inspector Actions (`TaskInspectorModal.tsx`)
1. **Metadata Badges**:
   - If `task.external_url`: Displays `GitHub Issue #<id>` with external link.
   - If `task.pull_request_url`: Displays `GitHub PR #<id>` link with branch icon.
2. **Action Bar**:
   - If task status is `completed` and `task.pull_request_url` is empty:
     - Displays **"Create Pull Request"** button.
     - Clicking sends `POST /api/v1/tasks/:id/pr`.
     - Displays spinner while pushing and opening PR.
     - On success: dynamically updates task with PR link and shows a success toast.

---

## 6. Security & Hardening Checklist

1. **Secret Encryption**: All GitHub PATs must only be stored encrypted via `crypto.SecretCipher` (AES-256-GCM with unique 12-byte IVs).
2. **Webhook Verification**: Inbound webhook payloads MUST pass HMAC-SHA256 signature verification before any database mutation or event processing.
3. **Idempotency**: Webhook deliveries MUST check `X-GitHub-Delivery` against `idempotencyStore` to prevent duplicate task execution.
4. **Git Credential Scrubbing**: Token must be injected via in-memory `http.extraHeader` and NEVER persisted in `.git/config` on disk or exposed in error messages.

---

## 7. Testing Strategy

1. **Unit Tests**:
   - `internal/repository/integration_repository_test.go`: CRUD operations on GORM SQLite/Postgres.
   - `internal/service/integration_service_test.go`: Repository slug validation, token validation, error paths.
   - `internal/git/push_test.go`: Authenticated push command arguments and header formatting.
   - `internal/service/webhook_service_test.go`: HMAC signature verification, label filtering, issue mapping.
2. **Integration Tests**:
   - `internal/api/integration_handlers_test.go`: REST endpoints for listing, creating, patching, and deleting integrations.
   - `internal/api/task_handlers_test.go`: `POST /api/v1/tasks/:id/pr` manual PR creation endpoint with mock GitHub server.
3. **Frontend Component Tests**:
   - `web/src/features/integrations/ProjectIntegrationModal.test.tsx`: Form rendering, validation, submission.
   - `web/src/features/tasks/components/TaskInspectorModal.test.tsx`: "Create Pull Request" button appearance and interaction.
