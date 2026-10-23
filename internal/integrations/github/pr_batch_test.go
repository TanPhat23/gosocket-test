package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"agent-task-manager/internal/integrations/github"
)

type graphQLRecorder struct {
	mu      sync.Mutex
	queries []string
	paths   []string
	auths   []string
}

// newGraphQLServer answers every request with respond(query) and records what it received.
func newGraphQLServer(t *testing.T, respond func(query string) (int, string)) (*httptest.Server, *graphQLRecorder) {
	t.Helper()
	rec := &graphQLRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(raw, &req)

		rec.mu.Lock()
		rec.queries = append(rec.queries, req.Query)
		rec.paths = append(rec.paths, r.URL.Path)
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		rec.mu.Unlock()

		status, payload := respond(req.Query)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func TestGetPullRequestStatuses_OneRequestPerBatch(t *testing.T) {
	srv, rec := newGraphQLServer(t, func(string) (int, string) {
		return http.StatusOK, `{
			"data": {
				"pr0": {"pullRequest": {"number": 7, "state": "MERGED", "merged": true, "mergedAt": "2026-10-17T10:00:00Z", "url": "https://github.com/owner/repo-a/pull/7"}},
				"pr1": {"pullRequest": {"number": 9, "state": "OPEN", "merged": false, "mergedAt": null, "url": "https://github.com/owner/repo-b/pull/9"}},
				"pr2": null
			},
			"errors": [{"message": "Could not resolve to a PullRequest with the number of 404."}]
		}`
	})
	p := github.NewProvider(github.WithBaseURL(srv.URL))
	urls := []string{
		"https://github.com/owner/repo-a/pull/7",
		"https://github.com/owner/repo-b/pull/9",
		"https://github.com/owner/repo-b/pull/404",
	}

	results := p.GetPullRequestStatuses(context.Background(), "tok", urls)

	if n := len(rec.queries); n != 1 {
		t.Fatalf("expected one GraphQL request for three pull requests, got %d", n)
	}
	if rec.paths[0] != "/graphql" {
		t.Errorf("expected /graphql, got %s", rec.paths[0])
	}
	if rec.auths[0] != "Bearer tok" {
		t.Errorf("expected bearer token, got %q", rec.auths[0])
	}
	for _, want := range []string{
		`pr0: repository(owner: "owner", name: "repo-a") { pullRequest(number: 7)`,
		`pr1: repository(owner: "owner", name: "repo-b") { pullRequest(number: 9)`,
		`pr2: repository(owner: "owner", name: "repo-b") { pullRequest(number: 404)`,
	} {
		if !strings.Contains(rec.queries[0], want) {
			t.Errorf("query missing %q:\n%s", want, rec.queries[0])
		}
	}

	merged := results[urls[0]]
	if merged.Err != nil || merged.Status == nil || !merged.Status.Merged {
		t.Fatalf("expected merged status, got %+v", merged)
	}
	if merged.Status.State != "closed" || merged.Status.MergedAt != "2026-10-17T10:00:00Z" || merged.Status.Number != 7 {
		t.Errorf("unexpected merged status %+v", merged.Status)
	}

	open := results[urls[1]]
	if open.Err != nil || open.Status == nil || open.Status.Merged || open.Status.State != "open" {
		t.Errorf("expected open status, got %+v", open)
	}

	missing := results[urls[2]]
	if missing.Err == nil || missing.Status != nil {
		t.Errorf("expected an error for the missing pull request, got %+v", missing)
	}
}

func TestGetPullRequestStatuses_SplitsLargeBatches(t *testing.T) {
	srv, rec := newGraphQLServer(t, func(string) (int, string) {
		return http.StatusOK, `{"data": {}}`
	})
	p := github.NewProvider(github.WithBaseURL(srv.URL))
	var urls []string
	for n := 1; n <= 51; n++ {
		urls = append(urls, fmt.Sprintf("https://github.com/owner/repo/pull/%d", n))
	}

	results := p.GetPullRequestStatuses(context.Background(), "tok", urls)

	if n := len(rec.queries); n != 2 {
		t.Fatalf("expected 51 pull requests to take two requests, got %d", n)
	}
	if len(results) != len(urls) {
		t.Errorf("expected a result for each of %d URLs, got %d", len(urls), len(results))
	}
}

func TestGetPullRequestStatuses_RejectsUnsafeOrNonPullURLsWithoutRequest(t *testing.T) {
	srv, rec := newGraphQLServer(t, func(string) (int, string) {
		return http.StatusOK, `{"data": {}}`
	})
	p := github.NewProvider(github.WithBaseURL(srv.URL))
	urls := []string{
		"https://github.com/owner/repo/issues/3",
		"https://github.com/own%22er/repo/pull/3",
		"https://github.com/owner/repo/pull/0",
		"not a url",
	}

	results := p.GetPullRequestStatuses(context.Background(), "tok", urls)

	if n := len(rec.queries); n != 0 {
		t.Errorf("expected no GraphQL request for invalid URLs, got %d", n)
	}
	for _, u := range urls {
		if r := results[u]; r.Err == nil || r.Status != nil {
			t.Errorf("%q: expected an error and no status, got %+v", u, r)
		}
	}
}

func TestGetPullRequestStatuses_FailedBatchFailsOnlyItsURLs(t *testing.T) {
	srv, rec := newGraphQLServer(t, func(string) (int, string) {
		return http.StatusBadGateway, "bad gateway"
	})
	p := github.NewProvider(github.WithBaseURL(srv.URL))
	urls := []string{
		"https://github.com/owner/repo/pull/1",
		"https://github.com/owner/repo/pull/2",
	}

	results := p.GetPullRequestStatuses(context.Background(), "tok", urls)

	if n := len(rec.queries); n != 1 {
		t.Fatalf("expected one request, got %d", n)
	}
	for _, u := range urls {
		if r := results[u]; r.Err == nil || r.Status != nil {
			t.Errorf("%q: expected the batch error, got %+v", u, r)
		}
	}
}

func TestGetPullRequestStatuses_EnterpriseUsesAPIGraphQLPath(t *testing.T) {
	srv, rec := newGraphQLServer(t, func(string) (int, string) {
		return http.StatusOK, `{"data": {}}`
	})
	p := github.NewProvider(github.WithBaseURL(srv.URL + "/api/v3"))

	p.GetPullRequestStatuses(context.Background(), "tok", []string{"https://ghe.example.com/owner/repo/pull/1"})

	if len(rec.paths) != 1 || rec.paths[0] != "/api/graphql" {
		t.Errorf("expected /api/graphql, got %v", rec.paths)
	}
}

func TestGetPullRequestStatuses_EmptyInputMakesNoRequest(t *testing.T) {
	srv, rec := newGraphQLServer(t, func(string) (int, string) {
		return http.StatusOK, `{"data": {}}`
	})
	p := github.NewProvider(github.WithBaseURL(srv.URL))

	results := p.GetPullRequestStatuses(context.Background(), "tok", nil)

	if len(rec.queries) != 0 || len(results) != 0 {
		t.Errorf("expected no request and no results, got %d requests and %d results", len(rec.queries), len(results))
	}
}
