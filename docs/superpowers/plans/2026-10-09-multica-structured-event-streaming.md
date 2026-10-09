# Multica-Inspired Structured Event Streaming Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Multica's structured event streaming architecture into Agent Task Manager, replacing raw TTY scraping with `--format json`, a 32MB-buffered stream scanner, fail-closed terminal completion, and dual-channel WebSocket broadcasting (ANSI logs + typed UI events).

**Architecture:** OpenCode runs with `--format json`. Stdout is scanned by a 32MB dynamic `AgentStreamScanner` preventing `bufio.ErrTooLong`. Lines are parsed into typed `OpenCodeWireEvent` objects by `event_parser.go`, evaluated for terminal completion by `TerminalTracker`, formatted into ANSI chunks for `task_logs`/xterm, and broadcast as structured `agent_event` frames to the web frontend.

**Tech Stack:** Go 1.27, Docker SDK, Fiber v3, Fasthttp WebSockets, TypeScript, React 19, Vitest, xterm.js.

**Spec:** `docs/superpowers/specs/2026-10-09-multica-structured-event-streaming-design.md`

## Global Constraints

- Go packages must pass `make check` (`staticcheck`, `go vet`, `go test ./...`) with zero warnings.
- Maximum scan line size is exactly 32 MiB (`32 * 1024 * 1024`) with 1 MiB initial allocation (`1024 * 1024`).
- OpenCode CLI invocations pass `--format json` and `--dangerously-skip-permissions` (or `--auto`).
- Process exit code `0` alone is not proof of success; `sawTerminalSignal` is required to succeed.
- Dual-channel WebSocket messages use `type: "log"` for ANSI text and `type: "agent_event"` for structured JSON payloads.
- Frontend test suite (`npm test --prefix web -- --run`) must remain 100% passing across all 18 suites (79+ tests).

## Review Focus

1. **Large Tool Output (lines > 64KB):** When a tool outputs a 500KB JSON bundle or file diff, `NewAgentStreamScanner` must not fail with `bufio.ErrTooLong`.
2. **Premature Process Exit:** When OpenCode exits 0 mid-step without a `step_finish` reason `stop`, the tracker must fail closed rather than reporting false success.
3. **Malformed JSON Lines:** Non-JSON output (e.g. system warnings, npm deprecations) on stdout must be treated gracefully as fallback log chunks rather than crashing the parser.
4. **WebSocket Replay & Framing:** `BroadcastTaskEvent` and `BroadcastTaskLog` must serialize strictly formatted `WSMessage` frames compatible with `parseWsFrames` in TypeScript.
5. **Dual UI Synchronization:** Both `TerminalView` (ANSI) and `AgentSessionView` (structured cards) must receive updates without race conditions or transcript duplication.

---

### Task 1: Resilient Stream Scanner (`internal/runner/stream_scanner.go`)

**Files:**
- Create: `internal/runner/stream_scanner.go`
- Test: `internal/runner/stream_scanner_test.go`

**Interfaces:**
- Produces: `NewAgentStreamScanner(r io.Reader) *bufio.Scanner`
- Constants: `AgentStreamMaxLineBytes = 32 * 1024 * 1024`, `AgentStreamInitialBufferBytes = 1024 * 1024`

- [ ] **Step 1: Write failing test for standard scanner vs AgentStreamScanner on large lines**

```go
package runner

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestAgentStreamScanner_LargeLineCapacity(t *testing.T) {
	// 500 KB single line (exceeds default bufio.MaxScanTokenSize of 64 KB)
	largeLine := strings.Repeat("A", 500*1024) + "\n"

	// Standard scanner should fail with ErrTooLong
	stdScanner := bufio.NewScanner(bytes.NewReader([]byte(largeLine)))
	if stdScanner.Scan() {
		t.Fatalf("expected standard bufio.Scanner to fail on 500KB line, but it succeeded")
	}
	if stdScanner.Err() != bufio.ErrTooLong {
		t.Fatalf("expected ErrTooLong, got: %v", stdScanner.Err())
	}

	// NewAgentStreamScanner must succeed
	agentScanner := NewAgentStreamScanner(bytes.NewReader([]byte(largeLine)))
	if !agentScanner.Scan() {
		t.Fatalf("expected AgentStreamScanner to succeed on 500KB line, got err: %v", agentScanner.Err())
	}
	if len(agentScanner.Text()) != 500*1024 {
		t.Fatalf("expected 500KB line length, got %d", len(agentScanner.Text()))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/runner -run TestAgentStreamScanner_LargeLineCapacity`
