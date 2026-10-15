package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

	server := api.NewServer(cfg, sessionManager, authService, credService, webhookService)

	t.Run("POST /api/v1/auth/login success with admin credentials", func(t *testing.T) {
		reqBody, _ := json.Marshal(dto.LoginRequestDTO{
			Email:    "admin@example.com",
			Password: "password123",
		})
		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

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
