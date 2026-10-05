package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// DockerClient abstracts the required Docker Engine client operations for mocking in tests.
type DockerClient interface {
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error)
	ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
}

// NewDefaultHostConfig creates a security-hardened HostConfig for sandboxes.
// Dropping ALL capabilities, disallowing privilege escalation, and restricting resources.
func NewDefaultHostConfig(worktreeHostPath string) *container.HostConfig {
	bindPath := worktreeHostPath
	if !strings.Contains(bindPath, ":") {
		bindPath = fmt.Sprintf("%s:/workspace:rw", worktreeHostPath)
	}

	return &container.HostConfig{
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges:true"},
		NetworkMode: container.NetworkMode("bridge"),
		Binds:       []string{bindPath},
		Resources: container.Resources{
			Memory:   4 * 1024 * 1024 * 1024,
			NanoCPUs: 2 * 1e9,
		},
	}
}

// Runner manages the lifecycle of sandboxed Docker containers.
type Runner struct {
	cli DockerClient
}

// NewRunner returns a new Runner backed by the provided DockerClient.
func NewRunner(cli DockerClient) *Runner {
	return &Runner{cli: cli}
}

// Start creates and starts a sandboxed container according to cfg.
func (r *Runner) Start(ctx context.Context, cfg SandboxConfig) (*ContainerInstance, error) {
	if cfg.Image == "" {
		return nil, errors.New("sandbox image is required")
	}

	workingDir := cfg.WorkingDir
	if workingDir == "" {
		workingDir = "/workspace"
	}

	containerCfg := &container.Config{
		Image:        cfg.Image,
		Cmd:          cfg.Command,
		Env:          cfg.Env,
		WorkingDir:   workingDir,
		AttachStdout: true,
		AttachStderr: true,
	}

	hostCfg := NewDefaultHostConfig(cfg.WorktreeBind)
	if cfg.MemoryLimit > 0 {
		hostCfg.Resources.Memory = cfg.MemoryLimit
	}
	if cfg.CPULimit > 0 {
		hostCfg.Resources.NanoCPUs = cfg.CPULimit
	}
	if cfg.NetworkMode != "" {
		hostCfg.NetworkMode = container.NetworkMode(cfg.NetworkMode)
	}

	resp, err := r.cli.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	if err := r.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	return &ContainerInstance{
		ID: resp.ID,
	}, nil
}

// Wait blocks until the specified container exits and returns its exit code.
func (r *Runner) Wait(ctx context.Context, containerID string) (int64, error) {
	statusCh, errCh := r.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return -1, fmt.Errorf("error waiting for container: %w", err)
		}
	case status := <-statusCh:
		if status.Error != nil {
			return status.StatusCode, errors.New(status.Error.Message)
		}
		return status.StatusCode, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
	return -1, errors.New("container wait completed without status or error")
}

// Logs returns a read closer for the container's stdout/stderr stream.
func (r *Runner) Logs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	}
	return r.cli.ContainerLogs(ctx, containerID, opts)
}

// Stop stops the container gracefully with a timeout.
func (r *Runner) Stop(ctx context.Context, containerID string) error {
	return r.cli.ContainerStop(ctx, containerID, container.StopOptions{})
}
