package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHandleSocket(t *testing.T) {
	// Initialize package-level hub as main() does
	hub = NewHub()
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(HandleSocket))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect first client
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	client1, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial client 1: %v", err)
	}
	defer client1.Close()

	// Connect second client
	client2, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial client 2: %v", err)
	}
	defer client2.Close()

	// Give time for registration
	time.Sleep(50 * time.Millisecond)

	// Send message from client1
	msg := []byte("broadcast test from client1")
	err = client1.WriteMessage(websocket.TextMessage, msg)
	if err != nil {
		t.Fatalf("failed to write message from client1: %v", err)
	}

	// client1 should receive echo (broadcast to all clients)
	_, p1, err := client1.ReadMessage()
	if err != nil {
		t.Fatalf("client1 failed to read broadcast: %v", err)
	}
	if !bytes.Equal(p1, msg) {
		t.Fatalf("client1 expected %s, got %s", msg, p1)
	}

	// client2 should also receive broadcast
	_, p2, err := client2.ReadMessage()
	if err != nil {
		t.Fatalf("client2 failed to read broadcast: %v", err)
	}
	if !bytes.Equal(p2, msg) {
		t.Fatalf("client2 expected %s, got %s", msg, p2)
	}
}

func TestHandleSocketNonWebSocketRequest(t *testing.T) {
	hub = NewHub()
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(HandleSocket))
	defer server.Close()

	// Send plain HTTP GET request (not a WebSocket upgrade request)
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	// Handshake should fail gracefully (gorilla/websocket returns 400 Bad Request)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request for non-websocket request, got %d", resp.StatusCode)
	}
}
