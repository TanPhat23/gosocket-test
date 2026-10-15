package repository

import (
	"context"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// TaskRepository defines data access methods for tasks.
type TaskRepository interface {
	GetByID(ctx context.Context, id string) (*models.Task, error)
	GetByProjectAndExternalID(ctx context.Context, projectID, externalID string) (*models.Task, error)
	List(ctx context.Context, projectID string) ([]models.Task, error)
	ListByParentID(ctx context.Context, parentTaskID string) ([]models.Task, error)
	Create(ctx context.Context, task *models.Task) error
	Update(ctx context.Context, task *models.Task) error
	UpdateStatus(ctx context.Context, id string, status models.TaskStatus) error
}

type taskRepo struct {
	db *gorm.DB
}

// NewTaskRepository creates a new TaskRepository backed by GORM generic API.
func NewTaskRepository(db *gorm.DB) TaskRepository {
	return &taskRepo{db: db}
}

func (r *taskRepo) GetByID(ctx context.Context, id string) (*models.Task, error) {
	return findFirst[models.Task](ctx, r.db, "id = ?", id)
}

func (r *taskRepo) GetByProjectAndExternalID(ctx context.Context, projectID, externalID string) (*models.Task, error) {
	return findFirst[models.Task](ctx, r.db, "project_id = ? AND external_id = ?", projectID, externalID)
}

func (r *taskRepo) List(ctx context.Context, projectID string) ([]models.Task, error) {
	if projectID != "" {
		return gorm.G[models.Task](r.db).Where("project_id = ?", projectID).Order("created_at DESC").Find(ctx)
	}
	return gorm.G[models.Task](r.db).Order("created_at DESC").Find(ctx)
}

func (r *taskRepo) ListByParentID(ctx context.Context, parentTaskID string) ([]models.Task, error) {
	return gorm.G[models.Task](r.db).Where("parent_task_id = ?", parentTaskID).Order("created_at ASC").Find(ctx)
}

func (r *taskRepo) Create(ctx context.Context, task *models.Task) error {
	return gorm.G[models.Task](r.db).Create(ctx, task)
}

func (r *taskRepo) Update(ctx context.Context, task *models.Task) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *taskRepo) UpdateStatus(ctx context.Context, id string, status models.TaskStatus) error {
	rows, err := gorm.G[models.Task](r.db).Where("id = ?", id).Update(ctx, "status", status)
	if err != nil {
		return err
	}
	if rows == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
