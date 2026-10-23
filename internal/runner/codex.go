package runner

import (
	"errors"

	"agent-task-manager/internal/models"

	"github.com/docker/docker/api/types/container"
)

// DefaultCodexImage specifies the standard container image for OpenAI Codex.
const DefaultCodexImage = "codex-agent:latest"

// CodexRunner implements AgentRunner for Codex non-interactive execution.
type CodexRunner struct {
	image string
}

// CodexOption provides functional options for CodexRunner.
type CodexOption func(*CodexRunner)

// WithCodexImage sets a custom image for CodexRunner.
func WithCodexImage(image string) CodexOption {
	return func(r *CodexRunner) {
		if image != "" {
			r.image = image
		}
	}
}

// NewCodexRunner returns a new CodexRunner.
func NewCodexRunner(opts ...CodexOption) *CodexRunner {
	r := &CodexRunner{
		image: DefaultCodexImage,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *CodexRunner) Name() string {
	return "codex"
}

func (r *CodexRunner) DefaultImage() string {
	return r.image
}

func (r *CodexRunner) BuildCommand(task *models.Task) []string {
	if task == nil {
		return []string{"codex"}
	}
	cmd := []string{"codex", "exec", task.Prompt}
	if task.Model != nil && *task.Model != "" {
		cmd = append(cmd, "--model", *task.Model)
	}
	return cmd
}

func (r *CodexRunner) BuildEnv(task *models.Task) []string {
	return buildBaseEnv(task)
}

func (r *CodexRunner) GenerateContainerConfig(task *models.Task) (*container.Config, error) {
	if task == nil {
		return nil, errors.New("task cannot be nil")
	}

	return &container.Config{
		Image:        r.DefaultImage(),
		Cmd:          r.BuildCommand(task),
		Env:          r.BuildEnv(task),
		WorkingDir:   "/workspace",
		AttachStdout: true,
		AttachStderr: true,
	}, nil
}
