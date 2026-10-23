package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
)

func TestAuthService_LoginWithCredentials(t *testing.T) {
	db := setupTestDB(t)
	userRepo := repository.NewUserRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	tokenRepo := repository.NewTokenRepository(db)
	mockAuth := &mockAuthenticator{
		issuerURL: "http://localhost/dex",
	}

	authSvc := service.NewAuthService(userRepo, teamRepo, tokenRepo, mockAuth)
	ctx := context.Background()

	t.Run("valid admin login creates or retrieves user", func(t *testing.T) {
		user, err := authSvc.LoginWithCredentials(ctx, "admin@example.com", "password123")
		if err != nil {
			t.Fatalf("expected login to succeed, got error: %v", err)
		}
		if user == nil {
			t.Fatalf("expected non-nil user")
		}
		if user.Email != "admin@example.com" {
			t.Errorf("expected email admin@example.com, got %s", user.Email)
		}
		if user.DisplayName != "admin" {
			t.Errorf("expected display name admin, got %s", user.DisplayName)
		}
		if user.Subject != "CiQwOGE1MmM2My00NDRhLTRkNTYtOTgzMy00MTUyYmIwZDQ0YWMSBWxvY2Fs" {
			t.Errorf("expected admin subject, got %s", user.Subject)
		}
		if user.Issuer != "http://localhost/dex" {
			t.Errorf("expected issuer http://localhost/dex, got %s", user.Issuer)
		}

		// Subsequent login returns existing user without duplicating
		secondUser, err := authSvc.LoginWithCredentials(ctx, "admin@example.com", "password123")
		if err != nil {
			t.Fatalf("expected second login to succeed, got: %v", err)
		}
		if secondUser.ID != user.ID {
			t.Errorf("expected same user ID %s, got %s", user.ID, secondUser.ID)
		}
	})

	t.Run("valid developer login", func(t *testing.T) {
		user, err := authSvc.LoginWithCredentials(ctx, "developer@example.com", "password123")
		if err != nil {
			t.Fatalf("expected login to succeed, got error: %v", err)
		}
		if user.Email != "developer@example.com" {
			t.Errorf("expected email developer@example.com, got %s", user.Email)
		}
		if user.DisplayName != "developer" {
			t.Errorf("expected display name developer, got %s", user.DisplayName)
		}
		if user.Subject != "CiQxOGE1MmM2My00NDRhLTRkNTYtOTgzMy00MTUyYmIwZDQ0YWQSBWxvY2Fs" {
			t.Errorf("expected developer subject, got %s", user.Subject)
		}
	})

	t.Run("invalid password", func(t *testing.T) {
		user, err := authSvc.LoginWithCredentials(ctx, "admin@example.com", "wrongpassword")
		if !errors.Is(err, service.ErrInvalidCredentials) {
			t.Fatalf("expected ErrInvalidCredentials, got user=%v, err=%v", user, err)
		}
	})

	t.Run("unknown email", func(t *testing.T) {
		user, err := authSvc.LoginWithCredentials(ctx, "nonexistent@example.com", "password123")
		if !errors.Is(err, service.ErrInvalidCredentials) {
			t.Fatalf("expected ErrInvalidCredentials, got user=%v, err=%v", user, err)
		}
	})
}

