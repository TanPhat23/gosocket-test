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

// Helper to set up a test websocket server and client connection pair
func setupWebSocketPair(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *websocket.Conn) {
	t.Helper()
	server := httptest.NewServer(handler)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	clientConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		server.Close()
		t.Fatalf("failed to dial websocket: %v", err)
	}

	return server, clientConn
}

func TestClientWriteMessages(t *testing.T) {
	serverConnChan := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}

	server, clientConn := setupWebSocketPair(t, func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnChan <- conn
	})
	defer server.Close()
	defer clientConn.Close()

	serverWsConn := <-serverConnChan
	defer serverWsConn.Close()

	c := NewClient(serverWsConn)
	go c.Write()

	// Send message through c.msg
	testMsg := []byte("hello from client write")
	c.msg <- testMsg

	// Client connection should read this message
	messageType, p, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message on client side: %v", err)
	}
	if messageType != websocket.TextMessage {
		t.Fatalf("expected message type %d, got %d", websocket.TextMessage, messageType)
	}
	if !bytes.Equal(p, testMsg) {
		t.Fatalf("expected %s, got %s", testMsg, p)
	}

	// Close c.msg, c.Write should terminate and close underlying conn
	close(c.msg)

	// Attempting to read should result in EOF / close error
	_, _, err = clientConn.ReadMessage()
	if err == nil {
		t.Fatal("expected error reading after client.Write closed, got nil")
	}
}

func TestClientWriteDoneChannel(t *testing.T) {
	serverConnChan := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}

	server, clientConn := setupWebSocketPair(t, func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnChan <- conn
	})
	defer server.Close()
	defer clientConn.Close()

	serverWsConn := <-serverConnChan
	defer serverWsConn.Close()

	c := NewClient(serverWsConn)
	go c.Write()

	// Close c.done to signal exit
	close(c.done)

	// Connection should close
	_, _, err := clientConn.ReadMessage()
	if err == nil {
		t.Fatal("expected error reading after closing c.done, got nil")
	}
}

func TestClientReadToHub(t *testing.T) {
	serverConnChan := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}

	server, clientConn := setupWebSocketPair(t, func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnChan <- conn
	})
	defer server.Close()
	defer clientConn.Close()

	serverWsConn := <-serverConnChan

	h := NewHub()
	// Drain register channel or run hub
	go func() {
		for range h.register {
		}
	}()

	c := NewClient(serverWsConn)
	go c.Read(h)

	// Write message from clientConn to serverWsConn
	testMsg := []byte("hello from client connection")
	err := clientConn.WriteMessage(websocket.TextMessage, testMsg)
	if err != nil {
		t.Fatalf("failed to write message from clientConn: %v", err)
	}

	select {
	case received := <-h.broadcast:
		if !bytes.Equal(received, testMsg) {
			t.Fatalf("expected broadcast %s, got %s", testMsg, received)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for message to be broadcast to hub")
	}

	// Close client connection, c.Read should exit and send unregister to hub
	clientConn.Close()

	select {
	case unregClient := <-h.unregister:
		if unregClient != c {
			t.Fatalf("expected unregister for client %v, got %v", c, unregClient)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for client unregistration after disconnect")
	}
}
