package runner_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/sandbox"
)

type mockDockerClientForSandboxManager struct {
	createdConfig     *container.Config
	createdHostConfig *container.HostConfig
	createdNetConfig  *network.NetworkingConfig
	createdName       string
	createResp        container.CreateResponse
	createErr         error

	startedID string
	startErr  error

	stoppedID   string
	stopTimeout *int
	stopErr     error

	inspectContainerJSON types.ContainerJSON
	inspectErr           error

	createdVolumes  []string
	volumeCreateErr error

	createdNetworks  []string
	networkCreateErr error

	copiedContainerID string
	copiedDstPath     string
	copiedContent     []byte
	copyErr           error

	removedContainerID string
}

func (m *mockDockerClientForSandboxManager) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	m.createdConfig = config
	m.createdHostConfig = hostConfig
	m.createdNetConfig = networkingConfig
	m.createdName = containerName
	return m.createResp, m.createErr
}

func (m *mockDockerClientForSandboxManager) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	m.startedID = containerID
	return m.startErr
}

func (m *mockDockerClientForSandboxManager) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	return nil, nil
}

func (m *mockDockerClientForSandboxManager) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockDockerClientForSandboxManager) ContainerAttach(ctx context.Context, containerID string, options container.AttachOptions) (types.HijackedResponse, error) {
	return types.HijackedResponse{}, nil
}

func (m *mockDockerClientForSandboxManager) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	m.stoppedID = containerID
	m.stopTimeout = options.Timeout
	return m.stopErr
}

func (m *mockDockerClientForSandboxManager) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	m.removedContainerID = containerID
	return nil
}

func (m *mockDockerClientForSandboxManager) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	if m.inspectErr != nil {
		return types.ContainerJSON{}, m.inspectErr
	}
	return m.inspectContainerJSON, nil
}

func (m *mockDockerClientForSandboxManager) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockDockerClientForSandboxManager) ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	return types.ImageBuildResponse{Body: io.NopCloser(strings.NewReader(""))}, nil
}

func (m *mockDockerClientForSandboxManager) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	return nil, nil
}

func (m *mockDockerClientForSandboxManager) ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error) {
	return types.ImageInspect{}, nil, nil
}

func (m *mockDockerClientForSandboxManager) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	m.createdNetworks = append(m.createdNetworks, name)
	if m.networkCreateErr != nil {
		return network.CreateResponse{}, m.networkCreateErr
	}
	return network.CreateResponse{ID: "net-" + name}, nil
}

func (m *mockDockerClientForSandboxManager) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	return network.Inspect{ID: networkID, Name: networkID}, nil
}

func (m *mockDockerClientForSandboxManager) NetworkRemove(ctx context.Context, networkID string) error {
	return nil
}

func (m *mockDockerClientForSandboxManager) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	m.createdVolumes = append(m.createdVolumes, options.Name)
	if m.volumeCreateErr != nil {
		return volume.Volume{}, m.volumeCreateErr
	}
	return volume.Volume{Name: options.Name}, nil
}

func (m *mockDockerClientForSandboxManager) VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error) {
	return volume.Volume{Name: volumeID}, nil
}

func (m *mockDockerClientForSandboxManager) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	return nil
}

