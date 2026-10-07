package dto

import (
	"time"

	"agent-task-manager/internal/models"
)

// CreateTaskRequest defines the input payload for creating a task.
type CreateTaskRequest struct {
	ProjectID    string              `json:"project_id" validate:"required,uuid"`
	Title        string              `json:"title" validate:"required,min=1,max=255"`
	Prompt       string              `json:"prompt" validate:"required"`
	AssigneeID   string              `json:"assignee_id" validate:"required"`
	AssigneeType models.AssigneeType `json:"assignee_type" validate:"required,assignee_type"`
	Priority     int                 `json:"priority" validate:"task_priority"`
	ParentTaskID *string             `json:"parent_task_id,omitempty" validate:"omitempty,uuid"`
	Model        *string             `json:"model,omitempty"`
	Skills       []string            `json:"skills,omitempty"`
	MCPServers   []string            `json:"mcp_servers,omitempty"`
}

// TaskResponse represents the API response serialization for a Task.
type TaskResponse struct {
	ID            string              `json:"id"`
	ProjectID     string              `json:"project_id"`
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
		HeartbeatAt:  t.HeartbeatAt,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
}
