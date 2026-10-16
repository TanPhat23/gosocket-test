package api_test

import (
	"net/http"
	"testing"

	"agent-task-manager/internal/dto"
)

const trustedFrontend = "https://app.example.com"

// httptest requests target Host "example.com", so "http://example.com" is the API's own origin.
const selfOrigin = "http://example.com"

func TestCSRF_CookieAuthenticatedMutations(t *testing.T) {
	t.Setenv("ATM_ALLOWED_ORIGINS", trustedFrontend)
	f := setupRouteAuthzFixture(t)

	cases := []struct {
		name                     string
		fetchSite, origin, refer string
		wantAllowed              bool
	}{
		{name: "sec-fetch-site same-origin", fetchSite: "same-origin", wantAllowed: true},
		{name: "sec-fetch-site none (user-initiated)", fetchSite: "none", wantAllowed: true},
		{name: "sec-fetch-site cross-site", fetchSite: "cross-site"},
		{name: "sec-fetch-site same-site", fetchSite: "same-site"},
		{name: "cross-site with foreign origin", fetchSite: "cross-site", origin: "https://evil.example"},
		{name: "cross-site from configured frontend origin", fetchSite: "cross-site", origin: trustedFrontend, wantAllowed: true},
		{name: "cross-site claiming the API origin", fetchSite: "cross-site", origin: selfOrigin},
		{name: "no fetch metadata, same-host origin", origin: selfOrigin, wantAllowed: true},
		{name: "no fetch metadata, configured origin", origin: trustedFrontend, wantAllowed: true},
		{name: "no fetch metadata, configured host with other scheme", origin: "http://app.example.com"},
		{name: "no fetch metadata, foreign origin", origin: "https://evil.example"},
		{name: "no fetch metadata, host-suffix origin", origin: "http://example.com.evil.test"},
		{name: "no fetch metadata, null origin", origin: "null"},
		{name: "no fetch metadata, same-host referer", refer: selfOrigin + "/projects?x=1", wantAllowed: true},
		{name: "no fetch metadata, foreign referer", refer: "https://evil.example/" + "example.com"},
		{name: "origin wins over referer", origin: "https://evil.example", refer: selfOrigin + "/"},
		{name: "no origin signal at all fails closed"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := f.countProjects()
			req := newJSONRequest(http.MethodPost, "/api/v1/projects",
				map[string]string{"name": "csrf-" + string(rune('a'+i)), "team_id": f.teamA.ID})
			req.AddCookie(f.cookies["owner-a"])
			setFetchHeaders(req, tc.fetchSite, tc.origin, tc.refer)
			code, body := f.send(t, req)
			if tc.wantAllowed {
				if code != http.StatusCreated {
					t.Fatalf("expected 201, got %d %s", code, body)
				}
				return
			}
			if code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d %s", code, body)
			}
			if after := f.countProjects(); after != before {
				t.Fatalf("rejected request persisted a project: %d -> %d", before, after)
			}
		})
	}

	t.Run("cross-site delete is rejected", func(t *testing.T) {
		req := newJSONRequest(http.MethodDelete, "/api/v1/projects/"+f.projA.ID, nil)
		req.AddCookie(f.cookies["owner-a"])
		setFetchHeaders(req, "cross-site", "https://evil.example", "")
		if code, body := f.send(t, req); code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d %s", code, body)
		}
		if !f.projectExists(f.projA.ID) {
			t.Fatal("rejected delete removed the project")
		}
	})

	t.Run("safe methods are not checked", func(t *testing.T) {
		req := newJSONRequest(http.MethodGet, "/api/v1/projects", nil)
		req.AddCookie(f.cookies["owner-a"])
		setFetchHeaders(req, "cross-site", "https://evil.example", "")
		if code, body := f.send(t, req); code != http.StatusOK {
			t.Fatalf("expected 200, got %d %s", code, body)
		}
	})

	t.Run("bearer PAT without a cookie is not checked", func(t *testing.T) {
		token := f.pat(t, "admin-a", f.teamA)
		code, body := f.doPAT(t, http.MethodPost, "/api/v1/projects", map[string]string{"name": "via-pat", "team_id": f.teamA.ID}, token)
		if code != http.StatusCreated {
			t.Fatalf("expected 201, got %d %s", code, body)
		}
	})
}

// Login and logout are origin-checked even without a session cookie (login CSRF).
func TestCSRF_LoginAndLogout(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	login := dto.LoginRequestDTO{Email: "admin@example.com", Password: "password123"}

	for _, tc := range []struct {
		name, path        string
		body              any
		fetchSite, origin string
		wantForbidden     bool
	}{
		{name: "login cross-site", path: "/api/v1/auth/login", body: login, fetchSite: "cross-site", origin: "https://evil.example", wantForbidden: true},
		{name: "login without origin signal", path: "/api/v1/auth/login", body: login, wantForbidden: true},
		// Local login is disabled in this fixture, so an accepted request reaches the 401 handler.
		{name: "login same-origin", path: "/api/v1/auth/login", body: login, fetchSite: "same-origin"},
		{name: "logout cross-site", path: "/api/v1/auth/logout", fetchSite: "cross-site", wantForbidden: true},
		{name: "logout without origin signal", path: "/api/v1/auth/logout", wantForbidden: true},
		{name: "logout same-host origin", path: "/api/v1/auth/logout", origin: selfOrigin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := newJSONRequest(http.MethodPost, tc.path, tc.body)
			setFetchHeaders(req, tc.fetchSite, tc.origin, "")
			code, body := f.send(t, req)
			if tc.wantForbidden != (code == http.StatusForbidden) {
				t.Fatalf("wantForbidden=%v, got %d %s", tc.wantForbidden, code, body)
			}
		})
	}
}

func setFetchHeaders(req *http.Request, fetchSite, origin, referer string) {
	if fetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", fetchSite)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
}