Expected: FAIL (compilation error: `NewAgentStreamScanner undefined`)

- [ ] **Step 3: Implement NewAgentStreamScanner**

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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/runner -run TestAgentStreamScanner_LargeLineCapacity`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runner/stream_scanner.go internal/runner/stream_scanner_test.go
git commit -m "feat(runner): implement 32MB-buffered AgentStreamScanner from Multica"
```

---

### Task 2: OpenCode Wire Event Parser & ANSI Formatter (`internal/runner/event_parser.go`)

**Files:**
- Create: `internal/runner/event_parser.go`
- Test: `internal/runner/event_parser_test.go`

**Interfaces:**
- Produces:
  - `type OpenCodeWireEvent struct`
  - `ParseOpenCodeWireEvent(line []byte) (*OpenCodeWireEvent, error)`
  - `FormatWireEventANSI(evt *OpenCodeWireEvent) (ansiText string, isLog bool)`

- [ ] **Step 1: Write failing tests for event parser and ANSI formatter**

```go
package runner

import (
	"testing"
)

func TestParseOpenCodeWireEvent_AllEventTypes(t *testing.T) {
	// 1. step_start
	startJSON := `{"type":"step_start","timestamp":1791452475152,"sessionID":"ses_123","part":{"id":"prt_1","type":"step-start"}}`
	evt, err := ParseOpenCodeWireEvent([]byte(startJSON))
	if err != nil || evt.Type != "step_start" {
		t.Fatalf("failed to parse step_start: %v", err)
	}

	// 2. tool_use
	toolJSON := `{"type":"tool_use","timestamp":1791452475164,"sessionID":"ses_123","part":{"tool":"shell","state":{"status":"completed","input":{"command":"npm test"},"output":"PASS\n"}}}`
	evt, err = ParseOpenCodeWireEvent([]byte(toolJSON))
	if err != nil || evt.Type != "tool_use" || evt.Part.Tool != "shell" {
		t.Fatalf("failed to parse tool_use: %v", err)
	}
	ansi, isLog := FormatWireEventANSI(evt)
	if !isLog || ansi == "" {
		t.Fatalf("expected formatted ANSI log for tool_use, got: %q", ansi)
	}

	// 3. text
	textJSON := `{"type":"text","timestamp":1791452478523,"sessionID":"ses_123","part":{"text":"All tests pass cleanly"}}`
	evt, err = ParseOpenCodeWireEvent([]byte(textJSON))
	if err != nil || evt.Type != "text" || evt.Part.Text != "All tests pass cleanly" {
		t.Fatalf("failed to parse text event: %v", err)
	}

	// 4. step_finish
	finishJSON := `{"type":"step_finish","timestamp":1791452475469,"sessionID":"ses_123","part":{"type":"step-finish","reason":"stop","tokens":{"input":100,"output":50}}}`
	evt, err = ParseOpenCodeWireEvent([]byte(finishJSON))
	if err != nil || evt.Type != "step_finish" || evt.Part.Reason != "stop" {
		t.Fatalf("failed to parse step_finish: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/runner -run TestParseOpenCodeWireEvent_AllEventTypes`
Expected: FAIL (compilation error: `ParseOpenCodeWireEvent undefined`)

- [ ] **Step 3: Implement OpenCodeWireEvent and FormatWireEventANSI**

