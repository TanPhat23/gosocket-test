package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Config fully describes one upstream model provider. Every field is required unless noted;
// NewHandler fails closed on anything missing.
type Config struct {
	UpstreamBaseURL string        // https only; the sandbox can never choose a destination
	KeyHeader       string        // header that carries the provider key, e.g. "x-api-key"
	KeyPrefix       string        // optional prefix, e.g. "Bearer "
	Key             func() string // read on every request so rotation needs no restart
	AllowedPaths    []string      // exact request paths forwarded upstream
	AllowedModels   []string
	MaxBodyBytes    int64
	MaxOutputTokens int64
	IdleTimeout     time.Duration // aborts an upstream that sends nothing for this long
	Issuer          *Issuer
	Ledger          *Ledger
	Transport       http.RoundTripper // optional; defaults to a proxy-less clone of the default transport
	Logf            func(format string, args ...any)
}

// Handler authenticates sandbox requests with a capability token, validates them, and forwards
// them to the fixed upstream with the provider key injected.
type Handler struct {
	cfg    Config
	client *http.Client
	base   *url.URL
}

var forwardedHeaders = []string{"Content-Type", "Accept", "Anthropic-Version", "Anthropic-Beta"}

// tokenLimitFields are clamped when present; max_tokens is always set.
var tokenLimitFields = []string{"max_completion_tokens", "max_output_tokens"}

func NewHandler(cfg Config) (*Handler, error) {
	base, err := url.Parse(cfg.UpstreamBaseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return nil, errors.New("gateway: upstream must be an https URL")
	}
	switch {
	case cfg.Key == nil, cfg.KeyHeader == "", cfg.Issuer == nil, cfg.Ledger == nil,
		len(cfg.AllowedPaths) == 0, len(cfg.AllowedModels) == 0,
		cfg.MaxBodyBytes <= 0, cfg.MaxOutputTokens <= 0, cfg.IdleTimeout <= 0:
		return nil, errors.New("gateway: incomplete configuration")
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	rt := cfg.Transport
	if rt == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = nil
		rt = t
	}
	return &Handler{
		cfg:  cfg,
		base: base,
		client: &http.Client{
			Transport:     rt,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !slices.Contains(h.cfg.AllowedPaths, r.URL.Path) || r.URL.RawQuery != "" {
		fail(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, err := h.cfg.Issuer.Verify(callerToken(r))
	if err != nil {
		h.cfg.Logf("gateway: rejected unauthenticated request")
		fail(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.cfg.MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, http.StatusRequestEntityTooLarge, "request too large")
		} else {
			fail(w, http.StatusBadRequest, "bad request")
		}
		return
	}
	body, outTokens, status, msg := h.prepare(raw)
	if status != 0 {
		fail(w, status, msg)
		return
	}
	if err := h.cfg.Ledger.Reserve(claims.AttemptID, int64(len(body))+outTokens); err != nil {
		h.cfg.Logf("gateway: quota exceeded project=%s attempt=%s", claims.ProjectID, claims.AttemptID)
		fail(w, http.StatusTooManyRequests, "quota exceeded")
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	idle := time.AfterFunc(h.cfg.IdleTimeout, cancel)
	defer idle.Stop()

	target := *h.base
	target.Path = strings.TrimRight(h.base.Path, "/") + r.URL.Path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		fail(w, http.StatusBadGateway, "upstream error")
		return
	}
	for _, name := range forwardedHeaders {
		if v := r.Header.Get(name); v != "" {
			req.Header.Set(name, v)
		}
	}
	req.Header.Set(h.cfg.KeyHeader, h.cfg.KeyPrefix+h.cfg.Key())

	resp, err := h.client.Do(req)
	if err != nil {
		h.cfg.Logf("gateway: upstream transport error project=%s attempt=%s", claims.ProjectID, claims.AttemptID)
		fail(w, http.StatusBadGateway, "upstream error")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		h.cfg.Logf("gateway: upstream status=%d project=%s attempt=%s", resp.StatusCode, claims.ProjectID, claims.AttemptID)
		if resp.StatusCode == http.StatusTooManyRequests {
			fail(w, http.StatusTooManyRequests, "upstream rate limited")
		} else {
			fail(w, http.StatusBadGateway, "upstream error")
		}
		return
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		idle.Reset(h.cfg.IdleTimeout)
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

// prepare validates the JSON body and clamps output-token limits. It returns a non-zero status on
// rejection, and otherwise the clamped max_tokens used for the quota reservation.
func (h *Handler) prepare(raw []byte) ([]byte, int64, int, string) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, 0, http.StatusBadRequest, "bad request"
	}
	var model string
	if err := json.Unmarshal(obj["model"], &model); err != nil || !slices.Contains(h.cfg.AllowedModels, model) {
		return nil, 0, http.StatusForbidden, "model not allowed"
	}
	limit := h.cfg.MaxOutputTokens
	var outTokens int64
	for _, field := range append([]string{"max_tokens"}, tokenLimitFields...) {
		v, present := obj[field]
		if !present && field != "max_tokens" {
			continue
		}
		var n float64
		if present && json.Unmarshal(v, &n) != nil {
			return nil, 0, http.StatusBadRequest, "bad request"
		}
		if !present || n <= 0 || n > float64(limit) {
			n = float64(limit)
		}
		obj[field], _ = json.Marshal(int64(n))
		if field == "max_tokens" {
			outTokens = int64(n)
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, 0, http.StatusBadRequest, "bad request"
	}
	return out, outTokens, 0, ""
}

func callerToken(r *http.Request) string {
	if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return v
	}
	return r.Header.Get("x-api-key")
}

func fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
