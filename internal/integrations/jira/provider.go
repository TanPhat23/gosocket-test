package jira

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/integrations/httpx"
	"agent-task-manager/internal/models"
)

const defaultTimeout = 10 * time.Second

// ValidateHostURL checks if a host URL is a valid, secure Jira Cloud URL (SSRF prevention).
func ValidateHostURL(rawURL string) (*url.URL, error) {
	if rawURL == "" {
		return nil, errors.New("empty host url")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid host url: %w", err)
	}

	if parsed.Scheme != "https" {
		return nil, errors.New("host url must use https")
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return nil, errors.New("host url missing hostname")
	}

	// Check loopback / localhost
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return nil, errors.New("loopback host not allowed")
	}

	// Check IP literals
	ip := net.ParseIP(hostname)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return nil, errors.New("private or local IP address not allowed")
		}
	}

	return parsed, nil
}

// Provider implements integrations.SyncProvider for Jira Cloud.
type Provider struct {
	httpClient        *http.Client
	allowInsecureHTTP bool // For local test servers
}

// Option configures Provider.
type Option func(*Provider)

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(p *Provider) {
		p.httpClient = client
	}
}

// WithAllowInsecureHTTP allows http URLs in unit tests.
func WithAllowInsecureHTTP(allow bool) Option {
	return func(p *Provider) {
		p.allowInsecureHTTP = allow
	}
}

