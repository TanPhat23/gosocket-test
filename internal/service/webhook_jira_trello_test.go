package service_test

import (
	"context"
	"errors"
	"testing"

	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
)

type recStore struct {
	inner     integrations.IdempotencyStore
	recordErr error
	calls     []string
}

func (r *recStore) RecordEvent(ctx context.Context, p, proj, id string) (bool, error) {
	r.calls = append(r.calls, "record:"+p+":"+proj+":"+id)
	if r.recordErr != nil {
		return false, r.recordErr
	}
	return r.inner.RecordEvent(ctx, p, proj, id)
}
func (r *recStore) MarkProcessed(ctx context.Context, p, proj, id string) error {
	r.calls = append(r.calls, "processed:"+p+":"+proj+":"+id)
	return r.inner.MarkProcessed(ctx, p, proj, id)
}
func (r *recStore) MarkFailed(ctx context.Context, p, proj, id, msg string) error {
	r.calls = append(r.calls, "failed:"+p+":"+proj+":"+id)
	return r.inner.MarkFailed(ctx, p, proj, id, msg)
}

func newJiraTrelloSvc(t *testing.T, store integrations.IdempotencyStore) service.WebhookService {
	t.Helper()
	db := setupTestDB(t)
	integRepo := repository.NewIntegrationRepository(db)
	ctx := context.Background()
	_ = integRepo.Create(ctx, &models.ProjectIntegration{ID: "ij", ProjectID: "proj-j", CredentialID: "c", Provider: models.IntegrationJira, ExternalID: "JKEY", Enabled: true, Config: models.JSONMap{"webhook_secret": hardeningWebhookSecret}})
	_ = integRepo.Create(ctx, &models.ProjectIntegration{ID: "it", ProjectID: "proj-t", CredentialID: "c", Provider: models.IntegrationTrello, ExternalID: "board1", Enabled: true, Config: models.JSONMap{"webhook_secret": hardeningWebhookSecret, "webhook_callback_url": hardeningCallbackURL}})
	return service.NewWebhookService(integRepo, repository.NewCredentialRepository(db), repository.NewTaskRepository(db), store, nil, nil)
}

func TestJiraWebhook_Characterization(t *testing.T) {
	ctx := context.Background()
	rs := &recStore{inner: integrations.NewMemoryIdempotencyStore()}
	svc := newJiraTrelloSvc(t, rs)
	good := []byte(`{"issue":{"fields":{"project":{"key":"JKEY"}}}}`)

	if _, err := svc.HandleJiraWebhook(ctx, []byte(`{bad`), "e", hexSig(hardeningWebhookSecret, []byte(`{bad`))); !errors.Is(err, service.ErrInvalidWebhookPayload) || err.Error() != "invalid webhook payload" {
		t.Fatalf("bad payload: %v", err)
	}
	r, err := svc.HandleJiraWebhook(ctx, []byte(`{}`), "e0", hexSig(hardeningWebhookSecret, []byte(`{}`)))
	if err != nil || r.Status != "ignored" || r.Reason != "missing project key" || r.EventID != "e0" {
		t.Fatalf("missing key: %+v %v", r, err)
	}
	r, err = svc.HandleJiraWebhook(ctx, []byte(`{"issue":{"fields":{"project":{"key":"NOPE"}}}}`), "e1", hexSig(hardeningWebhookSecret, []byte(`{"issue":{"fields":{"project":{"key":"NOPE"}}}}`)))
	if err != nil || r.Status != "ignored" || r.Reason != "no active integration" || r.EventID != "e1" {
		t.Fatalf("no integ: %+v %v", r, err)
	}
	if len(rs.calls) != 0 {
		t.Fatalf("unexpected store calls %v", rs.calls)
	}
	r, err = svc.HandleJiraWebhook(ctx, good, "ev-1", hexSig(hardeningWebhookSecret, good))
	if err != nil || r.Status != "processed" || r.EventID != "ev-1" || r.Reason != "" || r.TaskID != "" || r.DeliveryID != "" {
		t.Fatalf("first: %+v %v", r, err)
	}
	r, err = svc.HandleJiraWebhook(ctx, good, "ev-1", hexSig(hardeningWebhookSecret, good))
	if err != nil || r.Status != "duplicate" || r.EventID != "ev-1" {
		t.Fatalf("dup: %+v %v", r, err)
	}
	want := []string{"record:jira:proj-j:ev-1", "processed:jira:proj-j:ev-1", "record:jira:proj-j:ev-1"}
	if len(rs.calls) != 3 || rs.calls[0] != want[0] || rs.calls[1] != want[1] || rs.calls[2] != want[2] {
		t.Fatalf("calls %v", rs.calls)
	}
	// empty event id generates one
	r, err = svc.HandleJiraWebhook(ctx, good, "", hexSig(hardeningWebhookSecret, good))
	if err != nil || r.Status != "processed" || r.EventID == "" {
		t.Fatalf("generated: %+v %v", r, err)
	}
	// store error
	rs.recordErr = errors.New("boom")
	r, err = svc.HandleJiraWebhook(ctx, good, "ev-2", hexSig(hardeningWebhookSecret, good))
	if r != nil || err == nil || err.Error() != "idempotency check failed: boom" {
		t.Fatalf("store err: %+v %v", r, err)
	}
}

