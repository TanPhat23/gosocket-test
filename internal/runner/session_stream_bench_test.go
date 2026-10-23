package runner

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// relayBenchOC serves a fixed transcript to the stream relay with no HTTP round trip. Only the two calls the
// relay makes are implemented; any other OpenCodeClient method panics through the nil embedded interface.
type relayBenchOC struct {
	OpenCodeClient
	msgs []OpenCodeMessageDTO
}

func (o relayBenchOC) GetSessionMessages(context.Context, string) ([]OpenCodeMessageDTO, error) {
	return o.msgs, nil
}

func (o relayBenchOC) ListChildSessions(context.Context, string) ([]OpenCodeSessionDTO, error) {
	return nil, nil
}

func relayBenchTranscript(turns int) []OpenCodeMessageDTO {
	at := time.Unix(1_700_000_000, 0)
	msgs := make([]OpenCodeMessageDTO, 0, turns)
	for i := range turns {
		msgs = append(msgs, OpenCodeMessageDTO{
			ID: fmt.Sprintf("msg_%d", i), Role: "assistant", Agent: "build", CreatedAt: &at,
			Content: []any{
				map[string]any{
					"type": "tool", "name": "bash", "id": fmt.Sprintf("tool_%d", i),
					"state": map[string]any{
						"status": "completed",
						"input":  map[string]any{"command": "go test ./..."},
						"output": "ok\nok\nok\nok\nok\nok",
					},
				},
				map[string]any{"type": "text", "text": "step finished"},
			},
		})
	}
	return msgs
}

// BenchmarkRelaySessionMessages_SettledTranscript measures one poll tick over a transcript whose parts were
// all relayed by an earlier tick, which is the steady state of a long-running turn.
func BenchmarkRelaySessionMessages_SettledTranscript(b *testing.B) {
	r := &AgentTaskRunner{}
	oc := relayBenchOC{msgs: relayBenchTranscript(200)}
	seen := map[string]bool{}
	src := streamSource{sessionID: "ses_root"}
	ctx := context.Background()
	since := time.Unix(1_600_000_000, 0)
	r.relaySessionMessages(ctx, oc, "task", "attempt", src, since, seen)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r.relaySessionMessages(ctx, oc, "task", "attempt", src, since, seen)
	}
}

// BenchmarkRelayToolPart_LargeOutput measures relaying one completed tool call whose output has many lines.
func BenchmarkRelayToolPart_LargeOutput(b *testing.B) {
	r := &AgentTaskRunner{}
	out := strings.Repeat("line of tool output\n", 20_000)
	part := map[string]any{
		"type": "tool", "name": "read", "id": "tool_big",
		"state": map[string]any{"status": "completed", "input": map[string]any{"path": "big.txt"}, "output": out},
	}
	src := streamSource{sessionID: "ses_root"}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r.relayToolPart(ctx, "task", "attempt", src, "msg_big_0", part, map[string]bool{})
	}
}
