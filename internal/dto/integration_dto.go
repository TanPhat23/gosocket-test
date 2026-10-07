package dto

import (
	"time"

	"agent-task-manager/internal/models"
)

type CreateIntegrationRequest struct {
	CredentialID  string `json:"credential_id" validate:"required,uuid"`
	Provider      string `json:"provider" validate:"required,oneof=github jira trello"`
	Repo          string `json:"repo" validate:"required"`
	BaseBranch    string `json:"base_branch,omitempty"`
	AutoPR        *bool  `json:"auto_pr,omitempty"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
	FilterLabel   string `json:"filter_label,omitempty"`
}

type UpdateIntegrationRequest struct {
	CredentialID  *string `json:"credential_id,omitempty" validate:"omitempty,uuid"`
	Enabled       *bool   `json:"enabled,omitempty"`
	BaseBranch    *string `json:"base_branch,omitempty"`
	AutoPR        *bool   `json:"auto_pr,omitempty"`
	WebhookSecret *string `json:"webhook_secret,omitempty"`
	FilterLabel   *string `json:"filter_label,omitempty"`
}

type IntegrationResponse struct {
	ID           string         `json:"id"`
	ProjectID    string         `json:"project_id"`
	CredentialID string         `json:"credential_id"`
	Provider     string         `json:"provider"`
	ExternalID   string         `json:"external_id"`
	ExternalURL  string         `json:"external_url"`
	Enabled      bool           `json:"enabled"`
	Config       models.JSONMap `json:"config"`
	LastSyncedAt *time.Time     `json:"last_synced_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}
