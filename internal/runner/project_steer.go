package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"agent-task-manager/internal/models"
)

// SteerTurnResult describes what a finished steered turn left behind.
type SteerTurnResult struct {
	Committed bool
	Pushed    bool
	Files     int
	Additions int
	Deletions int
}

// FinalizeSteeredTurn is the tail of a user steer: it waits for the turn that started after `before` to
// reach a terminal outcome, commits whatever the agent left in the task worktree, and pushes the task
// branch when the integration auto-pushes (the run path's rule), which also updates an open pull request.
//
// It only talks to the project's already running container (it never provisions one) and reuses the run
// path's hardened git calls and git-transfer helper. It does not verify, open a pull request or touch the
// task status; the caller owns status and logging (logf receives terminal-formatted lines).
func (r *AgentTaskRunner) FinalizeSteeredTurn(ctx context.Context, task *models.Task, before *time.Time, logf func(string)) (*SteerTurnResult, error) {
	if task == nil || task.SessionID == nil || *task.SessionID == "" {
		return nil, errors.New("task has no associated session")
	}
	if r.repoTransfer == nil || r.projectSandboxMgr == nil || r.dockerCli == nil || r.projectRepo == nil {
		return nil, errors.New("project container runtime is not configured")
	}
	if logf == nil {
		logf = func(string) {}
	}
	project, err := r.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup project %s: %w", task.ProjectID, err)
	}
	route, ok, err := r.projectSandboxMgr.GetProjectRoute(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	if !ok || route == nil {
		return nil, errors.New("project container is not running")
	}
	newClient := r.projectClientFor
	if newClient == nil {
		newClient = func(url, pw string) (OpenCodeClient, error) { return NewProjectOpenCodeClient(url, pw, nil) }
	}
	oc, err := newClient(route.EndpointURL, route.AuthPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize project opencode client: %w", err)
	}
	sessionID := *task.SessionID

	// The session was bound to the task worktree when the run started; that directory is where the turn edits.
	sess, err := oc.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to read session: %w", err)
	}
	worktree := sess.Directory
	if !strings.HasPrefix(worktree, "/") {
		return nil, errors.New("session is not bound to a task worktree")
	}

	if err := r.waitForSessionOutcome(ctx, oc, sessionID, before); err != nil {
		return nil, err
	}

	res := &SteerTurnResult{}
	if _, err := r.runGit(ctx, route.ContainerID, worktree, "add", "-A"); err != nil {
		return nil, err
	}
	staged, err := r.runGitStatus(ctx, route.ContainerID, worktree, "diff", "--cached", "--quiet")
	if err != nil {
		return nil, err
	}
	switch staged.ExitCode {
	case 0:
		logf("\x1b[33m[Agent Control Plane]\x1b[0m The steered turn made no changes; nothing to commit.\r\n")
		return res, nil
	case 1:
	default:
		return nil, fmt.Errorf("git diff --cached exited with code %d", staged.ExitCode)
	}

	prevHead, err := r.runGit(ctx, route.ContainerID, worktree, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	prevHead = strings.TrimSpace(prevHead)
	msg := task.Title
	if msg == "" {
		msg = "Task " + task.ID
	}
	msg += " (steered)"
	// Only this commit is made under the creator's identity: earlier commits may already be pushed, and rewriting
	// them would make the next push a non-fast-forward.
	if _, err := r.runGitAs(ctx, route.ContainerID, worktree, r.taskGitIdentity(ctx, task), "commit", "-q", "-m", msg); err != nil {
		return nil, err
	}
	numstat, err := r.runGit(ctx, route.ContainerID, worktree, "diff", "--no-ext-diff", "--no-textconv", "--numstat", prevHead, "HEAD")
	if err != nil {
		return nil, err
	}
	res.Committed = true
	res.Files, res.Additions, res.Deletions = parseNumstat(numstat)
	logf(fmt.Sprintf("\r\n\x1b[32m✔ Steered changes committed: %d files changed (+%d/-%d)\x1b[0m\r\n", res.Files, res.Additions, res.Deletions))

	integ, token, err := r.resolveGitHubIntegration(ctx, task)
	if err != nil {
		logf(fmt.Sprintf("\x1b[33m[Agent Control Plane]\x1b[0m Not pushed: %v\r\n", err))
		return res, nil
	}
	if !integrationAutoPR(integ) || r.ghProvider == nil {
		logf("\x1b[33m[Agent Control Plane]\x1b[0m Auto-push is off for this integration; the change is committed locally only.\r\n")
		return res, nil
	}
	if err := r.repoTransfer.PushTaskBranch(ctx, project.ID, task.ID, integ.ExternalID, token); err != nil {
		logf(fmt.Sprintf("\x1b[31m[Agent Control Plane] Git push failed: %v\x1b[0m\r\n", err))
		return res, err
	}
	res.Pushed = true
	logf("\x1b[32m✔ Pushed the steered changes to the task branch.\x1b[0m\r\n")
	return res, nil
}
