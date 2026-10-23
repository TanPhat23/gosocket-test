package repository

import (
	"context"
	"strings"

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

// NewLogRepository creates a new LogRepository backed by GORM generic API.
func NewLogRepository(db *gorm.DB) LogRepository {
	return &logRepo{db: db}
}

func (r *logRepo) Append(ctx context.Context, log *models.TaskLog) error {
	return gorm.G[models.TaskLog](r.db).Create(ctx, log)
}

func (r *logRepo) ListByTaskID(ctx context.Context, taskID string) ([]models.TaskLog, error) {
	if taskID == "" || strings.Contains(taskID, "/") {
		return []models.TaskLog{}, nil
	}
	return gorm.G[models.TaskLog](r.db).
		Where("task_id = ?", taskID).
		Order("timestamp ASC, id ASC").
		Find(ctx)
}
