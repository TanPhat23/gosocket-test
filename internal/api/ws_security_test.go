package api_test

import (
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/models"

	"github.com/fasthttp/websocket"
)

// testRevalidateInterval keeps revocation tests fast; each waits at most closeWait.
const (
	testRevalidateInterval = 50 * time.Millisecond
	closeWait              = 2 * time.Second
)

func TestWS_ConfiguredOriginAllowlist(t *testing.T) {
	t.Setenv("ATM_ALLOWED_ORIGINS", trustedFrontend)
	f := setupRouteAuthzFixture(t)
	addr := startWSServer(t, f)

	for _, path := range []string{"/api/v1/ws/events", "/api/v1/ws/terminal/" + f.taskA.ID} {
		for _, tc := range []struct {
			origin string
			want   int
		}{
			{"http://" + addr, http.StatusSwitchingProtocols},
			{trustedFrontend, http.StatusSwitchingProtocols},
			{"HTTPS://APP.EXAMPLE.COM", http.StatusSwitchingProtocols},
			{"http://app.example.com", http.StatusForbidden},
			{"https://app.example.com:8443", http.StatusForbidden},
			{"https://evil.example", http.StatusForbidden},
		} {
			if _, code := dialWS(t, f, addr, path, "viewer-a", tc.origin); code != tc.want {
				t.Errorf("%s origin %q: expected %d, got %d", path, tc.origin, tc.want, code)
			}
		}
	}
}

