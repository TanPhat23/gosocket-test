package dto

import (
	"time"

	"agent-task-manager/internal/models"
)

// CreateTaskRequest defines the input payload for creating a task.
type CreateTaskRequest struct {
	ProjectID     string              `json:"project_id" validate:"required,uuid"`
	Title         string              `json:"title" validate:"required,min=1,max=255"`
	Prompt        string              `json:"prompt" validate:"required"`
	AssigneeID    string              `json:"assignee_id" validate:"required"`
	AssigneeType  models.AssigneeType `json:"assignee_type" validate:"required,assignee_type"`
	Priority      int                 `json:"priority" validate:"task_priority"`
	ParentTaskID  *string             `json:"parent_task_id,omitempty" validate:"omitempty,uuid"`
	IntegrationID *string             `json:"integration_id,omitempty" validate:"omitempty,uuid"`
	Model         *string             `json:"model,omitempty"`
	Skills        []string            `json:"skills,omitempty"`
	MCPServers    []string            `json:"mcp_servers,omitempty"`
}

// TaskResponse represents the API response serialization for a Task.
type TaskResponse struct {
	ID            string              `json:"id"`
	ProjectID     string              `json:"project_id"`
	IntegrationID *string             `json:"integration_id,omitempty"`
	Title         string              `json:"title"`
	Prompt        string              `json:"prompt"`
	AssigneeID    string              `json:"assignee_id"`
	AssigneeType  models.AssigneeType `json:"assignee_type"`
	Status        models.TaskStatus   `json:"status"`
	Priority      models.TaskPriority `json:"priority"`
	ParentTaskID  *string             `json:"parent_task_id,omitempty"`
	Model         *string             `json:"model,omitempty"`
	Skills        []string            `json:"skills"`
	MCPServers    []string            `json:"mcp_servers"`
	AsynqTaskID   *string             `json:"asynq_task_id,omitempty"`
	SessionID     *string             `json:"session_id,omitempty"`
	ExternalID    *string             `json:"external_id,omitempty"`
	ExternalURL   *string             `json:"external_url,omitempty"`
	PRURL          *string             `json:"pr_url,omitempty"`
	PullRequestURL *string             `json:"pull_request_url,omitempty"`
	WorktreePath   *string             `json:"worktree_path,omitempty"`
	BranchName    *string             `json:"branch_name,omitempty"`
	ContainerID   *string             `json:"container_id,omitempty"`
	ExitCode      *int                `json:"exit_code,omitempty"`
	FailureReason *string             `json:"failure_reason,omitempty"`
	HeartbeatAt   *time.Time          `json:"heartbeat_at,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

// ToTaskResponse maps a models.Task to TaskResponse.
func ToTaskResponse(t *models.Task) TaskResponse {
	if t == nil {
		return TaskResponse{}
	}
	skills := []string(t.Skills)
	if skills == nil {
		skills = []string{}
	}
	mcpServers := []string(t.MCPServers)
	if mcpServers == nil {
		mcpServers = []string{}
	}
	prURL := t.PRURL
	if prURL == nil && t.PullRequestURL != nil {
		prURL = t.PullRequestURL
	}
	return TaskResponse{
		ID:             t.ID,
		ProjectID:      t.ProjectID,
		IntegrationID:  t.IntegrationID,
		Title:          t.Title,
		Prompt:         t.Prompt,
		AssigneeID:     t.AssigneeID,
		AssigneeType:   t.AssigneeType,
		Status:         t.Status,
		Priority:       t.Priority,
		ParentTaskID:   t.ParentTaskID,
		Model:          t.Model,
		Skills:         skills,
		MCPServers:     mcpServers,
		AsynqTaskID:    t.AsynqTaskID,
		SessionID:      t.SessionID,
		ExternalID:     t.ExternalID,
		ExternalURL:    t.ExternalURL,
		PRURL:          prURL,
		PullRequestURL: prURL,
		WorktreePath:   t.WorktreePath,
		BranchName:   t.BranchName,
		ContainerID:  t.ContainerID,
		ExitCode:     t.ExitCode,
		FailureReason: t.FailureReason,
		HeartbeatAt:  t.HeartbeatAt,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
}

// TaskSessionPromptRequest represents the payload for prompting an ongoing OpenCode session.
type TaskSessionPromptRequest struct {
	Prompt string            `json:"prompt" validate:"required,min=1,max=32768"`
	Images []SessionImageDTO `json:"images,omitempty"`
}

// SessionImageDTO is an image attached to a steer. Data is standard base64 without a data: prefix.
type SessionImageDTO struct {
	Name string `json:"name,omitempty"`
	MIME string `json:"mime"`
	Data string `json:"data"`
}

// OpenCodeMessageDTO represents an individual message returned from OpenCode session history.
type OpenCodeMessageDTO struct {
	ID          string     `json:"id"`
	Type        string     `json:"type,omitempty"`
	Role        string     `json:"role,omitempty"`
	Agent       string     `json:"agent,omitempty"`
	Text        string     `json:"text,omitempty"`
	Content     any        `json:"content,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// SessionTokensDTO represents token usage metrics for an agent session.
type SessionTokensDTO struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	Reasoning  int64 `json:"reasoning,omitempty"`
	CacheRead  int64 `json:"cache_read,omitempty"`
	CacheWrite int64 `json:"cache_write,omitempty"`
}

// SessionChildDTO is a subagent (child) session spawned by the task's session.
type SessionChildDTO struct {
	ID        string     `json:"id"`
	Title     string     `json:"title,omitempty"`
	Status    string     `json:"status,omitempty"`
	Cost      float64    `json:"cost,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// TaskSessionResponseDTO represents the aggregated session information and history.
type TaskSessionResponseDTO struct {
	SessionID string               `json:"session_id"`
	TaskID    string               `json:"task_id"`
	Status    string               `json:"status,omitempty"`
	Model     string               `json:"model,omitempty"`
	Cost      float64              `json:"cost,omitempty"`
	Tokens    *SessionTokensDTO    `json:"tokens,omitempty"`
	Title     string               `json:"title,omitempty"`
	Directory string               `json:"directory,omitempty"`
	CreatedAt *time.Time           `json:"created_at,omitempty"`
	UpdatedAt *time.Time           `json:"updated_at,omitempty"`
	Simulated bool                 `json:"simulated,omitempty"`
	Messages  []OpenCodeMessageDTO `json:"messages"`
	Children  []SessionChildDTO    `json:"children,omitempty"`
}
