package auth_test

import (
	"testing"
	"time"

	"agent-task-manager/internal/auth"
)

func TestPKCEAndStateGeneration(t *testing.T) {
	// 1. Test random string generation
	s1, err := auth.GenerateRandomString(32)
	if err != nil {
		t.Fatalf("failed to generate random string: %v", err)
	}
	s2, err := auth.GenerateRandomString(32)
	if err != nil {
		t.Fatalf("failed to generate random string: %v", err)
	}
	if s1 == s2 {
		t.Fatalf("expected unique random strings, got identical values: %s", s1)
	}

	// 2. Test PKCE S256 code challenge
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := auth.GenerateCodeChallenge(verifier)
	if challenge == "" || challenge == verifier {
		t.Fatalf("invalid code challenge: %s", challenge)
	}

	// S256 challenge for known verifier
	// SHA256 of "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk" in base64url is "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" (RFC 7636 Appendix B)
	expectedRFC := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if challenge != expectedRFC {
		t.Errorf("expected RFC 7636 challenge %s, got %s", expectedRFC, challenge)
	}
}

func TestOIDCStateSigningAndVerification(t *testing.T) {
	cfg := auth.OIDCConfig{
		IssuerURL:    "https://idp.example.com",
		ClientID:     "client-id-123",
		ClientSecret: "client-secret-456",
		RedirectURL:  "http://localhost:8080/api/v1/auth/callback",
		CookieSecret: []byte("super-secret-oidc-cookie-key-32b"),
	}

	state := &auth.OIDCState{
		State:        "state-abc",
		Nonce:        "nonce-def",
		CodeVerifier: "verifier-ghi",
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}

	// Test signing with SessionManager (HMAC verification)
	sm := auth.NewSessionManager(cfg.CookieSecret, 10*time.Minute, false)
	signed, err := sm.SignPayload(auth.SessionClaims{
		IssuedAt: time.Now(),
		UserID:    state.State,
		Issuer:    state.Nonce,
		Subject:   state.CodeVerifier,
		ExpiresAt: state.ExpiresAt,
	})
	if err != nil {
		t.Fatalf("failed to sign payload: %v", err)
	}

	claims, err := sm.VerifyPayload(signed)
	if err != nil {
		t.Fatalf("expected valid state, got err: %v", err)
	}
	if claims.UserID != state.State || claims.Issuer != state.Nonce || claims.Subject != state.CodeVerifier {
		t.Errorf("state payload mismatch: %+v vs %+v", claims, state)
	}
}
