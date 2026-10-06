package ws_test

import (
	"encoding/json"
	"testing"
	"time"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/ws"
)

func TestHubSubscriptionAndBroadcast(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	client := ws.NewMockClient("client-1", "task-42")
	hub.Register(client)

	// Allow registration processing
	time.Sleep(10 * time.Millisecond)

	hub.BroadcastToTask("task-42", []byte("terminal-output-chunk"))

	select {
	case msg := <-client.Messages():
		if string(msg) != "terminal-output-chunk" {
			t.Errorf("expected terminal-output-chunk, got %s", string(msg))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("client did not receive broadcast message within timeout")
	}

	// Unregister
	hub.Unregister(client)
	time.Sleep(10 * time.Millisecond)

	hub.BroadcastToTask("task-42", []byte("after-unregistration"))
	select {
	case msg := <-client.Messages():
		t.Fatalf("unregistered client received message: %s", string(msg))
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

func TestHubBroadcastTaskLogAndStatus(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	client := ws.NewMockClient("client-2", "task-99")
	hub.Register(client)
	time.Sleep(10 * time.Millisecond)

	// Broadcast Log
	err := hub.BroadcastTaskLog("task-99", "chunk of logs\n")
	if err != nil {
		t.Fatalf("BroadcastTaskLog failed: %v", err)
	}

	select {
	case msg := <-client.Messages():
		var envelope ws.Message[string]
		if err := json.Unmarshal(msg, &envelope); err != nil {
			t.Fatalf("failed to unmarshal log message envelope: %v", err)
		}
		if envelope.Type != "log" || envelope.TaskID != "task-99" || envelope.Payload != "chunk of logs\n" {
			t.Fatalf("unexpected envelope content: %+v", envelope)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for log message")
	}

	// Broadcast Status
	err = hub.BroadcastTaskStatus("task-99", models.TaskStatusInProgress)
	if err != nil {
		t.Fatalf("BroadcastTaskStatus failed: %v", err)
	}

	select {
	case msg := <-client.Messages():
		var envelope ws.Message[models.TaskStatus]
		if err := json.Unmarshal(msg, &envelope); err != nil {
			t.Fatalf("failed to unmarshal status message envelope: %v", err)
		}
		if envelope.Type != "status" || envelope.TaskID != "task-99" || envelope.Payload != models.TaskStatusInProgress {
			t.Fatalf("unexpected envelope content: %+v", envelope)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for status message")
	}
}
