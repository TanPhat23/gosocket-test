package config_test

import (
	"os"
	"testing"

	"agent-task-manager/internal/config"
)

func TestConfigLoadDefaultsAndEnv(t *testing.T) {
	// Clear any environment variables that might interfere
	envKeys := []string{
		"PORT", "APP_ENV", "DATABASE_URL",
		"DATABASE_STATEMENT_TIMEOUT_SECONDS", "DB_STATEMENT_TIMEOUT_SECONDS",
		"DATABASE_CONNECT_TIMEOUT_SECONDS", "DB_CONNECT_TIMEOUT_SECONDS",
		"REDIS_ADDR", "REDIS_PASSWORD",
		"SECRET_ENCRYPTION_KEY", "SESSION_SECRET", "MAX_CONCURRENT_TASKS",
		"TASK_TIMEOUT_MINUTES", "HOST_WORKTREE_ROOT", "HOST_REPO_ROOT",
		"OIDC_ISSUER_URL", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "OIDC_REDIRECT_URL",
	}
	for _, k := range envKeys {
		os.Unsetenv(k)
	}

	// First verify defaults when env is unset
	defaultCfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected nil error on defaults, got: %v", err)
	}
	if defaultCfg.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", defaultCfg.Port)
	}
	if defaultCfg.DatabaseStatementTimeoutSeconds != 15 {
		t.Errorf("expected default DatabaseStatementTimeoutSeconds 15, got %d", defaultCfg.DatabaseStatementTimeoutSeconds)
	}
	if defaultCfg.DatabaseConnectTimeoutSeconds != 5 {
		t.Errorf("expected default DatabaseConnectTimeoutSeconds 5, got %d", defaultCfg.DatabaseConnectTimeoutSeconds)
	}
	if defaultCfg.MaxConcurrentTasks != 4 {
		t.Errorf("expected default max concurrent tasks 4, got %d", defaultCfg.MaxConcurrentTasks)
	}
	if defaultCfg.TaskTimeoutMinutes != 60 {
		t.Errorf("expected default task timeout minutes 60, got %d", defaultCfg.TaskTimeoutMinutes)
	}
	if defaultCfg.HostWorktreeRoot != "/srv/agent-tasks/worktrees" {
		t.Errorf("expected default worktree root /srv/agent-tasks/worktrees, got %s", defaultCfg.HostWorktreeRoot)
	}
	if defaultCfg.HostRepoRoot != "/srv/agent-tasks/repos" {
		t.Errorf("expected default repo root /srv/agent-tasks/repos, got %s", defaultCfg.HostRepoRoot)
	}
	if defaultCfg.RedisAddr != "localhost:6379" {
		t.Errorf("expected default redis addr localhost:6379, got %s", defaultCfg.RedisAddr)
	}
	if defaultCfg.AppEnv != "development" {
		t.Errorf("expected default AppEnv development, got %s", defaultCfg.AppEnv)
	}

	// Now set custom env vars
	os.Setenv("PORT", "8080")
	os.Setenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/agent_tasks?sslmode=disable")
	os.Setenv("DATABASE_STATEMENT_TIMEOUT_SECONDS", "30")
	os.Setenv("DATABASE_CONNECT_TIMEOUT_SECONDS", "10")
	os.Setenv("REDIS_ADDR", "localhost:6379")
	os.Setenv("REDIS_PASSWORD", "redispass123")
	os.Setenv("SECRET_ENCRYPTION_KEY", "01234567890123456789012345678901")
	os.Setenv("MAX_CONCURRENT_TASKS", "6")
	os.Setenv("HOST_WORKTREE_ROOT", "/srv/agent-tasks/worktrees")
	os.Setenv("OIDC_ISSUER_URL", "https://accounts.google.com")
	os.Setenv("OIDC_CLIENT_ID", "client-123")
	os.Setenv("OIDC_CLIENT_SECRET", "client-secret-xyz")
	os.Setenv("OIDC_REDIRECT_URL", "http://localhost:8080/api/v1/auth/callback")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.DatabaseStatementTimeoutSeconds != 30 {
		t.Errorf("expected DatabaseStatementTimeoutSeconds 30, got %d", cfg.DatabaseStatementTimeoutSeconds)
	}
	if cfg.DatabaseConnectTimeoutSeconds != 10 {
		t.Errorf("expected DatabaseConnectTimeoutSeconds 10, got %d", cfg.DatabaseConnectTimeoutSeconds)
	}
	if cfg.DatabaseURL != "postgres://postgres:postgres@localhost:5432/agent_tasks?sslmode=disable" {
		t.Errorf("expected database URL, got %s", cfg.DatabaseURL)
	}
	if cfg.RedisAddr != "localhost:6379" {
		t.Errorf("expected redis addr localhost:6379, got %s", cfg.RedisAddr)
	}
	if cfg.RedisPassword != "redispass123" {
		t.Errorf("expected redis password redispass123, got %s", cfg.RedisPassword)
	}
	if cfg.SecretEncryptionKey != "01234567890123456789012345678901" {
		t.Errorf("expected secret encryption key, got %s", cfg.SecretEncryptionKey)
	}
	if cfg.MaxConcurrentTasks != 6 {
		t.Errorf("expected max concurrent tasks 6, got %d", cfg.MaxConcurrentTasks)
	}
	if cfg.HostWorktreeRoot != "/srv/agent-tasks/worktrees" {
		t.Errorf("expected worktree root /srv/agent-tasks/worktrees, got %s", cfg.HostWorktreeRoot)
	}
	if cfg.OIDCIssuerURL != "https://accounts.google.com" {
		t.Errorf("expected OIDC issuer URL, got %s", cfg.OIDCIssuerURL)
	}
}

