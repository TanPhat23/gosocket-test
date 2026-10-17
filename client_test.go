package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func createTestClientPair(t *testing.T, hub *Hub) (*Client, *websocket.Conn, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	var serverConn *websocket.Conn
	var serverErr error
	var connOnce sync.WaitGroup
	connOnce.Add(1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverConn, serverErr = upgrader.Upgrade(w, r, nil)
		connOnce.Done()
	}))

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}

	connOnce.Wait()
	if serverErr != nil {
		t.Fatalf("Upgrade failed: %v", serverErr)
	}

	client := NewClient(hub, serverConn)

	cleanup := func() {
		client.Close()
		clientConn.Close()
		server.Close()
	}

	return client, clientConn, cleanup
}

func TestClientCloseOnce(t *testing.T) {
	hub := NewHub()
	client, clientConn, cleanup := createTestClientPair(t, hub)
	defer cleanup()

	// Calling Close multiple times concurrently must not panic.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.Close()
		}()
	}
	wg.Wait()

	// Verify send channel is closed
	select {
	case _, ok := <-client.send:
		if ok {
			t.Errorf("expected send channel to be closed")
		}
	default:
		t.Errorf("expected send channel to be closed, but receive would block")
	}

	_ = clientConn
}

func TestClientReadPumpMessage(t *testing.T) {
	hub := NewHub()
	client, clientConn, cleanup := createTestClientPair(t, hub)
	defer cleanup()

	go client.ReadPump()

	testMsg := []byte("hello websocket")
	err := clientConn.WriteMessage(websocket.TextMessage, testMsg)
	if err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}

	select {
	case msg := <-hub.broadcast:
		if string(msg) != string(testMsg) {
			t.Errorf("expected message %q, got %q", string(testMsg), string(msg))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message on hub.broadcast")
	}
}

func TestClientWritePumpMessage(t *testing.T) {
	hub := NewHub()
	client, clientConn, cleanup := createTestClientPair(t, hub)
	defer cleanup()

	go client.WritePump()

	testMsg := []byte("outbound message")
	client.send <- testMsg

	messageType, msg, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}
	if messageType != websocket.TextMessage {
		t.Errorf("expected TextMessage, got %d", messageType)
	}
	if string(msg) != string(testMsg) {
		t.Errorf("expected %q, got %q", string(testMsg), string(msg))
	}
}

func TestClientReadPumpPongDeadline(t *testing.T) {
	hub := NewHub()
	client, clientConn, cleanup := createTestClientPair(t, hub)
	defer cleanup()

	go client.ReadPump()

	// Send a ping and expect pong handler to handle it or respond
	err := clientConn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("failed to write ping: %v", err)
	}

	// Wait briefly to make sure connection didn't drop
	time.Sleep(100 * time.Millisecond)

	// Send a message to ensure ReadPump is still reading
	err = clientConn.WriteMessage(websocket.TextMessage, []byte("alive"))
	if err != nil {
		t.Fatalf("WriteMessage after ping failed: %v", err)
	}

	select {
	case msg := <-hub.broadcast:
		if string(msg) != "alive" {
			t.Errorf("expected 'alive', got %q", string(msg))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message after ping")
	}
}
