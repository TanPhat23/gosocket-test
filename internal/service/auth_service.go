package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// patLastUsedInterval is the minimum gap between LastUsedAt writes for one personal access token.
const patLastUsedInterval = 5 * time.Minute

var (
	ErrOIDCNotConfigured = errors.New("OIDC authentication is not configured")
	ErrInvalidState      = errors.New("invalid or expired authorization state")
	ErrStateMismatch     = errors.New("mismatched authorization state")
	ErrTokenExchange     = errors.New("failed to exchange authorization code")
	ErrMissingIDToken    = errors.New("missing id_token in token response")
	ErrVerifyIDToken     = errors.New("failed to verify ID token")
	ErrNonceMismatch     = errors.New("nonce mismatch")
	ErrInvalidToken      = errors.New("invalid or expired personal access token")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrNotTeamMember     = errors.New("user is not a member of this team")
	ErrForbidden         = errors.New("insufficient team role permissions")
)

// AuthService defines authentication and authorization business logic.
type AuthService interface {
	GenerateLoginURL() (authURL string, signedState string, state *auth.OIDCState, err error)
	HandleCallback(ctx context.Context, stateCookie, stateParam, code string) (*models.User, error)
	LoginWithCredentials(ctx context.Context, email, password string) (*models.User, error)
	GetUserByID(ctx context.Context, userID string) (*models.User, error)
	// RevokeSessions invalidates every session cookie issued to userID before now.
	RevokeSessions(ctx context.Context, userID string) error
	AuthenticatePAT(ctx context.Context, rawToken string) (*models.User, *models.APIToken, error)
	AuthorizeTeamRole(ctx context.Context, teamID, userID string, requiredRole models.TeamRole) (models.TeamRole, error)
	// UpdateGitIdentity sets the author name and email the user's tasks are committed as; both empty clears it.
	UpdateGitIdentity(ctx context.Context, userID, name, email string) (*models.User, error)
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
			ID:          common.NewID(),
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

type localAccount struct {
	subject  string
	name     string
	passHash string
}

var localAccounts = map[string]localAccount{
	"admin@example.com": {
		subject:  "CiQwOGE1MmM2My00NDRhLTRkNTYtOTgzMy00MTUyYmIwZDQ0YWMSBWxvY2Fs",
		name:     "admin",
		passHash: "$2a$10$kky4TaVOWLyxpZsBNNktmeSgy0pqobTo1aYbjlpgCG.IxEReQpgqi",
	},
	"developer@example.com": {
		subject:  "CiQxOGE1MmM2My00NDRhLTRkNTYtOTgzMy00MTUyYmIwZDQ0YWQSBWxvY2Fs",
		name:     "developer",
		passHash: "$2a$10$kky4TaVOWLyxpZsBNNktmeSgy0pqobTo1aYbjlpgCG.IxEReQpgqi",
	},
}

// dummyPassHash is a bcrypt hash (same cost as the local accounts) of a random, discarded password.
// Unknown emails are compared against it so a miss costs the same bcrypt work as a wrong password and
// response timing does not reveal which emails are local accounts.
const dummyPassHash = "$2a$10$Ce8VVwDU6V7jAdFHyOiZ5eNHdZlCcnG7O5B.fykTMN44CGYkE9Tji"

// LoginWithCredentials authenticates one of the built-in development accounts. It only resolves or
// creates the user record; it never grants team membership. Whether the endpoint is reachable at all
// is decided by the API layer (config.SecurityConfig.EnableLocalLogin, never true in production).
func (s *authService) LoginWithCredentials(ctx context.Context, email, password string) (*models.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	acc, ok := localAccounts[email]
	hash := acc.passHash
	if !ok {
		hash = dummyPassHash
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil || !ok {
		return nil, ErrInvalidCredentials
	}

	issuer := "http://localhost/dex"
	if s.authenticator != nil && s.authenticator.IssuerURL() != "" {
		issuer = s.authenticator.IssuerURL()
	}

	existingUser, err := s.userRepo.GetByIssuerSubject(ctx, issuer, acc.subject)
	if err == nil {
		return existingUser, nil
	}

	now := time.Now()
	newUser := &models.User{
		ID:          common.NewID(),
		Issuer:      issuer,
		Subject:     acc.subject,
		Email:       email,
		DisplayName: acc.name,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.userRepo.Create(ctx, newUser); err != nil {
		return nil, fmt.Errorf("failed to create user record: %w", err)
	}
	return newUser, nil
}

func (s *authService) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	return s.userRepo.GetByID(ctx, userID)
}

func (s *authService) UpdateGitIdentity(ctx context.Context, userID, name, email string) (*models.User, error) {
	name, email, err := NormalizeGitIdentity(name, email)
	if err != nil {
		return nil, err
	}
	if err := s.userRepo.Update(ctx, &models.User{ID: userID}, map[string]any{"git_name": name, "git_email": email}); err != nil {
		return nil, err
	}
	return s.userRepo.GetByID(ctx, userID)
}

// RevokeSessions sets the user's SessionsValidAfter to now. The time is truncated to microseconds,
// the precision of timestamptz, so a session issued after this call always compares as newer.
func (s *authService) RevokeSessions(ctx context.Context, userID string) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return s.userRepo.Update(ctx, &models.User{ID: userID}, map[string]any{"sessions_valid_after": now})
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

	// LastUsedAt is informational, so it is refreshed at most once per patLastUsedInterval; long-lived
	// sockets re-authenticate every few seconds and must not cost a write each time.
	now := time.Now().UTC()
	if apiToken.LastUsedAt == nil || now.Sub(*apiToken.LastUsedAt) >= patLastUsedInterval {
		_ = s.tokenRepo.UpdateLastUsed(ctx, apiToken.ID, now)
		apiToken.LastUsedAt = &now
	}

	return user, apiToken, nil
}

// AuthorizeTeamRole checks userID's role on teamID. A team outside ctx's PAT team scope is treated
// as non-membership.
func (s *authService) AuthorizeTeamRole(ctx context.Context, teamID, userID string, requiredRole models.TeamRole) (models.TeamRole, error) {
	if !auth.TeamAllowed(ctx, teamID) {
		return 0, ErrNotTeamMember
	}
	membership, err := s.teamRepo.GetMembership(ctx, teamID, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrNotTeamMember
		}
		return 0, fmt.Errorf("look up team membership: %w", err)
	}

	if !auth.HasRolePermission(membership.Role, requiredRole) {
		return membership.Role, ErrForbidden
	}

	return membership.Role, nil
}
