package repository

import (
	"context"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// UserRepository defines data access methods for users.
type UserRepository interface {
	GetByID(ctx context.Context, id string) (*models.User, error)
	GetByIssuerSubject(ctx context.Context, issuer, subject string) (*models.User, error)
	Create(ctx context.Context, user *models.User) error
	Update(ctx context.Context, user *models.User, updates map[string]any) error
}

type userRepo struct {
	db *gorm.DB
}

// NewUserRepository creates a new UserRepository backed by GORM.
func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepo{db: db}
}

func (r *userRepo) GetByID(ctx context.Context, id string) (*models.User, error) {
	var user models.User
	if err := r.db.WithContext(ctx).First(&user, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepo) GetByIssuerSubject(ctx context.Context, issuer, subject string) (*models.User, error) {
	var user models.User
	if err := r.db.WithContext(ctx).Where("issuer = ? AND subject = ?", issuer, subject).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepo) Create(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *userRepo) Update(ctx context.Context, user *models.User, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	res := r.db.WithContext(ctx).Model(user).Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
