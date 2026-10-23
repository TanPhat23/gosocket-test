//go:build live

package runner

import (
	"bufio"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"

	"agent-task-manager/internal/gateway"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/sandbox"
)

// Live probe for the assembled project runtime (ProjectRuntimeFromEnv) against a real Docker daemon behind the
// Gate 0D firewall: the real firewall proof, network/volume/container creation and secret delivery.
//
//	ATM_LIVE_RUNTIME_ENV   path to data/runtime/runtime.env written by scripts/runtime-dev-setup.sh
//	ATM_LIVE_RUNTIME_DIR   directory that holds the files runtime.env names under /etc/atm
//	ATM_LIVE_IMAGE         a local image to run as the project container (default atm-0g-sandbox:latest)
func liveRuntimeEnv(t *testing.T, override map[string]string) (func(string) string, func(string) ([]byte, error)) {
	t.Helper()
	path, dir := os.Getenv("ATM_LIVE_RUNTIME_ENV"), os.Getenv("ATM_LIVE_RUNTIME_DIR")
	if path == "" || dir == "" {
		t.Skip("ATM_LIVE_RUNTIME_ENV and ATM_LIVE_RUNTIME_DIR are required")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	env := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			env[k] = v
		}
	}
	for k, v := range override {
		env[k] = v
	}
	return func(k string) string { return env[k] }, func(p string) ([]byte, error) {
		return os.ReadFile(strings.Replace(p, "/etc/atm", dir, 1))
	}
}

func liveCleanup(t *testing.T, cli *dockerclient.Client, projectID string) {
	ctx := context.Background()
	_ = cli.ContainerRemove(ctx, "atm-project-"+projectID, container.RemoveOptions{Force: true})
	_ = cli.NetworkRemove(ctx, "atm-net-"+projectID)
	_ = cli.VolumeRemove(ctx, sandbox.ProjectDataVolumeName(projectID), true)
	_ = cli.VolumeRemove(ctx, "atm-project-state-"+projectID, true)
}

func liveRuntime(t *testing.T, override map[string]string) (*ProjectRuntime, *dockerclient.Client) {
	getenv, readFile := liveRuntimeEnv(t, override)
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	gw, err := gateway.ServiceFromEnv(getenv, readFile, t.Logf)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	rt, err := ProjectRuntimeFromEnv(getenv, readFile, cli, NewDefaultRuntimeCatalog(), gw, 15*time.Minute)
	if err != nil || rt == nil {
		t.Fatalf("runtime: %v", err)
	}
	return rt, cli
}

func TestLive_EnsureProjectContainer_ProvesFirewallThenProvisions(t *testing.T) {
	rt, cli := liveRuntime(t, nil)
	const pid = "live1"
	liveCleanup(t, cli, pid)
	defer liveCleanup(t, cli, pid)
	img := os.Getenv("ATM_LIVE_IMAGE")
	if img == "" {
		img = "atm-0g-sandbox:latest"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	route, err := rt.Manager.EnsureProjectContainer(ctx, &models.Project{ID: pid}, img)
	if err != nil || route == nil {
		t.Fatalf("EnsureProjectContainer: %v", err)
	}
	in, err := cli.ContainerInspect(ctx, "atm-project-"+pid)
	if err != nil || !in.State.Running {
		t.Fatalf("project container not running: %v", err)
	}
	if in.HostConfig.ReadonlyRootfs != true || len(in.HostConfig.CapAdd) != 0 || in.HostConfig.Privileged {
		t.Errorf("container is not hardened: %+v", in.HostConfig)
	}
	for _, e := range in.Config.Env {
		if strings.Contains(e, "sk-test-not-real") || strings.Contains(strings.ToLower(e), "token") && strings.Contains(e, "=") && len(e) > 60 {
			t.Errorf("secret-looking value in the container environment: %.20s...", e)
		}
	}
	if len(in.NetworkSettings.Networks) != 1 {
		t.Errorf("want exactly the project network, got %d networks", len(in.NetworkSettings.Networks))
	}
}

// A canary the firewall lets through (the proxy itself) must fail the proof and leave no container behind.
func TestLive_EnsureProjectContainer_OpenCanaryRefusesToProvision(t *testing.T) {
	rt, cli := liveRuntime(t, map[string]string{"ATM_EGRESS_CANARIES": "172.28.200.10:3128"})
	const pid = "live2"
	liveCleanup(t, cli, pid)
	defer liveCleanup(t, cli, pid)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	route, err := rt.Manager.EnsureProjectContainer(ctx, &models.Project{ID: pid}, "atm-0g-sandbox:latest")
	if route != nil || err == nil {
		t.Fatalf("want refusal, got %+v, %v", route, err)
	}
	if !errors.Is(err, sandbox.ErrEgressOpen) {
		t.Errorf("want ErrEgressOpen, got %v", err)
	}
	if _, ierr := cli.ContainerInspect(ctx, "atm-project-"+pid); ierr == nil {
		t.Error("a project container exists after a failed firewall proof")
	}
	_ = network.InspectOptions{}
}
