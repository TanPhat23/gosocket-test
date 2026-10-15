package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
)

// loginTestConfig replaces app.Test's 1s default. Each login runs a real bcrypt comparison
// (cost 10), which the race detector slows past 1s; the bound still fails a hung handler.
var loginTestConfig = fiber.TestConfig{Timeout: 30 * time.Second, FailOnTimeout: true}

func TestAuthHandlers_LoginWithCredentials(t *testing.T) {
	db := setupTestDB(t)
	cfg := &config.Config{
		AppEnv:        "development",
		SessionSecret: "test-session-secret-32-bytes-long!",
	}

	mockAuth := &mockAuthenticator{
		issuerURL: "http://localhost/dex",
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

	t.Setenv("ATM_ENABLE_LOCAL_LOGIN", "true")
	server := api.NewServer(cfg, sessionManager, authService, credService, webhookService)

	t.Run("POST /api/v1/auth/login success with admin credentials", func(t *testing.T) {
		reqBody, _ := json.Marshal(dto.LoginRequestDTO{
			Email:    "admin@example.com",
			Password: "password123",
		})
		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")

		resp, err := server.App.Test(req, loginTestConfig)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 OK, got: %d, body: %s", resp.StatusCode, string(body))
		}

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

		var respData struct {
			Status string              `json:"status"`
			User   dto.UserResponseDTO `json:"user"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if respData.Status != "authenticated" {
			t.Errorf("expected status 'authenticated', got '%s'", respData.Status)
		}
		if respData.User.Email != "admin@example.com" {
			t.Errorf("expected email 'admin@example.com', got '%s'", respData.User.Email)
		}
		if respData.User.DisplayName != "admin" {
			t.Errorf("expected displayName 'admin', got '%s'", respData.User.DisplayName)
		}
		if respData.User.Subject != "CiQwOGE1MmM2My00NDRhLTRkNTYtOTgzMy00MTUyYmIwZDQ0YWMSBWxvY2Fs" {
			t.Errorf("unexpected subject '%s'", respData.User.Subject)
		}
	})

	t.Run("POST /api/v1/auth/login invalid credentials returns 401", func(t *testing.T) {
		reqBody, _ := json.Marshal(dto.LoginRequestDTO{
			Email:    "admin@example.com",
			Password: "wrongpassword",
		})
		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")

		resp, err := server.App.Test(req, loginTestConfig)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got: %d", resp.StatusCode)
		}

		var errData map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&errData)
		if errData["error"] != "Invalid email or password" {
			t.Errorf("expected error 'Invalid email or password', got '%s'", errData["error"])
		}
	})

	t.Run("POST /api/v1/auth/login invalid request payload returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader([]byte(`invalid-json`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")

		resp, err := server.App.Test(req, loginTestConfig)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got: %d", resp.StatusCode)
		}
	})
}

// newLocalLoginServer builds a Server for appEnv after the caller has set ATM_ENABLE_LOCAL_LOGIN.
func newLocalLoginServer(t *testing.T, appEnv string) (*api.Server, func() int64) {
	t.Helper()
	db := setupTestDB(t)
	cfg := &config.Config{AppEnv: appEnv, SessionSecret: "test-session-secret-32-bytes-long!"}
	sm := auth.NewSessionManager([]byte(cfg.SessionSecret), 24*time.Hour, false)
	authService := service.NewAuthService(repository.NewUserRepository(db), repository.NewTeamRepository(db),
		repository.NewTokenRepository(db), &mockAuthenticator{issuerURL: "http://localhost/dex"})
	countUsers := func() int64 {
		var n int64
		db.Table("users").Count(&n)
		return n
	}
	return api.NewServer(cfg, sm, authService, nil, nil), countUsers
}

func postLogin(t *testing.T, server *api.Server, email, password string) (*http.Response, string) {
	t.Helper()
	reqBody, _ := json.Marshal(dto.LoginRequestDTO{Email: email, Password: password})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := server.App.Test(req, loginTestConfig)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

// Local credential login is off unless ATM_ENABLE_LOCAL_LOGIN=true: valid built-in credentials then
// take the same 401 invalid-credentials path as a wrong password, set no session and create no user.
func TestAuthHandlers_LocalLoginDisabledByDefault(t *testing.T) {
	for _, tc := range []struct{ name, appEnv, flag string }{
		{"development unset", "development", ""},
		{"development false", "development", "false"},
		{"production unset", "production", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ATM_ENABLE_LOCAL_LOGIN", tc.flag)
			server, countUsers := newLocalLoginServer(t, tc.appEnv)
			resp, body := postLogin(t, server, "admin@example.com", "password123")
			if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "Invalid email or password") {
				t.Fatalf("expected 401 invalid credentials, got %d %s", resp.StatusCode, body)
			}
			for _, c := range resp.Cookies() {
				if c.Name == auth.SessionCookieName && c.Value != "" {
					t.Fatalf("disabled local login set a session cookie")
				}
			}
			if n := countUsers(); n != 0 {
				t.Fatalf("disabled local login created %d users", n)
			}
		})
	}
}

// Enabling local login in production is a startup error: NewServer refuses to build the server.
func TestNewServer_PanicsOnLocalLoginInProduction(t *testing.T) {
	t.Setenv("ATM_ENABLE_LOCAL_LOGIN", "true")
	defer func() {
		if recover() == nil {
			t.Fatal("expected NewServer to panic for ATM_ENABLE_LOCAL_LOGIN=true in production")
		}
	}()
	newLocalLoginServer(t, "production")
}
