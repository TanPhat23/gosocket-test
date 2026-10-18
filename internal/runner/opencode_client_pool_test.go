package runner_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"agent-task-manager/internal/runner"
)

// connCountingServer starts a server that answers every request with a minimal session body and counts
// the TCP connections it accepts. It records the Basic auth pair seen on each request.
type connCountingServer struct {
	*httptest.Server
	newConns atomic.Int64
	mu       sync.Mutex
	auths    []string
}

func newConnCountingServer(t *testing.T) *connCountingServer {
	t.Helper()
	s := &connCountingServer{}
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ := r.BasicAuth()
		s.mu.Lock()
		s.auths = append(s.auths, user+":"+pass)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"s1"}}`))
	}))
	s.Config.ConnState = func(_ net.Conn, st http.ConnState) {
		if st == http.StateNew {
			s.newConns.Add(1)
		}
	}
	s.Start()
	t.Cleanup(s.Close)
	return s
}

func (s *connCountingServer) recordedAuths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auths...)
}

// Each constructor call used to build a fresh transport, so every new client dialled a new connection.
func TestOpenCodeClient_ReusesConnectionsAcrossClients(t *testing.T) {
	srv := newConnCountingServer(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		c := runner.NewOpenCodeClient(srv.URL, "pw", nil)
		if _, err := c.GetSession(ctx, "s1"); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if got := srv.newConns.Load(); got != 1 {
		t.Fatalf("new connections = %d, want 1 shared by all clients", got)
	}
}

func TestProjectOpenCodeClient_ReusesConnectionsAcrossClients(t *testing.T) {
	srv := newConnCountingServer(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		c, err := runner.NewProjectOpenCodeClient(srv.URL, "pw", nil)
		if err != nil {
			t.Fatal(err)
		}
		// InterruptSession does not read the response body, so the connection is reusable only if the
		// client drains it before closing.
		if err := c.InterruptSession(ctx, "s1"); err != nil {
			t.Fatalf("interrupt %d: %v", i, err)
		}
		if _, err := c.GetSession(ctx, "s1"); err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
	}
	if got := srv.newConns.Load(); got != 1 {
		t.Fatalf("new connections = %d, want 1 shared by all clients", got)
	}
}

// Credentials are per request: a pooled connection must never carry one project's password to another.
func TestOpenCodeClient_PasswordIsSentPerClientOnSharedEndpoint(t *testing.T) {
	srv := newConnCountingServer(t)
	ctx := context.Background()

	a, err := runner.NewProjectOpenCodeClient(srv.URL, "password-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runner.NewProjectOpenCodeClient(srv.URL, "password-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []runner.OpenCodeClient{a, b, a} {
		if _, err := c.GetSession(ctx, "s1"); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"opencode:password-a", "opencode:password-b", "opencode:password-a"}
	got := srv.recordedAuths()
	if len(got) != len(want) {
		t.Fatalf("recorded %d requests, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d auth = %q, want %q", i, got[i], want[i])
		}
	}
}

// Distinct endpoints must not share a connection pool: each server sees only its own connections.
func TestOpenCodeClient_DistinctEndpointsGetOwnConnections(t *testing.T) {
	a := newConnCountingServer(t)
	b := newConnCountingServer(t)
	ctx := context.Background()

	ca, err := runner.NewProjectOpenCodeClient(a.URL, "pw", nil)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := runner.NewProjectOpenCodeClient(b.URL, "pw", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ca.GetSession(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := cb.GetSession(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if len(a.recordedAuths()) != 1 || len(b.recordedAuths()) != 1 {
		t.Fatalf("requests routed: a=%d b=%d, want 1 each", len(a.recordedAuths()), len(b.recordedAuths()))
	}
	if a.newConns.Load() != 1 || b.newConns.Load() != 1 {
		t.Fatalf("connections: a=%d b=%d, want 1 each", a.newConns.Load(), b.newConns.Load())
	}
}

// Message content is decoded in the envelope pass: arrays stay []any, absent and null content stay nil.
func TestOpenCodeClient_GetSessionMessages_ContentShapes(t *testing.T) {
	body := `{"data":[` +
		`{"id":"m1","type":"assistant","time":{"created":1700000000001},"content":[{"type":"text","text":"hi"}]},` +
		`{"id":"m2","type":"user","time":{"created":1700000000002}},` +
		`{"id":"m3","type":"user","time":{"created":1700000000003},"content":null}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	msgs, err := runner.NewOpenCodeClient(srv.URL, "pw", nil).GetSessionMessages(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	parts, ok := msgs[0].Content.([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("m1 content = %#v, want one part", msgs[0].Content)
	}
	if part, ok := parts[0].(map[string]any); !ok || part["text"] != "hi" {
		t.Errorf("m1 part = %#v", parts[0])
	}
	if msgs[1].Content != nil || msgs[2].Content != nil {
		t.Errorf("absent/null content = %#v, %#v; want nil", msgs[1].Content, msgs[2].Content)
	}
}
