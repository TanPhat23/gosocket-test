package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/dto"
)

const httpMaxPooledBufferBytes = 1024 * 1024 // 1 MiB

var httpBufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 128*1024))
	},
}

func readResponseBody(r io.Reader) (*bytes.Buffer, error) {
	buf := httpBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	_, err := buf.ReadFrom(r)
	if err != nil {
		if buf.Cap() <= httpMaxPooledBufferBytes {
			buf.Reset()
			httpBufferPool.Put(buf)
		}
		return nil, err
	}
	return buf, nil
}

func releaseResponseBody(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	if buf.Cap() <= httpMaxPooledBufferBytes {
		buf.Reset()
		httpBufferPool.Put(buf)
	}
}

var sessionIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// OpenCodeSessionDTO represents session details returned from the OpenCode daemon.
type OpenCodeSessionDTO struct {
	ID        string                `json:"id"`
	ParentID  string                `json:"parentID,omitempty"`
	ProjectID string                `json:"projectID,omitempty"`
	Title     string                `json:"title,omitempty"`
	Directory string                `json:"directory,omitempty"`
	Status    string                `json:"status,omitempty"`
	Model     string                `json:"model,omitempty"`
	Cost      float64               `json:"cost,omitempty"`
	Tokens    *dto.SessionTokensDTO `json:"tokens,omitempty"`
	CreatedAt *time.Time            `json:"created_at,omitempty"`
	UpdatedAt *time.Time            `json:"updated_at,omitempty"`
}

// OpenCodeMessageDTO aliases dto.OpenCodeMessageDTO for runner consumers.
type OpenCodeMessageDTO = dto.OpenCodeMessageDTO

// OpenCodeClient provides access to OpenCode's background server REST API.
type OpenCodeClient interface {
	GetSession(ctx context.Context, sessionID string) (*OpenCodeSessionDTO, error)
	GetSessionMessages(ctx context.Context, sessionID string) ([]OpenCodeMessageDTO, error)
	// ListChildSessions queries all active sessions from OpenCode daemon and filters those whose parentID matches parentID.
	ListChildSessions(ctx context.Context, parentID string) ([]OpenCodeSessionDTO, error)
	// InterruptSession interrupts an active session via POST /api/session/{sessionID}/interrupt.
	// Returns nil on 200 (even if interrupted was false/idle) and 404 (safe no-op).
	InterruptSession(ctx context.Context, sessionID string) error
	// InterruptSessionTree interrupts the root session and all verified descendant child sessions.
	InterruptSessionTree(ctx context.Context, rootSessionID string) error
	PromptSession(ctx context.Context, sessionID, prompt string) error
	CompactSession(ctx context.Context, sessionID string) error
	EnsureSessionAtLocation(ctx context.Context, sessionID, title, directory string) error
}

type openCodeClient struct {
	baseURL    string
	password   string
	httpClient *http.Client
}

// NewOpenCodeClient creates and validates an OpenCodeClient instance.
// AC-SEC-03: Strictly verifies HTTP/HTTPS URL scheme and disables redirects.
// In containerized deployments, automatically bridges loopback port 49374 via local opencode.sock when present.
func NewOpenCodeClient(baseURL, password string, httpClient *http.Client) OpenCodeClient {
	unixSocketCandidate := ""
	if strings.HasPrefix(baseURL, "unix://") {
		unixSocketCandidate = strings.TrimPrefix(baseURL, "unix://")
		baseURL = "http://localhost"
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		// Default to safe localhost if invalid or empty
		baseURL = "http://127.0.0.1:49374"
	} else {
		// Clean URL, strip trailing slash
		baseURL = strings.TrimRight(baseURL, "/")
	}

	if unixSocketCandidate == "" {
		candidates := []string{
			"/home/ghug/.local/share/opencode/opencode.sock",
			"/root/.local/share/opencode/opencode.sock",
			"/tmp/opencode.sock",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				unixSocketCandidate = c
				break
			}
		}
	}

	client := httpClient
	if client == nil {
		transport := &http.Transport{}
		if unixSocketCandidate != "" {
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Only route to unix socket if destination port is 49374 (OpenCode daemon port)
				if strings.HasSuffix(addr, ":49374") {
					var d net.Dialer
					if conn, err := d.DialContext(ctx, "unix", unixSocketCandidate); err == nil {
						return conn, nil
					}
				}
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			}
		}
		client = &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
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