```go
package runner

import (
	"encoding/json"
	"fmt"
	"strings"
)

// OpenCodeWireEvent models the JSON events emitted by OpenCode CLI when run with --format json.
type OpenCodeWireEvent struct {
	Type      string           `json:"type"`      // "step_start" | "tool_use" | "step_finish" | "text" | "error"
	Timestamp int64            `json:"timestamp"` // Unix millisecond timestamp
	SessionID string           `json:"sessionID"`
	Part      OpenCodeWirePart `json:"part"`
	Error     string           `json:"error,omitempty"`
	Message   string           `json:"message,omitempty"`
}

type OpenCodeWirePart struct {
	ID        string               `json:"id,omitempty"`
	PartID    string               `json:"partID,omitempty"`
	MessageID string               `json:"messageID,omitempty"`
	Type      string               `json:"type,omitempty"`
	Tool      string               `json:"tool,omitempty"`
	State     *OpenCodeToolState   `json:"state,omitempty"`
	Text      string               `json:"text,omitempty"`
	Reason    string               `json:"reason,omitempty"` // "stop" | "tool-calls" | "length"
	Tokens    *OpenCodeTokensUsage `json:"tokens,omitempty"`
	Cost      float64              `json:"cost,omitempty"`
	Snapshot  string               `json:"snapshot,omitempty"`
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

// ParseOpenCodeWireEvent parses a JSON line emitted by opencode run --format json.
func ParseOpenCodeWireEvent(line []byte) (*OpenCodeWireEvent, error) {
	var evt OpenCodeWireEvent
	if err := json.Unmarshal(line, &evt); err != nil {
		return nil, err
	}
	return &evt, nil
}

// FormatWireEventANSI formats an OpenCode event into human-readable ANSI colored text for the terminal.
func FormatWireEventANSI(evt *OpenCodeWireEvent) (string, bool) {
	if evt == nil {
		return "", false
	}

	switch evt.Type {
	case "tool_use":
		toolName := evt.Part.Tool
		if toolName == "" {
			toolName = "tool"
		}
		if evt.Part.State != nil {
			cmd := ""
			if cmdVal, ok := evt.Part.State.Input["command"].(string); ok {
				cmd = cmdVal
			} else if pathVal, ok := evt.Part.State.Input["path"].(string); ok {
				cmd = pathVal
			}

			if evt.Part.State.Status == "completed" {
				var b strings.Builder
				if cmd != "" {
					b.WriteString(fmt.Sprintf("\x1b[33m→ [%s]\x1b[0m \x1b[90m%s\x1b[0m\r\n", toolName, cmd))
				}
				if evt.Part.State.Output != "" {
					lines := strings.Split(strings.TrimSpace(evt.Part.State.Output), "\n")
					limit := 4
					if len(lines) < limit {
						limit = len(lines)
					}
					for i := 0; i < limit; i++ {
						b.WriteString(fmt.Sprintf("  \x1b[90m%s\x1b[0m\r\n", lines[i]))
					}
					if len(lines) > 4 {
						b.WriteString(fmt.Sprintf("  \x1b[90m... (%d lines)\x1b[0m\r\n", len(lines)))
					}
				}
				b.WriteString(fmt.Sprintf("\x1b[32m✔ Completed tool: %s\x1b[0m\r\n", toolName))
				return b.String(), true
			} else if evt.Part.State.Status == "failed" {
				return fmt.Sprintf("\x1b[31m✖ Tool failed: %s\x1b[0m\r\n", toolName), true
			}
			return fmt.Sprintf("\x1b[33m→ [%s]\x1b[0m \x1b[90m%s\x1b[0m\r\n", toolName, cmd), true
		}
		return fmt.Sprintf("\x1b[33m→ [%s]\x1b[0m\r\n", toolName), true

	case "text":
		if strings.TrimSpace(evt.Part.Text) != "" {
			return fmt.Sprintf("\r\n\x1b[36m> agent\x1b[0m\r\n%s\r\n", evt.Part.Text), true
		}

	case "error":
		errMsg := evt.Error
		if errMsg == "" {
			errMsg = evt.Message
		}
		return fmt.Sprintf("\r\n\x1b[31m✖ Agent error: %s\x1b[0m\r\n", errMsg), true
	}

	return "", false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/runner -run TestParseOpenCodeWireEvent_AllEventTypes`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runner/event_parser.go internal/runner/event_parser_test.go
