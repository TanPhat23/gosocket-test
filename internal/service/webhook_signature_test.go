package service_test

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/service"
)

func hexSig(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func b64Sig(secret string, body []byte, cb string) string {
	m := hmac.New(sha1.New, []byte(secret))
	m.Write(body)
	m.Write([]byte(cb))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// jira delivers a correctly signed Jira webhook.
func (f *hardeningFixture) jira(body []byte, eventID string) (*dto.WebhookResultDTO, error) {
	return f.svc.HandleJiraWebhook(f.ctx, body, eventID, hexSig(hardeningWebhookSecret, body))
}

// trello delivers a correctly signed Trello webhook (HEAD requests are unsigned).
func (f *hardeningFixture) trello(body []byte, isHead bool) (*dto.WebhookResultDTO, error) {
	return f.svc.HandleTrelloWebhook(f.ctx, body, isHead, b64Sig(hardeningWebhookSecret, body, hardeningCallbackURL))
}

func TestHandleJiraWebhook_RequiresValidSignature(t *testing.T) {
	f := newHardeningFixture(t)
	f.addIntegration(t, models.ProjectIntegration{ID: "ij", ProjectID: "pj", CredentialID: "c", Provider: models.IntegrationJira, ExternalID: "KEY", Enabled: true})
	body := []byte(`{"issue":{"fields":{"project":{"key":"KEY"}}}}`)

	for name, sig := range map[string]string{
		"missing":      "",
		"wrong secret": hexSig("other", body),
		"no prefix":    hexSig(hardeningWebhookSecret, body)[len("sha256="):],
		"tampered":     hexSig(hardeningWebhookSecret, append(body, ' ')),
	} {
		if _, err := f.svc.HandleJiraWebhook(f.ctx, body, "ev-1", sig); !errors.Is(err, service.ErrInvalidSignature) {
			t.Errorf("%s: err = %v, want ErrInvalidSignature", name, err)
		}
	}
	// Rejected deliveries must not consume the idempotency slot.
	if res, err := f.jira(body, "ev-1"); err != nil || res.Status != "processed" {
		t.Fatalf("valid delivery after rejections: %+v %v", res, err)
	}
}

func TestHandleJiraWebhook_FailsClosedWithoutConfiguredSecret(t *testing.T) {
	f := newHardeningFixture(t)
	f.addIntegration(t, models.ProjectIntegration{ID: "ij", ProjectID: "pj", CredentialID: "c", Provider: models.IntegrationJira, ExternalID: "KEY", Enabled: true, Config: models.JSONMap{}})
	body := []byte(`{"issue":{"fields":{"project":{"key":"KEY"}}}}`)
	if _, err := f.svc.HandleJiraWebhook(f.ctx, body, "ev", hexSig("", body)); !errors.Is(err, service.ErrInvalidSignature) {
		t.Fatalf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestHandleTrelloWebhook_RequiresValidSignature(t *testing.T) {
	f := newHardeningFixture(t)
	f.addIntegration(t, models.ProjectIntegration{ID: "it", ProjectID: "pt", CredentialID: "c", Provider: models.IntegrationTrello, ExternalID: "board", Enabled: true})
	body := []byte(`{"model":{"idBoard":"board"},"action":{"id":"act-1"}}`)

	for name, sig := range map[string]string{
		"missing":      "",
		"wrong secret": b64Sig("other", body, hardeningCallbackURL),
		"wrong url":    b64Sig(hardeningWebhookSecret, body, hardeningCallbackURL+"x"),
	} {
		if _, err := f.svc.HandleTrelloWebhook(f.ctx, body, false, sig); !errors.Is(err, service.ErrInvalidSignature) {
			t.Errorf("%s: err = %v, want ErrInvalidSignature", name, err)
		}
	}
	if res, err := f.trello(body, false); err != nil || res.Status != "processed" {
		t.Fatalf("valid delivery after rejections: %+v %v", res, err)
	}
}

func TestHandleTrelloWebhook_FailsClosedWithoutCallbackURL(t *testing.T) {
	f := newHardeningFixture(t)
	f.addIntegration(t, models.ProjectIntegration{ID: "it", ProjectID: "pt", CredentialID: "c", Provider: models.IntegrationTrello, ExternalID: "board", Enabled: true, Config: models.JSONMap{"webhook_secret": hardeningWebhookSecret}})
	body := []byte(`{"model":{"idBoard":"board"},"action":{"id":"act-1"}}`)
	if _, err := f.svc.HandleTrelloWebhook(f.ctx, body, false, b64Sig(hardeningWebhookSecret, body, "")); !errors.Is(err, service.ErrInvalidSignature) {
		t.Fatalf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestHandleTrelloWebhook_HeadProbeStaysOpen(t *testing.T) {
	f := newHardeningFixture(t)
	if res, err := f.svc.HandleTrelloWebhook(f.ctx, nil, true, ""); err != nil || res.Status != "processed" {
		t.Fatalf("HEAD probe: %+v %v", res, err)
	}
}
