package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
)

var (
	ErrOIDCNotConfigured = errors.New("OIDC authentication is not configured")
	ErrInvalidState      = errors.New("invalid or expired authorization state")
	ErrStateMismatch     = errors.New("mismatched authorization state")
	ErrTokenExchange     = errors.New("failed to exchange authorization code")
	ErrMissingIDToken    = errors.New("missing id_token in token response")
	ErrVerifyIDToken     = errors.New("failed to verify ID token")
	ErrNonceMismatch     = errors.New("nonce mismatch")
	ErrInvalidToken      = errors.New("invalid or expired personal access token")
	ErrNotTeamMember     = errors.New("user is not a member of this team")
	ErrForbidden         = errors.New("insufficient team role permissions")
)

// AuthService defines authentication and authorization business logic.
type AuthService interface {
	GenerateLoginURL() (authURL string, signedState string, state *auth.OIDCState, err error)
	HandleCallback(ctx context.Context, stateCookie, stateParam, code string) (*models.User, error)
	GetUserByID(ctx context.Context, userID string) (*models.User, error)
	AuthenticatePAT(ctx context.Context, rawToken string) (*models.User, *models.APIToken, error)
	AuthorizeTeamRole(ctx context.Context, teamID, userID string, requiredRole models.TeamRole) (models.TeamRole, error)
}

type authService struct {
	userRepo      repository.UserRepository
	teamRepo      repository.TeamRepository
	tokenRepo     repository.TokenRepository
	authenticator auth.Authenticator
}

// NewAuthService creates a new AuthService instance.
func NewAuthService(
	userRepo repository.UserRepository,
	teamRepo repository.TeamRepository,
	tokenRepo repository.TokenRepository,
	authenticator auth.Authenticator,
) AuthService {
	return &authService{
		userRepo:      userRepo,
		teamRepo:      teamRepo,
		tokenRepo:     tokenRepo,
		authenticator: authenticator,
	}
}

func (s *authService) GenerateLoginURL() (string, string, *auth.OIDCState, error) {
	if s.authenticator == nil {
		return "", "", nil, ErrOIDCNotConfigured
	}

	state, challenge, err := s.authenticator.GenerateOIDCState()
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to generate authorization state: %w", err)
	}

	signedState, err := s.authenticator.SignOIDCState(state)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to sign authorization state: %w", err)
	}

	authURL := s.authenticator.AuthCodeURL(state.State, challenge, state.Nonce)
	return authURL, signedState, state, nil
}

func (s *authService) HandleCallback(ctx context.Context, stateCookie, stateParam, code string) (*models.User, error) {
	if s.authenticator == nil {
		return nil, ErrOIDCNotConfigured
	}

	storedState, err := s.authenticator.VerifyOIDCState(stateCookie)
	if err != nil {
		return nil, ErrInvalidState
	}

	if storedState.State != stateParam {
		return nil, ErrStateMismatch
	}

	token, err := s.authenticator.Exchange(ctx, code, storedState.CodeVerifier)
	if err != nil {
		return nil, ErrTokenExchange
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, ErrMissingIDToken
	}

	idToken, err := s.authenticator.VerifyIDToken(ctx, rawIDToken)
	if err != nil {
		return nil, ErrVerifyIDToken
	}

	if idToken.Nonce != storedState.Nonce {
		return nil, ErrNonceMismatch
	}

	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	_ = idToken.Claims(&claims)

	issuer := idToken.Issuer
	subject := idToken.Subject

	existingUser, err := s.userRepo.GetByIssuerSubject(ctx, issuer, subject)
	if err != nil {
		// Create new user
		now := time.Now()
		newUser := &models.User{
			ID:          "usr_" + subject,
			Issuer:      issuer,
			Subject:     subject,
			Email:       claims.Email,
			DisplayName: claims.Name,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.userRepo.Create(ctx, newUser); err != nil {
			return nil, fmt.Errorf("failed to create user record: %w", err)
		}
		return newUser, nil
	}

	// User exists, update profile if needed
	updates := make(map[string]any)
	if claims.Email != "" && claims.Email != existingUser.Email {
		updates["email"] = claims.Email
	}
	if claims.Name != "" && claims.Name != existingUser.DisplayName {
		updates["display_name"] = claims.Name
	}
	if len(updates) > 0 {
		_ = s.userRepo.Update(ctx, existingUser, updates)
		if email, ok := updates["email"].(string); ok {
			existingUser.Email = email
		}
		if name, ok := updates["display_name"].(string); ok {
			existingUser.DisplayName = name
		}
	}

	return existingUser, nil
}

func (s *authService) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	return s.userRepo.GetByID(ctx, userID)
}

func (s *authService) AuthenticatePAT(ctx context.Context, rawToken string) (*models.User, *models.APIToken, error) {
	if rawToken == "" || !strings.HasPrefix(rawToken, auth.TokenPrefix) {
		return nil, nil, ErrInvalidToken
	}

	tokenHash := auth.HashToken(rawToken)
	apiToken, err := s.tokenRepo.GetByHash(ctx, tokenHash)
	if err != nil {
		return nil, nil, ErrInvalidToken
	}

	if apiToken.ExpiresAt != nil && apiToken.ExpiresAt.Before(time.Now()) {
		return nil, nil, ErrInvalidToken
	}

	user, err := s.userRepo.GetByID(ctx, apiToken.UserID)
	if err != nil {
		return nil, nil, ErrInvalidToken
	}

	now := time.Now().UTC()
	_ = s.tokenRepo.UpdateLastUsed(ctx, apiToken.ID, now)
	apiToken.LastUsedAt = &now

	return user, apiToken, nil
}

func (s *authService) AuthorizeTeamRole(ctx context.Context, teamID, userID string, requiredRole models.TeamRole) (models.TeamRole, error) {
	membership, err := s.teamRepo.GetMembership(ctx, teamID, userID)
	if err != nil {
		return 0, ErrNotTeamMember
	}

	if !auth.HasRolePermission(membership.Role, requiredRole) {
		return membership.Role, ErrForbidden
	}

	return membership.Role, nil
}