func TestJiraWebhook_NilStore(t *testing.T) {
	svc := newJiraTrelloSvc(t, nil)
	r, err := svc.HandleJiraWebhook(context.Background(), []byte(`{"issue":{"fields":{"project":{"key":"JKEY"}}}}`), "ev", hexSig(hardeningWebhookSecret, []byte(`{"issue":{"fields":{"project":{"key":"JKEY"}}}}`)))
	if err != nil || r.Status != "processed" || r.EventID != "ev" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestTrelloWebhook_Characterization(t *testing.T) {
	ctx := context.Background()
	rs := &recStore{inner: integrations.NewMemoryIdempotencyStore()}
	svc := newJiraTrelloSvc(t, rs)

	r, err := svc.HandleTrelloWebhook(ctx, nil, true, b64Sig(hardeningWebhookSecret, nil, hardeningCallbackURL))
	if err != nil || r.Status != "processed" || r.EventID != "" {
		t.Fatalf("head: %+v %v", r, err)
	}
	if _, err := svc.HandleTrelloWebhook(ctx, []byte(`{bad`), false, b64Sig(hardeningWebhookSecret, []byte(`{bad`), hardeningCallbackURL)); err == nil || err.Error() != "invalid webhook payload" {
		t.Fatalf("bad: %v", err)
	}
	r, err = svc.HandleTrelloWebhook(ctx, []byte(`{}`), false, b64Sig(hardeningWebhookSecret, []byte(`{}`), hardeningCallbackURL))
	if err != nil || r.Status != "ignored" || r.Reason != "missing board id" || r.EventID != "" {
		t.Fatalf("no board: %+v %v", r, err)
	}
	r, err = svc.HandleTrelloWebhook(ctx, []byte(`{"model":{"id":"other"},"action":{"id":"a0"}}`), false, b64Sig(hardeningWebhookSecret, []byte(`{"model":{"id":"other"},"action":{"id":"a0"}}`), hardeningCallbackURL))
	if err != nil || r.Status != "ignored" || r.Reason != "no active integration" || r.EventID != "a0" {
		t.Fatalf("no integ: %+v %v", r, err)
	}
	// idBoard preferred; fall back to model.id
	p1 := []byte(`{"model":{"idBoard":"board1","id":"zzz"},"action":{"id":"a1"}}`)
	p2 := []byte(`{"model":{"id":"board1"},"action":{"id":"a2"}}`)
	r, err = svc.HandleTrelloWebhook(ctx, p1, false, b64Sig(hardeningWebhookSecret, p1, hardeningCallbackURL))
	if err != nil || r.Status != "processed" || r.EventID != "a1" {
		t.Fatalf("p1: %+v %v", r, err)
	}
	r, err = svc.HandleTrelloWebhook(ctx, p1, false, b64Sig(hardeningWebhookSecret, p1, hardeningCallbackURL))
	if err != nil || r.Status != "duplicate" || r.EventID != "a1" {
		t.Fatalf("dup: %+v %v", r, err)
	}
	r, err = svc.HandleTrelloWebhook(ctx, p2, false, b64Sig(hardeningWebhookSecret, p2, hardeningCallbackURL))
	if err != nil || r.Status != "processed" || r.EventID != "a2" {
		t.Fatalf("p2: %+v %v", r, err)
	}
	want := []string{"record:trello:proj-t:a1", "processed:trello:proj-t:a1", "record:trello:proj-t:a1", "record:trello:proj-t:a2", "processed:trello:proj-t:a2"}
	if len(rs.calls) != len(want) {
		t.Fatalf("calls %v", rs.calls)
	}
	for i := range want {
		if rs.calls[i] != want[i] {
			t.Fatalf("calls %v", rs.calls)
		}
	}
	r, err = svc.HandleTrelloWebhook(ctx, []byte(`{"model":{"idBoard":"board1"}}`), false, b64Sig(hardeningWebhookSecret, []byte(`{"model":{"idBoard":"board1"}}`), hardeningCallbackURL))
	if err != nil || r.Status != "processed" || r.EventID == "" {
		t.Fatalf("generated: %+v %v", r, err)
	}
	rs.recordErr = errors.New("boom")
	r, err = svc.HandleTrelloWebhook(ctx, p1, false, b64Sig(hardeningWebhookSecret, p1, hardeningCallbackURL))
	if r != nil || err == nil || err.Error() != "idempotency check failed: boom" {
		t.Fatalf("store err: %+v %v", r, err)
	}
}

func TestTrelloWebhook_NilStore(t *testing.T) {
	svc := newJiraTrelloSvc(t, nil)
	r, err := svc.HandleTrelloWebhook(context.Background(), []byte(`{"model":{"idBoard":"board1"},"action":{"id":"x"}}`), false, b64Sig(hardeningWebhookSecret, []byte(`{"model":{"idBoard":"board1"},"action":{"id":"x"}}`), hardeningCallbackURL))
	if err != nil || r.Status != "processed" || r.EventID != "x" {
		t.Fatalf("%+v %v", r, err)
	}
}
