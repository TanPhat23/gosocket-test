package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/dto"
)

var sessionIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// OpenCodeSessionDTO represents session details returned from the OpenCode daemon.
type OpenCodeSessionDTO struct {
	ID        string     `json:"id"`
	ParentID  string     `json:"parentID,omitempty"`
	ProjectID string     `json:"projectID,omitempty"`
	Title     string     `json:"title,omitempty"`
	Directory string     `json:"directory,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// OpenCodeMessageDTO aliases dto.OpenCodeMessageDTO for runner consumers.
type OpenCodeMessageDTO = dto.OpenCodeMessageDTO

// OpenCodeClient provides access to OpenCode's background server REST API.
type OpenCodeClient interface {
	GetSession(ctx context.Context, sessionID string) (*OpenCodeSessionDTO, error)
	GetSessionMessages(ctx context.Context, sessionID string) ([]OpenCodeMessageDTO, error)
	PromptSession(ctx context.Context, sessionID, prompt string) error
	CompactSession(ctx context.Context, sessionID string) error
}

type openCodeClient struct {
	baseURL    string
	password   string
	httpClient *http.Client
}

// NewOpenCodeClient creates and validates an OpenCodeClient instance.
// AC-SEC-03: Strictly verifies HTTP/HTTPS URL scheme and disables redirects.
func NewOpenCodeClient(baseURL, password string, httpClient *http.Client) OpenCodeClient {
	parsedURL, err := url.Parse(baseURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		// Default to safe localhost if invalid or empty
		baseURL = "http://127.0.0.1:49374"
	} else {
		// Clean URL, strip trailing slash
		baseURL = strings.TrimRight(baseURL, "/")
	}

	client := httpClient
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	} else {
		// Ensure redirect disabled
		clone := *client
		clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
		client = &clone
	}

	return &openCodeClient{
		baseURL:    baseURL,
		password:   password,
		httpClient: client,
	}
}

// validateSessionID verifies that sessionID contains only alphanumeric, dash, or underscore characters (AC-SEC-05).
func validateSessionID(sessionID string) (string, error) {
	if sessionID == "" || !sessionIDRegex.MatchString(sessionID) {
		return "", fmt.Errorf("invalid session id format")
	}
	return url.PathEscape(sessionID), nil
}

func (c *openCodeClient) doRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Response, error) {
	reqURL := fmt.Sprintf("%s%s", c.baseURL, endpoint)
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, common.ErrServiceUnavailable
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.password != "" {
		req.SetBasicAuth("opencode", c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// AC-SEC-04: Sanitize daemon network/offline errors, do not leak credentials or raw stack
		return nil, common.ErrServiceUnavailable
	}
	return resp, nil
}

func (c *openCodeClient) GetSession(ctx context.Context, sessionID string) (*OpenCodeSessionDTO, error) {
	escapedID, err := validateSessionID(sessionID)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/session/"+escapedID, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, common.ErrNotFound
	}
	if resp.StatusCode >= 500 {
		return nil, common.ErrServiceUnavailable
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("opencode server returned status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, common.ErrServiceUnavailable
	}

	// OpenCode returns {"data": {...}} or direct object
	var wrapper struct {
		Data struct {
			ID        string `json:"id"`
			ParentID  string `json:"parentID"`
			ProjectID string `json:"projectID"`
			Title     string `json:"title"`
			Time      struct {
				Created int64 `json:"created"`
				Updated int64 `json:"updated"`
			} `json:"time"`
			Location struct {
				Directory string `json:"directory"`
			} `json:"location"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &wrapper); err == nil && wrapper.Data.ID != "" {
		var createdAt, updatedAt *time.Time
		if wrapper.Data.Time.Created > 0 {
			t := time.UnixMilli(wrapper.Data.Time.Created)
			createdAt = &t
		}
		if wrapper.Data.Time.Updated > 0 {
			t := time.UnixMilli(wrapper.Data.Time.Updated)
			updatedAt = &t
		}
		return &OpenCodeSessionDTO{
			ID:        wrapper.Data.ID,
			ParentID:  wrapper.Data.ParentID,
			ProjectID: wrapper.Data.ProjectID,
			Title:     wrapper.Data.Title,
			Directory: wrapper.Data.Location.Directory,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}, nil
	}

	// Fallback direct unmarshal
	var direct OpenCodeSessionDTO
	if err := json.Unmarshal(respBody, &direct); err != nil {
		return nil, fmt.Errorf("failed to parse opencode session response: %w", err)
	}
	return &direct, nil
}

func (c *openCodeClient) GetSessionMessages(ctx context.Context, sessionID string) ([]OpenCodeMessageDTO, error) {
	escapedID, err := validateSessionID(sessionID)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/session/"+escapedID+"/message", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, common.ErrNotFound
	}
	if resp.StatusCode >= 500 {
		return nil, common.ErrServiceUnavailable
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("opencode server returned status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, common.ErrServiceUnavailable
	}

	// OpenCode returns {"data": [...]} or [...]
	var wrapper struct {
		Data []struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Agent string `json:"agent"`
			Time  struct {
				Created int64 `json:"created"`
			} `json:"time"`
			Content json.RawMessage `json:"content"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &wrapper); err == nil && wrapper.Data != nil {
		messages := make([]OpenCodeMessageDTO, len(wrapper.Data))
		for i, m := range wrapper.Data {
			var createdAt *time.Time
			if m.Time.Created > 0 {
				t := time.UnixMilli(m.Time.Created)
				createdAt = &t
			}
			var contentObj interface{}
			if len(m.Content) > 0 {
				_ = json.Unmarshal(m.Content, &contentObj)
			}
			messages[i] = OpenCodeMessageDTO{
				ID:        m.ID,
				Type:      m.Type,
				Agent:     m.Agent,
				Content:   contentObj,
				CreatedAt: createdAt,
			}
		}
		return messages, nil
	}

	var direct []OpenCodeMessageDTO
	if err := json.Unmarshal(respBody, &direct); err != nil {
		return nil, fmt.Errorf("failed to parse opencode messages: %w", err)
	}
	return direct, nil
}

func (c *openCodeClient) PromptSession(ctx context.Context, sessionID, prompt string) error {
	escapedID, err := validateSessionID(sessionID)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"parts": []map[string]string{
			{"type": "text", "text": prompt},
		},
		"prompt": prompt,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/api/session/"+escapedID+"/prompt", bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return common.ErrNotFound
	}
	if resp.StatusCode >= 500 {
		return common.ErrServiceUnavailable
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("opencode server returned status %d", resp.StatusCode)
	}

	return nil
}

func (c *openCodeClient) CompactSession(ctx context.Context, sessionID string) error {
	escapedID, err := validateSessionID(sessionID)
	if err != nil {
		return err
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/api/session/"+escapedID+"/compact", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return common.ErrNotFound
	}
	if resp.StatusCode >= 500 {
		return common.ErrServiceUnavailable
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("opencode server returned status %d", resp.StatusCode)
	}

	return nil
}
