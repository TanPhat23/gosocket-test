package runner

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
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
)

// failingDockerClient is a sandbox.DockerClient whose every method fails the test.
// ContainerStop is the only exception, and only when allowStop is set (used to
// prove StopProjectContainer is unchanged). Every invocation increments calls.
type failingDockerClient struct {
	t         *testing.T
	calls     atomic.Int64
	allowStop bool
	stoppedID string
}

func (f *failingDockerClient) fail(method string) {
	f.calls.Add(1)
	f.t.Errorf("unexpected Docker call: %s", method)
}

func (f *failingDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	f.fail("ContainerCreate")
	return container.CreateResponse{}, errors.New("unexpected docker call")
}
func (f *failingDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	f.fail("ContainerStart")
	return errors.New("unexpected docker call")
}
func (f *failingDockerClient) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	f.fail("ContainerWait")
	return nil, nil
}
func (f *failingDockerClient) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	f.fail("ContainerLogs")
	return nil, errors.New("unexpected docker call")
}
func (f *failingDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	if f.allowStop {
		f.stoppedID = containerID
		return nil
	}
	f.fail("ContainerStop")
	return errors.New("unexpected docker call")
}
func (f *failingDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	f.fail("ContainerRemove")
	return errors.New("unexpected docker call")
}
func (f *failingDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	f.fail("ContainerInspect")
	return types.ContainerJSON{}, errors.New("unexpected docker call")
}
func (f *failingDockerClient) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	f.fail("ImagePull")
	return nil, errors.New("unexpected docker call")
}
func (f *failingDockerClient) ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	f.fail("ImageBuild")
	return types.ImageBuildResponse{}, errors.New("unexpected docker call")
}
func (f *failingDockerClient) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	f.fail("ImageList")
	return nil, errors.New("unexpected docker call")
}
func (f *failingDockerClient) ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error) {
	f.fail("ImageInspectWithRaw")
	return types.ImageInspect{}, nil, errors.New("unexpected docker call")
}
func (f *failingDockerClient) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	f.fail("NetworkCreate")
	return network.CreateResponse{}, errors.New("unexpected docker call")
}
func (f *failingDockerClient) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	f.fail("NetworkInspect")
	return network.Inspect{}, errors.New("unexpected docker call")
}
func (f *failingDockerClient) NetworkRemove(ctx context.Context, networkID string) error {
	f.fail("NetworkRemove")
	return errors.New("unexpected docker call")
}
func (f *failingDockerClient) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	f.fail("VolumeCreate")
	return volume.Volume{}, errors.New("unexpected docker call")
}
func (f *failingDockerClient) VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error) {
	f.fail("VolumeInspect")
	return volume.Volume{}, errors.New("unexpected docker call")
}
func (f *failingDockerClient) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	f.fail("VolumeRemove")
	return errors.New("unexpected docker call")
}
func (f *failingDockerClient) CopyToContainer(ctx context.Context, containerID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error {
	f.fail("CopyToContainer")
	return errors.New("unexpected docker call")
}
func (f *failingDockerClient) ContainerAttach(ctx context.Context, containerID string, options container.AttachOptions) (types.HijackedResponse, error) {
	f.fail("ContainerAttach")
	return types.HijackedResponse{}, errors.New("failingDockerClient: ContainerAttach")
}

func newQuarantineTestManager(t *testing.T, cli *failingDockerClient) *projectSandboxManager {
	t.Helper()
	mgr, ok := NewProjectSandboxManager(cli, nil, "").(*projectSandboxManager)
	if !ok {
		t.Fatalf("expected *projectSandboxManager")
	}
	return mgr
}

func TestEnsureProjectContainer_QuarantineBypassesCachedRoute(t *testing.T) {
	cli := &failingDockerClient{t: t}
	mgr := newQuarantineTestManager(t, cli)

	project := &models.Project{ID: "p-cached"}
	mgr.routes[project.ID] = &ProjectRouteHandle{
		ProjectID:    project.ID,
		ContainerID:  "cnt-cached",
		EndpointURL:  "http://127.0.0.1:4096",
		AuthPassword: "cached-secret",
	}

	handle, err := mgr.EnsureProjectContainer(context.Background(), project, "atm-project-runtime:test")
	if handle != nil {
		t.Errorf("expected nil handle, got %+v", handle)
	}
	if !errors.Is(err, ErrQuarantineActive) {
		t.Fatalf("expected ErrQuarantineActive, got %v", err)
	}
	if !errors.Is(err, common.ErrRuntimeQuarantined) {
		t.Errorf("expected error to wrap common.ErrRuntimeQuarantined, got %v", err)
	}
	if n := cli.calls.Load(); n != 0 {
		t.Errorf("expected zero Docker calls, got %d", n)
	}
}

func TestEnsureProjectContainer_QuarantineIsFirstEffect(t *testing.T) {
	cases := []struct {
		name     string
		project  *models.Project
		imageTag string
	}{
		{"nil project", nil, "atm-project-runtime:test"},
		{"empty project id", &models.Project{}, "atm-project-runtime:test"},
		{"empty image tag", &models.Project{ID: "p-1"}, ""},
		{"valid arguments", &models.Project{ID: "p-1"}, "atm-project-runtime:test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cli := &failingDockerClient{t: t}
			mgr := newQuarantineTestManager(t, cli)

			// Hold the write lock: if the refusal were not the first statement, a lock
			// acquisition would deadlock and the call would time out.
			mgr.mu.Lock()
			type result struct {
				handle *ProjectRouteHandle
				err    error
			}
			done := make(chan result, 1)
			go func() {
				h, err := mgr.EnsureProjectContainer(context.Background(), tc.project, tc.imageTag)
				done <- result{h, err}
			}()
			select {
			case r := <-done:
				if r.handle != nil {
					t.Errorf("expected nil handle, got %+v", r.handle)
				}
				if !errors.Is(r.err, ErrQuarantineActive) {
					t.Errorf("expected ErrQuarantineActive, got %v", r.err)
				}
			case <-time.After(2 * time.Second):
				t.Errorf("EnsureProjectContainer blocked on the lock; refusal is not the first statement")
			}
			mgr.mu.Unlock()

			if n := cli.calls.Load(); n != 0 {
				t.Errorf("expected zero Docker calls, got %d", n)
			}
			if len(mgr.routes) != 0 {
				t.Errorf("route cache must stay empty, got %d entries", len(mgr.routes))
			}
		})
	}
}

