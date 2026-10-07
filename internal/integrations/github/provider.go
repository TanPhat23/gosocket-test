package github

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
)

const (
	defaultBaseURL = "https://api.github.com"
	defaultTimeout = 10 * time.Second
)

// VerifySignature validates a GitHub webhook payload using HMAC-SHA256.
func VerifySignature(secret string, payload []byte, signatureHeader string) bool {
	if secret == "" || signatureHeader == "" {
		return false
	}
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return false
	}
	sigHex := strings.TrimPrefix(signatureHeader, "sha256=")
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedMAC := mac.Sum(nil)

	return hmac.Equal(sigBytes, expectedMAC)
}

// Provider implements integrations.SyncProvider and integrations.PullRequestProvider for GitHub.
type Provider struct {
	httpClient *http.Client
	baseURL    string
}

// Option configures Provider.
type Option func(*Provider)

// WithBaseURL overrides the GitHub API baseURL (useful for tests or GitHub Enterprise).
func WithBaseURL(rawURL string) Option {
	return func(p *Provider) {
		p.baseURL = strings.TrimRight(rawURL, "/")
	}
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(p *Provider) {
		p.httpClient = client
	}
}

// NewProvider creates a new GitHub provider instance.
func NewProvider(opts ...Option) *Provider {
	p := &Provider{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    defaultBaseURL,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Provider returns the IntegrationGitHub enum.
func (p *Provider) Provider() models.IntegrationProvider {
	return models.IntegrationGitHub
}

// parseRepo resolves "owner/repo" from ProjectIntegration.
func parseRepo(integration *models.ProjectIntegration) (string, error) {
	if integration == nil {
		return "", errors.New("nil integration")
	}
	if integration.ExternalID != "" {
		parts := strings.Split(integration.ExternalID, "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return integration.ExternalID, nil
		}
	}
	if integration.Config != nil {
		if repo, ok := integration.Config["repo"].(string); ok && repo != "" {
			return repo, nil
		}
	}
	return "", errors.New("missing or invalid repository name in ExternalID (expected 'owner/repo')")
}

func (p *Provider) doRequest(ctx context.Context, method, endpoint, token string, body any) (*http.Response, error) {
	reqURL := fmt.Sprintf("%s%s", p.baseURL, endpoint)
	var bodyReader io.Reader
	if body != nil {
		b, err := common.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github api request failed: %w", err)
	}
	return resp, nil
}

type ghIssue struct {
	Number      int      `json:"number"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	State       string   `json:"state"`
	HTMLURL     string   `json:"html_url"`
	PullRequest any      `json:"pull_request,omitempty"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// FetchIssues fetches open issues for the configured repo.
func (p *Provider) FetchIssues(ctx context.Context, integration *models.ProjectIntegration, plainSecret string) ([]integrations.ExternalIssue, error) {
	repo, err := parseRepo(integration)
	if err != nil {
		return nil, err
	}

	// Filter by state=open
	endpoint := fmt.Sprintf("/repos/%s/issues?state=open", repo)
	resp, err := p.doRequest(ctx, http.MethodGet, endpoint, plainSecret, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api returned status %d: %s", resp.StatusCode, string(b))
	}

	var ghIssues []ghIssue
	if err := json.NewDecoder(resp.Body).Decode(&ghIssues); err != nil {
		return nil, fmt.Errorf("failed to decode issues: %w", err)
	}

	var issues []integrations.ExternalIssue
	for _, it := range ghIssues {
		// Ignore pull requests returned by GitHub issues endpoint
		if it.PullRequest != nil {
			continue
		}
		var labels []string
		for _, l := range it.Labels {
			labels = append(labels, l.Name)
		}
		issues = append(issues, integrations.ExternalIssue{
			ID:          fmt.Sprintf("%d", it.Number),
			Title:       it.Title,
			Description: it.Body,
			Status:      it.State,
			URL:         it.HTMLURL,
			Labels:      labels,
		})
	}

	return issues, nil
}

// UpdateIssueStatus updates issue state and/or adds labels (e.g. in-progress:agent).
func (p *Provider) UpdateIssueStatus(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, externalID string, status string) error {
	repo, err := parseRepo(integration)
	if err != nil {
		return err
	}

	// GitHub issue state can be 'open' or 'closed'. Other statuses are represented by labels.
	ghState := ""
	if status == "closed" || status == "done" {
		ghState = "closed"
	} else if status == "open" {
		ghState = "open"
	}

	if ghState != "" {
		endpoint := fmt.Sprintf("/repos/%s/issues/%s", repo, url.PathEscape(externalID))
		payload := map[string]any{"state": ghState}
		resp, err := p.doRequest(ctx, http.MethodPatch, endpoint, plainSecret, payload)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("github update issue state returned %d: %s", resp.StatusCode, string(b))
		}
	}

	// If status represents an agent label transition such as "in_progress" or "in-progress:agent"
	if strings.Contains(status, "progress") || strings.Contains(status, "agent") || strings.Contains(status, "review") {
		labelName := status
		if status == "in_progress" {
			labelName = "in-progress:agent"
		}
		endpoint := fmt.Sprintf("/repos/%s/issues/%s/labels", repo, url.PathEscape(externalID))
		payload := map[string]any{"labels": []string{labelName}}
		resp, err := p.doRequest(ctx, http.MethodPost, endpoint, plainSecret, payload)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("github add label returned %d: %s", resp.StatusCode, string(b))
		}
	}

	return nil
}

// AddComment posts a comment to the specified issue.
func (p *Provider) AddComment(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, externalID string, comment string) error {
	repo, err := parseRepo(integration)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("/repos/%s/issues/%s/comments", repo, url.PathEscape(externalID))
	payload := map[string]string{"body": comment}
	resp, err := p.doRequest(ctx, http.MethodPost, endpoint, plainSecret, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github add comment returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

type ghPRResponse struct {
	HTMLURL string `json:"html_url"`
}

// CreatePullRequest creates a pull request on the GitHub repository.
func (p *Provider) CreatePullRequest(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, branch, title, body string) (string, error) {
	repo, err := parseRepo(integration)
	if err != nil {
		return "", err
	}

	baseBranch := "main"
	if integration.Config != nil {
		if b, ok := integration.Config["base_branch"].(string); ok && b != "" {
			baseBranch = b
		}
	}

	endpoint := fmt.Sprintf("/repos/%s/pulls", repo)
	payload := map[string]string{
		"title": title,
		"head":  branch,
		"base":  baseBranch,
		"body":  body,
	}

	resp, err := p.doRequest(ctx, http.MethodPost, endpoint, plainSecret, payload)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github create pull request returned %d: %s", resp.StatusCode, string(b))
	}

	var prResp ghPRResponse
	if err := json.NewDecoder(resp.Body).Decode(&prResp); err != nil {
		return "", fmt.Errorf("failed to decode pr response: %w", err)
	}

	return prResp.HTMLURL, nil
}

type ghRepoResponse struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Description   string `json:"description"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
}

// ListRepositories lists repositories accessible by the given GitHub PAT.
func (p *Provider) ListRepositories(ctx context.Context, token string) ([]dto.GitHubRepositoryDTO, error) {
	if token == "" {
		return nil, errors.New("github token cannot be empty")
	}

	endpoint := "/user/repos?sort=updated&per_page=100&affiliation=owner,collaborator,organization_member"
	resp, err := p.doRequest(ctx, http.MethodGet, endpoint, token, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("invalid or expired github token")
	}
	if resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(strings.ToLower(string(body)), "rate limit") {
			return nil, errors.New("github api rate limit exceeded")
		}
		return nil, fmt.Errorf("github api access forbidden: %s", string(body))
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errors.New("github api rate limit exceeded")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github list repositories returned %d: %s", resp.StatusCode, string(body))
	}

	var ghRepos []ghRepoResponse
	if err := json.NewDecoder(resp.Body).Decode(&ghRepos); err != nil {
		return nil, fmt.Errorf("failed to decode github repositories: %w", err)
	}

	repos := make([]dto.GitHubRepositoryDTO, 0, len(ghRepos))
	for _, r := range ghRepos {
		repos = append(repos, dto.GitHubRepositoryDTO{
			ID:            r.ID,
			Name:          r.Name,
			FullName:      r.FullName,
			Private:       r.Private,
			Description:   r.Description,
			HTMLURL:       r.HTMLURL,
			DefaultBranch: r.DefaultBranch,
		})
	}

	return repos, nil
}
