package api_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

const (
	hardeningWebhookPath = "/api/v1/webhooks/"
	hardeningHookSecret  = "http-hook-secret"
	hardeningTrelloURL   = "https://atm.example/api/v1/webhooks/trello"
)

func hardeningHookConfig() models.JSONMap {
	return models.JSONMap{"webhook_secret": hardeningHookSecret, "webhook_callback_url": hardeningTrelloURL}
}

func hardeningJiraHeaders(body []byte, extra map[string]string) map[string]string {
	mac := hmac.New(sha256.New, []byte(hardeningHookSecret))
	mac.Write(body)
	h := map[string]string{"X-Hub-Signature": "sha256=" + hex.EncodeToString(mac.Sum(nil))}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func hardeningTrelloHeaders(body []byte) map[string]string {
	mac := hmac.New(sha1.New, []byte(hardeningHookSecret))
	mac.Write(body)
	mac.Write([]byte(hardeningTrelloURL))
	return map[string]string{"X-Trello-Webhook": base64.StdEncoding.EncodeToString(mac.Sum(nil))}
}

type hardeningWebhookResult struct {
	Status     string `json:"status"`
	DeliveryID string `json:"delivery_id"`
	EventID    string `json:"event_id"`
	TaskID     string `json:"task_id"`
	Reason     string `json:"reason"`
}

// newHardeningWebhookApp builds the webhook route (POST and HEAD, as Trello probes with HEAD)
// on top of the shared sqlite schema from setupWebhookTestApp.
func newHardeningWebhookApp(t *testing.T) (*fiber.App, *gorm.DB) {
	t.Helper()
	_, db, _, cipher := setupWebhookTestApp(t)

	svc := service.NewWebhookService(
		repository.NewIntegrationRepository(db),
		repository.NewCredentialRepository(db),
		repository.NewTaskRepository(db),
		integrations.NewMemoryIdempotencyStore(),
		cipher,
		nil,
	)
	handler := api.NewWebhookHandlers(svc)

	app := fiber.New()
	app.Post("/api/v1/webhooks/:provider", handler.HandleWebhook)
	app.Head("/api/v1/webhooks/:provider", handler.HandleWebhook)
	return app, db
}

func hardeningSignGitHub(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// seedHardeningGitHub inserts an enabled GitHub integration whose secret lives in config.
func seedHardeningGitHub(t *testing.T, db *gorm.DB, projectID, integID, repo, secret string) {
	t.Helper()
	if err := db.Create(&models.ProjectIntegration{
		ID:           integID,
		ProjectID:    projectID,
		CredentialID: "cred-" + integID,
		Provider:     models.IntegrationGitHub,
		ExternalID:   repo,
		Enabled:      true,
		Config:       models.JSONMap{"webhook_secret": secret},
	}).Error; err != nil {
		t.Fatalf("seed integration %s: %v", integID, err)
	}
}

func seedHardeningTask(t *testing.T, db *gorm.DB, task models.Task) {
	t.Helper()
	task.Skills = models.StringArray{}
	task.MCPServers = models.StringArray{}
	if task.AssigneeID == "" {
		task.AssigneeID = "agent"
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("seed task %s: %v", task.ID, err)
	}
}

func hardeningTaskCount(t *testing.T, db *gorm.DB, projectID string) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&models.Task{}).Where("project_id = ?", projectID).Count(&n).Error; err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return n
}

func hardeningTaskTitle(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var task models.Task
	if err := db.Where("id = ?", id).First(&task).Error; err != nil {
		t.Fatalf("load task %s: %v", id, err)
	}
	return task.Title
}

func doHardeningRequest(t *testing.T, app *fiber.App, method, path string, body []byte, headers map[string]string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: request failed: %v", method, path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return resp.StatusCode, out
}

func decodeHardeningResult(t *testing.T, raw []byte) hardeningWebhookResult {
	t.Helper()
	var res hardeningWebhookResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("decode response %q: %v", raw, err)
	}
	return res
}

