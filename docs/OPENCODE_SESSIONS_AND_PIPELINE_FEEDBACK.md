# OpenCode Session Architecture & Pipeline Failure Feedback Loops

## 1. Overview & Architectural Goals

In **Agent Task Manager (ATM)**, coding agents work within isolated Git worktrees to solve development tasks autonomously. To achieve reliable self-healing software engineering workflows, ATM integrates deeply with **OpenCode**.

This guide details:
1. **OpenCode Session Architecture**: How sessions function, differences between ephemeral and persistent sessions, and storage mechanics.
2. **Integration Options**: Comparing the CLI runtime (`opencode run`) and OpenCode's background HTTP REST server (`http://127.0.0.1:49374`).
3. **Current vs. Target Workflow**: Moving from blind retries to informed iterative refinement.
4. **Local In-Worktree Pre-Flight Verification Loop**: Autonomous test/linter verification and self-repair before code review or PR creation.
5. **Remote CI/CD Webhook Feedback Loop**: Ingesting GitHub Actions `check_run` failures to repair PRs autonomously.
6. **Production Configuration & Concrete Code Snippets**: Implementation patterns ready for ATM services.

---

## 2. OpenCode Session Architecture

OpenCode manages conversation state, tool invocations, file read caches, and agent thinking blocks inside a structured **Session**.

```
┌────────────────────────────────────────────────────────────────────────┐
│                        OpenCode Background Server                      │
│                          (http://127.0.0.1:49374)                       │
│                                                                        │
│   ~/.local/share/opencode/opencode.db (SQLite / Event Sourced Store)   │
│   ┌────────────────────────────────────────────────────────────────┐   │
│   │ Session: ses_atm_1d803621-e374-4ec5-b220-d3a95d5fe038          │   │
│   │ ├── Metadata: { title: "ATM Task: Fix JWT Expiry", ... }       │   │
│   │ ├── Timeline Messages:                                         │   │
│   │ │   ├── [User Prompt]     "Fix the token expiry check..."      │   │
│   │ │   ├── [Assistant / Tool] ReadFile, EditFile, RunCommand       │   │
│   │ │   ├── [User / System]   "Pre-flight failed: 1 test broken"   │   │
│   │ │   └── [Assistant / Tool] Patch applied, tests pass!          │   │
│   │ └── Model Context State & Tool Memory                          │   │
│   └────────────────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────────────────┘
```

### Ephemeral vs. Persistent Sessions

| Attribute | Ephemeral Sessions (Previous Default) | Persistent Sessions (`ses_atm_<task_id>`) |
|---|---|---|
| **Session Identification** | Random auto-generated ID per process invocation (e.g. `ses_3b8f91a`) | Deterministic ID mapped to task: `ses_atm_<task.id>` |
| **CLI Arguments** | `opencode run --server ... "<prompt>"` | `opencode run --server ... --session "ses_atm_<id>" --title "..." "<prompt>"` |
| **Context Across Retries** | **Lost**. Each retry restarts from a blank slate with zero memory. | **Preserved**. The agent retains knowledge of files inspected, edits made, and prior hypotheses. |
| **Tool Trace History** | Discarded on process termination. | Retained across runs; visible in OpenCode UI and accessible via REST API. |
| **Token Utilization** | Repeated reads of identical project files across attempts. | High cache hit rate; agent immediately focuses on failure diffs. |
| **Failure Feedback** | Impossible to feed compiler/test errors back into the ongoing reasoning stream. | Clean prompt injection into the existing message timeline. |

### Storage & Filesystem Topology
- **Binary Path**: Configured via `OPENCODE_BIN_PATH` (default: `/home/ghug/.opencode/bin/opencode` or `opencode` in `$PATH`).
- **Data Directory**: `~/.local/share/opencode` (`OPENCODE_DATA_DIR`) contains session databases, history, and artifacts.
- **Config & Credentials**: `~/.config/opencode/service.json` contains the daemon authentication password used for HTTP Basic authentication.

---

## 3. Integration Modes: CLI vs. Background REST Server

ATM utilizes a hybrid integration strategy, selecting the right interface for each operational phase:

