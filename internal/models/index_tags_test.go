package models_test

import (
	"sync"
	"testing"

	"agent-task-manager/internal/models"

	"gorm.io/gorm/schema"
)

// parsedIndexes returns the GORM index definitions of model v, keyed by index name, without opening a database.
func parsedIndexes(t *testing.T, v any) map[string][]string {
	t.Helper()
	s, err := schema.Parse(v, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	out := map[string][]string{}
	for _, idx := range s.ParseIndexes() {
		fields := make([]string, 0, len(idx.Fields))
		for _, f := range idx.Fields {
			fields = append(fields, f.DBName)
		}
		out[idx.Name] = fields
	}
	return out
}

// TestWebhookProviderLookupIsIndexed fails until project_integrations has an index that leads with provider,
// the columns of GetActiveByProviderAndExternalID. The existing (project_id, provider, external_id) index cannot serve it.
func TestWebhookProviderLookupIsIndexed(t *testing.T) {
	idx := parsedIndexes(t, &models.ProjectIntegration{})
	got, ok := idx["idx_project_integrations_provider_external"]
	if !ok {
		t.Fatalf("missing idx_project_integrations_provider_external; indexes = %v", idx)
	}
	if len(got) != 2 || got[0] != "provider" || got[1] != "external_id" {
		t.Errorf("index columns = %v, want [provider external_id]", got)
	}
}

// TestTaskListingIsIndexedByProjectAndCreatedAt fails until tasks has an index on (project_id, created_at),
// the access path of the ordered per-project task listings.
func TestTaskListingIsIndexedByProjectAndCreatedAt(t *testing.T) {
	idx := parsedIndexes(t, &models.Task{})
	got, ok := idx["idx_tasks_project_created"]
	if !ok {
		t.Fatalf("missing idx_tasks_project_created; indexes = %v", idx)
	}
	if len(got) != 2 || got[0] != "project_id" || got[1] != "created_at" {
		t.Errorf("index columns = %v, want [project_id created_at]", got)
	}
}
