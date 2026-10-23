package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testKey  = "sk-upstream-SECRET-123"
	testPath = "/v1/messages"
)

type upstreamCall struct {
	path   string
	header http.Header
	body   map[string]any
}

type fixture struct {
	t       *testing.T
	gw      *Handler
	issuer  *Issuer
	ledger  *Ledger
	calls   atomic.Int64
	last    atomic.Pointer[upstreamCall]
	logs    *bytes.Buffer
	handler http.HandlerFunc // upstream behaviour
	srv     *httptest.Server
}

func newFixture(t *testing.T, quota Quota) *fixture {
	t.Helper()
	f := &fixture{t: t, logs: &bytes.Buffer{}}
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.last.Store(&upstreamCall{path: r.URL.Path, header: r.Header.Clone(), body: body})
		f.handler(w, r)
	}))
	t.Cleanup(f.srv.Close)
	is, err := NewIssuer(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	f.issuer = is
	if quota.MaxMicroUSD == 0 {
		quota.MaxMicroUSD = 1_000_000_000_000
	}
	f.ledger = NewLedger(quota)
	h, err := NewHandler(Config{
		UpstreamBaseURL: f.srv.URL,
		Transport:       f.srv.Client().Transport,
		KeyHeader:       "x-api-key",
		Key:             func() string { return testKey },
		AllowedPaths:    []string{testPath},
		AllowedModels:   []string{"model-ok"},
		MaxBodyBytes:    4096,
		MaxOutputTokens: 100,
		IdleTimeout:     2 * time.Second,
		Issuer:          is,
		Ledger:          f.ledger,
		Pricing:         testPricing,
		Logf:            func(format string, args ...any) { fmt.Fprintf(f.logs, format+"\n", args...) },
	})
	if err != nil {
		t.Fatal(err)
	}
	f.gw = h
	return f
}

func (f *fixture) token(project, attempt string) string {
	return f.issuer.Mint(Claims{ProjectID: project, TaskID: "t", AttemptID: attempt}, time.Hour)
}

func (f *fixture) do(token, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, testPath, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.gw.ServeHTTP(rec, req)
	return rec
}

// testPricing: model-ok costs 1 micro-USD per input token and 2 per output token.
var testPricing = Pricing{"model-ok": {Input: 1_000_000, Output: 2_000_000}}

var goodBody = `{"model":"model-ok","max_tokens":50,"messages":[]}`

func bigQuota() Quota { return Quota{MaxRequests: 100, MaxTokens: 100000} }

