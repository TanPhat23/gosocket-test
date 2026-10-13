package repository

import (
	"context"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// ProjectRepository defines data access methods for projects.
type ProjectRepository interface {
	List(ctx context.Context, teamID string) ([]models.Project, error)
	GetByID(ctx context.Context, id string) (*models.Project, error)
	Create(ctx context.Context, p *models.Project) error
	Update(ctx context.Context, p *models.Project) error
	Delete(ctx context.Context, id string) error
}

type projectRepo struct {
	db *gorm.DB
}

// NewProjectRepository creates a new ProjectRepository backed by GORM generic API.
func NewProjectRepository(db *gorm.DB) ProjectRepository {
	return &projectRepo{db: db}
}

func (r *projectRepo) List(ctx context.Context, teamID string) ([]models.Project, error) {
	if teamID != "" {
		return gorm.G[models.Project](r.db).Where("team_id = ?", teamID).Order("created_at DESC").Find(ctx)
	}
	return gorm.G[models.Project](r.db).Order("created_at DESC").Find(ctx)
}

func (r *projectRepo) GetByID(ctx context.Context, id string) (*models.Project, error) {
	return findFirst[models.Project](ctx, r.db, "id = ?", id)
}

func (r *projectRepo) Create(ctx context.Context, p *models.Project) error {
	return gorm.G[models.Project](r.db).Create(ctx, p)
}

func (r *projectRepo) Update(ctx context.Context, p *models.Project) error {
	return r.db.WithContext(ctx).Save(p).Error
}

func (r *projectRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Project{}).Error
}
