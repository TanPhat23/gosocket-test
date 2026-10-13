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
	sm := auth.NewSessionManager([]byte("secret-key-32-bytes-long-for-test!"), 24*time.Hour, false)

	user := models.User{ID: "usr_opt", Issuer: "iss", Subject: "sub_opt"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	team := models.Team{ID: "team_opt", Name: "T", Slug: "t-opt"}
	if err := db.Create(&team).Error; err != nil {
		t.Fatal(err)
	}
	rawToken, hash, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.APIToken{ID: "tok_opt", UserID: user.ID, TeamID: team.ID, Name: "n", TokenHash: hash}).Error; err != nil {
		t.Fatal(err)
	}

	authSvc := service.NewAuthService(repository.NewUserRepository(db), repository.NewTeamRepository(db), repository.NewTokenRepository(db), nil)
	app := fiber.New()
	app.Get("/opt", api.OptionalAuth(authSvc, sm), func(c fiber.Ctx) error {
		u := api.CurrentUser(c)
		id := ""
		if u != nil {
			id = u.ID
		}
		return c.JSON(fiber.Map{"user_id": id, "has_token": api.CurrentAPIToken(c) != nil})
	})

	do := func(t *testing.T, mutate func(*http.Request)) map[string]any {
		t.Helper()
		req := httptest.NewRequest("GET", "/opt", nil)
		mutate(req)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return body
	}

	t.Run("anonymous continues without user", func(t *testing.T) {
		b := do(t, func(*http.Request) {})
		if b["user_id"] != "" || b["has_token"] != false {
			t.Errorf("unexpected %+v", b)
		}
	})
	t.Run("session cookie sets user only", func(t *testing.T) {
		signed, err := sm.SignPayload(auth.SessionClaims{UserID: user.ID, Issuer: "iss", Subject: "sub_opt", ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		b := do(t, func(r *http.Request) { r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: signed}) })
		if b["user_id"] != user.ID || b["has_token"] != false {
			t.Errorf("unexpected %+v", b)
		}
	})
	t.Run("bearer sets user and token", func(t *testing.T) {
		b := do(t, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+rawToken) })
		if b["user_id"] != user.ID || b["has_token"] != true {
			t.Errorf("unexpected %+v", b)
		}
	})
	t.Run("invalid bearer continues anonymous", func(t *testing.T) {
		b := do(t, func(r *http.Request) { r.Header.Set("Authorization", "Bearer bogus") })
		if b["user_id"] != "" {
			t.Errorf("unexpected %+v", b)
		}
	})
	t.Run("nil session manager falls through to bearer", func(t *testing.T) {
		app2 := fiber.New()
		app2.Get("/x", api.OptionalAuth(authSvc, nil), func(c fiber.Ctx) error {
			return c.SendString(map[bool]string{true: "user", false: "anon"}[api.CurrentUser(c) != nil])
		})
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		resp, err := app2.Test(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("err=%v", err)
		}
	})
}
