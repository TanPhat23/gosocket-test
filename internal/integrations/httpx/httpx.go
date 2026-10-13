// Package httpx holds the small HTTP plumbing shared by the integration
// providers (GitHub, Jira, Trello). Callers supply their own error wording so
// each provider keeps its exact, externally matched error strings.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"agent-task-manager/internal/common"
)

// Do builds a request and sends it with client.
//
// A non-nil body is JSON-marshaled and sent with Content-Type: application/json.
// Headers with an empty value are skipped. Transport failures are returned as
// "<failPrefix> request failed: <err>".
func Do(ctx context.Context, client *http.Client, method, url string, body any, headers map[string]string, failPrefix string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := common.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request failed: %w", failPrefix, err)
	}
	return resp, nil
}

// IsSuccess reports whether status is in the 2xx range.
func IsSuccess(status int) bool {
	return status >= 200 && status < 300
}

// CheckSuccess returns nil for a 2xx response. Otherwise it reads the response
// body and returns "<prefix> <status>: <body>".
func CheckSuccess(resp *http.Response, prefix string) error {
	if IsSuccess(resp.StatusCode) {
		return nil
	}
	b, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("%s %d: %s", prefix, resp.StatusCode, string(b))
}

// DecodeJSON decodes a single JSON value from r. A failure is returned as
// "failed to decode <what>: <err>".
func DecodeJSON[T any](r io.Reader, what string) (T, error) {
	var v T
	if err := json.NewDecoder(r).Decode(&v); err != nil {
		return v, fmt.Errorf("failed to decode %s: %w", what, err)
	}
	return v, nil
}
