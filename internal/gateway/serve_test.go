package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testTokenSecret = "0123456789abcdef0123456789abcdef"

// goodEnv is a complete, enabled configuration; tests override single keys.
func goodEnv(over map[string]string) func(string) string {
	env := map[string]string{
		"ATM_GATEWAY_ENABLED":              "true",
		"ATM_GATEWAY_FIREWALL_ATTESTATION": FirewallAttestation,
		"ATM_GATEWAY_LISTEN_ADDR":          "127.0.0.1:8082",
		"ATM_GATEWAY_UPSTREAM_URL":         "https://models.example.com",
		"ATM_GATEWAY_PROVIDER_KEY":         "sk-provider-secret",
		"ATM_GATEWAY_TOKEN_SECRET":         testTokenSecret,
		"ATM_GATEWAY_MODELS":               "gpt-a, gpt-b",
		"ATM_GATEWAY_TLS_CERT_FILE":        "/tls/cert.pem",
		"ATM_GATEWAY_TLS_KEY_FILE":         "/tls/key.pem",
		"ATM_GATEWAY_PRICE_FILE":           "/etc/atm/prices.json",
		"ATM_GATEWAY_QUOTA_USD":            "5.00",
	}
	for k, v := range over {
		if v == "-" {
			delete(env, k)
		} else {
			env[k] = v
		}
	}
	return func(k string) string { return env[k] }
}

var errNoFile = os.ErrNotExist

// noFiles serves no files; tests that need the TLS and price files use goodFiles(t).
func noFiles(string) ([]byte, error) { return nil, errNoFile }

func TestServiceFromEnv_DisabledByDefault(t *testing.T) {
	for _, v := range []string{"", "false"} {
		svc, err := ServiceFromEnv(goodEnv(map[string]string{"ATM_GATEWAY_ENABLED": v}), goodFiles(t), nil)
		if err != nil || svc != nil {
			t.Errorf("ENABLED=%q: want (nil,nil), got (%v,%v)", v, svc, err)
		}
	}
	// A nonsense switch is a configuration error, never silently "off" or "on".
	if _, err := ServiceFromEnv(goodEnv(map[string]string{"ATM_GATEWAY_ENABLED": "yes"}), goodFiles(t), nil); err == nil {
		t.Error("unrecognised ENABLED value must be an error")
	}
}

func TestServiceFromEnv_WarnsWithoutFirewallAttestation(t *testing.T) {
	for name, v := range map[string]string{"missing": "-", "empty": "", "wrong": "yes", "case": strings.ToUpper(FirewallAttestation), "prefix": "sandbox", "suffix": FirewallAttestation + "-x", "padded": " " + FirewallAttestation} {
		var logs []string
		logf := func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
		svc, err := ServiceFromEnv(goodEnv(map[string]string{"ATM_GATEWAY_FIREWALL_ATTESTATION": v}), goodFiles(t), logf)
		if err != nil || svc == nil {
			t.Errorf("%s: gateway must still start, got (%v,%v)", name, svc, err)
			continue
		}
		if len(logs) != 1 || !strings.Contains(logs[0], "WARNING") || !strings.Contains(logs[0], "firewall") {
			t.Errorf("%s: want exactly one firewall warning, got %q", name, logs)
		}
	}

	// The warning is never silent, even without a logger (it falls back to the standard log).
	if svc, err := ServiceFromEnv(goodEnv(map[string]string{"ATM_GATEWAY_FIREWALL_ATTESTATION": "-"}), goodFiles(t), nil); err != nil || svc == nil {
		t.Errorf("nil logger: gateway must still start, got (%v,%v)", svc, err)
	}

	// With the attestation there is nothing to warn about.
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	if _, err := ServiceFromEnv(goodEnv(nil), goodFiles(t), logf); err != nil || len(logs) != 0 {
		t.Errorf("attested start must be quiet, got err=%v logs=%q", err, logs)
	}
}

