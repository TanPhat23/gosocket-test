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

func TestGitHubProvider_ListRepositories(t *testing.T) {
	t.Run("Success returns mapped repositories", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/user/repos" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.URL.Query().Get("sort") != "updated" || r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("affiliation") != "owner,collaborator,organization_member" {
				t.Errorf("unexpected query params: %s", r.URL.RawQuery)
			}
			if r.Header.Get("Authorization") != "Bearer ghp_valid_token" {
				t.Errorf("missing or invalid authorization header: %s", r.Header.Get("Authorization"))
			}
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				t.Errorf("missing or invalid accept header: %s", r.Header.Get("Accept"))
			}

			repos := []map[string]any{
				{
					"id":             123456,
					"name":           "agent-task-manager",
					"full_name":      "owner/agent-task-manager",
					"private":        true,
					"description":    "Autonomous agent task manager",
					"html_url":       "https://github.com/owner/agent-task-manager",
					"default_branch": "main",
				},
				{
					"id":             789012,
					"name":           "frontend-ui",
					"full_name":      "org/frontend-ui",
					"private":        false,
					"description":    "Web client UI",
					"html_url":       "https://github.com/org/frontend-ui",
					"default_branch": "master",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(repos)
		}))
		defer server.Close()

		provider := github.NewProvider(github.WithBaseURL(server.URL), github.WithHTTPClient(server.Client()))
		repos, err := provider.ListRepositories(context.Background(), "ghp_valid_token")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(repos) != 2 {
			t.Fatalf("expected 2 repos, got %d", len(repos))
		}
		if repos[0].ID != 123456 || repos[0].FullName != "owner/agent-task-manager" || !repos[0].Private || repos[0].DefaultBranch != "main" {
			t.Errorf("repo 0 mismatch: %+v", repos[0])
		}
		if repos[1].ID != 789012 || repos[1].FullName != "org/frontend-ui" || repos[1].Private || repos[1].DefaultBranch != "master" {
			t.Errorf("repo 1 mismatch: %+v", repos[1])
		}
	})

	t.Run("401 Unauthorized returns clean error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message": "Bad credentials"}`))
		}))
		defer server.Close()

		provider := github.NewProvider(github.WithBaseURL(server.URL), github.WithHTTPClient(server.Client()))
		_, err := provider.ListRepositories(context.Background(), "ghp_expired_token")
		if err == nil || err.Error() != "invalid or expired github token" {
			t.Fatalf("expected 'invalid or expired github token', got %v", err)
		}
	})

	t.Run("403 Forbidden with rate limit message returns rate limit error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message": "API rate limit exceeded for user ID 123"}`))
		}))
		defer server.Close()

		provider := github.NewProvider(github.WithBaseURL(server.URL), github.WithHTTPClient(server.Client()))
		_, err := provider.ListRepositories(context.Background(), "ghp_token")
		if err == nil || err.Error() != "github api rate limit exceeded" {
			t.Fatalf("expected 'github api rate limit exceeded', got %v", err)
		}
	})

	t.Run("429 Too Many Requests returns rate limit error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message": "too many requests"}`))
		}))
		defer server.Close()

		provider := github.NewProvider(github.WithBaseURL(server.URL), github.WithHTTPClient(server.Client()))
		_, err := provider.ListRepositories(context.Background(), "ghp_token")
		if err == nil || err.Error() != "github api rate limit exceeded" {
			t.Fatalf("expected 'github api rate limit exceeded', got %v", err)
		}
	})

	t.Run("Empty token returns error without HTTP request", func(t *testing.T) {
		provider := github.NewProvider()
		_, err := provider.ListRepositories(context.Background(), "")
		if err == nil || err.Error() != "github token cannot be empty" {
			t.Fatalf("expected empty token error, got %v", err)
		}
	})
}