func TestConfigLoadInvalidValues(t *testing.T) {
	// Invalid PORT
	os.Setenv("PORT", "not-a-number")
	if _, err := config.Load(); err == nil {
		t.Errorf("expected error for invalid PORT, got nil")
	}
	os.Unsetenv("PORT")

	// Invalid MAX_CONCURRENT_TASKS
	os.Setenv("MAX_CONCURRENT_TASKS", "abc")
	if _, err := config.Load(); err == nil {
		t.Errorf("expected error for invalid MAX_CONCURRENT_TASKS, got nil")
	}
	os.Unsetenv("MAX_CONCURRENT_TASKS")

	// Invalid TASK_TIMEOUT_MINUTES
	os.Setenv("TASK_TIMEOUT_MINUTES", "xyz")
	if _, err := config.Load(); err == nil {
		t.Errorf("expected error for invalid TASK_TIMEOUT_MINUTES, got nil")
	}
	os.Unsetenv("TASK_TIMEOUT_MINUTES")

	// Invalid DATABASE_STATEMENT_TIMEOUT_SECONDS
	os.Setenv("DATABASE_STATEMENT_TIMEOUT_SECONDS", "invalid")
	if _, err := config.Load(); err == nil {
		t.Errorf("expected error for invalid DATABASE_STATEMENT_TIMEOUT_SECONDS, got nil")
	}
	os.Unsetenv("DATABASE_STATEMENT_TIMEOUT_SECONDS")

	// Invalid DATABASE_CONNECT_TIMEOUT_SECONDS
	os.Setenv("DATABASE_CONNECT_TIMEOUT_SECONDS", "invalid")
	if _, err := config.Load(); err == nil {
		t.Errorf("expected error for invalid DATABASE_CONNECT_TIMEOUT_SECONDS, got nil")
	}
	os.Unsetenv("DATABASE_CONNECT_TIMEOUT_SECONDS")

	// Alternate env var DB_STATEMENT_TIMEOUT_SECONDS and DB_CONNECT_TIMEOUT_SECONDS
	os.Setenv("DB_STATEMENT_TIMEOUT_SECONDS", "25")
	os.Setenv("DB_CONNECT_TIMEOUT_SECONDS", "8")
	cfgAlt, err := config.Load()
	if err != nil {
		t.Fatalf("expected nil error with DB_* timeout env vars, got: %v", err)
	}
	if cfgAlt.DatabaseStatementTimeoutSeconds != 25 {
		t.Errorf("expected DatabaseStatementTimeoutSeconds 25, got %d", cfgAlt.DatabaseStatementTimeoutSeconds)
	}
	if cfgAlt.DatabaseConnectTimeoutSeconds != 8 {
		t.Errorf("expected DatabaseConnectTimeoutSeconds 8, got %d", cfgAlt.DatabaseConnectTimeoutSeconds)
	}
	os.Unsetenv("DB_STATEMENT_TIMEOUT_SECONDS")
	os.Unsetenv("DB_CONNECT_TIMEOUT_SECONDS")
}