```
                  ┌──────────────────────────────────────────────┐
                  │           Agent Task Manager (ATM)           │
                  └──────────────┬────────────────┬──────────────┘
                                 │                │
            Heavy Sandbox Run    │                │ Asynchronous Follow-up &
           (Stdout/Stderr Piped) │                │ Status Queries
                                 ▼                ▼
                     ┌────────────────┐     ┌────────────────────────┐
                     │  CLI Invocation │     │ OpenCode Server REST   │
                     │ `opencode run` │     │ `http://127.0.0.1:49374│
                     └───────┬────────┘     └───────────┬────────────┘
                             │                          │
                             ▼                          ▼
                     ┌────────────────────────────────────────┐
                     │          Session Context Store         │
                     │   `ses_atm_<task_id>` Timeline State   │
                     └────────────────────────────────────────┘
```

### Mode Comparison

| Feature | CLI (`opencode run`) | Server REST API (`/api/session/...`) |
|---|---|---|
| **Primary Use Case** | Initial task execution, major attempts, and sandbox execution within Docker. | Follow-up prompts, error injection, session status inspection, and history extraction. |
| **Sandbox Isolation** | Runs inside Docker (`debian:bookworm-slim`) mounting worktrees, config, and data dirs. | Executes on the OpenCode server daemon process directly. |
| **Output Streaming** | Emits real-time stdout/stderr lines piped directly into ATM's WebSocket terminal stream. | Emits JSON events or requires polling `/api/session/{id}/message`. |
| **Authentication** | Automatically inherits server authentication or reads `OPENCODE_PASSWORD`. | Requires HTTP Basic Auth `opencode:<service_password>`. |
| **Continuation Semantics** | Flags: `--session <id>`, `--continue` (`-c`), `--fork`. | Endpoint: `POST /api/session/{sessionID}/prompt`. |

---

## 4. Current vs. Target Workflow

### Current Workflow (Broken Feedback)

```
[Task Queued]
     │
     ▼
[Attempt 1 Started] ──> opencode run (Ephemeral Session ses_xyz)
     │
     ├── Agent modifies code
     ├── Local test fails (Exit 1)
     ▼
[Attempt 1 FAILED]
     │
     ▼ (Retry triggered)
[Attempt 2 Started] ──> opencode run (NEW Ephemeral Session ses_abc)
     │
     ├── Agent has NO memory of Attempt 1!
     ├── Re-reads repo from scratch, repeats similar failing edit
     ▼
[Attempt 2 FAILED] ──> Task marked permanently FAILED
```

### Target Workflow (Continuous Feedback Loops)

```
[Task Queued]
     │
     ▼
[Task Attempt 1] ──> opencode run --session ses_atm_<id> --title "Task <title>"
     │
     ▼
[In-Worktree Pre-Flight Verification]
     │
     ├─ Tests Pass ───────────────────────────────────────┐
     │                                                    │
     └─ Tests FAIL (e.g., compile error, panic, assertion)│
            │                                             │
            ▼                                             │
   [Capture Stdout/Stderr]                                │
            │                                             │
            ▼                                             │
   [Loop: Re-prompt ses_atm_<id> with Error Logs]         │
            │                                             │
            ├── Agent: "I see the type error at line 42." │
            ├── Agent applies targeted fix                │
            └── Re-run Pre-Flight Verification            │
                    │                                     │
                    ├── Succeeded!                        │
                    ▼                                     ▼
        [Git Commit & Diff Snapshot] ◄────────────────────┘
                    │
                    ▼
        [Create Pull Request (GitHub)]
                    │
                    ▼
        [Remote CI/CD Pipeline (GitHub Actions)]
                    │
                    ├─ CI Check Passed ──> Ready for Review!
                    │
                    └─ CI Check Failed
                            │
                            ▼
               [ATM Webhook Ingests check_run]
                            │
                            ▼
               [Extract CI Annotations & Logs]
                            │
                            ▼
               [Re-engage ses_atm_<id> on Existing Worktree Branch]
                            │
                            ▼
               [Agent Fixes -> Git Push -> CI Re-runs]
```

---

## 5. Local In-Worktree Pre-Flight Verification Loop

### Operational Pattern

Before ATM marks a task as `review` or opens a Pull Request, the runner executes a configured pre-flight verification script inside the worktree directory.

If verification fails:
1. ATM extracts the failure logs (trimmed to the last 200 lines / 16KB to prevent context blowup).
2. ATM formats a structured diagnostic prompt.
3. ATM invokes OpenCode targeting the existing session `ses_atm_<task.id>`.
4. The process iterates until tests pass or the max iteration limit (default: 3) is reached.

### Implementation Reference (Go)

```go
package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"agent-task-manager/internal/models"
)

type VerificationResult struct {
	Passed bool
	Output string
}

