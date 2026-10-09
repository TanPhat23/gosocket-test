package runner

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
)

const ansiMaxPooledBufferBytes = 16 * 1024 // 16 KiB

var ansiBufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 512))
	},
}

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

			buf := ansiBufferPool.Get().(*bytes.Buffer)
			buf.Reset()
			defer func() {
				if buf.Cap() <= ansiMaxPooledBufferBytes {
					ansiBufferPool.Put(buf)
				}
			}()

			if evt.Part.State.Status == "completed" {
				if cmd != "" {
					buf.WriteString("\x1b[33m→ [")
					buf.WriteString(toolName)
					buf.WriteString("]\x1b[0m \x1b[90m")
					buf.WriteString(cmd)
					buf.WriteString("\x1b[0m\r\n")
				}
				if evt.Part.State.Output != "" {
					trimmedOutput := strings.TrimSpace(evt.Part.State.Output)
					rem := trimmedOutput
					linesPrinted := 0
					totalLines := strings.Count(trimmedOutput, "\n") + 1

					for len(rem) > 0 && linesPrinted < 4 {
						idx := strings.IndexByte(rem, '\n')
						var line string
						if idx >= 0 {
							line = rem[:idx]
							rem = rem[idx+1:]
						} else {
							line = rem
							rem = ""
						}
						buf.WriteString("  \x1b[90m")
						buf.WriteString(line)
						buf.WriteString("\x1b[0m\r\n")
						linesPrinted++
					}
					if totalLines > 4 {
						buf.WriteString("  \x1b[90m... (")
						buf.WriteString(strconv.Itoa(totalLines))
						buf.WriteString(" lines)\x1b[0m\r\n")
					}
				}
				buf.WriteString("\x1b[32m✔ Completed tool: ")
				buf.WriteString(toolName)
				buf.WriteString("\x1b[0m\r\n")
				return buf.String(), true
			} else if evt.Part.State.Status == "failed" {
				buf.WriteString("\x1b[31m✖ Tool failed: ")
				buf.WriteString(toolName)
				buf.WriteString("\x1b[0m\r\n")
				return buf.String(), true
			}

			buf.WriteString("\x1b[33m→ [")
			buf.WriteString(toolName)
			buf.WriteString("]\x1b[0m")
			if cmd != "" {
				buf.WriteString(" \x1b[90m")
				buf.WriteString(cmd)
				buf.WriteString("\x1b[0m")
			}
			buf.WriteString("\r\n")
			return buf.String(), true
		}

		buf := ansiBufferPool.Get().(*bytes.Buffer)
		buf.Reset()
		defer func() {
			if buf.Cap() <= ansiMaxPooledBufferBytes {
				ansiBufferPool.Put(buf)
			}
		}()
		buf.WriteString("\x1b[33m→ [")
		buf.WriteString(toolName)
		buf.WriteString("]\x1b[0m\r\n")
		return buf.String(), true

	case "text":
		trimmed := strings.TrimSpace(evt.Part.Text)
		if trimmed != "" {
			buf := ansiBufferPool.Get().(*bytes.Buffer)
			buf.Reset()
			defer func() {
				if buf.Cap() <= ansiMaxPooledBufferBytes {
					ansiBufferPool.Put(buf)
				}
			}()
			buf.WriteString("\r\n\x1b[36m> agent\x1b[0m\r\n")
			buf.WriteString(evt.Part.Text)
			buf.WriteString("\r\n")
			return buf.String(), true
		}

	case "error":
		errMsg := evt.Error
		if errMsg == "" {
			errMsg = evt.Message
		}
		buf := ansiBufferPool.Get().(*bytes.Buffer)
		buf.Reset()
		defer func() {
			if buf.Cap() <= ansiMaxPooledBufferBytes {
				ansiBufferPool.Put(buf)
			}
		}()
		buf.WriteString("\r\n\x1b[31m✖ Agent error: ")
		buf.WriteString(errMsg)
		buf.WriteString("\x1b[0m\r\n")
		return buf.String(), true
	}

	return "", false
}