func TestServiceFromEnv_RejectsUnsafeListenAddr(t *testing.T) {
	for _, addr := range []string{"", "-", "0.0.0.0:8082", "[::]:8082", ":8082", "127.0.0.1", "127.0.0.1:abc", "127.0.0.1:0", "127.0.0.1:70000"} {
		if _, err := ServiceFromEnv(goodEnv(map[string]string{"ATM_GATEWAY_LISTEN_ADDR": addr}), goodFiles(t), nil); err == nil {
			t.Errorf("listen addr %q must be rejected", addr)
		}
	}
	svc, err := ServiceFromEnv(goodEnv(map[string]string{"ATM_GATEWAY_LISTEN_ADDR": "gateway.internal:8082"}), goodFiles(t), nil)
	if err != nil || svc.Addr != "gateway.internal:8082" {
		t.Errorf("explicit host must be accepted: %v %v", svc, err)
	}
}

func TestServiceFromEnv_RequiredSettings(t *testing.T) {
	for _, key := range []string{"ATM_GATEWAY_UPSTREAM_URL", "ATM_GATEWAY_PROVIDER_KEY", "ATM_GATEWAY_TOKEN_SECRET", "ATM_GATEWAY_MODELS"} {
		if _, err := ServiceFromEnv(goodEnv(map[string]string{key: "-"}), goodFiles(t), nil); err == nil {
			t.Errorf("missing %s must be an error", key)
		}
	}
	bad := map[string]string{
		"ATM_GATEWAY_UPSTREAM_URL": "http://models.example.com", // plain-http upstream
		"ATM_GATEWAY_TOKEN_SECRET": "short",
		"ATM_GATEWAY_MODELS":       " , ",
	}
	for key, v := range bad {
		if _, err := ServiceFromEnv(goodEnv(map[string]string{key: v}), goodFiles(t), nil); err == nil {
			t.Errorf("%s=%q must be rejected", key, v)
		}
	}
	for _, key := range []string{"ATM_GATEWAY_MAX_BODY_BYTES", "ATM_GATEWAY_MAX_OUTPUT_TOKENS", "ATM_GATEWAY_QUOTA_REQUESTS", "ATM_GATEWAY_QUOTA_TOKENS", "ATM_GATEWAY_IDLE_TIMEOUT"} {
		for _, v := range []string{"0", "-1", "abc"} {
			if _, err := ServiceFromEnv(goodEnv(map[string]string{key: v}), goodFiles(t), nil); err == nil {
				t.Errorf("%s=%q must be rejected", key, v)
			}
		}
	}
}