func hardeningIssueBody(repo string, number int, title string) []byte {
	return []byte(fmt.Sprintf(
		`{"action":"opened","repository":{"full_name":%q},"issue":{"number":%d,"title":%q,"body":"details","html_url":"https://github.com/%s/issues/%d"}}`,
		repo, number, title, repo, number))
}

func TestWebhookHTTP_Hardening_UnsupportedProvider(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string // exact JSON from the validation branch; empty when routing itself rejects
	}{
		{name: "unknown provider", path: hardeningWebhookPath + "unknown", wantStatus: http.StatusBadRequest, wantBody: `{"error":"unsupported or missing provider"}`},
		{name: "provider name is case sensitive", path: hardeningWebhookPath + "GitHub", wantStatus: http.StatusBadRequest, wantBody: `{"error":"unsupported or missing provider"}`},
		{name: "unsupported known-looking provider", path: hardeningWebhookPath + "slack", wantStatus: http.StatusBadRequest, wantBody: `{"error":"unsupported or missing provider"}`},
		{name: "missing provider segment does not route", path: hardeningWebhookPath, wantStatus: http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app, db := newHardeningWebhookApp(t)
			seedHardeningGitHub(t, db, "proj-prov", "integ-prov", "owner/prov", "prov-secret")
			body := hardeningIssueBody("owner/prov", 1, "Should not be created")

			status, raw := doHardeningRequest(t, app, http.MethodPost, tc.path, body, map[string]string{
				"X-GitHub-Delivery":   "del-prov-1",
				"X-Hub-Signature-256": hardeningSignGitHub("prov-secret", body),
			})
			if status != tc.wantStatus {
				t.Errorf("expected %d, got %d (body %q)", tc.wantStatus, status, raw)
			}
			if tc.wantBody != "" && strings.TrimSpace(string(raw)) != tc.wantBody {
				t.Errorf("expected body %s, got %q", tc.wantBody, raw)
			}
			if n := hardeningTaskCount(t, db, "proj-prov"); n != 0 {
				t.Errorf("rejected provider created %d tasks", n)
			}
		})
	}
}

func TestWebhookHTTP_Hardening_GitHubRejectionsHaveNoSideEffects(t *testing.T) {
	const secret = "http-gh-secret"
	const repo = "owner/http-gh"
	validBody := hardeningIssueBody(repo, 42, "Attacker issue")
	validSig := hardeningSignGitHub(secret, validBody)

	tests := []struct {
		name       string
		body       []byte
		headers    map[string]string
		wantStatus int
	}{
		{
			name:       "missing signature header",
			body:       validBody,
			headers:    map[string]string{"X-GitHub-Delivery": "d-1"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "signature from wrong secret",
			body:       validBody,
			headers:    map[string]string{"X-GitHub-Delivery": "d-2", "X-Hub-Signature-256": hardeningSignGitHub("wrong", validBody)},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "signature without sha256 prefix",
			body:       validBody,
			headers:    map[string]string{"X-GitHub-Delivery": "d-3", "X-Hub-Signature-256": strings.TrimPrefix(validSig, "sha256=")},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "signature over different body",
			body:       validBody,
			headers:    map[string]string{"X-GitHub-Delivery": "d-4", "X-Hub-Signature-256": hardeningSignGitHub(secret, hardeningIssueBody(repo, 42, "Other"))},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing delivery ID",
			body:       validBody,
			headers:    map[string]string{"X-Hub-Signature-256": validSig},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty body with valid signature",
			body:       []byte{},
			headers:    map[string]string{"X-GitHub-Delivery": "d-5", "X-Hub-Signature-256": hardeningSignGitHub(secret, []byte{})},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "truncated json with valid signature",
			body:       []byte(`{"action":"opened","repository":`),
			headers:    map[string]string{"X-GitHub-Delivery": "d-6", "X-Hub-Signature-256": hardeningSignGitHub(secret, []byte(`{"action":"opened","repository":`))},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "json array with valid signature",
			body:       []byte(`[{"action":"opened"}]`),
			headers:    map[string]string{"X-GitHub-Delivery": "d-7", "X-Hub-Signature-256": hardeningSignGitHub(secret, []byte(`[{"action":"opened"}]`))},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app, db := newHardeningWebhookApp(t)
			seedHardeningGitHub(t, db, "proj-http-gh", "integ-http-gh", repo, secret)
			seedHardeningTask(t, db, models.Task{
				ID: "task-http-gh", ProjectID: "proj-http-gh", ExternalID: hardeningPtr("42"),
				Title: "Keep me", Prompt: "p", Status: models.TaskStatusReview,
			})

			status, raw := doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"github", tc.body, tc.headers)
			if status != tc.wantStatus {
				t.Errorf("expected %d, got %d (body %q)", tc.wantStatus, status, truncateForLog(raw))
			}
			if strings.Contains(string(raw), "signature") {
				t.Errorf("rejection leaks signature detail: %q", truncateForLog(raw))
			}
			if n := hardeningTaskCount(t, db, "proj-http-gh"); n != 1 {
				t.Errorf("rejected request changed task count to %d", n)
			}
			if got := hardeningTaskTitle(t, db, "task-http-gh"); got != "Keep me" {
				t.Errorf("rejected request mutated task title to %q", got)
			}
		})
	}
}

