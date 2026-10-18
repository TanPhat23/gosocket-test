package transfer

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/pkg/stdcopy"

	"agent-task-manager/internal/sandbox"
)

const (
	ExitCodeSubmoduleDetected = 101
	ExitCodeLFSDetected       = 102
	// ExitCodeVolumeNotWritable means /workspaces is not writable by UID 1000. Docker copies the image's
	// /workspaces ownership into an empty named volume on first mount, so the helper image must ship
	// /workspaces owned by 1000:1000 (the project runtime image does).
	ExitCodeVolumeNotWritable = 103
)

const (
	// helperRemoveTimeout bounds removing a helper container once its operation has ended.
	helperRemoveTimeout = 30 * time.Second
	// maxSyncCheckBytes caps the sync-age reply; a valid reply is a short integer or "missing".
	maxSyncCheckBytes = 64
)

type volumeTransferHelper struct {
	dockerCli sandbox.DockerClient
	imageTag  string
}

// removeHelperContainer force-removes a helper container its operation started. It ignores the operation's
// cancellation: a request cancelled while its helper runs must still remove that helper, or the container leaks.
func removeHelperContainer(ctx context.Context, cli sandbox.DockerClient, id string) {
	rmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), helperRemoveTimeout)
	defer cancel()
	_ = cli.ContainerRemove(rmCtx, id, container.RemoveOptions{Force: true})
}

// NewVolumeTransferHelper creates an instance of VolumeHelper using DockerClient.
func NewVolumeTransferHelper(dockerCli sandbox.DockerClient, imageTag string) VolumeHelper {
	return &volumeTransferHelper{
		dockerCli: dockerCli,
		imageTag:  imageTag,
	}
}

