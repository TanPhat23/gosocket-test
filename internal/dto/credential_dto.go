package dto

import (
	"time"

	"agent-task-manager/internal/models"
)

// CreateCredentialRequest defines the input payload for creating a credential.
type CreateCredentialRequest struct {
	Provider models.CredentialProvider `json:"provider" validate:"required,oneof=github jira trello oidc generic"`
	Label    string                    `json:"label" validate:"required,min=1,max=100"`
	Secret   string                    `json:"secret" validate:"required,min=1"`
}

// UpdateCredentialRequest defines the input payload for updating an existing credential.
type UpdateCredentialRequest struct {
	Label  *string `json:"label" validate:"omitempty,min=1,max=100"`
	Secret *string `json:"secret" validate:"omitempty,min=1"`
}

// CredentialResponseDTO is the safe response DTO for credentials.
// Invariant: Never contains raw secrets or EncryptedData ciphertext.
type CredentialResponseDTO struct {
	ID        string                    `json:"id"`
	TeamID    string                    `json:"team_id"`
	Provider  models.CredentialProvider `json:"provider"`
	Label     string                    `json:"label"`
	CreatedAt time.Time                 `json:"created_at"`
	UpdatedAt time.Time                 `json:"updated_at"`
}

// GitHubRepositoryDTO represents a GitHub repository returned when listing repos with a PAT.
type GitHubRepositoryDTO struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Description   string `json:"description"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
}
