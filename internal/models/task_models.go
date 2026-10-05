package models

import (
	"time"
)

// Task represents a unit of work executable by an agent or human.
type Task struct {
	CreatedAt     time.Time           `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt     time.Time           `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	HeartbeatAt   *time.Time          `gorm:"type:timestamptz" json:"heartbeat_at,omitempty"`
	IntegrationID *string             `gorm:"type:text;index:idx_tasks_integration" json:"integration_id,omitempty"`
	AsynqTaskID   *string             `gorm:"type:text;index:idx_tasks_asynq_id" json:"asynq_task_id,omitempty"`
	ExternalID    *string             `gorm:"type:text" json:"external_id,omitempty"`
	ExternalURL   *string             `gorm:"type:text" json:"external_url,omitempty" validate:"omitempty,url"`
	PRURL         *string             `gorm:"type:text" json:"pr_url,omitempty" validate:"omitempty,url"`
	WorktreePath  *string             `gorm:"type:text" json:"worktree_path,omitempty"`
	BranchName    *string             `gorm:"type:text" json:"branch_name,omitempty"`
	Model         *string             `gorm:"type:text" json:"model,omitempty"`
	ContainerID   *string             `gorm:"type:text" json:"container_id,omitempty"`
	ExitCode      *int                `gorm:"type:integer" json:"exit_code,omitempty"`
	Skills        StringArray         `gorm:"type:jsonb;not null;default:'[]';index:idx_tasks_skills,type:gin" json:"skills"`
	MCPServers    StringArray         `gorm:"type:jsonb;not null;default:'[]';index:idx_tasks_mcp,type:gin" json:"mcp_servers"`
	Attempts      []TaskAttempt       `gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE" json:"attempts,omitempty"`
	Logs          []TaskLog           `gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE" json:"logs,omitempty"`
	Project       Project             `gorm:"foreignKey:ProjectID" json:"project,omitempty" validate:"-"`
	Integration   *ProjectIntegration `gorm:"foreignKey:IntegrationID" json:"integration,omitempty" validate:"-"`
	ID            string              `gorm:"type:text;primaryKey" json:"id" validate:"required"`
	ProjectID     string              `gorm:"type:text;not null;index:idx_tasks_project" json:"project_id" validate:"required"`
	Title         string              `gorm:"type:text;not null" json:"title" validate:"required,min=1,max=255"`
	Prompt        string              `gorm:"type:text;not null" json:"prompt" validate:"required"`
	AssigneeID    string              `gorm:"type:text;not null" json:"assignee_id" validate:"required"`
	Status        TaskStatus          `gorm:"type:text;not null;index:idx_tasks_status;check:status IN ('backlog', 'queued', 'in_progress', 'review', 'done', 'failed', 'cancelled')" json:"status" validate:"required,task_status"`
	AssigneeType  AssigneeType        `gorm:"type:text;not null;default:'agent';check:assignee_type IN ('agent', 'human')" json:"assignee_type" validate:"required,assignee_type"`
	Priority      TaskPriority        `gorm:"type:integer;not null;default:50;index:idx_tasks_priority" json:"priority" validate:"task_priority"`
}

// TaskAttempt represents an execution attempt for a task.
type TaskAttempt struct {
	CreatedAt     time.Time     `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	StartedAt     *time.Time    `gorm:"type:timestamptz" json:"started_at,omitempty"`
	FinishedAt    *time.Time    `gorm:"type:timestamptz" json:"finished_at,omitempty"`
	ContainerID   *string       `gorm:"type:text" json:"container_id,omitempty"`
	WorktreePath  *string       `gorm:"type:text" json:"worktree_path,omitempty"`
	BranchName    *string       `gorm:"type:text" json:"branch_name,omitempty"`
	ExitCode      *int          `gorm:"type:integer" json:"exit_code,omitempty"`
	FailureReason *string       `gorm:"type:text" json:"failure_reason,omitempty"`
	Task          Task          `gorm:"foreignKey:TaskID" json:"task,omitempty" validate:"-"`
	ID            string        `gorm:"type:text;primaryKey" json:"id" validate:"required"`
	TaskID        string        `gorm:"type:text;not null;uniqueIndex:idx_attempts_task_attempt,priority:1" json:"task_id" validate:"required"`
	AttemptNumber int           `gorm:"type:integer;not null;uniqueIndex:idx_attempts_task_attempt,priority:2" json:"attempt_number" validate:"required,min=1"`
	Status        AttemptStatus `gorm:"type:text;not null;check:status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')" json:"status" validate:"required"`
}

// WebhookEvent represents an incoming webhook from an external provider.
type WebhookEvent struct {
	ReceivedAt      time.Time     `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"received_at"`
	ProcessedAt     *time.Time    `gorm:"type:timestamptz" json:"processed_at,omitempty"`
	Project         Project       `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE" json:"project,omitempty" validate:"-"`
	ErrorMessage    *string       `gorm:"type:text" json:"error_message,omitempty"`
	Provider        string        `gorm:"type:text;primaryKey" json:"provider" validate:"required,oneof=github jira trello"`
	ProjectID       string        `gorm:"type:text;primaryKey;index:idx_webhook_project" json:"project_id" validate:"required"`
	ProviderEventID string        `gorm:"type:text;primaryKey" json:"provider_event_id" validate:"required"`
	Status          WebhookStatus `gorm:"type:text;not null;check:status IN ('received', 'processed', 'failed')" json:"status" validate:"required"`
}

// TaskLog represents an appended chunk of logs for a task execution.
type TaskLog struct {
	Timestamp time.Time    `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP;index:idx_task_logs_task_attempt,priority:3" json:"timestamp"`
	Task      Task         `gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE" json:"task,omitempty" validate:"-"`
	Attempt   *TaskAttempt `gorm:"foreignKey:AttemptID;constraint:OnDelete:CASCADE" json:"attempt,omitempty" validate:"-"`
	ID        int64        `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID    string       `gorm:"type:text;not null;index:idx_task_logs_task_attempt,priority:1" json:"task_id" validate:"required"`
	AttemptID *string      `gorm:"type:text;index:idx_task_logs_task_attempt,priority:2" json:"attempt_id,omitempty"`
	LogChunk  string       `gorm:"type:text;not null" json:"log_chunk" validate:"required"`
}