git commit -m "feat(runner): implement OpenCode wire event parser and ANSI formatter"
```

---

### Task 3: Terminal Completion & Fail-Closed Contract (`internal/runner/terminal_contract.go`)

**Files:**
- Create: `internal/runner/terminal_contract.go`
- Test: `internal/runner/terminal_contract_test.go`

**Interfaces:**
- Produces:
  - `type TerminalTracker struct`
  - `NewTerminalTracker() *TerminalTracker`
  - `(t *TerminalTracker) Process(evt *OpenCodeWireEvent)`
  - `(t *TerminalTracker) CheckCompletion() error`

- [ ] **Step 1: Write failing test for TerminalTracker**

```go
package runner

import (
	"testing"
)

func TestTerminalTracker_FailClosed(t *testing.T) {
	tracker := NewTerminalTracker()

	// 1. Process step start
	tracker.Process(&OpenCodeWireEvent{Type: "step_start"})
	// Clean exit before finish must fail closed
	if err := tracker.CheckCompletion(); err == nil {
		t.Fatalf("expected error for open step without step_finish")
	}

	// 2. Process tool continuation
	tracker.Process(&OpenCodeWireEvent{
		Type: "step_finish",
		Part: OpenCodeWirePart{Reason: "tool-calls"},
	})
	// Exit on tool-calls continuation must fail closed
	if err := tracker.CheckCompletion(); err == nil {
		t.Fatalf("expected error for step finishing with tool-calls pending")
	}

	// 3. Process final stop reason
	tracker.Process(&OpenCodeWireEvent{
		Type: "step_finish",
		Part: OpenCodeWirePart{Reason: "stop"},
	})
	// Now it must succeed
	if err := tracker.CheckCompletion(); err != nil {
		t.Fatalf("expected clean completion after reason: stop, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/runner -run TestTerminalTracker_FailClosed`
Expected: FAIL (compilation error: `NewTerminalTracker undefined`)

- [ ] **Step 3: Implement TerminalTracker**

```go
package runner

import (
	"errors"
)

// TerminalTracker monitors OpenCode stream step bracketing and continuation reasons.
// Directly ported from Multica's fail-closed agent completion contract.
type TerminalTracker struct {
	openStep             bool
	awaitingContinuation bool
	sawTerminalSignal    bool
	sawAnyStep           bool
}

// NewTerminalTracker creates a new terminal lifecycle tracker.
func NewTerminalTracker() *TerminalTracker {
	return &TerminalTracker{}
}

// Process updates tracker state from incoming OpenCode wire events.
func (t *TerminalTracker) Process(evt *OpenCodeWireEvent) {
	if evt == nil {
		return
	}

	switch evt.Type {
	case "step_start":
		t.openStep = true
		t.awaitingContinuation = false
		t.sawAnyStep = true

	case "step_finish":
		t.openStep = false
		if evt.Part.Reason == "tool-calls" {
			t.awaitingContinuation = true
		} else if evt.Part.Reason == "stop" || evt.Part.Reason == "end_turn" {
			t.awaitingContinuation = false
			t.sawTerminalSignal = true
		}
	}
}

// CheckCompletion evaluates whether the stream reached a provable, clean terminal state.
func (t *TerminalTracker) CheckCompletion() error {
	if t.openStep {
		return errors.New("stream terminated prematurely while step was still executing")
	}
	if t.awaitingContinuation {
		return errors.New("stream terminated prematurely while model tool calls were pending")
	}
	if !t.sawTerminalSignal && t.sawAnyStep {
		return errors.New("stream ended without terminal stop signal from model")
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/runner -run TestTerminalTracker_FailClosed`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runner/terminal_contract.go internal/runner/terminal_contract_test.go
git commit -m "feat(runner): implement fail-closed TerminalTracker from Multica"
```

---

### Task 4: WebSocket Hub Structured Event Delivery (`internal/ws/hub.go`)

**Files:**
- Modify: `internal/ws/messages.go:20-40`
- Modify: `internal/ws/hub.go:130-150`
- Test: `internal/ws/hub_test.go`

**Interfaces:**
- Produces:
  - `NewAgentEventMessage(taskID string, payload any) *Message`
  - `(h *Hub) BroadcastTaskEvent(taskID string, event any) error`

- [ ] **Step 1: Write failing test in `internal/ws/hub_test.go`**

```go
func TestHub_BroadcastTaskEvent(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	client := &mockClient{id: "client-1", room: "task-123", sendCh: make(chan []byte, 10)}
	hub.Register(client)

	payload := map[string]any{"eventType": "tool_use", "tool": "shell"}
	if err := hub.BroadcastTaskEvent("task-123", payload); err != nil {
		t.Fatalf("BroadcastTaskEvent failed: %v", err)
	}

	select {
	case msg := <-client.sendCh:
		var parsed Message
		if err := json.Unmarshal(msg, &parsed); err != nil {
			t.Fatalf("failed to unmarshal broadcast message: %v", err)
		}
		if parsed.Type != "agent_event" || parsed.TaskID != "task-123" {
			t.Fatalf("unexpected message: %+v", parsed)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for broadcast event")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/ws -run TestHub_BroadcastTaskEvent`
Expected: FAIL (compilation error: `BroadcastTaskEvent undefined`)

- [ ] **Step 3: Implement NewAgentEventMessage and BroadcastTaskEvent**

In `internal/ws/messages.go`:
```go
// NewAgentEventMessage creates a typed structured agent event message frame.
func NewAgentEventMessage(taskID string, payload any) *Message {
	return &Message{
		TaskID:    taskID,
		Type:      "agent_event",
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}
```

In `internal/ws/hub.go`:
```go
// BroadcastTaskEvent marshals and broadcasts a structured agent event to subscribers of taskID.
func (h *Hub) BroadcastTaskEvent(taskID string, event any) error {
	msg := NewAgentEventMessage(taskID, event)
	data, err := common.Marshal(msg)
	if err != nil {
		return err
	}
	h.BroadcastToTask(taskID, data)
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/ws -run TestHub_BroadcastTaskEvent`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ws/messages.go internal/ws/hub.go internal/ws/hub_test.go
git commit -m "feat(ws): add BroadcastTaskEvent for structured agent UI streaming"
```

---

### Task 5: Runner Execution Integration (`internal/runner/task_runner.go`)

**Files:**
- Modify: `internal/runner/task_runner.go:750-770, 830-850, 870-900`
- Test: `internal/runner/task_runner_test.go`

**Interfaces:**
- Consumes:
  - `NewAgentStreamScanner`
  - `ParseOpenCodeWireEvent`
  - `FormatWireEventANSI`
  - `NewTerminalTracker`
  - `hub.BroadcastTaskEvent`

- [ ] **Step 1: Write integration test verifying `--format json` argument and scanner**

In `internal/runner/task_runner_test.go`, verify command arguments include `--format` and `json`.

- [ ] **Step 2: Update `runInDocker` and `runOnHost` command arguments to include `--format json`**

In `internal/runner/task_runner.go`:
```go
	cmd := []string{
		"/root/.opencode/bin/opencode",
		"run",
		"--format", "json",
		"--server", r.cfg.OpenCodeServerURL,
		"--session", sessionID,
		"--title", task.Title,
		"--model", model,
		"--auto",
		prompt,
	}
```

And update `streamPipe` to use `NewAgentStreamScanner` and parse each line:
```go
func (r *AgentTaskRunner) streamPipe(ctx context.Context, taskID, attemptID string, reader io.Reader, stream string) {
	scanner := NewAgentStreamScanner(reader)
	tracker := NewTerminalTracker()

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		// Try structured JSON event parse
		if evt, err := ParseOpenCodeWireEvent(line); err == nil {
			tracker.Process(evt)
			if r.hub != nil {
				_ = r.hub.BroadcastTaskEvent(taskID, evt)
			}
			if ansi, isLog := FormatWireEventANSI(evt); isLog {
				r.broadcastLog(ctx, taskID, attemptID, ansi)
			}
		} else {
			// Fallback plain text / system logs
			r.broadcastLog(ctx, taskID, attemptID, string(line)+"\r\n")
		}
	}
	if scanner.Err() != nil {
		log.Printf("error reading %s stream for task %s: %v", stream, taskID, scanner.Err())
	}
}
```

- [ ] **Step 3: Run runner tests**

Run: `go test -v ./internal/runner -run TestRunTask`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/runner/task_runner.go internal/runner/task_runner_test.go
git commit -m "feat(runner): wire --format json, AgentStreamScanner, and dual-channel event streaming"
```

---

### Task 6: Steering Watcher Parity (`internal/service/task_service.go`)

**Files:**
- Modify: `internal/service/task_service.go:500-560`
- Test: `internal/service/task_service_test.go`

**Interfaces:**
- Consumes:
  - `hub.BroadcastTaskEvent`
  - `hub.BroadcastTaskLog`

- [ ] **Step 1: Write test verifying steering turn emits `agent_event` frames on tool activity**

- [ ] **Step 2: Update `watchSteeringTurn` in `task_service.go` to emit `agent_event`**

Whenever a tool call or text response is identified in active messages:
```go
if s.hub != nil {
	_ = s.hub.BroadcastTaskEvent(taskID, map[string]any{
		"eventType":  "tool_use",
		"sessionID":  sessionID,
		"toolName":   toolName,
		"toolStatus": status,
		"toolInput":  state["input"],
	})
}
```

- [ ] **Step 3: Run service tests**

Run: `go test -v ./internal/service -run TestTaskService_Session`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/service/task_service.go internal/service/task_service_test.go
git commit -m "feat(service): emit structured agent_event frames during steering watcher"
```

---

### Task 7: Frontend Protocol & UI Cards (`web/`)

**Files:**
- Modify: `web/src/features/tasks/utils/terminalParser.ts`
- Modify: `web/src/features/tasks/components/TerminalView.tsx`
- Modify: `web/src/features/tasks/components/AgentSessionView.tsx`
- Test: `web/src/features/tasks/utils/terminalParser.test.ts`
- Test: `web/src/features/tasks/components/TerminalView.test.tsx`

**Interfaces:**
- Consumes: `WSMessage` with `type: 'agent_event'`
- Produces: Real-time tool status badges & expandable logs in UI

- [ ] **Step 1: Update `terminalParser.test.ts` to test `agent_event` message frames**

```ts
it('parses structured agent_event frames', () => {
  const raw = JSON.stringify({
    task_id: 'task-1',
    type: 'agent_event',
    payload: { eventType: 'tool_use', toolName: 'shell' },
  });
  const frames = parseWsFrames(raw);
  expect(frames).toHaveLength(1);
  expect(frames[0].type).toBe('agent_event');
});
```

- [ ] **Step 2: Update `terminalParser.ts` type definition**

Allow `type?: 'log' | 'status' | 'diff' | 'agent_event' | string;`

- [ ] **Step 3: Update `AgentSessionView.tsx` to display active running tool state from `agent_event`**

- [ ] **Step 4: Run frontend tests**

Run: `npm test --prefix web -- --run`
Expected: PASS (all 18 test files passing)

- [ ] **Step 5: Commit**

```bash
git add web/src/features/tasks/utils/terminalParser.ts web/src/features/tasks/utils/terminalParser.test.ts web/src/features/tasks/components/AgentSessionView.tsx
git commit -m "feat(web): support structured agent_event frames in WebSocket parser and session view"
```

---

### Task 8: Full End-to-End Verification & Build Gate

**Files:**
- `Makefile`
- Live containers

- [ ] **Step 1: Run comprehensive local validation**

Run: `make check`
Expected:
- staticcheck: 0 findings
- go vet: clean
- All 19 Go test packages pass
- Vitest: All 18 frontend suites (79+ tests) pass

- [ ] **Step 2: Build and deploy containers**

Run:
```bash
npm run build --prefix web && \
docker compose cp web/dist/. web-ui:/usr/share/nginx/html/ && \
docker compose exec web-ui nginx -s reload && \
docker compose build control-plane && \
docker compose up -d control-plane
```

- [ ] **Step 3: Live WebSocket test**

Connect to `wss://localhost/api/v1/ws/terminal/:taskId` and verify both `log` frames and `agent_event` frames are streaming properly.
