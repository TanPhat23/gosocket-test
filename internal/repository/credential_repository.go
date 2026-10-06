package repository

import (
	"context"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// CredentialRepository defines data access methods for credentials.
type CredentialRepository interface {
	ListByTeamID(ctx context.Context, teamID string) ([]models.Credential, error)
	GetByIDAndTeam(ctx context.Context, id, teamID string) (*models.Credential, error)
	Create(ctx context.Context, cred *models.Credential) error
	Delete(ctx context.Context, id, teamID string) error
}

type credRepo struct {
	db *gorm.DB
}

// NewCredentialRepository creates a new CredentialRepository backed by GORM.
func NewCredentialRepository(db *gorm.DB) CredentialRepository {
	return &credRepo{db: db}
}

func (r *credRepo) ListByTeamID(ctx context.Context, teamID string) ([]models.Credential, error) {
	var credentials []models.Credential
	if err := r.db.WithContext(ctx).Where("team_id = ?", teamID).Find(&credentials).Error; err != nil {
		return nil, err
	}
	return credentials, nil
}

func (r *credRepo) GetByIDAndTeam(ctx context.Context, id, teamID string) (*models.Credential, error) {
	var cred models.Credential
	if err := r.db.WithContext(ctx).Where("id = ? AND team_id = ?", id, teamID).First(&cred).Error; err != nil {
		return nil, err
	}
	return &cred, nil
}

func (r *credRepo) Create(ctx context.Context, cred *models.Credential) error {
	return r.db.WithContext(ctx).Create(cred).Error
}

func (r *credRepo) Delete(ctx context.Context, id, teamID string) error {
	res := r.db.WithContext(ctx).Where("id = ? AND team_id = ?", id, teamID).Delete(&models.Credential{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
