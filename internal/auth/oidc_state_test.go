package auth_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"agent-task-manager/internal/auth"
)

// newOIDCTestClient builds an OIDCClient against an in-process discovery endpoint so no network
// access is needed. The provider's issuer is the httptest server URL.
func newOIDCTestClient(t *testing.T, cookieSecret []byte) (*auth.OIDCClient, string) {
	t.Helper()

	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srvURL,
			"authorization_endpoint":                srvURL + "/authorize",
			"token_endpoint":                        srvURL + "/token",
			"jwks_uri":                              srvURL + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	srvURL = srv.URL

	client, err := auth.NewOIDCClient(context.Background(), auth.OIDCConfig{
		IssuerURL:    srv.URL,
		ClientID:     "client-id-test",
		ClientSecret: "client-secret-test",
		RedirectURL:  "http://localhost:8080/api/v1/auth/callback",
		CookieSecret: cookieSecret,
	})
	if err != nil {
		t.Fatalf("NewOIDCClient: %v", err)
	}
	return client, srv.URL
}

var oidcStateTestSecret = []byte("oidc-state-test-secret-0123456789abcdef")

// signedStateFixture returns a freshly generated state and its signed token.
func signedStateFixture(t *testing.T, client *auth.OIDCClient) (*auth.OIDCState, string) {
	t.Helper()
	state, _, err := client.GenerateOIDCState()
	if err != nil {
		t.Fatalf("GenerateOIDCState: %v", err)
	}
	signed, err := client.SignOIDCState(state)
	if err != nil {
		t.Fatalf("SignOIDCState: %v", err)
	}
	return state, signed
}

func TestOIDCStateRoundTrip(t *testing.T) {
	client, _ := newOIDCTestClient(t, oidcStateTestSecret)
	state, signed := signedStateFixture(t, client)

	got, err := client.VerifyOIDCState(signed)
	if err != nil {
		t.Fatalf("VerifyOIDCState on freshly signed state: %v", err)
	}
	if got.State != state.State || got.Nonce != state.Nonce || got.CodeVerifier != state.CodeVerifier {
		t.Errorf("round trip mismatch: got %+v, want %+v", got, state)
	}
	if !got.ExpiresAt.Equal(state.ExpiresAt) {
		t.Errorf("ExpiresAt round trip mismatch: got %v, want %v", got.ExpiresAt, state.ExpiresAt)
	}
}

func TestGenerateOIDCStateExpiryWindow(t *testing.T) {
	client, _ := newOIDCTestClient(t, oidcStateTestSecret)

	before := time.Now()
	state, _, err := client.GenerateOIDCState()
	if err != nil {
		t.Fatalf("GenerateOIDCState: %v", err)
	}
	after := time.Now()

	// ExpiresAt must be StateTTL after generation, bracketed by the wall clock around the call.
	if state.ExpiresAt.Before(before.Add(auth.StateTTL)) || state.ExpiresAt.After(after.Add(auth.StateTTL)) {
		t.Errorf("ExpiresAt %v not within [%v, %v]", state.ExpiresAt, before.Add(auth.StateTTL), after.Add(auth.StateTTL))
	}
}

func TestGenerateOIDCStateProducesFreshValues(t *testing.T) {
	client, _ := newOIDCTestClient(t, oidcStateTestSecret)

	s1, c1, err := client.GenerateOIDCState()
	if err != nil {
		t.Fatalf("GenerateOIDCState #1: %v", err)
	}
	s2, c2, err := client.GenerateOIDCState()
	if err != nil {
		t.Fatalf("GenerateOIDCState #2: %v", err)
	}

	// 32 random bytes encode to 43 unpadded base64url characters.
	for name, v := range map[string]string{"state": s1.State, "nonce": s1.Nonce, "verifier": s1.CodeVerifier} {
		if len(v) != 43 {
			t.Errorf("%s length = %d, want 43", name, len(v))
		}
	}

	if s1.State == s2.State {
		t.Error("two generated states share the same state value")
	}
	if s1.Nonce == s2.Nonce {
		t.Error("two generated states share the same nonce")
	}
	if s1.CodeVerifier == s2.CodeVerifier {
		t.Error("two generated states share the same PKCE verifier")
	}
	if c1 == c2 {
		t.Error("two generated states share the same PKCE challenge")
	}

	// The challenge must be the RFC 7636 S256 transform of the verifier, computed independently here.
	for _, tc := range []struct {
		name      string
		verifier  string
		challenge string
	}{
		{"first", s1.CodeVerifier, c1},
		{"second", s2.CodeVerifier, c2},
	} {
		digest := sha256.Sum256([]byte(tc.verifier))
		want := base64.RawURLEncoding.EncodeToString(digest[:])
		if tc.challenge != want {
			t.Errorf("%s challenge = %q, want S256(verifier) = %q", tc.name, tc.challenge, want)
		}
	}
}

func TestVerifyOIDCStateRejectsTamperedToken(t *testing.T) {
	client, _ := newOIDCTestClient(t, oidcStateTestSecret)
	_, signed := signedStateFixture(t, client)

	payload, sig, ok := strings.Cut(signed, ".")
	if !ok {
		t.Fatalf("signed state has no separator: %q", signed)
	}

	// Pick a replacement first character that is guaranteed to differ, so the decoded bytes change.
	swapFirst := func(s string) string {
		if s[0] == 'A' {
			return "B" + s[1:]
		}
		return "A" + s[1:]
	}

	tests := []struct {
		name  string
		token string
	}{
		{"signature first character flipped", payload + "." + swapFirst(sig)},
		{"payload first character flipped", swapFirst(payload) + "." + sig},
		{"signature replaced by zero bytes", payload + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := client.VerifyOIDCState(tc.token)
			if !errors.Is(err, auth.ErrInvalidState) {
				t.Fatalf("VerifyOIDCState err = %v, want ErrInvalidState", err)
			}
			if got != nil {
				t.Errorf("tampered token returned state %+v", got)
			}
		})
	}
}

