package service

import (
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/runner"
)

// Bounds on the subagent activity sent with each session frame. The session stream re-sends the whole response
// whenever it changes, so these keep a long run from growing the frame without limit.
const (
	maxSubagentTranscripts = 8    // newest subagents whose activity is included
	maxSubagentMessages    = 40   // newest messages kept per subagent
	maxSubagentTextRunes   = 2000 // longest tool output or text kept per message part
)

// newestSubagents keeps the most recently listed subagents, which are the ones still running or just finished.
func newestSubagents(kids []runner.OpenCodeSessionDTO) []runner.OpenCodeSessionDTO {
	if len(kids) > maxSubagentTranscripts {
		return kids[len(kids)-maxSubagentTranscripts:]
	}
	return kids
}

// recentSubagentMessages keeps the newest messages of a subagent transcript and caps long tool output and text.
func recentSubagentMessages(msgs []dto.OpenCodeMessageDTO) []dto.OpenCodeMessageDTO {
	if len(msgs) > maxSubagentMessages {
		msgs = msgs[len(msgs)-maxSubagentMessages:]
	}
	for i := range msgs {
		capMessagePartText(msgs[i].Content)
	}
	return msgs
}

// capMessagePartText truncates the text, output and result fields of a message's parts, in place.
func capMessagePartText(content any) {
	parts, ok := content.([]any)
	if !ok {
		return
	}
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		capField(part, "text")
		capField(part, "output")
		capField(part, "result")
		if state, ok := part["state"].(map[string]any); ok {
			capField(state, "output")
		}
	}
}

func capField(m map[string]any, key string) {
	s, ok := m[key].(string)
	if !ok {
		return
	}
	runes := []rune(s)
	if len(runes) <= maxSubagentTextRunes {
		return
	}
	m[key] = string(runes[:maxSubagentTextRunes]) + "…"
}
