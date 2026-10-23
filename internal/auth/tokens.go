package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const TokenPrefix = "atm_pat_"

// GenerateAPIToken generates a new Personal Access Token with the format:
// atm_pat_<32_random_bytes_hex>
// It returns the raw token string and its SHA-256 hex digest.
func GenerateAPIToken() (rawToken string, tokenHash string, err error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	rawToken = TokenPrefix + hex.EncodeToString(randomBytes)
	tokenHash = HashToken(rawToken)
	return rawToken, tokenHash, nil
}

// HashToken computes the SHA-256 hex digest of a raw token string.
func HashToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}
