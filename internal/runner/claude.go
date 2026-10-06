package runner

import (
	"errors"

	"agent-task-manager/internal/models"

	"github.com/docker/docker/api/types/container"
)

// DefaultClaudeImage specifies the standard container image for Claude Code agent.
const DefaultClaudeImage = "claude-code-agent:latest"

// ClaudeRunner implements AgentRunner for Claude Code CLI execution.
type ClaudeRunner struct {
	image                    string
	dangerouslySkipPermissions bool
}

// ClaudeOption provides functional options for ClaudeRunner.
type ClaudeOption func(*ClaudeRunner)

// WithClaudeImage sets a custom image for ClaudeRunner.
func WithClaudeImage(image string) ClaudeOption {
	return func(r *ClaudeRunner) {
		if image != "" {
			r.image = image
		}
	}
}

// WithClaudeSkipPermissions explicitly enables --dangerously-skip-permissions.
func WithClaudeSkipPermissions(skip bool) ClaudeOption {
	return func(r *ClaudeRunner) {
		r.dangerouslySkipPermissions = skip
	}
}

// NewClaudeRunner returns a ClaudeRunner with default hardened permission checks.
func NewClaudeRunner(opts ...ClaudeOption) *ClaudeRunner {
	r := &ClaudeRunner{
		image:                    DefaultClaudeImage,
		dangerouslySkipPermissions: false,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *ClaudeRunner) Name() string {
	return "claude"
}

func (r *ClaudeRunner) DefaultImage() string {
	return r.image
}

func (r *ClaudeRunner) BuildCommand(task *models.Task) []string {
	if task == nil {
		return []string{"claude"}
	}
	cmd := []string{"claude", "-p", task.Prompt}
	if task.Model != nil && *task.Model != "" {
		cmd = append(cmd, "--model", *task.Model)
	}
	if r.dangerouslySkipPermissions {
		cmd = append(cmd, "--dangerously-skip-permissions")
	}
	return cmd
}

func (r *ClaudeRunner) BuildEnv(task *models.Task) []string {
	return buildBaseEnv(task)
}

func (r *ClaudeRunner) GenerateContainerConfig(task *models.Task) (*container.Config, error) {
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