func (h *volumeTransferHelper) ApplyBundleAndProvisionWorktree(
	ctx context.Context,
	repoID, taskID, refName string,
	bundleStream io.Reader,
) (string, error) {
	if err := ValidateRepoID(repoID); err != nil {
		return "", err
	}
	if err := ValidateTaskID(taskID); err != nil {
		return "", err
	}
	if err := ValidateRefName(refName); err != nil {
		return "", err
	}

	volumeName := sandbox.ProjectDataVolumeName(repoID)
	branchName := strings.TrimPrefix(refName, "refs/heads/")

	// In-container helper shell script executed with strict options.
	// Spools stdin bundle to /tmp/scratch.bundle, updates/inits bare repo,
	// checks submodules and LFS, and creates worktree with --no-checkout.
	helperScript := fmt.Sprintf(`
set -euo pipefail
REPO_DIR="/workspaces/%s/repository.git"
TASK_DIR="/workspaces/%s/tasks/%s"
TMP_BUNDLE="/tmp/scratch.bundle"

[ -w /workspaces ] || { echo "/workspaces is not writable by the helper user" >&2; exit %d; }
cat > "$TMP_BUNDLE"

if [ ! -d "$REPO_DIR" ]; then
    mkdir -p "$(dirname "$REPO_DIR")"
    git clone --bare "$TMP_BUNDLE" "$REPO_DIR"
else
    # Forced: the volume's base branch mirrors upstream, and upstream may be behind a ref that was moved locally
    # (a non-forced fetch refuses that as non-fast-forward and fails every later ingest of the project).
    git -C "$REPO_DIR" -c core.hooksPath=/dev/null -c core.fsmonitor=false fetch "$TMP_BUNDLE" "+%s:%s"
fi
rm -f "$TMP_BUNDLE"
# Stamp the sync so tasks started soon after can reuse the fetched base instead of pulling it again.
date +%%s > "$REPO_DIR/atm-synced-at"

# Inspection 1: Submodules (.gitmodules or mode 160000 gitlink)
if git -C "$REPO_DIR" -c core.hooksPath=/dev/null cat-file -e "%s:.gitmodules" 2>/dev/null; then
    echo "repository contains submodules" >&2
    exit %d
fi
if git -C "$REPO_DIR" -c core.hooksPath=/dev/null ls-tree -r "%s" | grep -q "^160000 commit "; then
    echo "repository contains gitlink submodules" >&2
    exit %d
fi

# Inspection 2: Git LFS (.gitattributes filter=lfs or pointer signature)
if git -C "$REPO_DIR" -c core.hooksPath=/dev/null show "%s:.gitattributes" 2>/dev/null | grep -q "filter=lfs"; then
    echo "repository contains Git LFS pointers" >&2
    exit %d
fi
if git -C "$REPO_DIR" -c core.hooksPath=/dev/null grep -q -E "^version https://git-lfs.github.com/spec/v1" "%s" 2>/dev/null; then
    echo "repository contains Git LFS pointer blobs" >&2
    exit %d
fi

# Provision task worktree with --no-checkout
mkdir -p "$(dirname "$TASK_DIR")"
rm -rf "$TASK_DIR"
git -C "$REPO_DIR" -c core.hooksPath=/dev/null worktree prune 2>/dev/null || true
git -C "$REPO_DIR" -c core.hooksPath=/dev/null branch -D "task/%s" 2>/dev/null || true
git -C "$REPO_DIR" -c core.hooksPath=/dev/null -c core.fsmonitor=false worktree add --no-checkout -b "task/%s" "$TASK_DIR" "%s"

# Pin the base commit this task was provisioned from. Diffs read the pin, so a later move of the base branch
# does not change them. The pin is created once and never moved by a re-ingest.
if ! git -C "$REPO_DIR" -c core.hooksPath=/dev/null rev-parse -q --verify "refs/atm/base/%s" >/dev/null; then
    git -C "$REPO_DIR" -c core.hooksPath=/dev/null update-ref "refs/atm/base/%s" "%s"
fi
`, repoID, repoID, taskID, ExitCodeVolumeNotWritable, refName, refName, branchName, ExitCodeSubmoduleDetected, branchName, ExitCodeSubmoduleDetected, branchName, ExitCodeLFSDetected, branchName, ExitCodeLFSDetected, taskID, taskID, branchName, taskID, taskID, refName)

	containerCfg := &container.Config{
		Image:        h.imageTag,
		User:         "1000:1000",
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		StdinOnce:    true,
		Cmd:          []string{"/bin/sh", "-c", helperScript},
	}

	hostCfg := &container.HostConfig{
		NetworkMode:    "none",
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    defaultSecurityOpt(),
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: volumeName,
				Target: "/workspaces",
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
				TmpfsOptions: &mount.TmpfsOptions{
					SizeBytes: 1024 * 1024 * 1024, // 1GiB
				},
			},
		},
	}

	resp, err := h.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return "", fmt.Errorf("failed to create volume helper container: %w", err)
	}
	defer removeHelperContainer(ctx, h.dockerCli, resp.ID)

	// Attach to container stdin/stdout/stderr before ContainerStart
	hijacked, err := h.dockerCli.ContainerAttach(ctx, resp.ID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return "", fmt.Errorf("failed to attach to volume helper container: %w", err)
	}
	defer hijacked.Close()

	if err := h.dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start volume helper container: %w", err)
	}
	// Nothing reads the helper's output, so drain it: an unread attach stream can stall the helper.
	go func() { _, _ = io.Copy(io.Discard, hijacked.Reader) }()

	// Stream bundle bytes into container stdin via hijacked connection, then close write half
	streamErrCh := make(chan error, 1)
	go func() {
		defer hijacked.CloseWrite()
		if bundleStream != nil {
			if _, err := io.Copy(hijacked.Conn, bundleStream); err != nil {
				streamErrCh <- fmt.Errorf("failed to pipe bundle to volume helper: %w", err)
				return
			}
		}
		streamErrCh <- nil
	}()

	statusCh, errCh := h.dockerCli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case streamErr := <-streamErrCh:
		if streamErr != nil {
			return "", streamErr
		}
	case <-ctx.Done():
		return "", ctx.Err()
	}

	select {
	case waitErr, ok := <-errCh:
		if ok && waitErr != nil {
			return "", fmt.Errorf("volume helper container wait failed: %w", waitErr)
		}
	case status, ok := <-statusCh:
		if !ok {
			break
		}
		if status.StatusCode == ExitCodeSubmoduleDetected {
			return "", ErrSubmodulesNotSupported
		}
		if status.StatusCode == ExitCodeLFSDetected {
			return "", ErrGitLFSNotSupported
		}
		if status.StatusCode == ExitCodeVolumeNotWritable {
			return "", ErrVolumeNotWritable
		}
		if status.StatusCode != 0 {
			return "", fmt.Errorf("%w: helper exited with code %d", ErrGitTransferFailed, status.StatusCode)
		}
	case <-ctx.Done():
		return "", ctx.Err()
	}

	return fmt.Sprintf("/workspaces/%s/tasks/%s", repoID, taskID), nil
}

