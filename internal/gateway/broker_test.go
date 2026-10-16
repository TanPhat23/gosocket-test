package gateway

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func newTestBroker(t *testing.T) (*Broker, *Service) {
	t.Helper()
	svc, err := ServiceFromEnv(goodEnv(nil), noFiles, func(string, ...any) {})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.NewBroker("http://172.29.0.2:8082/v1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return b, svc
}

func TestNewBroker_RejectsBadConfig(t *testing.T) {
	svc, _ := ServiceFromEnv(goodEnv(nil), noFiles, func(string, ...any) {})
	for name, c := range map[string]struct {
		url string
		ttl time.Duration
	}{
		"empty url":    {"", time.Hour},
		"no host":      {"http:///v1", time.Hour},
		"bad scheme":   {"ftp://gw/v1", time.Hour},
		"credentials":  {"http://u:p@gw/v1", time.Hour},
		"query":        {"http://gw/v1?x=1", time.Hour},
		"zero ttl":     {"http://gw/v1", 0},
		"negative ttl": {"http://gw/v1", -time.Second},
	} {
		if _, err := svc.NewBroker(c.url, c.ttl); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
	var nilSvc *Service
	if _, err := nilSvc.NewBroker("http://gw/v1", time.Hour); err == nil {
		t.Error("a disabled (nil) gateway cannot issue grants")
	}
}

func TestBroker_GrantMintsLeaseBoundToken(t *testing.T) {
	b, svc := newTestBroker(t)
	g, err := b.Grant("proj-1", "lease-1")
	if err != nil {
		t.Fatal(err)
	}
	if g.BaseURL != "http://172.29.0.2:8082/v1" || g.Token == "" {
		t.Fatalf("unexpected grant: %+v", g)
	}
	claims, err := svc.Issuer.Verify(g.Token)
	if err != nil {
		t.Fatalf("granted token must verify: %v", err)
	}
	if claims.ProjectID != "proj-1" || claims.TaskID != projectScope || claims.AttemptID != "lease-1" {
		t.Errorf("claims must bind the lease, got %+v", claims)
	}
	if exp := time.Unix(claims.Expires, 0); time.Until(exp) < 59*time.Minute || time.Until(exp) > 61*time.Minute {
		t.Errorf("token must expire after the configured ttl, got %v", exp)
	}

	other, _ := b.Grant("proj-1", "lease-2")
	if other.Token == g.Token {
		t.Error("each lease needs its own token")
	}
	for _, args := range [][2]string{{"", "l"}, {"p", ""}} {
		if _, err := b.Grant(args[0], args[1]); err == nil {
			t.Errorf("Grant%v must reject empty ids", args)
		}
	}
}

func TestBroker_ReleaseRevokesAndFreesQuota(t *testing.T) {
	b, svc := newTestBroker(t)
	g, _ := b.Grant("p", "att-r")
	other, _ := b.Grant("p", "att-keep")

	if err := svc.Ledger.Reserve("att-r", 10); err != nil {
		t.Fatal(err)
	}
	b.Release("att-r")
	if _, err := svc.Issuer.Verify(g.Token); err == nil {
		t.Error("released attempt's token must stop verifying")
	}
	if _, err := svc.Issuer.Verify(other.Token); err != nil {
		t.Errorf("releasing one attempt must not affect another: %v", err)
	}
	if got := svc.Ledger.used["att-r"]; got != (usage{}) {
		t.Errorf("ledger entry must be forgotten, got %+v", got)
	}
	b.Release("att-r") // idempotent
	b.Release("never-granted")
	b.Release("")
}

func TestOpenCodeConfig(t *testing.T) {
	raw, err := OpenCodeConfig(OpenCodeConfigParams{
		BaseURL: "http://172.29.0.2:8082/v1", Token: "tok.sig", Models: []string{"gpt-a", "gpt-b"},
		ContextTokens: 128000, OutputTokens: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Schema   string `json:"$schema"`
		Model    string `json:"model"`
		Share    string `json:"share"`
		Autoupd  bool   `json:"autoupdate"`
		Provider map[string]struct {
			NPM     string `json:"npm"`
			Options struct {
				BaseURL string `json:"baseURL"`
				APIKey  string `json:"apiKey"`
			} `json:"options"`
			Models map[string]struct {
				Limit struct{ Context, Output int } `json:"limit"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("config must be valid JSON: %v\n%s", err, raw)
	}
	p, ok := cfg.Provider["gw"]
	if !ok || len(cfg.Provider) != 1 {
		t.Fatalf("exactly one provider named gw expected, got %v", cfg.Provider)
	}
	if p.NPM != "@ai-sdk/openai-compatible" || p.Options.BaseURL != "http://172.29.0.2:8082/v1" || p.Options.APIKey != "tok.sig" {
		t.Errorf("provider options: %+v", p)
	}
	if len(p.Models) != 2 || p.Models["gpt-a"].Limit.Context != 128000 || p.Models["gpt-b"].Limit.Output != 4096 {
		t.Errorf("models/limits: %+v", p.Models)
	}
	if cfg.Model != "gw/gpt-a" {
		t.Errorf("default model = %q, want gw/gpt-a", cfg.Model)
	}
	if cfg.Share != "disabled" || cfg.Autoupd {
		t.Errorf("sharing and autoupdate must be off: %q %v", cfg.Share, cfg.Autoupd)
	}

	// Awkward characters in inputs must stay data, never break out of the JSON.
	nasty, err := OpenCodeConfig(OpenCodeConfigParams{BaseURL: "http://gw/v1", Token: `a"b\n`, Models: []string{`m"x`}, ContextTokens: 1, OutputTokens: 1})
	if err != nil || !json.Valid([]byte(nasty)) || strings.Count(nasty, `"apiKey"`) != 1 {
		t.Errorf("nasty input must still yield valid JSON: %v %s", err, nasty)
	}

	for name, p := range map[string]OpenCodeConfigParams{
		"no base url": {Token: "t", Models: []string{"m"}, ContextTokens: 1, OutputTokens: 1},
		"no token":    {BaseURL: "http://gw/v1", Models: []string{"m"}, ContextTokens: 1, OutputTokens: 1},
		"no models":   {BaseURL: "http://gw/v1", Token: "t", ContextTokens: 1, OutputTokens: 1},
		"no context":  {BaseURL: "http://gw/v1", Token: "t", Models: []string{"m"}, OutputTokens: 1},
		"no output":   {BaseURL: "http://gw/v1", Token: "t", Models: []string{"m"}, ContextTokens: 1},
	} {
		if _, err := OpenCodeConfig(p); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}
