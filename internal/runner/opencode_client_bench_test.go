package runner_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-task-manager/internal/runner"
)

// messagesBody builds an OpenCode message listing with n assistant messages, each carrying a text part and
// a completed tool part, the shape the stream relay re-reads on every tick.
func messagesBody(n int) string {
	var b strings.Builder
	b.WriteString(`{"data":[`)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":"msg_%d","type":"assistant","role":"assistant","agent":"build","text":"","time":{"created":%d,"completed":%d},"content":[{"type":"text","text":"Working on step %d of the task with some explanatory prose."},{"type":"tool","id":"tool_%d","name":"bash","state":{"status":"completed","input":{"command":"go test ./internal/runner/..."},"output":"ok  \tagent-task-manager/internal/runner\t0.031s"}}]}`,
			i, 1700000000000+i, 1700000000001+i, i, i)
	}
	b.WriteString(`]}`)
	return b.String()
}

func BenchmarkOpenCodeClient_GetSessionMessages(b *testing.B) {
	body := messagesBody(400)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	b.Cleanup(srv.Close)
	client := runner.NewOpenCodeClient(srv.URL, "pw", nil)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		msgs, err := client.GetSessionMessages(ctx, "s1")
		if err != nil {
			b.Fatal(err)
		}
		if len(msgs) != 400 {
			b.Fatalf("got %d messages", len(msgs))
		}
	}
}
