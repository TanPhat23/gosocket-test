package api

import (
	"strings"
	"time"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
)

// AuthHandlers holds dependencies for authentication endpoints.
type AuthHandlers struct {
	authService       service.AuthService
	sessionManager    *auth.SessionManager
	secureCookies     bool
	localLoginEnabled bool
}

// NewAuthHandlers creates a new AuthHandlers instance. localLoginEnabled gates the built-in
// development credential login (config.SecurityConfig.EnableLocalLogin).
func NewAuthHandlers(authService service.AuthService, sm *auth.SessionManager, secureCookies, localLoginEnabled bool) *AuthHandlers {
	return &AuthHandlers{
		authService:       authService,
		sessionManager:    sm,
		secureCookies:     secureCookies,
		localLoginEnabled: localLoginEnabled,
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
		return CentralErrorHandler(c, err)
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
//
// When local login is disabled (the default, and always in production) the route stays registered
// and answers exactly like a wrong password (401 ErrInvalidCredentials), so it neither checks the
// built-in accounts nor advertises whether they exist.
func (h *AuthHandlers) LoginWithCredentials(c fiber.Ctx) error {
	if !h.localLoginEnabled {
		return CentralErrorHandler(c, service.ErrInvalidCredentials)
	}

	var req dto.LoginRequestDTO
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	user, err := h.authService.LoginWithCredentials(c.Context(), req.Email, req.Password)
	if err != nil {
		return CentralErrorHandler(c, err)
	}

	_, err = h.sessionManager.CreateSessionFiber(c, user.ID, user.Issuer, user.Subject)
	if err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": "authenticated",
		"user":   dto.ToUserResponse(user),
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
		return CentralErrorHandler(c, err)
	}

	// Create session
	_, err = h.sessionManager.CreateSessionFiber(c, user.ID, user.Issuer, user.Subject)
	if err != nil {
		return CentralErrorHandler(c, err)
	}

	if strings.Contains(c.Get("Accept"), "text/html") {
		return c.Redirect().To("/")
	}

	return c.JSON(fiber.Map{
		"status": "authenticated",
		"user":   dto.ToUserResponse(user),
	})
}

// Logout revokes the caller's sessions and clears the atm_session cookie. Sessions are stateless
// signed cookies, so revocation is per user ("log out everywhere"): every cookie issued before now
// stops validating. Personal Access Tokens are unaffected. Without a valid, unrevoked cookie it only
// clears the cookie, so it is idempotent and a replayed revoked cookie cannot end newer sessions.
// POST /api/v1/auth/logout
func (h *AuthHandlers) Logout(c fiber.Ctx) error {
	h.sessionManager.ClearSessionFiber(c)
	if claims, err := h.sessionManager.ValidateFiberRequest(c); err == nil {
		if _, ok := sessionUser(c.Context(), h.authService, claims); ok {
			if err := h.authService.RevokeSessions(c.Context(), claims.UserID); err != nil {
				return CentralErrorHandler(c, err)
			}
		}
	}
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

	user, ok := sessionUser(c.Context(), h.authService, claims)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	return c.JSON(fiber.Map{
		"user": dto.ToUserResponse(user),
	})
}

// UpdateGitIdentity handles PUT /api/v1/me/git-identity. Protected by RequireAuth. It sets the author name and
// email the caller's tasks are committed as; both empty clears it.
func (h *AuthHandlers) UpdateGitIdentity(c fiber.Ctx) error {
	user := CurrentUser(c)
	if user == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	req, err := bindBody[dto.GitIdentityRequest](c)
	if err != nil {
		return badRequest(c, err)
	}
	updated, err := h.authService.UpdateGitIdentity(c.Context(), user.ID, req.GitName, req.GitEmail)
	if err != nil {
		return CentralErrorHandler(c, err)
	}
	return c.JSON(fiber.Map{"user": dto.ToUserResponse(updated)})
}
