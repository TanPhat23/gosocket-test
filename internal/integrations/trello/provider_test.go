package trello_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agent-task-manager/internal/integrations/trello"
	"agent-task-manager/internal/models"
)

func TestTrelloProvider_FetchIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1/boards/board-123/cards" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "app-key" || r.URL.Query().Get("token") != "secret-token" {
			t.Errorf("missing or invalid key/token in query: %v", r.URL.RawQuery)
		}

		cards := []map[string]any{
			{
				"id":      "card-99",
				"name":    "Setup DB",
				"desc":    "Postgres setup instructions",
				"closed":  false,
				"url":     "https://trello.com/c/card-99",
				"idList":  "list-todo",
				"labels":  []map[string]string{{"name": "infra"}},
			},
		}
		_ = json.NewEncoder(w).Encode(cards)
	}))
	defer server.Close()

	provider := trello.NewProvider(trello.WithBaseURL(server.URL), trello.WithHTTPClient(server.Client()))
	if provider.Provider() != models.IntegrationTrello {
		t.Errorf("expected %v, got %v", models.IntegrationTrello, provider.Provider())
	}

	integration := &models.ProjectIntegration{
		ExternalID: "board-123",
		Config: models.JSONMap{
			"api_key": "app-key",
		},
	}

	issues, err := provider.FetchIssues(context.Background(), integration, "secret-token")
	if err != nil {
		t.Fatalf("FetchIssues failed: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].ID != "card-99" || issues[0].Title != "Setup DB" || issues[0].Labels[0] != "infra" {
		t.Errorf("unexpected card data: %+v", issues[0])
	}
}

func TestTrelloProvider_UpdateIssueStatusAndComment(t *testing.T) {
	cardMoved := false
	commentAdded := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/1/cards/card-99":
			if r.Method == http.MethodPut {
				cardMoved = true
				if r.URL.Query().Get("idList") != "list-done" {
					t.Errorf("expected idList=list-done, got %s", r.URL.Query().Get("idList"))
				}
				w.WriteHeader(http.StatusOK)
			}
		case "/1/cards/card-99/actions/comments":
			if r.Method == http.MethodPost {
				commentAdded = true
				if r.URL.Query().Get("text") != "Completed" {
					t.Errorf("expected text=Completed, got %s", r.URL.Query().Get("text"))
				}
				w.WriteHeader(http.StatusOK)
			}
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	provider := trello.NewProvider(trello.WithBaseURL(server.URL), trello.WithHTTPClient(server.Client()))
	integration := &models.ProjectIntegration{
		ExternalID: "board-123",
		Config: models.JSONMap{
			"api_key": "app-key",
			"lists": map[string]any{
				"done": "list-done",
			},
		},
	}

	if err := provider.UpdateIssueStatus(context.Background(), integration, "secret-token", "card-99", "done"); err != nil {
		t.Fatalf("UpdateIssueStatus failed: %v", err)
	}
	if !cardMoved {
		t.Errorf("expected card to be moved")
	}

	if err := provider.AddComment(context.Background(), integration, "secret-token", "card-99", "Completed"); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
	if !commentAdded {
		t.Errorf("expected comment to be added")
	}
}
