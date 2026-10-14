package runner_test

import (
	"os"
	"path/filepath"
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

func TestListAvailableModels_DoesNotExecHostBinary(t *testing.T) {
	runner.ResetModelCacheForTest()
	t.Cleanup(runner.ResetModelCacheForTest)

	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	script := filepath.Join(dir, "opencode")
	body := "#!/bin/sh\necho executed > '" + marker + "'\necho fake/only-model\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cfg := &config.Config{
		OpenCodeBinPath:   script,
		OpenCodeServerURL: "http://127.0.0.1:9999",
		OpenCodePassword:  "secret-not-to-be-exported",
	}

	got := runner.ListAvailableModels(cfg)

	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("host binary was executed (marker stat err=%v)", err)
	}
	defaults := runner.DefaultKnownModels()
	if len(got) != len(defaults) {
		t.Fatalf("expected %d default models, got %d", len(defaults), len(got))
	}
	if got[0].ID != "google/gemini-3.8-flash" {
		t.Errorf("expected active model first, got %s", got[0].ID)
	}
	for _, m := range got {
		if m.ID == "fake/only-model" {
			t.Fatalf("model list came from host binary output")
		}
	}

	// Second call is served from the cache and still returns defaults.
	cached := runner.ListAvailableModels(cfg)
	if len(cached) != len(defaults) {
		t.Errorf("cached result length %d, want %d", len(cached), len(defaults))
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("host binary executed on cached call")
	}
}
