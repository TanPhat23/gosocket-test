package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/sandbox"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// rtDocker answers inspect, stop and logs for the project container by name.
type rtDocker struct {
	*sandbox.MockDockerClient
	inspect    types.ContainerJSON
	inspectErr error
	stopped    []string
	logs       string
}

func (d *rtDocker) ContainerInspect(context.Context, string) (types.ContainerJSON, error) {
	return d.inspect, d.inspectErr
}

func (d *rtDocker) ContainerStop(_ context.Context, id string, _ container.StopOptions) error {
	d.stopped = append(d.stopped, id)
	return nil
}

func (d *rtDocker) ContainerLogs(context.Context, string, container.LogsOptions) (io.ReadCloser, error) {
	var mux bytes.Buffer
	_, _ = stdcopy.NewStdWriter(&mux, stdcopy.Stdout).Write([]byte(d.logs))
	return io.NopCloser(&mux), nil
}

func runningContainer() types.ContainerJSON {
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			ID:    "0123456789abcdef0123",
			State: &types.ContainerState{Running: true, StartedAt: "2026-10-19T01:02:03.5Z"},
		},
		Config: &container.Config{Image: "atm-runtime:abc"},
	}
}

func newRuntimeManageRunner(d *rtDocker) *AgentTaskRunner {
	return &AgentTaskRunner{dockerCli: d, projectSandboxMgr: &fakeSandboxManager{}, projectImage: "img"}
}

func TestProjectRuntimeStatus_ReportsARunningContainer(t *testing.T) {
	r := newRuntimeManageRunner(&rtDocker{MockDockerClient: sandbox.NewMockDockerClient(), inspect: runningContainer()})

	info, err := r.ProjectRuntimeStatus(context.Background(), models.Project{ID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if info.State != RuntimeStateRunning || info.ContainerID != "0123456789ab" || info.Image != "atm-runtime:abc" || info.StartedAt == nil {
		t.Fatalf("info = %+v", info)
	}
}

func TestProjectRuntimeStatus_AbsentStoppedAndStarting(t *testing.T) {
	notFound := errors.New("Error response from daemon: No such container: atm-project-p1")
	absent := &rtDocker{MockDockerClient: sandbox.NewMockDockerClient(), inspectErr: notFound}
	r := newRuntimeManageRunner(absent)
	if info, err := r.ProjectRuntimeStatus(context.Background(), models.Project{ID: "p1"}); err != nil || info.State != RuntimeStateAbsent {
		t.Fatalf("absent: %+v, %v", info, err)
	}

	r.warming = map[string]bool{"p1": true}
	if info, _ := r.ProjectRuntimeStatus(context.Background(), models.Project{ID: "p1"}); info.State != RuntimeStateStarting {
		t.Fatalf("a start in flight must read as starting, got %q", info.State)
	}

	exited := runningContainer()
	exited.State.Running = false
	r = newRuntimeManageRunner(&rtDocker{MockDockerClient: sandbox.NewMockDockerClient(), inspect: exited})
	if info, _ := r.ProjectRuntimeStatus(context.Background(), models.Project{ID: "p1"}); info.State != RuntimeStateStopped {
		t.Fatalf("stopped: got %q", info.State)
	}

	if _, err := r.ProjectRuntimeStatus(context.Background(), models.Project{ID: "p1"}); err != nil {
		t.Fatal(err)
	}
	other := newRuntimeManageRunner(&rtDocker{MockDockerClient: sandbox.NewMockDockerClient(), inspectErr: errors.New("daemon down")})
	if _, err := other.ProjectRuntimeStatus(context.Background(), models.Project{ID: "p1"}); err == nil {
		t.Fatal("a daemon error must not read as absent")
	}
}

func TestStopProjectRuntime_StopsTheContainerByName(t *testing.T) {
	d := &rtDocker{MockDockerClient: sandbox.NewMockDockerClient()}
	r := newRuntimeManageRunner(d)

	if err := r.StopProjectRuntime(context.Background(), models.Project{ID: "p1"}); err != nil {
		t.Fatal(err)
	}
	if len(d.stopped) != 1 || d.stopped[0] != "atm-project-p1" {
		t.Fatalf("stopped = %v", d.stopped)
	}
}

func TestProjectRuntime_RefusesWithoutARuntime(t *testing.T) {
	r := &AgentTaskRunner{}
	p := models.Project{ID: "p1"}
	if _, err := r.ProjectRuntimeStatus(context.Background(), p); err != ErrRuntimeExecutionUnavailable {
		t.Fatalf("status: %v", err)
	}
	if err := r.StopProjectRuntime(context.Background(), p); err != ErrRuntimeExecutionUnavailable {
		t.Fatalf("stop: %v", err)
	}
	if _, err := r.ProjectRuntimeLogs(context.Background(), p, 10); err != ErrRuntimeExecutionUnavailable {
		t.Fatalf("logs: %v", err)
	}
}

func TestProjectRuntimeLogs_DemultiplexesTheStream(t *testing.T) {
	r := newRuntimeManageRunner(&rtDocker{MockDockerClient: sandbox.NewMockDockerClient(), logs: "2026-10-19T01:00:00Z server started\n"})

	got, err := r.ProjectRuntimeLogs(context.Background(), models.Project{ID: "p1"}, 50)
	if err != nil || !strings.Contains(got, "server started") || strings.Contains(got, "\x01") {
		t.Fatalf("logs = %q, %v", got, err)
	}
}
