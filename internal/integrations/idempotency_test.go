package integrations_test

import (
	"context"
	"testing"
	"time"

	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// mockSyncProvider implements integrations.SyncProvider for tests.
type mockSyncProvider struct {
	provider models.IntegrationProvider
}

func (m *mockSyncProvider) Provider() models.IntegrationProvider {
	return m.provider
}

func (m *mockSyncProvider) FetchIssues(ctx context.Context, integration *models.ProjectIntegration, plainSecret string) ([]integrations.ExternalIssue, error) {
	return []integrations.ExternalIssue{
		{
			ID:          "ext-1",
			Title:       "Test Issue",
			Description: "Issue desc",
			Status:      "open",
			URL:         "https://example.com/1",
			Labels:      []string{"bug"},
		},
	}, nil
}

func (m *mockSyncProvider) UpdateIssueStatus(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, externalID string, status string) error {
	return nil
}

func (m *mockSyncProvider) AddComment(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, externalID string, comment string) error {
	return nil
}

// mockPRProvider implements integrations.PullRequestProvider
type mockPRProvider struct {
	mockSyncProvider
}

func (m *mockPRProvider) CreatePullRequest(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, branch, title, body string) (string, error) {
	return "https://example.com/pr/1", nil
}

func (m *mockPRProvider) GetPullRequestStatus(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, prNumberOrURL string) (*integrations.PullRequestStatus, error) {
	return &integrations.PullRequestStatus{
		Number:  1,
		State:   "closed",
		Merged:  true,
		HTMLURL: "https://example.com/pr/1",
	}, nil
}

func TestEventDeduplication(t *testing.T) {
	store := integrations.NewMemoryIdempotencyStore()
	ctx := context.Background()

	firstSeen, err := store.RecordEvent(ctx, "github", "proj-1", "evt-12345")
	if err != nil || !firstSeen {
		t.Fatalf("expected first event to be recorded, got seen=%v, err=%v", firstSeen, err)
	}

	secondSeen, err := store.RecordEvent(ctx, "github", "proj-1", "evt-12345")
	if err != nil || secondSeen {
		t.Fatalf("expected duplicate event to return seen=false, got seen=%v", secondSeen)
	}
}

func TestMemoryIdempotencyStore_Lifecycle(t *testing.T) {
	store := integrations.NewMemoryIdempotencyStore()
	ctx := context.Background()

	// 1. MarkProcessed non-existent event should fail
	if err := store.MarkProcessed(ctx, "github", "proj-1", "non-existent"); err == nil {
		t.Errorf("expected error marking non-existent event processed, got nil")
	}

	// 2. MarkFailed non-existent event should fail
	if err := store.MarkFailed(ctx, "github", "proj-1", "non-existent", "error"); err == nil {
		t.Errorf("expected error marking non-existent event failed, got nil")
	}

	// 3. Record event
	seen, err := store.RecordEvent(ctx, "github", "proj-1", "evt-lifecycle-1")
	if err != nil || !seen {
		t.Fatalf("expected recorded=true, err=nil; got seen=%v, err=%v", seen, err)
	}

	// 4. MarkProcessed
	if err := store.MarkProcessed(ctx, "github", "proj-1", "evt-lifecycle-1"); err != nil {
		t.Fatalf("MarkProcessed failed: %v", err)
	}

	// 5. Record another event and MarkFailed
	seen2, err := store.RecordEvent(ctx, "jira", "proj-1", "evt-lifecycle-2")
	if err != nil || !seen2 {
		t.Fatalf("expected recorded=true, err=nil; got seen=%v, err=%v", seen2, err)
	}

	if err := store.MarkFailed(ctx, "jira", "proj-1", "evt-lifecycle-2", "network timeout"); err != nil {
		t.Fatalf("MarkFailed failed: %v", err)
	}
}

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to connect sqlite: %v", err)
	}

	err = db.Exec(`CREATE TABLE webhook_events (
		provider TEXT NOT NULL,
		project_id TEXT NOT NULL,
		provider_event_id TEXT NOT NULL,
		status TEXT NOT NULL,
		received_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		processed_at DATETIME,
		error_message TEXT,
		PRIMARY KEY (provider, project_id, provider_event_id)
	)`).Error
	if err != nil {
		t.Fatalf("failed to create webhook_events table: %v", err)
	}

	return db
}

