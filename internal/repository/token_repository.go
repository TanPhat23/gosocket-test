package repository

import (
	"context"
	"time"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// TokenRepository defines data access methods for API tokens.
type TokenRepository interface {
	GetByHash(ctx context.Context, tokenHash string) (*models.APIToken, error)
	UpdateLastUsed(ctx context.Context, tokenID string, lastUsed time.Time) error
}

type tokenRepo struct {
	db *gorm.DB
}

// NewTokenRepository creates a new TokenRepository backed by GORM.
func NewTokenRepository(db *gorm.DB) TokenRepository {
	return &tokenRepo{db: db}
}

func (r *tokenRepo) GetByHash(ctx context.Context, tokenHash string) (*models.APIToken, error) {
	var token models.APIToken
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&token).Error; err != nil {
		return nil, err
	}
	return &token, nil
}

func (r *tokenRepo) UpdateLastUsed(ctx context.Context, tokenID string, lastUsed time.Time) error {
	res := r.db.WithContext(ctx).Model(&models.APIToken{}).Where("id = ?", tokenID).Update("last_used_at", lastUsed)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
