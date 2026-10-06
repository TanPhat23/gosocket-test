package db

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"agent-task-manager/internal/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// BuildDatabaseDSN enriches a database connection string or URL with statement_timeout and connect_timeout
// if they are not already set.
// statementTimeoutSec is converted to milliseconds for PostgreSQL / pgx statement_timeout parameter.
// connectTimeoutSec is set in seconds.
func BuildDatabaseDSN(rawURL string, statementTimeoutSec, connectTimeoutSec int) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", errors.New("empty database connection string")
	}

	// PostgreSQL connection URI schemes
	if strings.HasPrefix(trimmed, "postgres://") || strings.HasPrefix(trimmed, "postgresql://") {
		parsedURL, err := url.Parse(trimmed)
		if err != nil {
			return "", fmt.Errorf("failed to parse postgres URL: %w", err)
		}

		q := parsedURL.Query()
		if statementTimeoutSec > 0 && !q.Has("statement_timeout") {
			q.Set("statement_timeout", strconv.Itoa(statementTimeoutSec*1000))
		}
		if connectTimeoutSec > 0 && !q.Has("connect_timeout") {
			q.Set("connect_timeout", strconv.Itoa(connectTimeoutSec))
		}
		parsedURL.RawQuery = q.Encode()
		return parsedURL.String(), nil
	}

	// Key-value DSN style (e.g. "host=localhost user=postgres dbname=app ...")
	fields := strings.Fields(trimmed)
	hasStmtTimeout := false
	hasConnTimeout := false

	for _, field := range fields {
		if strings.HasPrefix(field, "statement_timeout=") {
			hasStmtTimeout = true
		} else if strings.HasPrefix(field, "connect_timeout=") {
			hasConnTimeout = true
		}
	}

	res := trimmed
	if statementTimeoutSec > 0 && !hasStmtTimeout {
		res += fmt.Sprintf(" statement_timeout=%d", statementTimeoutSec*1000)
	}
	if connectTimeoutSec > 0 && !hasConnTimeout {
		res += fmt.Sprintf(" connect_timeout=%d", connectTimeoutSec)
	}

	return res, nil
}

// Connect initializes and returns a GORM database connection pool using config.Config.
func Connect(cfg *config.Config) (*gorm.DB, error) {
	if cfg == nil {
		return nil, errors.New("config cannot be nil")
	}
	if cfg.DatabaseURL == "" {
		return nil, errors.New("database URL is required")
	}

	enrichedDSN, err := BuildDatabaseDSN(cfg.DatabaseURL, cfg.DatabaseStatementTimeoutSeconds, cfg.DatabaseConnectTimeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to build database DSN: %w", err)
	}

	gormConfig := &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Warn),
		SkipDefaultTransaction: false, // Ensures all create/update operations run within explicit transactions
	}

	db, err := gorm.Open(postgres.Open(enrichedDSN), gormConfig)
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
