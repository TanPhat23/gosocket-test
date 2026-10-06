package repository

import (
	"context"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// LogRepository defines data access methods for task logs.
type LogRepository interface {
	Append(ctx context.Context, log *models.TaskLog) error
	ListByTaskID(ctx context.Context, taskID string) ([]models.TaskLog, error)
}

type logRepo struct {
	db *gorm.DB
}

// NewLogRepository creates a new LogRepository backed by GORM.
func NewLogRepository(db *gorm.DB) LogRepository {
	return &logRepo{db: db}
}

func (r *logRepo) Append(ctx context.Context, log *models.TaskLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *logRepo) ListByTaskID(ctx context.Context, taskID string) ([]models.TaskLog, error) {
	var logs []models.TaskLog
	err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("timestamp ASC, id ASC").
		Find(&logs).Error
	if err != nil {
		return nil, err
	}
	return logs, nil
}