func (r *AgentTaskRunner) RunPreFlightVerification(ctx context.Context, worktreeDir, verifyCmd string) (*VerificationResult, error) {
	if verifyCmd == "" {
		return &VerificationResult{Passed: true}, nil
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", verifyCmd)
	cmd.Dir = worktreeDir
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	err := cmd.Run()
	if err != nil {
		return &VerificationResult{
			Passed: false,
			Output: outBuf.String(),
		}, nil
	}

	return &VerificationResult{Passed: true, Output: outBuf.String()}, nil
}

func (r *AgentTaskRunner) ExecuteWithFeedbackLoop(
	ctx context.Context,
	task *models.Task,
	attempt *models.TaskAttempt,
	worktreeDir,
	model string,
	maxRetries int,
) error {
	sessionID := fmt.Sprintf("ses_atm_%s", task.ID)
	prompt := task.Prompt

	for iteration := 1; iteration <= maxRetries; iteration++ {
		// 1. Execute agent turn within the persistent session
		err := r.runInDockerOrHost(ctx, task, attempt, worktreeDir, model, sessionID, prompt)
		if err != nil {
			return fmt.Errorf("agent execution failed in iteration %d: %w", iteration, err)
		}

		// 2. Run local pre-flight checks (e.g. "go test ./...")
		verifyRes, err := r.RunPreFlightVerification(ctx, worktreeDir, "go test ./...")
		if err != nil {
			return fmt.Errorf("verification script execution error: %w", err)
		}

		if verifyRes.Passed {
			r.logRepo.Append(task.ID, &attempt.ID, fmt.Sprintf("Pre-flight passed on iteration %d\n", iteration))
			return nil
		}

		// 3. Prepare structured feedback prompt for the next loop iteration
		prompt = fmt.Sprintf(
			"Pre-flight verification failed (Iteration %d/%d) with the following error output:\n\n"+
				"```\n%s\n```\n\n"+
				"Please review the error output, inspect the relevant files, fix the root cause, "+
				"and ensure all tests pass.",
			iteration, maxRetries, truncateLog(verifyRes.Output, 200),
		)
	}

	return fmt.Errorf("pre-flight verification failed after %d iterations", maxRetries)
}
```

---

## 6. Remote CI/CD Webhook Feedback Loop

### GitHub Actions `check_run` Flow

When code is pushed to a remote branch and a GitHub Actions workflow executes, failures can be automatically fed back into ATM:

```
GitHub Actions Runner          ATM Webhook Handler             AgentTaskRunner
         │                              │                             │
         │ check_run.completed (failure)│                             │
         ├─────────────────────────────>│                             │
         │                              ├── Verify signature & match  │
         │                              │   task by branch/commit     │
         │                              ├── Fetch run failure logs    │
         │                              │                             │
         │                              ├─ Dispatch Asynq Retry Job ─>│
         │                              │                             ├── Attach to worktree
         │                              │                             ├── Target ses_atm_<task.id>
         │                              │                             ├── Ingest CI failure logs
         │                              │                             ├── Agent repairs code
         │                              │                             ├── Git commit & push
         │   git push origin task/...   │                             │
         │<───────────────────────────────────────────────────────────┤
         │                              │                             │
    [CI Re-runs]                        │                             │
```

### Webhook Event Handler Implementation

```go
package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/service"
)

type CheckRunPayload struct {
	Action   string `json:"action"`
	CheckRun struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadSHA    string `json:"head_sha"`
		CheckSuite struct {
			HeadBranch string `json:"head_branch"`
		} `json:"check_suite"`
		Output struct {
			Title   string `json:"title"`
			Summary string `json:"summary"`
			Text    string `json:"text"`
		} `json:"output"`
	} `json:"check_run"`
}

