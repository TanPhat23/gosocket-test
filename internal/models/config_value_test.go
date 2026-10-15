package models_test

import (
	"testing"

	"agent-task-manager/internal/models"
)

func TestConfigValue(t *testing.T) {
	m := models.JSONMap{"s": "x", "empty": "", "b": true, "n": nil, "i": 3, "lists": map[string]any{"a": "b"}}

	if v, ok := models.ConfigValue[string](m, "s"); !ok || v != "x" {
		t.Fatalf("string present: %q %v", v, ok)
	}
	if v, ok := models.ConfigValue[string](m, "empty"); !ok || v != "" {
		t.Fatalf("empty string must be ok=true: %q %v", v, ok)
	}
	if v, ok := models.ConfigValue[bool](m, "b"); !ok || !v {
		t.Fatalf("bool present: %v %v", v, ok)
	}
	if v, ok := models.ConfigValue[string](m, "missing"); ok || v != "" {
		t.Fatalf("absent: %q %v", v, ok)
	}
	if v, ok := models.ConfigValue[string](m, "b"); ok || v != "" {
		t.Fatalf("wrong type: %q %v", v, ok)
	}
	if v, ok := models.ConfigValue[string](m, "n"); ok || v != "" {
		t.Fatalf("nil value: %q %v", v, ok)
	}
	if v, ok := models.ConfigValue[bool](m, "i"); ok || v {
		t.Fatalf("int as bool: %v %v", v, ok)
	}
	if v, ok := models.ConfigValue[map[string]any](m, "lists"); !ok || v["a"] != "b" {
		t.Fatalf("map: %v %v", v, ok)
	}
	if v, ok := models.ConfigValue[map[string]any](m, "s"); ok || v != nil {
		t.Fatalf("map wrong type: %v %v", v, ok)
	}
	var nilMap models.JSONMap
	if v, ok := models.ConfigValue[string](nilMap, "s"); ok || v != "" {
		t.Fatalf("nil map: %q %v", v, ok)
	}
}
