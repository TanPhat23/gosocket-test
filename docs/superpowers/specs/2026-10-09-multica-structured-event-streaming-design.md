# Design Specification: Multica-Inspired Structured Event Streaming for Agent Task Manager

**Status:** Proposed  
**Author:** Antigravity / Engineering Coordinator  
**Date:** 2026-10-09  
**Target:** Agent Task Manager (`agent-task-manager`)  
**Reference Codebase:** Multica (`/home/ghug/Code/Go/multica`, `server/pkg/agent/opencode.go`, `stream_scanner.go`, `stream_json_result.go`)

---

## 1. Executive Summary & Problem Statement

### 1.1 Context
In **Agent Task Manager (ATM)**, coding agents execute autonomously within isolated Git worktrees. Agent execution occurs via the CLI runner (`opencode run` inside Docker or on the host) and via background steering prompts (`POST /api/v1/tasks/:id/session/prompt`).

### 1.2 Identified Problems
1. **Unstructured Output Scraping:** ATM historically piped raw TTY stdout from `opencode run` into `bufio.NewScanner` and broadcast raw strings over WebSockets. Tool calls, arguments, and outputs were mixed into terminal text with no structured telemetry.
2. **Buffer Overflow on Large Lines (64KB limit):** Standard Go `bufio.NewScanner` enforces `bufio.MaxScanTokenSize = 64 * 1024` (64KB). When agents return large tool outputs (e.g. `read` of 1000-line files, bundle builds, or large git diffs), standard scanners crash immediately with `bufio.ErrTooLong`, dropping the stream.
3. **Silent Failure & Premature Completion:** In non-structured streams, process exit code 0 does not prove task completion. If the CLI exits cleanly mid-tool without emitting a final response, or if intermediate turns stall, ATM has no way to distinguish a finished turn from an interrupted one.
4. **Disjointed UI Telemetry:** The web frontend (`AgentSessionView` vs. `TerminalView`) had to poll REST endpoints (`/api/session/:id/message`) every 800ms while simultaneously listening to raw ANSI terminal websockets, leading to out-of-sync transcripts and duplicate renders.

### 1.3 Solution
Port and integrate **Multica's Structured Event Streaming Architecture**:
- Run OpenCode with `--format json` to stream newline-delimited JSON events.
- Implement a 32MB-buffered `AgentStreamScanner` preventing `bufio.ErrTooLong`.
- Create a dedicated Go `EventParser` to transform raw stream events into typed lifecycle events (`step_start`, `tool_use`, `text`, `step_finish`).
- Broadcast structured `WSMessage` frames over WebSockets, giving both terminal ANSI logs and rich collapsible UI cards.
- Implement a fail-closed terminal completion contract (`sawTerminalSignal`).

---

## 2. Architecture & Data Flow

```
┌────────────────────────────────────────────────────────────────────────┐
│                        Execution Engine (Runner)                       │
│                                                                        │
│   opencode run --format json --auto "<prompt>"                         │
│   (stdin: prompt pipe | stdout: JSON stream | stderr: error tail)      │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ stdout (newline-delimited JSON)
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│               AgentStreamScanner (32MB Max Buffer)                     │
│                                                                        │
│   - Initial buffer: 1MB                                                │
│   - Max token size: 32MB (prevents ErrTooLong on big diffs/files)      │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ lines
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│               OpenCode Event Parser (event_parser.go)                  │
│                                                                        │
│   ├── event: "step_start"   ──> Record turn, emit step start           │
│   ├── event: "tool_use"     ──> Parse tool name, args, live output     │
│   ├── event: "text"         ──> Stream model reasoning tokens          │
│   ├── event: "step_finish"  ──> Capture tokens (in/out/reason/cache)   │
│   └── event: "error"        ──> Capture structured error payload       │
└──────────────────┬─────────────────────────────────┬───────────────────┘
                   │                                 │
                   │ Dual-Channel Broadcast          │ Persistence
                   ▼                                 ▼
┌───────────────────────────────────┐    ┌───────────────────────────────┐
│           WebSocket Hub           │    │    PostgreSQL `task_logs`     │
│                                   │    │                               │
│  Channel 1: type: "agent_event"   │    │  Persists formatted ANSI log  │
│  (Structured JSON for UI cards)   │    │  chunks for permanent replay  │
│                                   │    └───────────────────────────────┘
│  Channel 2: type: "log"           │
│  (ANSI escape stream for xterm)   │
└──────────────────┬────────────────┘
                   │
                   ▼ WebSocket: /api/v1/ws/terminal/:taskId
┌────────────────────────────────────────────────────────────────────────┐
│                            React Frontend                              │
│                                                                        │
│  ┌───────────────────────────────┐   ┌──────────────────────────────┐  │
│  │         TerminalView          │   │       AgentSessionView       │  │
│  │ (xterm.js ANSI live stream)   │   │ (Structured tool cards, live │  │
│  │ - Auto-scrolling              │   │  spinners, diff inspections) │  │
│  └───────────────────────────────┘   └──────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Detailed Component Specifications

### 3.1 Resilient Stream Scanner (`internal/runner/stream_scanner.go`)

Adapted from Multica (`server/pkg/agent/stream_scanner.go`), the scanner provides dynamic buffer growth up to 32MB:

```go
package runner

