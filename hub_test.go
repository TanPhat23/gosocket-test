package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestHub_RegisterAndUnregister(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := NewHub()
	go h.Run(ctx)

	client := NewClient(nil, h)
	h.register <- client

	// Give time to register
	time.Sleep(10 * time.Millisecond)

	if count := h.ClientCount(); count != 1 {
		t.Fatalf("expected 1 client registered, got %d", count)
	}

	h.unregister <- client
	time.Sleep(10 * time.Millisecond)

	if count := h.ClientCount(); count != 0 {
		t.Fatalf("expected 0 clients registered, got %d", count)
	}
}

func TestHub_Broadcast(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := NewHub()
	go h.Run(ctx)

	client1 := NewClient(nil, h)
	client2 := NewClient(nil, h)

	h.register <- client1
	h.register <- client2
	time.Sleep(10 * time.Millisecond)

	msg := []byte("broadcast test")
	h.Broadcast(msg)

	select {
	case received := <-client1.send:
		if string(received) != string(msg) {
			t.Fatalf("client1 expected %s, got %s", msg, received)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("client1 did not receive message")
	}

	select {
	case received := <-client2.send:
		if string(received) != string(msg) {
			t.Fatalf("client2 expected %s, got %s", msg, received)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("client2 did not receive message")
	}
}

func TestHub_SlowClientEviction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := NewHub()
	go h.Run(ctx)

	client := NewClient(nil, h)
	// Fill the client's send buffer
	for i := 0; i < cap(client.send); i++ {
		client.send <- []byte(fmt.Sprintf("msg %d", i))
	}

	h.register <- client
	time.Sleep(10 * time.Millisecond)

	// Broadcasting when buffer is full should evict the slow client
	h.Broadcast([]byte("overflow message"))
	time.Sleep(20 * time.Millisecond)

	// Verify client is unregistered and send channel is closed
	select {
	case _, ok := <-client.send:
		// Drain and check if it eventually closed
		for ok {
			_, ok = <-client.send
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected client.send to be closed")
	}
}

func TestHub_ShutdownContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	h := NewHub()
	hubStopped := make(chan struct{})
	go func() {
		h.Run(ctx)
		close(hubStopped)
	}()

	client := NewClient(nil, h)
	h.register <- client
	time.Sleep(10 * time.Millisecond)

	cancel() // Stop the hub

	select {
	case <-hubStopped:
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Fatal("hub did not stop on context cancellation")
	}

	// Verify client resources cleaned up
	select {
	case _, ok := <-client.send:
		if ok {
			t.Fatal("expected client.send to be closed on hub shutdown")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for client.send to close")
	}
}

func TestHub_ConcurrentOperations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := NewHub()
	go h.Run(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c := NewClient(nil, h)
			h.register <- c
			for j := 0; j < 5; j++ {
				h.Broadcast([]byte(fmt.Sprintf("msg-%d-%d", id, j)))
			}
			h.unregister <- c
		}(i)
	}
	wg.Wait()
}
