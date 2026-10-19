package service

import (
	"context"
	"errors"

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

// ErrRuntimeBusy means tasks are still queued or running in the project's container, so stopping it would fail them.
var ErrRuntimeBusy = errors.New("tasks are still running in this project's container")

// ProjectRuntimeManager inspects and controls a project's container.
type ProjectRuntimeManager interface {
	ProjectRuntimeStatus(ctx context.Context, project models.Project) (*runner.ProjectRuntimeInfo, error)
	StopProjectRuntime(ctx context.Context, project models.Project) error
	RestartProjectRuntime(ctx context.Context, project models.Project) error
	ProjectRuntimeLogs(ctx context.Context, project models.Project, tail int) (string, error)
}

// WithTaskProjectRuntimeManager wires container management. Without it the management calls refuse with the
// runtime-unavailable error.
func WithTaskProjectRuntimeManager(m ProjectRuntimeManager) TaskServiceOption {
	return func(s *taskService) { s.runtimeMgr = m }
}

func (s *taskService) runtimeManager(ctx context.Context, projectID, userID string, role models.TeamRole) (*models.Project, ProjectRuntimeManager, error) {
	project, err := s.authorizeUserForProject(ctx, projectID, userID, role)
	if err != nil {
		return nil, nil, err
	}
	if s.runtimeMgr == nil {
		return nil, nil, runner.ErrRuntimeExecutionUnavailable
	}
	return project, s.runtimeMgr, nil
}

// GetProjectRuntime reports the state of the project's container. AC-SEC-02: requires RoleMember or higher.
func (s *taskService) GetProjectRuntime(ctx context.Context, projectID, userID string) (*runner.ProjectRuntimeInfo, error) {
	project, mgr, err := s.runtimeManager(ctx, projectID, userID, models.RoleMember)
	if err != nil {
		return nil, err
	}
	return mgr.ProjectRuntimeStatus(ctx, *project)
}

// GetProjectRuntimeLogs returns the tail of the project container's output. Requires RoleMember or higher.
func (s *taskService) GetProjectRuntimeLogs(ctx context.Context, projectID, userID string, tail int) (string, error) {
	project, mgr, err := s.runtimeManager(ctx, projectID, userID, models.RoleMember)
	if err != nil {
		return "", err
	}
	return mgr.ProjectRuntimeLogs(ctx, *project, tail)
}

// StopProjectRuntime stops the project's container. It stops teammates' work too, so it needs RoleAdmin, and it
// refuses with ErrRuntimeBusy while tasks are queued or running unless force is set.
func (s *taskService) StopProjectRuntime(ctx context.Context, projectID, userID string, force bool) error {
	project, mgr, err := s.runtimeManager(ctx, projectID, userID, models.RoleAdmin)
	if err != nil {
		return err
	}
	if err := s.refuseWhenBusy(ctx, project.ID, force); err != nil {
		return err
	}
	return mgr.StopProjectRuntime(ctx, *project)
}

// RestartProjectRuntime stops the container and starts it again. Same rules as StopProjectRuntime.
func (s *taskService) RestartProjectRuntime(ctx context.Context, projectID, userID string, force bool) error {
	project, mgr, err := s.runtimeManager(ctx, projectID, userID, models.RoleAdmin)
	if err != nil {
		return err
	}
	if err := s.refuseWhenBusy(ctx, project.ID, force); err != nil {
		return err
	}
	return mgr.RestartProjectRuntime(ctx, *project)
}

func (s *taskService) refuseWhenBusy(ctx context.Context, projectID string, force bool) error {
	if force {
		return nil
	}
	tasks, err := s.taskRepo.List(ctx, projectID)
	if err != nil {
		return err
	}
	for i := range tasks {
		if tasks[i].Status == models.TaskStatusInProgress || tasks[i].Status == models.TaskStatusQueued {
			return ErrRuntimeBusy
		}
	}
	return nil
}
