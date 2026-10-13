package jira_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-task-manager/internal/integrations/jira"
	"agent-task-manager/internal/models"
)

type jiraOp struct {
	name         string
	errText      string // 500 + "boom"
	decodePrefix string // 200 + malformed JSON
	run          func(p *jira.Provider, host string) error
}

func jiraInteg(host string) *models.ProjectIntegration {
	return &models.ProjectIntegration{
		ExternalID: "PROJ",
		Config:     models.JSONMap{"host": host, "user_email": "u@example.com"},
	}
}

func jiraOps() []jiraOp {
	ctx := context.Background()
	return []jiraOp{
		{"FetchIssues", "jira search returned status 500: boom", "failed to decode jira response: ", func(p *jira.Provider, h string) error {
			_, err := p.FetchIssues(ctx, jiraInteg(h), "tok")
			return err
		}},
		{"UpdateIssueStatus", "jira fetch transitions returned 500: boom", "failed to decode transitions: ", func(p *jira.Provider, h string) error {
			return p.UpdateIssueStatus(ctx, jiraInteg(h), "tok", "PROJ-1", "done")
		}},
		{"AddComment", "jira add comment returned 500: boom", "", func(p *jira.Provider, h string) error {
			return p.AddComment(ctx, jiraInteg(h), "tok", "PROJ-1", "hi")
		}},
	}
}

func TestJiraChar_NonOKAndMalformedAndTransport(t *testing.T) {
	for _, op := range jiraOps() {
		t.Run(op.name+"/500", func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500)
				_, _ = io.WriteString(w, "boom")
			}))
			defer s.Close()
			p := jira.NewProvider(jira.WithHTTPClient(s.Client()), jira.WithAllowInsecureHTTP(true))
			if err := op.run(p, s.URL); err == nil || err.Error() != op.errText {
				t.Fatalf("got %v want %q", err, op.errText)
			}
		})
		if op.decodePrefix != "" {
			t.Run(op.name+"/malformed", func(t *testing.T) {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.WriteString(w, "{not json")
				}))
				defer s.Close()
				p := jira.NewProvider(jira.WithHTTPClient(s.Client()), jira.WithAllowInsecureHTTP(true))
				if err := op.run(p, s.URL); err == nil || !strings.HasPrefix(err.Error(), op.decodePrefix) {
					t.Fatalf("got %v want prefix %q", err, op.decodePrefix)
				}
			})
		}
		t.Run(op.name+"/transport", func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			url, c := s.URL, s.Client()
			s.Close()
			p := jira.NewProvider(jira.WithHTTPClient(c), jira.WithAllowInsecureHTTP(true))
			if err := op.run(p, url); err == nil || !strings.HasPrefix(err.Error(), "jira api request failed: ") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestJiraChar_ExecuteTransitionFailureAndHeaders(t *testing.T) {
	var gets, posts int
	var postAuth, postCT, postAccept, getCT string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets++
			getCT = r.Header.Get("Content-Type")
			_, _ = io.WriteString(w, `{"transitions":[{"id":"31","name":"Done","to":{"name":"Done"}}]}`)
			return
		}
		posts++
		postAuth, postCT, postAccept = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.Header.Get("Accept")
		w.WriteHeader(400)
		_, _ = io.WriteString(w, "bad")
	}))
	defer s.Close()
	p := jira.NewProvider(jira.WithHTTPClient(s.Client()), jira.WithAllowInsecureHTTP(true))
	err := p.UpdateIssueStatus(context.Background(), jiraInteg(s.URL), "tok", "PROJ-1", "done")
	if err == nil || err.Error() != "jira execute transition returned 400: bad" {
		t.Fatalf("got %v", err)
	}
	if gets != 1 || posts != 1 || getCT != "" {
		t.Errorf("gets=%d posts=%d getCT=%q", gets, posts, getCT)
	}
	if !strings.HasPrefix(postAuth, "Basic ") || postCT != "application/json" || postAccept != "application/json" {
		t.Errorf("headers: %q %q %q", postAuth, postCT, postAccept)
	}

	// no transition match
	err = p.UpdateIssueStatus(context.Background(), jiraInteg(s.URL), "tok", "PROJ-1", "zzz")
	if err == nil || err.Error() != `no transition found matching target status "zzz"` {
		t.Fatalf("got %v", err)
	}
}

func TestJiraChar_BearerFallbackAndStatusBoundaries(t *testing.T) {
	var auth string
	status := 200
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"issues":[]}`)
	}))
	defer s.Close()
	p := jira.NewProvider(jira.WithHTTPClient(s.Client()), jira.WithAllowInsecureHTTP(true))
	integ := &models.ProjectIntegration{ExternalID: "PROJ", Config: models.JSONMap{"host": s.URL}}
	for _, tc := range []struct {
		st int
		ok bool
	}{{201, true}, {299, true}, {300, false}} {
		status = tc.st
		_, err := p.FetchIssues(context.Background(), integ, "tok")
		if (err == nil) != tc.ok {
			t.Errorf("status %d err=%v", tc.st, err)
		}
	}
	if auth != "Bearer tok" {
		t.Errorf("auth=%q", auth)
	}
}
