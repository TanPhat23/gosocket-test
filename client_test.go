package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClientTeardownOnce(t *testing.T) {
	c := &Client{
		msg:  make(chan []byte, 1),
		done: make(chan struct{}),
	}

	// Should not panic when Close() is called multiple times
	c.Close()
	c.Close()

	// done channel should be closed
	select {
	case <-c.done:
	default:
		t.Fatal("expected c.done to be closed")
	}
}

func TestSlowClientEvictionNoDoubleClosePanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := NewHub()
	go h.Run(ctx)

	server := httptest.NewServer(NewRouter(h))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	slowConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial slow client: %v", err)
	}
	defer slowConn.Close()

	time.Sleep(50 * time.Millisecond)

	senderConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial sender: %v", err)
	}
	defer senderConn.Close()

	time.Sleep(50 * time.Millisecond)

	// Send more messages than buffer size (256) without reading from slow client
	for i := 0; i < 300; i++ {
		err := senderConn.WriteMessage(websocket.TextMessage, []byte("flood"))
		if err != nil {
			t.Fatalf("failed to write message %d: %v", i, err)
		}
	}

	time.Sleep(100 * time.Millisecond)

	// Close slowConn to trigger readPump unregister, which should not panic
	slowConn.Close()
	time.Sleep(50 * time.Millisecond)
}

func TestClientPingPongHeartbeat(t *testing.T) {
	cfg := ClientConfig{
		WriteWait:  100 * time.Millisecond,
		PongWait:   300 * time.Millisecond,
		PingPeriod: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := NewHub()
	go h.Run(ctx)

	server := httptest.NewServer(NewRouterWithConfig(h, cfg))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	var pingCount int32
	conn.SetPingHandler(func(appData string) error {
		atomic.AddInt32(&pingCount, 1)
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(100*time.Millisecond))
	})

	stopRead := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopRead:
				return
			default:
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}
	}()

	// Wait enough for ping/pong exchange
	time.Sleep(350 * time.Millisecond)
	close(stopRead)

	if count := atomic.LoadInt32(&pingCount); count == 0 {
		t.Errorf("expected at least 1 ping, got %d", count)
	}
}

func TestClientReadLimit(t *testing.T) {
	cfg := ClientConfig{
		MaxMessageSize: 64,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := NewHub()
	go h.Run(ctx)

	server := httptest.NewServer(NewRouterWithConfig(h, cfg))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	// Send message larger than maxMessageSize (64 bytes)
	largeMsg := strings.Repeat("A", 128)
	err = conn.WriteMessage(websocket.TextMessage, []byte(largeMsg))
	if err != nil {
		t.Fatalf("failed to write message: %v", err)
	}

	// The server should close connection with close error due to read limit exceeded
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("expected error reading after sending message exceeding limit, got nil")
	}
}
