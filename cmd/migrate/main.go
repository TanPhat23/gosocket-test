package main

import (
	"flag"
	"fmt"
	"log"

	"agent-task-manager/internal/config"
	"agent-task-manager/internal/db"
	"agent-task-manager/internal/models"
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

	fmt.Println("Database automigrations completed successfully.")
}
