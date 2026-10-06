package runner_test

import (
	"testing"

	"agent-task-manager/internal/config"
	"agent-task-manager/internal/runner"
)

func TestParseModelEntry(t *testing.T) {
	entry := "google/gemini-3.8-flash"
	info := runner.ParseModelEntry(entry)

	if info.ID != "google/gemini-3.8-flash" {
		t.Errorf("expected ID google/gemini-3.8-flash, got %s", info.ID)
	}
	if info.Provider != "google" {
		t.Errorf("expected Provider google, got %s", info.Provider)
	}
	if info.Name == "" {
		t.Errorf("expected non-empty name")
	}
}

func TestListAvailableModelsFallback(t *testing.T) {
	cfg := &config.Config{
		OpenCodeBinPath:   "/non/existent/path/opencode",
		OpenCodeServerURL: "http://127.0.0.1:9999",
	}

	models := runner.ListAvailableModels(cfg)
	if len(models) == 0 {
		t.Fatalf("expected fallback models when opencode is unreachable")
	}

	foundGemini := false
	for _, m := range models {
		if m.ID == "google/gemini-3.8-flash" {
			foundGemini = true
			break
		}
	}
	if !foundGemini {
		t.Errorf("expected google/gemini-3.8-flash in fallback list")
	}
}
