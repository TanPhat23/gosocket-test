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

// NewTokenRepository creates a new TokenRepository backed by GORM generic API.
func NewTokenRepository(db *gorm.DB) TokenRepository {
	return &tokenRepo{db: db}
}

func (r *tokenRepo) GetByHash(ctx context.Context, tokenHash string) (*models.APIToken, error) {
	return findFirst[models.APIToken](ctx, r.db, "token_hash = ?", tokenHash)
}

func (r *tokenRepo) UpdateLastUsed(ctx context.Context, tokenID string, lastUsed time.Time) error {
	rows, err := gorm.G[models.APIToken](r.db).Where("id = ?", tokenID).Update(ctx, "last_used_at", lastUsed)
	if err != nil {
		return err
	}
	if rows == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
