package runner

import (
	"context"
	"errors"
	"log"
	"time"

	"agent-task-manager/internal/models"
)

// projectWarmTimeout bounds one background start. The first start may build the runtime image, which is slow.
const projectWarmTimeout = 15 * time.Minute

// EnsureProjectRuntime starts the project's sandbox container when it is not already running and returns its route.
// It resolves the image and provisions the container the same way a task run does, so a warm start and a run agree.
func (r *AgentTaskRunner) EnsureProjectRuntime(ctx context.Context, project *models.Project) (*ProjectRouteHandle, error) {
	if project == nil {
		return nil, errors.New("project is required")
	}
	if r.projectSandboxMgr == nil {
		return nil, ErrRuntimeExecutionUnavailable
	}
	imageTag, err := r.projectRuntimeImage(ctx, project)
	if err != nil {
		return nil, err
	}
	return r.projectSandboxMgr.EnsureProjectContainer(ctx, project, imageTag)
}

// WarmProjectRuntime starts the project's container in the background, so it is ready before the first task runs.
// At most one start per project is in flight; a call while one is running returns at once. Errors are logged, since
// the task run starts the container itself when it needs it.
func (r *AgentTaskRunner) WarmProjectRuntime(project models.Project) error {
	if r.projectSandboxMgr == nil {
		return ErrRuntimeExecutionUnavailable
	}

	r.mu.Lock()
	if r.warming == nil {
		r.warming = make(map[string]bool)
	}
	if r.warming[project.ID] {
		r.mu.Unlock()
		return nil
	}
	r.warming[project.ID] = true
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.warming, project.ID)
			r.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), projectWarmTimeout)
		defer cancel()
		if _, err := r.EnsureProjectRuntime(ctx, &project); err != nil {
			log.Printf("warning: starting project runtime %s: %v", project.ID, err)
		}
	}()
	return nil
}
