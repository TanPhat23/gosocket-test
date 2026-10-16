package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/db"
	"agent-task-manager/internal/models"

	"gorm.io/gorm"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	rollback := flag.Bool("rollback", false, "Drop all application tables (caution: destructive)")
	flag.Parse()

	gormDB, err := db.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	modelList := []any{
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
	}

	if *rollback {
		fmt.Println("Rolling back database schema (dropping tables)...")
		if err := gormDB.Migrator().DropTable(modelList...); err != nil {
			log.Fatalf("failed to drop tables: %v", err)
		}
		fmt.Println("Database tables dropped successfully.")
		return
	}

	fmt.Println("Running database automigrations...")
	if err := gormDB.AutoMigrate(modelList...); err != nil {
		log.Fatalf("failed to run database automigrations: %v", err)
	}

	if shouldSeed(cfg.AppEnv) {
		seedDefaults(gormDB, cfg.HostRepoRoot)
	} else {
		fmt.Println("Skipping default data seeding in production")
	}

	fmt.Println("Database automigrations completed successfully.")
}

// shouldSeed reports whether the development defaults (a default team, project and the admin@example.com
// owner) may be created. They must never exist in production.
func shouldSeed(appEnv string) bool {
	return appEnv != "production"
}

// seedDefaults creates the default team, project and admin owner when no team exists yet.
func seedDefaults(db *gorm.DB, hostRepoRoot string) {
	var teamCount int64
	db.Model(&models.Team{}).Count(&teamCount)
	if teamCount == 0 {
		teamID := common.NewID()
		projectID := common.NewID()
		defaultTeam := &models.Team{
			ID:   teamID,
			Name: "Core Team",
			Slug: "core-team",
		}
		if err := db.Create(defaultTeam).Error; err != nil {
			log.Printf("warning: failed to seed default team: %v", err)
		} else {
			defaultProject := &models.Project{
				ID:       projectID,
				TeamID:   teamID,
				Name:     "Core Workspace",
				RepoPath: filepath.Join(hostRepoRoot, "core"),
			}
			if err := db.Create(defaultProject).Error; err != nil {
				log.Printf("warning: failed to seed default project: %v", err)
			} else {
				fmt.Printf("Seeded default team (%s) and project (%s)\n", teamID, projectID)
			}

			// Seed admin user and membership
			adminUserID := common.NewID()
			adminUser := &models.User{
				ID:          adminUserID,
				Issuer:      "http://localhost/dex",
				Subject:     "CiQwOGE1MmM2My00NDRhLTRkNTYtOTgzMy00MTUyYmIwZDQ0YWMSBWxvY2Fs",
				Email:       "admin@example.com",
				DisplayName: "admin",
			}
			if err := db.Create(adminUser).Error; err == nil {
				_ = db.Create(&models.TeamMembership{
					TeamID: teamID,
					UserID: adminUserID,
					Role:   models.RoleOwner,
				}).Error
				fmt.Printf("Seeded default admin user (%s) as Team Owner\n", adminUser.Email)
			}
		}
	}
}
