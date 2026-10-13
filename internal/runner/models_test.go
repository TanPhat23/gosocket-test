package runner_test

import (
	"sync"
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

func TestListAvailableModelsCallerCannotMutateCache(t *testing.T) {
	cfg := &config.Config{OpenCodeBinPath: "/non/existent/path/opencode"}

	first := runner.ListAvailableModels(cfg)
	if len(first) == 0 {
		t.Fatalf("expected models")
	}
	want := first[0].ID
	first[0].ID = "mutated"

	second := runner.ListAvailableModels(cfg)
	if second[0].ID != want {
		t.Fatalf("cache was mutated through returned slice: got %q want %q", second[0].ID, want)
	}
	second[0].ID = "mutated-again"
	if third := runner.ListAvailableModels(cfg); third[0].ID != want {
		t.Fatalf("cached read result aliased cache: got %q want %q", third[0].ID, want)
	}
}

func TestListAvailableModelsConcurrent(t *testing.T) {
	cfg := &config.Config{OpenCodeBinPath: "/non/existent/path/opencode"}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				m := runner.ListAvailableModels(cfg)
				if len(m) == 0 {
					t.Error("expected models")
					return
				}
				m[0].Name = "scribble"
			}
		}()
	}
	wg.Wait()
}
