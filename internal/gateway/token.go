// Package gateway is the control-plane model gateway for Gate 0G. Sandboxes hold only a
// short-lived capability token; provider keys never leave this process.
package gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// ErrInvalidToken is returned for every token failure so callers cannot distinguish forged,
// expired and revoked tokens.
var ErrInvalidToken = errors.New("gateway: invalid capability token")

const minSecretLen = 32

// Claims bind a capability token to exactly one task attempt.
type Claims struct {
	ProjectID string `json:"p"`
	TaskID    string `json:"t"`
	AttemptID string `json:"a"`
	Expires   int64  `json:"e"`
}

// Issuer mints and verifies HMAC-signed capability tokens and tracks revoked attempts.
type Issuer struct {
	secret  []byte
	now     func() time.Time
	mu      sync.RWMutex
	revoked map[string]struct{}
}

// NewIssuer fails closed on a weak signing secret.
func NewIssuer(secret []byte) (*Issuer, error) {
	if len(secret) < minSecretLen {
		return nil, errors.New("gateway: signing secret must be at least 32 bytes")
	}
	return &Issuer{secret: append([]byte(nil), secret...), now: time.Now, revoked: map[string]struct{}{}}, nil
}

// Mint returns a token valid for ttl, or "" when any claim is empty or ttl is not positive.
func (i *Issuer) Mint(c Claims, ttl time.Duration) string {
	if c.ProjectID == "" || c.TaskID == "" || c.AttemptID == "" || ttl <= 0 {
		return ""
	}
	c.Expires = i.now().Add(ttl).Unix()
	body, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	enc := base64.RawURLEncoding.EncodeToString(body)
	return enc + "." + base64.RawURLEncoding.EncodeToString(i.sign(enc))
}

// Verify returns the claims of a well-formed, correctly signed, unexpired, unrevoked token.
func (i *Issuer) Verify(token string) (Claims, error) {
	enc, sig, ok := strings.Cut(token, ".")
	if !ok || enc == "" || sig == "" {
		return Claims{}, ErrInvalidToken
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, i.sign(enc)) {
		return Claims{}, ErrInvalidToken
	}
	body, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(body, &c); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if c.ProjectID == "" || c.TaskID == "" || c.AttemptID == "" || i.now().Unix() >= c.Expires {
		return Claims{}, ErrInvalidToken
	}
	i.mu.RLock()
	_, gone := i.revoked[c.AttemptID]
	i.mu.RUnlock()
	if gone {
		return Claims{}, ErrInvalidToken
	}
	return c, nil
}

// Revoke permanently invalidates every token for the attempt.
func (i *Issuer) Revoke(attemptID string) {
	i.mu.Lock()
	i.revoked[attemptID] = struct{}{}
	i.mu.Unlock()
}

func (i *Issuer) sign(enc string) []byte {
	m := hmac.New(sha256.New, i.secret)
	m.Write([]byte(enc))
	return m.Sum(nil)
}