// TestWebhookHTTP_Hardening_OversizedBodyNeverReachesHandler pins that a body above the
// app limit is refused before the webhook handler runs. fasthttp rejects it on the transport
// path, so the test client may see a transport error instead of a status code; either is fine.
func TestWebhookHTTP_Hardening_OversizedBodyNeverReachesHandler(t *testing.T) {
	const secret = "http-big-secret"
	const repo = "owner/http-big"
	app, db := newHardeningWebhookApp(t)
	seedHardeningGitHub(t, db, "proj-http-big", "integ-http-big", repo, secret)
	seedHardeningTask(t, db, models.Task{
		ID: "task-http-big", ProjectID: "proj-http-big", ExternalID: hardeningPtr("42"),
		Title: "Keep big", Prompt: "p", Status: models.TaskStatusReview,
	})

	big := bytes.Repeat([]byte("a"), 5*1024*1024)
	req := httptest.NewRequest(http.MethodPost, hardeningWebhookPath+"github", bytes.NewReader(big))
	req.Header.Set("X-GitHub-Delivery", "d-big-1")
	req.Header.Set("X-Hub-Signature-256", hardeningSignGitHub(secret, big))
	resp, err := app.Test(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("expected 413 for oversized body, got %d", resp.StatusCode)
		}
	} else if !strings.Contains(err.Error(), "body size exceeds") {
		t.Errorf("unexpected transport error for oversized body: %v", err)
	}
	if n := hardeningTaskCount(t, db, "proj-http-big"); n != 1 {
		t.Errorf("oversized body changed task count to %d", n)
	}
	if got := hardeningTaskTitle(t, db, "task-http-big"); got != "Keep big" {
		t.Errorf("oversized body mutated task to %q", got)
	}
}

func TestWebhookHTTP_Hardening_UnknownRepoIsUnauthorized(t *testing.T) {
	const secret = "http-unknown-secret"
	app, db := newHardeningWebhookApp(t)
	seedHardeningGitHub(t, db, "proj-http-unk", "integ-http-unk", "owner/known", secret)

	body := hardeningIssueBody("owner/unknown", 1, "Unknown repo")
	status, _ := doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"github", body, map[string]string{
		"X-GitHub-Delivery":   "d-unk-1",
		"X-Hub-Signature-256": hardeningSignGitHub(secret, body),
	})
	if status != http.StatusUnauthorized {
		t.Errorf("expected 401 for unknown repo, got %d", status)
	}
	if n := hardeningTaskCount(t, db, "proj-http-unk"); n != 0 {
		t.Errorf("unknown repo created %d tasks", n)
	}
}

