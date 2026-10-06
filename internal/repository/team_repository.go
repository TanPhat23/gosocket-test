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

// NewTeamRepository creates a new TeamRepository backed by GORM.
func NewTeamRepository(db *gorm.DB) TeamRepository {
	return &teamRepo{db: db}
}

func (r *teamRepo) GetByID(ctx context.Context, id string) (*models.Team, error) {
	var team models.Team
	if err := r.db.WithContext(ctx).First(&team, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &team, nil
}

func (r *teamRepo) GetMembership(ctx context.Context, teamID, userID string) (*models.TeamMembership, error) {
	var membership models.TeamMembership
	if err := r.db.WithContext(ctx).Where("team_id = ? AND user_id = ?", teamID, userID).First(&membership).Error; err != nil {
		return nil, err
	}
	return &membership, nil
}
