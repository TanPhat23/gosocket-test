package auth_test

import (
	"strings"
	"testing"

	"agent-task-manager/internal/auth"
)

func TestTokenGenerationAndHashing(t *testing.T) {
	rawToken, hash, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("expected nil error generating token: %v", err)
	}

	if !strings.HasPrefix(rawToken, "atm_pat_") {
		t.Errorf("expected token to start with 'atm_pat_', got %s", rawToken)
	}

	// atm_pat_ (8 bytes) + 32 random bytes hex (64 chars) = 72 chars
	if len(rawToken) != 72 {
		t.Errorf("expected token length 72, got %d (%s)", len(rawToken), rawToken)
	}

	expectedHash := auth.HashToken(rawToken)
	if hash != expectedHash {
		t.Errorf("expected hash %s, got %s", expectedHash, hash)
	}

	// Verify SHA-256 hex string length
	if len(hash) != 64 {
		t.Errorf("expected SHA-256 hex digest length 64, got %d", len(hash))
	}

	// Different tokens should have different values
	token2, hash2, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("expected nil error on second generation: %v", err)
	}
	if rawToken == token2 {
		t.Errorf("generated tokens should be distinct")
	}
	if hash == hash2 {
		t.Errorf("generated hashes should be distinct")
	}
}