func TestServiceFromEnv_ProviderKeySources(t *testing.T) {
	both := goodEnv(map[string]string{"ATM_GATEWAY_PROVIDER_KEY_FILE": "/run/secrets/k"})
	if _, err := ServiceFromEnv(both, goodFiles(t), nil); err == nil {
		t.Error("key from both env and file is ambiguous and must be rejected")
	}

	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	if err := os.WriteFile(keyFile, []byte("sk-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := goodEnv(map[string]string{"ATM_GATEWAY_PROVIDER_KEY": "-", "ATM_GATEWAY_PROVIDER_KEY_FILE": keyFile})
	svc, err := ServiceFromEnv(env, func(p string) ([]byte, error) {
		if b, ferr := goodFiles(t)(p); ferr == nil {
			return b, nil
		}
		return os.ReadFile(p)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.Handler.cfg.Key(); got != "sk-from-file" {
		t.Errorf("file key must be trimmed, got %q", got)
	}
	// Rotation: the file is re-read per request, no restart.
	if err := os.WriteFile(keyFile, []byte("sk-rotated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := svc.Handler.cfg.Key(); got != "sk-rotated" {
		t.Errorf("rotated key not picked up, got %q", got)
	}

	// An unreadable or empty key file fails at startup, not at first request.
	if _, err := ServiceFromEnv(env, goodFiles(t), nil); err == nil {
		t.Error("unreadable key file must fail startup")
	}
	if err := os.WriteFile(keyFile, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceFromEnv(env, os.ReadFile, nil); err == nil {
		t.Error("empty key file must fail startup")
	}
}

func TestServiceFromEnv_BuildsHandlerWithSafeDefaults(t *testing.T) {
	svc, err := ServiceFromEnv(goodEnv(nil), goodFiles(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := svc.Handler.cfg
	if cfg.KeyHeader != "Authorization" || cfg.KeyPrefix != "Bearer " {
		t.Errorf("default key scheme: %q %q", cfg.KeyHeader, cfg.KeyPrefix)
	}
	if len(cfg.AllowedPaths) != 1 || cfg.AllowedPaths[0] != "/v1/chat/completions" {
		t.Errorf("only the OpenAI-compatible chat path may be forwarded, got %v", cfg.AllowedPaths)
	}
	if len(cfg.AllowedModels) != 2 || cfg.AllowedModels[0] != "gpt-a" || cfg.AllowedModels[1] != "gpt-b" {
		t.Errorf("models must be split and trimmed, got %v", cfg.AllowedModels)
	}
	if cfg.MaxBodyBytes <= 4096 || cfg.MaxOutputTokens <= 0 || cfg.IdleTimeout <= 0 {
		t.Errorf("limits: %+v", cfg)
	}
	if svc.Issuer == nil {
		t.Error("service must expose the issuer so the server can mint attempt tokens")
	}

	custom, err := ServiceFromEnv(goodEnv(map[string]string{"ATM_GATEWAY_KEY_HEADER": "x-api-key"}), goodFiles(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if h := custom.Handler.cfg; h.KeyHeader != "x-api-key" || h.KeyPrefix != "" {
		t.Errorf("custom header must not inherit the Bearer prefix: %q %q", h.KeyHeader, h.KeyPrefix)
	}
}

func TestServiceFromEnv_ErrorsNeverEchoSecrets(t *testing.T) {
	_, err := ServiceFromEnv(goodEnv(map[string]string{"ATM_GATEWAY_TOKEN_SECRET": "short-secret-value"}), goodFiles(t), nil)
	if err == nil || strings.Contains(err.Error(), "short-secret-value") || strings.Contains(err.Error(), "sk-provider-secret") {
		t.Errorf("error must not contain secrets: %v", err)
	}
}

func TestService_RunServesAndShutsDown(t *testing.T) {
	svc, err := ServiceFromEnv(goodEnv(map[string]string{"ATM_GATEWAY_LISTEN_ADDR": "127.0.0.1:8082"}), goodFiles(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(nil)
	ln := ts.Listener
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx, ln) }()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: validPKI(t).pool}}, Timeout: 5 * time.Second}
	resp, err := client.Post("https://"+ln.Addr().String()+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated request must get 401 from the gateway handler, got %d", resp.StatusCode)
	}

	// PROBE-18: plain HTTP is refused and never reaches the handler.
	plain, err := (&http.Client{Timeout: 5 * time.Second}).Post("http://"+ln.Addr().String()+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
	if err == nil {
		defer plain.Body.Close()
		if plain.StatusCode != http.StatusBadRequest {
			t.Errorf("plain HTTP must be rejected, got %d", plain.StatusCode)
		}
	}

	// TLS 1.2 is refused.
	old := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: validPKI(t).pool, MaxVersion: tls.VersionTLS12}}, Timeout: 5 * time.Second}
	if r, err := old.Post("https://"+ln.Addr().String()+"/v1/chat/completions", "application/json", strings.NewReader(`{}`)); err == nil {
		r.Body.Close()
		t.Error("a TLS 1.2 client must be refused")
	}

	// A client that does not trust the gateway's CA is refused.
	if r, err := (&http.Client{Timeout: 5 * time.Second}).Post("https://"+ln.Addr().String()+"/v1/chat/completions", "application/json", strings.NewReader(`{}`)); err == nil {
		r.Body.Close()
		t.Error("a client without the gateway CA must fail verification")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("clean shutdown must return nil, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestService_RunReportsListenerFailure(t *testing.T) {
	svc, err := ServiceFromEnv(goodEnv(nil), goodFiles(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(nil)
	ln := ts.Listener
	_ = ln.Close()
	if err := svc.Run(context.Background(), ln); err == nil || errors.Is(err, http.ErrServerClosed) {
		t.Errorf("a dead listener must surface as an error, got %v", err)
	}
}

func TestService_RunRefusesWithoutTLS(t *testing.T) {
	svc, err := ServiceFromEnv(goodEnv(nil), goodFiles(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	svc.TLS = nil
	ln := httptest.NewUnstartedServer(nil).Listener
	defer ln.Close()
	if err := svc.Run(context.Background(), ln); err == nil {
		t.Error("Run must refuse to serve plain HTTP")
	}
}

func TestServiceFromEnv_TLSAndSpendSettings(t *testing.T) {
	expired := newTestPKI(t, time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour))
	future := newTestPKI(t, time.Now().Add(time.Hour), time.Now().Add(48*time.Hour))
	good := validPKI(t)
	other := newTestPKI(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	files := func(over map[string][]byte) func(string) ([]byte, error) {
		base := goodFiles(t)
		return func(p string) ([]byte, error) {
			if b, ok := over[p]; ok {
				return b, nil
			}
			return base(p)
		}
	}
	cases := map[string]struct {
		env   map[string]string
		files map[string][]byte
	}{
		"no cert setting":        {env: map[string]string{"ATM_GATEWAY_TLS_CERT_FILE": "-"}},
		"no key setting":         {env: map[string]string{"ATM_GATEWAY_TLS_KEY_FILE": "-"}},
		"cert unreadable":        {env: map[string]string{"ATM_GATEWAY_TLS_CERT_FILE": "/nope"}},
		"key unreadable":         {env: map[string]string{"ATM_GATEWAY_TLS_KEY_FILE": "/nope"}},
		"garbage cert":           {files: map[string][]byte{"/tls/cert.pem": []byte("junk")}},
		"mismatched key":         {files: map[string][]byte{"/tls/key.pem": other.keyPEM}},
		"expired cert":           {files: map[string][]byte{"/tls/cert.pem": expired.certPEM, "/tls/key.pem": expired.keyPEM}},
		"not yet valid cert":     {files: map[string][]byte{"/tls/cert.pem": future.certPEM, "/tls/key.pem": future.keyPEM}},
		"no price file setting":  {env: map[string]string{"ATM_GATEWAY_PRICE_FILE": "-"}},
		"price file unreadable":  {env: map[string]string{"ATM_GATEWAY_PRICE_FILE": "/nope"}},
		"unpriced allowed model": {files: map[string][]byte{"/etc/atm/prices.json": []byte(`{"models":{"gpt-a":{"input_usd_per_mtok":"1","output_usd_per_mtok":"2"}}}`)}},
		"no usd quota":           {env: map[string]string{"ATM_GATEWAY_QUOTA_USD": "-"}},
		"zero usd quota":         {env: map[string]string{"ATM_GATEWAY_QUOTA_USD": "0"}},
		"negative usd quota":     {env: map[string]string{"ATM_GATEWAY_QUOTA_USD": "-1"}},
		"too many decimals":      {env: map[string]string{"ATM_GATEWAY_QUOTA_USD": "1.0000001"}},
		"exponent usd quota":     {env: map[string]string{"ATM_GATEWAY_QUOTA_USD": "1e3"}},
	}
	for name, c := range cases {
		if _, err := ServiceFromEnv(goodEnv(c.env), files(c.files), nil); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), string(good.keyPEM)) || strings.Contains(err.Error(), "BEGIN") {
			t.Errorf("%s: error leaks key material: %v", name, err)
		}
	}
	svc, err := ServiceFromEnv(goodEnv(nil), files(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if svc.TLS == nil || svc.TLS.MinVersion != tls.VersionTLS13 {
		t.Error("service must carry a TLS 1.3 config")
	}
	if svc.Ledger.quota.MaxMicroUSD != 5_000_000 {
		t.Errorf("5.00 USD must be 5,000,000 micro-USD, got %d", svc.Ledger.quota.MaxMicroUSD)
	}
}
