package integrations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"agent-task-manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// StaleReceivedAfter is how long an event may sit in "received" before a redelivery reclaims it. Webhook
// processing is short, so an event this old belongs to a process that died before recording an outcome.
const StaleReceivedAfter = 10 * time.Minute

// IdempotencyStore tracks and deduplicates incoming webhook events.
type IdempotencyStore interface {
	RecordEvent(ctx context.Context, provider string, projectID string, providerEventID string) (bool, error)
	MarkProcessed(ctx context.Context, provider string, projectID string, providerEventID string) error
	MarkFailed(ctx context.Context, provider string, projectID string, providerEventID string, errMsg string) error
}

type memoryEventKey struct {
	provider        string
	projectID       string
	providerEventID string
}

type memoryEventRecord struct {
	status       models.WebhookStatus
	errorMessage *string
	receivedAt   time.Time
	processedAt  *time.Time
}

// MemoryIdempotencyStore provides an in-memory implementation for tests using common.SafeMap.
type MemoryIdempotencyStore struct {
	mu     sync.Mutex
	events map[memoryEventKey]*memoryEventRecord
	now    func() time.Time
}

// NewMemoryIdempotencyStore creates an in-memory IdempotencyStore.
func NewMemoryIdempotencyStore() IdempotencyStore {
	return NewMemoryIdempotencyStoreWithClock(time.Now)
}

// NewMemoryIdempotencyStoreWithClock is NewMemoryIdempotencyStore with an injectable clock.
func NewMemoryIdempotencyStoreWithClock(now func() time.Time) IdempotencyStore {
	return &MemoryIdempotencyStore{events: make(map[memoryEventKey]*memoryEventRecord), now: now}
}

