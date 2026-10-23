// Package persistence opens the database, prepares its schema and default data, and bundles the repositories.
package persistence

import (
	"fmt"
	"log"
	"path/filepath"

	"gorm.io/gorm"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/db"
	"agent-task-manager/internal/models"
)

// Open connects, migrates the schema, and seeds the default team and project on first start.
func Open(cfg *config.Config) (*gorm.DB, error) {
	gormDB, err := db.Connect(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	_ = gormDB.AutoMigrate(
		&models.Team{},
		&models.User{},
		&models.TeamMembership{},
		&models.APIToken{},
		&models.Project{},
		&models.ProjectIntegration{},
		&models.Credential{},
		&models.Task{},
		&models.TaskAttempt{},
		&models.WebhookEvent{},
		&models.TaskLog{},
	)
	seedDefaults(gormDB, cfg.HostRepoRoot)
	return gormDB, nil
}

// seedDefaults creates the default team and its workspace when no team exists yet. It does nothing on later starts.
func seedDefaults(gormDB *gorm.DB, hostRepoRoot string) {
	var teamCount int64
	gormDB.Model(&models.Team{}).Count(&teamCount)
	if teamCount != 0 {
		return
	}
	teamID := common.NewID()
	projectID := common.NewID()
	defaultTeam := &models.Team{
		ID:   teamID,
		Name: "Core Team",
		Slug: "core-team",
	}
	if err := gormDB.Create(defaultTeam).Error; err != nil {
		return
	}
	defaultProject := &models.Project{
		ID:       projectID,
		TeamID:   teamID,
		Name:     "Core Workspace",
		RepoPath: filepath.Join(hostRepoRoot, "core"),
	}
	if err := gormDB.Create(defaultProject).Error; err != nil {
		log.Printf("warning: failed to seed default project: %v", err)
		return
	}
	log.Printf("Seeded default team (%s) and project (%s)", teamID, projectID)
}
