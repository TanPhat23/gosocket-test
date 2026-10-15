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

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func setupMiddlewareTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}

	tables := []string{
		`CREATE TABLE users (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			issuer TEXT NOT NULL,
			subject TEXT NOT NULL,
			email TEXT,
			display_name TEXT,
			sessions_valid_after DATETIME
		);`,
		`CREATE TABLE teams (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			name TEXT NOT NULL,
			slug TEXT NOT NULL
		);`,
		`CREATE TABLE team_memberships (
			team_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			role TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (team_id, user_id)
		);`,
		`CREATE TABLE api_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			team_id TEXT NOT NULL,
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			expires_at DATETIME,
			last_used_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, stmt := range tables {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to execute schema stmt %s: %v", stmt, err)
		}
	}

	return db
}

func TestRequireAuthMiddleware(t *testing.T) {
	db := setupMiddlewareTestDB(t)
	sessionSecret := []byte("secret-key-32-bytes-long-for-test!")
	sm := auth.NewSessionManager(sessionSecret, 24*time.Hour, false)

	// Seed user
	user := models.User{
		ID:          "usr_123",
		Issuer:      "https://idp.example.com",
		Subject:     "sub_123",
		Email:       "usr@example.com",
		DisplayName: "Test User",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	// Seed team
	team := models.Team{
		ID:   "team_123",
		Name: "Test Team",
		Slug: "test-team",
	}
	if err := db.Create(&team).Error; err != nil {
		t.Fatalf("failed to seed team: %v", err)
	}

	// Seed PAT
	rawToken, tokenHash, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	apiToken := models.APIToken{
		ID:        "tok_123",
		UserID:    user.ID,
		TeamID:    team.ID,
		Name:      "CI token",
		TokenHash: tokenHash,
	}
	if err := db.Create(&apiToken).Error; err != nil {
		t.Fatalf("failed to seed token: %v", err)
	}

	// Seed expired PAT
	expiredRawToken, expiredHash, _ := auth.GenerateAPIToken()
	past := time.Now().Add(-1 * time.Hour)
	expiredAPIToken := models.APIToken{
		ID:        "tok_expired",
		UserID:    user.ID,
		TeamID:    team.ID,
		Name:      "Expired token",
		TokenHash: expiredHash,
		ExpiresAt: &past,
	}
	if err := db.Create(&expiredAPIToken).Error; err != nil {
		t.Fatalf("failed to seed expired token: %v", err)
	}

	// Setup Fiber App
	userRepo := repository.NewUserRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	tokenRepo := repository.NewTokenRepository(db)
	authSvc := service.NewAuthService(userRepo, teamRepo, tokenRepo, nil)

	app := fiber.New()
	app.Get("/protected", api.RequireAuth(authSvc, sm), func(c fiber.Ctx) error {
		u := api.CurrentUser(c)
		token := api.CurrentAPIToken(c)
		return c.JSON(fiber.Map{
			"user_id":   u.ID,
			"has_token": token != nil,
		})
	})

	t.Run("Fails with 401 when no auth provided", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("Authenticates with valid session cookie", func(t *testing.T) {
		claims := auth.SessionClaims{
			UserID:    user.ID,
			Issuer:    user.Issuer,
			Subject:   user.Subject,
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		signedToken, err := sm.SignPayload(claims)
		if err != nil {
			t.Fatalf("failed to sign payload: %v", err)
		}

		req := httptest.NewRequest("GET", "/protected", nil)
		req.AddCookie(&http.Cookie{
			Name:  auth.SessionCookieName,
			Value: signedToken,
		})

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body["user_id"] != user.ID || body["has_token"] != false {
			t.Errorf("unexpected body: %+v", body)
		}
	})

	t.Run("Authenticates with valid Bearer PAT", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body["user_id"] != user.ID || body["has_token"] != true {
			t.Errorf("unexpected body: %+v", body)
		}

		// Verify LastUsedAt was updated
		var updatedToken models.APIToken
		db.First(&updatedToken, "id = ?", apiToken.ID)
		if updatedToken.LastUsedAt == nil {
			t.Errorf("expected LastUsedAt to be updated")
		}
	})

	t.Run("Fails with 401 on expired Bearer PAT", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+expiredRawToken)

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("Fails with 401 on malformed or invalid Bearer PAT", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer invalid-token-without-prefix")

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})
}

func TestRequireTeamRoleMiddleware(t *testing.T) {
	db := setupMiddlewareTestDB(t)
	sessionSecret := []byte("secret-key-32-bytes-long-for-test!")
	sm := auth.NewSessionManager(sessionSecret, 24*time.Hour, false)

	// Seed users
	owner := models.User{ID: "usr_owner", Issuer: "https://idp.example.com", Subject: "owner"}
	admin := models.User{ID: "usr_admin", Issuer: "https://idp.example.com", Subject: "admin"}
	member := models.User{ID: "usr_member", Issuer: "https://idp.example.com", Subject: "member"}
	viewer := models.User{ID: "usr_viewer", Issuer: "https://idp.example.com", Subject: "viewer"}
	outsider := models.User{ID: "usr_outside", Issuer: "https://idp.example.com", Subject: "outsider"}

	for _, u := range []models.User{owner, admin, member, viewer, outsider} {
		db.Create(&u)
	}

	team := models.Team{ID: "team_target", Name: "Target Team", Slug: "target-team"}
	db.Create(&team)

	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: owner.ID, Role: models.RoleOwner})
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: admin.ID, Role: models.RoleAdmin})
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: member.ID, Role: models.RoleMember})
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: viewer.ID, Role: models.RoleViewer})

	userRepo := repository.NewUserRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	tokenRepo := repository.NewTokenRepository(db)
	authSvc := service.NewAuthService(userRepo, teamRepo, tokenRepo, nil)

	app := fiber.New()
	app.Get("/teams/:team_id/admin-only",
		api.RequireAuth(authSvc, sm),
		api.RequireTeamRole(authSvc, models.RoleAdmin),
		func(c fiber.Ctx) error {
			role := api.CurrentTeamRole(c)
			return c.JSON(fiber.Map{"role": role})
		},
	)

	makeReq := func(userID string) *http.Response {
		claims := auth.SessionClaims{
			UserID:    userID,
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		tok, _ := sm.SignPayload(claims)
		req := httptest.NewRequest("GET", "/teams/"+team.ID+"/admin-only", nil)
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
		resp, _ := app.Test(req)
		return resp
	}

	t.Run("Owner is allowed (status 200)", func(t *testing.T) {
		resp := makeReq(owner.ID)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Admin is allowed (status 200)", func(t *testing.T) {
		resp := makeReq(admin.ID)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Member is forbidden (status 403)", func(t *testing.T) {
		resp := makeReq(member.ID)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("Viewer is forbidden (status 403)", func(t *testing.T) {
		resp := makeReq(viewer.ID)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("Outsider is forbidden (status 403)", func(t *testing.T) {
		resp := makeReq(outsider.ID)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d", resp.StatusCode)
		}
	})
}