func TestWebhookHTTP_Hardening_GitHubSuccessAndReplay(t *testing.T) {
	const secret = "http-ok-secret"
	const repo = "owner/http-ok"
	app, db := newHardeningWebhookApp(t)
	seedHardeningGitHub(t, db, "proj-http-ok", "integ-http-ok", repo, secret)
	seedHardeningTask(t, db, models.Task{
		ID: "task-http-ok-existing", ProjectID: "proj-http-ok", ExternalID: hardeningPtr("7"),
		Title: "Existing", Prompt: "p", Status: models.TaskStatusReview,
	})

	body := hardeningIssueBody(repo, 9, "New from webhook")
	headers := map[string]string{
		"X-GitHub-Delivery":   "d-ok-1",
		"X-Hub-Signature-256": hardeningSignGitHub(secret, body),
	}

	status, raw := doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"github", body, headers)
	if status != http.StatusOK {
		t.Fatalf("valid delivery: expected 200, got %d (%q)", status, raw)
	}
	res := decodeHardeningResult(t, raw)
	if res.Status != "processed" || res.DeliveryID != "d-ok-1" || res.TaskID == "" {
		t.Fatalf("unexpected success body: %+v", res)
	}
	if got := hardeningTaskTitle(t, db, res.TaskID); got != "New from webhook" {
		t.Errorf("created task title %q", got)
	}
	if n := hardeningTaskCount(t, db, "proj-http-ok"); n != 2 {
		t.Errorf("expected 2 tasks after creation, got %d", n)
	}

	status, raw = doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"github", body, headers)
	if status != http.StatusOK {
		t.Fatalf("replay: expected 200, got %d", status)
	}
	dup := decodeHardeningResult(t, raw)
	if dup.Status != "duplicate" || dup.TaskID != "" {
		t.Errorf("expected duplicate with no task, got %+v", dup)
	}
	if n := hardeningTaskCount(t, db, "proj-http-ok"); n != 2 {
		t.Errorf("replay changed task count to %d", n)
	}
}

func TestWebhookHTTP_Hardening_GitHubUnhandledEventIsProcessedWithoutTask(t *testing.T) {
	const secret = "http-unhandled-secret"
	const repo = "owner/http-unhandled"
	app, db := newHardeningWebhookApp(t)
	seedHardeningGitHub(t, db, "proj-http-unh", "integ-http-unh", repo, secret)

	body := []byte(`{"action":"synchronize","repository":{"full_name":"owner/http-unhandled"}}`)
	status, raw := doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"github", body, map[string]string{
		"X-GitHub-Delivery":   "d-unh-1",
		"X-Hub-Signature-256": hardeningSignGitHub(secret, body),
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200 for unhandled event, got %d", status)
	}
	if res := decodeHardeningResult(t, raw); res.Status != "processed" || res.TaskID != "" {
		t.Errorf("unexpected result for unhandled event: %+v", res)
	}
	if n := hardeningTaskCount(t, db, "proj-http-unh"); n != 0 {
		t.Errorf("unhandled event created %d tasks", n)
	}
}

