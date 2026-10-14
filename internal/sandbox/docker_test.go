package sandbox_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"agent-task-manager/internal/sandbox"
)

type mockDockerClient struct {
	createdConfig     *container.Config
	createdHostConfig *container.HostConfig
	createResp        container.CreateResponse
	createErr         error

	startedID  string
	startOpts  container.StartOptions
	startErr   error

	waitStatusCh chan container.WaitResponse
	waitErrCh    chan error

	logsReader io.ReadCloser
	logsErr    error

	stoppedID string
	stopErr   error

	removedID string
	removeErr error
}

func (m *mockDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	m.createdConfig = config
	m.createdHostConfig = hostConfig
	return m.createResp, m.createErr
}

func (m *mockDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	m.startedID = containerID
	m.startOpts = options
	return m.startErr
}

func (m *mockDockerClient) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	return m.waitStatusCh, m.waitErrCh
}

func (m *mockDockerClient) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	return m.logsReader, m.logsErr
}

func (m *mockDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	m.stoppedID = containerID
	return m.stopErr
}

func (m *mockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	m.removedID = containerID
	return m.removeErr
}

func (m *mockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	return types.ContainerJSON{}, nil
}

func (m *mockDockerClient) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockDockerClient) ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	return types.ImageBuildResponse{Body: io.NopCloser(strings.NewReader(""))}, nil
}

func (m *mockDockerClient) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	return nil, nil
}

func (m *mockDockerClient) ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error) {
	return types.ImageInspect{}, nil, nil
}

func (m *mockDockerClient) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	return network.CreateResponse{}, nil
}

func (m *mockDockerClient) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	return network.Inspect{}, nil
}

func (m *mockDockerClient) NetworkRemove(ctx context.Context, networkID string) error {
	return nil
}

func (m *mockDockerClient) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	return volume.Volume{}, nil
}

func (m *mockDockerClient) VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error) {
	return volume.Volume{}, nil
}

func (m *mockDockerClient) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	return nil
}

func (m *mockDockerClient) CopyToContainer(ctx context.Context, containerID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error {
	return nil
}

func TestSecurityHardeningDefaults(t *testing.T) {
	cfg := sandbox.NewDefaultHostConfig("/srv/agent-tasks/worktrees/task-1")

	if len(cfg.CapDrop) != 1 || cfg.CapDrop[0] != "ALL" {
		t.Errorf("expected CapDrop ALL, got: %v", cfg.CapDrop)
	}
	if len(cfg.SecurityOpt) != 1 || cfg.SecurityOpt[0] != "no-new-privileges:true" {
		t.Errorf("expected no-new-privileges:true, got: %v", cfg.SecurityOpt)
	}
	if string(cfg.NetworkMode) != "bridge" {
		t.Errorf("expected isolated network bridge, got: %s", cfg.NetworkMode)
	}
	if cfg.Resources.Memory != 4*1024*1024*1024 {
		t.Errorf("expected 4GB memory limit, got: %d", cfg.Resources.Memory)
	}
	if cfg.Resources.NanoCPUs != 2*1e9 {
		t.Errorf("expected 2 NanoCPUs, got: %d", cfg.Resources.NanoCPUs)
	}
	expectedBind := "/srv/agent-tasks/worktrees/task-1:/workspace:rw"
	if len(cfg.Binds) != 1 || cfg.Binds[0] != expectedBind {
		t.Errorf("expected Binds [%s], got: %v", expectedBind, cfg.Binds)
	}
}

func TestRunner_StartSuccess(t *testing.T) {
	mockCli := &mockDockerClient{
		createResp: container.CreateResponse{ID: "c-12345"},
	}

	runner := sandbox.NewRunner(mockCli)
	cfg := sandbox.SandboxConfig{
		Image:        "golang:1.24-alpine",
		Command:      []string{"go", "test", "./..."},
		Env:          []string{"FOO=BAR"},
		WorktreeBind: "/host/worktrees/task-1:/workspace:rw",
		WorkingDir:   "/workspace",
	}

	inst, err := runner.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if inst.ID != "c-12345" {
		t.Fatalf("expected container ID c-12345, got: %s", inst.ID)
	}

	// Verify host config hardening in created container
	if mockCli.createdHostConfig == nil {
		t.Fatal("expected hostConfig to be passed to ContainerCreate")
	}
	if len(mockCli.createdHostConfig.CapDrop) != 1 || mockCli.createdHostConfig.CapDrop[0] != "ALL" {
		t.Errorf("expected CapDrop ALL, got: %v", mockCli.createdHostConfig.CapDrop)
	}
	if len(mockCli.createdHostConfig.SecurityOpt) != 1 || mockCli.createdHostConfig.SecurityOpt[0] != "no-new-privileges:true" {
		t.Errorf("expected no-new-privileges:true, got: %v", mockCli.createdHostConfig.SecurityOpt)
	}
	if mockCli.createdHostConfig.Resources.Memory != 4*1024*1024*1024 {
		t.Errorf("expected 4GB memory, got: %d", mockCli.createdHostConfig.Resources.Memory)
	}
	if mockCli.createdHostConfig.Resources.NanoCPUs != 2*1e9 {
		t.Errorf("expected 2 NanoCPUs, got: %d", mockCli.createdHostConfig.Resources.NanoCPUs)
	}
	if mockCli.startedID != "c-12345" {
		t.Errorf("expected started container c-12345, got: %s", mockCli.startedID)
	}
}

func TestRunner_StartCustomOverrides(t *testing.T) {
	mockCli := &mockDockerClient{
		createResp: container.CreateResponse{ID: "c-custom"},
	}

	runner := sandbox.NewRunner(mockCli)
	cfg := sandbox.SandboxConfig{
		Image:        "alpine:latest",
		WorktreeBind: "/custom/path",
		MemoryLimit:  1024 * 1024 * 1024,
		CPULimit:     1e9,
		NetworkMode:  "none",
	}

	_, err := runner.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mockCli.createdHostConfig.Resources.Memory != 1024*1024*1024 {
		t.Errorf("expected custom memory limit, got: %d", mockCli.createdHostConfig.Resources.Memory)
	}
	if mockCli.createdHostConfig.Resources.NanoCPUs != 1e9 {
		t.Errorf("expected custom CPU limit, got: %d", mockCli.createdHostConfig.Resources.NanoCPUs)
	}
	if string(mockCli.createdHostConfig.NetworkMode) != "none" {
		t.Errorf("expected network mode none, got: %s", mockCli.createdHostConfig.NetworkMode)
	}
	if len(mockCli.createdHostConfig.Binds) != 1 || mockCli.createdHostConfig.Binds[0] != "/custom/path:/workspace:rw" {
		t.Errorf("expected formatted bind, got: %v", mockCli.createdHostConfig.Binds)
	}
}

func TestRunner_StartMissingImage(t *testing.T) {
	mockCli := &mockDockerClient{}
	runner := sandbox.NewRunner(mockCli)

	_, err := runner.Start(context.Background(), sandbox.SandboxConfig{})
	if err == nil {
		t.Fatal("expected error when Image is empty, got nil")
	}
}

func TestRunner_StartCreateFailure(t *testing.T) {
	mockCli := &mockDockerClient{
		createErr: errors.New("daemon connection failed"),
	}
	runner := sandbox.NewRunner(mockCli)

	_, err := runner.Start(context.Background(), sandbox.SandboxConfig{Image: "alpine"})
	if err == nil {
		t.Fatal("expected error on Create failure, got nil")
	}
}

func TestRunner_StartStartFailure(t *testing.T) {
	mockCli := &mockDockerClient{
		createResp: container.CreateResponse{ID: "c-start-fail"},
		startErr:   errors.New("failed to start container"),
	}
	runner := sandbox.NewRunner(mockCli)

	_, err := runner.Start(context.Background(), sandbox.SandboxConfig{Image: "alpine"})
	if err == nil {
		t.Fatal("expected error on Start failure, got nil")
	}
}

func TestRunner_WaitSuccess(t *testing.T) {
	statusCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)
	statusCh <- container.WaitResponse{StatusCode: 0}

	mockCli := &mockDockerClient{
		waitStatusCh: statusCh,
		waitErrCh:    errCh,
	}
	runner := sandbox.NewRunner(mockCli)

	exitCode, err := runner.Wait(context.Background(), "c-wait")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got: %d", exitCode)
	}
}

