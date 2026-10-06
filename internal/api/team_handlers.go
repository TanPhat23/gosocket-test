package api

import (
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"

	"github.com/gofiber/fiber/v3"
)

// TeamHandlers handles teams and projects endpoints.
type TeamHandlers struct {
	teamRepo    repository.TeamRepository
	projectRepo repository.ProjectRepository
}

// NewTeamHandlers creates a new TeamHandlers instance.
func NewTeamHandlers(teamRepo repository.TeamRepository, projectRepo repository.ProjectRepository) *TeamHandlers {
	return &TeamHandlers{
		teamRepo:    teamRepo,
		projectRepo: projectRepo,
	}
}

// ListTeams handles GET /api/v1/teams
func (h *TeamHandlers) ListTeams(c fiber.Ctx) error {
	teams, err := h.teamRepo.List(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to list teams"})
	}

	if teams == nil {
		teams = []models.Team{}
	}

	return c.JSON(teams)
}

// ListProjects handles GET /api/v1/projects?team_id=...
func (h *TeamHandlers) ListProjects(c fiber.Ctx) error {
	teamID := c.Query("team_id")
	projects, err := h.projectRepo.List(c.Context(), teamID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to list projects"})
	}
	if projects == nil {
		projects = []models.Project{}
	}

	return c.JSON(projects)
}
