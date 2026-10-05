package queue

import (
	"agent-task-manager/internal/models"
)

// Task payload types and queue tier names.
const (
	TypeExecuteAgentTask = "task:execute"

	QueueCritical = "critical"
	QueueDefault  = "default"
	QueueLow      = "low"
)

// TaskExecutionPayload is the serialized payload passed to Asynq task dispatcher.
type TaskExecutionPayload struct {
	TaskID    string `json:"task_id"`
	ProjectID string `json:"project_id"`
}

// ResolveQueueTier maps TaskPriority to an Asynq queue tier name.
func ResolveQueueTier(priority models.TaskPriority) string {
	switch priority {
	case models.PriorityUrgent, models.PriorityHigh:
		return QueueCritical
	case models.PriorityLow:
		return QueueLow
	case models.PriorityNormal:
		fallthrough
	default:
		return QueueDefault
	}
}
