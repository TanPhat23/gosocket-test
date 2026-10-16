package main

import (
	"testing"

	"agent-task-manager/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestShouldSeed(t *testing.T) {
	for env, want := range map[string]bool{
		"development": true,
		"test":        true,
		"":            true,
		"production":  false,
	} {
		if got := shouldSeed(env); got != want {
			t.Errorf("shouldSeed(%q) = %v, want %v", env, got, want)
		}
	}
}

func seedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE teams (id TEXT PRIMARY KEY, created_at DATETIME, updated_at DATETIME, name TEXT NOT NULL, slug TEXT NOT NULL)`,
		`CREATE TABLE projects (id TEXT PRIMARY KEY, created_at DATETIME, updated_at DATETIME, team_id TEXT NOT NULL, name TEXT NOT NULL, repo_path TEXT NOT NULL, runtime_config TEXT NOT NULL DEFAULT '{}')`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, created_at DATETIME, updated_at DATETIME, issuer TEXT NOT NULL, subject TEXT NOT NULL, email TEXT, display_name TEXT, sessions_valid_after DATETIME)`,
		`CREATE TABLE team_memberships (team_id TEXT, user_id TEXT, created_at DATETIME, updated_at DATETIME, role INTEGER, PRIMARY KEY (team_id, user_id))`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return db
}

func count(t *testing.T, db *gorm.DB, model any) int64 {
	t.Helper()
	var n int64
	if err := db.Model(model).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestSeedDefaults_EmptyDatabase(t *testing.T) {
	db := seedTestDB(t)
	seedDefaults(db, "/repos")

	if n := count(t, db, &models.Team{}); n != 1 {
		t.Errorf("teams = %d, want 1", n)
	}
	if n := count(t, db, &models.Project{}); n != 1 {
		t.Errorf("projects = %d, want 1", n)
	}
	var admin models.User
	if err := db.First(&admin, "email = ?", "admin@example.com").Error; err != nil {
		t.Fatalf("admin user not seeded: %v", err)
	}
	var m models.TeamMembership
	if err := db.First(&m, "user_id = ?", admin.ID).Error; err != nil || m.Role != models.RoleOwner {
		t.Errorf("admin membership = %+v, err %v; want RoleOwner", m, err)
	}
}

func TestSeedDefaults_SkipsWhenTeamsExist(t *testing.T) {
	db := seedTestDB(t)
	seedDefaults(db, "/repos")
	seedDefaults(db, "/repos")

	if n := count(t, db, &models.Team{}); n != 1 {
		t.Errorf("teams = %d after second run, want 1", n)
	}
	if n := count(t, db, &models.User{}); n != 1 {
		t.Errorf("users = %d after second run, want 1", n)
	}
}
