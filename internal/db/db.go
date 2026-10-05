package db

import (
	"errors"
	"fmt"
	"time"

	"agent-task-manager/internal/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect initializes and returns a GORM database connection pool using config.Config.
func Connect(cfg *config.Config) (*gorm.DB, error) {
	if cfg == nil {
		return nil, errors.New("config cannot be nil")
	}
	if cfg.DatabaseURL == "" {
		return nil, errors.New("database URL is required")
	}

	gormConfig := &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Warn),
		SkipDefaultTransaction: false, // Ensures all create/update operations run within explicit transactions
	}

	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to open gorm connection: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to extract sql.DB: %w", err)
	}

	// PostgreSQL 18 connection pool settings
	maxOpen := 25
	maxIdle := 5
	connMaxLifetime := 30 * time.Minute
	connMaxIdleTime := 5 * time.Minute

	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)
	sqlDB.SetConnMaxIdleTime(connMaxIdleTime)

	return db, nil
}
