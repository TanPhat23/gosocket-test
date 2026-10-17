package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// SecurityConfig holds browser-facing security settings for the API server.
type SecurityConfig struct {
	// AllowedOrigins lists additional browser origins (lowercase "scheme://host[:port]") trusted for
	// WebSocket handshakes and cookie-authenticated mutations. The API's own host is always trusted.
	AllowedOrigins []string
	// EnableLocalLogin enables the built-in development credential login. Never true in production.
	EnableLocalLogin bool
	// OperatorEmails lists the (lowercase) account emails allowed to change site-wide runtime settings such as the
	// LLM gateway provider key. Empty means nobody can.
	OperatorEmails []string
}

// LoadSecurityConfig reads ATM_ALLOWED_ORIGINS (comma-separated origins) and ATM_ENABLE_LOCAL_LOGIN.
// It fails on any malformed origin and when local login is requested in production, so
// misconfiguration is loud instead of silently widening or narrowing access.
func LoadSecurityConfig(appEnv string) (SecurityConfig, error) {
	var cfg SecurityConfig

	for _, entry := range strings.Split(os.Getenv("ATM_ALLOWED_ORIGINS"), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		origin, err := normalizeOrigin(entry)
		if err != nil {
			return SecurityConfig{}, fmt.Errorf("invalid ATM_ALLOWED_ORIGINS entry %q: %w", entry, err)
		}
		cfg.AllowedOrigins = append(cfg.AllowedOrigins, origin)
	}

	if raw := strings.TrimSpace(os.Getenv("ATM_ENABLE_LOCAL_LOGIN")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return SecurityConfig{}, fmt.Errorf("invalid ATM_ENABLE_LOCAL_LOGIN value %q: %w", raw, err)
		}
		if enabled && appEnv == "production" {
			return SecurityConfig{}, errors.New("ATM_ENABLE_LOCAL_LOGIN=true is not allowed when APP_ENV=production")
		}
		cfg.EnableLocalLogin = enabled
	}

	for _, e := range strings.Split(os.Getenv("ATM_OPERATOR_EMAILS"), ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			cfg.OperatorEmails = append(cfg.OperatorEmails, e)
		}
	}

	return cfg, nil
}

// normalizeOrigin accepts only an absolute http(s) "scheme://host[:port]" with no path, query,
// fragment or userinfo, and returns it lowercased.
func normalizeOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme != "http" && scheme != "https":
		return "", errors.New("scheme must be http or https")
	case u.Host == "" || u.Hostname() == "":
		return "", errors.New("host is required")
	case u.User != nil:
		return "", errors.New("userinfo is not allowed")
	case u.Path != "" || u.RawPath != "":
		return "", errors.New("path is not allowed")
	case u.RawQuery != "" || u.ForceQuery:
		return "", errors.New("query is not allowed")
	case u.Fragment != "" || strings.Contains(raw, "#"):
		return "", errors.New("fragment is not allowed")
	case u.Opaque != "":
		return "", errors.New("origin must be absolute")
	}
	return scheme + "://" + strings.ToLower(u.Host), nil
}
