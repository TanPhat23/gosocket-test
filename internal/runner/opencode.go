package runner

import (
	"errors"

	"agent-task-manager/internal/models"

	"github.com/docker/docker/api/types/container"
)

// DefaultOpenCodeImage specifies the standard container image for OpenCode agents.
const DefaultOpenCodeImage = "opencode-agent:latest"

// OpenCodeRunner implements AgentRunner for OpenCode CLI execution.
type OpenCodeRunner struct {
	image string
}

// OpenCodeOption provides configuration options for OpenCodeRunner.
type OpenCodeOption func(*OpenCodeRunner)

// WithOpenCodeImage overrides the default container image.
func WithOpenCodeImage(image string) OpenCodeOption {
	return func(r *OpenCodeRunner) {
		if image != "" {
			r.image = image
		}
	}
}

// NewOpenCodeRunner creates a new OpenCodeRunner.
func NewOpenCodeRunner(opts ...OpenCodeOption) *OpenCodeRunner {
	r := &OpenCodeRunner{
		image: DefaultOpenCodeImage,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *OpenCodeRunner) Name() string {
	return "opencode"
}

func (r *OpenCodeRunner) DefaultImage() string {
	return r.image
}

func (r *OpenCodeRunner) BuildCommand(task *models.Task) []string {
	if task == nil {
		return []string{"opencode"}
	}
	cmd := []string{"opencode", "run", task.Prompt}
	if task.Model != nil && *task.Model != "" {
		cmd = append(cmd, "--model", *task.Model)
	}
	return cmd
}

func (r *OpenCodeRunner) BuildEnv(task *models.Task) []string {
	return buildBaseEnv(task)
}

func (r *OpenCodeRunner) GenerateContainerConfig(task *models.Task) (*container.Config, error) {
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