// waitGlobalSubscriber blocks until an /ws/events client is registered on the hub.
func waitGlobalSubscriber(t *testing.T, f *routeAuthzFixture) {
	t.Helper()
	deadline := time.Now().Add(closeWait)
	for !f.hub.HasSubscribers("global") {
		if time.Now().After(deadline) {
			t.Fatal("events client never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// expectPolicyClose reads until the server closes the socket and requires close code 1008.
func expectPolicyClose(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(closeWait))
	for {
		_, _, err := conn.ReadMessage()
		if err == nil {
			continue
		}
		var ce *websocket.CloseError
		if !errors.As(err, &ce) || ce.Code != websocket.ClosePolicyViolation {
			t.Fatalf("expected close %d (policy violation), got %v", websocket.ClosePolicyViolation, err)
		}
		return
	}
}

func TestWS_RevalidationDisconnects(t *testing.T) {
	eventsPath := "/api/v1/ws/events"
	cases := []struct {
		name   string
		path   func(f *routeAuthzFixture) string
		revoke func(t *testing.T, f *routeAuthzFixture)
	}{
		{
			name: "events: deleted user",
			path: func(*routeAuthzFixture) string { return eventsPath },
			revoke: func(t *testing.T, f *routeAuthzFixture) {
				if err := f.db.Delete(&models.User{}, "id = ?", f.users["viewer-a"].ID).Error; err != nil {
					t.Fatalf("delete user: %v", err)
				}
			},
		},
		{
			name: "terminal: deleted user",
			path: func(f *routeAuthzFixture) string { return "/api/v1/ws/terminal/" + f.taskA.ID },
			revoke: func(t *testing.T, f *routeAuthzFixture) {
				if err := f.db.Delete(&models.User{}, "id = ?", f.users["viewer-a"].ID).Error; err != nil {
					t.Fatalf("delete user: %v", err)
				}
			},
		},
		{
			name: "terminal: team membership removed",
			path: func(f *routeAuthzFixture) string { return "/api/v1/ws/terminal/" + f.taskA.ID },
			revoke: func(t *testing.T, f *routeAuthzFixture) {
				if err := f.db.Where("team_id = ? AND user_id = ?", f.teamA.ID, f.users["viewer-a"].ID).
					Delete(&models.TeamMembership{}).Error; err != nil {
					t.Fatalf("delete membership: %v", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupRouteAuthzFixture(t, api.WithWSRevalidateInterval(testRevalidateInterval))
			addr := startWSServer(t, f)
			conn, code := dialWS(t, f, addr, tc.path(f), "viewer-a", "http://"+addr)
			if code != http.StatusSwitchingProtocols {
				t.Fatalf("expected 101, got %d", code)
			}
			tc.revoke(t, f)
			expectPolicyClose(t, conn)
		})
	}
}

func TestWS_RevalidationDisconnectsExpiredSession(t *testing.T) {
	f := setupRouteAuthzFixture(t, api.WithWSRevalidateInterval(testRevalidateInterval))
	addr := startWSServer(t, f)
	u := f.users["viewer-a"]
	short, err := f.sm.SignPayload(auth.SessionClaims{IssuedAt: time.Now(), UserID: u.ID, Issuer: u.Issuer, Subject: u.Subject, ExpiresAt: time.Now().Add(300 * time.Millisecond)})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	f.cookies["viewer-a-short"] = &http.Cookie{Name: auth.SessionCookieName, Value: short}

	conn, code := dialWS(t, f, addr, "/api/v1/ws/events", "viewer-a-short", "")
	if code != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", code)
	}
	expectPolicyClose(t, conn)
}

// A socket authorized by a session cookie must not silently continue on a Bearer PAT sent alongside
// it once the cookie expires: the PAT's team scope was never applied to the connection.
func TestWS_RevalidationDisconnectsOnCredentialSwitch(t *testing.T) {
	f := setupRouteAuthzFixture(t, api.WithWSRevalidateInterval(testRevalidateInterval))
	addr := startWSServer(t, f)
	u := f.users["viewer-a"]
	short, err := f.sm.SignPayload(auth.SessionClaims{IssuedAt: time.Now(), UserID: u.ID, Issuer: u.Issuer, Subject: u.Subject, ExpiresAt: time.Now().Add(300 * time.Millisecond)})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	token := f.pat(t, "viewer-a", f.teamA)

	conn, code := dialWSHeader(t, addr, "/api/v1/ws/events", http.Header{
		"Cookie":        {(&http.Cookie{Name: auth.SessionCookieName, Value: short}).String()},
		"Authorization": {"Bearer " + token},
	})
	if code != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", code)
	}
	expectPolicyClose(t, conn)
}

func TestWS_RevalidationDisconnectsDeletedPAT(t *testing.T) {
	f := setupRouteAuthzFixture(t, api.WithWSRevalidateInterval(testRevalidateInterval))
	addr := startWSServer(t, f)
	token := f.pat(t, "viewer-a", f.teamA)

	conn, code := dialWSHeader(t, addr, "/api/v1/ws/terminal/"+f.taskA.ID, http.Header{"Authorization": {"Bearer " + token}})
	if code != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", code)
	}
	if err := f.db.Where("user_id = ?", f.users["viewer-a"].ID).Delete(&models.APIToken{}).Error; err != nil {
		t.Fatalf("delete token: %v", err)
	}
	expectPolicyClose(t, conn)
}

// While credentials and access stay valid, re-validation must not drop the socket.
func TestWS_RevalidationKeepsAuthorizedSocketsOpen(t *testing.T) {
	f := setupRouteAuthzFixture(t, api.WithWSRevalidateInterval(testRevalidateInterval))
	addr := startWSServer(t, f)

	events, code := dialWS(t, f, addr, "/api/v1/ws/events", "viewer-a", "")
	if code != http.StatusSwitchingProtocols {
		t.Fatalf("events: expected 101, got %d", code)
	}
	terminal, code := dialWS(t, f, addr, "/api/v1/ws/terminal/"+f.taskA.ID, "viewer-a", "")
	if code != http.StatusSwitchingProtocols {
		t.Fatalf("terminal: expected 101, got %d", code)
	}
	waitGlobalSubscriber(t, f)

	for name, conn := range map[string]*websocket.Conn{"events": events, "terminal": terminal} {
		_ = conn.SetReadDeadline(time.Now().Add(10 * testRevalidateInterval))
		_, _, err := conn.ReadMessage()
		var ne net.Error
		if !errors.As(err, &ne) || !ne.Timeout() {
			t.Errorf("%s: expected the socket to stay open (read timeout), got %v", name, err)
		}
	}
}
