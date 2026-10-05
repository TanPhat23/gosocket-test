package sandbox

import (
	"io"
	"time"
)

// SandboxConfig specifies the runtime configuration for a sandboxed container.
type SandboxConfig struct {
	Image         string
	Command       []string
	Env           []string
	WorktreeBind  string // e.g. /srv/agent-tasks/worktrees/task-1:/workspace:rw or host path
	WorkingDir    string // default: /workspace
	MemoryLimit   int64  // default: 4GB (4 * 1024 * 1024 * 1024)
	CPULimit      int64  // default: 2 CPUs = 2e9 NanoCPUs
	NetworkMode   string // default: "bridge"
	Timeout       time.Duration
}

// ContainerInstance represents an active or executed container run.
type ContainerInstance struct {
	ID       string
	ExitCode int
	Logs     io.ReadCloser
}
