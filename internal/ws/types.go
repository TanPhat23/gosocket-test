package ws

import (
	"time"

	"agent-task-manager/internal/models"
)

// MessageType represents the type of real-time WebSocket message.
type MessageType string

const (
	MessageTypeLog        MessageType = "log"
	MessageTypeStatus     MessageType = "status"
	MessageTypeDiff       MessageType = "diff"
	MessageTypeAgentEvent MessageType = "agent_event"
)

func (t MessageType) IsValid() bool {
	switch t {
	case MessageTypeLog, MessageTypeStatus, MessageTypeDiff, MessageTypeAgentEvent:
		return true
	}
	return false
}

func (t MessageType) String() string {
	return string(t)
}

// Message is a generic envelope for WebSocket messages sent across rooms.
type Message[T any] struct {
	TaskID    string      `json:"task_id"`
	Type      MessageType `json:"type"`
	Payload   T           `json:"payload"`
	Timestamp time.Time   `json:"timestamp"`
}

// NewLogMessage creates a new generic log chunk message envelope.
func NewLogMessage(taskID string, chunk string) Message[string] {
	return Message[string]{
		TaskID:    taskID,
		Type:      MessageTypeLog,
		Payload:   chunk,
		Timestamp: time.Now().UTC(),
	}
}

// NewStatusMessage creates a new generic task status message envelope.
func NewStatusMessage(taskID string, status models.TaskStatus) Message[models.TaskStatus] {
	return Message[models.TaskStatus]{
		TaskID:    taskID,
		Type:      MessageTypeStatus,
		Payload:   status,
		Timestamp: time.Now().UTC(),
	}
}

// NewDiffMessage creates a new generic diff message envelope.
func NewDiffMessage(taskID string, diff string) Message[string] {
	return Message[string]{
		TaskID:    taskID,
		Type:      MessageTypeDiff,
		Payload:   diff,
		Timestamp: time.Now().UTC(),
	}
}

// NewAgentEventMessage creates a new structured agent event message envelope.
func NewAgentEventMessage[T any](taskID string, event T) Message[T] {
	return Message[T]{
		TaskID:    taskID,
		Type:      MessageTypeAgentEvent,
		Payload:   event,
		Timestamp: time.Now().UTC(),
	}
}
