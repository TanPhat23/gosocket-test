package repository

import (
	"context"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// CredentialRepository defines data access methods for credentials.
type CredentialRepository interface {
	GetByID(ctx context.Context, id string) (*models.Credential, error)
	ListByTeamID(ctx context.Context, teamID string) ([]models.Credential, error)
	GetByIDAndTeam(ctx context.Context, id, teamID string) (*models.Credential, error)
	Create(ctx context.Context, cred *models.Credential) error
	Update(ctx context.Context, cred *models.Credential) error
	Delete(ctx context.Context, id, teamID string) error
}

type credRepo struct {
	db *gorm.DB
}

// NewCredentialRepository creates a new CredentialRepository backed by GORM generic API.
func NewCredentialRepository(db *gorm.DB) CredentialRepository {
	return &credRepo{db: db}
}

func (r *credRepo) GetByID(ctx context.Context, id string) (*models.Credential, error) {
	return findFirst[models.Credential](ctx, r.db, "id = ?", id)
}

func (r *credRepo) ListByTeamID(ctx context.Context, teamID string) ([]models.Credential, error) {
	return gorm.G[models.Credential](r.db).Where("team_id = ?", teamID).Find(ctx)
}

func (r *credRepo) GetByIDAndTeam(ctx context.Context, id, teamID string) (*models.Credential, error) {
	return findFirst[models.Credential](ctx, r.db, "id = ? AND team_id = ?", id, teamID)
}

func (r *credRepo) Create(ctx context.Context, cred *models.Credential) error {
	return gorm.G[models.Credential](r.db).Create(ctx, cred)
}

func (r *credRepo) Update(ctx context.Context, cred *models.Credential) error {
	return r.db.WithContext(ctx).Save(cred).Error
}

func (r *credRepo) Delete(ctx context.Context, id, teamID string) error {
	rows, err := gorm.G[models.Credential](r.db).Where("id = ? AND team_id = ?", id, teamID).Delete(ctx)
	if err != nil {
		return err
	}
	if rows == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
