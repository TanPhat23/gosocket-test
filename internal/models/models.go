package models

import (
	"time"
)

type Team struct {
	CreatedAt   time.Time        `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time        `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	Memberships []TeamMembership `gorm:"foreignKey:TeamID;constraint:OnDelete:CASCADE" json:"memberships,omitempty"`
	Projects    []Project        `gorm:"foreignKey:TeamID;constraint:OnDelete:CASCADE" json:"projects,omitempty"`
	Credentials []Credential     `gorm:"foreignKey:TeamID;constraint:OnDelete:CASCADE" json:"credentials,omitempty"`
	ID          string           `gorm:"type:text;primaryKey" json:"id" validate:"required"`
	Name        string           `gorm:"type:text;not null" json:"name" validate:"required,min=1,max=100"`
	Slug        string           `gorm:"type:text;not null;uniqueIndex:idx_teams_slug" json:"slug" validate:"required,min=1,max=50,slug"`
}

type User struct {
	CreatedAt   time.Time        `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time        `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	Memberships []TeamMembership `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"memberships,omitempty"`
	ID          string           `gorm:"type:text;primaryKey" json:"id" validate:"required"`
	Issuer      string           `gorm:"type:text;not null;uniqueIndex:idx_users_issuer_subject,priority:1" json:"issuer" validate:"required,url"`
	Subject     string           `gorm:"type:text;not null;uniqueIndex:idx_users_issuer_subject,priority:2" json:"subject" validate:"required"`
	Email       string           `gorm:"type:text" json:"email,omitempty" validate:"omitempty,email"`
	DisplayName string           `gorm:"type:text" json:"display_name,omitempty" validate:"omitempty,max=100"`
}

type TeamMembership struct {
	CreatedAt time.Time `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	Team      Team      `gorm:"foreignKey:TeamID" json:"team,omitempty"`
	User      User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
	TeamID    string    `gorm:"type:text;primaryKey" json:"team_id" validate:"required"`
	UserID    string    `gorm:"type:text;primaryKey;index:idx_team_memberships_user" json:"user_id" validate:"required"`
	Role      TeamRole  `gorm:"type:text;not null;check:role IN ('owner', 'admin', 'member', 'viewer')" json:"role" validate:"required,team_role"`
}

type APIToken struct {
	CreatedAt  time.Time  `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	ExpiresAt  *time.Time `gorm:"type:timestamptz" json:"expires_at,omitempty"`
	LastUsedAt *time.Time `gorm:"type:timestamptz" json:"last_used_at,omitempty"`
	User       User       `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Team       Team       `gorm:"foreignKey:TeamID" json:"team,omitempty"`
	ID         string     `gorm:"type:text;primaryKey" json:"id" validate:"required"`
	UserID     string     `gorm:"type:text;not null;index:idx_tokens_user" json:"user_id" validate:"required"`
	TeamID     string     `gorm:"type:text;not null;index:idx_tokens_team" json:"team_id" validate:"required"`
	Name       string     `gorm:"type:text;not null" json:"name" validate:"required,min=1,max=100"`
	TokenHash  string     `gorm:"type:text;not null;uniqueIndex:idx_tokens_hash" json:"-"`
}

type Project struct {
	CreatedAt    time.Time            `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt    time.Time            `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	Team         Team                 `gorm:"foreignKey:TeamID" json:"team,omitempty"`
	Integrations []ProjectIntegration `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE" json:"integrations,omitempty"`
	Tasks        []Task               `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE" json:"tasks,omitempty"`
	ID           string               `gorm:"type:text;primaryKey" json:"id" validate:"required"`
	TeamID       string               `gorm:"type:text;not null;index:idx_projects_team;uniqueIndex:idx_projects_team_name,priority:1" json:"team_id" validate:"required"`
	Name         string               `gorm:"type:text;not null;uniqueIndex:idx_projects_team_name,priority:2" json:"name" validate:"required,min=1,max=100"`
	RepoPath     string               `gorm:"type:text;not null" json:"repo_path" validate:"required"`
}

type ProjectIntegration struct {
	CreatedAt    time.Time           `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt    time.Time           `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	LastSyncedAt *time.Time          `gorm:"type:timestamptz" json:"last_synced_at,omitempty"`
	ExternalURL  *string             `gorm:"type:text" json:"external_url,omitempty"`
	Project      Project             `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
	Credential   Credential          `gorm:"foreignKey:CredentialID" json:"credential,omitempty"`
	Config       JSONMap             `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	ID           string              `gorm:"type:text;primaryKey" json:"id" validate:"required"`
	ProjectID    string              `gorm:"type:text;not null;index:idx_project_integrations_project;uniqueIndex:idx_proj_integ_ext,priority:1" json:"project_id" validate:"required"`
	CredentialID string              `gorm:"type:text;not null" json:"credential_id" validate:"required"`
	Provider     IntegrationProvider `gorm:"type:text;not null;uniqueIndex:idx_proj_integ_ext,priority:2;check:provider IN ('github', 'jira', 'trello')" json:"provider" validate:"required,oneof=github jira trello"`
	ExternalID   string              `gorm:"type:text;not null;uniqueIndex:idx_proj_integ_ext,priority:3" json:"external_id" validate:"required"`
	Enabled      bool                `gorm:"not null;default:true" json:"enabled"`
}

