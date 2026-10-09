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