func TestNewHandler_FailsClosedOnBadConfig(t *testing.T) {
	is, _ := NewIssuer(testSecret)
	base := Config{
		UpstreamBaseURL: "https://api.example.com", KeyHeader: "x-api-key", Key: func() string { return "k" },
		AllowedPaths: []string{testPath}, AllowedModels: []string{"m"}, MaxBodyBytes: 1, MaxOutputTokens: 1,
		IdleTimeout: time.Second, Issuer: is, Ledger: NewLedger(bigQuota()), Pricing: Pricing{"m": {Input: 1_000_000, Output: 1_000_000}},
	}
	if _, err := NewHandler(base); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	mut := map[string]func(*Config){
		"plaintext upstream": func(c *Config) { c.UpstreamBaseURL = "http://api.example.com" },
		"no key func":        func(c *Config) { c.Key = nil },
		"no models":          func(c *Config) { c.AllowedModels = nil },
		"no paths":           func(c *Config) { c.AllowedPaths = nil },
		"no issuer":          func(c *Config) { c.Issuer = nil },
		"no ledger":          func(c *Config) { c.Ledger = nil },
		"no pricing":         func(c *Config) { c.Pricing = nil },
		"model unpriced":     func(c *Config) { c.AllowedModels = []string{"m", "other"} },
		"no body cap":        func(c *Config) { c.MaxBodyBytes = 0 },
		"no token cap":       func(c *Config) { c.MaxOutputTokens = 0 },
		"no idle timeout":    func(c *Config) { c.IdleTimeout = 0 },
	}
	for name, m := range mut {
		c := base
		m(&c)
		if _, err := NewHandler(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestHandler_RejectsUnauthenticated(t *testing.T) {
	f := newFixture(t, bigQuota())
	good := f.token("p", "a")
	cases := map[string]string{"missing": "", "forged": good + "x", "garbage": "abc"}
	for name, tok := range cases {
		if rec := f.do(tok, goodBody, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: code %d", name, rec.Code)
		}
	}
	f.issuer.Revoke("a")
	if rec := f.do(good, goodBody, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked: code %d", rec.Code)
	}
	if f.calls.Load() != 0 {
		t.Errorf("%d upstream calls for rejected requests", f.calls.Load())
	}
}

func TestHandler_AcceptsTokenInXAPIKey(t *testing.T) {
	f := newFixture(t, bigQuota())
	rec := f.do("", goodBody, map[string]string{"x-api-key": f.token("p", "a")})
	if rec.Code != http.StatusOK {
		t.Errorf("code %d body %s", rec.Code, rec.Body)
	}
}

func TestHandler_InjectsKeyAndStripsCallerCredentials(t *testing.T) {
	f := newFixture(t, bigQuota())
	tok := f.token("p", "a")
	rec := f.do(tok, goodBody, map[string]string{
		"Proxy-Authorization": "Basic zzz", "Cookie": "c=1", "X-Forwarded-For": "1.2.3.4",
		"Anthropic-Version": "2023-06-01", "Content-Type": "application/json",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d %s", rec.Code, rec.Body)
	}
	c := f.last.Load()
	if c.header.Get("x-api-key") != testKey {
		t.Errorf("upstream key = %q", c.header.Get("x-api-key"))
	}
	for _, h := range []string{"Authorization", "Proxy-Authorization", "Cookie", "X-Forwarded-For"} {
		if c.header.Get(h) != "" {
			t.Errorf("header %s forwarded upstream: %q", h, c.header.Get(h))
		}
	}
	if c.header.Get("Anthropic-Version") != "2023-06-01" {
		t.Error("allowlisted anthropic-version dropped")
	}
	if strings.Contains(rec.Body.String(), testKey) || strings.Contains(rec.Header().Get("x-api-key"), testKey) {
		t.Error("provider key reflected to client")
	}
}

func TestHandler_BearerKeyScheme(t *testing.T) {
	f := newFixture(t, bigQuota())
	f.gw.cfg.KeyHeader = "Authorization"
	f.gw.cfg.KeyPrefix = "Bearer "
	f.do(f.token("p", "a"), goodBody, nil)
	if got := f.last.Load().header.Get("Authorization"); got != "Bearer "+testKey {
		t.Errorf("Authorization = %q", got)
	}
}

func TestHandler_ValidatesRequest(t *testing.T) {
	f := newFixture(t, bigQuota())
	tok := f.token("p", "a")
	cases := []struct {
		name string
		body string
		code int
	}{
		{"unknown model", `{"model":"gpt-evil","max_tokens":5}`, http.StatusForbidden},
		{"missing model", `{"max_tokens":5}`, http.StatusForbidden},
		{"model not string", `{"model":["model-ok"],"max_tokens":5}`, http.StatusForbidden},
		{"not json", `nope`, http.StatusBadRequest},
		{"json array", `[]`, http.StatusBadRequest},
		{"too large", `{"model":"model-ok","pad":"` + strings.Repeat("x", 5000) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		if rec := f.do(tok, c.body, nil); rec.Code != c.code {
			t.Errorf("%s: code %d, want %d", c.name, rec.Code, c.code)
		}
	}
	if f.calls.Load() != 0 {
		t.Errorf("%d upstream calls for invalid requests", f.calls.Load())
	}
}

func TestHandler_RejectsWrongMethodAndPath(t *testing.T) {
	f := newFixture(t, bigQuota())
	tok := f.token("p", "a")
	for _, p := range []string{"/v1/other", "/v1/messages/../admin", "http://169.254.169.254/latest", "/v1/messages?x=1"} {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(goodBody))
		req.URL.Path, req.URL.RawQuery = p, ""
		if i := strings.Index(p, "?"); i >= 0 {
			req.URL.Path, req.URL.RawQuery = p[:i], p[i+1:]
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		f.gw.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
			t.Errorf("path %q: code %d", p, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, testPath, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	f.gw.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: code %d", rec.Code)
	}
	if f.calls.Load() != 0 {
		t.Errorf("%d upstream calls", f.calls.Load())
	}
}

func TestHandler_UpstreamDestinationIsFixed(t *testing.T) {
	f := newFixture(t, bigQuota())
	f.do(f.token("p", "a"), goodBody, map[string]string{"Host": "evil.example", "X-Target-Url": "http://169.254.169.254"})
	if c := f.last.Load(); c == nil || c.path != testPath {
		t.Fatalf("upstream call = %+v", c)
	}
}

func TestHandler_ClampsMaxTokens(t *testing.T) {
	f := newFixture(t, bigQuota())
	tok := f.token("p", "a")
	for name, body := range map[string]string{
		"too big": `{"model":"model-ok","max_tokens":999999}`,
		"missing": `{"model":"model-ok"}`,
	} {
		f.do(tok, body, nil)
		if got, _ := f.last.Load().body["max_tokens"].(float64); got != 100 {
			t.Errorf("%s: forwarded max_tokens = %v, want 100", name, got)
		}
	}
	f.do(tok, goodBody, nil)
	if got, _ := f.last.Load().body["max_tokens"].(float64); got != 50 {
		t.Errorf("in-range max_tokens changed to %v", got)
	}
}

func TestHandler_EnforcesQuotaBeforeUpstream(t *testing.T) {
	f := newFixture(t, Quota{MaxRequests: 2, MaxTokens: 100000})
	tok := f.token("p", "a")
	for i := 0; i < 2; i++ {
		if rec := f.do(tok, goodBody, nil); rec.Code != http.StatusOK {
			t.Fatalf("request %d: %d", i, rec.Code)
		}
	}
	if rec := f.do(tok, goodBody, nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("over quota: %d", rec.Code)
	}
	if f.calls.Load() != 2 {
		t.Errorf("upstream calls = %d, want 2", f.calls.Load())
	}
	// another attempt is unaffected
	if rec := f.do(f.token("p", "b"), goodBody, nil); rec.Code != http.StatusOK {
		t.Errorf("other attempt: %d", rec.Code)
	}
}

func TestHandler_TokenQuotaCountsBodyAndOutput(t *testing.T) {
	// goodBody is ~50 bytes + 50 max_tokens: a 120-token budget fits once, not twice.
	f := newFixture(t, Quota{MaxRequests: 10, MaxTokens: 120})
	tok := f.token("p", "a")
	if rec := f.do(tok, goodBody, nil); rec.Code != http.StatusOK {
		t.Fatalf("first: %d", rec.Code)
	}
	if rec := f.do(tok, goodBody, nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("second: %d", rec.Code)
	}
}

func TestHandler_RedirectIsTerminal(t *testing.T) {
	f := newFixture(t, bigQuota())
	var hit atomic.Int64
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Add(1)
	}))
	defer evil.Close()
	for _, code := range []int{301, 302, 307, 308} {
		f.handler = func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, evil.URL+"/steal", code)
		}
		rec := f.do(f.token("p", "a"+string(rune('0'+code%10))), goodBody, nil)
		if rec.Code != http.StatusBadGateway {
			t.Errorf("%d: client saw %d", code, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Errorf("%d: Location leaked: %q", code, loc)
		}
	}
	if hit.Load() != 0 {
		t.Errorf("redirect target contacted %d times (key would have been forwarded)", hit.Load())
	}
}

func TestHandler_SanitizesUpstreamErrors(t *testing.T) {
	f := newFixture(t, bigQuota())
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"key ` + testKey + ` over limit; prompt was: TOPSECRETPROMPT"}`))
	}
	rec := f.do(f.token("p", "a"), `{"model":"model-ok","max_tokens":5,"messages":"TOPSECRETPROMPT"}`, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("code %d", rec.Code)
	}
	for _, leak := range []string{testKey, "TOPSECRETPROMPT"} {
		if strings.Contains(rec.Body.String(), leak) || strings.Contains(f.logs.String(), leak) {
			t.Errorf("%q leaked in body or logs", leak)
		}
	}
	f.handler = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }
	if rec := f.do(f.token("p", "b"), goodBody, nil); rec.Code != http.StatusBadGateway {
		t.Errorf("5xx mapped to %d, want 502", rec.Code)
	}
}

func TestHandler_LogsNeverContainSecretsOrPrompts(t *testing.T) {
	f := newFixture(t, bigQuota())
	tok := f.token("p", "a")
	f.do(tok, `{"model":"model-ok","max_tokens":5,"messages":"TOPSECRETPROMPT"}`, nil)
	f.do("bad"+tok, goodBody, nil)
	for _, leak := range []string{testKey, tok, "TOPSECRETPROMPT"} {
		if strings.Contains(f.logs.String(), leak) {
			t.Errorf("%q in logs:\n%s", leak, f.logs)
		}
	}
}

func TestHandler_StreamsAndPropagatesClientAbort(t *testing.T) {
	f := newFixture(t, bigQuota())
	cancelled := make(chan struct{})
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		close(cancelled)
	}
	srv := httptest.NewServer(f.gw)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+testPath, strings.NewReader(goodBody))
	req.Header.Set("Authorization", "Bearer "+f.token("p", "a"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 11)
	if _, err := io.ReadFull(resp.Body, buf); err != nil || string(buf) != "data: one\n\n" {
		t.Fatalf("first chunk = %q, %v", buf, err)
	}
	resp.Body.Close() // client abort
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request not cancelled after client abort")
	}
}

func TestHandler_IdleTimeoutAbortsStalledUpstream(t *testing.T) {
	f := newFixture(t, bigQuota())
	f.gw.cfg.IdleTimeout = 150 * time.Millisecond
	cancelled := make(chan struct{})
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		close(cancelled)
	}
	done := make(chan struct{})
	go func() { f.do(f.token("p", "a"), goodBody, nil); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not abort a stalled stream")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("upstream not cancelled")
	}
}

func TestHandler_ActiveStreamOutlivesIdleTimeout(t *testing.T) {
	f := newFixture(t, bigQuota())
	f.gw.cfg.IdleTimeout = 200 * time.Millisecond
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 8; i++ {
			_, _ = io.WriteString(w, "data: x\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(80 * time.Millisecond) // total 640ms > idle timeout, but never idle
		}
	}
	rec := f.do(f.token("p", "a"), goodBody, nil)
	if got := strings.Count(rec.Body.String(), "data: x"); got != 8 {
		t.Errorf("received %d/8 chunks; an active stream was cut off", got)
	}
}

// spend: model-ok costs 1 micro-USD per input token and 2 per output token (testPricing).

func (f *fixture) spent(attempt string) int64 {
	f.ledger.mu.Lock()
	defer f.ledger.mu.Unlock()
	return f.ledger.used[attempt].micro
}

func spendQuota(micro int64) Quota {
	return Quota{MaxRequests: 100, MaxTokens: 1_000_000, MaxMicroUSD: micro}
}

func TestHandler_SpendReservationIsUpperBound(t *testing.T) {
	f := newFixture(t, spendQuota(1_000_000))
	f.handler = func(w http.ResponseWriter, r *http.Request) { // reports no usage: the bound stands
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
	body := `{"model":"model-ok","max_tokens":50,"messages":[]}`
	if rec := f.do(f.token("p", "a"), body, nil); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	sent := f.last.Load()
	_ = sent
	// bound = len(body as sent) input tokens * 1 + 50 output tokens * 2; the sent body is re-marshalled.
	got := f.spent("a")
	if got < int64(len(`{"max_tokens":50,"messages":[],"model":"model-ok"}`))+100 {
		t.Errorf("reservation %d is below the conservative bound", got)
	}
}

func TestHandler_SpendCapRejectsBeforeUpstream(t *testing.T) {
	f := newFixture(t, spendQuota(150)) // one request reserves ~ body bytes + 100
	tok := f.token("p", "a")
	if rec := f.do(tok, goodBody, nil); rec.Code != 200 {
		t.Fatalf("first request: %d", rec.Code)
	}
	calls := f.calls.Load()
	rec := f.do(tok, goodBody, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request must exceed the cap, got %d", rec.Code)
	}
	if f.calls.Load() != calls {
		t.Error("a request over the cap must never reach the provider")
	}
}

func TestHandler_SettlesToReportedUsage_NonStreaming(t *testing.T) {
	f := newFixture(t, spendQuota(1_000_000))
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}
	if rec := f.do(f.token("p", "a"), goodBody, nil); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := f.spent("a"); got != 10*1+5*2 {
		t.Errorf("spend must settle to 10 in + 5 out = 20 micro-USD, got %d", got)
	}
}

func TestHandler_SettlesToReportedUsage_Streaming(t *testing.T) {
	f := newFixture(t, spendQuota(1_000_000))
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, ev := range []string{
			`data: {"choices":[{"delta":{"content":"hi"}}]}`,
			``,
			`data: {"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3}}`,
			``,
			`data: [DONE]`, // no trailing blank line or newline-terminated final event is also fine
		} {
			_, _ = w.Write([]byte(ev + "\n"))
			fl.Flush()
		}
	}
	body := `{"model":"model-ok","max_tokens":50,"stream":true,"messages":[]}`
	if rec := f.do(f.token("p", "a"), body, nil); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := f.spent("a"); got != 7+3*2 {
		t.Errorf("spend must settle to the streamed usage (13), got %d", got)
	}
	opts, _ := f.last.Load().body["stream_options"].(map[string]any)
	if opts["include_usage"] != true {
		t.Errorf("streaming requests must ask the provider for usage, sent %v", f.last.Load().body["stream_options"])
	}
}

