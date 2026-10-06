package repository

import (
	"context"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// AttemptRepository defines data access methods for task attempts.
type AttemptRepository interface {
	CountByTaskID(ctx context.Context, taskID string) (int64, error)
	Create(ctx context.Context, attempt *models.TaskAttempt) error
	Update(ctx context.Context, attempt *models.TaskAttempt) error
}

type attemptRepo struct {
	db *gorm.DB
}

// NewAttemptRepository creates a new AttemptRepository backed by GORM generic API.
func NewAttemptRepository(db *gorm.DB) AttemptRepository {
	return &attemptRepo{db: db}
}

func (r *attemptRepo) CountByTaskID(ctx context.Context, taskID string) (int64, error) {
	return gorm.G[models.TaskAttempt](r.db).Where("task_id = ?", taskID).Count(ctx, "*")
}

func (r *attemptRepo) Create(ctx context.Context, attempt *models.TaskAttempt) error {
	return gorm.G[models.TaskAttempt](r.db).Create(ctx, attempt)
}

func (r *attemptRepo) Update(ctx context.Context, attempt *models.TaskAttempt) error {
	return r.db.WithContext(ctx).Save(attempt).Error
}
