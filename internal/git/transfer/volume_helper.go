package transfer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

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

type volumeTransferHelper struct {
	dockerCli sandbox.DockerClient
	imageTag  string
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
    git -C "$REPO_DIR" -c core.hooksPath=/dev/null -c core.fsmonitor=false fetch "$TMP_BUNDLE" "%s:%s"
fi
rm -f "$TMP_BUNDLE"

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
`, repoID, repoID, taskID, ExitCodeVolumeNotWritable, refName, refName, branchName, ExitCodeSubmoduleDetected, branchName, ExitCodeSubmoduleDetected, branchName, ExitCodeLFSDetected, branchName, ExitCodeLFSDetected, taskID, taskID, branchName)

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
	defer func() {
		_ = h.dockerCli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

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

	resp, err := h.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create export helper container: %w", err)
	}
	defer func() {
		_ = h.dockerCli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	// Attach to capture stdout (bundle export) and stderr (diagnostics)
	hijacked, err := h.dockerCli.ContainerAttach(ctx, resp.ID, container.AttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to export container: %w", err)
	}
	defer hijacked.Close()

	if err := h.dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start export helper: %w", err)
	}

	// Read bundle bytes from hijacked.Reader and pipe to destination outStream
	streamErrCh := make(chan error, 1)
	go func() {
		var errBuf bytes.Buffer
		var outDst io.Writer = outStream
		if outDst == nil {
			outDst = io.Discard
		}
		// In raw Docker attach stream without TTY, stream is multiplexed with stdout/stderr
		// stdcopy.StdCopy demultiplexes into outDst and errBuf. If plain reader, fallback to io.Copy.
		_, copyErr := stdcopy.StdCopy(outDst, &errBuf, hijacked.Reader)
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
			return fmt.Errorf("export container wait failed: %w", waitErr)
		}
	case status, ok := <-statusCh:
		if !ok {
			break
		}
		if status.StatusCode != 0 {
			return fmt.Errorf("%w: export failed with exit code %d", ErrGitTransferFailed, status.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	if streamErr := <-streamErrCh; streamErr != nil {
		return fmt.Errorf("failed streaming exported bundle: %w", streamErr)
	}

	return nil
}
