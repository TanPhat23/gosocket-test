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

func (r *taskRepo) ListByProjectIDs(ctx context.Context, projectIDs []string) ([]models.Task, error) {
	if len(projectIDs) == 0 {
		return []models.Task{}, nil
	}
	return gorm.G[models.Task](r.db).Where("project_id IN ?", projectIDs).Order("created_at DESC").Find(ctx)
}

// FindByProjectAndBranch returns the newest task of projectID whose branch_name equals branchName, or
// gorm.ErrRecordNotFound. It is deliberately not on TaskRepository: callers type-assert it and fall back to List.
func (r *taskRepo) FindByProjectAndBranch(ctx context.Context, projectID, branchName string) (*models.Task, error) {
	return r.findNewest(ctx, "project_id = ? AND branch_name = ?", projectID, branchName)
}

// FindByProjectAndPRURL returns the newest task of projectID whose pr_url equals prURL, or
// gorm.ErrRecordNotFound. Like FindByProjectAndBranch it is an optional method, type-asserted by callers.
func (r *taskRepo) FindByProjectAndPRURL(ctx context.Context, projectID, prURL string) (*models.Task, error) {
	return r.findNewest(ctx, "project_id = ? AND pr_url = ?", projectID, prURL)
}

func (r *taskRepo) findNewest(ctx context.Context, query string, args ...any) (*models.Task, error) {
	rows, err := gorm.G[models.Task](r.db).Where(query, args...).Order("created_at DESC").Limit(1).Find(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &rows[0], nil
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
