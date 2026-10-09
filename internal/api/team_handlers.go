package api

import (
	"agent-task-manager/internal/common"
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
		return CentralErrorHandler(c, err)
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
		return CentralErrorHandler(c, err)
	}
	if projects == nil {
		projects = []models.Project{}
	}

	return c.JSON(projects)
}

// CreateProject handles POST /api/v1/projects
func (h *TeamHandlers) CreateProject(c fiber.Ctx) error {
	var req struct {
		Name     string `json:"name"`
		TeamID   string `json:"team_id"`
		RepoPath string `json:"repo_path"`
	}
	if err := c.Bind().JSON(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "project name is required")
	}
	if req.TeamID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "team_id is required")
	}
	repoPath := req.RepoPath
	if repoPath == "" {
		repoPath = req.Name
	}

	p := &models.Project{
		ID:       common.NewID(),
		Name:     req.Name,
		TeamID:   req.TeamID,
		RepoPath: repoPath,
	}
	if err := h.projectRepo.Create(c.Context(), p); err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.Status(fiber.StatusCreated).JSON(p)
}

// DeleteProject handles DELETE /api/v1/projects/:id
func (h *TeamHandlers) DeleteProject(c fiber.Ctx) error {
	projectID := c.Params("id")
	if projectID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "project id is required")
	}

	if err := h.projectRepo.Delete(c.Context(), projectID); err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}
