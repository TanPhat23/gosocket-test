package repository

import (
	"context"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// IntegrationRepository defines data access methods for project integrations.
type IntegrationRepository interface {
	GetActiveByProviderAndExternalID(ctx context.Context, provider models.IntegrationProvider, externalID string) (*models.ProjectIntegration, error)
}

type integrationRepo struct {
	db *gorm.DB
}

// NewIntegrationRepository creates a new IntegrationRepository backed by GORM generic API.
func NewIntegrationRepository(db *gorm.DB) IntegrationRepository {
	return &integrationRepo{db: db}
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