// A local credential login must never grant team membership, neither when it creates the user nor
// on later logins of the existing user.
func TestAuthService_LoginWithCredentials_GrantsNoMembership(t *testing.T) {
	db := setupTestDB(t)
	teamRepo := repository.NewTeamRepository(db)
	authSvc := service.NewAuthService(repository.NewUserRepository(db), teamRepo,
		repository.NewTokenRepository(db), &mockAuthenticator{issuerURL: "http://localhost/dex"})
	ctx := context.Background()

	for _, team := range []models.Team{
		{ID: common.NewID(), Name: "Alpha", Slug: "alpha"},
		{ID: common.NewID(), Name: "Beta", Slug: "beta"},
	} {
		if err := db.Create(&team).Error; err != nil {
			t.Fatalf("create team: %v", err)
		}
	}

	for i := range 2 {
		user, err := authSvc.LoginWithCredentials(ctx, "admin@example.com", "password123")
		if err != nil {
			t.Fatalf("login %d: %v", i+1, err)
		}
		var n int64
		if err := db.Model(&models.TeamMembership{}).Where("user_id = ?", user.ID).Count(&n).Error; err != nil {
			t.Fatalf("count memberships: %v", err)
		}
		if n != 0 {
			t.Fatalf("login %d granted %d team memberships, want 0", i+1, n)
		}
	}
}

// A missing membership is a plain denial; a failing membership lookup is not "not a member" and must
// surface as an error (fail closed, but reported as a server fault rather than a 403).
func TestAuthService_AuthorizeTeamRole_DistinguishesDeniedFromFailure(t *testing.T) {
	db := setupTestDB(t)
	authSvc := service.NewAuthService(repository.NewUserRepository(db), repository.NewTeamRepository(db),
		repository.NewTokenRepository(db), &mockAuthenticator{issuerURL: "http://localhost/dex"})

	if _, err := authSvc.AuthorizeTeamRole(context.Background(), "team-x", "user-x", models.RoleViewer); !errors.Is(err, service.ErrNotTeamMember) {
		t.Fatalf("no membership row: want ErrNotTeamMember, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := authSvc.AuthorizeTeamRole(ctx, "team-x", "user-x", models.RoleViewer)
	if err == nil {
		t.Fatal("failing lookup must not authorize")
	}
	if errors.Is(err, service.ErrNotTeamMember) || errors.Is(err, service.ErrForbidden) {
		t.Fatalf("lookup failure must not be reported as a denial, got %v", err)
	}
}

// PAT authentication records last use, but a socket re-authenticating every 30s must not turn that
// into a database write per tick: a recent LastUsedAt is left alone, a stale or absent one is refreshed.
func TestAuthService_AuthenticatePAT_ThrottlesLastUsedWrites(t *testing.T) {
	db := setupTestDB(t)
	authSvc := service.NewAuthService(repository.NewUserRepository(db), repository.NewTeamRepository(db),
		repository.NewTokenRepository(db), &mockAuthenticator{issuerURL: "http://localhost/dex"})
	ctx := context.Background()

	user := models.User{ID: common.NewID(), Subject: "sub-pat", Email: "pat@example.com", DisplayName: "pat"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	raw, hash, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	tok := models.APIToken{ID: "tok-throttle", UserID: user.ID, TeamID: "team-1", Name: "t", TokenHash: hash}
	if err := db.Create(&tok).Error; err != nil {
		t.Fatal(err)
	}
	stored := func() *time.Time {
		var got models.APIToken
		if err := db.First(&got, "id = ?", tok.ID).Error; err != nil {
			t.Fatal(err)
		}
		return got.LastUsedAt
	}

	if _, _, err := authSvc.AuthenticatePAT(ctx, raw); err != nil {
		t.Fatal(err)
	}
	first := stored()
	if first == nil {
		t.Fatal("first use must record LastUsedAt")
	}

	recent := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	db.Model(&models.APIToken{}).Where("id = ?", tok.ID).Update("last_used_at", recent)
	if _, _, err := authSvc.AuthenticatePAT(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if got := stored(); got == nil || !got.Equal(recent) {
		t.Fatalf("recent LastUsedAt must not be rewritten, got %v want %v", got, recent)
	}

	stale := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	db.Model(&models.APIToken{}).Where("id = ?", tok.ID).Update("last_used_at", stale)
	if _, _, err := authSvc.AuthenticatePAT(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if got := stored(); got == nil || !got.After(stale.Add(time.Minute)) {
		t.Fatalf("stale LastUsedAt must be refreshed, got %v", got)
	}
}
