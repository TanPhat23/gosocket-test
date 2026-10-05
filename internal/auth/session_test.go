package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agent-task-manager/internal/auth"
)

func TestSessionCreationAndValidation(t *testing.T) {
	sm := auth.NewSessionManager([]byte("super-secret-key-32-bytes-long!"), 24*time.Hour, false)
	rec := httptest.NewRecorder()

	token, err := sm.CreateSession(rec, "user-123", "github.com", "sub-456")
	if err != nil {
		t.Fatalf("unexpected error creating session: %v", err)
	}
	if token == "" {
		t.Errorf("expected non-empty token")
	}

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected at least one cookie in response")
	}
	cookie := cookies[0]
	if cookie.Name != "atm_session" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie missing required security flags: %+v", cookie)
	}

	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.AddCookie(cookie)

	claims, err := sm.ValidateRequest(req)
	if err != nil {
		t.Fatalf("expected valid session, got: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("expected user-123, got %s", claims.UserID)
	}
	if claims.Issuer != "github.com" {
		t.Errorf("expected github.com, got %s", claims.Issuer)
	}
	if claims.Subject != "sub-456" {
		t.Errorf("expected sub-456, got %s", claims.Subject)
	}
}

func TestSessionTampering(t *testing.T) {
	sm := auth.NewSessionManager([]byte("super-secret-key-32-bytes-long!"), 24*time.Hour, false)
	rec := httptest.NewRecorder()

	_, err := sm.CreateSession(rec, "user-123", "github.com", "sub-456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cookie := rec.Result().Cookies()[0]
	// Tamper cookie value
	cookie.Value = cookie.Value + "tampered"

	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.AddCookie(cookie)

	_, err = sm.ValidateRequest(req)
	if err == nil {
		t.Fatalf("expected error for tampered session cookie, got nil")
	}
}

func TestSessionExpiration(t *testing.T) {
	sm := auth.NewSessionManager([]byte("super-secret-key-32-bytes-long!"), -1*time.Minute, false)
	rec := httptest.NewRecorder()

	_, err := sm.CreateSession(rec, "user-123", "github.com", "sub-456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cookie := rec.Result().Cookies()[0]
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.AddCookie(cookie)

	_, err = sm.ValidateRequest(req)
	if err == nil {
		t.Fatalf("expected error for expired session cookie, got nil")
	}
}

func TestClearSession(t *testing.T) {
	sm := auth.NewSessionManager([]byte("super-secret-key-32-bytes-long!"), 24*time.Hour, false)
	rec := httptest.NewRecorder()

	sm.ClearSession(rec)
	cookie := rec.Result().Cookies()[0]
	if cookie.Name != "atm_session" || cookie.MaxAge != -1 || cookie.Value != "" {
		t.Errorf("expected cookie to be cleared: %+v", cookie)
	}
}
