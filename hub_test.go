package main

import (
	"bytes"
	"testing"
	"time"
)

func TestHubRegisterAndUnregister(t *testing.T) {
	h := NewHub()
	go h.Run()

	c := &Client{
		msg:  make(chan []byte, 256),
		done: make(chan struct{}),
	}

	// Register client
	h.register <- c

	// Wait briefly for registration
	time.Sleep(20 * time.Millisecond)

	// Send a broadcast to verify the registered client receives it
	testMsg := []byte("hello hub")
	h.broadcast <- testMsg

	select {
	case received := <-c.msg:
		if !bytes.Equal(received, testMsg) {
			t.Fatalf("expected %s, got %s", testMsg, received)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for message after registration")
	}

	// Unregister client
	h.unregister <- c

	// c.done and c.msg should be closed upon unregister
	select {
	case <-c.done:
		// success: done channel closed
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for client done channel to close")
	}

	select {
	case _, ok := <-c.msg:
		if ok {
			t.Fatal("expected client msg channel to be closed")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for client msg channel to close")
	}

	// Unregistering an already unregistered client should be handled gracefully (no panic)
	h.unregister <- c
	time.Sleep(20 * time.Millisecond)
}

func TestHubBroadcastMultipleClients(t *testing.T) {
	h := NewHub()
	go h.Run()

	c1 := &Client{msg: make(chan []byte, 256), done: make(chan struct{})}
	c2 := &Client{msg: make(chan []byte, 256), done: make(chan struct{})}
	c3 := &Client{msg: make(chan []byte, 256), done: make(chan struct{})}

	h.register <- c1
	h.register <- c2
	h.register <- c3

	time.Sleep(20 * time.Millisecond)

	testMsg := []byte("broadcast to all")
	h.broadcast <- testMsg

	clients := []*Client{c1, c2, c3}
	for i, c := range clients {
		select {
		case msg := <-c.msg:
			if !bytes.Equal(msg, testMsg) {
				t.Fatalf("client %d received %s, expected %s", i, msg, testMsg)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out waiting for broadcast on client %d", i)
		}
	}

	// Clean up
	h.unregister <- c1
	h.unregister <- c2
	h.unregister <- c3
}

func TestHubBufferOverflowHandling(t *testing.T) {
	h := NewHub()
	go h.Run()

	// Client with buffer size 1
	cBlocked := &Client{
		msg:  make(chan []byte, 1),
		done: make(chan struct{}),
	}
	// Fill buffer
	cBlocked.msg <- []byte("fill buffer")

	// Client with normal buffer
	cNormal := &Client{
		msg:  make(chan []byte, 256),
		done: make(chan struct{}),
	}

	h.register <- cBlocked
	h.register <- cNormal

	time.Sleep(20 * time.Millisecond)

	// Broadcast message - cBlocked should overflow and be dropped/closed
	testMsg := []byte("overflow test")
	h.broadcast <- testMsg

	// cNormal should receive the broadcast
	select {
	case msg := <-cNormal.msg:
		if !bytes.Equal(msg, testMsg) {
			t.Fatalf("normal client received %s, expected %s", msg, testMsg)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for broadcast on normal client")
	}

	// cBlocked should be evicted: done and msg closed
	select {
	case <-cBlocked.done:
		// success
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for overflowing client to be evicted (done channel)")
	}

	// Read existing message from buffer
	<-cBlocked.msg
	// Next read should observe channel closure
	select {
	case _, ok := <-cBlocked.msg:
		if ok {
			t.Fatal("expected overflowing client msg channel to be closed")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for overflowing client msg channel close")
	}

	h.unregister <- cNormal
}
