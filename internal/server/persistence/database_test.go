package persistence

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
)

// newSeedTestDB opens an isolated in-memory database with the two tables the seed step writes.
func newSeedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", common.NewID())
	gormDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	err = gormDB.Exec(`
		CREATE TABLE teams (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			name TEXT NOT NULL,
			slug TEXT NOT NULL UNIQUE
		);
		CREATE TABLE projects (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			team_id TEXT NOT NULL,
			name TEXT NOT NULL,
			repo_path TEXT NOT NULL,
			runtime_config TEXT NOT NULL DEFAULT '{}'
		);
	`).Error
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}
	return gormDB
}

func TestSeedDefaultsCreatesTheDefaultWorkspaceOnlyOnce(t *testing.T) {
	gormDB := newSeedTestDB(t)

	seedDefaults(gormDB, "/srv/repos")
	seedDefaults(gormDB, "/srv/repos")

	var teams, projects int64
	gormDB.Model(&models.Team{}).Count(&teams)
	gormDB.Model(&models.Project{}).Count(&projects)
	if teams != 1 || projects != 1 {
		t.Fatalf("expected one team and one project after two starts, got %d and %d", teams, projects)
	}
	var project models.Project
	gormDB.First(&project)
	if project.RepoPath != "/srv/repos/core" {
		t.Errorf("unexpected default repo path %q", project.RepoPath)
	}
}
