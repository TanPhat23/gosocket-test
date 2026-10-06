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
