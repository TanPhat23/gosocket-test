package api_test

import (
	"io"
	"net/http"
	"testing"
	"time"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/models"
)

const protectedPath = "/api/v1/models"

func (f *routeAuthzFixture) logout(t *testing.T, cookie *http.Cookie) (int, []*http.Cookie) {
	t.Helper()
	req := newJSONRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := f.server.App.Test(req)
	if err != nil {
		t.Fatalf("logout request failed: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, resp.Cookies()
}

func (f *routeAuthzFixture) status(t *testing.T, path string, cookie *http.Cookie) int {
	t.Helper()
	req := newJSONRequest(http.MethodGet, path, nil)
	req.AddCookie(cookie)
	code, _ := f.send(t, req)
	return code
}

func (f *routeAuthzFixture) validAfter(t *testing.T, who string) *time.Time {
	t.Helper()
	var u models.User
	if err := f.db.First(&u, "id = ?", f.users[who].ID).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	return u.SessionsValidAfter
}

// issued returns a fresh cookie for who carrying an IssuedAt claim, like a real login would.
func (f *routeAuthzFixture) issued(t *testing.T, who string) *http.Cookie {
	t.Helper()
	u := f.users[who]
	v, err := f.sm.SignPayload(auth.SessionClaims{UserID: u.ID, Issuer: u.Issuer, Subject: u.Subject, IssuedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return &http.Cookie{Name: auth.SessionCookieName, Value: v}
}

func TestLogout_RevokesSessionEverywhere(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	cookie := f.issued(t, "viewer-a")
	legacy := f.cookies["viewer-a"] // signed without IssuedAt
	other := f.issued(t, "member-a")

	// Legacy cookies (no IssuedAt) stay valid until the user's first revocation.
	for _, c := range []*http.Cookie{cookie, legacy} {
		if code := f.status(t, protectedPath, c); code != http.StatusOK {
			t.Fatalf("before logout: expected 200, got %d", code)
		}
	}
	code, set := f.logout(t, cookie)
	if code != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d", code)
	}
	cleared := false
	for _, c := range set {
		cleared = cleared || (c.Name == auth.SessionCookieName && c.Value == "" && c.MaxAge < 0)
	}
	if !cleared {
		t.Error("logout did not clear the session cookie")
	}

	for name, c := range map[string]*http.Cookie{"issued": cookie, "legacy": legacy} {
		if code := f.status(t, protectedPath, c); code != http.StatusUnauthorized {
			t.Errorf("%s cookie on protected route after logout: expected 401, got %d", name, code)
		}
		if code := f.status(t, "/api/v1/auth/me", c); code != http.StatusUnauthorized {
			t.Errorf("%s cookie on /auth/me after logout: expected 401, got %d", name, code)
		}
	}
	if code := f.status(t, protectedPath, other); code != http.StatusOK {
		t.Errorf("another user's session must be unaffected, got %d", code)
	}
	if f.validAfter(t, "member-a") != nil {
		t.Error("logout of one user revoked another user's sessions")
	}
}

func TestLogout_WithoutValidCookieIsIdempotentAndRevokesNothing(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	cookie := f.issued(t, "viewer-a")
	expired, err := f.sm.SignPayload(auth.SessionClaims{UserID: f.users["viewer-a"].ID, IssuedAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	tampered := cookie.Value[:len(cookie.Value)-2] + "xx"

	for name, c := range map[string]*http.Cookie{
		"no cookie": nil,
		"garbage":   {Name: auth.SessionCookieName, Value: "garbage"},
		"tampered":  {Name: auth.SessionCookieName, Value: tampered},
		"expired":   {Name: auth.SessionCookieName, Value: expired},
	} {
		if code, _ := f.logout(t, c); code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", name, code)
		}
	}
	if f.validAfter(t, "viewer-a") != nil {
		t.Error("logout without a valid cookie revoked sessions")
	}
	if code := f.status(t, protectedPath, cookie); code != http.StatusOK {
		t.Errorf("session must still be valid, got %d", code)
	}
}

// A revoked cookie still carries a valid signature; replaying it at /logout must not revoke the
// user's newer sessions.
func TestLogout_RevokedCookieCannotRevokeNewSessions(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	old := f.issued(t, "viewer-a")
	f.logout(t, old)
	fresh := f.issued(t, "viewer-a")

	if code, _ := f.logout(t, old); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if code := f.status(t, protectedPath, fresh); code != http.StatusOK {
		t.Errorf("replayed revoked cookie logged out a newer session, got %d", code)
	}
}

func TestLogout_PATStillWorks(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	token := f.pat(t, "viewer-a", f.teamA)
	f.logout(t, f.issued(t, "viewer-a"))
	if code, body := f.doPAT(t, http.MethodGet, protectedPath, nil, token); code != http.StatusOK {
		t.Errorf("PAT after logout: expected 200, got %d %s", code, body)
	}
}

func TestLogout_LoginAfterLogoutWorks(t *testing.T) {
	t.Setenv("ATM_ENABLE_LOCAL_LOGIN", "true")
	f := setupRouteAuthzFixture(t)

	login := func() *http.Cookie {
		req := newJSONRequest(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": "admin@example.com", "password": "password123"})
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		resp, err := f.server.App.Test(req, loginTestConfig)
		if err != nil {
			t.Fatalf("login request failed: %v", err)
		}
		defer resp.Body.Close()
		for _, c := range resp.Cookies() {
			if c.Name == auth.SessionCookieName && c.Value != "" {
				return &http.Cookie{Name: c.Name, Value: c.Value}
			}
		}
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login did not set a session cookie: %d %s", resp.StatusCode, b)
		return nil
	}

	first := login()
	if code := f.status(t, protectedPath, first); code != http.StatusOK {
		t.Fatalf("first login: expected 200, got %d", code)
	}
	f.logout(t, first)
	second := login() // immediately after logout
	if code := f.status(t, protectedPath, first); code != http.StatusUnauthorized {
		t.Errorf("old cookie after relogin: expected 401, got %d", code)
	}
	if code := f.status(t, protectedPath, second); code != http.StatusOK {
		t.Errorf("new cookie after logout: expected 200, got %d", code)
	}
	if code := f.status(t, "/api/v1/auth/me", second); code != http.StatusOK {
		t.Errorf("/auth/me with new cookie: expected 200, got %d", code)
	}
}

func TestWS_RevalidationDisconnectsLoggedOutSession(t *testing.T) {
	f := setupRouteAuthzFixture(t, api.WithWSRevalidateInterval(testRevalidateInterval))
	addr := startWSServer(t, f)
	f.cookies["viewer-a-issued"] = f.issued(t, "viewer-a")

	conn, code := dialWS(t, f, addr, "/api/v1/ws/events", "viewer-a-issued", "http://"+addr)
	if code != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", code)
	}
	if code, _ := f.logout(t, f.cookies["viewer-a-issued"]); code != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d", code)
	}
	expectPolicyClose(t, conn)
}

// If the revoke write fails the session is still valid server-side, so logout must say so (non-2xx)
// instead of reporting "logged_out"; the browser's cookie is cleared regardless.
func TestLogout_RevokeFailureIsReportedNotSwallowed(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	cookie := f.issued(t, "viewer-a")
	if err := f.db.Exec(`CREATE TRIGGER block_revoke BEFORE UPDATE OF sessions_valid_after ON users
		BEGIN SELECT RAISE(ABORT, 'revoke blocked'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	code, set := f.logout(t, cookie)
	if code < 500 {
		t.Fatalf("failed revocation must not look like success, got %d", code)
	}
	cleared := false
	for _, c := range set {
		cleared = cleared || (c.Name == auth.SessionCookieName && c.Value == "" && c.MaxAge < 0)
	}
	if !cleared {
		t.Error("cookie must still be cleared client-side")
	}
	if f.validAfter(t, "viewer-a") != nil {
		t.Error("revocation must not have been recorded")
	}
}
