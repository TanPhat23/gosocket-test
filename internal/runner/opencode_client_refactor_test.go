package runner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agent-task-manager/internal/runner"
)

func newRefactorClient(t *testing.T, body string) runner.OpenCodeClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return runner.NewOpenCodeClient(srv.URL, "pw", nil)
}

func TestOpenCodeClient_GetSession_TimeConversion(t *testing.T) {
	c := newRefactorClient(t, `{"data":{"id":"s1","time":{"created":1700000000123,"updated":0}}}`)
	s, err := c.GetSession(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if s.CreatedAt == nil || !s.CreatedAt.Equal(time.UnixMilli(1700000000123)) {
		t.Errorf("created: %v", s.CreatedAt)
	}
	if s.UpdatedAt != nil {
		t.Errorf("zero updated must be nil, got %v", s.UpdatedAt)
	}
}

func TestOpenCodeClient_GetSessionMessages_WrappedRolesTimesSort(t *testing.T) {
	c := newRefactorClient(t, `{"data":[
		{"id":"c","type":"system","time":{"created":3000,"completed":4000}},
		{"id":"a","type":"user","time":{"created":1000}},
		{"id":"b","type":"assistant","role":"custom","time":{"created":2000}},
		{"id":"d","type":"other","time":{"created":2500}}]}`)
	msgs, err := c.GetSessionMessages(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"a", "b", "d", "c"}
	wantRole := []string{"user", "custom", "", "system"}
	if len(msgs) != 4 {
		t.Fatalf("len %d", len(msgs))
	}
	for i, m := range msgs {
		if m.ID != wantIDs[i] || m.Role != wantRole[i] {
			t.Errorf("[%d] got %s/%q want %s/%q", i, m.ID, m.Role, wantIDs[i], wantRole[i])
		}
	}
	if msgs[3].CompletedAt == nil || !msgs[3].CompletedAt.Equal(time.UnixMilli(4000)) {
		t.Errorf("completed: %v", msgs[3].CompletedAt)
	}
	if msgs[0].CompletedAt != nil {
		t.Errorf("completed must be nil")
	}
}

func TestOpenCodeClient_GetSessionMessages_NilCreatedKeepsOrder(t *testing.T) {
	c := newRefactorClient(t, `{"data":[
		{"id":"x","type":"user","time":{"created":5000}},
		{"id":"y","type":"user"},
		{"id":"z","type":"user","time":{"created":1000}}]}`)
	msgs, err := c.GetSessionMessages(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	for _, m := range msgs {
		got += m.ID
	}
	if got != "xyz" {
		t.Errorf("order with nil CreatedAt must be untouched, got %s", got)
	}
}

func TestOpenCodeClient_GetSessionMessages_DirectSorted(t *testing.T) {
	c := newRefactorClient(t, `[{"id":"b","created_at":"2024-01-02T00:00:00Z"},{"id":"a","created_at":"2024-01-01T00:00:00Z"}]`)
	msgs, err := c.GetSessionMessages(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].ID != "a" || msgs[1].ID != "b" {
		t.Errorf("unexpected: %+v", msgs)
	}
}
