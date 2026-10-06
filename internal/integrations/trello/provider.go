package trello

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
)

const (
	defaultBaseURL = "https://api.trello.com"
	defaultTimeout = 10 * time.Second
)

// Provider implements integrations.SyncProvider for Trello.
type Provider struct {
	httpClient *http.Client
	baseURL    string
}

// Option configures Provider.
type Option func(*Provider)

// WithBaseURL overrides the default Trello baseURL (useful for tests).
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

// NewProvider creates a new Trello provider instance.
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

// Provider returns the IntegrationTrello enum.
func (p *Provider) Provider() models.IntegrationProvider {
	return models.IntegrationTrello
}

func (p *Provider) resolveAuth(integration *models.ProjectIntegration, plainSecret string) (apiKey string, token string, err error) {
	if integration == nil {
		return "", "", errors.New("nil integration")
	}

	apiKey = ""
	if integration.Config != nil {
		if k, ok := integration.Config["api_key"].(string); ok {
			apiKey = k
		}
	}
	if apiKey == "" {
		return "", "", errors.New("missing api_key in integration config")
	}

	token = plainSecret
	if token == "" {
		return "", "", errors.New("empty trello token")
	}

	return apiKey, token, nil
}

func (p *Provider) doRequest(ctx context.Context, method, endpoint string, queryParams url.Values) (*http.Response, error) {
	reqURL := fmt.Sprintf("%s%s", p.baseURL, endpoint)
	if len(queryParams) > 0 {
		reqURL += "?" + queryParams.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trello api request failed: %w", err)
	}
	return resp, nil
}

type trelloCard struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Desc   string `json:"desc"`
	Closed bool   `json:"closed"`
	URL    string `json:"url"`
	IDList string `json:"idList"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// FetchIssues fetches cards from the board specified by integration.ExternalID.
func (p *Provider) FetchIssues(ctx context.Context, integration *models.ProjectIntegration, plainSecret string) ([]integrations.ExternalIssue, error) {
	apiKey, token, err := p.resolveAuth(integration, plainSecret)
	if err != nil {
		return nil, err
	}

	boardID := integration.ExternalID
	if boardID == "" {
		return nil, errors.New("missing board ID in ExternalID")
	}

	params := url.Values{}
	params.Set("key", apiKey)
	params.Set("token", token)

	endpoint := fmt.Sprintf("/1/boards/%s/cards", url.PathEscape(boardID))
	resp, err := p.doRequest(ctx, http.MethodGet, endpoint, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("trello fetch cards returned status %d: %s", resp.StatusCode, string(b))
	}

	var cards []trelloCard
	if err := json.NewDecoder(resp.Body).Decode(&cards); err != nil {
		return nil, fmt.Errorf("failed to decode cards: %w", err)
	}

	var issues []integrations.ExternalIssue
	for _, card := range cards {
		if card.Closed {
			continue
		}
		var labels []string
		for _, l := range card.Labels {
			labels = append(labels, l.Name)
		}
		issues = append(issues, integrations.ExternalIssue{
			ID:          card.ID,
			Title:       card.Name,
			Description: card.Desc,
			Status:      card.IDList,
			URL:         card.URL,
			Labels:      labels,
		})
	}

	return issues, nil
}

// UpdateIssueStatus moves a card to a different list based on config.lists mapping or direct list ID.
func (p *Provider) UpdateIssueStatus(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, externalID string, status string) error {
	apiKey, token, err := p.resolveAuth(integration, plainSecret)
	if err != nil {
		return err
	}

	targetListID := status
	if integration.Config != nil {
		if listsMap, ok := integration.Config["lists"].(map[string]any); ok {
			statusKey := strings.ToLower(status)
			if id, ok := listsMap[statusKey].(string); ok && id != "" {
				targetListID = id
			}
		}
	}

	params := url.Values{}
	params.Set("key", apiKey)
	params.Set("token", token)
	params.Set("idList", targetListID)

	endpoint := fmt.Sprintf("/1/cards/%s", url.PathEscape(externalID))
	resp, err := p.doRequest(ctx, http.MethodPut, endpoint, params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trello update card list returned status %d: %s", resp.StatusCode, string(b))
	}

	return nil
}

// AddComment posts a comment to a Trello card.
func (p *Provider) AddComment(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, externalID string, comment string) error {
	apiKey, token, err := p.resolveAuth(integration, plainSecret)
	if err != nil {
		return err
	}

	params := url.Values{}
	params.Set("key", apiKey)
	params.Set("token", token)
	params.Set("text", comment)

	endpoint := fmt.Sprintf("/1/cards/%s/actions/comments", url.PathEscape(externalID))
	resp, err := p.doRequest(ctx, http.MethodPost, endpoint, params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trello add comment returned status %d: %s", resp.StatusCode, string(b))
	}

	return nil
}
