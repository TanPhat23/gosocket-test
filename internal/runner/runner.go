package runner

import (
	"fmt"
	"strings"

	"agent-task-manager/internal/models"

	"github.com/docker/docker/api/types/container"
)

// AgentRunner defines the standard interface for containerized agent execution.
type AgentRunner interface {
	Name() string
	DefaultImage() string
	BuildCommand(task *models.Task) []string
	BuildEnv(task *models.Task) []string
	GenerateContainerConfig(task *models.Task) (*container.Config, error)
}

// NewRunner returns the configured AgentRunner implementation for the requested agentName.
func NewRunner(agentName string) (AgentRunner, error) {
	switch strings.ToLower(agentName) {
	case "opencode":
		return NewOpenCodeRunner(), nil
	case "claude":
		return NewClaudeRunner(), nil
	case "codex":
		return NewCodexRunner(), nil
	default:
		return nil, fmt.Errorf("unsupported agent runner: %q", agentName)
	}
}

// buildBaseEnv constructs common environment variables for tasks while keeping credentials isolated.
func buildBaseEnv(task *models.Task) []string {
	if task == nil {
		return []string{}
	}
	env := []string{
		"ATM_TASK_ID=" + task.ID,
		"ATM_PROJECT_ID=" + task.ProjectID,
	}
	if task.Model != nil && *task.Model != "" {
		env = append(env, "ATM_TASK_MODEL="+*task.Model)
	}
	return env
}
