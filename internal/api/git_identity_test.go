package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type meBody struct {
	User struct {
		GitName  string `json:"git_name"`
		GitEmail string `json:"git_email"`
	} `json:"user"`
}

func decodeMe(t *testing.T, body string) meBody {
	t.Helper()
	var m meBody
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("response is not a user: %v: %s", err, body)
	}
	return m
}

func TestGitIdentity_SetReadAndClear(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	set := map[string]string{"git_name": "Jane Dev", "git_email": "jane@example.com"}

	if code, body := f.do(t, http.MethodPut, "/api/v1/me/git-identity", set, ""); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated PUT: status %d, want 401 (%s)", code, body)
	}

	code, body := f.do(t, http.MethodPut, "/api/v1/me/git-identity", set, "member-a")
	if m := decodeMe(t, body); code != http.StatusOK || m.User.GitName != "Jane Dev" || m.User.GitEmail != "jane@example.com" {
		t.Fatalf("PUT: status %d body %s", code, body)
	}

	// The profile the web UI loads carries the saved identity, and only for its owner.
	code, body = f.do(t, http.MethodGet, "/api/v1/auth/me", nil, "member-a")
	if m := decodeMe(t, body); code != http.StatusOK || m.User.GitName != "Jane Dev" {
		t.Fatalf("GET /auth/me as the owner: status %d body %s", code, body)
	}
	if _, body = f.do(t, http.MethodGet, "/api/v1/auth/me", nil, "viewer-a"); strings.Contains(body, "jane@example.com") {
		t.Errorf("another user's profile shows the identity: %s", body)
	}

	clear := map[string]string{"git_name": "", "git_email": ""}
	code, body = f.do(t, http.MethodPut, "/api/v1/me/git-identity", clear, "member-a")
	if m := decodeMe(t, body); code != http.StatusOK || m.User.GitName != "" || m.User.GitEmail != "" {
		t.Fatalf("clearing: status %d body %s", code, body)
	}
}

func TestGitIdentity_RejectsInvalidInput(t *testing.T) {
	f := setupRouteAuthzFixture(t)
	for label, in := range map[string]map[string]string{
		"name only":     {"git_name": "Jane", "git_email": ""},
		"bad email":     {"git_name": "Jane", "git_email": "not-an-email"},
		"angle in name": {"git_name": "Jane <x@y.z>", "git_email": "jane@example.com"},
	} {
		if code, body := f.do(t, http.MethodPut, "/api/v1/me/git-identity", in, "member-a"); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (%s)", label, code, body)
		}
	}
	// A rejected request must not have changed what is stored.
	if _, body := f.do(t, http.MethodGet, "/api/v1/auth/me", nil, "member-a"); decodeMe(t, body).User.GitEmail != "" {
		t.Errorf("a rejected update was stored: %s", body)
	}
}