// mapStatus maps an HTTP status code to a sanitised error (AC-SEC-04): the
// response body is never included. 404 -> ErrNotFound, 5xx -> ErrServiceUnavailable,
// other 4xx -> a generic status error with an optional suffix. Codes below 400 yield nil.
func mapStatus(statusCode int, suffix string) error {
	switch {
	case statusCode == http.StatusNotFound:
		return common.ErrNotFound
	case statusCode >= 500:
		return common.ErrServiceUnavailable
	case statusCode >= 400:
		return fmt.Errorf("opencode server returned status %d%s", statusCode, suffix)
	}
	return nil
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

	if err := mapStatus(resp.StatusCode, ""); err != nil {
		return nil, err
	}

	buf, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, common.ErrServiceUnavailable
	}
	defer releaseResponseBody(buf)
	respBody := buf.Bytes()

	// OpenCode returns {"data": {...}} or direct object
	var wrapper struct {
		Data struct {
			ID        string  `json:"id"`
			ParentID  string  `json:"parentID"`
			ProjectID string  `json:"projectID"`
			Title     string  `json:"title"`
			Outcome   string  `json:"outcome"`
			Cost      float64 `json:"cost"`
			Model     struct {
				ID         string `json:"id"`
				ProviderID string `json:"providerID"`
			} `json:"model"`
			Tokens struct {
				Input     int64 `json:"input"`
				Output    int64 `json:"output"`
				Reasoning int64 `json:"reasoning"`
				Cache     struct {
					Read  int64 `json:"read"`
					Write int64 `json:"write"`
				} `json:"cache"`
			} `json:"tokens"`
			Time struct {
				Created int64 `json:"created"`
				Updated int64 `json:"updated"`
			} `json:"time"`
			Location struct {
				Directory string `json:"directory"`
			} `json:"location"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &wrapper); err == nil && wrapper.Data.ID != "" {
		createdAt := millisToTimePtr(wrapper.Data.Time.Created)
		updatedAt := millisToTimePtr(wrapper.Data.Time.Updated)

		status := wrapper.Data.Outcome
		if status == "" {
			status = "idle"
		}

		modelName := wrapper.Data.Model.ID
		if modelName == "" && wrapper.Data.Model.ProviderID != "" {
			modelName = wrapper.Data.Model.ProviderID
		}

		tokens := &dto.SessionTokensDTO{
			Input:      wrapper.Data.Tokens.Input,
			Output:     wrapper.Data.Tokens.Output,
			Reasoning:  wrapper.Data.Tokens.Reasoning,
			CacheRead:  wrapper.Data.Tokens.Cache.Read,
			CacheWrite: wrapper.Data.Tokens.Cache.Write,
		}

		return &OpenCodeSessionDTO{
			ID:        wrapper.Data.ID,
			ParentID:  wrapper.Data.ParentID,
			ProjectID: wrapper.Data.ProjectID,
			Title:     wrapper.Data.Title,
			Directory: wrapper.Data.Location.Directory,
			Status:    status,
			Model:     modelName,
			Cost:      wrapper.Data.Cost,
			Tokens:    tokens,
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

// millisToTimePtr converts Unix milliseconds to a time pointer; non-positive values yield nil.
func millisToTimePtr(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms)
	return &t
}

// sortMessagesByCreatedAt orders messages oldest first; pairs with a nil CreatedAt compare equal.
func sortMessagesByCreatedAt(msgs []OpenCodeMessageDTO) {
	slices.SortFunc(msgs, func(a, b OpenCodeMessageDTO) int {
		if a.CreatedAt == nil || b.CreatedAt == nil {
			return 0
		}
		return a.CreatedAt.Compare(*b.CreatedAt)
	})
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

	if err := mapStatus(resp.StatusCode, ""); err != nil {
		return nil, err
	}

	buf, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, common.ErrServiceUnavailable
	}
	defer releaseResponseBody(buf)
	respBody := buf.Bytes()

	// OpenCode returns {"data": [...]} or [...]
	var wrapper struct {
		Data []struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Role  string `json:"role"`
			Agent string `json:"agent"`
			Text  string `json:"text"`
			Time  struct {
				Created   int64 `json:"created"`
				Completed int64 `json:"completed"`
			} `json:"time"`
			Content json.RawMessage `json:"content"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &wrapper); err == nil && wrapper.Data != nil {
		messages := make([]OpenCodeMessageDTO, len(wrapper.Data))
		for i, m := range wrapper.Data {
			createdAt := millisToTimePtr(m.Time.Created)
			completedAt := millisToTimePtr(m.Time.Completed)
			var contentObj any
			if len(m.Content) > 0 {
				_ = json.Unmarshal(m.Content, &contentObj)
			}
			textVal := m.Text
			if strings.HasPrefix(textVal, "\"") && strings.HasSuffix(textVal, "\"") && len(textVal) >= 2 {
				var unquoted string
				if err := json.Unmarshal([]byte(textVal), &unquoted); err == nil {
					textVal = unquoted
				}
			}
			roleVal := m.Role
			if roleVal == "" {
				switch m.Type {
				case "user", "assistant", "system":
					roleVal = m.Type
				}
			}
			messages[i] = OpenCodeMessageDTO{
				ID:          m.ID,
				Type:        m.Type,
				Role:        roleVal,
				Agent:       m.Agent,
				Text:        textVal,
				Content:     contentObj,
				CreatedAt:   createdAt,
				CompletedAt: completedAt,
			}
		}
		// Sort messages chronologically (oldest first, newest last)
		sortMessagesByCreatedAt(messages)
		return messages, nil
	}

	var direct []OpenCodeMessageDTO
	if err := json.Unmarshal(respBody, &direct); err != nil {
		return nil, fmt.Errorf("failed to parse opencode messages: %w", err)
	}
	sortMessagesByCreatedAt(direct)
	return direct, nil
}

