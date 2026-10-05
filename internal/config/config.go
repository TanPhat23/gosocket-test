package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config represents the application configuration.
type Config struct {
	// Server settings
	Port   int
	AppEnv string

	// Database settings
	DatabaseURL string

	// Redis settings
	RedisAddr     string
	RedisPassword string

	// Secrets & Encryption
	SecretEncryptionKey string
	SessionSecret       string

	// Worker / Task Queue settings
	MaxConcurrentTasks int
	TaskTimeoutMinutes int

	// Host path mappings for Docker-out-of-Docker
	HostWorktreeRoot string
	HostRepoRoot     string

	// OIDC authentication settings
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
}

// Load loads the configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                8080,
		AppEnv:              getEnv("APP_ENV", "development"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		RedisAddr:           getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:       os.Getenv("REDIS_PASSWORD"),
		SecretEncryptionKey: os.Getenv("SECRET_ENCRYPTION_KEY"),
		SessionSecret:       os.Getenv("SESSION_SECRET"),
		MaxConcurrentTasks:  4,
		TaskTimeoutMinutes:  60,
		HostWorktreeRoot:    getEnv("HOST_WORKTREE_ROOT", "/srv/agent-tasks/worktrees"),
		HostRepoRoot:        getEnv("HOST_REPO_ROOT", "/srv/agent-tasks/repos"),
		OIDCIssuerURL:       os.Getenv("OIDC_ISSUER_URL"),
		OIDCClientID:        os.Getenv("OIDC_CLIENT_ID"),
		OIDCClientSecret:    os.Getenv("OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:     os.Getenv("OIDC_REDIRECT_URL"),
	}

	if portStr := os.Getenv("PORT"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid PORT value %q: %w", portStr, err)
		}
		cfg.Port = port
	}

	if maxTasksStr := os.Getenv("MAX_CONCURRENT_TASKS"); maxTasksStr != "" {
		maxTasks, err := strconv.Atoi(maxTasksStr)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_CONCURRENT_TASKS value %q: %w", maxTasksStr, err)
		}
		cfg.MaxConcurrentTasks = maxTasks
	}

	if timeoutStr := os.Getenv("TASK_TIMEOUT_MINUTES"); timeoutStr != "" {
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TASK_TIMEOUT_MINUTES value %q: %w", timeoutStr, err)
		}
		cfg.TaskTimeoutMinutes = timeout
	}

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
