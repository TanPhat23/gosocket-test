package api

import (
	"errors"
	"strings"
	"time"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/models"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

const (
	LocalsUserKey        = "currentUser"
	LocalsAPITokenKey    = "currentAPIToken"
	LocalsTeamRoleKey    = "currentTeamRole"
	LocalsTeamIDKey      = "currentTeamID"
	BearerPrefix         = "Bearer "
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
		return ""
	}
	role, _ := val.(models.TeamRole)
	return role
}

// RequireAuth provides dual authentication via session cookie (atm_session) or Bearer Personal Access Token.
// For session cookies: extracts claims and loads user from DB.
// For Bearer tokens: verifies atm_pat_ prefix, hashes with SHA-256, verifies in `api_tokens` (non-expired),
// updates last_used_at, and loads associated user.
// Returns 401 Unauthorized if neither is present or valid.
func RequireAuth(db *gorm.DB, sm *auth.SessionManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		// 1. Check atm_session cookie first
		if sm != nil {
			claims, err := sm.ValidateFiberRequest(c)
			if err == nil && claims != nil {
				var user models.User
				if err := db.Where("id = ?", claims.UserID).First(&user).Error; err == nil {
					c.Locals(LocalsUserKey, &user)
					return c.Next()
				}
			}
		}

		// 2. Check Authorization: Bearer <token>
		authHeader := c.Get(fiber.HeaderAuthorization)
		if strings.HasPrefix(authHeader, BearerPrefix) {
			rawToken := strings.TrimSpace(strings.TrimPrefix(authHeader, BearerPrefix))
			if strings.HasPrefix(rawToken, auth.TokenPrefix) {
				tokenHash := auth.HashToken(rawToken)

				var apiToken models.APIToken
				err := db.Where("token_hash = ?", tokenHash).First(&apiToken).Error
				if err == nil {
					// Check expiration
					if apiToken.ExpiresAt == nil || apiToken.ExpiresAt.After(time.Now()) {
						var user models.User
						if err := db.Where("id = ?", apiToken.UserID).First(&user).Error; err == nil {
							// Update last_used_at asynchronously or in-line
							now := time.Now()
							_ = db.Model(&apiToken).Update("last_used_at", &now).Error

							c.Locals(LocalsUserKey, &user)
							c.Locals(LocalsAPITokenKey, &apiToken)
							return c.Next()
						}
					}
				}
			}
		}

		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized: valid session cookie or Personal Access Token required",
		})
	}
}

// RequireTeamRole enforces team-level RBAC based on team_id in params or queries.
// It checks the authenticated user's role against minRole: owner > admin > member > viewer.
// Returns 403 Forbidden if the user is not in the team or lacks sufficient permission.
func RequireTeamRole(db *gorm.DB, minRole models.TeamRole) fiber.Handler {
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

		var membership models.TeamMembership
		err := db.Where("team_id = ? AND user_id = ?", teamID, user.ID).First(&membership).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "Access denied: you are not a member of this team",
				})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Database error checking team permissions",
			})
		}

		if !auth.HasRolePermission(membership.Role, minRole) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "Forbidden: insufficient team role permissions",
			})
		}

		c.Locals(LocalsTeamRoleKey, membership.Role)
		c.Locals(LocalsTeamIDKey, teamID)

		return c.Next()
	}
}
