package runner

import "context"

// PromptFile is an image sent with a prompt. Data is standard base64 without a data: prefix.
type PromptFile struct {
	MIME string
	Name string
	Data string
}

// FilePromptSender is implemented by OpenCode clients that can take files with a prompt. It is optional, so a
// client without it only ever receives plain text.
type FilePromptSender interface {
	PromptSessionWithFiles(ctx context.Context, sessionID, prompt string, files []PromptFile) error
}

// PromptSessionWithFiles sends a prompt with inline files. OpenCode stores each file as a file part of the
// user message, next to the text.
func (c *openCodeClient) PromptSessionWithFiles(ctx context.Context, sessionID, prompt string, files []PromptFile) error {
	parts := make([]map[string]any, 0, len(files))
	for _, f := range files {
		part := map[string]any{
			"data":   f.Data,
			"mime":   f.MIME,
			"source": map[string]any{"type": "inline"},
		}
		if f.Name != "" {
			part["name"] = f.Name
		}
		parts = append(parts, part)
	}
	return c.sendPrompt(ctx, sessionID, map[string]any{"text": prompt, "files": parts})
}
