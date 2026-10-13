package runner

import (
	"bytes"
	"context"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"agent-task-manager/internal/config"
)

// ModelInfo describes an AI model available from OpenCode.
type ModelInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Active   bool   `json:"active,omitempty"`
}

// modelCacheEntry is an immutable snapshot of the model list and its expiry.
// It is published atomically so readers always observe a consistent pair.
type modelCacheEntry struct {
	models []ModelInfo
	expiry time.Time
}

var modelCache atomic.Pointer[modelCacheEntry]

// ParseModelEntry converts a raw provider/model string into a structured ModelInfo.
func ParseModelEntry(entry string) ModelInfo {
	trimmed := strings.TrimSpace(entry)
	parts := strings.SplitN(trimmed, "/", 2)
	provider := "custom"
	name := trimmed

	if len(parts) == 2 {
		provider = parts[0]
		name = parts[1]
	}

	displayName := formatModelDisplayName(name, provider)

	return ModelInfo{
		ID:       trimmed,
		Name:     displayName,
		Provider: provider,
		Active:   trimmed == "google/gemini-3.8-flash",
	}
}

func formatModelDisplayName(name, provider string) string {
	cleaned := strings.ReplaceAll(name, "-", " ")
	cleaned = strings.ReplaceAll(cleaned, "_", " ")
	words := strings.Fields(cleaned)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	title := strings.Join(words, " ")
	if strings.Contains(strings.ToLower(name), "gemini-3.8-flash") {
		title += " (Active)"
	}
	return title
}

// DefaultKnownModels returns the primary configured models.
func DefaultKnownModels() []ModelInfo {
	raw := []string{
		"google/gemini-3.8-flash",
		"google/gemini-2.5-pro",
		"anthropic/claude-sonnet-4-5",
		"anthropic/claude-opus-4-6",
		"openai/gpt-5.5",
		"openai/gpt-5.6-luna",
		"opencode-go/qwen3.8-max",
		"opencode-go/deepseek-v4-pro",
		"opencode/big-pickle",
	}

	result := make([]ModelInfo, 0, len(raw))
	for _, id := range raw {
		result = append(result, ParseModelEntry(id))
	}
	return result
}

// ListAvailableModels queries OpenCode for models, falling back to default known models.
func ListAvailableModels(cfg *config.Config) []ModelInfo {
	if c := modelCache.Load(); c != nil && len(c.models) > 0 && time.Now().Before(c.expiry) {
		return slices.Clone(c.models)
	}

	var models []ModelInfo

	if cfg != nil && cfg.OpenCodeBinPath != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		args := []string{"models"}
		if cfg.OpenCodeServerURL != "" {
			args = append(args, "--server", cfg.OpenCodeServerURL)
		}

		cmd := exec.CommandContext(ctx, cfg.OpenCodeBinPath, args...)
		if cfg.OpenCodePassword != "" {
			cmd.Env = append(cmd.Environ(), "OPENCODE_PASSWORD="+cfg.OpenCodePassword)
		}

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err == nil {
			lines := strings.Split(stdout.String(), "\n")
			seen := make(map[string]bool)
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || seen[line] {
					continue
				}
				seen[line] = true
				models = append(models, ParseModelEntry(line))
			}
		}
	}

	if len(models) == 0 {
		models = DefaultKnownModels()
	}

	// Always ensure google/gemini-3.8-flash is at the top if present
	prioritized := make([]ModelInfo, 0, len(models))
	var activeModel *ModelInfo
	for i := range models {
		if models[i].ID == "google/gemini-3.8-flash" {
			activeModel = &models[i]
		}
	}
	if activeModel != nil {
		prioritized = append(prioritized, *activeModel)
	}
	for _, m := range models {
		if m.ID != "google/gemini-3.8-flash" {
			prioritized = append(prioritized, m)
		}
	}

	modelCache.Store(&modelCacheEntry{
		models: prioritized,
		expiry: time.Now().Add(1 * time.Minute),
	})

	// Return a copy so callers cannot mutate the cached snapshot.
	return slices.Clone(prioritized)
}