func TestVerifyOIDCStateRejectsMalformedTokens(t *testing.T) {
	client, _ := newOIDCTestClient(t, oidcStateTestSecret)
	_, signed := signedStateFixture(t, client)

	payload, sig, _ := strings.Cut(signed, ".")

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"garbage", "not-a-state-token"},
		{"only separator", "."},
		{"three segments", payload + "." + sig + ".extra"},
		{"payload without separator", payload},
		{"empty signature", payload + "."},
		{"empty payload", "." + sig},
		{"truncated signature", payload + "." + sig[:len(sig)-10]},
		{"non-base64 signature", payload + ".!!!not-base64!!!"},
		{"non-base64 payload", "!!!not-base64!!!." + sig},
		{"correctly signed but payload is not JSON", hmacSignRaw(oidcStateTestSecret, []byte("plain-text-not-json"))},
		{"correctly signed but payload is JSON of wrong type", hmacSignRaw(oidcStateTestSecret, []byte(`["not","a","state"]`))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := client.VerifyOIDCState(tc.token)
			if !errors.Is(err, auth.ErrInvalidState) {
				t.Fatalf("VerifyOIDCState(%q) err = %v, want ErrInvalidState", tc.token, err)
			}
			if got != nil {
				t.Errorf("malformed token returned state %+v", got)
			}
		})
	}
}

// hmacSignRaw HMAC-SHA256-signs raw payload bytes with the token layout used by SignOIDCState, so a
// test can present a correctly signed token whose payload is not a valid OIDCState.
func hmacSignRaw(secret, payload []byte) string {
	enc := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(enc))
	return enc + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifyOIDCStateRejectsExpiredState(t *testing.T) {
	client, _ := newOIDCTestClient(t, oidcStateTestSecret)

	tests := []struct {
		name      string
		expiresAt time.Time
		wantErr   bool
	}{
		{"expired one second ago", time.Now().Add(-time.Second), true},
		{"expired one hour ago", time.Now().Add(-time.Hour), true},
		{"expires in one minute", time.Now().Add(time.Minute), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state, _, err := client.GenerateOIDCState()
			if err != nil {
				t.Fatalf("GenerateOIDCState: %v", err)
			}
			state.ExpiresAt = tc.expiresAt
			signed, err := client.SignOIDCState(state)
			if err != nil {
				t.Fatalf("SignOIDCState: %v", err)
			}

			got, err := client.VerifyOIDCState(signed)
			if tc.wantErr {
				if !errors.Is(err, auth.ErrInvalidState) {
					t.Fatalf("VerifyOIDCState err = %v, want ErrInvalidState", err)
				}
				if got != nil {
					t.Errorf("expired state returned %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("VerifyOIDCState on unexpired state: %v", err)
			}
			if got.State != state.State {
				t.Errorf("state = %q, want %q", got.State, state.State)
			}
		})
	}
}

func TestVerifyOIDCStateRejectsDifferentSecret(t *testing.T) {
	signer, _ := newOIDCTestClient(t, []byte("signer-secret-aaaaaaaaaaaaaaaaaaaa"))
	verifier, _ := newOIDCTestClient(t, []byte("verifier-secret-bbbbbbbbbbbbbbbbbbb"))
	sameSecret, _ := newOIDCTestClient(t, []byte("signer-secret-aaaaaaaaaaaaaaaaaaaa"))

	_, signed := signedStateFixture(t, signer)

	if _, err := verifier.VerifyOIDCState(signed); !errors.Is(err, auth.ErrInvalidState) {
		t.Errorf("state signed with another secret: err = %v, want ErrInvalidState", err)
	}

	// Control: a client holding the same secret accepts the token, so the rejection above is about the key.
	if _, err := sameSecret.VerifyOIDCState(signed); err != nil {
		t.Errorf("same-secret client rejected valid state: %v", err)
	}
}

func TestOIDCClientIssuerURL(t *testing.T) {
	client, issuer := newOIDCTestClient(t, oidcStateTestSecret)
	if got := client.IssuerURL(); got != issuer {
		t.Errorf("IssuerURL() = %q, want %q", got, issuer)
	}
}

func TestOIDCClientAuthCodeURL(t *testing.T) {
	client, issuer := newOIDCTestClient(t, oidcStateTestSecret)

	const (
		state     = "state-value"
		challenge = "challenge-value"
		nonce     = "nonce-value"
	)
	raw := client.AuthCodeURL(state, challenge, nonce)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("AuthCodeURL returned unparsable URL %q: %v", raw, err)
	}
	if got := u.Scheme + "://" + u.Host + u.Path; got != issuer+"/authorize" {
		t.Errorf("authorize endpoint = %q, want %q", got, issuer+"/authorize")
	}

	q := u.Query()
	want := map[string]string{
		"response_type":         "code",
		"client_id":             "client-id-test",
		"redirect_uri":          "http://localhost:8080/api/v1/auth/callback",
		"state":                 state,
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
		"nonce":                 nonce,
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("query %s = %q, want %q", k, got, v)
		}
	}
	if !strings.Contains(q.Get("scope"), "openid") {
		t.Errorf("scope = %q, want it to contain openid", q.Get("scope"))
	}
}