import (
	"bufio"
	"io"
)

const (
	// AgentStreamMaxLineBytes bounds a single line read from an agent CLI's event stream (32 MiB).
	// Prevents bufio.ErrTooLong when agents emit massive tool outputs (e.g. read_file of large files or large git diffs).
	AgentStreamMaxLineBytes = 32 * 1024 * 1024

	// AgentStreamInitialBufferBytes is the scanner's initial allocation (1 MiB).
	AgentStreamInitialBufferBytes = 1024 * 1024
)

// NewAgentStreamScanner returns a bufio.Scanner sized for high-throughput agent JSON streams.
func NewAgentStreamScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, AgentStreamInitialBufferBytes), AgentStreamMaxLineBytes)
	return scanner
}
```

### 3.2 Wire Event Contracts (`internal/runner/event_parser.go`)

OpenCode emitted wire format when invoked with `--format json`:

```go
package runner

import (
	"encoding/json"
	"time"
)

// OpenCodeWireEvent models the JSON events emitted by OpenCode CLI when run with --format json.
type OpenCodeWireEvent struct {
	Type      string               `json:"type"`      // "step_start" | "tool_use" | "step_finish" | "text" | "error"
	Timestamp int64                `json:"timestamp"` // Unix millisecond timestamp
	SessionID string               `json:"sessionID"`
	Part      OpenCodeWirePart     `json:"part"`
	Error     string               `json:"error,omitempty"`
	Message   string               `json:"message,omitempty"`
}

type OpenCodeWirePart struct {
	ID        string                 `json:"id,omitempty"`
	PartID    string                 `json:"partID,omitempty"`
	MessageID string                 `json:"messageID,omitempty"`
	Type      string                 `json:"type,omitempty"`
	Tool      string                 `json:"tool,omitempty"`
	State     *OpenCodeToolState     `json:"state,omitempty"`
	Text      string                 `json:"text,omitempty"`
	Reason    string                 `json:"reason,omitempty"` // "stop" | "tool-calls" | "length"
	Tokens    *OpenCodeTokensUsage   `json:"tokens,omitempty"`
	Cost      float64                `json:"cost,omitempty"`
	Snapshot  string                 `json:"snapshot,omitempty"`
	Time      *OpenCodeTimeSpan      `json:"time,omitempty"`
}

type OpenCodeToolState struct {
	Status   string         `json:"status"` // "running" | "completed" | "failed"
	Input    map[string]any `json:"input"`
	Output   string         `json:"output,omitempty"`
	Title    string         `json:"title,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type OpenCodeTokensUsage struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning,omitempty"`
	Cache     *struct {
		Read  int64 `json:"read"`
		Write int64 `json:"write"`
	} `json:"cache,omitempty"`
}