func (h *volumeTransferHelper) ExportTaskBundle(ctx context.Context, repoID, taskID string, outStream io.Writer) error {
	if err := ValidateRepoID(repoID); err != nil {
		return err
	}
	if err := ValidateTaskID(taskID); err != nil {
		return err
	}

	volumeName := sandbox.ProjectDataVolumeName(repoID)
	branchRef := fmt.Sprintf("refs/heads/task/%s", taskID)

	exportScript := fmt.Sprintf(`
set -euo pipefail
REPO_DIR="/workspaces/%s/repository.git"
git -C "$REPO_DIR" -c core.hooksPath=/dev/null -c core.fsmonitor=false bundle create - "%s"
`, repoID, branchRef)

	containerCfg := &container.Config{
		Image:        h.imageTag,
		User:         "1000:1000",
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"/bin/sh", "-c", exportScript},
	}

	hostCfg := &container.HostConfig{
		NetworkMode:    "none",
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    defaultSecurityOpt(),
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeVolume,
				Source:   volumeName,
				Target:   "/workspaces",
				ReadOnly: true,
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
			},
		},
	}

	return h.runCapturedHelper(ctx, containerCfg, hostCfg, outStream, "export")
}

// DiffTask writes the unified diff of a task's branch against its pinned base commit to outStream. The helper
// mounts the project volume read-only and has no network, so the diff needs no project container.
// A task that never committed writes nothing.
func (h *volumeTransferHelper) DiffTask(ctx context.Context, repoID, taskID, baseRef string, outStream io.Writer) error {
	if err := ValidateRepoID(repoID); err != nil {
		return err
	}
	if err := ValidateTaskID(taskID); err != nil {
		return err
	}
	if err := ValidateRefName(baseRef); err != nil {
		return err
	}

	volumeName := sandbox.ProjectDataVolumeName(repoID)
	diffScript := fmt.Sprintf(`
set -euo pipefail
REPO_DIR="/workspaces/%s/repository.git"
[ -d "$REPO_DIR" ] || { echo "project repository is missing" >&2; exit 2; }
TASK_REF="refs/heads/task/%s"
PIN_REF="refs/atm/base/%s"
git -C "$REPO_DIR" rev-parse -q --verify "$TASK_REF" >/dev/null 2>&1 || exit 0
# Tasks provisioned before the pin existed fall back to the base branch the caller names.
BASE_REF="$PIN_REF"
git -C "$REPO_DIR" rev-parse -q --verify "$PIN_REF" >/dev/null 2>&1 || BASE_REF="$ATM_BASE_REF"
if git -C "$REPO_DIR" merge-base "$BASE_REF" "$TASK_REF" >/dev/null 2>&1; then
    exec git -C "$REPO_DIR" -c core.hooksPath=/dev/null -c core.fsmonitor=false diff --no-ext-diff --no-textconv --no-color "$BASE_REF...$TASK_REF"
fi
# A task whose history was re-initialized shares no commit with its base. Show the whole difference between the two
# trees instead of failing the review.
exec git -C "$REPO_DIR" -c core.hooksPath=/dev/null -c core.fsmonitor=false diff --no-ext-diff --no-textconv --no-color "$BASE_REF" "$TASK_REF"
`, repoID, taskID, taskID)

	containerCfg := &container.Config{
		Image:        h.imageTag,
		User:         "1000:1000",
		AttachStdout: true,
		AttachStderr: true,
		Env:          []string{"ATM_BASE_REF=" + baseRef},
		Cmd:          []string{"/bin/sh", "-c", diffScript},
	}

	hostCfg := &container.HostConfig{
		NetworkMode:    "none",
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    defaultSecurityOpt(),
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeVolume,
				Source:   volumeName,
				Target:   "/workspaces",
				ReadOnly: true,
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
			},
		},
	}

	return h.runCapturedHelper(ctx, containerCfg, hostCfg, outStream, "diff")
}

