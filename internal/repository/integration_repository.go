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

// NewIntegrationRepository creates a new IntegrationRepository backed by GORM.
func NewIntegrationRepository(db *gorm.DB) IntegrationRepository {
	return &integrationRepo{db: db}
}

func (r *integrationRepo) GetActiveByProviderAndExternalID(ctx context.Context, provider models.IntegrationProvider, externalID string) (*models.ProjectIntegration, error) {
	var integration models.ProjectIntegration
	if err := r.db.WithContext(ctx).Preload("Credential").
		Where("provider = ? AND external_id = ? AND enabled = ?", provider, externalID, true).
		First(&integration).Error; err != nil {
		return nil, err
	}
	return &integration, nil
}