func TestDBIdempotencyStore(t *testing.T) {
	db := setupTestDB(t)
	store := integrations.NewDBIdempotencyStore(db)
	ctx := context.Background()

	t.Run("nil db error handling", func(t *testing.T) {
		nilStore := integrations.NewDBIdempotencyStore(nil)
		_, err := nilStore.RecordEvent(ctx, "github", "proj-db-1", "evt-1")
		if err == nil {
			t.Errorf("expected error with nil db on RecordEvent")
		}
		if err := nilStore.MarkProcessed(ctx, "github", "proj-db-1", "evt-1"); err == nil {
			t.Errorf("expected error with nil db on MarkProcessed")
		}
		if err := nilStore.MarkFailed(ctx, "github", "proj-db-1", "evt-1", "err"); err == nil {
			t.Errorf("expected error with nil db on MarkFailed")
		}
	})

	t.Run("record event and duplicate detection", func(t *testing.T) {
		firstSeen, err := store.RecordEvent(ctx, "github", "proj-db-1", "evt-100")
		if err != nil || !firstSeen {
			t.Fatalf("expected first event to be recorded, got seen=%v, err=%v", firstSeen, err)
		}

		secondSeen, err := store.RecordEvent(ctx, "github", "proj-db-1", "evt-100")
		if err != nil || secondSeen {
			t.Fatalf("expected duplicate event to return seen=false, got seen=%v, err=%v", secondSeen, err)
		}
	})

	t.Run("mark processed", func(t *testing.T) {
		firstSeen, err := store.RecordEvent(ctx, "github", "proj-db-1", "evt-200")
		if err != nil || !firstSeen {
			t.Fatalf("failed to record evt-200: %v", err)
		}

		if err := store.MarkProcessed(ctx, "github", "proj-db-1", "evt-200"); err != nil {
			t.Fatalf("MarkProcessed failed: %v", err)
		}

		var event models.WebhookEvent
		if err := db.Where("provider = ? AND project_id = ? AND provider_event_id = ?", "github", "proj-db-1", "evt-200").First(&event).Error; err != nil {
			t.Fatalf("failed to query webhook event: %v", err)
		}

		if event.Status != models.WebhookProcessed {
			t.Errorf("expected status %s, got %s", models.WebhookProcessed, event.Status)
		}
		if event.ProcessedAt == nil {
			t.Errorf("expected ProcessedAt to be set, got nil")
		}
	})

	t.Run("mark failed", func(t *testing.T) {
		firstSeen, err := store.RecordEvent(ctx, "jira", "proj-db-1", "evt-300")
		if err != nil || !firstSeen {
			t.Fatalf("failed to record evt-300: %v", err)
		}

		expectedErr := "syntax error in webhook json payload"
		if err := store.MarkFailed(ctx, "jira", "proj-db-1", "evt-300", expectedErr); err != nil {
			t.Fatalf("MarkFailed failed: %v", err)
		}

		var event models.WebhookEvent
		if err := db.Where("provider = ? AND project_id = ? AND provider_event_id = ?", "jira", "proj-db-1", "evt-300").First(&event).Error; err != nil {
			t.Fatalf("failed to query webhook event: %v", err)
		}

		if event.Status != models.WebhookFailed {
			t.Errorf("expected status %s, got %s", models.WebhookFailed, event.Status)
		}
		if event.ProcessedAt == nil {
			t.Errorf("expected ProcessedAt to be set, got nil")
		}
		if event.ErrorMessage == nil || *event.ErrorMessage != expectedErr {
			t.Errorf("expected ErrorMessage=%q, got %v", expectedErr, event.ErrorMessage)
		}
	})

	t.Run("mark non-existent returns error", func(t *testing.T) {
		if err := store.MarkProcessed(ctx, "trello", "proj-db-1", "nonexistent-evt"); err == nil {
			t.Errorf("expected error marking non-existent event processed")
		}
		if err := store.MarkFailed(ctx, "trello", "proj-db-1", "nonexistent-evt", "err"); err == nil {
			t.Errorf("expected error marking non-existent event failed")
		}
	})
}

func TestRegistry(t *testing.T) {
	reg := integrations.NewRegistry()

	// 1. Register nil provider
	if err := reg.Register(nil); err == nil {
		t.Errorf("expected error registering nil provider")
	}

	// 2. Register GitHub provider
	ghProvider := &mockSyncProvider{provider: models.IntegrationGitHub}
	if err := reg.Register(ghProvider); err != nil {
		t.Fatalf("unexpected error registering provider: %v", err)
	}

	// 3. Register duplicate provider
	if err := reg.Register(ghProvider); err == nil {
		t.Errorf("expected error registering duplicate provider, got nil")
	}

	// 4. Get registered provider
	found, err := reg.Get(models.IntegrationGitHub)
	if err != nil {
		t.Fatalf("failed to get registered provider: %v", err)
	}
	if found.Provider() != models.IntegrationGitHub {
		t.Errorf("expected provider %s, got %s", models.IntegrationGitHub, found.Provider())
	}

	// 5. Get unregistered provider
	if _, err := reg.Get(models.IntegrationJira); err == nil {
		t.Errorf("expected error for unregistered provider Jira, got nil")
	}

	// 6. List providers
	jiraProvider := &mockSyncProvider{provider: models.IntegrationJira}
	if err := reg.Register(jiraProvider); err != nil {
		t.Fatalf("unexpected error registering Jira provider: %v", err)
	}

	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 providers in list, got %d", len(list))
	}
}

func TestPullRequestProvider(t *testing.T) {
	prProvider := &mockPRProvider{
		mockSyncProvider: mockSyncProvider{provider: models.IntegrationGitHub},
	}

	var _ integrations.PullRequestProvider = prProvider

	url, err := prProvider.CreatePullRequest(context.Background(), &models.ProjectIntegration{}, "secret", "feature-branch", "PR title", "PR body")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://example.com/pr/1" {
		t.Errorf("unexpected pr url: %s", url)
	}
}

