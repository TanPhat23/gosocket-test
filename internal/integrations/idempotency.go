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

// MemoryIdempotencyStore provides an in-memory implementation for tests using sync.Map.
type MemoryIdempotencyStore struct {
	events sync.Map // map[memoryEventKey]*memoryEventRecord
}

// NewMemoryIdempotencyStore creates an in-memory IdempotencyStore.
func NewMemoryIdempotencyStore() IdempotencyStore {
	return &MemoryIdempotencyStore{}
}

func (m *MemoryIdempotencyStore) RecordEvent(ctx context.Context, provider string, projectID string, providerEventID string) (bool, error) {
	key := memoryEventKey{
		provider:        provider,
		projectID:       projectID,
		providerEventID: providerEventID,
	}

	record := &memoryEventRecord{
		status:     models.WebhookReceived,
		receivedAt: time.Now().UTC(),
	}

	_, loaded := m.events.LoadOrStore(key, record)
	return !loaded, nil
}

func (m *MemoryIdempotencyStore) MarkProcessed(ctx context.Context, provider string, projectID string, providerEventID string) error {
	key := memoryEventKey{
		provider:        provider,
		projectID:       projectID,
		providerEventID: providerEventID,
	}

	val, exists := m.events.Load(key)
	if !exists {
		return fmt.Errorf("webhook event not found: %s/%s/%s", provider, projectID, providerEventID)
	}

	rec := val.(*memoryEventRecord)
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

	val, exists := m.events.Load(key)
	if !exists {
		return fmt.Errorf("webhook event not found: %s/%s/%s", provider, projectID, providerEventID)
	}

	rec := val.(*memoryEventRecord)
	now := time.Now().UTC()
	rec.status = models.WebhookFailed
	rec.processedAt = &now
	rec.errorMessage = &errMsg
	return nil
}

// DBIdempotencyStore implements IdempotencyStore using GORM and models.WebhookEvent.
type DBIdempotencyStore struct {
	db *gorm.DB
}

// NewDBIdempotencyStore creates a new DB-backed IdempotencyStore.
func NewDBIdempotencyStore(db *gorm.DB) IdempotencyStore {
	return &DBIdempotencyStore{db: db}
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
		ReceivedAt:      time.Now().UTC(),
	}

	// Clauses OnConflict DoNothing allows detecting if a new row was inserted or ignored due to PK conflict
	res := d.db.WithContext(ctx).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(&event)

	if res.Error != nil {
		// If driver doesn't support OnConflict DoNothing or fails with duplicate error
		if isUniqueConstraintViolation(res.Error) {
			return false, nil
		}
		return false, res.Error
	}

	// RowsAffected == 1 means it was inserted (first seen).
	// RowsAffected == 0 means it collided with existing PK and was skipped (duplicate).
	if res.RowsAffected == 0 {
		return false, nil
	}

	return true, nil
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