func TestWebhookHTTP_Hardening_GitHubCrossProjectIsolation(t *testing.T) {
	const secret = "http-iso-secret"
	app, db := newHardeningWebhookApp(t)
	seedHardeningGitHub(t, db, "proj-http-a", "integ-http-a", "owner/http-a", secret)
	seedHardeningGitHub(t, db, "proj-http-b", "integ-http-b", "owner/http-b", secret)
	seedHardeningTask(t, db, models.Task{
		ID: "task-http-b", ProjectID: "proj-http-b", ExternalID: hardeningPtr("42"),
		Title: "Project B issue", Prompt: "p", Status: models.TaskStatusReview,
	})

	// Project A delivers issue 42. Project B's task with the same external ID must stay untouched.
	body := hardeningIssueBody("owner/http-a", 42, "Project A issue")
	status, raw := doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"github", body, map[string]string{
		"X-GitHub-Delivery":   "d-iso-1",
		"X-Hub-Signature-256": hardeningSignGitHub(secret, body),
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	res := decodeHardeningResult(t, raw)
	if res.TaskID == "" || res.TaskID == "task-http-b" {
		t.Errorf("expected a new project A task, got %+v", res)
	}
	if got := hardeningTaskTitle(t, db, "task-http-b"); got != "Project B issue" {
		t.Errorf("project B task mutated to %q", got)
	}
	if n := hardeningTaskCount(t, db, "proj-http-b"); n != 1 {
		t.Errorf("project B gained tasks: %d", n)
	}
}

func TestWebhookHTTP_Hardening_JiraHandling(t *testing.T) {
	app, db := newHardeningWebhookApp(t)
	if err := db.Create(&models.ProjectIntegration{
		ID: "integ-http-jira", ProjectID: "proj-http-jira", CredentialID: "cred-jira",
		Provider: models.IntegrationJira, ExternalID: "HJ", Enabled: true, Config: hardeningHookConfig(),
	}).Error; err != nil {
		t.Fatalf("seed jira integration: %v", err)
	}

	valid := []byte(`{"webhookEvent":"jira:issue_created","issue":{"key":"HJ-1","fields":{"project":{"key":"HJ"}}}}`)
	unknown := []byte(`{"webhookEvent":"jira:issue_created","issue":{"fields":{"project":{"key":"ZZ"}}}}`)

	t.Run("valid event processed and echoes header", func(t *testing.T) {
		status, raw := doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"jira", valid,
			hardeningJiraHeaders(valid, map[string]string{"X-Atlassian-Webhook-Identifier": "jira-h-1"}))
		res := decodeHardeningResult(t, raw)
		if status != http.StatusOK || res.Status != "processed" || res.EventID != "jira-h-1" {
			t.Errorf("expected 200 processed jira-h-1, got %d %+v", status, res)
		}
	})

	t.Run("replayed identifier is duplicate", func(t *testing.T) {
		status, raw := doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"jira", valid,
			hardeningJiraHeaders(valid, map[string]string{"X-Atlassian-Webhook-Identifier": "jira-h-1"}))
		if res := decodeHardeningResult(t, raw); status != http.StatusOK || res.Status != "duplicate" {
			t.Errorf("expected 200 duplicate, got %d %+v", status, res)
		}
	})

	t.Run("unknown project key is ignored with 200", func(t *testing.T) {
		status, raw := doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"jira", unknown, hardeningJiraHeaders(unknown, nil))
		if res := decodeHardeningResult(t, raw); status != http.StatusOK || res.Status != "ignored" {
			t.Errorf("expected 200 ignored, got %d %+v", status, res)
		}
	})

	t.Run("malformed json is 400", func(t *testing.T) {
		status, _ := doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"jira", []byte(`{"issue":`), nil)
		if status != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", status)
		}
	})

	t.Run("empty body is 400", func(t *testing.T) {
		status, _ := doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"jira", []byte{}, nil)
		if status != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", status)
		}
	})

	t.Run("missing identifier still answers 200 with generated event ID", func(t *testing.T) {
		status, raw := doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"jira", valid, hardeningJiraHeaders(valid, nil))
		res := decodeHardeningResult(t, raw)
		if status != http.StatusOK || res.Status != "processed" || res.EventID == "" {
			t.Errorf("expected 200 processed with generated ID, got %d %+v", status, res)
		}
	})

	t.Run("missing or wrong signature is 401", func(t *testing.T) {
		status, _ := doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"jira", valid, nil)
		if status != http.StatusUnauthorized {
			t.Errorf("unsigned: expected 401, got %d", status)
		}
		status, _ = doHardeningRequest(t, app, http.MethodPost, hardeningWebhookPath+"jira", valid,
			map[string]string{"X-Hub-Signature": "sha256=" + strings.Repeat("0", 64)})
		if status != http.StatusUnauthorized {
			t.Errorf("bad signature: expected 401, got %d", status)
		}
	})

	if n := hardeningTaskCount(t, db, "proj-http-jira"); n != 0 {
		t.Errorf("jira webhook created %d tasks", n)
	}
}

