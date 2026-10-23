package repository

import (
	"context"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
)

// TeamRepository defines data access methods for teams and team memberships.
type TeamRepository interface {
	GetByID(ctx context.Context, id string) (*models.Team, error)
	GetMembership(ctx context.Context, teamID, userID string) (*models.TeamMembership, error)
	// ListMembershipsByUserID returns every membership of userID in one query.
	ListMembershipsByUserID(ctx context.Context, userID string) ([]models.TeamMembership, error)
	AddMembership(ctx context.Context, membership *models.TeamMembership) error
	List(ctx context.Context) ([]models.Team, error)
}

type teamRepo struct {
	db *gorm.DB
}

// NewTeamRepository creates a new TeamRepository backed by GORM generic API.
func NewTeamRepository(db *gorm.DB) TeamRepository {
	return &teamRepo{db: db}
}

func (r *teamRepo) GetByID(ctx context.Context, id string) (*models.Team, error) {
	return findFirst[models.Team](ctx, r.db, "id = ?", id)
}

func (r *teamRepo) GetMembership(ctx context.Context, teamID, userID string) (*models.TeamMembership, error) {
	return findFirst[models.TeamMembership](ctx, r.db, "team_id = ? AND user_id = ?", teamID, userID)
}

func (r *teamRepo) ListMembershipsByUserID(ctx context.Context, userID string) ([]models.TeamMembership, error) {
	return gorm.G[models.TeamMembership](r.db).Where("user_id = ?", userID).Find(ctx)
}

func (r *teamRepo) AddMembership(ctx context.Context, membership *models.TeamMembership) error {
	return r.db.WithContext(ctx).Create(membership).Error
}

func (r *teamRepo) List(ctx context.Context) ([]models.Team, error) {
	return gorm.G[models.Team](r.db).Order("created_at DESC").Find(ctx)
}
