package runner

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// A project's tasks each get their own worktree, and an npm project's node_modules is by far the largest part of
// one (hundreds of MB). To keep N tasks from holding N copies, the first finished install is kept under
// <project>/.deps/<sha256 of package-lock.json>, and later tasks on the same lockfile start from a hardlinked copy
// of it: the files are shared on disk and a task that runs `npm install` replaces them rather than editing them.
//
// Both steps are best effort. A task without a lockfile, or with a cache that is missing, is simply left to
// install on its own.

const depsCacheKeep = 3

// seedDepsScript hardlinks the cached node_modules for the worktree's lockfile into the worktree. $1 is the
// worktree and $2 the cache root. It does nothing when node_modules already exists or no cache entry matches.
const seedDepsScript = `
[ -f "$1/package-lock.json" ] && [ ! -e "$1/node_modules" ] || exit 0
key=$(sha256sum "$1/package-lock.json" | cut -d' ' -f1)
[ -d "$2/$key/node_modules" ] || exit 0
cp -al "$2/$key/node_modules" "$1/node_modules" || { rm -rf "$1/node_modules"; exit 1; }
echo "$key"
`

// publishDepsScript keeps the worktree's node_modules as the cache entry for its lockfile when there is none yet,
// then drops all but the newest $3 entries. $1 is the worktree and $2 the cache root.
const publishDepsScript = `
[ -f "$1/package-lock.json" ] && [ -d "$1/node_modules" ] || exit 0
key=$(sha256sum "$1/package-lock.json" | cut -d' ' -f1)
[ ! -e "$2/$key" ] || exit 0
mkdir -p "$2" && tmp="$2/.tmp-$key-$$" && rm -rf "$tmp" && mkdir "$tmp" \
  && cp -al "$1/node_modules" "$tmp/node_modules" && mv "$tmp" "$2/$key" || { rm -rf "$tmp"; exit 1; }
echo "$key"
ls -1t "$2" | tail -n +$(($3 + 1)) | while read -r old; do rm -rf "$2/$old"; done
`

// depsCacheDir is the project's dependency cache, next to its tasks directory: <project>/.deps.
func depsCacheDir(worktree string) string {
	return path.Join(path.Dir(path.Dir(worktree)), ".deps")
}

// seedTaskDependencies starts the worktree's node_modules from the project's cache. It reports whether a cache
// entry was used. A failure is logged to the task and never fails the run.
func (r *AgentTaskRunner) seedTaskDependencies(ctx context.Context, containerID, worktree string) bool {
	res, err := execInContainer(ctx, r.dockerCli, containerID, worktree,
		[]string{"sh", "-c", seedDepsScript, "sh", worktree, depsCacheDir(worktree)})
	if err != nil || res.ExitCode != 0 {
		return false
	}
	return strings.TrimSpace(res.Output) != ""
}

// publishTaskDependencies keeps the worktree's node_modules for later tasks when the project has no entry for
// its lockfile yet.
func (r *AgentTaskRunner) publishTaskDependencies(ctx context.Context, containerID, worktree string) error {
	res, err := execInContainer(ctx, r.dockerCli, containerID, worktree,
		[]string{"sh", "-c", publishDepsScript, "sh", worktree, depsCacheDir(worktree), fmt.Sprint(depsCacheKeep)})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("dependency cache publish exited with code %d: %s", res.ExitCode, strings.TrimSpace(res.Output))
	}
	return nil
}
