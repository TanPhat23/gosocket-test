package jira_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agent-task-manager/internal/integrations/jira"
	"agent-task-manager/internal/models"
)

func TestValidateHostURL_SSRFProtection(t *testing.T) {
	cases := []struct {
		url    string
		valid  bool
		reason string
	}{
		{"https://company.atlassian.net", true, "valid Jira Cloud URL"},
		{"http://company.atlassian.net", false, "insecure http blocked"},
		{"https://127.0.0.1", false, "loopback blocked"},
		{"https://169.254.169.254/metadata", false, "link-local/metadata IP blocked"},
		{"https://localhost:8080", false, "localhost blocked"},
		{"https://10.0.0.1", false, "private IP blocked"},
		{"https://192.168.1.1", false, "private IP blocked"},
		{"https://172.16.0.1", false, "private IP blocked"},
		{"ftp://company.atlassian.net", false, "invalid scheme"},
		{"", false, "empty url"},
	}

	for _, tc := range cases {
		_, err := jira.ValidateHostURL(tc.url)
		if tc.valid && err != nil {
			t.Errorf("expected valid for %s (%s), got err: %v", tc.url, tc.reason, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("expected error for %s (%s), got nil", tc.url, tc.reason)
		}
	}
}

func TestJiraProvider_FetchIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Errorf("missing Authorization header")
		}

		resp := map[string]any{
			"issues": []map[string]any{
				{
					"id":  "10001",
					"key": "PROJ-123",
					"fields": map[string]any{
						"summary":     "Implement SSO",
						"description": "Details about SSO",
						"status": map[string]any{
							"name": "To Do",
						},
						"labels": []string{"backend", "auth"},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := jira.NewProvider(jira.WithHTTPClient(server.Client()), jira.WithAllowInsecureHTTP(true))
	if provider.Provider() != models.IntegrationJira {
		t.Errorf("expected %v, got %v", models.IntegrationJira, provider.Provider())
	}

	integration := &models.ProjectIntegration{
		ExternalID: "PROJ",
		Config: models.JSONMap{
			"host":       server.URL,
			"user_email": "user@example.com",
			"jql":        "project = PROJ AND status = 'To Do'",
		},
	}

	issues, err := provider.FetchIssues(context.Background(), integration, "jira-api-token")
	if err != nil {
		t.Fatalf("FetchIssues failed: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].ID != "PROJ-123" || issues[0].Title != "Implement SSO" || issues[0].Status != "To Do" {
		t.Errorf("unexpected issue data: %+v", issues[0])
	}
}

func TestJiraProvider_UpdateIssueStatusAndComment(t *testing.T) {
	transitionFetched := false
	transitionExecuted := false
	commentAdded := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/PROJ-123/transitions":
			if r.Method == http.MethodGet {
				transitionFetched = true
				resp := map[string]any{
					"transitions": []map[string]any{
						{
							"id":   "21",
							"name": "In Progress",
							"to": map[string]string{
								"name": "In Progress",
							},
						},
						{
							"id":   "31",
							"name": "In Review",
							"to": map[string]string{
								"name": "In Review",
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			} else if r.Method == http.MethodPost {
				transitionExecuted = true
				var payload map[string]map[string]string
				_ = json.NewDecoder(r.Body).Decode(&payload)
				if payload["transition"]["id"] != "21" {
					t.Errorf("expected transition ID 21, got %v", payload["transition"]["id"])
				}
				w.WriteHeader(http.StatusNoContent)
			}
		case "/rest/api/3/issue/PROJ-123/comment":
			commentAdded = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	provider := jira.NewProvider(jira.WithHTTPClient(server.Client()), jira.WithAllowInsecureHTTP(true))
	integration := &models.ProjectIntegration{
		ExternalID: "PROJ",
		Config: models.JSONMap{
			"host":       server.URL,
			"user_email": "user@example.com",
		},
	}

	if err := provider.UpdateIssueStatus(context.Background(), integration, "token", "PROJ-123", "in_progress"); err != nil {
		t.Fatalf("UpdateIssueStatus failed: %v", err)
	}
	if !transitionFetched || !transitionExecuted {
		t.Errorf("expected transition fetch and execute to be called")
	}

	if err := provider.AddComment(context.Background(), integration, "token", "PROJ-123", "Work started"); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
	if !commentAdded {
		t.Errorf("expected comment to be added")
	}
}
