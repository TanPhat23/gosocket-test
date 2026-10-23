package github_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-task-manager/internal/integrations/github"
	"agent-task-manager/internal/models"
)

// Characterization tests pin the exact error strings and wire behavior of the
// GitHub provider's HTTP plumbing.

type ghOp struct {
	name string
	// errPrefix is the exact error text expected for a 500 response with body "boom".
	errText string
	// decodePrefix is the exact prefix expected for a 200 response with malformed JSON ("" = no decode step).
	decodePrefix string
	run          func(p *github.Provider) error
}

func ghOps() []ghOp {
	integ := &models.ProjectIntegration{ExternalID: "owner/repo"}
	ctx := context.Background()
	return []ghOp{
		{"FetchIssues", "github api returned status 500: boom", "failed to decode issues: ", func(p *github.Provider) error {
			_, err := p.FetchIssues(ctx, integ, "tok")
			return err
		}},
		{"UpdateIssueStatus/state", "github update issue state returned 500: boom", "", func(p *github.Provider) error {
			return p.UpdateIssueStatus(ctx, integ, "tok", "1", "closed")
		}},
		{"UpdateIssueStatus/label", "github add label returned 500: boom", "", func(p *github.Provider) error {
			return p.UpdateIssueStatus(ctx, integ, "tok", "1", "in_progress")
		}},
		{"AddComment", "github add comment returned 500: boom", "", func(p *github.Provider) error {
			return p.AddComment(ctx, integ, "tok", "1", "hi")
		}},
		{"CreatePullRequest", "github create pull request returned 500: boom", "failed to decode pr response: ", func(p *github.Provider) error {
			_, err := p.CreatePullRequest(ctx, integ, "tok", "br", "t", "b")
			return err
		}},
		{"GetPullRequestStatus", "github get pull request returned 500: boom", "failed to decode pull request details: ", func(p *github.Provider) error {
			_, err := p.GetPullRequestStatus(ctx, integ, "tok", "7")
			return err
		}},
		{"ListRepositories", "github list repositories returned 500: boom", "failed to decode github repositories: ", func(p *github.Provider) error {
			_, err := p.ListRepositories(ctx, "tok")
			return err
		}},
		{"ListBranches", "github list branches returned 500: boom", "failed to decode github branches: ", func(p *github.Provider) error {
			_, err := p.ListBranches(ctx, "tok", "o", "r")
			return err
		}},
	}
}

func ghServer(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func TestGitHubChar_NonOKStatusErrors(t *testing.T) {
	for _, op := range ghOps() {
		t.Run(op.name, func(t *testing.T) {
			s := ghServer(500, "boom")
			defer s.Close()
			p := github.NewProvider(github.WithBaseURL(s.URL), github.WithHTTPClient(s.Client()))
			err := op.run(p)
			if err == nil || err.Error() != op.errText {
				t.Fatalf("got %v, want %q", err, op.errText)
			}
		})
	}
}

func TestGitHubChar_MalformedJSON(t *testing.T) {
	for _, op := range ghOps() {
		if op.decodePrefix == "" {
			continue
		}
		t.Run(op.name, func(t *testing.T) {
			s := ghServer(200, "{not json")
			defer s.Close()
			p := github.NewProvider(github.WithBaseURL(s.URL), github.WithHTTPClient(s.Client()))
			err := op.run(p)
			if err == nil || !strings.HasPrefix(err.Error(), op.decodePrefix) {
				t.Fatalf("got %v, want prefix %q", err, op.decodePrefix)
			}
		})
	}
}

func TestGitHubChar_TransportError(t *testing.T) {
	for _, op := range ghOps() {
		t.Run(op.name, func(t *testing.T) {
			s := ghServer(200, "{}")
			url := s.URL
			client := s.Client()
			s.Close()
			p := github.NewProvider(github.WithBaseURL(url), github.WithHTTPClient(client))
			err := op.run(p)
			if err == nil || !strings.HasPrefix(err.Error(), "github api request failed: ") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestGitHubChar_SuccessStatusesAndHeaders(t *testing.T) {
	var gotAccept, gotAuth, gotCT string
	var gotBody string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept, gotAuth, gotCT = r.Header.Get("Accept"), r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated) // any 2xx is accepted
		_, _ = io.WriteString(w, `{"html_url":"https://x/pr/1"}`)
	}))
	defer s.Close()
	p := github.NewProvider(github.WithBaseURL(s.URL), github.WithHTTPClient(s.Client()))
	integ := &models.ProjectIntegration{ExternalID: "owner/repo"}

	url, err := p.CreatePullRequest(context.Background(), integ, "tok", "br", "t", "b")
	if err != nil || url != "https://x/pr/1" {
		t.Fatalf("url=%q err=%v", url, err)
	}
	if gotAccept != "application/vnd.github+json" || gotAuth != "Bearer tok" || gotCT != "application/json" {
		t.Errorf("headers: %q %q %q", gotAccept, gotAuth, gotCT)
	}
	if !strings.Contains(gotBody, `"head":"br"`) {
		t.Errorf("body: %s", gotBody)
	}

	// GET with empty token: no Authorization, no Content-Type.
	if _, err := p.FetchIssues(context.Background(), integ, ""); err == nil {
		// body is an object, not an array -> decode error expected; headers still checked
		t.Fatalf("expected decode error")
	}
	if gotAuth != "" || gotCT != "" || gotAccept != "application/vnd.github+json" {
		t.Errorf("GET headers: %q %q %q", gotAccept, gotAuth, gotCT)
	}
}

func TestGitHubChar_ListRepositoriesStatusMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"401", 401, "x", "invalid or expired github token"},
		{"403 rate limit", 403, "API Rate Limit exceeded", "github api rate limit exceeded"},
		{"403 other", 403, "nope", "github api access forbidden: nope"},
		{"429", 429, "x", "github api rate limit exceeded"},
		{"404", 404, "gone", "github list repositories returned 404: gone"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := ghServer(c.status, c.body)
			defer s.Close()
			p := github.NewProvider(github.WithBaseURL(s.URL), github.WithHTTPClient(s.Client()))
			_, err := p.ListRepositories(context.Background(), "tok")
			if err == nil || err.Error() != c.want {
				t.Fatalf("got %v want %q", err, c.want)
			}
		})
	}
}

func TestGitHubChar_ListBranchesStatusMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{401, "x", "invalid or expired github token"},
		{404, "x", "repository not found or token lacks access"},
		{403, "denied", "github list branches returned 403: denied"},
	}
	for _, c := range cases {
		s := ghServer(c.status, c.body)
		p := github.NewProvider(github.WithBaseURL(s.URL), github.WithHTTPClient(s.Client()))
		_, err := p.ListBranches(context.Background(), "tok", "o", "r")
		s.Close()
		if err == nil || err.Error() != c.want {
			t.Errorf("status %d: got %v want %q", c.status, err, c.want)
		}
	}
}

func TestGitHubChar_BoundaryStatuses(t *testing.T) {
	for _, tc := range []struct {
		status int
		ok     bool
	}{{200, true}, {299, true}, {300, false}} {
		s := ghServer(tc.status, "[]")
		p := github.NewProvider(github.WithBaseURL(s.URL), github.WithHTTPClient(s.Client()))
		_, err := p.FetchIssues(context.Background(), &models.ProjectIntegration{ExternalID: "o/r"}, "t")
		s.Close()
		if (err == nil) != tc.ok {
			t.Errorf("status %d: err=%v", tc.status, err)
		}
	}
}
