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
	project, err := gorm.G[models.Project](r.db).Where("id = ?", id).First(ctx)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (r *projectRepo) Create(ctx context.Context, p *models.Project) error {
	return gorm.G[models.Project](r.db).Create(ctx, p)
}