type Credential struct {
	CreatedAt      time.Time          `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt      time.Time          `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	Team           Team               `gorm:"foreignKey:TeamID" json:"team,omitempty"`
	ID             string             `gorm:"type:text;primaryKey" json:"id" validate:"required"`
	TeamID         string             `gorm:"type:text;not null;index:idx_credentials_team;uniqueIndex:idx_credentials_team_label,priority:1" json:"team_id" validate:"required"`
	Provider       CredentialProvider `gorm:"type:text;not null;check:provider IN ('github', 'jira', 'trello', 'oidc', 'generic')" json:"provider" validate:"required,oneof=github jira trello oidc generic"`
	Label          string             `gorm:"type:text;not null;uniqueIndex:idx_credentials_team_label,priority:2" json:"label" validate:"required,min=1,max=100"`
	EncryptedData  string             `gorm:"type:text;not null" json:"-"` // AES-256-GCM base64(nonce || ciphertext || tag)
	KeyFingerprint string             `gorm:"type:text;not null" json:"-"` // Master key fingerprint for key rotation
}

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
	Project       Project             `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
	Integration   *ProjectIntegration `gorm:"foreignKey:IntegrationID" json:"integration,omitempty"`
	ID            string              `gorm:"type:text;primaryKey" json:"id" validate:"required"`
	ProjectID     string              `gorm:"type:text;not null;index:idx_tasks_project" json:"project_id" validate:"required"`
	Title         string              `gorm:"type:text;not null" json:"title" validate:"required,min=1,max=255"`
	Prompt        string              `gorm:"type:text;not null" json:"prompt" validate:"required"`
	AssigneeID    string              `gorm:"type:text;not null" json:"assignee_id" validate:"required"`
	Status        TaskStatus          `gorm:"type:text;not null;index:idx_tasks_status;check:status IN ('backlog', 'queued', 'in_progress', 'review', 'done', 'failed', 'cancelled')" json:"status" validate:"required,task_status"`
	AssigneeType  AssigneeType        `gorm:"type:text;not null;default:'agent';check:assignee_type IN ('agent', 'human')" json:"assignee_type" validate:"required,assignee_type"`
	Priority      TaskPriority        `gorm:"type:integer;not null;default:50;index:idx_tasks_priority" json:"priority" validate:"task_priority"`
}

type TaskAttempt struct {
	CreatedAt     time.Time     `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	StartedAt     *time.Time    `gorm:"type:timestamptz" json:"started_at,omitempty"`
	FinishedAt    *time.Time    `gorm:"type:timestamptz" json:"finished_at,omitempty"`
	ContainerID   *string       `gorm:"type:text" json:"container_id,omitempty"`
	WorktreePath  *string       `gorm:"type:text" json:"worktree_path,omitempty"`
	BranchName    *string       `gorm:"type:text" json:"branch_name,omitempty"`
	ExitCode      *int          `gorm:"type:integer" json:"exit_code,omitempty"`
	FailureReason *string       `gorm:"type:text" json:"failure_reason,omitempty"`
	Task          Task          `gorm:"foreignKey:TaskID" json:"task,omitempty"`
	ID            string        `gorm:"type:text;primaryKey" json:"id" validate:"required"`
	TaskID        string        `gorm:"type:text;not null;uniqueIndex:idx_attempts_task_attempt,priority:1" json:"task_id" validate:"required"`
	AttemptNumber int           `gorm:"type:integer;not null;uniqueIndex:idx_attempts_task_attempt,priority:2" json:"attempt_number" validate:"required,min=1"`
	Status        AttemptStatus `gorm:"type:text;not null;check:status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')" json:"status" validate:"required"`
}

type WebhookEvent struct {
	ReceivedAt      time.Time     `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP" json:"received_at"`
	ProcessedAt     *time.Time    `gorm:"type:timestamptz" json:"processed_at,omitempty"`
	Project         Project       `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE" json:"project,omitempty"`
	ErrorMessage    *string       `gorm:"type:text" json:"error_message,omitempty"`
	Provider        string        `gorm:"type:text;primaryKey" json:"provider" validate:"required,oneof=github jira trello"`
	ProjectID       string        `gorm:"type:text;primaryKey;index:idx_webhook_project" json:"project_id" validate:"required"`
	ProviderEventID string        `gorm:"type:text;primaryKey" json:"provider_event_id" validate:"required"`
	Status          WebhookStatus `gorm:"type:text;not null;check:status IN ('received', 'processed', 'failed')" json:"status" validate:"required"`
}

type TaskLog struct {
	Timestamp time.Time    `gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP;index:idx_task_logs_task_attempt,priority:3" json:"timestamp"`
	Task      Task         `gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE" json:"task,omitempty"`
	Attempt   *TaskAttempt `gorm:"foreignKey:AttemptID;constraint:OnDelete:CASCADE" json:"attempt,omitempty"`
	ID        int64        `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID    string       `gorm:"type:text;not null;index:idx_task_logs_task_attempt,priority:1" json:"task_id" validate:"required"`
	AttemptID *string      `gorm:"type:text;index:idx_task_logs_task_attempt,priority:2" json:"attempt_id,omitempty"`
	LogChunk  string       `gorm:"type:text;not null" json:"log_chunk" validate:"required"`
}
