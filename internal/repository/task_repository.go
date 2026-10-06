package repository

import (
	"context"
	"time"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// TaskRepository defines data access methods for tasks.
type TaskRepository interface {
	GetByID(ctx context.Context, id string) (*models.Task, error)
	GetByProjectAndExternalID(ctx context.Context, projectID, externalID string) (*models.Task, error)
	Create(ctx context.Context, task *models.Task) error
	Update(ctx context.Context, task *models.Task) error
	UpdateStatus(ctx context.Context, id string, status models.TaskStatus) error
}

type taskRepo struct {
	db *gorm.DB
}

// NewTaskRepository creates a new TaskRepository backed by GORM.
func NewTaskRepository(db *gorm.DB) TaskRepository {
	return &taskRepo{db: db}
}

func (r *taskRepo) GetByID(ctx context.Context, id string) (*models.Task, error) {
	var task models.Task
	if err := r.db.WithContext(ctx).First(&task, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *taskRepo) GetByProjectAndExternalID(ctx context.Context, projectID, externalID string) (*models.Task, error) {
	var task models.Task
	if err := r.db.WithContext(ctx).Where("project_id = ? AND external_id = ?", projectID, externalID).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *taskRepo) Create(ctx context.Context, task *models.Task) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *taskRepo) Update(ctx context.Context, task *models.Task) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *taskRepo) UpdateStatus(ctx context.Context, id string, status models.TaskStatus) error {
	res := r.db.WithContext(ctx).Model(&models.Task{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":     status,
			"updated_at": time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