// RemoveTaskWorkspace deletes the task's worktree from the project volume. It is idempotent: a worktree that is
// already gone only gets its registration pruned. The task branch and the base pin stay in the repository.
func (h *volumeTransferHelper) RemoveTaskWorkspace(ctx context.Context, repoID, taskID string) error {
	if err := ValidateRepoID(repoID); err != nil {
		return err
	}
	if err := ValidateTaskID(taskID); err != nil {
		return err
	}

	volumeName := sandbox.ProjectDataVolumeName(repoID)
	removeScript := fmt.Sprintf(`
set -eu
REPO_DIR="/workspaces/%s/repository.git"
TASK_DIR="/workspaces/%s/tasks/%s"
if [ -d "$TASK_DIR" ]; then
    git -C "$REPO_DIR" -c core.hooksPath=/dev/null worktree remove --force "$TASK_DIR" 2>/dev/null || rm -rf "$TASK_DIR"
fi
git -C "$REPO_DIR" -c core.hooksPath=/dev/null worktree prune
`, repoID, repoID, taskID)

	containerCfg := &container.Config{
		Image:        h.imageTag,
		User:         "1000:1000",
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"/bin/sh", "-c", removeScript},
	}

	hostCfg := &container.HostConfig{
		NetworkMode:    "none",
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    defaultSecurityOpt(),
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: volumeName,
				Target: "/workspaces",
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
			},
		},
	}

	return h.runCapturedHelper(ctx, containerCfg, hostCfg, io.Discard, "workspace cleanup")
}

// LastSyncAge reports how long ago the project was last fetched from its remote. found is false when the project
// has never been fetched or baseRef is not present locally, so the caller must fetch.
func (h *volumeTransferHelper) LastSyncAge(ctx context.Context, repoID, baseRef string) (time.Duration, bool, error) {
	if err := ValidateRepoID(repoID); err != nil {
		return 0, false, err
	}
	if err := ValidateRefName(baseRef); err != nil {
		return 0, false, err
	}

	ageScript := fmt.Sprintf(`
set -eu
REPO_DIR="/workspaces/%s/repository.git"
STAMP="$REPO_DIR/atm-synced-at"
if [ ! -f "$STAMP" ] || ! git -C "$REPO_DIR" rev-parse -q --verify "$ATM_BASE_REF" >/dev/null 2>&1; then
    echo missing
    exit 0
fi
echo $(( $(date +%%s) - $(cat "$STAMP") ))
`, repoID)

	containerCfg := &container.Config{
		Image:        h.imageTag,
		User:         "1000:1000",
		AttachStdout: true,
		AttachStderr: true,
		Env:          []string{"ATM_BASE_REF=" + baseRef},
		Cmd:          []string{"/bin/sh", "-c", ageScript},
	}
	hostCfg := &container.HostConfig{
		NetworkMode:    "none",
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    defaultSecurityOpt(),
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeVolume,
				Source:   sandbox.ProjectDataVolumeName(repoID),
				Target:   "/workspaces",
				ReadOnly: true,
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
			},
		},
	}

	// A reply past the cap is cut short, so it fails to parse below and the caller falls back to the full fetch.
	out := &limitedBuffer{max: maxSyncCheckBytes}
	if err := h.runCapturedHelper(ctx, containerCfg, hostCfg, out, "sync check"); err != nil {
		return 0, false, err
	}
	reply := strings.TrimSpace(out.buf.String())
	if reply == "missing" {
		return 0, false, nil
	}
	seconds, err := strconv.ParseInt(reply, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("unexpected sync check output %q: %w", reply, err)
	}
	if seconds < 0 {
		seconds = 0
	}
	return time.Duration(seconds) * time.Second, true, nil
}

