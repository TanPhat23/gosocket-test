package config

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// RawEnvironmentConfig captures the initial unmarshaled environment inputs.
type RawEnvironmentConfig struct {
	AppEnv              string
	PreflightStrict     bool
	DatabaseURL         string
	DatabasePassword    string
	RedisPassword       string
	SecretEncryptionKey string
	SessionSecret       string
	OIDCClientSecret    string
	OpenCodeAuthMode    string
	OpenCodePassword    string
}

// MasterEncryptionKey holds a validated and normalized 256-bit AES master key.
type MasterEncryptionKey struct {
	RawBytes    [32]byte
	IsHexFormat bool
}

// ProductionHardeningConfig governs preflight boundaries and secret validation.
type ProductionHardeningConfig struct {
	PreflightStrict  bool
	RequireTLS       bool
	DisallowDefaults bool
}

// ProposedConfig represents the modernized target application configuration contract.
type ProposedConfig struct {
	AppEnv           string
	Port             int
	Hardening        ProductionHardeningConfig
	MasterKey        MasterEncryptionKey
	SessionSecret    string
	DatabaseURL      string
	DatabasePassword string
	RedisAddr        string
	RedisPassword    string
	Domain           string
	OIDCClientID     string
	OIDCClientSecret string
	OpenCodeAuthMode string
	OpenCodePassword string
}

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
	DevHostAuth       bool
	OpenCodeDataDir   string

	// Project runtime settings
	ProjectRuntimeIdleTimeout time.Duration
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

	cfg.DevHostAuth = getEnv("ATM_DEV_HOST_AUTH", "false") == "true"
	cfg.OpenCodeDataDir = getEnv("OPENCODE_DATA_DIR", "/home/ghug/.local/share/opencode")

	if cfg.DevHostAuth && cfg.AppEnv != "development" {
		return nil, fmt.Errorf("ATM_DEV_HOST_AUTH=true is only permitted when APP_ENV=development (current: %s)", cfg.AppEnv)
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

	idleTimeout, err := time.ParseDuration(getEnv("ATM_PROJECT_RUNTIME_IDLE_TIMEOUT", "15m"))
	if err != nil || idleTimeout < 0 || (idleTimeout > 0 && idleTimeout < 30*time.Second) {
		return nil, fmt.Errorf("invalid ATM_PROJECT_RUNTIME_IDLE_TIMEOUT: expected 0s or at least 30s")
	}
	cfg.ProjectRuntimeIdleTimeout = idleTimeout

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

// ToRaw converts Config to RawEnvironmentConfig.
func (c *Config) ToRaw() *RawEnvironmentConfig {
	return &RawEnvironmentConfig{
		AppEnv:              c.AppEnv,
		PreflightStrict:     os.Getenv("PREFLIGHT_STRICT") == "true",
		DatabaseURL:         c.DatabaseURL,
		RedisPassword:       c.RedisPassword,
		SecretEncryptionKey: c.SecretEncryptionKey,
		SessionSecret:       c.SessionSecret,
		OIDCClientSecret:    c.OIDCClientSecret,
		OpenCodeAuthMode:    "legacy_static",
		OpenCodePassword:    c.OpenCodePassword,
	}
}

// NormalizeEncryptionKey decodes and normalizes a 64-hex or 32-literal character encryption key.
func NormalizeEncryptionKey(keyStr string) (*MasterEncryptionKey, error) {
	if keyStr == "" {
		return nil, errors.New("SECRET_ENCRYPTION_KEY is required and cannot be empty")
	}

	key := &MasterEncryptionKey{}
	if len(keyStr) == 64 {
		decoded, err := hex.DecodeString(keyStr)
		if err != nil {
			return nil, errors.New("invalid SECRET_ENCRYPTION_KEY: 64-character key contains invalid hex encoding")
		}
		copy(key.RawBytes[:], decoded)
		key.IsHexFormat = true
		return key, nil
	} else if len(keyStr) == 32 {
		copy(key.RawBytes[:], []byte(keyStr))
		key.IsHexFormat = false
		return key, nil
	}

	return nil, fmt.Errorf("invalid SECRET_ENCRYPTION_KEY: must be either a 64-character hex string or exactly 32 bytes literal (got length %d)", len(keyStr))
}

// ValidateRawProductionConfig performs fail-closed startup validation on raw inputs.
func ValidateRawProductionConfig(raw *RawEnvironmentConfig) error {
	isProduction := raw.AppEnv == "production"
	isPreflightStrict := raw.PreflightStrict || os.Getenv("PREFLIGHT_STRICT") == "true"
	if !isProduction && !isPreflightStrict {
		return nil
	}

	// Extract database password from raw.DatabasePassword or raw.DatabaseURL
	dbPass := raw.DatabasePassword
	if dbPass == "" && raw.DatabaseURL != "" {
		if u, err := url.Parse(raw.DatabaseURL); err == nil && u.User != nil {
			dbPass, _ = u.User.Password()
		}
	}

	insecurePostgres := map[string]bool{
		"postgres": true,
		"password": true,
		"admin":    true,
		"root":     true,
		"secret":   true,
		"":         true,
	}
	if insecurePostgres[dbPass] {
		return errors.New("production preflight check failed: database password uses an insecure default or is empty")
	}

	insecureRedis := map[string]bool{
		"secretredis": true,
		"redis":       true,
		"password":    true,
		"admin":       true,
		"":            true,
	}
	if insecureRedis[raw.RedisPassword] {
		return errors.New("production preflight check failed: REDIS_PASSWORD uses an insecure default or is empty")
	}

	masterKey, err := NormalizeEncryptionKey(raw.SecretEncryptionKey)
	if err != nil {
		return err
	}

	insecureHexKeys := map[string]bool{
		"3031323334353637383930313233343536373839303132333435363738393031": true, // literal ASCII "01234567890123456789012345678901"
		"3132333435363738393031323334353637383930313233343536373839303132": true, // literal ASCII "12345678901234567890123456789012"
		"3030303030303030303030303030303030303030303030303030303030303030": true, // literal ASCII 32 '0's
		"3131313131313131313131313131313131313131313131313131313131313131": true, // literal ASCII 32 '1's
		"0000000000000000000000000000000000000000000000000000000000000000": true, // 32 zero bytes
		"1111111111111111111111111111111111111111111111111111111111111111": true, // 32 0x11 bytes
	}
	if insecureHexKeys[hex.EncodeToString(masterKey.RawBytes[:])] {
		return errors.New("production preflight check failed: SECRET_ENCRYPTION_KEY uses an insecure default")
	}

	if raw.SessionSecret == "dev-session-secret-change-in-prod-1234" || len(raw.SessionSecret) < 32 {
		return errors.New("production preflight check failed: SESSION_SECRET uses an insecure default or is shorter than 32 characters")
	}

	insecureOIDC := map[string]bool{
		"dev-oidc-secret-change-in-prod-1234": true,
		"secret":                              true,
		"":                                    true,
	}
	if insecureOIDC[raw.OIDCClientSecret] {
		return errors.New("production preflight check failed: OIDC_CLIENT_SECRET uses an insecure default or is empty")
	}

	if raw.OpenCodeAuthMode == "legacy_static" {
		insecureOpenCode := map[string]bool{
			"4YXQ3udRl8k_UkFdyhHSFF-bdInQgKk0F9Qz6zp2y0U": true,
			"opencode":                                   true,
			"":                                           true,
		}
		if insecureOpenCode[raw.OpenCodePassword] {
			return errors.New("production preflight check failed: OPENCODE_PASSWORD uses an insecure default or is empty in legacy static auth mode")
		}
	}

	return nil
}
