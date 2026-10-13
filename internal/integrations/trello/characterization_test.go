package trello_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-task-manager/internal/integrations/trello"
	"agent-task-manager/internal/models"
)

type trelloOp struct {
	name         string
	errText      string
	decodePrefix string
	run          func(p *trello.Provider) error
}

func trelloOps() []trelloOp {
	ctx := context.Background()
	integ := &models.ProjectIntegration{ExternalID: "b1", Config: models.JSONMap{"api_key": "k"}}
	return []trelloOp{
		{"FetchIssues", "trello fetch cards returned status 500: boom", "failed to decode cards: ", func(p *trello.Provider) error {
			_, err := p.FetchIssues(ctx, integ, "tok")
			return err
		}},
		{"UpdateIssueStatus", "trello update card list returned status 500: boom", "", func(p *trello.Provider) error {
			return p.UpdateIssueStatus(ctx, integ, "tok", "c1", "list1")
		}},
		{"AddComment", "trello add comment returned status 500: boom", "", func(p *trello.Provider) error {
			return p.AddComment(ctx, integ, "tok", "c1", "hi")
		}},
	}
}

func TestTrelloChar_Errors(t *testing.T) {
	for _, op := range trelloOps() {
		t.Run(op.name+"/500", func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500)
				_, _ = io.WriteString(w, "boom")
			}))
			defer s.Close()
			p := trello.NewProvider(trello.WithBaseURL(s.URL), trello.WithHTTPClient(s.Client()))
			if err := op.run(p); err == nil || err.Error() != op.errText {
				t.Fatalf("got %v want %q", err, op.errText)
			}
		})
		if op.decodePrefix != "" {
			t.Run(op.name+"/malformed", func(t *testing.T) {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.WriteString(w, "{not json")
				}))
				defer s.Close()
				p := trello.NewProvider(trello.WithBaseURL(s.URL), trello.WithHTTPClient(s.Client()))
				if err := op.run(p); err == nil || !strings.HasPrefix(err.Error(), op.decodePrefix) {
					t.Fatalf("got %v want prefix %q", err, op.decodePrefix)
				}
			})
		}
		t.Run(op.name+"/transport", func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			url, c := s.URL, s.Client()
			s.Close()
			p := trello.NewProvider(trello.WithBaseURL(url), trello.WithHTTPClient(c))
			if err := op.run(p); err == nil || !strings.HasPrefix(err.Error(), "trello api request failed: ") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestTrelloChar_SuccessWireFormat(t *testing.T) {
	var method, accept, auth, ct, query string
	status := 200
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, accept, auth, ct, query = r.Method, r.Header.Get("Accept"), r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.URL.RawQuery
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `[]`)
	}))
	defer s.Close()
	p := trello.NewProvider(trello.WithBaseURL(s.URL), trello.WithHTTPClient(s.Client()))
	integ := &models.ProjectIntegration{ExternalID: "b1", Config: models.JSONMap{"api_key": "k"}}

	if err := p.AddComment(context.Background(), integ, "tok", "c1", "a b"); err != nil {
		t.Fatal(err)
	}
	if method != "POST" || accept != "application/json" || auth != "" || ct != "" {
		t.Errorf("wire: %s %q %q %q", method, accept, auth, ct)
	}
	if query != "key=k&text=a+b&token=tok" {
		t.Errorf("query=%q", query)
	}
	for _, tc := range []struct {
		st int
		ok bool
	}{{201, true}, {299, true}, {300, false}} {
		status = tc.st
		if err := p.UpdateIssueStatus(context.Background(), integ, "tok", "c1", "l"); (err == nil) != tc.ok {
			t.Errorf("status %d err=%v", tc.st, err)
		}
	}
}