func TestHandler_StreamOptionsMergeKeepsCallerFields(t *testing.T) {
	f := newFixture(t, spendQuota(1_000_000))
	body := `{"model":"model-ok","stream":true,"stream_options":{"include_usage":false,"keep":1},"messages":[]}`
	if rec := f.do(f.token("p", "a"), body, nil); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	opts, _ := f.last.Load().body["stream_options"].(map[string]any)
	if opts["include_usage"] != true || opts["keep"] != float64(1) {
		t.Errorf("include_usage must be forced on and other fields kept, got %v", opts)
	}
}

func TestHandler_ProviderErrorRefundsSpend(t *testing.T) {
	f := newFixture(t, spendQuota(1_000_000))
	f.handler = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }
	if rec := f.do(f.token("p", "a"), goodBody, nil); rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d", rec.Code)
	}
	if got := f.spent("a"); got != 0 {
		t.Errorf("a provider error generated nothing and must refund the reservation, kept %d", got)
	}
}

func TestHandler_NoReportedUsageKeepsConservativeBound(t *testing.T) {
	f := newFixture(t, spendQuota(1_000_000))
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}
	if rec := f.do(f.token("p", "a"), goodBody, nil); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := f.spent("a"); got < 100 {
		t.Errorf("without usage the full bound must stay charged, got %d", got)
	}
}

