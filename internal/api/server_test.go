package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

type mockAuthenticator struct {
	authCodeURLFunc func(state, codeChallenge, nonce string) string
	exchangeFunc    func(ctx context.Context, code, codeVerifier string) (*oauth2.Token, error)
	verifyIDToken   func(ctx context.Context, rawIDToken string) (*oidc.IDToken, error)
	state           *auth.OIDCState
	challenge       string
	issuerURL       string
}

func (m *mockAuthenticator) AuthCodeURL(state, codeChallenge, nonce string) string {
	if m.authCodeURLFunc != nil {
		return m.authCodeURLFunc(state, codeChallenge, nonce)
	}
	return "https://idp.example.com/auth?state=" + state
}

func (m *mockAuthenticator) Exchange(ctx context.Context, code, codeVerifier string) (*oauth2.Token, error) {
	if m.exchangeFunc != nil {
		return m.exchangeFunc(ctx, code, codeVerifier)
	}
	token := &oauth2.Token{
		AccessToken: "mock-access-token",
		TokenType:   "Bearer",
	}
	return token.WithExtra(map[string]any{
		"id_token": "mock-raw-id-token",
	}), nil
}

func (m *mockAuthenticator) VerifyIDToken(ctx context.Context, rawIDToken string) (*oidc.IDToken, error) {
	if m.verifyIDToken != nil {
		return m.verifyIDToken(ctx, rawIDToken)
	}
	return &oidc.IDToken{
		Issuer:  m.IssuerURL(),
		Subject: "sub-12345",
		Nonce:   m.state.Nonce,
	}, nil
}

func (m *mockAuthenticator) GenerateOIDCState() (*auth.OIDCState, string, error) {
	if m.state == nil {
		m.state = &auth.OIDCState{
			State:        "test-state-abc",
			Nonce:        "test-nonce-def",
			CodeVerifier: "test-verifier-ghi",
			ExpiresAt:    time.Now().Add(10 * time.Minute),
		}
		m.challenge = auth.GenerateCodeChallenge(m.state.CodeVerifier)
	}
	return m.state, m.challenge, nil
}

func (m *mockAuthenticator) SignOIDCState(state *auth.OIDCState) (string, error) {
	mac := auth.NewSessionManager([]byte("mock-32-byte-secret-for-testing!"), 10*time.Minute, false)
	claims := auth.SessionClaims{
		UserID:    state.State,
		Issuer:    state.Nonce,
		Subject:   state.CodeVerifier,
		ExpiresAt: state.ExpiresAt,
	}
	return mac.SignPayload(claims)
}

func (m *mockAuthenticator) VerifyOIDCState(signedState string) (*auth.OIDCState, error) {
	mac := auth.NewSessionManager([]byte("mock-32-byte-secret-for-testing!"), 10*time.Minute, false)
	claims, err := mac.VerifyPayload(signedState)
	if err != nil {
		return nil, auth.ErrInvalidState
	}
	return &auth.OIDCState{
		State:        claims.UserID,
		Nonce:        claims.Issuer,
		CodeVerifier: claims.Subject,
		ExpiresAt:    claims.ExpiresAt,
	}, nil
}

func (m *mockAuthenticator) IssuerURL() string {
	if m.issuerURL != "" {
		return m.issuerURL
	}
	return "https://idp.example.com"
}

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}

	// In SQLite, models defined with `gorm:"type:timestamptz"` create column type timestamptz,
	// which modernc.org/sqlite returns as string, causing sql: Scan error into *time.Time.
	// For testing with sqlite, create table with DATETIME:
	err = db.Exec(`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		issuer TEXT NOT NULL,
		subject TEXT NOT NULL,
		email TEXT,
		display_name TEXT,
		sessions_valid_after DATETIME
	)`).Error
	if err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}

	return db
}

func TestStructValidator(t *testing.T) {
	validator := api.NewStructValidator()

	type TestPayload struct {
		Name  string `validate:"required,min=3"`
		Email string `validate:"required,email"`
	}

	valid := TestPayload{Name: "Alice", Email: "alice@example.com"}
	if err := validator.Validate(&valid); err != nil {
		t.Errorf("expected valid struct, got err: %v", err)
	}

	invalid := TestPayload{Name: "Al", Email: "not-an-email"}
	if err := validator.Validate(&invalid); err == nil {
		t.Errorf("expected error for invalid struct, got nil")
	}
}

