package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

func TestClient_ReadPumpAndWritePump(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start hub in background
	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("failed to upgrade: %v", err)
		}
		client := NewClient(conn, hub)
		hub.register <- client
		go client.WritePump()
		go client.ReadPump()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	dialer := websocket.Dialer{}
	wsConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer wsConn.Close()

	// Send message to server
	testMsg := "hello world"
	if err := wsConn.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Server broadcasts back to clients registered in hub
	_, resp, err := wsConn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read echoed message: %v", err)
	}
	if string(resp) != testMsg {
		t.Fatalf("expected %s, got %s", testMsg, string(resp))
	}
}

func TestClient_SafeClose(t *testing.T) {
	hub := NewHub()
	c := NewClient(nil, hub)

	// Close client multiple times concurrently to ensure no panic (double-close check)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Close()
		}()
	}
	wg.Wait()
}
