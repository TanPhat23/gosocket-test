package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agent-task-manager/internal/common"

	"github.com/gofiber/fiber/v3"
)

const (
	SessionCookieName = "atm_session"
)

var (
	ErrInvalidSession = errors.New("invalid session")
	ErrExpiredSession = errors.New("expired session")
	ErrNoSession      = errors.New("no session cookie found")
)

// SessionClaims represents the payload stored inside a signed session token.
type SessionClaims struct {
	UserID    string    `json:"uid"`
	Issuer    string    `json:"iss"`
	Subject   string    `json:"sub"`
	ExpiresAt time.Time `json:"exp"`
}

// SessionManager manages signed session cookies.
type SessionManager struct {
	secret []byte
	ttl    time.Duration
	secure bool
}

// NewSessionManager creates a new SessionManager instance.
func NewSessionManager(secret []byte, ttl time.Duration, secure bool) *SessionManager {
	return &SessionManager{
		secret: secret,
		ttl:    ttl,
		secure: secure,
	}
}

// SignPayload encodes and signs the given claims using HMAC-SHA256.
func (sm *SessionManager) SignPayload(claims SessionClaims) (string, error) {
	data, err := common.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("failed to marshal claims: %w", err)
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(data)

	mac := hmac.New(sha256.New, sm.secret)
	mac.Write([]byte(encodedPayload))
	signature := mac.Sum(nil)
	encodedSig := base64.RawURLEncoding.EncodeToString(signature)

	return encodedPayload + "." + encodedSig, nil
}

// VerifyPayload parses and verifies a signed token, returning the claims if valid.
func (sm *SessionManager) VerifyPayload(token string) (*SessionClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, ErrInvalidSession
	}

	payloadPart, sigPart := parts[0], parts[1]

	expectedMAC := hmac.New(sha256.New, sm.secret)
	expectedMAC.Write([]byte(payloadPart))
	expectedSig := expectedMAC.Sum(nil)

	actualSig, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil {
		return nil, ErrInvalidSession
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return nil, ErrInvalidSession
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return nil, ErrInvalidSession
	}

	claims, err := common.Unmarshal[SessionClaims](payloadBytes)
	if err != nil {
		return nil, ErrInvalidSession
	}

	if time.Now().After(claims.ExpiresAt) {
		return nil, ErrExpiredSession
	}

	return &claims, nil
}

// CreateSession creates a signed session token and sets the cookie on an http.ResponseWriter.
func (sm *SessionManager) CreateSession(w http.ResponseWriter, userID, issuer, subject string) (string, error) {
	claims := SessionClaims{
		UserID:    userID,
		Issuer:    issuer,
		Subject:   subject,
		ExpiresAt: time.Now().Add(sm.ttl),
	}

	token, err := sm.SignPayload(claims)
	if err != nil {
		return "", err
	}

	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  claims.ExpiresAt,
		MaxAge:   int(sm.ttl.Seconds()),
		HttpOnly: true,
		Secure:   sm.secure,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)

	return token, nil
}

// CreateSessionFiber creates a signed session token and sets the cookie on a Fiber context.
func (sm *SessionManager) CreateSessionFiber(c fiber.Ctx, userID, issuer, subject string) (string, error) {
	claims := SessionClaims{
		UserID:    userID,
		Issuer:    issuer,
		Subject:   subject,
		ExpiresAt: time.Now().Add(sm.ttl),
	}

	token, err := sm.SignPayload(claims)
	if err != nil {
		return "", err
	}

	c.Cookie(&fiber.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  claims.ExpiresAt,
		MaxAge:   int(sm.ttl.Seconds()),
		HTTPOnly: true,
		Secure:   sm.secure,
		SameSite: "Lax",
	})

	return token, nil
}

// ValidateRequest extracts and verifies the session cookie from an http.Request.
func (sm *SessionManager) ValidateRequest(req *http.Request) (*SessionClaims, error) {
	cookie, err := req.Cookie(SessionCookieName)
	if err != nil {
		return nil, ErrNoSession
	}
	return sm.VerifyPayload(cookie.Value)
}

// ValidateFiberRequest extracts and verifies the session cookie from a Fiber context.
func (sm *SessionManager) ValidateFiberRequest(c fiber.Ctx) (*SessionClaims, error) {
	token := c.Cookies(SessionCookieName)
	if token == "" {
		return nil, ErrNoSession
	}
	return sm.VerifyPayload(token)
}

// ClearSession clears the session cookie on an http.ResponseWriter.
func (sm *SessionManager) ClearSession(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   sm.secure,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)
}

// ClearSessionFiber clears the session cookie on a Fiber context.
func (sm *SessionManager) ClearSessionFiber(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HTTPOnly: true,
		Secure:   sm.secure,
		SameSite: "Lax",
	})
}
