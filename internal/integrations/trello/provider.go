package trello

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/integrations/httpx"
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
		if k, ok := models.ConfigValue[string](integration.Config, "api_key"); ok {
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
	headers := map[string]string{"Accept": "application/json"}
	return httpx.Do(ctx, p.httpClient, method, reqURL, nil, headers, "trello api")
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

	if err := httpx.CheckSuccess(resp, "trello fetch cards returned status"); err != nil {
		return nil, err
	}

	cards, err := httpx.DecodeJSON[[]trelloCard](resp.Body, "cards")
	if err != nil {
		return nil, err
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
		if listsMap, ok := models.ConfigValue[map[string]any](integration.Config, "lists"); ok {
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

	if err := httpx.CheckSuccess(resp, "trello update card list returned status"); err != nil {
		return err
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

	if err := httpx.CheckSuccess(resp, "trello add comment returned status"); err != nil {
		return err
	}

	return nil
}