// ListChildSessions queries all active sessions from OpenCode daemon and filters those whose parentID matches parentID.
func (c *openCodeClient) ListChildSessions(ctx context.Context, parentID string) ([]OpenCodeSessionDTO, error) {
	escapedID, err := validateSessionID(parentID)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/session", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("opencode server returned status %d", resp.StatusCode)
	}

	buf, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, err
	}
	defer releaseResponseBody(buf)
	respBody := buf.Bytes()

	var wrapper struct {
		Data []struct {
			ID       string `json:"id"`
			ParentID string `json:"parentID"`
			Title    string `json:"title"`
			Agent    string `json:"agent"`
			Outcome  string `json:"outcome"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &wrapper); err != nil {
		return nil, err
	}

	var childSessions []OpenCodeSessionDTO
	for _, s := range wrapper.Data {
		if s.ParentID == escapedID {
			childSessions = append(childSessions, OpenCodeSessionDTO{
				ID:       s.ID,
				ParentID: s.ParentID,
				Title:    s.Title,
				Model:    s.Agent,
				Status:   s.Outcome,
			})
		}
	}

	return childSessions, nil
}

func (c *openCodeClient) PromptSession(ctx context.Context, sessionID, prompt string) error {
	escapedID, err := validateSessionID(sessionID)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"text": prompt,
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
		buf, _ := readResponseBody(resp.Body)
		defer releaseResponseBody(buf)
		errMsg := ""
		if buf != nil {
			errMsg = strings.TrimSpace(buf.String())
		}
		if errMsg != "" {
			return fmt.Errorf("opencode server returned status %d: %s", resp.StatusCode, errMsg)
		}
		return fmt.Errorf("opencode server returned status %d", resp.StatusCode)
	}

	return nil
}

func (c *openCodeClient) CompactSession(ctx context.Context, sessionID string) error {
	escapedID, err := validateSessionID(sessionID)
	if err != nil {
		return err
	}

	body := bytes.NewReader([]byte("{}"))
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/session/"+escapedID+"/compact", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return mapStatus(resp.StatusCode, "")
}

func (c *openCodeClient) InterruptSession(ctx context.Context, sessionID string) error {
	escapedID, err := validateSessionID(sessionID)
	if err != nil {
		return err
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/api/session/"+escapedID+"/interrupt", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return mapStatus(resp.StatusCode, "")
}

func (c *openCodeClient) InterruptSessionTree(ctx context.Context, rootSessionID string) error {
	if _, err := validateSessionID(rootSessionID); err != nil {
		return err
	}

	visited := make(map[string]bool)
	var errs []error
	c.interruptTreeHelper(ctx, rootSessionID, visited, &errs)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (c *openCodeClient) interruptTreeHelper(ctx context.Context, sessionID string, visited map[string]bool, errs *[]error) {
	if sessionID == "" || visited[sessionID] {
		return
	}
	visited[sessionID] = true

	// Interrupt current session
	if err := c.InterruptSession(ctx, sessionID); err != nil {
		*errs = append(*errs, fmt.Errorf("failed to interrupt session %s: %w", sessionID, err))
	}

	// Discover children
	children, listErr := c.ListChildSessions(ctx, sessionID)
	if listErr != nil {
		*errs = append(*errs, fmt.Errorf("failed to list child sessions for %s: %w", sessionID, listErr))
		return
	}

	for _, child := range children {
		if child.ID != "" && !visited[child.ID] {
			c.interruptTreeHelper(ctx, child.ID, visited, errs)
		}
	}
}

func (c *openCodeClient) EnsureSessionAtLocation(ctx context.Context, sessionID, title, directory string) error {
	escapedID, err := validateSessionID(sessionID)
	if err != nil {
		return err
	}

	if directory == "" {
		return errors.New("directory cannot be empty")
	}

	// 1. Try to create session with location directory
	createPayload := map[string]any{
		"id":    sessionID,
		"title": title,
		"location": map[string]string{
			"directory": directory,
		},
	}
	body, err := json.Marshal(createPayload)
	if err == nil {
		resp, reqErr := c.doRequest(ctx, http.MethodPost, "/api/session", bytes.NewReader(body))
		if reqErr == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
				return nil
			}
		}
	}

	// 2. If session already exists or create didn't finish, move session to directory
	movePayload := map[string]string{
		"directory": directory,
	}
	moveBody, err := json.Marshal(movePayload)
	if err != nil {
		return fmt.Errorf("failed to marshal move session payload: %w", err)
	}

	moveResp, err := c.doRequest(ctx, http.MethodPost, "/api/session/"+escapedID+"/move", bytes.NewReader(moveBody))
	if err != nil {
		return err
	}
	defer moveResp.Body.Close()

	if moveResp.StatusCode == http.StatusOK || moveResp.StatusCode == http.StatusNoContent {
		return nil
	}
	return mapStatus(moveResp.StatusCode, " on move")
}