func TestWebhookHTTP_Hardening_Trello(t *testing.T) {
	app, db := newHardeningWebhookApp(t)
	if err := db.Create(&models.ProjectIntegration{
		ID: "integ-http-trello", ProjectID: "proj-http-trello", CredentialID: "cred-trello",
		Provider: models.IntegrationTrello, ExternalID: "board-http", Enabled: true, Config: hardeningHookConfig(),
	}).Error; err != nil {
		t.Fatalf("seed trello integration: %v", err)
	}
	const path = hardeningWebhookPath + "trello"
	valid := []byte(`{"model":{"idBoard":"board-http"},"action":{"id":"act-http-1","type":"updateCard"}}`)

	t.Run("HEAD probe returns 200 regardless of body", func(t *testing.T) {
		status, _ := doHardeningRequest(t, app, http.MethodHead, path, []byte("not json"), nil)
		if status != http.StatusOK {
			t.Errorf("expected 200 for HEAD probe, got %d", status)
		}
	})

	t.Run("malformed json is 400", func(t *testing.T) {
		status, _ := doHardeningRequest(t, app, http.MethodPost, path, []byte(`{"action":`), nil)
		if status != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", status)
		}
	})

	t.Run("empty body is 400", func(t *testing.T) {
		status, _ := doHardeningRequest(t, app, http.MethodPost, path, []byte{}, nil)
		if status != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", status)
		}
	})

	t.Run("unknown board is ignored with 200", func(t *testing.T) {
		body := []byte(`{"model":{"idBoard":"board-none"},"action":{"id":"act-none"}}`)
		status, raw := doHardeningRequest(t, app, http.MethodPost, path, body, hardeningTrelloHeaders(body))
		if res := decodeHardeningResult(t, raw); status != http.StatusOK || res.Status != "ignored" {
			t.Errorf("expected 200 ignored, got %d %+v", status, res)
		}
	})

	t.Run("valid event processed then replay duplicate", func(t *testing.T) {
		status, raw := doHardeningRequest(t, app, http.MethodPost, path, valid, hardeningTrelloHeaders(valid))
		if res := decodeHardeningResult(t, raw); status != http.StatusOK || res.Status != "processed" || res.EventID != "act-http-1" {
			t.Fatalf("expected 200 processed, got %d %+v", status, res)
		}
		status, raw = doHardeningRequest(t, app, http.MethodPost, path, valid, hardeningTrelloHeaders(valid))
		if res := decodeHardeningResult(t, raw); status != http.StatusOK || res.Status != "duplicate" {
			t.Errorf("expected 200 duplicate on replay, got %d %+v", status, res)
		}
	})

	t.Run("missing or wrong signature is 401", func(t *testing.T) {
		status, _ := doHardeningRequest(t, app, http.MethodPost, path, valid, nil)
		if status != http.StatusUnauthorized {
			t.Errorf("unsigned: expected 401, got %d", status)
		}
		status, _ = doHardeningRequest(t, app, http.MethodPost, path, valid, map[string]string{"X-Trello-Webhook": "AAAA"})
		if status != http.StatusUnauthorized {
			t.Errorf("bad signature: expected 401, got %d", status)
		}
	})

	if n := hardeningTaskCount(t, db, "proj-http-trello"); n != 0 {
		t.Errorf("trello webhook created %d tasks", n)
	}
}

func hardeningPtr(s string) *string { return &s }

func truncateForLog(b []byte) string {
	const max = 200
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}
