package config_test

import (
	"os"
	"strings"
	"testing"
	"time"

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
		"ATM_PROJECT_RUNTIME_IDLE_TIMEOUT",
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
	if defaultCfg.ProjectRuntimeIdleTimeout != 15*time.Minute {
		t.Errorf("expected default ProjectRuntimeIdleTimeout 15m, got %v", defaultCfg.ProjectRuntimeIdleTimeout)
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
	os.Setenv("ATM_PROJECT_RUNTIME_IDLE_TIMEOUT", "45s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if cfg.ProjectRuntimeIdleTimeout != 45*time.Second {
		t.Errorf("expected ProjectRuntimeIdleTimeout 45s, got %v", cfg.ProjectRuntimeIdleTimeout)
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

func TestLoadProjectRuntimeIdleTimeout(t *testing.T) {
	cases := []struct {
		name      string
		val       string
		set       bool
		expected  time.Duration
		expectErr bool
	}{
		{
			name:      "default 15m when unset",
			set:       false,
			expected:  15 * time.Minute,
			expectErr: false,
		},
		{
			name:      "0s disables idle stop",
			set:       true,
			val:       "0s",
			expected:  0,
			expectErr: false,
		},
		{
			name:      "valid 45s",
			set:       true,
			val:       "45s",
			expected:  45 * time.Second,
			expectErr: false,
		},
		{
			name:      "valid 30s minimum boundary",
			set:       true,
			val:       "30s",
			expected:  30 * time.Second,
			expectErr: false,
		},
		{
			name:      "sub-30s rejected (29s)",
			set:       true,
			val:       "29s",
			expectErr: true,
		},
		{
			name:      "negative duration rejected (-1s)",
			set:       true,
			val:       "-1s",
			expectErr: true,
		},
		{
			name:      "malformed duration rejected",
			set:       true,
			val:       "not-a-duration",
			expectErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				os.Setenv("ATM_PROJECT_RUNTIME_IDLE_TIMEOUT", tc.val)
			} else {
				os.Unsetenv("ATM_PROJECT_RUNTIME_IDLE_TIMEOUT")
			}
			defer os.Unsetenv("ATM_PROJECT_RUNTIME_IDLE_TIMEOUT")

			cfg, err := config.Load()
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error for value %q, got nil", tc.val)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for value %q: %v", tc.val, err)
			}
			if cfg.ProjectRuntimeIdleTimeout != tc.expected {
				t.Errorf("expected ProjectRuntimeIdleTimeout %v, got %v", tc.expected, cfg.ProjectRuntimeIdleTimeout)
			}
		})
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

func TestNormalizeEncryptionKey(t *testing.T) {
	t.Run("valid 64-hex key decodes to exact 32 raw bytes and IsHexFormat true", func(t *testing.T) {
		hexStr := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		key, err := config.NormalizeEncryptionKey(hexStr)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !key.IsHexFormat {
			t.Errorf("expected IsHexFormat to be true")
		}
		expectedByte0 := byte(0x01)
		expectedByte31 := byte(0xef)
		if key.RawBytes[0] != expectedByte0 || key.RawBytes[31] != expectedByte31 {
			t.Errorf("unexpected decoded bytes")
		}
	})

	t.Run("valid 32-literal key converts to 32 raw bytes and IsHexFormat false", func(t *testing.T) {
		literalStr := "my-secret-key-32-bytes-long-1234"
		key, err := config.NormalizeEncryptionKey(literalStr)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key.IsHexFormat {
			t.Errorf("expected IsHexFormat to be false")
		}
		if string(key.RawBytes[:]) != literalStr {
			t.Errorf("expected %q, got %q", literalStr, string(key.RawBytes[:]))
		}
	})

	t.Run("empty key error", func(t *testing.T) {
		_, err := config.NormalizeEncryptionKey("")
		if err == nil {
			t.Fatal("expected error for empty key, got nil")
		}
		expectedMsg := "SECRET_ENCRYPTION_KEY is required and cannot be empty"
		if err.Error() != expectedMsg {
			t.Errorf("expected %q, got %q", expectedMsg, err.Error())
		}
	})

	t.Run("invalid length error does not leak key characters", func(t *testing.T) {
		secretKey := "super-secret-key"
		_, err := config.NormalizeEncryptionKey(secretKey)
		if err == nil {
			t.Fatal("expected error for length != 32 and != 64, got nil")
		}
		expectedMsg := "invalid SECRET_ENCRYPTION_KEY: must be either a 64-character hex string or exactly 32 bytes literal (got length 16)"
		if err.Error() != expectedMsg {
			t.Errorf("expected %q, got %q", expectedMsg, err.Error())
		}
	})

	t.Run("invalid hex in 64-character key", func(t *testing.T) {
		invalidHex := "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
		_, err := config.NormalizeEncryptionKey(invalidHex)
		if err == nil {
			t.Fatal("expected error for non-hex 64-character key, got nil")
		}
		expectedMsg := "invalid SECRET_ENCRYPTION_KEY: 64-character key contains invalid hex encoding"
		if err.Error() != expectedMsg {
			t.Errorf("expected %q, got %q", expectedMsg, err.Error())
		}
	})
}

func TestValidateProductionSecrets(t *testing.T) {
	validRaw := func() *config.RawEnvironmentConfig {
		return &config.RawEnvironmentConfig{
			AppEnv:              "production",
			DatabaseURL:         "postgres://user:strong-db-pass@localhost:5432/agent_tasks",
			DatabasePassword:    "strong-db-pass",
			RedisPassword:       "strong-redis-password-here",
			SecretEncryptionKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			SessionSecret:       "a-very-long-and-secure-session-secret-32-chars",
			OIDCClientSecret:    "secure-oidc-client-secret-value",
			OpenCodeAuthMode:    "legacy_static",
			OpenCodePassword:    "strong-opencode-password-value",
		}
	}

	t.Run("valid production config passes", func(t *testing.T) {
		raw := validRaw()
		if err := config.ValidateRawProductionConfig(raw); err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
	})

	t.Run("skipped in non-production unless PreflightStrict", func(t *testing.T) {
		raw := validRaw()
		raw.AppEnv = "development"
		raw.DatabasePassword = "password" // insecure, but non-prod
		if err := config.ValidateRawProductionConfig(raw); err != nil {
			t.Fatalf("expected nil error in development, got: %v", err)
		}

		raw.PreflightStrict = true
		if err := config.ValidateRawProductionConfig(raw); err == nil {
			t.Fatalf("expected error when PreflightStrict is true in development")
		}
	})

	t.Run("enforced via PREFLIGHT_STRICT env var", func(t *testing.T) {
		raw := validRaw()
		raw.AppEnv = "development"
		raw.DatabasePassword = "password"

		os.Setenv("PREFLIGHT_STRICT", "true")
		defer os.Unsetenv("PREFLIGHT_STRICT")

		if err := config.ValidateRawProductionConfig(raw); err == nil {
			t.Fatalf("expected error when PREFLIGHT_STRICT=true env var set")
		}
	})

	t.Run("weak encryption key rejection (all 6 patterns)", func(t *testing.T) {
		weakKeys := []string{
			"01234567890123456789012345678901", // literal ASCII 01234567890123456789012345678901
			"12345678901234567890123456789012", // literal ASCII 12345678901234567890123456789012
			"00000000000000000000000000000000", // literal ASCII 32 '0's
			"11111111111111111111111111111111", // literal ASCII 32 '1's
			"0000000000000000000000000000000000000000000000000000000000000000", // 64 hex '0's -> 32 zero bytes
			"1111111111111111111111111111111111111111111111111111111111111111", // 64 hex '1's -> 32 0x11 bytes
		}
		for _, wk := range weakKeys {
			raw := validRaw()
			raw.SecretEncryptionKey = wk
			err := config.ValidateRawProductionConfig(raw)
			if err == nil {
				t.Errorf("expected error for weak encryption key %q, got nil", wk)
			} else if err.Error() != "production preflight check failed: SECRET_ENCRYPTION_KEY uses an insecure default" {
				t.Errorf("unexpected error for weak key %q: %v", wk, err)
			}
		}
	})

	t.Run("banned postgres password rejection direct and DSN", func(t *testing.T) {
		bannedPass := []string{"postgres", "password", "admin", "root", "secret", ""}
		for _, bp := range bannedPass {
			// Direct DatabasePassword
			raw := validRaw()
			raw.DatabasePassword = bp
			raw.DatabaseURL = ""
			err := config.ValidateRawProductionConfig(raw)
			if err == nil {
				t.Errorf("expected error for banned postgres password %q, got nil", bp)
			} else if err.Error() != "production preflight check failed: database password uses an insecure default or is empty" {
				t.Errorf("unexpected error for direct pass %q: %v", bp, err)
			}

			// In DSN
			rawDSN := validRaw()
			rawDSN.DatabasePassword = ""
			if bp != "" {
				rawDSN.DatabaseURL = "postgres://postgres:" + bp + "@localhost:5432/agent_tasks"
			} else {
				rawDSN.DatabaseURL = "postgres://postgres@localhost:5432/agent_tasks"
			}
			errDSN := config.ValidateRawProductionConfig(rawDSN)
			if errDSN == nil {
				t.Errorf("expected error for banned postgres password in DSN %q, got nil", bp)
			} else if errDSN.Error() != "production preflight check failed: database password uses an insecure default or is empty" {
				t.Errorf("unexpected error for DSN pass %q: %v", bp, errDSN)
			}
		}
	})

	t.Run("banned redis password rejection", func(t *testing.T) {
		bannedRedis := []string{"secretredis", "redis", "password", "admin", ""}
		for _, br := range bannedRedis {
			raw := validRaw()
			raw.RedisPassword = br
			err := config.ValidateRawProductionConfig(raw)
			if err == nil {
				t.Errorf("expected error for banned redis password %q, got nil", br)
			} else if err.Error() != "production preflight check failed: REDIS_PASSWORD uses an insecure default or is empty" {
				t.Errorf("unexpected error for redis pass %q: %v", br, err)
			}
		}
	})

	t.Run("session secret rejection", func(t *testing.T) {
		shortOrDefault := []string{
			"dev-session-secret-change-in-prod-1234",
			"too-short",
			"",
			"1234567890123456789012345678901", // 31 chars
		}
		for _, s := range shortOrDefault {
			raw := validRaw()
			raw.SessionSecret = s
			err := config.ValidateRawProductionConfig(raw)
			if err == nil {
				t.Errorf("expected error for session secret %q, got nil", s)
			} else if err.Error() != "production preflight check failed: SESSION_SECRET uses an insecure default or is shorter than 32 characters" {
				t.Errorf("unexpected error for session secret %q: %v", s, err)
			}
		}
	})

	t.Run("banned OIDC client secret rejection", func(t *testing.T) {
		bannedOIDC := []string{"dev-oidc-secret-change-in-prod-1234", "secret", ""}
		for _, oidc := range bannedOIDC {
			raw := validRaw()
			raw.OIDCClientSecret = oidc
			err := config.ValidateRawProductionConfig(raw)
			if err == nil {
				t.Errorf("expected error for OIDC client secret %q, got nil", oidc)
			} else if err.Error() != "production preflight check failed: OIDC_CLIENT_SECRET uses an insecure default or is empty" {
				t.Errorf("unexpected error for OIDC secret %q: %v", oidc, err)
			}
		}
	})

	t.Run("banned OpenCode password rejection in legacy_static mode", func(t *testing.T) {
		bannedOpenCode := []string{"4YXQ3udRl8k_UkFdyhHSFF-bdInQgKk0F9Qz6zp2y0U", "opencode", ""}
		for _, op := range bannedOpenCode {
			raw := validRaw()
			raw.OpenCodeAuthMode = "legacy_static"
			raw.OpenCodePassword = op
			err := config.ValidateRawProductionConfig(raw)
			if err == nil {
				t.Errorf("expected error for OpenCode password %q, got nil", op)
			} else if err.Error() != "production preflight check failed: OPENCODE_PASSWORD uses an insecure default or is empty in legacy static auth mode" {
				t.Errorf("unexpected error for OpenCode pass %q: %v", op, err)
			}
		}

		// When not legacy_static, it should pass
		rawNonLegacy := validRaw()
		rawNonLegacy.OpenCodeAuthMode = "gate_0g_dynamic"
		rawNonLegacy.OpenCodePassword = ""
		if err := config.ValidateRawProductionConfig(rawNonLegacy); err != nil {
			t.Errorf("expected no error for non-legacy mode, got: %v", err)
		}
	})
}

func TestConfigToRaw(t *testing.T) {
	cfg := &config.Config{
		AppEnv:              "production",
		DatabaseURL:         "postgres://user:pass@localhost:5432/db",
		RedisPassword:       "redis-pass",
		SecretEncryptionKey: "key-123",
		SessionSecret:       "sess-123",
		OIDCClientSecret:    "oidc-secret",
		OpenCodePassword:    "opencode-pass",
	}
	raw := cfg.ToRaw()
	if raw.AppEnv != cfg.AppEnv ||
		raw.DatabaseURL != cfg.DatabaseURL ||
		raw.RedisPassword != cfg.RedisPassword ||
		raw.SecretEncryptionKey != cfg.SecretEncryptionKey ||
		raw.SessionSecret != cfg.SessionSecret ||
		raw.OIDCClientSecret != cfg.OIDCClientSecret ||
		raw.OpenCodePassword != cfg.OpenCodePassword ||
		raw.OpenCodeAuthMode != "legacy_static" {
		t.Errorf("unexpected raw config values: %+v", raw)
	}
}

func TestConfig_DevHostAuth_Validation(t *testing.T) {
	// 1. When APP_ENV=production and ATM_DEV_HOST_AUTH=true, config.Load() fails
	t.Run("rejected in production", func(t *testing.T) {
		os.Setenv("APP_ENV", "production")
		os.Setenv("ATM_DEV_HOST_AUTH", "true")
		defer os.Unsetenv("APP_ENV")
		defer os.Unsetenv("ATM_DEV_HOST_AUTH")

		cfg, err := config.Load()
		if err == nil {
			t.Fatalf("expected error when ATM_DEV_HOST_AUTH=true in production, got nil (cfg=%+v)", cfg)
		}
		expectedErrSubstr := "only permitted when APP_ENV=development"
		if !strings.Contains(err.Error(), expectedErrSubstr) {
			t.Fatalf("expected error to contain %q, got %q", expectedErrSubstr, err.Error())
		}
	})

	// 2. When APP_ENV=development and ATM_DEV_HOST_AUTH=true, config.Load() succeeds and cfg.DevHostAuth == true
	t.Run("permitted in development", func(t *testing.T) {
		os.Setenv("APP_ENV", "development")
		os.Setenv("ATM_DEV_HOST_AUTH", "true")
		defer os.Unsetenv("APP_ENV")
		defer os.Unsetenv("ATM_DEV_HOST_AUTH")

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("expected nil error when ATM_DEV_HOST_AUTH=true in development, got: %v", err)
		}
		if !cfg.DevHostAuth {
			t.Errorf("expected cfg.DevHostAuth to be true, got %v", cfg.DevHostAuth)
		}
	})
}

