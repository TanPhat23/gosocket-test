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
}

type teamRepo struct {
	db *gorm.DB
}

// NewTeamRepository creates a new TeamRepository backed by GORM generic API.
func NewTeamRepository(db *gorm.DB) TeamRepository {
	return &teamRepo{db: db}
}

func (r *teamRepo) GetByID(ctx context.Context, id string) (*models.Team, error) {
	team, err := gorm.G[models.Team](r.db).Where("id = ?", id).First(ctx)
	if err != nil {
		return nil, err
	}
	return &team, nil
}

func (r *teamRepo) GetMembership(ctx context.Context, teamID, userID string) (*models.TeamMembership, error) {
	membership, err := gorm.G[models.TeamMembership](r.db).Where("team_id = ? AND user_id = ?", teamID, userID).First(ctx)
	if err != nil {
		return nil, err
	}
	return &membership, nil
}
