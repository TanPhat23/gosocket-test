package gateway

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var testSecret = []byte(strings.Repeat("s", 32))

func newTestIssuer(t *testing.T, now *time.Time) *Issuer {
	t.Helper()
	is, err := NewIssuer(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	is.now = func() time.Time { return *now }
	return is
}

func TestNewIssuer_RejectsShortSecret(t *testing.T) {
	if _, err := NewIssuer([]byte("short")); err == nil {
		t.Fatal("expected error for short secret")
	}
}

func TestToken_RoundTrip(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	is := newTestIssuer(t, &now)
	tok := is.Mint(Claims{ProjectID: "p1", TaskID: "t1", AttemptID: "a1"}, time.Minute)
	c, err := is.Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.ProjectID != "p1" || c.TaskID != "t1" || c.AttemptID != "a1" {
		t.Errorf("claims = %+v", c)
	}
}

func TestToken_Rejections(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	is := newTestIssuer(t, &now)
	good := is.Mint(Claims{ProjectID: "p1", TaskID: "t1", AttemptID: "a1"}, time.Minute)
	other, _ := NewIssuer([]byte(strings.Repeat("x", 32)))
	other.now = is.now
	foreign := other.Mint(Claims{ProjectID: "p1", TaskID: "t1", AttemptID: "a1"}, time.Minute)
	parts := strings.SplitN(good, ".", 2)
	tampered := parts[0] + "A" + "." + parts[1]

	cases := map[string]string{
		"empty":         "",
		"garbage":       "not-a-token",
		"forged":        foreign,
		"tampered body": tampered,
		"no signature":  parts[0] + ".",
	}
	for name, tok := range cases {
		if _, err := is.Verify(tok); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("%s: expected ErrInvalidToken, got %v", name, err)
		}
	}

	now = now.Add(2 * time.Minute)
	if _, err := is.Verify(good); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expired: expected ErrInvalidToken, got %v", err)
	}
}

func TestToken_RevokedAttemptRejected(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	is := newTestIssuer(t, &now)
	tok := is.Mint(Claims{ProjectID: "p1", TaskID: "t1", AttemptID: "a1"}, time.Hour)
	other := is.Mint(Claims{ProjectID: "p1", TaskID: "t1", AttemptID: "a2"}, time.Hour)
	is.Revoke("a1")
	if _, err := is.Verify(tok); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("revoked attempt accepted: %v", err)
	}
	if _, err := is.Verify(other); err != nil {
		t.Errorf("unrelated attempt rejected: %v", err)
	}
}

func TestToken_MintRequiresAllClaims(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	is := newTestIssuer(t, &now)
	if tok := is.Mint(Claims{ProjectID: "p1", TaskID: "t1"}, time.Minute); tok != "" {
		t.Errorf("minted token with empty attempt id: %q", tok)
	}
	if tok := is.Mint(Claims{ProjectID: "p1", TaskID: "t1", AttemptID: "a"}, 0); tok != "" {
		t.Errorf("minted token with non-positive ttl")
	}
}