func (m *MemoryIdempotencyStore) RecordEvent(ctx context.Context, provider string, projectID string, providerEventID string) (bool, error) {
	key := memoryEventKey{
		provider:        provider,
		projectID:       projectID,
		providerEventID: providerEventID,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.events == nil {
		m.events = make(map[memoryEventKey]*memoryEventRecord) // zero value stays usable
	}
	clock := m.now
	if clock == nil {
		clock = time.Now
	}
	now := clock().UTC()
	if existing, ok := m.events[key]; ok {
		// A failed event, or one stuck in received past StaleReceivedAfter, is reclaimed by the
		// redelivery so the provider's retry is processed.
		stale := existing.status == models.WebhookReceived && now.Sub(existing.receivedAt) >= StaleReceivedAfter
		if existing.status != models.WebhookFailed && !stale {
			return false, nil
		}
		existing.status = models.WebhookReceived
		existing.errorMessage = nil
		existing.processedAt = nil
		existing.receivedAt = now
		return true, nil
	}
	m.events[key] = &memoryEventRecord{status: models.WebhookReceived, receivedAt: now}
	return true, nil
}

func (m *MemoryIdempotencyStore) MarkProcessed(ctx context.Context, provider string, projectID string, providerEventID string) error {
	key := memoryEventKey{
		provider:        provider,
		projectID:       projectID,
		providerEventID: providerEventID,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	rec, exists := m.events[key]
	if !exists {
		return fmt.Errorf("webhook event not found: %s/%s/%s", provider, projectID, providerEventID)
	}
	now := time.Now().UTC()
	rec.status = models.WebhookProcessed
	rec.processedAt = &now
	return nil
}

func (m *MemoryIdempotencyStore) MarkFailed(ctx context.Context, provider string, projectID string, providerEventID string, errMsg string) error {
	key := memoryEventKey{
		provider:        provider,
		projectID:       projectID,
		providerEventID: providerEventID,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	rec, exists := m.events[key]
	if !exists {
		return fmt.Errorf("webhook event not found: %s/%s/%s", provider, projectID, providerEventID)
	}
	now := time.Now().UTC()
	rec.status = models.WebhookFailed
	rec.processedAt = &now
	rec.errorMessage = &errMsg
	return nil
}

// DBIdempotencyStore implements IdempotencyStore using GORM and models.WebhookEvent.
type DBIdempotencyStore struct {
	db  *gorm.DB
	now func() time.Time
}

// NewDBIdempotencyStore creates a new DB-backed IdempotencyStore.
func NewDBIdempotencyStore(db *gorm.DB) IdempotencyStore {
	return NewDBIdempotencyStoreWithClock(db, time.Now)
}

// NewDBIdempotencyStoreWithClock is NewDBIdempotencyStore with an injectable clock.
func NewDBIdempotencyStoreWithClock(db *gorm.DB, now func() time.Time) IdempotencyStore {
	return &DBIdempotencyStore{db: db, now: now}
}

func (d *DBIdempotencyStore) RecordEvent(ctx context.Context, provider string, projectID string, providerEventID string) (bool, error) {
	if d.db == nil {
		return false, errors.New("db connection is nil")
	}

	event := models.WebhookEvent{
		Provider:        provider,
		ProjectID:       projectID,
		ProviderEventID: providerEventID,
		Status:          models.WebhookReceived,
		ReceivedAt:      d.now().UTC(),
	}

	// Clauses OnConflict DoNothing allows detecting if a new row was inserted or ignored due to PK conflict
	res := d.db.WithContext(ctx).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(&event)

	if res.Error != nil {
		// If driver doesn't support OnConflict DoNothing or fails with duplicate error
		if isUniqueConstraintViolation(res.Error) {
			return d.reclaimFailed(ctx, provider, projectID, providerEventID)
		}
		return false, res.Error
	}

	// RowsAffected == 1 means it was inserted (first seen).
	// RowsAffected == 0 means it collided with existing PK and was skipped (duplicate).
	if res.RowsAffected == 0 {
		return d.reclaimFailed(ctx, provider, projectID, providerEventID)
	}

	return true, nil
}

// reclaimFailed atomically flips a failed event, or one stuck in received past StaleReceivedAfter, back
// to received. Only one concurrent redelivery sees RowsAffected == 1, so the event is retried exactly once.
func (d *DBIdempotencyStore) reclaimFailed(ctx context.Context, provider, projectID, providerEventID string) (bool, error) {
	now := d.now().UTC()
	res := d.db.WithContext(ctx).Model(&models.WebhookEvent{}).
		Where("provider = ? AND project_id = ? AND provider_event_id = ?", provider, projectID, providerEventID).
		Where("status = ? OR (status = ? AND received_at <= ?)", models.WebhookFailed, models.WebhookReceived, now.Add(-StaleReceivedAfter)).
		Updates(map[string]any{
			"status":        models.WebhookReceived,
			"received_at":   now,
			"processed_at":  nil,
			"error_message": nil,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (d *DBIdempotencyStore) MarkProcessed(ctx context.Context, provider string, projectID string, providerEventID string) error {
	if d.db == nil {
		return errors.New("db connection is nil")
	}

	now := time.Now().UTC()
	res := d.db.WithContext(ctx).Model(&models.WebhookEvent{}).
		Where("provider = ? AND project_id = ? AND provider_event_id = ?", provider, projectID, providerEventID).
		Updates(map[string]any{
			"status":       models.WebhookProcessed,
			"processed_at": now,
		})

	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("webhook event not found: %s/%s/%s", provider, projectID, providerEventID)
	}
	return nil
}

func (d *DBIdempotencyStore) MarkFailed(ctx context.Context, provider string, projectID string, providerEventID string, errMsg string) error {
	if d.db == nil {
		return errors.New("db connection is nil")
	}

	now := time.Now().UTC()
	res := d.db.WithContext(ctx).Model(&models.WebhookEvent{}).
		Where("provider = ? AND project_id = ? AND provider_event_id = ?", provider, projectID, providerEventID).
		Updates(map[string]any{
			"status":        models.WebhookFailed,
			"processed_at":  now,
			"error_message": errMsg,
		})

	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("webhook event not found: %s/%s/%s", provider, projectID, providerEventID)
	}
	return nil
}

func isUniqueConstraintViolation(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "duplicate key") ||
		strings.Contains(s, "unique constraint") ||
		strings.Contains(s, "primary key constraint") ||
		strings.Contains(s, "unique violation")
}
