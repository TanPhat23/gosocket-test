//go:build live

package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	dockerclient "github.com/docker/docker/client"
)

// Live probe of VerifyEgressFirewall against a real daemon. ATM_LIVE_IMAGE needs busybox nc (the
// deploy/git-transfer image). The "firewall" here is simply whether a listener exists, which exercises
// the probe script and exit-code mapping, not an nftables ruleset.
func TestLiveVerifyEgressFirewall(t *testing.T) {
	image := os.Getenv("ATM_LIVE_IMAGE")
	if image == "" {
		t.Skip("ATM_LIVE_IMAGE not set")
	}
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	ip := func(name string) string {
		return run("inspect", "-f", "{{.NetworkSettings.Networks.bridge.IPAddress}}", name)
	}
	for _, n := range []string{"atmvctl", "atmvcan"} {
		_ = exec.Command("docker", "rm", "-f", n).Run()
	}
	run("run", "-d", "--name", "atmvctl", image, "sh", "-c", "while true; do nc -l -p 3128; done")
	run("run", "-d", "--name", "atmvcan", image, "sh", "-c", "while true; do nc -l -p 2222; done")
	// 2223 on a live host refuses at once, which must now fail. A dropped canary is modelled by an
	// unused address on the same subnet: nothing answers ARP, so the connect hangs until the timeout.
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", "atmvctl", "atmvcan").Run() })
	ctl, can := ip("atmvctl"), ip("atmvcan")
	cfg := func(control string, canaries ...string) EgressVerifyConfig {
		return EgressVerifyConfig{Image: image, Network: "bridge", Control: control, Canaries: canaries}
	}
	ctx := context.Background()
	silent := run("network", "inspect", "-f", "{{(index .IPAM.Config 0).Subnet}}", "bridge")
	silent = strings.TrimSuffix(strings.Split(silent, "/")[0], ".0") + ".250"
	if err := VerifyEgressFirewall(ctx, cli, cfg(ctl+":3128", silent+":2223")); err != nil {
		t.Errorf("control up, canary silent: want pass, got %v", err)
	}
	if err := VerifyEgressFirewall(ctx, cli, cfg(ctl+":3128", can+":2223")); !errors.Is(err, ErrEgressCanaryAnswered) {
		t.Errorf("a refusing canary must fail with ErrEgressCanaryAnswered, got %v", err)
	}
	if err := VerifyEgressFirewall(ctx, cli, cfg(ctl+":3128", silent+":2223", can+":2222")); !errors.Is(err, ErrEgressOpen) {
		t.Errorf("a reachable canary must fail with ErrEgressOpen, got %v", err)
	}
	if err := VerifyEgressFirewall(ctx, cli, cfg(ctl+":3129", silent+":2223")); !errors.Is(err, ErrEgressControlUnreachable) {
		t.Errorf("unreachable control must fail with ErrEgressControlUnreachable, got %v", err)
	}
}
