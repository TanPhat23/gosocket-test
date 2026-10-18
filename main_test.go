package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHandleSocketWithDefaultHub(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize package-level hub as in main()
	hub = NewHub()
	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(HandleSocket))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial default HandleSocket: %v", err)
	}
	defer conn.Close()

	// Send message and verify read doesn't error
	testMsg := []byte("ping test default hub")
	if err := conn.WriteMessage(websocket.TextMessage, testMsg); err != nil {
		t.Fatalf("failed to write message: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read echoed message: %v", err)
	}
	if string(msg) != string(testMsg) {
		t.Fatalf("expected message %s, got %s", testMsg, msg)
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	h := NewHub()
	go h.Run(ctx)

	server := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: NewRouter(h),
	}

	serverStopped := make(chan struct{})
	go func() {
		defer close(serverStopped)
		_ = server.ListenAndServe()
	}()

	// Wait briefly for server to bind or verify with actual listener
	// Since 127.0.0.1:0 binds dynamically, let's test graceful shutdown lifecycle:
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()

	cancel() // cancel hub context
	err := server.Shutdown(shutdownCtx)
	if err != nil && err != http.ErrServerClosed {
		t.Fatalf("unexpected server shutdown error: %v", err)
	}

	select {
	case <-serverStopped:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to stop")
	}
}
