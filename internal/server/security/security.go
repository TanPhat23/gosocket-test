// Package security builds the secret cipher and the identity provider the control plane authenticates users with.
package security

import (
	"context"
	"fmt"
	"log"
	"time"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/crypto"
)

// NewSecretCipher builds the AES-GCM cipher that protects stored credentials.
func NewSecretCipher(cfg *config.Config) (*crypto.SecretCipher, error) {
	masterKey, err := config.NormalizeEncryptionKey(cfg.SecretEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize encryption key: %w", err)
	}
	cipher, err := crypto.NewGCMCipher(masterKey.RawBytes[:])
	if err != nil {
		return nil, fmt.Errorf("failed to initialize secret cipher: %w", err)
	}
	return cipher, nil
}

// NewAuthenticator returns the OIDC authenticator when OIDC is configured. The identity provider may still be
// starting, so initialization is retried for up to ten seconds. Without OIDC, or if it never comes up, it returns nil.
func NewAuthenticator(cfg *config.Config) auth.Authenticator {
	if cfg.OIDCIssuerURL == "" || cfg.OIDCClientID == "" {
		return nil
	}
	oidcCfg := auth.OIDCConfig{
		IssuerURL:         cfg.OIDCIssuerURL,
		InternalIssuerURL: cfg.OIDCInternalIssuerURL,
		ClientID:          cfg.OIDCClientID,
		ClientSecret:      cfg.OIDCClientSecret,
		RedirectURL:       cfg.OIDCRedirectURL,
		CookieSecret:      []byte(cfg.SessionSecret),
		SecureCookie:      cfg.AppEnv == "production",
	}
	const attempts = 10
	for attempt := 1; attempt <= attempts; attempt++ {
		oidcClient, err := auth.NewOIDCClient(context.Background(), oidcCfg)
		if err == nil {
			log.Printf("OIDC client successfully initialized with issuer: %s", cfg.OIDCIssuerURL)
			return oidcClient
		}
		if attempt == attempts {
			log.Printf("warning: failed to initialize OIDC client after %d attempts: %v", attempts, err)
			break
		}
		log.Printf("OIDC initialization attempt %d waiting for provider (%v), retrying in 1s...", attempt, err)
		time.Sleep(1 * time.Second)
	}
	return nil
}
