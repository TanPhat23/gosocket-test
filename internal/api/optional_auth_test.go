package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
)

func TestOptionalAuthMiddleware(t *testing.T) {
	db := setupMiddlewareTestDB(t)
	sessionSecret := []byte("secret-key-32-bytes-long-for-test!")
	sm := auth.NewSessionManager(sessionSecret, 24*time.Hour, false)

	user := models.User{
		ID:          "usr_opt",
		Issuer:      "https://idp.example.com",
		Subject:     "sub_opt",
		Email:       "opt@example.com",
		DisplayName: "Optional User",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	// A user whose sessions were revoked by logout after the session below was issued.
	revokedAt := time.Now().Add(-time.Hour).UTC()
	revokedUser := models.User{
		ID:                 "usr_revoked",
		Issuer:             "https://idp.example.com",
		Subject:            "sub_revoked",
		SessionsValidAfter: &revokedAt,
	}
	if err := db.Create(&revokedUser).Error; err != nil {
		t.Fatalf("failed to seed revoked user: %v", err)
	}

	team := models.Team{ID: "team_opt", Name: "Optional Team", Slug: "optional-team"}
	if err := db.Create(&team).Error; err != nil {
		t.Fatalf("failed to seed team: %v", err)
	}

	rawToken, tokenHash, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	if err := db.Create(&models.APIToken{
		ID:        "tok_opt",
		UserID:    user.ID,
		TeamID:    team.ID,
		Name:      "Optional token",
		TokenHash: tokenHash,
	}).Error; err != nil {
		t.Fatalf("failed to seed token: %v", err)
	}

	expiredRawToken, expiredHash, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("failed to generate expired token: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	if err := db.Create(&models.APIToken{
		ID:        "tok_opt_expired",
		UserID:    user.ID,
		TeamID:    team.ID,
		Name:      "Expired optional token",
		TokenHash: expiredHash,
		ExpiresAt: &past,
	}).Error; err != nil {
		t.Fatalf("failed to seed expired token: %v", err)
	}

	// A well-formed PAT that was never stored.
	unknownToken, _, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("failed to generate unknown token: %v", err)
	}

	userRepo := repository.NewUserRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	tokenRepo := repository.NewTokenRepository(db)
	authSvc := service.NewAuthService(userRepo, teamRepo, tokenRepo, nil)

	app := fiber.New()
	app.Get("/optional", api.OptionalAuth(authSvc, sm), func(c fiber.Ctx) error {
		body := fiber.Map{
			"user_id":   "",
			"has_token": api.CurrentAPIToken(c) != nil,
		}
		if u := api.CurrentUser(c); u != nil {
			body["user_id"] = u.ID
		}
		return c.JSON(body)
	})

	signFor := func(t *testing.T, claims auth.SessionClaims, secret []byte) string {
		t.Helper()
		signer := auth.NewSessionManager(secret, 24*time.Hour, false)
		signed, err := signer.SignPayload(claims)
		if err != nil {
			t.Fatalf("failed to sign session payload: %v", err)
		}
		return signed
	}
	validClaims := func(userID string) auth.SessionClaims {
		now := time.Now()
		return auth.SessionClaims{
			UserID:    userID,
			Issuer:    user.Issuer,
			Subject:   user.Subject,
			IssuedAt:  now,
			ExpiresAt: now.Add(time.Hour),
		}
	}

	tests := []struct {
		name          string
		cookie        string
		authorization string
		wantUserID    string
		wantToken     bool
	}{
		{
			name:       "valid session cookie populates user",
			cookie:     signFor(t, validClaims(user.ID), sessionSecret),
			wantUserID: user.ID,
		},
		{
			name:          "valid bearer PAT populates user and token",
			authorization: api.BearerPrefix + rawToken,
			wantUserID:    user.ID,
			wantToken:     true,
		},
		{
			name:       "no credentials passes through unauthenticated",
			wantUserID: "",
		},
		{
			name:          "bearer without the Bearer prefix passes through unauthenticated",
			authorization: rawToken,
		},
		{
			name:          "bearer with empty token passes through unauthenticated",
			authorization: api.BearerPrefix,
		},
		{
			name:       "session cookie signed with another secret passes through unauthenticated",
			cookie:     signFor(t, validClaims(user.ID), []byte("another-secret-key-32-bytes-long!!")),
			wantUserID: "",
		},
		{
			name:       "malformed session cookie passes through unauthenticated",
			cookie:     "not-a-session-token",
			wantUserID: "",
		},
		{
			name: "expired session cookie passes through unauthenticated",
			cookie: signFor(t, auth.SessionClaims{
				UserID:    user.ID,
				IssuedAt:  time.Now().Add(-2 * time.Hour),
				ExpiresAt: time.Now().Add(-time.Hour),
			}, sessionSecret),
			wantUserID: "",
		},
		{
			name: "session revoked by logout passes through unauthenticated",
			cookie: signFor(t, auth.SessionClaims{
				UserID:    revokedUser.ID,
				IssuedAt:  time.Now().Add(-2 * time.Hour),
				ExpiresAt: time.Now().Add(time.Hour),
			}, sessionSecret),
			wantUserID: "",
		},
		{
			name:       "session for a user that does not exist passes through unauthenticated",
			cookie:     signFor(t, validClaims("usr_missing"), sessionSecret),
			wantUserID: "",
		},
		{
			name:          "unknown bearer PAT passes through unauthenticated",
			authorization: api.BearerPrefix + unknownToken,
			wantUserID:    "",
		},
		{
			name:          "expired bearer PAT passes through unauthenticated",
			authorization: api.BearerPrefix + expiredRawToken,
			wantUserID:    "",
		},
		{
			name:          "invalid bearer PAT with valid session cookie keeps the cookie user",
			cookie:        signFor(t, validClaims(user.ID), sessionSecret),
			authorization: api.BearerPrefix + unknownToken,
			wantUserID:    user.ID,
		},
		{
			name:          "invalid session cookie falls back to valid bearer PAT",
			cookie:        signFor(t, validClaims(user.ID), []byte("another-secret-key-32-bytes-long!!")),
			authorization: api.BearerPrefix + rawToken,
			wantUserID:    user.ID,
			wantToken:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/optional", nil)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tc.cookie})
			}
			if tc.authorization != "" {
				req.Header.Set(fiber.HeaderAuthorization, tc.authorization)
			}

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			// OptionalAuth must never block: every case reaches the handler with 200.
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200 (OptionalAuth must not reject)", resp.StatusCode)
			}

			var body struct {
				UserID   string `json:"user_id"`
				HasToken bool   `json:"has_token"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.UserID != tc.wantUserID {
				t.Errorf("user_id = %q, want %q", body.UserID, tc.wantUserID)
			}
			if body.HasToken != tc.wantToken {
				t.Errorf("has_token = %v, want %v", body.HasToken, tc.wantToken)
			}
		})
	}
}
