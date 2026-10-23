package ws_test

import (
	"encoding/json"
	"testing"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/ws"
)

func TestMessageTypeIsValid(t *testing.T) {
	valid := []ws.MessageType{
		ws.MessageTypeLog,
		ws.MessageTypeStatus,
		ws.MessageTypeDiff,
		ws.MessageTypeAgentEvent,
	}
	for _, mt := range valid {
		if !mt.IsValid() {
			t.Errorf("expected %q to be valid", mt)
		}
	}

	invalid := []ws.MessageType{"", "LOG", "diff ", "unknown", "agent-event"}
	for _, mt := range invalid {
		if mt.IsValid() {
			t.Errorf("expected %q to be invalid", mt)
		}
	}
}

func TestMessageTypeString(t *testing.T) {
	cases := map[ws.MessageType]string{
		ws.MessageTypeLog:        "log",
		ws.MessageTypeStatus:     "status",
		ws.MessageTypeDiff:       "diff",
		ws.MessageTypeAgentEvent: "agent_event",
		ws.MessageType(""):       "",
	}
	for mt, want := range cases {
		if got := mt.String(); got != want {
			t.Errorf("MessageType(%q).String() = %q, want %q", mt, got, want)
		}
	}
}

func TestNewDiffMessageEnvelope(t *testing.T) {
	msg := ws.NewDiffMessage("task-7", "@@ -1 +1 @@")

	if msg.TaskID != "task-7" {
		t.Errorf("TaskID = %q, want task-7", msg.TaskID)
	}
	if msg.Type != ws.MessageTypeDiff || !msg.Type.IsValid() {
		t.Errorf("Type = %q, want diff", msg.Type)
	}
	if msg.Payload != "@@ -1 +1 @@" {
		t.Errorf("Payload = %q", msg.Payload)
	}
	if msg.Timestamp.IsZero() || msg.Timestamp.Location().String() != "UTC" {
		t.Errorf("Timestamp must be set in UTC, got %v", msg.Timestamp)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal diff message: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal diff message: %v", err)
	}
	if decoded["type"] != "diff" || decoded["task_id"] != "task-7" || decoded["payload"] != "@@ -1 +1 @@" {
		t.Errorf("unexpected JSON envelope: %s", string(data))
	}
	if _, ok := decoded["timestamp"]; !ok {
		t.Errorf("JSON envelope missing timestamp: %s", string(data))
	}
}

func TestNewDiffMessageEmptyDiff(t *testing.T) {
	msg := ws.NewDiffMessage("", "")
	if msg.TaskID != "" || msg.Payload != "" {
		t.Errorf("expected empty fields to be preserved, got %+v", msg)
	}
	if msg.Type != ws.MessageTypeDiff {
		t.Errorf("empty diff must still be typed as diff, got %q", msg.Type)
	}
}

func TestNewAgentEventMessageUsesGenericPayload(t *testing.T) {
	msg := ws.NewAgentEventMessage("task-8", models.TaskStatusDone)
	if msg.Type != ws.MessageTypeAgentEvent {
		t.Errorf("Type = %q, want agent_event", msg.Type)
	}
	if msg.Payload != models.TaskStatusDone {
		t.Errorf("Payload = %v, want %v", msg.Payload, models.TaskStatusDone)
	}
}
