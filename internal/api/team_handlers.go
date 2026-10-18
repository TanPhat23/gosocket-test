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
	// One read for all of the caller's memberships, however many teams exist. A user has at most one
	// membership per team (team_id, user_id is the primary key), so a map by team is exact.
	memberships, err := h.teamRepo.ListMembershipsByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load team membership: %w", err)
	}
	roles := make(map[string]models.TeamRole, len(memberships))
	for i := range memberships {
		roles[memberships[i].TeamID] = memberships[i].Role
	}
	visible := []models.Team{}
	for i := range teams {
		if !auth.TeamAllowed(ctx, teams[i].ID) {
			continue
		}
		role, ok := roles[teams[i].ID]
		if !ok {
			continue
		}
		if auth.HasRolePermission(role, models.RoleViewer) {
			visible = append(visible, teams[i])
		}
	}
	return visible, nil
}

// batchProjectLister is the set-based project read ListProjects uses when the repository offers it.
// Other implementations get one read per team instead, so ProjectRepository itself is unchanged.
type batchProjectLister interface {
	ListByTeamIDs(ctx context.Context, teamIDs []string) ([]models.Project, error)
}

// listProjectsForTeams returns the projects of teams: grouped in the order of teams, and newest first within a team.
func (h *TeamHandlers) listProjectsForTeams(ctx context.Context, teams []models.Team) ([]models.Project, error) {
	projects := []models.Project{}
	if len(teams) == 0 {
		return projects, nil
	}
	if b, ok := h.projectRepo.(batchProjectLister); ok {
		teamIDs := make([]string, 0, len(teams))
		for i := range teams {
			teamIDs = append(teamIDs, teams[i].ID)
		}
		list, err := b.ListByTeamIDs(ctx, teamIDs)
		if err != nil {
			return nil, err
		}
		byTeam := make(map[string][]models.Project, len(teams))
		for i := range list {
			byTeam[list[i].TeamID] = append(byTeam[list[i].TeamID], list[i])
		}
		for i := range teams {
			projects = append(projects, byTeam[teams[i].ID]...)
		}
		return projects, nil
	}
	for i := range teams {
		list, err := h.projectRepo.List(ctx, teams[i].ID)
		if err != nil {
			return nil, err
		}
		projects = append(projects, list...)
	}
	return projects, nil
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
	list, err := h.listProjectsForTeams(c.Context(), teams)
	if err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.JSON(list)
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
