package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// Gate 0D fail-closed contract (decision 3, canary probe): before sandboxes may start, ATM runs a
// throwaway container on the sandbox network and checks, from the sandbox's point of view, that the
// operator's firewall really blocks. The probe needs no root and no capabilities. Any outcome other
// than "control reachable and every canary blocked" is an error, so a broken probe never passes.

var (
	// ErrEgressOpen means a canary was reachable from a sandbox: the firewall is missing or too loose.
	ErrEgressOpen = errors.New("egress firewall verification failed: a canary address is reachable from the sandbox network")
	// ErrEgressCanaryAnswered means a canary refused or rejected the connection quickly instead of
	// timing out. The packet reached something that answered, so no firewall dropped it; a missing
	// firewall in front of a port with no listener would otherwise look "blocked".
	ErrEgressCanaryAnswered = errors.New("egress firewall verification failed: a canary address answered (refused or rejected) instead of dropping the connection")
	// ErrEgressControlUnreachable means the control (the egress proxy) was not reachable, so the probe
	// cannot tell "blocked" from "no network at all".
	ErrEgressControlUnreachable = errors.New("egress firewall verification failed: the egress proxy is not reachable from the sandbox network")
)

const (
	probeExitControlUnreachable = 3
	probeExitCanaryReachable    = 10
	probeExitCanaryAnswered     = 11
)

// probeScript takes the control as $1 and the canaries as $2.., each "ipv4:port". Addresses are
// positional arguments, never interpolated into the script.
const probeScript = `command -v nc >/dev/null 2>&1 || exit 2
nc -w3 -z "${1%:*}" "${1##*:}" || exit 3
shift
for c in "$@"; do
  s=$(date +%s)
  if nc -w3 -z "${c%:*}" "${c##*:}"; then exit 10; fi
  e=$(date +%s)
  [ $((e - s)) -ge 2 ] || exit 11
done
exit 0`

// EgressVerifyConfig describes one verification run.
type EgressVerifyConfig struct {
	Image    string   // digest-pinned image with busybox nc (deploy/git-transfer/Dockerfile)
	Network  string   // the sandbox network the probe joins
	Control  string   // ipv4:port that MUST be reachable (the egress proxy)
	Canaries []string // ipv4:port that MUST be dropped (host services, LAN, WAN fixture); a fast refusal also fails
	Timeout  time.Duration
}

func validEndpoint(s string) bool {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return false
	}
	if n, perr := strconv.Atoi(port); perr != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() != nil
}

// ValidateEgressVerifyConfig checks cfg without touching Docker, so a bad setting fails at startup.
func ValidateEgressVerifyConfig(cfg EgressVerifyConfig) error {
	if cfg.Image == "" || cfg.Network == "" {
		return errors.New("egress verification: an image and network are required")
	}
	if !validEndpoint(cfg.Control) {
		return fmt.Errorf("egress verification: control %q must be ipv4:port", cfg.Control)
	}
	if len(cfg.Canaries) == 0 {
		return errors.New("egress verification: at least one canary is required")
	}
	for _, c := range cfg.Canaries {
		if !validEndpoint(c) {
			return fmt.Errorf("egress verification: canary %q must be ipv4:port", c)
		}
	}
	return nil
}

// VerifyEgressFirewall runs the probe and returns nil only when it proves the policy is enforced.
func VerifyEgressFirewall(ctx context.Context, cli DockerClient, cfg EgressVerifyConfig) error {
	if cli == nil {
		return errors.New("egress verification: a Docker client, image and network are required")
	}
	if err := ValidateEgressVerifyConfig(cfg); err != nil {
		return err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := append([]string{"/bin/sh", "-c", probeScript, "sh", cfg.Control}, cfg.Canaries...)
	resp, err := cli.ContainerCreate(ctx,
		&container.Config{Image: cfg.Image, User: "1000:1000", Cmd: args},
		&container.HostConfig{
			NetworkMode:    container.NetworkMode(cfg.Network),
			DNS:            []string{SandboxDeadDNS},
			CapDrop:        []string{"ALL"},
			ReadonlyRootfs: true,
			SecurityOpt:    []string{"no-new-privileges:true"},
		}, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return fmt.Errorf("egress verification: create probe: %w", err)
	}
	defer func() {
		_ = cli.ContainerRemove(context.WithoutCancel(ctx), resp.ID, container.RemoveOptions{Force: true})
	}()
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("egress verification: start probe: %w", err)
	}
	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case werr := <-errCh:
		if werr != nil {
			return fmt.Errorf("egress verification: wait for probe: %w", werr)
		}
		return errors.New("egress verification: probe wait ended without a status")
	case st := <-statusCh:
		switch st.StatusCode {
		case 0:
			return nil
		case probeExitCanaryReachable:
			return ErrEgressOpen
		case probeExitCanaryAnswered:
			return ErrEgressCanaryAnswered
		case probeExitControlUnreachable:
			return ErrEgressControlUnreachable
		default:
			return fmt.Errorf("egress verification: probe could not run (exit %d)", st.StatusCode)
		}
	case <-ctx.Done():
		return fmt.Errorf("egress verification: %w", ctx.Err())
	}
}
