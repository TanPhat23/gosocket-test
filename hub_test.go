package main

import (
	"sync"
	"testing"
	"time"
)

func TestHubRegisterAndBroadcast(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	client1, clientConn1, cleanup1 := createTestClientPair(t, hub)
	defer cleanup1()
	client2, clientConn2, cleanup2 := createTestClientPair(t, hub)
	defer cleanup2()

	go client1.WritePump()
	go client2.WritePump()

	hub.register <- client1
	hub.register <- client2

	// Wait for both clients to be confirmed registered before broadcasting
	time.Sleep(20 * time.Millisecond)

	testMsg := []byte("broadcast to all")
	hub.broadcast <- testMsg

	// Both clients should receive the message
	clientConn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg1, err := clientConn1.ReadMessage()
	if err != nil {
		t.Fatalf("client1 ReadMessage failed: %v", err)
	}
	if string(msg1) != string(testMsg) {
		t.Errorf("client1 expected %q, got %q", string(testMsg), string(msg1))
	}

	clientConn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg2, err := clientConn2.ReadMessage()
	if err != nil {
		t.Fatalf("client2 ReadMessage failed: %v", err)
	}
	if string(msg2) != string(testMsg) {
		t.Errorf("client2 expected %q, got %q", string(testMsg), string(msg2))
	}
}

func TestHubUnregister(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	client, clientConn, cleanup := createTestClientPair(t, hub)
	defer cleanup()

	go client.WritePump()
	hub.register <- client

	// Unregister client
	hub.unregister <- client

	// Wait for unregistration to complete and client to close
	time.Sleep(50 * time.Millisecond)

	// Send broadcast; unregistered client should not receive it and its send channel should be closed
	hub.broadcast <- []byte("after unregister")

	select {
	case _, ok := <-client.send:
		if ok {
			t.Errorf("expected client.send to be closed after unregister")
		}
	case <-time.After(500 * time.Millisecond):
		t.Errorf("timed out waiting for client.send to be closed")
	}

	_ = clientConn
}

func TestHubSlowClientEviction(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	// Create client with standard send channel (capacity 256)
	client, clientConn, cleanup := createTestClientPair(t, hub)
	defer cleanup()

	// Do NOT start WritePump on client, so send channel is never drained
	hub.register <- client
	time.Sleep(20 * time.Millisecond)

	// Fill the client's send buffer (capacity is 256)
	for i := 0; i < 256; i++ {
		client.send <- []byte("filler")
	}

	// Now broadcast through hub; default case in hub broadcast should trigger slow client eviction
	hub.broadcast <- []byte("overflow message")

	// Wait for eviction
	time.Sleep(50 * time.Millisecond)

	// The client's send channel should be closed due to client.Close()
	select {
	case _, ok := <-client.send:
		// Could drain one of the filled messages or see closed channel.
		// Drain until closed or timeout.
		for ok {
			select {
			case _, ok = <-client.send:
			case <-time.After(500 * time.Millisecond):
				t.Fatalf("timed out draining send channel")
			}
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("client was not evicted/closed on buffer overflow")
	}

	_ = clientConn
}

func TestHubStopShutsDownAllClients(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	client1, _, cleanup1 := createTestClientPair(t, hub)
	defer cleanup1()
	client2, _, cleanup2 := createTestClientPair(t, hub)
	defer cleanup2()

	hub.register <- client1
	hub.register <- client2
	time.Sleep(20 * time.Millisecond)

	// Stop the hub
	hub.Stop()

	// Both clients should have their send channels closed
	select {
	case _, ok := <-client1.send:
		if ok {
			t.Errorf("expected client1.send to be closed after hub.Stop()")
		}
	case <-time.After(500 * time.Millisecond):
		t.Errorf("timed out waiting for client1.send to close")
	}

	select {
	case _, ok := <-client2.send:
		if ok {
			t.Errorf("expected client2.send to be closed after hub.Stop()")
		}
	case <-time.After(500 * time.Millisecond):
		t.Errorf("timed out waiting for client2.send to close")
	}
}

func TestHubStopIdempotent(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hub.Stop()
		}()
	}
	wg.Wait()

	// Subsequent Stop() call should also succeed immediately without hanging or panic
	hub.Stop()
}
