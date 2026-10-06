package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agent-task-manager/internal/common"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	StateCookieName = "atm_oidc_state"
	StateTTL        = 10 * time.Minute
)

var (
	ErrInvalidState = errors.New("invalid or expired OIDC state")
)

// OIDCState holds the temporary state, nonce, and PKCE code_verifier.
type OIDCState struct {
	State        string    `json:"state"`
	Nonce        string    `json:"nonce"`
	CodeVerifier string    `json:"verifier"`
	ExpiresAt    time.Time `json:"exp"`
}

// Authenticator interface defines methods for OIDC authentication.
type Authenticator interface {
	AuthCodeURL(state, codeChallenge, nonce string) string
	Exchange(ctx context.Context, code, codeVerifier string) (*oauth2.Token, error)
	VerifyIDToken(ctx context.Context, rawIDToken string) (*oidc.IDToken, error)
	GenerateOIDCState() (*OIDCState, string, error)
	SignOIDCState(state *OIDCState) (string, error)
	VerifyOIDCState(signedState string) (*OIDCState, error)
	IssuerURL() string
}

// OIDCConfig contains configuration for the OIDC client.
type OIDCConfig struct {
	IssuerURL         string
	InternalIssuerURL string
	ClientID          string
	ClientSecret      string
	RedirectURL       string
	CookieSecret      []byte
	SecureCookie      bool
}

type rewritingTransport struct {
	base    http.RoundTripper
	fromURL *url.URL
	toURL   *url.URL
}

func (t *rewritingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == t.fromURL.Scheme && req.URL.Host == t.fromURL.Host && strings.HasPrefix(req.URL.Path, t.fromURL.Path) {
		newReq := req.Clone(req.Context())
		newReq.URL.Scheme = t.toURL.Scheme
		newReq.URL.Host = t.toURL.Host
		newReq.URL.Path = strings.Replace(req.URL.Path, t.fromURL.Path, t.toURL.Path, 1)
		return t.base.RoundTrip(newReq)
	}
	return t.base.RoundTrip(req)
}

// OIDCClient implements Authenticator.
type OIDCClient struct {
	cfg          OIDCConfig
	provider     *oidc.Provider
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
	httpClient   *http.Client
}

// NewOIDCClient initializes an OIDC provider and verifier.
func NewOIDCClient(ctx context.Context, cfg OIDCConfig) (*OIDCClient, error) {
	var httpClient *http.Client
	provCtx := ctx

	if cfg.InternalIssuerURL != "" {
		fromU, err := url.Parse(cfg.IssuerURL)
		if err == nil {
			toU, err := url.Parse(cfg.InternalIssuerURL)
			if err == nil {
				httpClient = &http.Client{
					Transport: &rewritingTransport{
						base:    http.DefaultTransport,
						fromURL: fromU,
						toURL:   toU,
					},
				}
				provCtx = oidc.ClientContext(ctx, httpClient)
				provCtx = context.WithValue(provCtx, oauth2.HTTPClient, httpClient)
			}
		}
	}

	provider, err := oidc.NewProvider(provCtx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize oidc provider: %w", err)
	}

	oauth2Config := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	return &OIDCClient{
		cfg:          cfg,
		provider:     provider,
		oauth2Config: oauth2Config,
		verifier:     verifier,
		httpClient:   httpClient,
	}, nil
}

// IssuerURL returns the configured issuer URL.
func (c *OIDCClient) IssuerURL() string {
	return c.cfg.IssuerURL
}

// AuthCodeURL constructs the authorization URL with PKCE and nonce.
func (c *OIDCClient) AuthCodeURL(state, codeChallenge, nonce string) string {
	return c.oauth2Config.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
}

// Exchange exchanges the authorization code for tokens using PKCE verifier.
func (c *OIDCClient) Exchange(ctx context.Context, code, codeVerifier string) (*oauth2.Token, error) {
	exCtx := ctx
	if c.httpClient != nil {
		exCtx = context.WithValue(exCtx, oauth2.HTTPClient, c.httpClient)
	}
	return c.oauth2Config.Exchange(exCtx, code, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
}

// VerifyIDToken verifies the raw ID token against the OIDC provider.
func (c *OIDCClient) VerifyIDToken(ctx context.Context, rawIDToken string) (*oidc.IDToken, error) {
	verCtx := ctx
	if c.httpClient != nil {
		verCtx = oidc.ClientContext(verCtx, c.httpClient)
	}
	return c.verifier.Verify(verCtx, rawIDToken)
}

// GenerateRandomString generates cryptographically secure base64url-encoded random string.
func GenerateRandomString(byteLength int) (string, error) {
	b := make([]byte, byteLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateCodeChallenge calculates the S256 code challenge from the code verifier.
func GenerateCodeChallenge(codeVerifier string) string {
	hash := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// GenerateOIDCState generates state, nonce, and PKCE verifier/challenge.
func (c *OIDCClient) GenerateOIDCState() (*OIDCState, string, error) {
	state, err := GenerateRandomString(32)
	if err != nil {
		return nil, "", err
	}
	nonce, err := GenerateRandomString(32)
	if err != nil {
		return nil, "", err
	}
	verifier, err := GenerateRandomString(32)
	if err != nil {
		return nil, "", err
	}

	challenge := GenerateCodeChallenge(verifier)

	s := &OIDCState{
		State:        state,
		Nonce:        nonce,
		CodeVerifier: verifier,
		ExpiresAt:    time.Now().Add(StateTTL),
	}
	return s, challenge, nil
}

// SignOIDCState encodes and signs OIDCState.
func (c *OIDCClient) SignOIDCState(state *OIDCState) (string, error) {
	data, err := common.Marshal(state)
	if err != nil {
		return "", err
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, c.cfg.CookieSecret)
	mac.Write([]byte(encodedPayload))
	signature := mac.Sum(nil)
	encodedSig := base64.RawURLEncoding.EncodeToString(signature)

	return encodedPayload + "." + encodedSig, nil
}

// VerifyOIDCState verifies and decodes the signed OIDC state token.
func (c *OIDCClient) VerifyOIDCState(signedState string) (*OIDCState, error) {
	parts := strings.Split(signedState, ".")
	if len(parts) != 2 {
		return nil, ErrInvalidState
	}

	payloadPart, sigPart := parts[0], parts[1]

	expectedMAC := hmac.New(sha256.New, c.cfg.CookieSecret)
	expectedMAC.Write([]byte(payloadPart))
	expectedSig := expectedMAC.Sum(nil)

	actualSig, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil {
		return nil, ErrInvalidState
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return nil, ErrInvalidState
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return nil, ErrInvalidState
	}

	state, err := common.Unmarshal[OIDCState](payloadBytes)
	if err != nil {
		return nil, ErrInvalidState
	}

	if time.Now().After(state.ExpiresAt) {
		return nil, ErrInvalidState
	}

	return &state, nil
}
