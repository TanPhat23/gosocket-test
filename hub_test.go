package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHubNormalMessagingAndMultipleClients(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := NewHub()
	go h.Run(ctx)

	server := httptest.NewServer(NewRouter(h))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	const numClients = 3
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("client %d failed to connect: %v", i, err)
		}
		defer conn.Close()
		conns[i] = conn
	}

	time.Sleep(50 * time.Millisecond)

	// Client 0 sends a broadcast message
	testMsg := "hello from client 0"
	err := conns[0].WriteMessage(websocket.TextMessage, []byte(testMsg))
	if err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// All clients (including client 0) should receive the broadcast message
	var wg sync.WaitGroup
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int, c *websocket.Conn) {
			defer wg.Done()
			_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, msg, err := c.ReadMessage()
			if err != nil {
				t.Errorf("client %d failed to receive message: %v", idx, err)
				return
			}
			if !strings.Contains(string(msg), testMsg) {
				t.Errorf("client %d received %s, expected %s", idx, string(msg), testMsg)
			}
		}(i, conns[i])
	}
	wg.Wait()
}

func TestHubStopAndGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := NewHub()
	go h.Run(ctx)

	server := httptest.NewServer(NewRouter(h))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	// Stopping the hub should disconnect clients and close channels
	h.Stop()
	// Calling Stop multiple times should be safe (sync.Once)
	h.Stop()

	// Wait for client to receive close / disconnect
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Error("expected error or close frame when hub stops, got nil")
	}
}

func TestHubContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	h := NewHub()
	go h.Run(ctx)

	server := httptest.NewServer(NewRouter(h))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	// Cancel context to trigger shutdown
	cancel()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Error("expected connection error after context cancellation, got nil")
	}
}