func TestRunner_WaitErrorExit(t *testing.T) {
	statusCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)
	statusCh <- container.WaitResponse{StatusCode: 137}

	mockCli := &mockDockerClient{
		waitStatusCh: statusCh,
		waitErrCh:    errCh,
	}
	runner := sandbox.NewRunner(mockCli)

	exitCode, err := runner.Wait(context.Background(), "c-wait-fail")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 137 {
		t.Errorf("expected exit code 137, got: %d", exitCode)
	}
}

func TestRunner_WaitWaitError(t *testing.T) {
	statusCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)
	errCh <- errors.New("docker daemon error")

	mockCli := &mockDockerClient{
		waitStatusCh: statusCh,
		waitErrCh:    errCh,
	}
	runner := sandbox.NewRunner(mockCli)

	_, err := runner.Wait(context.Background(), "c-wait-err")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRunner_WaitContextCancelled(t *testing.T) {
	statusCh := make(chan container.WaitResponse)
	errCh := make(chan error)

	mockCli := &mockDockerClient{
		waitStatusCh: statusCh,
		waitErrCh:    errCh,
	}
	runner := sandbox.NewRunner(mockCli)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := runner.Wait(ctx, "c-wait-cancelled")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestRunner_Logs(t *testing.T) {
	mockLogs := io.NopCloser(bytes.NewBufferString("container output logs\n"))
	mockCli := &mockDockerClient{
		logsReader: mockLogs,
	}
	runner := sandbox.NewRunner(mockCli)

	logs, err := runner.Logs(context.Background(), "c-logs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logs.Close()

	buf := new(bytes.Buffer)
	_, _ = io.Copy(buf, logs)
	if !strings.Contains(buf.String(), "container output logs") {
		t.Errorf("expected container logs, got: %s", buf.String())
	}
}

func TestRunner_Stop(t *testing.T) {
	mockCli := &mockDockerClient{}
	runner := sandbox.NewRunner(mockCli)

	err := runner.Stop(context.Background(), "c-stop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mockCli.stoppedID != "c-stop" {
		t.Errorf("expected stoppedID c-stop, got: %s", mockCli.stoppedID)
	}
}

func TestRunner_StopTimeout(t *testing.T) {
	mockCli := &mockDockerClient{
		stopErr: errors.New("timeout stopping container"),
	}
	runner := sandbox.NewRunner(mockCli)

	err := runner.Stop(context.Background(), "c-stop-timeout")
	if err == nil {
		t.Fatal("expected error stopping container, got nil")
	}
}
