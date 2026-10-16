package api

import (
	"context"
	"errors"
	"fmt"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// TeamHandlers handles teams and projects endpoints.
// All handlers expect RequireAuth to have populated the current user.
type TeamHandlers struct {
	teamRepo    repository.TeamRepository
	projectRepo repository.ProjectRepository
	authService service.AuthService
}

// NewTeamHandlers creates a new TeamHandlers instance.
func NewTeamHandlers(teamRepo repository.TeamRepository, projectRepo repository.ProjectRepository, authService service.AuthService) *TeamHandlers {
	return &TeamHandlers{
		teamRepo:    teamRepo,
		projectRepo: projectRepo,
		authService: authService,
	}
}

// memberTeams returns the teams on which userID holds at least RoleViewer, limited to ctx's PAT team
// scope.
func (h *TeamHandlers) memberTeams(ctx context.Context, userID string) ([]models.Team, error) {
	if h.teamRepo == nil {
		return nil, common.ErrServiceUnavailable
	}
	teams, err := h.teamRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list teams: %w", err)
	}
	visible := []models.Team{}
	for i := range teams {
		if !auth.TeamAllowed(ctx, teams[i].ID) {
			continue
		}
		membership, mErr := h.teamRepo.GetMembership(ctx, teams[i].ID, userID)
		if mErr != nil {
			if errors.Is(mErr, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, fmt.Errorf("failed to load team membership: %w", mErr)
		}
		if auth.HasRolePermission(membership.Role, models.RoleViewer) {
			visible = append(visible, teams[i])
		}
	}
	return visible, nil
}

// authorizeTeam maps a team role check onto the shared forbidden error (403) for non-members and
// insufficient roles alike.
func (h *TeamHandlers) authorizeTeam(ctx context.Context, teamID, userID string, minRole models.TeamRole) error {
	if h.authService == nil {
		return common.ErrServiceUnavailable
	}
	if _, err := h.authService.AuthorizeTeamRole(ctx, teamID, userID, minRole); err != nil {
		if errors.Is(err, service.ErrNotTeamMember) || errors.Is(err, service.ErrForbidden) {
			return common.ErrForbidden
		}
		return err
	}
	return nil
}

// ListTeams handles GET /api/v1/teams. It returns only teams the caller belongs to.
func (h *TeamHandlers) ListTeams(c fiber.Ctx) error {
	user := CurrentUser(c)
	if user == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	teams, err := h.memberTeams(c.Context(), user.ID)
	if err != nil {
		return CentralErrorHandler(c, err)
	}
	return c.JSON(teams)
}

// ListProjects handles GET /api/v1/projects?team_id=...
// With team_id the caller must be a member (RoleViewer+) of that team; without it, only projects of
// the caller's teams are returned.
func (h *TeamHandlers) ListProjects(c fiber.Ctx) error {
	user := CurrentUser(c)
	if user == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	projects := []models.Project{}
	if teamID := c.Query("team_id"); teamID != "" {
		if err := h.authorizeTeam(c.Context(), teamID, user.ID, models.RoleViewer); err != nil {
			return CentralErrorHandler(c, err)
		}
		list, err := h.projectRepo.List(c.Context(), teamID)
		if err != nil {
			return CentralErrorHandler(c, err)
		}
		projects = append(projects, list...)
		return c.JSON(projects)
	}

	teams, err := h.memberTeams(c.Context(), user.ID)
	if err != nil {
		return CentralErrorHandler(c, err)
	}
	for i := range teams {
		list, err := h.projectRepo.List(c.Context(), teams[i].ID)
		if err != nil {
			return CentralErrorHandler(c, err)
		}
		projects = append(projects, list...)
	}

	return c.JSON(projects)
}

// CreateProject handles POST /api/v1/projects. Requires RoleAdmin or higher on the target team.
func (h *TeamHandlers) CreateProject(c fiber.Ctx) error {
	user := CurrentUser(c)
	if user == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
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
	if err := h.authorizeTeam(c.Context(), req.TeamID, user.ID, models.RoleAdmin); err != nil {
		return CentralErrorHandler(c, err)
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

// DeleteProject handles DELETE /api/v1/projects/:id.
// Authorization (RoleAdmin+ on the project's team) is enforced by RequireProjectRole on the route.
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
