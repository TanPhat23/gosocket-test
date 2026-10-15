package auth_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-task-manager/internal/auth"
)

const (
	exchClientID     = "exch-client"
	exchClientSecret = "exch-secret"
	exchKeyID        = "exch-key-1"
)

// fakeProvider is an in-process OIDC provider: discovery, JWKS, and a token endpoint that records
// the form it was sent. ID tokens are RS256-signed by hand so the tests need no extra dependency.
type fakeProvider struct {
	t      *testing.T
	srv    *httptest.Server
	key    *rsa.PrivateKey
	mu     sync.Mutex
	form   url.Values
	user   string
	pass   string
	idTok  string // ID token returned by the token endpoint
	status int    // token endpoint status (0 = 200)
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeProvider{t: t, key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                p.srv.URL,
			"authorization_endpoint":                p.srv.URL + "/authorize",
			"token_endpoint":                        p.srv.URL + "/token",
			"jwks_uri":                              p.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		pub := key.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "use": "sig", "alg": "RS256", "kid": exchKeyID,
			"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		p.mu.Lock()
		p.form = r.PostForm
		p.user, p.pass, _ = r.BasicAuth()
		idTok, status := p.idTok, p.status
		p.mu.Unlock()
		if status != 0 {
			http.Error(w, `{"error":"invalid_grant"}`, status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-1", "token_type": "Bearer", "expires_in": 3600, "id_token": idTok,
		})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeProvider) client(t *testing.T) *auth.OIDCClient {
	t.Helper()
	c, err := auth.NewOIDCClient(context.Background(), auth.OIDCConfig{
		IssuerURL:    p.srv.URL,
		ClientID:     exchClientID,
		ClientSecret: exchClientSecret,
		RedirectURL:  "http://localhost:8080/cb",
		CookieSecret: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("NewOIDCClient: %v", err)
	}
	return c
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// sign builds a compact JWS over claims with the given header alg and key (nil key => empty signature).
func (p *fakeProvider) sign(alg string, claims map[string]any, key *rsa.PrivateKey) string {
	hdr, _ := json.Marshal(map[string]string{"alg": alg, "kid": exchKeyID, "typ": "JWT"})
	body, _ := json.Marshal(claims)
	signing := b64(hdr) + "." + b64(body)
	if key == nil {
		return signing + "."
	}
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		p.t.Fatal(err)
	}
	return signing + "." + b64(sig)
}

func (p *fakeProvider) claims(mod func(map[string]any)) map[string]any {
	c := map[string]any{
		"iss": p.srv.URL, "sub": "user-1", "aud": exchClientID,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"nonce": "n-1", "email": "u@example.com",
	}
	if mod != nil {
		mod(c)
	}
	return c
}

func TestOIDCVerifyIDToken(t *testing.T) {
	p := newFakeProvider(t)
	c := p.client(t)
	ctx := context.Background()

	tok, err := c.VerifyIDToken(ctx, p.sign("RS256", p.claims(nil), p.key))
	if err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if tok.Subject != "user-1" || tok.Nonce != "n-1" || tok.Issuer != p.srv.URL {
		t.Errorf("unexpected claims: sub=%q nonce=%q iss=%q", tok.Subject, tok.Nonce, tok.Issuer)
	}

	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	bad := map[string]string{
		"wrong audience":  p.sign("RS256", p.claims(func(m map[string]any) { m["aud"] = "someone-else" }), p.key),
		"wrong issuer":    p.sign("RS256", p.claims(func(m map[string]any) { m["iss"] = "https://evil.example" }), p.key),
		"expired":         p.sign("RS256", p.claims(func(m map[string]any) { m["exp"] = time.Now().Add(-time.Hour).Unix() }), p.key),
		"foreign key":     p.sign("RS256", p.claims(nil), otherKey),
		"alg none":        p.sign("none", p.claims(nil), nil),
		"alg HS256":       p.sign("HS256", p.claims(nil), nil),
		"tampered claims": tamper(p.sign("RS256", p.claims(nil), p.key)),
		"garbage":         "not-a-jwt",
		"empty":           "",
	}
	for name, raw := range bad {
		if _, err := c.VerifyIDToken(ctx, raw); err == nil {
			t.Errorf("%s: token must be rejected", name)
		}
	}
}

// tamper swaps the subject in an otherwise valid token, leaving the signature unchanged.
func tamper(raw string) string {
	parts := strings.Split(raw, ".")
	body, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	m["sub"] = "admin"
	nb, _ := json.Marshal(m)
	parts[1] = b64(nb)
	return strings.Join(parts, ".")
}

func TestOIDCExchange(t *testing.T) {
	p := newFakeProvider(t)
	p.idTok = p.sign("RS256", p.claims(nil), p.key)
	c := p.client(t)

	tok, err := c.Exchange(context.Background(), "auth-code-1", "verifier-1")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.AccessToken != "access-1" {
		t.Errorf("access token = %q", tok.AccessToken)
	}
	if raw, _ := tok.Extra("id_token").(string); raw != p.idTok {
		t.Errorf("id_token not passed through")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	want := map[string]string{
		"grant_type":    "authorization_code",
		"code":          "auth-code-1",
		"code_verifier": "verifier-1",
		"redirect_uri":  "http://localhost:8080/cb",
	}
	for k, v := range want {
		if got := p.form.Get(k); got != v {
			t.Errorf("token request %s = %q, want %q", k, got, v)
		}
	}
	// Client credentials travel in the Authorization header or the form, never as a bare guess.
	user, pass := p.user, p.pass
	if user == "" {
		user, pass = p.form.Get("client_id"), p.form.Get("client_secret")
	}
	if user != exchClientID || pass != exchClientSecret {
		t.Errorf("client credentials not sent: %q/%q", user, pass)
	}
}

func TestOIDCExchangeProviderErrorAndCancel(t *testing.T) {
	p := newFakeProvider(t)
	p.status = http.StatusBadRequest
	c := p.client(t)

	if _, err := c.Exchange(context.Background(), "bad-code", "v"); err == nil {
		t.Fatal("provider rejection must surface as an error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Exchange(ctx, "code", "v"); err == nil {
		t.Fatal("canceled context must fail the exchange")
	}
}

// With InternalIssuerURL set, discovery and token calls for the public issuer are routed to the
// internal address (the split-horizon docker setup), while the issuer claim stays the public one.
func TestOIDCInternalIssuerRewriting(t *testing.T) {
	p := newFakeProvider(t)
	p.idTok = p.sign("RS256", p.claims(nil), p.key)

	// The public issuer is the fake provider's URL rewritten to an unroutable host; only the
	// rewriting transport can reach it.
	internal, _ := url.Parse(p.srv.URL)
	public := "http://idp.invalid:" + internal.Port()
	// Re-point the provider's advertised URLs at the public name.
	p.srv.Config.Handler = rewriteIssuer(p.srv.Config.Handler, p.srv.URL, public)
	c, err := auth.NewOIDCClient(context.Background(), auth.OIDCConfig{
		IssuerURL:         public,
		InternalIssuerURL: p.srv.URL,
		ClientID:          exchClientID,
		ClientSecret:      exchClientSecret,
		RedirectURL:       "http://localhost:8080/cb",
		CookieSecret:      []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("discovery through internal URL failed: %v", err)
	}
	if c.IssuerURL() != public {
		t.Errorf("IssuerURL = %q, want public %q", c.IssuerURL(), public)
	}
	tok, err := c.Exchange(context.Background(), "code", "verifier")
	if err != nil {
		t.Fatalf("Exchange via rewritten endpoint: %v", err)
	}
	if tok.AccessToken != "access-1" {
		t.Errorf("access token = %q", tok.AccessToken)
	}
}

// rewriteIssuer makes the handler advertise the public issuer name instead of its listener URL.
func rewriteIssuer(next http.Handler, from, to string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write([]byte(strings.ReplaceAll(rec.Body.String(), from, to)))
	})
}
