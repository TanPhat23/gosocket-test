package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-task-manager/internal/models"

	"github.com/gofiber/fiber/v3"
)

func operatorApp(t *testing.T, email string, token *models.APIToken, keyFile string) *fiber.App {
	t.Helper()
	h := NewOperatorHandlers(keyFile, []string{"Op@Example.com"})
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		if email != "" {
			c.Locals(LocalsUserKey, &models.User{Email: email})
		}
		if token != nil {
			c.Locals(LocalsAPITokenKey, token)
		}
		return c.Next()
	})
	app.Get("/s", h.GetProviderKeyStatus)
	app.Put("/s", h.SetProviderKey)
	return app
}

func put(t *testing.T, app *fiber.App, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/s", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestOperator_OnlyAllowlistedSessionsCanWriteTheKey(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "provider.key")
	if err := os.WriteFile(keyFile, []byte("oldkeyoldkey"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, app := range map[string]*fiber.App{
		"anonymous":     operatorApp(t, "", nil, keyFile),
		"other user":    operatorApp(t, "dev@example.com", nil, keyFile),
		"operator, PAT": operatorApp(t, "op@example.com", &models.APIToken{}, keyFile),
	} {
		if code, _ := put(t, app, `{"key":"newkeynewkey"}`); code != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d", name, code)
		}
	}
	if b, _ := os.ReadFile(keyFile); string(b) != "oldkeyoldkey" {
		t.Fatalf("key changed by an unauthorized caller: %q", b)
	}
}

func TestOperator_WritesTrimmedKeyAndNeverEchoesIt(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "provider.key")
	if err := os.WriteFile(keyFile, []byte("oldkeyoldkey-with-more"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := operatorApp(t, "OP@example.com", nil, keyFile)
	code, body := put(t, app, `{"key":"  AIzaNEWKEY12345  "}`)
	if code != http.StatusOK || strings.Contains(body, "AIza") {
		t.Fatalf("code=%d body=%s", code, body)
	}
	if b, _ := os.ReadFile(keyFile); string(b) != "AIzaNEWKEY12345" {
		t.Fatalf("file = %q (must be fully replaced, trimmed)", b)
	}
	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/s", nil))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(b), `"configured":true`) || strings.Contains(string(b), "AIza") {
		t.Fatalf("status: %s", b)
	}
}

func TestOperator_RejectsBadKeysAndMissingFile(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "provider.key")
	_ = os.WriteFile(keyFile, []byte("oldkeyoldkey"), 0o600)
	app := operatorApp(t, "op@example.com", nil, keyFile)
	for _, body := range []string{`{"key":"short"}`, `{"key":"has space inside"}`, `{"key":"tab\tinside12"}`, `{"key":"` + strings.Repeat("a", 513) + `"}`, `nope`} {
		if code, _ := put(t, app, body); code != http.StatusBadRequest {
			t.Errorf("%.20s: expected 400, got %d", body, code)
		}
	}
	if b, _ := os.ReadFile(keyFile); string(b) != "oldkeyoldkey" {
		t.Fatalf("rejected input changed the key: %q", b)
	}
	if code, _ := put(t, operatorApp(t, "op@example.com", nil, filepath.Join(t.TempDir(), "absent")), `{"key":"validkey1234"}`); code != http.StatusServiceUnavailable {
		t.Errorf("missing file: expected 503, got %d", code)
	}
	if code, _ := put(t, operatorApp(t, "op@example.com", nil, ""), `{"key":"validkey1234"}`); code != http.StatusServiceUnavailable {
		t.Errorf("no key file configured: expected 503, got %d", code)
	}
}
