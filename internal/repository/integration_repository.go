package repository

import (
	"context"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// IntegrationRepository defines data access methods for project integrations.
type IntegrationRepository interface {
	Create(ctx context.Context, integ *models.ProjectIntegration) error
	GetByID(ctx context.Context, id string) (*models.ProjectIntegration, error)
	ListByProjectID(ctx context.Context, projectID string) ([]models.ProjectIntegration, error)
	GetActiveByProviderAndExternalID(ctx context.Context, provider models.IntegrationProvider, externalID string) (*models.ProjectIntegration, error)
	Update(ctx context.Context, integ *models.ProjectIntegration) error
	Delete(ctx context.Context, id string) error
}

type integrationRepo struct {
	db *gorm.DB
}

// NewIntegrationRepository creates a new IntegrationRepository backed by GORM generic API.
func NewIntegrationRepository(db *gorm.DB) IntegrationRepository {
	return &integrationRepo{db: db}
}

func (r *integrationRepo) Create(ctx context.Context, integ *models.ProjectIntegration) error {
	return gorm.G[models.ProjectIntegration](r.db).Create(ctx, integ)
}

func (r *integrationRepo) GetByID(ctx context.Context, id string) (*models.ProjectIntegration, error) {
	integ, err := gorm.G[models.ProjectIntegration](r.db).Where("id = ?", id).First(ctx)
	if err != nil {
		return nil, err
	}
	return &integ, nil
}

func (r *integrationRepo) ListByProjectID(ctx context.Context, projectID string) ([]models.ProjectIntegration, error) {
	return gorm.G[models.ProjectIntegration](r.db).
		Where("project_id = ?", projectID).
		Order("created_at ASC").
		Find(ctx)
}

func (r *integrationRepo) GetActiveByProviderAndExternalID(ctx context.Context, provider models.IntegrationProvider, externalID string) (*models.ProjectIntegration, error) {
	integ, err := gorm.G[models.ProjectIntegration](r.db).
		Where("provider = ? AND external_id = ? AND enabled = ?", provider, externalID, true).
		First(ctx)
	if err != nil {
		return nil, err
	}
	return &integ, nil
}

func (r *integrationRepo) Update(ctx context.Context, integ *models.ProjectIntegration) error {
	return r.db.WithContext(ctx).Save(integ).Error
}

func (r *integrationRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.ProjectIntegration{}, "id = ?", id).Error
}