// NewProvider creates a new Jira provider instance.
func NewProvider(opts ...Option) *Provider {
	p := &Provider{
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Provider returns the IntegrationJira enum.
func (p *Provider) Provider() models.IntegrationProvider {
	return models.IntegrationJira
}

func (p *Provider) resolveHost(integration *models.ProjectIntegration) (string, error) {
	if integration == nil {
		return "", errors.New("nil integration")
	}

	host := ""
	if integration.Config != nil {
		if h, ok := models.ConfigValue[string](integration.Config, "host"); ok {
			host = h
		}
	}
	if host == "" && integration.ExternalURL != nil {
		host = *integration.ExternalURL
	}
	if host == "" {
		return "", errors.New("jira host url not configured in config or external_url")
	}

	if !p.allowInsecureHTTP {
		u, err := ValidateHostURL(host)
		if err != nil {
			return "", fmt.Errorf("invalid jira host url: %w", err)
		}
		return strings.TrimRight(u.String(), "/"), nil
	}

	return strings.TrimRight(host, "/"), nil
}

func (p *Provider) buildAuthHeader(integration *models.ProjectIntegration, plainSecret string) string {
	email := ""
	if integration != nil && integration.Config != nil {
		if e, ok := models.ConfigValue[string](integration.Config, "user_email"); ok {
			email = e
		}
	}

	if email != "" {
		auth := email + ":" + plainSecret
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
	}
	// Fallback to Bearer token if no email provided
	return "Bearer " + plainSecret
}

func (p *Provider) doRequest(ctx context.Context, method, fullURL, authHeader string, body any) (*http.Response, error) {
	headers := map[string]string{"Accept": "application/json", "Authorization": authHeader}
	return httpx.Do(ctx, p.httpClient, method, fullURL, body, headers, "jira api")
}

type jiraIssueFieldStatus struct {
	Name string `json:"name"`
}

type jiraIssueFields struct {
	Summary     string               `json:"summary"`
	Description any                  `json:"description"` // Can be string or ADF struct
	Status      jiraIssueFieldStatus `json:"status"`
	Labels      []string             `json:"labels"`
}

type jiraIssue struct {
	ID     string          `json:"id"`
	Key    string          `json:"key"`
	Fields jiraIssueFields `json:"fields"`
}

type jiraSearchResponse struct {
	Issues []jiraIssue `json:"issues"`
}

// FetchIssues queries Jira issues using JQL.
func (p *Provider) FetchIssues(ctx context.Context, integration *models.ProjectIntegration, plainSecret string) ([]integrations.ExternalIssue, error) {
	host, err := p.resolveHost(integration)
	if err != nil {
		return nil, err
	}

	jql := ""
	if integration.Config != nil {
		if q, ok := models.ConfigValue[string](integration.Config, "jql"); ok {
			jql = q
		}
	}
	if jql == "" {
		projectKey := integration.ExternalID
		if projectKey == "" {
			return nil, errors.New("missing project key in ExternalID or jql in Config")
		}
		jql = fmt.Sprintf("project = %s AND statusCategory != Done ORDER BY created DESC", projectKey)
	}

	searchURL := fmt.Sprintf("%s/rest/api/3/search?jql=%s", host, url.QueryEscape(jql))
	authHeader := p.buildAuthHeader(integration, plainSecret)

	resp, err := p.doRequest(ctx, http.MethodGet, searchURL, authHeader, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := httpx.CheckSuccess(resp, "jira search returned status"); err != nil {
		return nil, err
	}

	searchResp, err := httpx.DecodeJSON[jiraSearchResponse](resp.Body, "jira response")
	if err != nil {
		return nil, err
	}

	var issues []integrations.ExternalIssue
	for _, it := range searchResp.Issues {
		desc := ""
		if s, ok := it.Fields.Description.(string); ok {
			desc = s
		}
		issues = append(issues, integrations.ExternalIssue{
			ID:          it.Key,
			Title:       it.Fields.Summary,
			Description: desc,
			Status:      it.Fields.Status.Name,
			URL:         fmt.Sprintf("%s/browse/%s", host, it.Key),
			Labels:      it.Fields.Labels,
		})
	}

	return issues, nil
}

type jiraTransitionsResponse struct {
	Transitions []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		To   struct {
			Name string `json:"name"`
		} `json:"to"`
	} `json:"transitions"`
}

// UpdateIssueStatus transitions an issue to the requested status.
func (p *Provider) UpdateIssueStatus(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, externalID string, status string) error {
	host, err := p.resolveHost(integration)
	if err != nil {
		return err
	}

	authHeader := p.buildAuthHeader(integration, plainSecret)
	transitionsURL := fmt.Sprintf("%s/rest/api/3/issue/%s/transitions", host, url.PathEscape(externalID))

	// Get available transitions
	resp, err := p.doRequest(ctx, http.MethodGet, transitionsURL, authHeader, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := httpx.CheckSuccess(resp, "jira fetch transitions returned"); err != nil {
		return err
	}

	transResp, err := httpx.DecodeJSON[jiraTransitionsResponse](resp.Body, "transitions")
	if err != nil {
		return err
	}

	var targetTransitionID string
	targetStatusLower := strings.ToLower(strings.ReplaceAll(status, "_", " "))

	for _, t := range transResp.Transitions {
		if strings.EqualFold(t.Name, targetStatusLower) || strings.EqualFold(t.To.Name, targetStatusLower) ||
			strings.Contains(strings.ToLower(t.Name), targetStatusLower) {
			targetTransitionID = t.ID
			break
		}
	}

	if targetTransitionID == "" {
		return fmt.Errorf("no transition found matching target status %q", status)
	}

	// Post transition
	payload := map[string]any{
		"transition": map[string]string{
			"id": targetTransitionID,
		},
	}
	postResp, err := p.doRequest(ctx, http.MethodPost, transitionsURL, authHeader, payload)
	if err != nil {
		return err
	}
	defer postResp.Body.Close()

	if err := httpx.CheckSuccess(postResp, "jira execute transition returned"); err != nil {
		return err
	}

	return nil
}

// AddComment posts a comment to a Jira issue using ADF or simple body.
func (p *Provider) AddComment(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, externalID string, comment string) error {
	host, err := p.resolveHost(integration)
	if err != nil {
		return err
	}

	authHeader := p.buildAuthHeader(integration, plainSecret)
	commentURL := fmt.Sprintf("%s/rest/api/3/issue/%s/comment", host, url.PathEscape(externalID))

	// Jira Cloud REST API v3 requires ADF (Atlassian Document Format) for comments
	adfBody := map[string]any{
		"body": map[string]any{
			"version": 1,
			"type":    "doc",
			"content": []any{
				map[string]any{
					"type": "paragraph",
					"content": []any{
						map[string]any{
							"type": "text",
							"text": comment,
						},
					},
				},
			},
		},
	}

	resp, err := p.doRequest(ctx, http.MethodPost, commentURL, authHeader, adfBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := httpx.CheckSuccess(resp, "jira add comment returned"); err != nil {
		return err
	}

	return nil
}
