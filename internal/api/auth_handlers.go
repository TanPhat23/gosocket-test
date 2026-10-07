package api

import (
	"errors"
	"strings"
	"time"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
)

// AuthHandlers holds dependencies for authentication endpoints.
type AuthHandlers struct {
	authService    service.AuthService
	sessionManager *auth.SessionManager
	secureCookies  bool
}

// NewAuthHandlers creates a new AuthHandlers instance.
func NewAuthHandlers(authService service.AuthService, sm *auth.SessionManager, secureCookies bool) *AuthHandlers {
	return &AuthHandlers{
		authService:    authService,
		sessionManager: sm,
		secureCookies:  secureCookies,
	}
}

func (h *AuthHandlers) isSecure(c fiber.Ctx) bool {
	return auth.IsSecureCookie(h.secureCookies, c)
}

// Login initiates the OIDC authorization code flow with PKCE.
// GET /api/v1/auth/login
func (h *AuthHandlers) Login(c fiber.Ctx) error {
	authURL, signedState, state, err := h.authService.GenerateLoginURL()
	if err != nil {
		if errors.Is(err, service.ErrOIDCNotConfigured) {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "OIDC authentication is not configured",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate authorization state",
		})
	}

	c.Cookie(&fiber.Cookie{
		Name:     auth.StateCookieName,
		Value:    signedState,
		Path:     "/",
		Expires:  state.ExpiresAt,
		MaxAge:   int(auth.StateTTL.Seconds()),
		HTTPOnly: true,
		Secure:   h.isSecure(c),
		SameSite: "Lax",
	})

	return c.Redirect().To(authURL)
}

// LoginWithCredentials authenticates direct user credentials and creates a session.
// POST /api/v1/auth/login
func (h *AuthHandlers) LoginWithCredentials(c fiber.Ctx) error {
	var req dto.LoginRequestDTO
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	user, err := h.authService.LoginWithCredentials(c.Context(), req.Email, req.Password)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid email or password",
		})
	}

	_, err = h.sessionManager.CreateSessionFiber(c, user.ID, user.Issuer, user.Subject)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create session",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": "authenticated",
		"user": dto.UserResponseDTO{
			ID:          user.ID,
			Issuer:      user.Issuer,
			Subject:     user.Subject,
			Email:       user.Email,
			DisplayName: user.DisplayName,
		},
	})
}

// Callback handles the OIDC provider redirect, exchanges code, verifies ID token,
// upserts user, and establishes an atm_session cookie.
// GET /api/v1/auth/callback
func (h *AuthHandlers) Callback(c fiber.Ctx) error {
	code := c.Query("code")
	stateParam := c.Query("state")
	if code == "" || stateParam == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Missing code or state parameter",
		})
	}

	stateCookie := c.Cookies(auth.StateCookieName)
	if stateCookie == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Missing authorization state cookie",
		})
	}

	// Clear temporary state cookie
	c.Cookie(&fiber.Cookie{
		Name:     auth.StateCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HTTPOnly: true,
		Secure:   h.isSecure(c),
		SameSite: "Lax",
	})

	user, err := h.authService.HandleCallback(c.Context(), stateCookie, stateParam, code)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrOIDCNotConfigured):
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "OIDC authentication is not configured",
			})
		case errors.Is(err, service.ErrInvalidState):
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid or expired authorization state",
			})
		case errors.Is(err, service.ErrStateMismatch):
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Mismatched authorization state",
			})
		case errors.Is(err, service.ErrTokenExchange):
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Failed to exchange authorization code",
			})
		case errors.Is(err, service.ErrMissingIDToken):
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Missing id_token in token response",
			})
		case errors.Is(err, service.ErrVerifyIDToken):
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Failed to verify ID token",
			})
		case errors.Is(err, service.ErrNonceMismatch):
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Nonce mismatch",
			})
		default:
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Authentication failed",
			})
		}
	}

	// Create session
	_, err = h.sessionManager.CreateSessionFiber(c, user.ID, user.Issuer, user.Subject)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create session",
		})
	}

	if strings.Contains(c.Get("Accept"), "text/html") {
		return c.Redirect().To("/")
	}

	return c.JSON(fiber.Map{
		"status": "authenticated",
		"user": dto.UserResponseDTO{
			ID:          user.ID,
			Issuer:      user.Issuer,
			Subject:     user.Subject,
			Email:       user.Email,
			DisplayName: user.DisplayName,
		},
	})
}

// Logout clears the atm_session cookie.
// POST /api/v1/auth/logout
func (h *AuthHandlers) Logout(c fiber.Ctx) error {
	h.sessionManager.ClearSessionFiber(c)
	return c.JSON(fiber.Map{
		"status": "logged_out",
	})
}

// Me returns the currently authenticated user's details.
// GET /api/v1/auth/me
func (h *AuthHandlers) Me(c fiber.Ctx) error {
	claims, err := h.sessionManager.ValidateFiberRequest(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}

	user, err := h.authService.GetUserByID(c.Context(), claims.UserID)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	return c.JSON(fiber.Map{
		"user": dto.UserResponseDTO{
			ID:          user.ID,
			Issuer:      user.Issuer,
			Subject:     user.Subject,
			Email:       user.Email,
			DisplayName: user.DisplayName,
		},
	})
}
