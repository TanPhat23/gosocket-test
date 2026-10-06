package common_test

import (
	"testing"

	"agent-task-manager/internal/common"
)

type sampleStruct struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestGenericJSONEncoder(t *testing.T) {
	sample := sampleStruct{Name: "test-task", Count: 42}

	bytes, err := common.Marshal(sample)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	decoded, err := common.Unmarshal[sampleStruct](bytes)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Name != sample.Name || decoded.Count != sample.Count {
		t.Fatalf("mismatched decoded struct: got %+v, want %+v", decoded, sample)
	}

	// Test unmarshal error on invalid JSON
	_, err = common.Unmarshal[sampleStruct]([]byte(`{invalid-json`))
	if err == nil {
		t.Fatalf("expected error unmarshaling invalid JSON, got nil")
	}
}