func HandleGitHubCheckRun(taskService service.TaskService, w http.ResponseWriter, r *http.Request) {
	var payload CheckRunPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if payload.CheckRun.Conclusion != "failure" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Match task by branch name (e.g. task/<task_id>)
	task, err := taskService.FindByBranch(r.Context(), payload.CheckRun.CheckSuite.HeadBranch)
	if err != nil || task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	feedbackPrompt := fmt.Sprintf(
		"Remote CI check '%s' failed on commit %s.\n\n"+
			"Title: %s\nSummary: %s\nDetails:\n```\n%s\n```\n\n"+
			"Please inspect the failure, correct the issues in the worktree, and ensure all checks succeed.",
		payload.CheckRun.Name,
		payload.CheckRun.HeadSHA,
		payload.CheckRun.Output.Title,
		payload.CheckRun.Output.Summary,
		payload.CheckRun.Output.Text,
	)

	// Enqueue automated repair attempt referencing existing session
	_ = taskService.TriggerSelfRepairAttempt(r.Context(), task.ID, feedbackPrompt)
	w.WriteHeader(http.StatusAccepted)
}
```

---

## 7. OpenCode Server REST API Integration

When sending asynchronous prompts to an existing session without spawning a brand-new CLI process, the OpenCode background server REST API is invoked directly.

### Authentication
OpenCode requires HTTP Basic Authentication:
- **Username**: `opencode`
- **Password**: Found in `~/.config/opencode/service.json` under `"password"` or via environment variable `OPENCODE_PASSWORD`.

```bash
# Example curl request to prompt an existing session
curl -X POST "http://127.0.0.1:49374/api/session/ses_atm_1d803621-e374-4ec5-b220-d3a95d5fe038/prompt" \
  -u "opencode:$(jq -r .password ~/.config/opencode/service.json)" \
  -H "Content-Type: application/json" \
  -d '{
    "text": "Tests failed: expected status code 200, got 500 in auth_test.go:42. Please fix.",
    "resume": true
  }'
```

### Go HTTP Client Implementation

```go
package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	password   string
	httpClient *http.Client
}

func NewClient(baseURL, password string) *Client {
	return &Client{
		baseURL:  baseURL,
		password: password,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type PromptRequest struct {
	Text   string `json:"text"`
	Resume *bool  `json:"resume,omitempty"`
}

func (c *Client) SendPrompt(ctx context.Context, sessionID, text string) error {
	url := fmt.Sprintf("%s/api/session/%s/prompt", c.baseURL, sessionID)
	resume := true
	body, err := json.Marshal(PromptRequest{
		Text:   text,
		Resume: &resume,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("opencode", c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to dispatch prompt to opencode: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("opencode returned non-2xx status: %d", resp.StatusCode)
	}

	return nil
}
```

---

## 8. CLI Command Invocations

When launching via `AgentTaskRunner` in Docker or on the host, always supply `--session` and `--title`:

### Docker Sandbox Execution (`runInDocker`)

```bash
docker run \
  --network host \
  -v /data/worktrees/1d803621:/data/worktrees/1d803621:rw \
  -v /home/ghug/.opencode:/root/.opencode:ro \
  -v /home/ghug/.config/opencode:/root/.config/opencode:ro \
  -v /home/ghug/.local/share/opencode:/root/.local/share/opencode:rw \
  -w /data/worktrees/1d803621 \
  -e HOME=/root \
  -e OPENCODE_PASSWORD="$OPENCODE_PASSWORD" \
  -e ATM_TASK_ID="1d803621-e374-4ec5-b220-d3a95d5fe038" \
  debian:bookworm-slim \
  /root/.opencode/bin/opencode run \
    --server "http://127.0.0.1:49374" \
    --session "ses_atm_1d803621-e374-4ec5-b220-d3a95d5fe038" \
    --title "ATM: Fix JWT Expiry (#1d803621)" \
    --model "claude-3-7-sonnet" \
    --auto \
    "Fix JWT token expiry calculation in internal/auth/jwt.go"
```

### Host Fallback Execution (`runOnHost`)

```bash
cd /data/worktrees/1d803621-e374-4ec5-b220-d3a95d5fe038 && \
opencode run \
  --server "http://127.0.0.1:49374" \
  --session "ses_atm_1d803621-e374-4ec5-b220-d3a95d5fe038" \
  --title "ATM: Fix JWT Expiry (#1d803621)" \
  --model "claude-3-7-sonnet" \
  --auto \
  "Fix JWT token expiry calculation in internal/auth/jwt.go"
```

---

## 9. Failure Modes & Safety Guarantees

1. **Loop Termination**:
   - Every feedback loop must have a hard iteration ceiling (e.g., maximum 3 local verification iterations and maximum 2 remote CI attempts).
   - If the budget is exhausted, the task transitions to `failed` and requests human review.
2. **Context Blowup & Compaction**:
   - Long test outputs must be truncated to the relevant stack trace / error hunk (last 100-200 lines).
   - If session token counts approach provider limits, call `POST /api/session/{sessionID}/compact` to summarize previous turns.
3. **Worktree Hygiene**:
   - The runner never deletes the worktree between iterative repair turns; intermediate edits remain on the task's Git branch.
   - When the task is accepted or permanently terminated, ATM cleans up the worktree via `git worktree remove --force`.
