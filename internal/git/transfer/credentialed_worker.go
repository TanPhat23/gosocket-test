package transfer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/pkg/stdcopy"

	"agent-task-manager/internal/sandbox"
)

type credentialedGitWorker struct {
	dockerCli sandbox.DockerClient
	imageTag  string
}

// NewCredentialedGitWorker returns an instance of CredentialedWorker.
func NewCredentialedGitWorker(dockerCli sandbox.DockerClient, imageTag string) CredentialedWorker {
	return &credentialedGitWorker{
		dockerCli: dockerCli,
		imageTag:  imageTag,
	}
}

func (w *credentialedGitWorker) FetchRemoteRefToBundle(
	ctx context.Context,
	remoteURL, refName, token string,
	outStream io.Writer,
) error {
	if err := ValidateRefName(refName); err != nil {
		return err
	}
	if err := validateRemoteURL(remoteURL); err != nil {
		return err
	}

	branchName := strings.TrimPrefix(refName, "refs/heads/")

	// Transient in-container helper script: reads the token from stdin directly
	// into the git credential store, clones the single branch into /tmp/bare,
	// generates a git bundle streaming to stdout, and erases credentials.
	workerScript := fmt.Sprintf(`
set -euo pipefail

# Read token from stdin (in-memory pipe)
read -r INGESTED_TOKEN

# Configure git credentials in-memory for this session
git config --global credential.helper 'cache --timeout=30'
printf "protocol=https\nhost=github.com\nusername=x-access-token\npassword=%%s\n\n" "$INGESTED_TOKEN" | git credential approve
unset INGESTED_TOKEN

mkdir -p /tmp/bare.git
git init -q --bare /tmp/bare.git
git -C /tmp/bare.git -c core.hooksPath=/dev/null fetch --depth=1 "%s" "%s:%s"

# Generate bundle to stdout
git -C /tmp/bare.git -c core.hooksPath=/dev/null bundle create - "%s"

# Clean up credentials
printf "protocol=https\nhost=github.com\nusername=x-access-token\n\n" | git credential reject
`, remoteURL, refName, refName, branchName)

	containerCfg := &container.Config{
		Image:        w.imageTag,
		User:         "1000:1000",
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		StdinOnce:    true,
		// UID 1000 has no passwd entry in minimal images, so HOME would be "/" on a read-only rootfs and
		// `git config --global` would fail. /tmp is the writable tmpfs mount.
		Env: []string{"HOME=/tmp", "GIT_TERMINAL_PROMPT=0"},
		Cmd: []string{"/bin/sh", "-c", workerScript},
	}

	hostCfg := &container.HostConfig{
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    []string{"no-new-privileges:true"},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
				TmpfsOptions: &mount.TmpfsOptions{
					SizeBytes: 1024 * 1024 * 1024,
				},
			},
		},
	}

	resp, err := w.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create credentialed worker container: %w", err)
	}
	defer func() {
		_ = w.dockerCli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	// Attach to container stdin/stdout/stderr before ContainerStart
	hijacked, err := w.dockerCli.ContainerAttach(ctx, resp.ID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to credentialed worker container: %w", err)
	}
	defer hijacked.Close()

	if err := w.dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start credentialed worker: %w", err)
	}

	// 1. Write the token to the hijacked connection stdin: fmt.Fprintf(hijacked.Conn, "%s\n", token)
	// 2. Read bundle bytes from hijacked.Reader and pipe to destination outStream
	// 3. Close write half of the connection so the worker script receives EOF on stdin
	streamErrCh := make(chan error, 1)
	go func() {
		if _, err := fmt.Fprintf(hijacked.Conn, "%s\n", token); err != nil {
			_ = hijacked.CloseWrite()
			streamErrCh <- fmt.Errorf("failed writing token to worker stdin: %w", err)
			return
		}
		_ = hijacked.CloseWrite()

		var errBuf bytes.Buffer
		var outDst io.Writer = outStream
		if outDst == nil {
			outDst = io.Discard
		}
		_, copyErr := stdcopy.StdCopy(outDst, &errBuf, hijacked.Reader)
		if copyErr != nil {
			_, copyErr = io.Copy(outDst, hijacked.Reader)
		}
		streamErrCh <- copyErr
	}()

	statusCh, errCh := w.dockerCli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case waitErr := <-errCh:
		if waitErr != nil {
			return fmt.Errorf("worker wait failed: %w", waitErr)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("%w: worker exited with code %d", ErrGitTransferFailed, status.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	if streamErr := <-streamErrCh; streamErr != nil {
		return fmt.Errorf("failed streaming worker output bundle: %w", streamErr)
	}

	return nil
}

func (w *credentialedGitWorker) ImportBundleAndPush(
	ctx context.Context,
	remoteURL, taskID, token string,
	bundleStream io.Reader,
) error {
	if err := ValidateTaskID(taskID); err != nil {
		return err
	}
	if err := validateRemoteURL(remoteURL); err != nil {
		return err
	}

	branchRef := fmt.Sprintf("refs/heads/task/%s", taskID)

	pushScript := fmt.Sprintf(`
set -euo pipefail

read -r INGESTED_TOKEN
git config --global credential.helper 'cache --timeout=30'
printf "protocol=https\nhost=github.com\nusername=x-access-token\npassword=%%s\n\n" "$INGESTED_TOKEN" | git credential approve
unset INGESTED_TOKEN

# Spool bundle stream from stdin after the first line (token)
cat > /tmp/task.bundle

mkdir -p /tmp/staging.git
git init -q --bare /tmp/staging.git
git -C /tmp/staging.git -c core.hooksPath=/dev/null fetch /tmp/task.bundle "%s:%s"
rm -f /tmp/task.bundle

# Push to remote GitHub
git -C /tmp/staging.git -c core.hooksPath=/dev/null push --porcelain --atomic "%s" "%s:%s"

printf "protocol=https\nhost=github.com\nusername=x-access-token\n\n" | git credential reject
`, branchRef, branchRef, remoteURL, branchRef, branchRef)

	containerCfg := &container.Config{
		Image:        w.imageTag,
		User:         "1000:1000",
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		StdinOnce:    true,
		// UID 1000 has no passwd entry in minimal images, so HOME would be "/" on a read-only rootfs and
		// `git config --global` would fail. /tmp is the writable tmpfs mount.
		Env: []string{"HOME=/tmp", "GIT_TERMINAL_PROMPT=0"},
		Cmd: []string{"/bin/sh", "-c", pushScript},
	}

	hostCfg := &container.HostConfig{
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    []string{"no-new-privileges:true"},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
				TmpfsOptions: &mount.TmpfsOptions{
					SizeBytes: 1024 * 1024 * 1024,
				},
			},
		},
	}

	resp, err := w.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create worker push container: %w", err)
	}
	defer func() {
		_ = w.dockerCli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	// Attach to container stdin/stdout/stderr before ContainerStart
	hijacked, err := w.dockerCli.ContainerAttach(ctx, resp.ID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to worker push container: %w", err)
	}
	defer hijacked.Close()

	if err := w.dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start worker push container: %w", err)
	}

	// 1. Write the token on the first line: fmt.Fprintf(hijacked.Conn, "%s\n", token)
	// 2. Stream bundle bytes into hijacked.Conn
	// 3. Close write half so worker receives EOF on stdin
	streamErrCh := make(chan error, 1)
	go func() {
		defer hijacked.CloseWrite()
		if _, err := fmt.Fprintf(hijacked.Conn, "%s\n", token); err != nil {
			streamErrCh <- fmt.Errorf("failed writing token to worker push stdin: %w", err)
			return
		}
		if bundleStream != nil {
			if _, err := io.Copy(hijacked.Conn, bundleStream); err != nil {
				streamErrCh <- fmt.Errorf("failed piping bundle stream to worker push: %w", err)
				return
			}
		}
		streamErrCh <- nil
	}()

	statusCh, errCh := w.dockerCli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case streamErr := <-streamErrCh:
		if streamErr != nil {
			return streamErr
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case waitErr := <-errCh:
		if waitErr != nil {
			return fmt.Errorf("push container wait failed: %w", waitErr)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("%w: push failed with exit code %d", ErrPushRejected, status.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func validateRemoteURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("remote URL cannot be empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return fmt.Errorf("remote URL must be a valid https URL: %q", rawURL)
	}
	if u.User != nil {
		return fmt.Errorf("remote URL must not contain embedded user credentials")
	}
	return nil
}