func TestHandler_ProviderCannotUnderstateBeyondItsReport(t *testing.T) {
	// A provider reporting more than the reservation is charged in full (it exceeded the bound).
	f := newFixture(t, spendQuota(1_000_000))
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usage":{"prompt_tokens":1000,"completion_tokens":1000}}`))
	}
	if rec := f.do(f.token("p", "a"), goodBody, nil); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := f.spent("a"); got != 1000+2000 {
		t.Errorf("reported usage above the bound must be charged in full (3000), got %d", got)
	}
}

func TestHandler_ConcurrentRequestsCannotOverspend(t *testing.T) {
	// Each request reserves about len(body)+100 micro-USD. A cap of 6 reservations admits exactly 6.
	one := int64(len(`{"max_tokens":50,"messages":[],"model":"model-ok"}`)) + 100
	f := newFixture(t, spendQuota(6*one))
	release := make(chan struct{})
	f.handler = func(w http.ResponseWriter, r *http.Request) { <-release; w.WriteHeader(200) }
	tok := f.token("p", "a")
	var wg sync.WaitGroup
	var ok, limited atomic.Int64
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch f.do(tok, goodBody, nil).Code {
			case 200:
				ok.Add(1)
			case http.StatusTooManyRequests:
				limited.Add(1)
			}
		}()
	}
	for f.calls.Load() < 6 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	if ok.Load() != 6 || limited.Load() != 4 {
		t.Errorf("want 6 admitted and 4 limited, got %d and %d", ok.Load(), limited.Load())
	}
}