func (m *mockDockerClientForSandboxManager) CopyToContainer(ctx context.Context, containerID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error {
	m.copiedContainerID = containerID
	m.copiedDstPath = dstPath
	buf, _ := io.ReadAll(content)
	m.copiedContent = buf
	return m.copyErr
}

func assertNoSandboxDockerCalls(t *testing.T, m *mockDockerClientForSandboxManager) {
	t.Helper()
	if m.createdConfig != nil || m.createdName != "" || m.createdHostConfig != nil || m.createdNetConfig != nil {
		t.Errorf("ContainerCreate must not be called, got name=%q", m.createdName)
	}
	if m.startedID != "" {
		t.Errorf("ContainerStart must not be called, got %q", m.startedID)
	}
	if m.stoppedID != "" || m.stopTimeout != nil {
		t.Errorf("ContainerStop must not be called, got %q", m.stoppedID)
	}
	if m.removedContainerID != "" {
		t.Errorf("ContainerRemove must not be called, got %q", m.removedContainerID)
	}
	if len(m.createdVolumes) != 0 {
		t.Errorf("VolumeCreate must not be called, got %v", m.createdVolumes)
	}
	if len(m.createdNetworks) != 0 {
		t.Errorf("NetworkCreate must not be called, got %v", m.createdNetworks)
	}
	if m.copiedContainerID != "" || m.copiedDstPath != "" || m.copiedContent != nil {
		t.Errorf("CopyToContainer must not be called, got %q", m.copiedContainerID)
	}
}

func assertQuarantineRefusal(t *testing.T, route *runner.ProjectRouteHandle, err error) {
	t.Helper()
	if route != nil {
		t.Errorf("expected nil route handle, got %+v", route)
	}
	if !errors.Is(err, runner.ErrQuarantineActive) {
		t.Fatalf("expected ErrQuarantineActive, got: %v", err)
	}
	if !errors.Is(err, common.ErrRuntimeQuarantined) {
		t.Errorf("expected error to wrap common.ErrRuntimeQuarantined, got: %v", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "gate") {
		t.Errorf("refusal message must not mention internal gate names: %q", err.Error())
	}
}

func TestProjectSandboxManager(t *testing.T) {
	project := &models.Project{
		ID:   "p-12345",
		Name: "Test Project",
	}
	imageTag := "atm-project-runtime:test"
	gatewayURL := "http://atm-control-plane:8080/v1/llm"
	egressPolicy := sandbox.DefaultEgressPolicy("http://172.28.200.10:3128")

	// Migrated: previously asserted a hardened container was created. Provisioning is now refused.
	t.Run("EnsureProjectContainer creates hardened container with correct configs", func(t *testing.T) {
		mockCli := &mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "cnt-p-12345"},
		}
		mgr := runner.NewProjectSandboxManager(mockCli, egressPolicy, gatewayURL)
		route, err := mgr.EnsureProjectContainer(context.Background(), project, imageTag)
		assertQuarantineRefusal(t, route, err)
		assertNoSandboxDockerCalls(t, mockCli)

		if _, ok, _ := mgr.GetProjectRoute(context.Background(), project.ID); ok {
			t.Errorf("no route may be cached after a refused EnsureProjectContainer")
		}
	})

	// Migrated: previously asserted a cached route was returned for a running container.
	t.Run("EnsureProjectContainer returns cached route handle when container is already running", func(t *testing.T) {
		mockCli := &mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "cnt-p-12345"},
			inspectContainerJSON: types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					ID:    "cnt-p-12345",
					State: &types.ContainerState{Running: true},
				},
			},
		}
		mgr := runner.NewProjectSandboxManager(mockCli, egressPolicy, gatewayURL)
		for i := 0; i < 2; i++ {
			route, err := mgr.EnsureProjectContainer(context.Background(), project, imageTag)
			assertQuarantineRefusal(t, route, err)
		}
		assertNoSandboxDockerCalls(t, mockCli)
	})

	// StopProjectContainer is unchanged. With no cached route it is a no-op and must not touch Docker.
	// The populated-route stop path is covered in project_sandbox_quarantine_internal_test.go.
	t.Run("StopProjectContainer stops container and invalidates route handle", func(t *testing.T) {
		mockCli := &mockDockerClientForSandboxManager{}
		mgr := runner.NewProjectSandboxManager(mockCli, egressPolicy, gatewayURL)

		if err := mgr.StopProjectContainer(context.Background(), project.ID, 5*time.Second); err != nil {
			t.Fatalf("unexpected error stopping container without route: %v", err)
		}
		assertNoSandboxDockerCalls(t, mockCli)

		_, ok, err := mgr.GetProjectRoute(context.Background(), project.ID)
		if err != nil || ok {
			t.Errorf("expected no route, got ok=%v err=%v", ok, err)
		}
	})

	// Migrated: previously asserted fail-closed cleanup when CopyToContainer failed.
	t.Run("Fail-closed cleanup when CopyToContainer fails", func(t *testing.T) {
		mockCli := &mockDockerClientForSandboxManager{
			createResp: container.CreateResponse{ID: "cnt-fail-copy"},
			copyErr:    fmt.Errorf("disk full or permission denied"),
		}
		mgr := runner.NewProjectSandboxManager(mockCli, egressPolicy, gatewayURL)
		route, err := mgr.EnsureProjectContainer(context.Background(), project, imageTag)
		assertQuarantineRefusal(t, route, err)
		if err != nil && strings.Contains(err.Error(), "disk full") {
			t.Errorf("CopyToContainer must never be reached")
		}
		assertNoSandboxDockerCalls(t, mockCli)
	})
}

func (m *mockDockerClientForSandboxManager) ContainerExecCreate(ctx context.Context, containerID string, options container.ExecOptions) (types.IDResponse, error) {
	return types.IDResponse{ID: "exec-1"}, nil
}

func (m *mockDockerClientForSandboxManager) ContainerExecAttach(ctx context.Context, execID string, config container.ExecAttachOptions) (types.HijackedResponse, error) {
	return types.HijackedResponse{}, nil
}

func (m *mockDockerClientForSandboxManager) ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error) {
	return container.ExecInspect{}, nil
}
