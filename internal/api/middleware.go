package api

import (
	"context"
	"errors"
	"strings"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
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
	val := c.Locals(LocalsUserKey)
	if val == nil {
		return nil
	}
	user, _ := val.(*models.User)
	return user
}

// CurrentAPIToken retrieves the used models.APIToken from the Fiber context (if Bearer PAT was used).
func CurrentAPIToken(c fiber.Ctx) *models.APIToken {
	val := c.Locals(LocalsAPITokenKey)
	if val == nil {
		return nil
	}
	tok, _ := val.(*models.APIToken)
	return tok
}

// CurrentTeamRole retrieves the resolved models.TeamRole from the Fiber context.
func CurrentTeamRole(c fiber.Ctx) models.TeamRole {
	val := c.Locals(LocalsTeamRoleKey)
	if val == nil {
		return 0
	}
	role, _ := val.(models.TeamRole)
	return role
}

// requestCredentials holds the raw credentials a request presented: the atm_session cookie value
// and the Bearer token, either of which may be empty.
type requestCredentials struct {
	session string
	bearer  string
}

// credentialsFrom copies the request's credentials so they stay valid after the request completes
// (fasthttp reuses its buffers), e.g. for re-validating a long-lived WebSocket.
func credentialsFrom(c fiber.Ctx) requestCredentials {
	creds := requestCredentials{session: strings.Clone(c.Cookies(auth.SessionCookieName))}
	if authHeader := c.Get(fiber.HeaderAuthorization); strings.HasPrefix(authHeader, BearerPrefix) {
		creds.bearer = strings.Clone(strings.TrimSpace(strings.TrimPrefix(authHeader, BearerPrefix)))
	}
	return creds
}

// authenticate resolves the credentials to a user: a valid session cookie first, then a Bearer
// Personal Access Token (which also yields the token). ok is false if neither is valid.
func (cr requestCredentials) authenticate(ctx context.Context, authService service.AuthService, sm *auth.SessionManager) (user *models.User, token *models.APIToken, ok bool) {
	if sm != nil && cr.session != "" {
		if claims, err := sm.VerifyPayload(cr.session); err == nil {
			if user, ok := sessionUser(ctx, authService, claims); ok {
				return user, nil, true
			}
		}
	}
	if cr.bearer != "" {
		if user, token, err := authService.AuthenticatePAT(ctx, cr.bearer); err == nil && user != nil {
			return user, token, true
		}
	}
	return nil, nil, false
}

// sessionUser loads the user a verified session belongs to. ok is false if the user is gone or the
// session was revoked by a later logout.
func sessionUser(ctx context.Context, authService service.AuthService, claims *auth.SessionClaims) (user *models.User, ok bool) {
	user, err := authService.GetUserByID(ctx, claims.UserID)
	if err != nil || user == nil || claims.RevokedBy(user.SessionsValidAfter) {
		return nil, false
	}
	return user, true
}

// RequireAuth provides dual authentication via session cookie (atm_session) or Bearer Personal Access Token.
// For session cookies: extracts claims and loads user via AuthService.
// For Bearer tokens: authenticates token via AuthService.
// Returns 401 Unauthorized if neither is present or valid.
func RequireAuth(authService service.AuthService, sm *auth.SessionManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		user, token, ok := credentialsFrom(c).authenticate(c.Context(), authService, sm)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Unauthorized: valid session cookie or Personal Access Token required",
			})
		}
		setAuthenticated(c, user, token)
		return c.Next()
	}
}

// OptionalAuth populates CurrentUser if a valid session cookie or token is present, but does not block requests if unauthenticated.
func OptionalAuth(authService service.AuthService, sm *auth.SessionManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		if user, token, ok := credentialsFrom(c).authenticate(c.Context(), authService, sm); ok {
			setAuthenticated(c, user, token)
		}
		return c.Next()
	}
}

// setAuthenticated records the authenticated user, and the PAT when one was used, in Locals. A PAT
// also restricts the request context to the token's team, which every team-scoped authorization
// check (AuthService.AuthorizeTeamRole, TaskService) enforces.
func setAuthenticated(c fiber.Ctx, user *models.User, token *models.APIToken) {
	c.Locals(LocalsUserKey, user)
	if token != nil {
		c.Locals(LocalsAPITokenKey, token)
		c.SetContext(withTokenScope(c.Context(), token))
	}
}

// withTokenScope restricts ctx to the PAT's team when token is non-nil.
func withTokenScope(ctx context.Context, token *models.APIToken) context.Context {
	if token == nil {
		return ctx
	}
	return auth.WithTeamScope(ctx, token.TeamID)
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

// RequireProjectRole enforces team-level RBAC for routes addressed by a project ID path parameter.
// It resolves the project's owning team and checks the authenticated user's role against minRole.
// An unknown project yields the same 403 as non-membership so project existence is not disclosed,
// matching the task service's project authorization.
func RequireProjectRole(authService service.AuthService, projectRepo repository.ProjectRepository, param string, minRole models.TeamRole) fiber.Handler {
	return func(c fiber.Ctx) error {
		user := CurrentUser(c)
		if user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Unauthorized",
			})
		}
		if projectRepo == nil {
			return CentralErrorHandler(c, common.ErrServiceUnavailable)
		}

		projectID := c.Params(param)
		if projectID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Missing project id parameter",
			})
		}

		project, err := projectRepo.GetByID(c.Context(), projectID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, common.ErrNotFound) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "Access denied: you are not a member of this team",
				})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Database error checking project permissions",
			})
		}

		actualRole, err := authService.AuthorizeTeamRole(c.Context(), project.TeamID, user.ID, minRole)
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
		c.Locals(LocalsTeamIDKey, project.TeamID)

		return c.Next()
	}
}
