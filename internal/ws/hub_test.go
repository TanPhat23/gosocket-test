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

	// Broadcast Agent Event
	eventPayload := map[string]any{"eventType": "tool_use", "tool": "shell"}
	err = hub.BroadcastTaskEvent("task-99", eventPayload)
	if err != nil {
		t.Fatalf("BroadcastTaskEvent failed: %v", err)
	}

	select {
	case msg := <-client.Messages():
		var envelope ws.Message[map[string]any]
		if err := json.Unmarshal(msg, &envelope); err != nil {
			t.Fatalf("failed to unmarshal agent_event message envelope: %v", err)
		}
		if envelope.Type != "agent_event" || envelope.TaskID != "task-99" {
			t.Fatalf("unexpected envelope type: %+v", envelope)
		}
		if envelope.Payload["tool"] != "shell" {
			t.Fatalf("unexpected payload tool: %+v", envelope.Payload)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for agent_event message")
	}
}

type discardedClient struct {
	id     string
	taskID string
}

func (d *discardedClient) ID() string         { return d.id }
func (d *discardedClient) TaskID() string     { return d.taskID }
func (d *discardedClient) Send([]byte) error  { return nil }
func (d *discardedClient) Close() error       { return nil }

func BenchmarkHub_BroadcastTaskLog_Unpooled(b *testing.B) {
	client := &discardedClient{id: "c1", taskID: "task-bench"}
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()
	hub.Register(client)
	time.Sleep(10 * time.Millisecond)

	chunk := "[subagent: frontend] → [shell] npm run build\n"
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		msg := ws.NewLogMessage("task-bench", chunk)
		data, err := json.Marshal(msg)
		if err != nil {
			b.Fatal(err)
		}
		hub.BroadcastToTask("task-bench", data)
	}
}

func BenchmarkHub_BroadcastTaskLog_WithSubscribers(b *testing.B) {
	client := &discardedClient{id: "c1", taskID: "task-bench"}
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()
	hub.Register(client)
	time.Sleep(10 * time.Millisecond)

	chunk := "[subagent: frontend] → [shell] npm run build\n"
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := hub.BroadcastTaskLog("task-bench", chunk); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHub_BroadcastTaskLog_NoSubscribers(b *testing.B) {
	hub := ws.NewHub()
	chunk := "[subagent: frontend] → [shell] npm run build\n"
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := hub.BroadcastTaskLog("no-subscribers-task", chunk); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHub_BroadcastTaskLog_Parallel_WithSubscribers(b *testing.B) {
	client := &discardedClient{id: "c1", taskID: "task-bench"}
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()
	hub.Register(client)
	time.Sleep(10 * time.Millisecond)

	chunk := "[subagent: frontend] → [shell] npm run build\n"
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = hub.BroadcastTaskLog("task-bench", chunk)
		}
	})
}
