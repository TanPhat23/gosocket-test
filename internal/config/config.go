package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Config represents the application configuration.
type Config struct {
	// Server settings
	Port   int
	AppEnv string

	// Database settings
	DatabaseURL                     string
	DatabaseStatementTimeoutSeconds int
	DatabaseConnectTimeoutSeconds   int

	// Redis settings
	RedisAddr     string
	RedisPassword string

	// Secrets & Encryption
	SecretEncryptionKey string
	SessionSecret       string

	// Worker / Task Queue settings
	MaxConcurrentTasks int
	TaskTimeoutMinutes int

	// Pre-flight Verification & Self-Repair settings
	MaxVerificationRetries     int
	VerificationTimeoutSeconds int

	// Host path mappings for Docker-out-of-Docker
	HostWorktreeRoot string
	HostRepoRoot     string

	// OIDC authentication settings
	OIDCIssuerURL         string
	OIDCInternalIssuerURL string
	OIDCClientID          string
	OIDCClientSecret      string
	OIDCRedirectURL       string

	// OpenCode integration settings
	OpenCodeServerURL string
	OpenCodePassword  string
	OpenCodeBinPath   string
	OpenCodeHomePath  string
	OpenCodeConfigDir string
}

// Load loads the configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                            8080,
		AppEnv:                          getEnv("APP_ENV", "development"),
		DatabaseURL:                     os.Getenv("DATABASE_URL"),
		DatabaseStatementTimeoutSeconds: 15,
		DatabaseConnectTimeoutSeconds:   5,
		RedisAddr:                       getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:                   os.Getenv("REDIS_PASSWORD"),
		SecretEncryptionKey:             os.Getenv("SECRET_ENCRYPTION_KEY"),
		SessionSecret:                   os.Getenv("SESSION_SECRET"),
		MaxConcurrentTasks:              4,
		TaskTimeoutMinutes:              60,
		MaxVerificationRetries:          3,
		VerificationTimeoutSeconds:      120,
		HostWorktreeRoot:                getEnv("HOST_WORKTREE_ROOT", "/srv/agent-tasks/worktrees"),
		HostRepoRoot:                    getEnv("HOST_REPO_ROOT", "/srv/agent-tasks/repos"),
		OIDCIssuerURL:                   os.Getenv("OIDC_ISSUER_URL"),
		OIDCInternalIssuerURL:           os.Getenv("OIDC_INTERNAL_ISSUER_URL"),
		OIDCClientID:                    os.Getenv("OIDC_CLIENT_ID"),
		OIDCClientSecret:                os.Getenv("OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:                 os.Getenv("OIDC_REDIRECT_URL"),
		OpenCodeServerURL:               getEnv("OPENCODE_SERVER_URL", "http://127.0.0.1:49374"),
		OpenCodePassword:                os.Getenv("OPENCODE_PASSWORD"),
		OpenCodeBinPath:                 getEnv("OPENCODE_BIN_PATH", "/home/ghug/.opencode/bin/opencode"),
		OpenCodeHomePath:                getEnv("OPENCODE_HOME_PATH", "/home/ghug/.opencode"),
		OpenCodeConfigDir:               getEnv("OPENCODE_CONFIG_DIR", "/home/ghug/.config/opencode"),
	}

	if cfg.OpenCodePassword == "" {
		serviceJSON := filepath.Join(cfg.OpenCodeConfigDir, "service.json")
		if data, err := os.ReadFile(serviceJSON); err == nil {
			var s struct {
				Password string `json:"password"`
			}
			if json.Unmarshal(data, &s) == nil && s.Password != "" {
				cfg.OpenCodePassword = s.Password
			}
		}
	}

	if stmtTimeoutStr := getEnvFirst("DATABASE_STATEMENT_TIMEOUT_SECONDS", "DB_STATEMENT_TIMEOUT_SECONDS"); stmtTimeoutStr != "" {
		stmtTimeout, err := strconv.Atoi(stmtTimeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid DATABASE_STATEMENT_TIMEOUT_SECONDS value %q: %w", stmtTimeoutStr, err)
		}
		cfg.DatabaseStatementTimeoutSeconds = stmtTimeout
	}

	if connTimeoutStr := getEnvFirst("DATABASE_CONNECT_TIMEOUT_SECONDS", "DB_CONNECT_TIMEOUT_SECONDS"); connTimeoutStr != "" {
		connTimeout, err := strconv.Atoi(connTimeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid DATABASE_CONNECT_TIMEOUT_SECONDS value %q: %w", connTimeoutStr, err)
		}
		cfg.DatabaseConnectTimeoutSeconds = connTimeout
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

	if retriesStr := os.Getenv("MAX_VERIFICATION_RETRIES"); retriesStr != "" {
		retries, err := strconv.Atoi(retriesStr)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_VERIFICATION_RETRIES value %q: %w", retriesStr, err)
		}
		cfg.MaxVerificationRetries = retries
	}

	if verifyTimeoutStr := os.Getenv("VERIFICATION_TIMEOUT_SECONDS"); verifyTimeoutStr != "" {
		vTimeout, err := strconv.Atoi(verifyTimeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid VERIFICATION_TIMEOUT_SECONDS value %q: %w", verifyTimeoutStr, err)
		}
		cfg.VerificationTimeoutSeconds = vTimeout
	}

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvFirst(keys ...string) string {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			return val
		}
	}
	return ""
}
