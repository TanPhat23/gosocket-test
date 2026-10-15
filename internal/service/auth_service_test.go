package service_test

import (
	"context"
	"errors"
	"testing"

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
