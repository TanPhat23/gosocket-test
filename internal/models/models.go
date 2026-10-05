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
	Role      TeamRole  `gorm:"type:smallint;not null;check:role IN (10, 20, 30, 40)" json:"role" validate:"required,team_role"`
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
