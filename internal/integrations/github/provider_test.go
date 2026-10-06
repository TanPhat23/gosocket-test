package github_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agent-task-manager/internal/integrations/github"
	"agent-task-manager/internal/models"
)

func TestGitHubHMACVerification(t *testing.T) {
	secret := "webhook-secret-token"
	body := []byte(`{"action":"opened","issue":{"number":42,"title":"Fix bug"}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !github.VerifySignature(secret, body, validSig) {
		t.Errorf("expected signature to verify successfully")
	}
	if github.VerifySignature(secret, body, "sha256=invalid") {
		t.Errorf("expected invalid signature to fail")
	}
	if github.VerifySignature("", body, validSig) {
		t.Errorf("expected empty secret to fail")
	}
	if github.VerifySignature(secret, body, "") {
		t.Errorf("expected empty signature to fail")
	}
	if github.VerifySignature(secret, body, "not-sha256-prefixed") {
		t.Errorf("expected invalid format to fail")
	}
}

func TestGitHubProvider_FetchIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/issues" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-gh-token" {
			t.Errorf("missing or invalid authorization header")
		}

		issues := []map[string]any{
			{
				"number":   101,
				"title":    "Issue 1",
				"body":     "Description 1",
				"state":    "open",
				"html_url": "https://github.com/owner/repo/issues/101",
				"labels":   []map[string]string{{"name": "bug"}},
			},
			{
				"number":       102,
				"title":        "PR 1",
				"body":         "This is PR",
				"state":        "open",
				"html_url":     "https://github.com/owner/repo/pull/102",
				"pull_request": map[string]string{"url": "https://api.github.com/repos/owner/repo/pulls/102"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	provider := github.NewProvider(github.WithBaseURL(server.URL), github.WithHTTPClient(server.Client()))
	if provider.Provider() != models.IntegrationGitHub {
		t.Errorf("expected provider %v, got %v", models.IntegrationGitHub, provider.Provider())
	}

	integration := &models.ProjectIntegration{
		ExternalID: "owner/repo",
	}

	issues, err := provider.FetchIssues(context.Background(), integration, "test-gh-token")
	if err != nil {
		t.Fatalf("FetchIssues failed: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue (PR filtered out), got %d", len(issues))
	}
	if issues[0].ID != "101" || issues[0].Title != "Issue 1" || issues[0].Labels[0] != "bug" {
		t.Errorf("unexpected issue details: %+v", issues[0])
	}
}

func TestGitHubProvider_UpdateIssueStatusAndComment(t *testing.T) {
	patchCalled := false
	labelCalled := false
	commentCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/issues/101":
			patchCalled = true
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["state"] != "closed" {
				t.Errorf("expected state closed, got %v", body["state"])
			}
			w.WriteHeader(http.StatusOK)
		case "/repos/owner/repo/issues/101/labels":
			labelCalled = true
			w.WriteHeader(http.StatusOK)
		case "/repos/owner/repo/issues/101/comments":
			commentCalled = true
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["body"] != "Finished work!" {
				t.Errorf("expected comment text 'Finished work!', got %q", body["body"])
			}
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	provider := github.NewProvider(github.WithBaseURL(server.URL), github.WithHTTPClient(server.Client()))
	integration := &models.ProjectIntegration{
		ExternalID: "owner/repo",
	}

	// Update issue status to closed
	if err := provider.UpdateIssueStatus(context.Background(), integration, "test-token", "101", "closed"); err != nil {
		t.Fatalf("UpdateIssueStatus closed failed: %v", err)
	}
	if !patchCalled {
		t.Errorf("expected PATCH /repos/owner/repo/issues/101 to be called")
	}

	// Update issue status with agent label transition
	if err := provider.UpdateIssueStatus(context.Background(), integration, "test-token", "101", "in_progress"); err != nil {
		t.Fatalf("UpdateIssueStatus in_progress failed: %v", err)
	}
	if !labelCalled {
		t.Errorf("expected POST /repos/owner/repo/issues/101/labels to be called")
	}

	// Add comment
	if err := provider.AddComment(context.Background(), integration, "test-token", "101", "Finished work!"); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
	if !commentCalled {
		t.Errorf("expected comment to be posted")
	}
}

func TestGitHubProvider_CreatePullRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/pulls" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["head"] != "feat/branch" || body["base"] != "master" || body["title"] != "PR Title" {
			t.Errorf("unexpected PR request body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"html_url": "https://github.com/owner/repo/pull/55",
		})
	}))
	defer server.Close()

	provider := github.NewProvider(github.WithBaseURL(server.URL), github.WithHTTPClient(server.Client()))
	integration := &models.ProjectIntegration{
		ExternalID: "owner/repo",
		Config: models.JSONMap{
			"base_branch": "master",
		},
	}

	prURL, err := provider.CreatePullRequest(context.Background(), integration, "test-token", "feat/branch", "PR Title", "Body text")
	if err != nil {
		t.Fatalf("CreatePullRequest failed: %v", err)
	}
	if prURL != "https://github.com/owner/repo/pull/55" {
		t.Errorf("expected pr url https://github.com/owner/repo/pull/55, got %s", prURL)
	}
}
