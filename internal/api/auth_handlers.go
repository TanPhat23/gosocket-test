package api

import (
	"errors"
	"time"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/models"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// AuthHandlers holds dependencies for authentication endpoints.
type AuthHandlers struct {
	db             *gorm.DB
	sessionManager *auth.SessionManager
	authenticator  auth.Authenticator
	secureCookies  bool
}

// NewAuthHandlers creates a new AuthHandlers instance.
func NewAuthHandlers(db *gorm.DB, sm *auth.SessionManager, authenticator auth.Authenticator, secureCookies bool) *AuthHandlers {
	return &AuthHandlers{
		db:             db,
		sessionManager: sm,
		authenticator:  authenticator,
		secureCookies:  secureCookies,
	}
}

// Login initiates the OIDC authorization code flow with PKCE.
// GET /api/v1/auth/login
func (h *AuthHandlers) Login(c fiber.Ctx) error {
	if h.authenticator == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "OIDC authentication is not configured",
		})
	}

	state, challenge, err := h.authenticator.GenerateOIDCState()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate authorization state",
		})
	}

	signedState, err := h.authenticator.SignOIDCState(state)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to sign authorization state",
		})
	}

	c.Cookie(&fiber.Cookie{
		Name:     auth.StateCookieName,
		Value:    signedState,
		Path:     "/",
		Expires:  state.ExpiresAt,
		MaxAge:   int(auth.StateTTL.Seconds()),
		HTTPOnly: true,
		Secure:   h.secureCookies,
		SameSite: "Lax",
	})

	authURL := h.authenticator.AuthCodeURL(state.State, challenge, state.Nonce)
	return c.Redirect().To(authURL)
}

// Callback handles the OIDC provider redirect, exchanges code, verifies ID token,
// upserts user, and establishes an atm_session cookie.
// GET /api/v1/auth/callback
func (h *AuthHandlers) Callback(c fiber.Ctx) error {
	if h.authenticator == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "OIDC authentication is not configured",
		})
	}

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
		Secure:   h.secureCookies,
		SameSite: "Lax",
	})

	storedState, err := h.authenticator.VerifyOIDCState(stateCookie)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid or expired authorization state",
		})
	}

	if storedState.State != stateParam {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Mismatched authorization state",
		})
	}

	token, err := h.authenticator.Exchange(c.Context(), code, storedState.CodeVerifier)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Failed to exchange authorization code",
		})
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Missing id_token in token response",
		})
	}

	idToken, err := h.authenticator.VerifyIDToken(c.Context(), rawIDToken)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Failed to verify ID token",
		})
	}

	if idToken.Nonce != storedState.Nonce {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Nonce mismatch",
		})
	}

	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	_ = idToken.Claims(&claims)

	issuer := idToken.Issuer
	subject := idToken.Subject

	// Upsert user strictly keyed by (issuer, subject)
	var user models.User
	result := h.db.Where("issuer = ? AND subject = ?", issuer, subject).First(&user)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		now := time.Now()
		user = models.User{
			ID:          "usr_" + idToken.Subject,
			Issuer:      issuer,
			Subject:     subject,
			Email:       claims.Email,
			DisplayName: claims.Name,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := h.db.Create(&user).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to create user record",
			})
		}
	} else if result.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Database error retrieving user",
		})
	} else {
		// Update profile info if present
		updates := map[string]interface{}{}
		if claims.Email != "" && claims.Email != user.Email {
			updates["email"] = claims.Email
		}
		if claims.Name != "" && claims.Name != user.DisplayName {
			updates["display_name"] = claims.Name
		}
		if len(updates) > 0 {
			_ = h.db.Model(&user).Updates(updates).Error
		}
	}

	// Create session
	_, err = h.sessionManager.CreateSessionFiber(c, user.ID, user.Issuer, user.Subject)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create session",
		})
	}

	return c.JSON(fiber.Map{
		"status": "authenticated",
		"user": fiber.Map{
			"id":           user.ID,
			"issuer":       user.Issuer,
			"subject":      user.Subject,
			"email":        user.Email,
			"display_name": user.DisplayName,
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

	var user models.User
	if err := h.db.Where("id = ?", claims.UserID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "User not found",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Database error",
		})
	}

	return c.JSON(fiber.Map{
		"user": fiber.Map{
			"id":           user.ID,
			"issuer":       user.Issuer,
			"subject":      user.Subject,
			"email":        user.Email,
			"display_name": user.DisplayName,
		},
	})
}