func TestAuthEndpoints(t *testing.T) {
	db := setupTestDB(t)
	cfg := &config.Config{
		AppEnv:        "development",
		SessionSecret: "test-session-secret-32-bytes-long!",
	}

	mockAuth := &mockAuthenticator{
		issuerURL: "https://idp.example.com",
	}

	sessionSecret := []byte(cfg.SessionSecret)
	sessionManager := auth.NewSessionManager(sessionSecret, 24*time.Hour, false)

	userRepo := repository.NewUserRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	tokenRepo := repository.NewTokenRepository(db)
	credentialRepo := repository.NewCredentialRepository(db)
	integrationRepo := repository.NewIntegrationRepository(db)
	taskRepo := repository.NewTaskRepository(db)

	cipher, _ := crypto.NewSecretCipher([]byte("12345678901234567890123456789012"))
	idempotencyStore := integrations.NewDBIdempotencyStore(db)

	authService := service.NewAuthService(userRepo, teamRepo, tokenRepo, mockAuth)
	credService := service.NewCredentialService(credentialRepo, cipher)
	webhookService := service.NewWebhookService(integrationRepo, credentialRepo, taskRepo, idempotencyStore, cipher, nil)

	server := api.NewServer(cfg, sessionManager, authService, credService, webhookService)

	t.Run("GET /api/v1/auth/login redirects and sets state cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/auth/login", nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusFound {
			t.Errorf("expected redirect status, got: %d", resp.StatusCode)
		}

		cookies := resp.Cookies()
		var stateCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == auth.StateCookieName {
				stateCookie = c
				break
			}
		}

		if stateCookie == nil {
			t.Fatalf("expected %s cookie in response", auth.StateCookieName)
		}
		if !stateCookie.HttpOnly {
			t.Errorf("expected HttpOnly flag on state cookie")
		}
	})

	t.Run("GET /api/v1/auth/callback fails without state or code", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/auth/callback", nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got: %d", resp.StatusCode)
		}
	})

	t.Run("GET /api/v1/auth/callback successfully exchanges and creates user & session", func(t *testing.T) {
		state, _, err := mockAuth.GenerateOIDCState()
		if err != nil {
			t.Fatalf("failed to generate state: %v", err)
		}
		signedState, err := mockAuth.SignOIDCState(state)
		if err != nil {
			t.Fatalf("failed to sign state: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/v1/auth/callback?code=mock-auth-code&state="+state.State, nil)
		req.AddCookie(&http.Cookie{
			Name:  auth.StateCookieName,
			Value: signedState,
		})

		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 OK, got: %d, body: %s", resp.StatusCode, string(body))
		}

		// Verify session cookie was set
		var sessionCookie *http.Cookie
		for _, c := range resp.Cookies() {
			if c.Name == auth.SessionCookieName {
				sessionCookie = c
				break
			}
		}
		if sessionCookie == nil {
			t.Fatalf("expected session cookie %s to be set", auth.SessionCookieName)
		}
		if !sessionCookie.HttpOnly {
			t.Errorf("expected HttpOnly on session cookie")
		}

		// Verify user was persisted in DB
		var user models.User
		err = db.Where("issuer = ? AND subject = ?", mockAuth.IssuerURL(), "sub-12345").First(&user).Error
		if err != nil {
			t.Fatalf("expected user to be created in DB, got: %v", err)
		}
		if user.Subject != "sub-12345" {
			t.Errorf("expected subject sub-12345, got %s", user.Subject)
		}

		// Now test /api/v1/auth/me with the session cookie
		meReq := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
		meReq.AddCookie(sessionCookie)

		meResp, err := server.App.Test(meReq)
		if err != nil {
			t.Fatalf("me request failed: %v", err)
		}
		defer meResp.Body.Close()

		if meResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(meResp.Body)
			t.Fatalf("expected 200 OK for /me, got: %d, body: %s", meResp.StatusCode, string(body))
		}

		var meResult map[string]any
		_ = json.NewDecoder(meResp.Body).Decode(&meResult)
		userInfo, ok := meResult["user"].(map[string]any)
		if !ok || userInfo["subject"] != "sub-12345" {
			t.Errorf("unexpected me user payload: %+v", meResult)
		}

		// Test logout
		logoutReq := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
		logoutReq.AddCookie(sessionCookie)
		logoutReq.Header.Set("Sec-Fetch-Site", "same-origin")

		logoutResp, err := server.App.Test(logoutReq)
		if err != nil {
			t.Fatalf("logout request failed: %v", err)
		}
		defer logoutResp.Body.Close()

		if logoutResp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK for logout, got: %d", logoutResp.StatusCode)
		}

		var clearedCookie *http.Cookie
		for _, c := range logoutResp.Cookies() {
			if c.Name == auth.SessionCookieName {
				clearedCookie = c
				break
			}
		}
		if clearedCookie == nil || clearedCookie.MaxAge != -1 {
			t.Errorf("expected session cookie to be cleared on logout: %+v", clearedCookie)
		}
	})

	t.Run("GET /api/v1/auth/me fails when unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
		resp, err := server.App.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got: %d", resp.StatusCode)
		}
	})
}

func TestStructValidatorBindingInFiber(t *testing.T) {
	app := fiber.New(fiber.Config{
		StructValidator: api.NewStructValidator(),
	})

	type LoginPayload struct {
		Email string `json:"email" validate:"required,email"`
	}

	app.Post("/test-bind", func(c fiber.Ctx) error {
		var req LoginPayload
		if err := c.Bind().Body(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"email": req.Email})
	})

	// Valid payload
	reqValid := httptest.NewRequest("POST", "/test-bind", strings.NewReader(`{"email":"test@example.com"}`))
	reqValid.Header.Set("Content-Type", "application/json")
	respValid, err := app.Test(reqValid)
	if err != nil {
		t.Fatalf("test request failed: %v", err)
	}
	if respValid.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got: %d", respValid.StatusCode)
	}

	// Invalid payload (invalid email)
	reqInvalid := httptest.NewRequest("POST", "/test-bind", strings.NewReader(`{"email":"not-an-email"}`))
	reqInvalid.Header.Set("Content-Type", "application/json")
	respInvalid, err := app.Test(reqInvalid)
	if err != nil {
		t.Fatalf("test request failed: %v", err)
	}
	if respInvalid.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got: %d", respInvalid.StatusCode)
	}
}
