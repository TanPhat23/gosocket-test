package sandbox

import (
	"bytes"
	"context"
	"io"
	"net"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// MockHijackedConn provides an in-memory bi-directional net.Conn for testing ContainerAttach.
type MockHijackedConn struct {
	InBuf  bytes.Buffer
	OutBuf bytes.Buffer
	Closed bool
}

func (m *MockHijackedConn) Read(b []byte) (n int, err error) {
	return m.OutBuf.Read(b)
}

func (m *MockHijackedConn) Write(b []byte) (n int, err error) {
	return m.InBuf.Write(b)
}

func (m *MockHijackedConn) Close() error {
	m.Closed = true
	return nil
}

func (m *MockHijackedConn) LocalAddr() net.Addr                { return nil }
func (m *MockHijackedConn) RemoteAddr() net.Addr               { return nil }
func (m *MockHijackedConn) SetDeadline(t time.Time) error      { return nil }
func (m *MockHijackedConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *MockHijackedConn) SetWriteDeadline(t time.Time) error { return nil }

// ContainerCreateCall records the arguments passed to ContainerCreate.
type ContainerCreateCall struct {
	Config           *container.Config
	HostConfig       *container.HostConfig
	NetworkingConfig *network.NetworkingConfig
	ContainerName    string
}

// MockDockerClient provides a fully-implemented DockerClient mock for testing container operations.
type MockDockerClient struct {
	CreateCalls    []ContainerCreateCall
	AttachCalls    []container.AttachOptions
	AttachConn     *MockHijackedConn
	AttachErr      error

	WaitStatusCode int64
	WaitErrMessage string
	WaitErr        error

	StartErr  error
	RemoveErr error
}

func NewMockDockerClient() *MockDockerClient {
	return &MockDockerClient{
		AttachConn: &MockHijackedConn{},
	}
}

func (m *MockDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	m.CreateCalls = append(m.CreateCalls, ContainerCreateCall{
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: networkingConfig,
		ContainerName:    containerName,
	})
	return container.CreateResponse{ID: "mock-container-id"}, nil
}

func (m *MockDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	return m.StartErr
}

func (m *MockDockerClient) ContainerAttach(ctx context.Context, containerID string, options container.AttachOptions) (types.HijackedResponse, error) {
	m.AttachCalls = append(m.AttachCalls, options)
	if m.AttachErr != nil {
		return types.HijackedResponse{}, m.AttachErr
	}
	if m.AttachConn == nil {
		m.AttachConn = &MockHijackedConn{}
	}
	return types.NewHijackedResponse(m.AttachConn, "application/vnd.docker.raw-stream"), nil
}

func (m *MockDockerClient) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	statusCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)

	var exitErr *container.WaitExitError
	if m.WaitErrMessage != "" {
		exitErr = &container.WaitExitError{Message: m.WaitErrMessage}
	}

	statusCh <- container.WaitResponse{
		StatusCode: m.WaitStatusCode,
		Error:      exitErr,
	}
	if m.WaitErr != nil {
		errCh <- m.WaitErr
	}
	return statusCh, errCh
}

func (m *MockDockerClient) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (m *MockDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	return nil
}

func (m *MockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	return m.RemoveErr
}

func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	return types.ContainerJSON{}, nil
}

func (m *MockDockerClient) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (m *MockDockerClient) ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	return types.ImageBuildResponse{Body: io.NopCloser(bytes.NewReader(nil))}, nil
}

func (m *MockDockerClient) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	return nil, nil
}

func (m *MockDockerClient) ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error) {
	return types.ImageInspect{}, nil, nil
}

func (m *MockDockerClient) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	return network.CreateResponse{}, nil
}

func (m *MockDockerClient) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	return network.Inspect{}, nil
}

func (m *MockDockerClient) NetworkRemove(ctx context.Context, networkID string) error {
	return nil
}

func (m *MockDockerClient) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	return volume.Volume{}, nil
}

func (m *MockDockerClient) VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error) {
	return volume.Volume{}, nil
}

func (m *MockDockerClient) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	return nil
}

func (m *MockDockerClient) CopyToContainer(ctx context.Context, containerID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error {
	return nil
}
