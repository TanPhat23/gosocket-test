package runner

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	defaultStreamPoll  = 1500 * time.Millisecond
	streamFlushTimeout = 5 * time.Second
	streamClockSkew    = 2 * time.Second
	streamToolCmdMax   = 80
	streamToolOutLines = 4
)

func (r *AgentTaskRunner) streamPoll() time.Duration {
	if r.sessionPoll > 0 {
		return r.sessionPoll
	}
	return defaultStreamPoll
}

// startSessionStream relays the root session's tool calls and assistant text to the task log and event hub
// while a turn runs. seen is shared across turns so later turns do not replay earlier output; since drops
// messages from before this run. The returned stop ends the poller, then flushes once more so output that
// landed after the last tick is in the log before the caller reports the turn finished.
func (r *AgentTaskRunner) startSessionStream(ctx context.Context, oc OpenCodeClient, taskID, attemptID, sessionID string, since time.Time, seen map[string]bool) (stop func()) {
	if oc == nil {
		return func() {}
	}
	sctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(r.streamPoll())
		defer ticker.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-ticker.C:
				r.relaySession(sctx, oc, taskID, attemptID, sessionID, since, seen)
			}
		}
	}()
	return func() {
		cancel()
		<-done
		flushCtx, flushCancel := context.WithTimeout(context.WithoutCancel(ctx), streamFlushTimeout)
		defer flushCancel()
		r.relaySession(flushCtx, oc, taskID, attemptID, sessionID, since, seen)
	}
}

// streamSource identifies whose output is being relayed: the root session, or a child (subagent) of it.
type streamSource struct {
	sessionID string
	parentID  string // empty for the root session
	label     string // subagent name; empty for the root session
}

func (s streamSource) prefix() string {
	if s.parentID == "" {
		return ""
	}
	return fmt.Sprintf("\x1b[35m[subagent: %s]\x1b[0m ", s.label)
}

// relaySession relays the root session, then every child session spawned from it.
func (r *AgentTaskRunner) relaySession(ctx context.Context, oc OpenCodeClient, taskID, attemptID, sessionID string, since time.Time, seen map[string]bool) {
	r.relaySessionMessages(ctx, oc, taskID, attemptID, streamSource{sessionID: sessionID}, since, seen)
	children, err := oc.ListChildSessions(ctx, sessionID)
	if err != nil {
		return
	}
	for _, child := range children {
		label := child.Model
		if label == "" {
			label = "subagent"
		}
		r.relaySessionMessages(ctx, oc, taskID, attemptID, streamSource{sessionID: child.ID, parentID: sessionID, label: label}, since, seen)
	}
}

func (r *AgentTaskRunner) relaySessionMessages(ctx context.Context, oc OpenCodeClient, taskID, attemptID string, src streamSource, since time.Time, seen map[string]bool) {
	msgs, err := oc.GetSessionMessages(ctx, src.sessionID)
	if err != nil {
		return
	}
	cutoff := since.Add(-streamClockSkew)
	for _, msg := range msgs {
		if msg.Role != "assistant" && msg.Type != "assistant" {
			continue
		}
		if msg.CreatedAt != nil && msg.CreatedAt.Before(cutoff) {
			continue
		}
		parts, ok := msg.Content.([]any)
		if !ok {
			continue
		}
		for idx, raw := range parts {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			partID := fmt.Sprintf("%s_%d", msg.ID, idx)
			switch part["type"] {
			case "tool":
				r.relayToolPart(ctx, taskID, attemptID, src, partID, part, seen)
			case "text":
				text, _ := part["text"].(string)
				key := partID + "_text"
				if text == "" || seen[key] {
					continue
				}
				seen[key] = true
				agent := msg.Agent
				if agent == "" {
					agent = "agent"
				}
				if r.hub != nil {
					_ = r.hub.BroadcastTaskEvent(taskID, src.event(map[string]any{
						"eventType": "text",
						"text":      text,
						"agent":     agent,
					}))
				}
				r.broadcastLog(ctx, taskID, attemptID, fmt.Sprintf("\r\n%s\x1b[36m> %s\x1b[0m\r\n%s\r\n", src.prefix(), agent, text))
			}
		}
	}
}

func (r *AgentTaskRunner) relayToolPart(ctx context.Context, taskID, attemptID string, src streamSource, partID string, part map[string]any, seen map[string]bool) {
	name, _ := part["name"].(string)
	if id, _ := part["id"].(string); id != "" {
		partID = id
	}
	state, _ := part["state"].(map[string]any)
	status := ""
	if state != nil {
		status, _ = state["status"].(string)
	}

	if startKey := partID + "_start"; !seen[startKey] {
		seen[startKey] = true
		var input any
		summary := ""
		if state != nil {
			input = state["input"]
			if in, ok := input.(map[string]any); ok {
				if cmd, ok := in["command"].(string); ok {
					summary = cmd
				} else if p, ok := in["path"].(string); ok {
					summary = p
				}
			}
		}
		if r.hub != nil {
			_ = r.hub.BroadcastTaskEvent(taskID, src.event(map[string]any{
				"eventType":  "tool_use",
				"toolName":   name,
				"toolStatus": "running",
				"toolInput":  input,
			}))
		}
		if len(summary) > streamToolCmdMax {
			summary = summary[:streamToolCmdMax-3] + "..."
		}
		if summary != "" {
			r.broadcastLog(ctx, taskID, attemptID, fmt.Sprintf("%s\x1b[33m→ [%s]\x1b[0m \x1b[90m%s\x1b[0m\r\n", src.prefix(), name, summary))
		} else {
			r.broadcastLog(ctx, taskID, attemptID, fmt.Sprintf("%s\x1b[33m→ [%s]\x1b[0m\r\n", src.prefix(), name))
		}
	}

	doneKey := partID + "_done"
	if (status != "completed" && status != "failed") || seen[doneKey] {
		return
	}
	seen[doneKey] = true
	var output any
	if state != nil {
		output = state["output"]
	}
	if r.hub != nil {
		_ = r.hub.BroadcastTaskEvent(taskID, src.event(map[string]any{
			"eventType":  "tool_use",
			"toolName":   name,
			"toolStatus": status,
			"toolOutput": output,
		}))
	}
	if out, _ := output.(string); strings.TrimSpace(out) != "" {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		show := lines
		if len(show) > streamToolOutLines {
			show = show[:streamToolOutLines]
		}
		for _, line := range show {
			r.broadcastLog(ctx, taskID, attemptID, fmt.Sprintf("  \x1b[90m%s\x1b[0m\r\n", line))
		}
		if len(lines) > streamToolOutLines {
			r.broadcastLog(ctx, taskID, attemptID, fmt.Sprintf("  \x1b[90m... (%d lines)\x1b[0m\r\n", len(lines)))
		}
	}
	if status == "completed" {
		r.broadcastLog(ctx, taskID, attemptID, fmt.Sprintf("%s\x1b[32m✔ Completed tool: %s\x1b[0m\r\n", src.prefix(), name))
	} else {
		r.broadcastLog(ctx, taskID, attemptID, fmt.Sprintf("%s\x1b[31m✖ Tool failed: %s\x1b[0m\r\n", src.prefix(), name))
	}
}

// event stamps a hub event with the session it came from, and its parent and name when it is a subagent's.
func (s streamSource) event(e map[string]any) map[string]any {
	e["sessionID"] = s.sessionID
	if s.parentID != "" {
		e["parentSessionID"] = s.parentID
		e["subagent"] = s.label
	}
	return e
}
