package gateway

import (
	"encoding/json"
	"errors"
	"net/url"
	"time"
)

// Broker hands per-attempt capability tokens to sandboxes and takes them back.
type Broker struct {
	svc     *Service
	baseURL string
	ttl     time.Duration
}

// Grant is what a sandbox needs to reach the gateway for one task attempt.
type Grant struct {
	Token   string
	BaseURL string
}

// NewBroker returns a Broker that tells sandboxes to reach the gateway at baseURL (the OpenAI-compatible
// root, e.g. http://172.29.0.2:8082/v1). Tokens live for ttl. A nil (disabled) Service cannot grant.
func (s *Service) NewBroker(baseURL string, ttl time.Duration) (*Broker, error) {
	if s == nil || s.Issuer == nil || s.Ledger == nil {
		return nil, errors.New("gateway: not enabled")
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("gateway: sandbox base URL must be http(s) with a host and no credentials, query or fragment")
	}
	if ttl <= 0 {
		return nil, errors.New("gateway: token ttl must be positive")
	}
	return &Broker{svc: s, baseURL: baseURL, ttl: ttl}, nil
}

// Grant mints a token bound to exactly one attempt.
func (b *Broker) Grant(projectID, taskID, attemptID string) (Grant, error) {
	if projectID == "" || taskID == "" || attemptID == "" {
		return Grant{}, errors.New("gateway: project, task and attempt ids are required")
	}
	tok := b.svc.Issuer.Mint(Claims{ProjectID: projectID, TaskID: taskID, AttemptID: attemptID}, b.ttl)
	return Grant{Token: tok, BaseURL: b.baseURL}, nil
}

// Release revokes the attempt's token and forgets its quota usage. It is idempotent.
func (b *Broker) Release(attemptID string) {
	if attemptID == "" {
		return
	}
	b.svc.Issuer.Revoke(attemptID)
	b.svc.Ledger.Forget(attemptID)
}

// OpenCodeConfigParams describes the single gateway-backed provider OpenCode may use.
type OpenCodeConfigParams struct {
	BaseURL       string
	Token         string
	Models        []string
	ContextTokens int
	OutputTokens  int
}

// OpenCodeConfig renders the OPENCODE_CONFIG_CONTENT JSON that points OpenCode at the gateway only.
// It never contains the real provider key. The first model is the default.
func OpenCodeConfig(p OpenCodeConfigParams) (string, error) {
	if p.BaseURL == "" || p.Token == "" || len(p.Models) == 0 || p.ContextTokens <= 0 || p.OutputTokens <= 0 {
		return "", errors.New("gateway: base URL, token, models and positive limits are required")
	}
	type limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	}
	type model struct {
		Name  string `json:"name"`
		Limit limit  `json:"limit"`
	}
	models := make(map[string]model, len(p.Models))
	for _, m := range p.Models {
		models[m] = model{Name: m, Limit: limit{Context: p.ContextTokens, Output: p.OutputTokens}}
	}
	cfg := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{"gw": map[string]any{
			"npm":     "@ai-sdk/openai-compatible",
			"name":    "gw",
			"options": map[string]string{"baseURL": p.BaseURL, "apiKey": p.Token},
			"models":  models,
		}},
		"model":      "gw/" + p.Models[0],
		"share":      "disabled",
		"autoupdate": false,
	}
	out, err := json.Marshal(cfg)
	return string(out), err
}
