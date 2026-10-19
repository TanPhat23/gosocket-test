package service

import (
	"context"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/runner"
)

// ProjectRuntimeWarmer starts a project's container in the background. It returns at once, with an error only when
// the runtime cannot start at all.
type ProjectRuntimeWarmer func(project models.Project) error

// WithTaskProjectRuntimeWarmer wires the warm start. Without it StartProjectRuntime refuses with the
// runtime-unavailable error.
func WithTaskProjectRuntimeWarmer(warm ProjectRuntimeWarmer) TaskServiceOption {
	return func(s *taskService) {
		s.warmRuntime = warm
	}
}

// StartProjectRuntime starts the project's container ahead of the first task, when a user opens the project.
// AC-SEC-02: Requires RoleMember or higher on the project's team; an empty userID is forbidden.
func (s *taskService) StartProjectRuntime(ctx context.Context, projectID, userID string) error {
	project, err := s.authorizeUserForProject(ctx, projectID, userID, models.RoleMember)
	if err != nil {
		return err
	}
	if s.warmRuntime == nil {
		return runner.ErrRuntimeExecutionUnavailable
	}
	return s.warmRuntime(*project)
}
