package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
)

// This file is the manual tail of a finished task: pushing task/<id> and opening its pull request on
// request, and reading the task's diff. Like the run path it never runs git on the host: the push goes
// through the git-transfer helper and every read is a hardened git call in the project container.

// taskWorktreeInContainer returns the project's running container and the worktree its session for the
// task is bound to. It never provisions a container; no running container is common.ErrServiceUnavailable.
func (r *AgentTaskRunner) taskWorktreeInContainer(ctx context.Context, task *models.Task) (*ProjectRouteHandle, string, error) {
	if task == nil || task.SessionID == nil || *task.SessionID == "" {
		return nil, "", errors.New("task has no associated session")
	}
	if r.projectSandboxMgr == nil || r.dockerCli == nil {
		return nil, "", common.ErrServiceUnavailable
	}
	route, ok, err := r.projectSandboxMgr.GetProjectRoute(ctx, task.ProjectID)
	if err != nil {
		return nil, "", err
	}
	if !ok || route == nil {
		return nil, "", common.ErrServiceUnavailable
	}
	newClient := r.projectClientFor
	if newClient == nil {
		newClient = func(url, pw string) (OpenCodeClient, error) { return NewProjectOpenCodeClient(url, pw, nil) }
	}
	oc, err := newClient(route.EndpointURL, route.AuthPassword)
	if err != nil {
		return nil, "", fmt.Errorf("failed to initialize project opencode client: %w", err)
	}
	sess, err := oc.GetSession(ctx, *task.SessionID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read session: %w", err)
	}
	if !strings.HasPrefix(sess.Directory, "/") {
		return nil, "", errors.New("session is not bound to a task worktree")
	}
	return route, sess.Directory, nil
}

// PublishTaskPullRequest pushes the task branch from the project volume and opens its pull request,
// returning the PR URL. A task that already has a pull request returns it without pushing. The push does
// not need the project container; the change counts in the PR body are best effort and are zero when the
// container is gone. logf receives terminal-formatted progress lines and may be nil.
func (r *AgentTaskRunner) PublishTaskPullRequest(ctx context.Context, task *models.Task, logf func(string)) (string, error) {
	if task == nil {
		return "", errors.New("task is required")
	}
	if task.PRURL != nil && *task.PRURL != "" {
		return *task.PRURL, nil
	}
	if r.repoTransfer == nil || r.ghProvider == nil || r.taskRepo == nil {
		return "", errors.New("pull request publishing is not configured")
	}
	if logf == nil {
		logf = func(string) {}
	}
	integ, token, err := r.resolveGitHubIntegration(ctx, task)
	if err != nil {
		return "", err
	}
	branch := "task/" + task.ID

	logf(fmt.Sprintf("\x1b[36m[Agent Control Plane]\x1b[0m Pushing branch %s to %s...\r\n", branch, integ.ExternalID))
	if err := r.repoTransfer.PushTaskBranch(ctx, task.ProjectID, task.ID, integ.ExternalID, token); err != nil {
		logf(fmt.Sprintf("\x1b[31m[Agent Control Plane] Git push failed: %v\x1b[0m\r\n", err))
		return "", err
	}

	var files, adds, dels int
	if route, worktree, err := r.taskWorktreeInContainer(ctx, task); err == nil {
		if numstat, err := r.runGit(ctx, route.ContainerID, worktree,
			"diff", "--no-ext-diff", "--no-textconv", "--numstat", integrationBaseBranch(integ)+"...HEAD"); err == nil {
			files, adds, dels = parseNumstat(numstat)
		}
	}
	return r.createTaskPullRequest(ctx, task, integ, token, branch, files, adds, dels, logf)
}

// TaskDiff returns the task's unified diff against the integration's base branch, read in the project
// container. A task that never started has no diff.
func (r *AgentTaskRunner) TaskDiff(ctx context.Context, task *models.Task) (string, error) {
	if task == nil || task.SessionID == nil || *task.SessionID == "" {
		return "", nil
	}
	route, worktree, err := r.taskWorktreeInContainer(ctx, task)
	if err != nil {
		return "", err
	}
	base := "main"
	if integ, err := r.findGitHubIntegration(ctx, task); err == nil {
		base = integrationBaseBranch(integ)
	}
	raw, err := r.runGit(ctx, route.ContainerID, worktree,
		"diff", "--no-ext-diff", "--no-textconv", "--no-color", base+"...HEAD")
	if err != nil {
		return "", err
	}
	if len(raw) > diffBroadcastCap {
		raw = raw[:diffBroadcastCap]
	}
	return raw, nil
}