// ProvisionTaskWorktree adds the task's worktree from the base ref already in the project repository, without
// fetching anything. It mirrors the provisioning step of ApplyBundleAndProvisionWorktree.
func (h *volumeTransferHelper) ProvisionTaskWorktree(ctx context.Context, repoID, taskID, refName string) error {
	if err := ValidateRepoID(repoID); err != nil {
		return err
	}
	if err := ValidateTaskID(taskID); err != nil {
		return err
	}
	if err := ValidateRefName(refName); err != nil {
		return err
	}

	provisionScript := fmt.Sprintf(`
set -euo pipefail
REPO_DIR="/workspaces/%s/repository.git"
TASK_DIR="/workspaces/%s/tasks/%s"
mkdir -p "$(dirname "$TASK_DIR")"
rm -rf "$TASK_DIR"
git -C "$REPO_DIR" -c core.hooksPath=/dev/null worktree prune 2>/dev/null || true
git -C "$REPO_DIR" -c core.hooksPath=/dev/null branch -D "task/%s" 2>/dev/null || true
git -C "$REPO_DIR" -c core.hooksPath=/dev/null -c core.fsmonitor=false worktree add --no-checkout -b "task/%s" "$TASK_DIR" "%s"
if ! git -C "$REPO_DIR" -c core.hooksPath=/dev/null rev-parse -q --verify "refs/atm/base/%s" >/dev/null; then
    git -C "$REPO_DIR" -c core.hooksPath=/dev/null update-ref "refs/atm/base/%s" "%s"
fi
`, repoID, repoID, taskID, taskID, taskID, refName, taskID, taskID, refName)

	containerCfg := &container.Config{
		Image:        h.imageTag,
		User:         "1000:1000",
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"/bin/sh", "-c", provisionScript},
	}
	hostCfg := &container.HostConfig{
		NetworkMode:    "none",
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    defaultSecurityOpt(),
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: sandbox.ProjectDataVolumeName(repoID),
				Target: "/workspaces",
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
			},
		},
	}

	return h.runCapturedHelper(ctx, containerCfg, hostCfg, io.Discard, "worktree provision")
}

// runCapturedHelper runs a read-only helper container and copies its stdout to outStream. label names the
// operation in errors.
func (h *volumeTransferHelper) runCapturedHelper(
	ctx context.Context,
	containerCfg *container.Config,
	hostCfg *container.HostConfig,
	outStream io.Writer,
	label string,
) error {
	resp, err := h.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create %s helper container: %w", label, err)
	}
	defer removeHelperContainer(ctx, h.dockerCli, resp.ID)

	// Attach to capture stdout (the payload) and stderr (diagnostics)
	hijacked, err := h.dockerCli.ContainerAttach(ctx, resp.ID, container.AttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to %s container: %w", label, err)
	}
	defer hijacked.Close()

	if err := h.dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start %s helper: %w", label, err)
	}

	streamErrCh := make(chan error, 1)
	go func() {
		var outDst io.Writer = outStream
		if outDst == nil {
			outDst = io.Discard
		}
		// In raw Docker attach stream without TTY, stream is multiplexed with stdout/stderr
		// stdcopy.StdCopy demultiplexes into outDst, and stderr is discarded: it is never reported, and keeping it
		// would grow without bound. If plain reader, fallback to io.Copy.
		_, copyErr := stdcopy.StdCopy(outDst, io.Discard, hijacked.Reader)
		if copyErr != nil {
			// Fallback if raw un-multiplexed stream
			_, copyErr = io.Copy(outDst, hijacked.Reader)
		}
		streamErrCh <- copyErr
	}()

	statusCh, errCh := h.dockerCli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case waitErr, ok := <-errCh:
		if ok && waitErr != nil {
			return fmt.Errorf("%s container wait failed: %w", label, waitErr)
		}
	case status, ok := <-statusCh:
		if !ok {
			break
		}
		if status.StatusCode != 0 {
			return fmt.Errorf("%w: %s failed with exit code %d", ErrGitTransferFailed, label, status.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	if streamErr := <-streamErrCh; streamErr != nil {
		return fmt.Errorf("failed streaming %s output: %w", label, streamErr)
	}
	return nil
}
