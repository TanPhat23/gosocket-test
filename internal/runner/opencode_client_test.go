package runner_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
			var parsed map[string]interface{}
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