type OpenCodeTimeSpan struct {
	Start int64 `json:"start"`
	End   int64 `json:"end,omitempty"`
}
```

### 3.3 Event Processor & ANSI Formatter (`internal/runner/event_processor.go`)

For each line scanned from stdout:
1. Parse `OpenCodeWireEvent`.
2. Format into clean ANSI representations:
   - `tool_use (start)`: `\x1b[33m→ [%s]\x1b[0m \x1b[90m%s\x1b[0m\r\n` (e.g. `→ [shell] npm test`)
   - `tool_use (output)`: `  \x1b[90m%s\x1b[0m\r\n` (truncated preview of output)
   - `tool_use (completed)`: `\x1b[32m✔ Completed tool: %s\x1b[0m\r\n`
   - `text`: `\x1b[36m> %s\x1b[0m\r\n%s\r\n`
   - `step_finish`: `\x1b[90mStep finished (%d tokens, reason: %s)\x1b[0m\r\n`
3. Broadcast via `hub.BroadcastTaskLog(taskID, ansiChunk)` to update the terminal stream and database.
4. Broadcast structured payload via `hub.BroadcastTaskEvent(taskID, event)` for interactive UI elements.

### 3.4 Terminal Completion Contract (`internal/runner/terminal_contract.go`)

Adapted from Multica (`server/pkg/agent/stream_json_result.go`):
- Process exit code `0` alone is not proof of success.
- A run is only marked `succeeded` if a valid terminal signal is observed:
  - `sawTerminalSignal == true`: At least one `step_finish` with `reason: "stop"` was received.
  - `noTerminalSignal == false`: The stream did not terminate abruptly while waiting for tool continuation.
  - If process exits without a terminal signal, status fails closed with `"premature EOF: stream disconnected before model completed turn"`.

---

## 4. Frontend Integration (`web/`)

### 4.1 WebSocket Frame Protocol (`terminalParser.ts`)

Expand `WSMessage` to support both log streams and typed structured events:

```ts
export interface WSMessage {
  task_id?: string;
  type?: 'log' | 'status' | 'diff' | 'agent_event';
  payload?: unknown;
  timestamp?: string;
}

export interface AgentEventPayload {
  eventType: 'step_start' | 'tool_use' | 'text' | 'step_finish' | 'error';
  sessionID: string;
  toolName?: string;
  toolStatus?: 'running' | 'completed' | 'failed';
  toolInput?: Record<string, unknown>;
  toolOutput?: string;
  text?: string;
  tokens?: {
    input: number;
    output: number;
    reasoning?: number;
    cacheRead?: number;
    cacheWrite?: number;
  };
  durationMs?: number;
}
```

### 4.2 UI Representation

1. **TerminalView (`TerminalView.tsx`):**
   - Continues receiving high-contrast ANSI logs over `type: "log"`.
   - Guaranteed automatic scrolling to bottom on incoming chunk.
2. **AgentSessionView (`AgentSessionView.tsx`):**
   - Listens to `type: "agent_event"`.
   - Displays real-time tool invocation cards with spinners when `toolStatus: "running"`.
   - Renders expandable input commands and output blocks.
   - Shows live token usage counter in modal header updating on `step_finish`.

---

## 5. Implementation Stages & Verification Plan

### Phase 1: Core Scanner & Wire Parser
1. Create `internal/runner/stream_scanner.go` with 32MB buffer capacity.
2. Create `internal/runner/event_parser.go` with wire types and JSON unmarshaler.
3. Unit tests in `internal/runner/stream_scanner_test.go` and `internal/runner/event_parser_test.go` verifying:
   - Lines > 64KB (e.g. 500KB test lines) scan without error.
   - All OpenCode event types parse correctly into ANSI chunks and event DTOs.

### Phase 2: Runner Execution Integration
1. Update `runInDocker` and `runOnHost` in `internal/runner/task_runner.go`:
   - Pass `--format json` argument.
   - Connect `NewAgentStreamScanner` to stdout.
   - Process events with `EventProcessor`.
2. Connect `BroadcastTaskEvent` to `internal/ws/hub.go`.
3. Unit & integration tests in `internal/runner/task_runner_test.go`.

### Phase 3: Steering Watcher Parity
1. Update `watchSteeringTurn` in `internal/service/task_service.go` to use identical parser logic when reading session message contents and tool states.
2. Verify live steering output streaming in `task_service_test.go`.

### Phase 4: Frontend Component Enhancement
1. Update `terminalParser.ts` to deserialize `agent_event` frames.
2. Enhance `AgentSessionView.tsx` to handle live tool state updates.
3. Verify with Vitest in `web/src/features/tasks/components/`.

---

## 6. Review & Approval Gate

- Specification reviewed for placeholders, ambiguity, and scope.
- Ready for written implementation plan handoff via the `writing-plans` skill.
