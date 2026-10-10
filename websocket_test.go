package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func setupTestServer(t *testing.T) (*httptest.Server, *Hub, func()) {
	t.Helper()
	testHub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	go testHub.RunWithContext(ctx)

	// Custom upgrader for tests that permits localhost test origins
	testUpgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("test upgrade error: %v", err)
			return
		}
		c := NewClient(conn)
		testHub.register <- c
		go c.Read(testHub)
		go c.Write()
	}))

	cleanup := func() {
		server.Close()
		cancel()
	}

	return server, testHub, cleanup
}

func TestClientConnectionAndBroadcast(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	dialer := websocket.Dialer{}
	conn1, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial client 1: %v", err)
	}
	defer conn1.Close()

	conn2, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial client 2: %v", err)
	}
	defer conn2.Close()

	// Wait briefly for clients to register
	time.Sleep(50 * time.Millisecond)

	testMsg := "hello everyone"
	if err := conn1.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
		t.Fatalf("failed to write message from client 1: %v", err)
	}

	// Both conn1 and conn2 should receive broadcast
	var wg sync.WaitGroup
	wg.Add(2)
	receivedCount := 0
	var mu sync.Mutex

	checkReceive := func(c *websocket.Conn, clientName string) {
		defer wg.Done()
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, msg, err := c.ReadMessage()
		if err != nil {
			t.Errorf("[%s] error reading broadcast: %v", clientName, err)
			return
		}
		if string(msg) != testMsg {
			t.Errorf("[%s] expected %q, got %q", clientName, testMsg, string(msg))
			return
		}
		mu.Lock()
		receivedCount++
		mu.Unlock()
	}

	go checkReceive(conn1, "client1")
	go checkReceive(conn2, "client2")

	wg.Wait()
	if receivedCount != 2 {
		t.Fatalf("expected 2 clients to receive broadcast, got %d", receivedCount)
	}
}

func TestReadLimitEnforcement(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	// Send message larger than maxMessageSize (512 bytes)
	largeMessage := make([]byte, maxMessageSize+100)
	for i := range largeMessage {
		largeMessage[i] = 'a'
	}

	if err := conn.WriteMessage(websocket.TextMessage, largeMessage); err != nil {
		t.Fatalf("failed to send large message: %v", err)
	}

	// Server should close the connection or return close error
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatalf("expected error/connection drop due to ReadLimit exceed, but got nil")
	}
}

func TestHeartbeatPingPong(t *testing.T) {
	// Test that server pong handler extends deadline and client can exchange ping/pong
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	// Set ping handler on client to respond with pong automatically or verify server ping
	pingReceived := make(chan struct{}, 1)
	conn.SetPingHandler(func(appData string) error {
		select {
		case pingReceived <- struct{}{}:
		default:
		}
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(writeWait))
	})

	// Manually send pong from client to server to ensure server PongHandler accepts it
	err = conn.WriteControl(websocket.PongMessage, []byte("test-pong"), time.Now().Add(writeWait))
	if err != nil {
		t.Fatalf("failed to send pong: %v", err)
	}

	// Also send a normal message to verify connection remains healthy
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping test msg")); err != nil {
		t.Fatalf("failed to write message: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read back message: %v", err)
	}
	if string(msg) != "ping test msg" {
		t.Fatalf("unexpected message: %s", string(msg))
	}
}

func TestGracefulUnregisterAndHubStop(t *testing.T) {
	server, testHub, cleanup := setupTestServer(t)
	defer cleanup()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Close client conn from client side
	_ = conn.Close()

	// Wait for unregistration defer logic to run
	time.Sleep(100 * time.Millisecond)

	// Stop the hub and verify no deadlock/panic occurs
	testHub.Stop()

	// Stop again to test idempotent stop
	testHub.Stop()
}

func TestCheckOrigin(t *testing.T) {
	req1, _ := http.NewRequest("GET", "http://example.com/ws", nil)
	req1.Host = "example.com"
	req1.Header.Set("Origin", "http://example.com")
	if !upgrader.CheckOrigin(req1) {
		t.Fatalf("expected origin http://example.com to be allowed for host example.com")
	}

	req2, _ := http.NewRequest("GET", "https://example.com/ws", nil)
	req2.Host = "example.com"
	req2.Header.Set("Origin", "https://example.com")
	if !upgrader.CheckOrigin(req2) {
		t.Fatalf("expected origin https://example.com to be allowed for host example.com")
	}

	req3, _ := http.NewRequest("GET", "http://example.com/ws", nil)
	req3.Host = "example.com"
	req3.Header.Set("Origin", "http://malicious.com")
	if upgrader.CheckOrigin(req3) {
		t.Fatalf("expected origin http://malicious.com to be denied for host example.com")
	}

	req4, _ := http.NewRequest("GET", "http://example.com/ws", nil)
	req4.Host = "example.com"
	if !upgrader.CheckOrigin(req4) {
		t.Fatalf("expected empty origin to be allowed")
	}
}