// A delivery whose processing failed was acknowledged with an error, so the provider will redeliver it
// under the same id. That redelivery must be processed again, exactly once, and never after success.
func TestIdempotencyStores_RetryFailedEvent(t *testing.T) {
	stores := map[string]func(t *testing.T) integrations.IdempotencyStore{
		"memory": func(t *testing.T) integrations.IdempotencyStore { return integrations.NewMemoryIdempotencyStore() },
		"db":     func(t *testing.T) integrations.IdempotencyStore { return integrations.NewDBIdempotencyStore(setupTestDB(t)) },
	}
	for name, mk := range stores {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store := mk(t)

			if first, err := store.RecordEvent(ctx, "github", "p", "evt"); err != nil || !first {
				t.Fatalf("first: %v %v", first, err)
			}
			// A received (in-flight) event is not retryable: that would double-process concurrent deliveries.
			if again, _ := store.RecordEvent(ctx, "github", "p", "evt"); again {
				t.Fatal("in-flight event must stay a duplicate")
			}
			if err := store.MarkFailed(ctx, "github", "p", "evt", "boom"); err != nil {
				t.Fatal(err)
			}
			// The failed event is reclaimed by exactly one redelivery.
			if retry, err := store.RecordEvent(ctx, "github", "p", "evt"); err != nil || !retry {
				t.Fatalf("failed event must be retryable, got %v %v", retry, err)
			}
			if second, _ := store.RecordEvent(ctx, "github", "p", "evt"); second {
				t.Fatal("only one redelivery may reclaim a failed event")
			}
			// Another provider or project with the same id is independent.
			if other, _ := store.RecordEvent(ctx, "jira", "p", "evt"); !other {
				t.Fatal("different provider must be independent")
			}
			// Once processed, redeliveries are duplicates again, and a late failure mark does not reopen them.
			if err := store.MarkProcessed(ctx, "github", "p", "evt"); err != nil {
				t.Fatal(err)
			}
			if again, _ := store.RecordEvent(ctx, "github", "p", "evt"); again {
				t.Fatal("processed event must stay a duplicate")
			}
		})
	}
}

func TestDBIdempotencyStore_RetryClearsFailureState(t *testing.T) {
	db := setupTestDB(t)
	store := integrations.NewDBIdempotencyStore(db)
	ctx := context.Background()

	_, _ = store.RecordEvent(ctx, "github", "p", "evt")
	_ = store.MarkFailed(ctx, "github", "p", "evt", "boom")
	if retry, err := store.RecordEvent(ctx, "github", "p", "evt"); err != nil || !retry {
		t.Fatalf("retry: %v %v", retry, err)
	}
	var ev models.WebhookEvent
	if err := db.Where("provider_event_id = ?", "evt").First(&ev).Error; err != nil {
		t.Fatal(err)
	}
	if ev.Status != models.WebhookReceived || ev.ErrorMessage != nil || ev.ProcessedAt != nil {
		t.Errorf("reclaimed event must be back in received state without stale failure data: %+v", ev)
	}
}

// A delivery stuck in "received" (the process died before MarkProcessed/MarkFailed) must not block the
// provider's retries forever: once older than StaleReceivedAfter, exactly one redelivery reclaims it.
func TestIdempotencyStores_ReclaimsStaleReceivedEvent(t *testing.T) {
	var now time.Time
	clock := func() time.Time { return now }
	stores := map[string]func(t *testing.T) integrations.IdempotencyStore{
		"memory": func(t *testing.T) integrations.IdempotencyStore {
			return integrations.NewMemoryIdempotencyStoreWithClock(clock)
		},
		"db": func(t *testing.T) integrations.IdempotencyStore {
			return integrations.NewDBIdempotencyStoreWithClock(setupTestDB(t), clock)
		},
	}
	for name, mk := range stores {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
			store := mk(t)

			if first, err := store.RecordEvent(ctx, "github", "p", "evt"); err != nil || !first {
				t.Fatalf("first: %v %v", first, err)
			}
			now = now.Add(integrations.StaleReceivedAfter - time.Second)
			if again, _ := store.RecordEvent(ctx, "github", "p", "evt"); again {
				t.Fatal("a fresh in-flight event must stay a duplicate")
			}
			now = now.Add(2 * time.Second)
			if retry, err := store.RecordEvent(ctx, "github", "p", "evt"); err != nil || !retry {
				t.Fatalf("stale received event must be reclaimable, got %v %v", retry, err)
			}
			if second, _ := store.RecordEvent(ctx, "github", "p", "evt"); second {
				t.Fatal("reclaiming must restart the clock: only one redelivery wins")
			}
			// A stale processed event is never reopened.
			if err := store.MarkProcessed(ctx, "github", "p", "evt"); err != nil {
				t.Fatal(err)
			}
			now = now.Add(24 * time.Hour)
			if again, _ := store.RecordEvent(ctx, "github", "p", "evt"); again {
				t.Fatal("processed events stay duplicates forever")
			}
		})
	}
}