// StopProjectContainer and GetProjectRoute are intentionally unchanged by the quarantine.
func TestStopProjectContainer_UnchangedByQuarantine(t *testing.T) {
	cli := &failingDockerClient{t: t, allowStop: true}
	mgr := newQuarantineTestManager(t, cli)
	mgr.routes["p-stop"] = &ProjectRouteHandle{ProjectID: "p-stop", ContainerID: "cnt-stop"}

	if route, ok, err := mgr.GetProjectRoute(context.Background(), "p-stop"); err != nil || !ok || route.ContainerID != "cnt-stop" {
		t.Fatalf("GetProjectRoute unexpected result: route=%+v ok=%v err=%v", route, ok, err)
	}
	if err := mgr.StopProjectContainer(context.Background(), "p-stop", 5*time.Second); err != nil {
		t.Fatalf("StopProjectContainer failed: %v", err)
	}
	if cli.stoppedID != "cnt-stop" {
		t.Errorf("expected cnt-stop to be stopped, got %q", cli.stoppedID)
	}
	if _, ok, _ := mgr.GetProjectRoute(context.Background(), "p-stop"); ok {
		t.Errorf("expected route to be invalidated")
	}
	if n := cli.calls.Load(); n != 0 {
		t.Errorf("expected zero disallowed Docker calls, got %d", n)
	}
}

func (f *failingDockerClient) ContainerExecCreate(ctx context.Context, containerID string, options container.ExecOptions) (types.IDResponse, error) {
	f.fail("ContainerExecCreate")
	return types.IDResponse{}, errors.New("unexpected docker call")
}
func (f *failingDockerClient) ContainerExecAttach(ctx context.Context, execID string, config container.ExecAttachOptions) (types.HijackedResponse, error) {
	f.fail("ContainerExecAttach")
	return types.HijackedResponse{}, errors.New("unexpected docker call")
}
func (f *failingDockerClient) ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error) {
	f.fail("ContainerExecInspect")
	return container.ExecInspect{}, errors.New("unexpected docker call")
}
