package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClientSafeClose(t *testing.T) {
	client := NewClient(nil)
	// Calling Close multiple times concurrently must not panic
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.Close()
		}()
	}
	wg.Wait()

	select {
	case <-client.done:
	default:
		t.Fatal("client.done channel should be closed")
	}
}

func TestHubBroadcastAndEviction(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	server := NewServer("", hub)
	ts := httptest.NewServer(http.HandlerFunc(server.HandleSocket))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	c1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial client 1: %v", err)
	}
	defer c1.Close()

	c2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial client 2: %v", err)
	}
	defer c2.Close()

	time.Sleep(50 * time.Millisecond)

	testMsg := []byte("hello websocket")
	if err := c1.WriteMessage(websocket.TextMessage, testMsg); err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg1, err := c1.ReadMessage()
	if err != nil {
		t.Fatalf("Client 1 failed to read broadcast: %v", err)
	}
	if string(msg1) != string(testMsg) {
		t.Fatalf("Expected %s, got %s", testMsg, msg1)
	}

	c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg2, err := c2.ReadMessage()
	if err != nil {
		t.Fatalf("Client 2 failed to read broadcast: %v", err)
	}
	if string(msg2) != string(testMsg) {
		t.Fatalf("Expected %s, got %s", testMsg, msg2)
	}
}

func TestHubSlowClientEviction(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	// Create a dummy client with a small buffer that is full
	client := &Client{
		msg:  make(chan []byte, 1),
		done: make(chan struct{}),
	}
	client.msg <- []byte("already full")

	hub.register <- client
	time.Sleep(20 * time.Millisecond)

	// Broadcast another message which should trigger eviction of the blocked client
	hub.broadcast <- []byte("evict me")
	time.Sleep(50 * time.Millisecond)

	// Check that the client was evicted and client.done closed
	select {
	case <-client.done:
	default:
		t.Fatal("Expected slow client to be evicted and done channel closed")
	}
}

func TestHubGracefulStop(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	server := NewServer("", hub)
	ts := httptest.NewServer(http.HandlerFunc(server.HandleSocket))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	hub.Stop()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("Expected read error/close after hub stopped, got nil")
	}
}

func TestConcurrentRegisterUnregister(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			client := NewClient(nil)
			hub.register <- client
			hub.broadcast <- []byte(fmt.Sprintf("msg %d", id))
			hub.unregister <- client
			client.Close()
		}(i)
	}
	wg.Wait()
}
