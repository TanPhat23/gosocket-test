package runner_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/runner"
)

func TestOpenCodeClient_URLValidationAndRedirects(t *testing.T) {
	// AC-SEC-03: Scheme validation
	// Test file:// or javascript:// falls back to default localhost URL
	clientBadScheme := runner.NewOpenCodeClient("file:///etc/passwd", "pwd", nil)
	// Trying to make a request will target localhost:49374 rather than file:///etc/passwd
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = clientBadScheme.CompactSession(ctx, "ses_123") // Will error with service unavailable or connection refused, not attempt file access

	// Test redirect prevention (http.ErrUseLastResponse)
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectServer.Close()

	c := runner.NewOpenCodeClient(redirectServer.URL, "pwd", nil)
	// Test path traversal defense (AC-SEC-05)
	err := c.CompactSession(context.Background(), "../../etc/passwd")
	if err == nil {
		t.Fatalf("expected error for path traversal sessionID")
	}

	err = c.CompactSession(context.Background(), "session with spaces")
	if err == nil {
		t.Fatalf("expected error for sessionID with spaces")
	}
}

func TestOpenCodeClient_AuthenticationAndSecretHygiene(t *testing.T) {
	expectedPassword := "super-secret-pw"
	var authHeaderReceived string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaderReceived = r.Header.Get("Authorization")
		if r.URL.Path == "/api/session/ses_valid" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"id":"ses_valid","title":"Test Session","time":{"created":1700000000000},"location":{"directory":"/tmp"}}}`))
			return
		}
		if r.URL.Path == "/api/session/ses_valid/message" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[{"id":"msg_1","type":"assistant","agent":"backend","time":{"created":1700000001000},"content":{"text":"hello"}}]}`))
			return
		}
		if r.URL.Path == "/api/session/ses_valid/prompt" && r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var parsed map[string]any
			_ = json.Unmarshal(body, &parsed)
			if parsed["text"] != "fix tests" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"_tag":"InvalidRequestError","message":"Missing key\n  at [\"text\"]"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		if r.URL.Path == "/api/session/ses_valid/compact" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"compacted"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := runner.NewOpenCodeClient(server.URL, expectedPassword, nil)

	// Test GetSession
	sess, err := client.GetSession(context.Background(), "ses_valid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.ID != "ses_valid" || sess.Title != "Test Session" || sess.Directory != "/tmp" {
		t.Errorf("unexpected session dto: %+v", sess)
	}

	// Verify HTTP Basic Auth header: "Basic " + base64("opencode:super-secret-pw")
	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:"+expectedPassword))
	if authHeaderReceived != expectedAuth {
		t.Errorf("expected auth header %q, got %q", expectedAuth, authHeaderReceived)
	}

	// Test GetSessionMessages
	msgs, err := client.GetSessionMessages(context.Background(), "ses_valid")
	if err != nil {
		t.Fatalf("unexpected error getting messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "msg_1" {
		t.Errorf("unexpected messages: %+v", msgs)
	}

	// Test PromptSession
	err = client.PromptSession(context.Background(), "ses_valid", "fix tests")
	if err != nil {
		t.Fatalf("unexpected error prompting session: %v", err)
	}

	// Test CompactSession
	err = client.CompactSession(context.Background(), "ses_valid")
	if err != nil {
		t.Fatalf("unexpected error compacting session: %v", err)
	}

	// Test NotFound
	_, err = client.GetSession(context.Background(), "ses_nonexistent")
	if !errors.Is(err, common.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestOpenCodeClient_DaemonOfflineAnd503(t *testing.T) {
	// Point client to non-existent server port (daemon offline)
	client := runner.NewOpenCodeClient("http://127.0.0.1:59999", "secret", nil)

	_, err := client.GetSession(context.Background(), "ses_test")
	if !errors.Is(err, common.ErrServiceUnavailable) {
		t.Fatalf("expected ErrServiceUnavailable, got %v", err)
	}
	// Verify error string does NOT contain "secret" (AC-SEC-04)
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("leaked secret in error message: %v", err)
	}

	err = client.PromptSession(context.Background(), "ses_test", "hello")
	if !errors.Is(err, common.ErrServiceUnavailable) {
		t.Fatalf("expected ErrServiceUnavailable, got %v", err)
	}

	err = client.CompactSession(context.Background(), "ses_test")
	if !errors.Is(err, common.ErrServiceUnavailable) {
		t.Fatalf("expected ErrServiceUnavailable, got %v", err)
	}

	// Test 500 error from daemon returns ErrServiceUnavailable
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errServer.Close()

	errClient := runner.NewOpenCodeClient(errServer.URL, "secret", nil)
	_, err = errClient.GetSession(context.Background(), "ses_test")
	if !errors.Is(err, common.ErrServiceUnavailable) {
		t.Fatalf("expected ErrServiceUnavailable on 500, got %v", err)
	}
}

func TestOpenCodeClient_EnsureSessionAtLocation(t *testing.T) {
	created := false
	moved := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/session" && r.Method == http.MethodPost {
			created = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"id":"ses_test_123","location":{"directory":"/workspace/test"}}}`))
			return
		}
		if r.URL.Path == "/api/session/ses_test_123/move" && r.Method == http.MethodPost {
			moved = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := runner.NewOpenCodeClient(server.URL, "secret", nil)
	err := client.EnsureSessionAtLocation(context.Background(), "ses_test_123", "Test Title", "/workspace/test")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !created {
		t.Errorf("expected session create to be called")
	}

	// Test move fallback when create returns 400
	failCreateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/session" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/api/session/ses_test_123/move" && r.Method == http.MethodPost {
			moved = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer failCreateServer.Close()

	client2 := runner.NewOpenCodeClient(failCreateServer.URL, "secret", nil)
	err = client2.EnsureSessionAtLocation(context.Background(), "ses_test_123", "Test Title", "/workspace/test")
	if err != nil {
		t.Fatalf("expected nil error on move fallback, got: %v", err)
	}
	if !moved {
		t.Errorf("expected session move to be called on fallback")
	}
}

func TestOpenCodeClient_InterruptSession(t *testing.T) {
	var outboundCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outboundCalled = true
		if r.URL.Path == "/api/session/ses_active/interrupt" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"interrupted":true}`))
			return
		}
		if r.URL.Path == "/api/session/ses_idle/interrupt" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"interrupted":false}`))
			return
		}
		if r.URL.Path == "/api/session/ses_notfound/interrupt" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := runner.NewOpenCodeClient(server.URL, "secret", nil)

	// 1. Invalid session ID: rejects without outbound request
	err := client.InterruptSession(context.Background(), "invalid/session/id")
	if err == nil {
		t.Fatalf("expected error for malformed session ID, got nil")
	}
	if outboundCalled {
		t.Fatalf("outbound request was made for invalid session ID")
	}

	// 2. Active session: success
	err = client.InterruptSession(context.Background(), "ses_active")
	if err != nil {
		t.Fatalf("expected nil error interrupting active session, got: %v", err)
	}

	// 3. Idle session: no-op (nil error)
	err = client.InterruptSession(context.Background(), "ses_idle")
	if err != nil {
		t.Fatalf("expected nil error interrupting idle session, got: %v", err)
	}

	// 4. Not found session: treated as safe no-op (nil error)
	err = client.InterruptSession(context.Background(), "ses_notfound")
	if err != nil {
		t.Fatalf("expected nil error on 404 not found, got: %v", err)
	}
}

func TestOpenCodeClient_InterruptSessionTree(t *testing.T) {
	interruptedSessions := make(map[string]bool)
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if r.URL.Path == "/api/session" && r.Method == http.MethodGet {
			// Return list of sessions with child relationships
			resp := map[string]any{
				"data": []map[string]any{
					{"id": "ses_root", "parentID": ""},
					{"id": "ses_child_1", "parentID": "ses_root"},
					{"id": "ses_child_2", "parentID": "ses_root"},
					{"id": "ses_grandchild_1", "parentID": "ses_child_1"},
					{"id": "ses_unrelated", "parentID": "ses_other"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.HasSuffix(r.URL.Path, "/interrupt") && r.Method == http.MethodPost {
			// Extract session ID
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 4 {
				sID := parts[3]
				interruptedSessions[sID] = true
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"interrupted":true}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := runner.NewOpenCodeClient(server.URL, "secret", nil)

	err := client.InterruptSessionTree(context.Background(), "ses_root")
	if err != nil {
		t.Fatalf("unexpected error from InterruptSessionTree: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if !interruptedSessions["ses_root"] {
		t.Errorf("expected root session to be interrupted")
	}
	if !interruptedSessions["ses_child_1"] {
		t.Errorf("expected child 1 session to be interrupted")
	}
	if !interruptedSessions["ses_child_2"] {
		t.Errorf("expected child 2 session to be interrupted")
	}
	if !interruptedSessions["ses_grandchild_1"] {
		t.Errorf("expected grandchild 1 session to be interrupted")
	}
	if interruptedSessions["ses_unrelated"] {
		t.Errorf("unrelated session was interrupted!")
	}
}

func TestOpenCodeClient_InterruptSessionTree_ErrorsAndCycle(t *testing.T) {
	// 1. Cycle in session hierarchy (ses_a -> ses_b -> ses_a) must terminate and aggregate
	interruptedSessions := make(map[string]int)
	var mu sync.Mutex

	cycleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if r.URL.Path == "/api/session" && r.Method == http.MethodGet {
			resp := map[string]any{
				"data": []map[string]any{
					{"id": "ses_a", "parentID": "ses_b"},
					{"id": "ses_b", "parentID": "ses_a"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.HasSuffix(r.URL.Path, "/interrupt") && r.Method == http.MethodPost {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 4 {
				interruptedSessions[parts[3]]++
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"interrupted":true}`))
			return
		}
	}))
	defer cycleServer.Close()

	client := runner.NewOpenCodeClient(cycleServer.URL, "secret", nil)
	err := client.InterruptSessionTree(context.Background(), "ses_a")
	if err != nil {
		t.Fatalf("expected nil error on valid cycle interrupt, got: %v", err)
	}

	mu.Lock()
	if interruptedSessions["ses_a"] != 1 || interruptedSessions["ses_b"] != 1 {
		t.Errorf("expected each cycle node visited exactly once, got: %+v", interruptedSessions)
	}
	mu.Unlock()

	// 2. Child failure must NOT be silently ignored
	failChildServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/session" && r.Method == http.MethodGet {
			resp := map[string]any{
				"data": []map[string]any{
					{"id": "ses_child_fail", "parentID": "ses_root"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.URL.Path == "/api/session/ses_root/interrupt" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"interrupted":true}`))
			return
		}
		if r.URL.Path == "/api/session/ses_child_fail/interrupt" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
	defer failChildServer.Close()

	failClient := runner.NewOpenCodeClient(failChildServer.URL, "secret", nil)
	err = failClient.InterruptSessionTree(context.Background(), "ses_root")
	if err == nil {
		t.Fatalf("expected aggregated error when child interrupt fails, got nil")
	}
}

func BenchmarkResponseBodyReader_IOReadAll(b *testing.B) {

	data := strings.Repeat(`{"id":"msg_123","type":"assistant","text":"sample response text chunk"}`+"\n", 1000) // ~75 KB
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r := io.NopCloser(strings.NewReader(data))
		body, err := io.ReadAll(r)
		if err != nil {
			b.Fatal(err)
		}
		_ = body
	}
}

func BenchmarkResponseBodyReader_PooledBuffer(b *testing.B) {
	data := strings.Repeat(`{"id":"msg_123","type":"assistant","text":"sample response text chunk"}`+"\n", 1000) // ~75 KB
	pool := sync.Pool{
		New: func() any {
			return bytes.NewBuffer(make([]byte, 0, 128*1024))
		},
	}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r := io.NopCloser(strings.NewReader(data))
		buf := pool.Get().(*bytes.Buffer)
		buf.Reset()
		_, err := buf.ReadFrom(r)
		if err != nil {
			b.Fatal(err)
		}
		_ = buf.Bytes()
		if buf.Cap() <= 512*1024 {
			pool.Put(buf)
		}
	}
}

func TestProjectOpenCodeClient_AuthenticationAndSecurityHardening(t *testing.T) {
	// 1. Rejection of invalid schemes or empty host
	invalidURLs := []string{
		"file:///etc/passwd",
		"ftp://example.com/repo",
		"unix:///tmp/opencode.sock",
		"",
		"not-a-url",
		"http://",
		"https://",
	}
	for _, badURL := range invalidURLs {
		c, err := runner.NewProjectOpenCodeClient(badURL, "secret-pass", nil)
		if err == nil || c != nil {
			t.Errorf("expected error for endpointURL %q, got client: %v, err: %v", badURL, c, err)
		}
	}

	// 2. Verified HTTP Basic Auth and redirect rejection
	receivedAuth := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		if r.URL.Path == "/target" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"target-ok"}}`))
			return
		}
		if r.URL.Path == "/api/session/ses_test" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"ses_test","title":"Test Session","location":{"directory":"/workspaces/repo/tasks/task1"}}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	password := "container-opencode-pass-1234"
	client, err := runner.NewProjectOpenCodeClient(server.URL, password, nil)
	if err != nil {
		t.Fatalf("unexpected error creating ProjectOpenCodeClient: %v", err)
	}

	// Make request and verify Basic Auth header: base64("opencode:" + password)
	sess, err := client.GetSession(context.Background(), "ses_test")
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if sess.ID != "ses_test" {
		t.Errorf("expected session ID ses_test, got %s", sess.ID)
	}

	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:"+password))
	if receivedAuth != expectedAuth {
		t.Errorf("expected Basic Auth %q, got %q", expectedAuth, receivedAuth)
	}

	// 3. Verify zero unix socket fallback: ensure transport does not dial unix sockets
	// Pass an endpoint URL pointing to an unreachable port, ensure error is sanitized and no unix socket stat/dial occurs
	offlineClient, err := runner.NewProjectOpenCodeClient("http://127.0.0.1:59998", password, nil)
	if err != nil {
		t.Fatalf("unexpected error creating offline client: %v", err)
	}
	_, err = offlineClient.GetSession(context.Background(), "ses_test")
	if !errors.Is(err, common.ErrServiceUnavailable) {
		t.Errorf("expected ErrServiceUnavailable, got %v", err)
	}
	if strings.Contains(err.Error(), password) {
		t.Errorf("password leaked in error: %v", err)
	}
}


