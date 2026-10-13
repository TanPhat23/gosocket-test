package api

import (
	"errors"
	"strings"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
)

const (
	LocalsUserKey     = "currentUser"
	LocalsAPITokenKey = "currentAPIToken"
	LocalsTeamRoleKey = "currentTeamRole"
	LocalsTeamIDKey   = "currentTeamID"
	BearerPrefix      = "Bearer "
)

// CurrentUser retrieves the authenticated models.User from the Fiber context.
func CurrentUser(c fiber.Ctx) *models.User {
	return fiber.Locals[*models.User](c, LocalsUserKey)
}

// CurrentAPIToken retrieves the used models.APIToken from the Fiber context (if Bearer PAT was used).
func CurrentAPIToken(c fiber.Ctx) *models.APIToken {
	return fiber.Locals[*models.APIToken](c, LocalsAPITokenKey)
}

// CurrentTeamRole retrieves the resolved models.TeamRole from the Fiber context.
func CurrentTeamRole(c fiber.Ctx) models.TeamRole {
	return fiber.Locals[models.TeamRole](c, LocalsTeamRoleKey)
}

// currentUserID returns the authenticated user's ID, or "" when unauthenticated.
func currentUserID(c fiber.Ctx) string {
	if user := CurrentUser(c); user != nil {
		return user.ID
	}
	return ""
}

// authenticate tries the atm_session cookie first, then an Authorization: Bearer
// Personal Access Token. On success it sets the Locals and returns true.
func authenticate(c fiber.Ctx, authService service.AuthService, sm *auth.SessionManager) bool {
	if sm != nil {
		claims, err := sm.ValidateFiberRequest(c)
		if err == nil && claims != nil {
			user, err := authService.GetUserByID(c.Context(), claims.UserID)
			if err == nil && user != nil {
				c.Locals(LocalsUserKey, user)
				return true
			}
		}
	}

	authHeader := c.Get(fiber.HeaderAuthorization)
	if strings.HasPrefix(authHeader, BearerPrefix) {
		rawToken := strings.TrimSpace(strings.TrimPrefix(authHeader, BearerPrefix))
		user, apiToken, err := authService.AuthenticatePAT(c.Context(), rawToken)
		if err == nil && user != nil {
			c.Locals(LocalsUserKey, user)
			c.Locals(LocalsAPITokenKey, apiToken)
			return true
		}
	}
	return false
}

// RequireAuth provides dual authentication via session cookie (atm_session) or Bearer Personal Access Token.
// Returns 401 Unauthorized if neither is present or valid.
func RequireAuth(authService service.AuthService, sm *auth.SessionManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		if authenticate(c, authService, sm) {
			return c.Next()
		}
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized: valid session cookie or Personal Access Token required",
		})
	}
}

// OptionalAuth populates CurrentUser if a valid session cookie or token is present, but does not block requests if unauthenticated.
func OptionalAuth(authService service.AuthService, sm *auth.SessionManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		authenticate(c, authService, sm)
		return c.Next()
	}
}

// RequireTeamRole enforces team-level RBAC based on team_id in params or queries.
// It checks the authenticated user's role against minRole: owner > admin > member > viewer.
// Returns 403 Forbidden if the user is not in the team or lacks sufficient permission.
func RequireTeamRole(authService service.AuthService, minRole models.TeamRole) fiber.Handler {
	return func(c fiber.Ctx) error {
		user := CurrentUser(c)
		if user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Unauthorized",
			})
		}

		teamID := c.Params("team_id")
		if teamID == "" {
			teamID = c.Query("team_id")
		}
		if teamID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Missing team_id parameter",
			})
		}

		actualRole, err := authService.AuthorizeTeamRole(c.Context(), teamID, user.ID, minRole)
		if err != nil {
			if errors.Is(err, service.ErrNotTeamMember) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "Access denied: you are not a member of this team",
				})
			}
			if errors.Is(err, service.ErrForbidden) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "Forbidden: insufficient team role permissions",
				})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Database error checking team permissions",
			})
		}

		c.Locals(LocalsTeamRoleKey, actualRole)
		c.Locals(LocalsTeamIDKey, teamID)

		return c.Next()
	}
}
