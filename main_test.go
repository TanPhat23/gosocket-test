package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Helper to convert httptest URL to ws URL
func httpToWS(u string) string {
	parsed, _ := url.Parse(u)
	parsed.Scheme = "ws"
	parsed.Path = "/ws"
	return parsed.String()
}

func TestServerE2EBroadcastSingleClient(t *testing.T) {
	cfg := Config{
		Addr: ":0",
	}
	server := NewServer(cfg)
	ts := httptest.NewServer(server.httpServer.Handler)
	defer ts.Close()
	defer server.Shutdown(context.Background())

	wsURL := httpToWS(ts.URL)
	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v, resp: %v", err, resp)
	}
	defer ws.Close()

	testMsg := "hello world"
	if err := ws.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}
	if msgType != websocket.TextMessage || string(msg) != testMsg {
		t.Fatalf("Expected message %q, got %q (type: %d)", testMsg, string(msg), msgType)
	}
}

func TestServerE2EBroadcastMultipleClients(t *testing.T) {
	cfg := Config{
		Addr: ":0",
	}
	server := NewServer(cfg)
	ts := httptest.NewServer(server.httpServer.Handler)
	defer ts.Close()
	defer server.Shutdown(context.Background())

	wsURL := httpToWS(ts.URL)

	c1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial c1 failed: %v", err)
	}
	defer c1.Close()

	c2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial c2 failed: %v", err)
	}
	defer c2.Close()

	time.Sleep(50 * time.Millisecond)

	testMsg := "broadcast to all"
	if err := c1.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
		t.Fatalf("c1 WriteMessage failed: %v", err)
	}

	c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg1, err := c1.ReadMessage()
	if err != nil {
		t.Fatalf("c1 ReadMessage failed: %v", err)
	}
	if string(msg1) != testMsg {
		t.Fatalf("c1 expected %q, got %q", testMsg, string(msg1))
	}

	c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg2, err := c2.ReadMessage()
	if err != nil {
		t.Fatalf("c2 ReadMessage failed: %v", err)
	}
	if string(msg2) != testMsg {
		t.Fatalf("c2 expected %q, got %q", testMsg, string(msg2))
	}
}

func TestServerOriginCheck(t *testing.T) {
	cfg := Config{
		Addr:           ":0",
		AllowedOrigins: []string{"https://example.com", "http://allowed.local"},
	}
	server := NewServer(cfg)
	ts := httptest.NewServer(server.httpServer.Handler)
	defer ts.Close()
	defer server.Shutdown(context.Background())

	wsURL := httpToWS(ts.URL)

	t.Run("Disallowed origin returns 403 / handshake failure", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("Origin", "http://malicious.com")

		_, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
		if err == nil {
			t.Fatalf("Expected dial to fail for disallowed origin, but succeeded")
		}
		if resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("Expected status 403 Forbidden, got: %v", resp)
		}
	})

	t.Run("Allowed origin connects successfully", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("Origin", "https://example.com")

		ws, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Expected dial to succeed for allowed origin, got err: %v, resp: %v", err, resp)
		}
		defer ws.Close()
	})

	t.Run("Missing origin (same-origin / non-browser) connects successfully", func(t *testing.T) {
		ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Expected dial to succeed with no Origin, got err: %v, resp: %v", err, resp)
		}
		defer ws.Close()
	})

	t.Run("Host match origin connects successfully", func(t *testing.T) {
		headers := http.Header{}
		parsed, _ := url.Parse(ts.URL)
		headers.Set("Origin", "http://"+parsed.Host)

		ws, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Expected dial to succeed for matching host origin, got err: %v, resp: %v", err, resp)
		}
		defer ws.Close()
	})
}

func TestServerGracefulShutdown(t *testing.T) {
	cfg := Config{
		Addr: ":0",
	}
	server := NewServer(cfg)
	ts := httptest.NewServer(server.httpServer.Handler)
	defer ts.Close()

	wsURL := httpToWS(ts.URL)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = ws.ReadMessage()
	if err == nil {
		t.Fatalf("Expected client to receive close / disconnect error, but ReadMessage succeeded")
	}
}

func TestServerOversizedPayloadRejection(t *testing.T) {
	cfg := Config{
		Addr: ":0",
	}
	server := NewServer(cfg)
	ts := httptest.NewServer(server.httpServer.Handler)
	defer ts.Close()
	defer server.Shutdown(context.Background())

	wsURL := httpToWS(ts.URL)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer ws.Close()

	// maxMessageSize is 512KB. Send 512KB + 1KB.
	oversized := make([]byte, maxMessageSize+1024)
	if err := ws.WriteMessage(websocket.BinaryMessage, oversized); err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = ws.ReadMessage()
	if err == nil {
		t.Fatalf("Expected connection closure or error after oversized payload, but ReadMessage succeeded")
	}
}

func TestServerAllowedOriginsEnv(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://foo.com, https://bar.com")
	cfg := Config{
		Addr: ":0",
	}
	server := NewServer(cfg)
	ts := httptest.NewServer(server.httpServer.Handler)
	defer ts.Close()
	defer server.Shutdown(context.Background())

	wsURL := httpToWS(ts.URL)

	headers := http.Header{}
	headers.Set("Origin", "https://foo.com")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("Expected origin from env to succeed, got: %v", err)
	}
	ws.Close()

	headers.Set("Origin", "https://notallowed.com")
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err == nil {
		t.Fatalf("Expected notallowed origin to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Expected 403 Forbidden, got: %v", resp)
	}
}
