package runner

import (
	"encoding/json"
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

		b := AcquireANSIBuilder()
		defer b.Release()

		if evt.Part.State != nil {
			if toolName == "subagent" {
				agentName := "subagent"
				if a, ok := evt.Part.State.Input["agent"].(string); ok && a != "" {
					agentName = a
				}
				desc := ""
				if d, ok := evt.Part.State.Input["description"].(string); ok {
					desc = d
				}
				if evt.Part.State.Status == "completed" {
					b.SubagentSuccess(agentName)
					return b.String(), true
				} else if evt.Part.State.Status == "failed" {
					b.SubagentFailure(agentName)
					return b.String(), true
				}
				b.SubagentDispatch(agentName, desc)
				return b.String(), true
			}

			cmd := ""
			if cmdVal, ok := evt.Part.State.Input["command"].(string); ok {
				cmd = cmdVal
			} else if pathVal, ok := evt.Part.State.Input["path"].(string); ok {
				cmd = pathVal
			} else if fpVal, ok := evt.Part.State.Input["file_path"].(string); ok {
				cmd = fpVal
			} else if qVal, ok := evt.Part.State.Input["query"].(string); ok {
				cmd = qVal
			} else if idVal, ok := evt.Part.State.Input["id"].(string); ok {
				cmd = idVal
			}

			if evt.Part.State.Status == "completed" {
				if cmd != "" {
					b.ToolHeader(toolName, cmd)
				}
				if evt.Part.State.Output != "" {
					b.TruncatedLines(evt.Part.State.Output, 4)
				}
				b.ToolSuccess(toolName)
				return b.String(), true
			} else if evt.Part.State.Status == "failed" {
				b.ToolFailure(toolName)
				return b.String(), true
			}

			b.ToolHeader(toolName, cmd)
			return b.String(), true
		}

		b.ToolHeader(toolName, "")
		return b.String(), true

	case "text":
		trimmed := strings.TrimSpace(evt.Part.Text)
		if trimmed != "" {
			b := AcquireANSIBuilder()
			defer b.Release()

			b.AgentHeader("agent").Text(evt.Part.Text).CRLF()
			return b.String(), true
		}

	case "error":
		errMsg := evt.Error
		if errMsg == "" {
			errMsg = evt.Message
		}
		b := AcquireANSIBuilder()
		defer b.Release()

		b.AgentError(errMsg)
		return b.String(), true
	}

	return "", false
}
