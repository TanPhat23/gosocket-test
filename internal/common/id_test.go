package common_test

import (
	"testing"
	"time"
	"uuid"

	"agent-task-manager/internal/common"
)

func TestUUIDv7GenerationAndValidation(t *testing.T) {
	id1 := common.NewID()
	if !common.IsValidUUID(id1) {
		t.Fatalf("expected valid UUID, got: %s", id1)
	}

	parsed, err := common.ParseUUID(id1)
	if err != nil {
		t.Fatalf("failed to parse generated UUID: %v", err)
	}

	if parsed.String() != id1 {
		t.Errorf("parsed string mismatch: got %s, want %s", parsed.String(), id1)
	}

	time.Sleep(2 * time.Millisecond)
	id2 := common.NewID()
	if !common.IsValidUUID(id2) {
		t.Fatalf("expected valid UUID, got: %s", id2)
	}

	// In UUIDv7, lexicographical order matches chronological order
	if id1 >= id2 {
		t.Errorf("expected time-ordered monotonicity: %s should be < %s", id1, id2)
	}

	// Verify standard library uuid type
	var u uuid.UUID = parsed
	if u.String() != id1 {
		t.Errorf("expected standard library uuid compatibility")
	}

	// Test invalid UUIDs
	if common.IsValidUUID("not-a-uuid") {
		t.Errorf("expected invalid UUID to return false")
	}
	if common.IsValidUUID("") {
		t.Errorf("expected empty string to return false")
	}
}
