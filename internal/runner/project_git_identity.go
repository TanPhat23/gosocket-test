package runner

import (
	"context"
	"fmt"
	"strings"

	"agent-task-manager/internal/models"
)

// gitIdentity is the author and committer of a task's commits.
type gitIdentity struct {
	name, email string
}

// defaultGitIdentity is who commits when the task's creator has not set a git identity of their own.
var defaultGitIdentity = gitIdentity{name: gitAgentName, email: gitAgentEmail}

// taskGitIdentity is the creator's own git identity when they set one, and the control plane's otherwise: a task
// from a webhook, a user since deleted, or an unreadable user all fall back rather than fail the run.
func (r *AgentTaskRunner) taskGitIdentity(ctx context.Context, task *models.Task) gitIdentity {
	if r.userRepo == nil || task == nil || task.CreatedByID == nil || *task.CreatedByID == "" {
		return defaultGitIdentity
	}
	user, err := r.userRepo.GetByID(ctx, *task.CreatedByID)
	if err != nil || user.GitName == "" || user.GitEmail == "" {
		return defaultGitIdentity
	}
	return gitIdentity{name: user.GitName, email: user.GitEmail}
}

// envArgs is `env` with the identity set, which wins over the GIT_* values fixed on the container.
func (id gitIdentity) envArgs() []string {
	return []string{
		"env",
		"GIT_AUTHOR_NAME=" + id.name, "GIT_AUTHOR_EMAIL=" + id.email,
		"GIT_COMMITTER_NAME=" + id.name, "GIT_COMMITTER_EMAIL=" + id.email,
	}
}

// runGitAs is runGit with the commit identity set to id.
func (r *AgentTaskRunner) runGitAs(ctx context.Context, containerID, dir string, id gitIdentity, args ...string) (string, error) {
	argv := append(append(id.envArgs(), hardenedGit...), args...)
	res, err := execInContainer(ctx, r.dockerCli, containerID, dir, argv)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("git %s exited with code %d: %s", args[0], res.ExitCode, strings.TrimSpace(res.Output))
	}
	return res.Output, nil
}

// rewriteIdentityScript points filter-branch at the identity passed in ATM_GIT_NAME and ATM_GIT_EMAIL. They are
// read from the environment, never spliced into the script, so no name or email can alter it.
const rewriteIdentityScript = `GIT_AUTHOR_NAME="$ATM_GIT_NAME"; GIT_AUTHOR_EMAIL="$ATM_GIT_EMAIL"; ` +
	`GIT_COMMITTER_NAME="$ATM_GIT_NAME"; GIT_COMMITTER_EMAIL="$ATM_GIT_EMAIL"; ` +
	`export GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL`

// alignCommitIdentity makes every commit in base..HEAD of the task branch carry id as author and committer. The
// agent has a shell and can commit under any name (one used a stranger's address, which GitHub then attributed to
// that stranger), so the identity is enforced here rather than trusted. It reports whether history was rewritten.
// Only commits that differ are touched, so a branch that is already right keeps its commit ids.
func (r *AgentTaskRunner) alignCommitIdentity(ctx context.Context, containerID, worktree, base, branch string, id gitIdentity) (bool, error) {
	out, err := r.runGit(ctx, containerID, worktree, "log", "--format=%an%x09%ae%x09%cn%x09%ce", base+"..HEAD")
	if err != nil {
		return false, err
	}
	want := strings.Join([]string{id.name, id.email, id.name, id.email}, "\t")
	differs := false
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" && line != want {
			differs = true
			break
		}
	}
	if !differs {
		return false, nil
	}
	argv := append([]string{"env", "ATM_GIT_NAME=" + id.name, "ATM_GIT_EMAIL=" + id.email, "FILTER_BRANCH_SQUELCH_WARNING=1"}, hardenedGit...)
	argv = append(argv, "filter-branch", "-f", "-d", worktree+"/.git-rewrite", "--env-filter", rewriteIdentityScript, base+"..HEAD")
	res, err := execInContainer(ctx, r.dockerCli, containerID, worktree, argv)
	if err != nil {
		return false, err
	}
	if res.ExitCode != 0 {
		return false, fmt.Errorf("git filter-branch exited with code %d: %s", res.ExitCode, strings.TrimSpace(res.Output))
	}
	// filter-branch keeps the old branch tip under refs/original; drop it so the old identity is not exported.
	_, _ = r.runGitStatus(ctx, containerID, worktree, "update-ref", "-d", "refs/original/refs/heads/"+branch)
	return true, nil
}
