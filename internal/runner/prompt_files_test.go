package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPromptSessionWithFiles_PostsImagesAsInlineFileParts(t *testing.T) {
	var gotPath string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewOpenCodeClient(server.URL, "pw", nil)
	files := []PromptFile{{MIME: "image/png", Name: "shot.png", Data: "aGVsbG8="}}
	if err := client.(FilePromptSender).PromptSessionWithFiles(context.Background(), "ses_1", "look at this", files); err != nil {
		t.Fatalf("PromptSessionWithFiles: %v", err)
	}

	if gotPath != "/api/session/ses_1/prompt" {
		t.Fatalf("path = %s, want the session prompt route", gotPath)
	}
	if body["text"] != "look at this" {
		t.Fatalf("text = %v", body["text"])
	}
	parts, ok := body["files"].([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("files = %v, want one file part", body["files"])
	}
	part := parts[0].(map[string]any)
	if part["data"] != "aGVsbG8=" || part["mime"] != "image/png" || part["name"] != "shot.png" {
		t.Fatalf("file part = %v", part)
	}
	if src, _ := part["source"].(map[string]any); src["type"] != "inline" {
		t.Fatalf("source = %v, want inline", part["source"])
	}
}

func TestPromptSession_TextOnlyStaysTheSameBody(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := NewOpenCodeClient(server.URL, "pw", nil).PromptSession(context.Background(), "ses_2", "just text"); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}
	if _, has := body["files"]; has {
		t.Fatalf("text-only prompt sent files: %v", body)
	}
	if body["text"] != "just text" {
		t.Fatalf("text = %v", body["text"])
	}
}