func TestHandler_AbortsRunawayResponse(t *testing.T) {
	f := newFixture(t, spendQuota(1_000_000))
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := []byte(strings.Repeat("x", 4096))
		for i := 0; i < 200; i++ { // 800 KiB against a limit of 100*1024 + 64 KiB
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}
	rec := f.do(f.token("p", "a"), goodBody, nil)
	if got := rec.Body.Len(); got > 100*1024+64*1024+4096 {
		t.Errorf("a response past the output ceiling must be cut off, relayed %d bytes", got)
	}
	if !strings.Contains(f.logs.String(), "runaway") {
		t.Error("the abort must be logged")
	}
}

func TestUsageScanner_SSEEdgeCases(t *testing.T) {
	u := newUsageScanner(true)
	// event split across writes, CRLF line endings, an over-long line, and a final event with no newline
	for _, p := range []string{"data: {\"usage\":{\"prompt_tokens\":", "4,\"completion_tokens\":2}}\r\n\r\n", "data: " + strings.Repeat("a", maxSSELine+5) + "\n", `data: {"usage":{"prompt_tokens":9,"completion_tokens":9}}`} {
		u.write([]byte(p))
	}
	in, out, ok := u.usage()
	if !ok || in != 9 || out != 9 {
		t.Errorf("want last usage 9/9, got %d/%d ok=%v", in, out, ok)
	}
	if _, _, ok := newUsageScanner(true).usage(); ok {
		t.Error("no usage seen must report not found")
	}
	z := newUsageScanner(false)
	z.write([]byte(`{"usage":{"prompt_tokens":0,"completion_tokens":0}}`))
	if _, _, ok := z.usage(); ok {
		t.Error("all-zero usage must not count as reported")
	}
}
