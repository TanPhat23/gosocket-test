package runner

import (
	"bufio"
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/sandbox"
)

// Gate 0G: these tests drive the (quarantined, unreferenced) provisioning draft directly so the
// secret-delivery contract is pinned before the quarantine can ever be lifted.

// stdinConn is a net.Conn whose writes are captured and whose CloseWrite is observable.
type stdinConn struct {
	net.Conn
	written     strings.Builder
	closedWrite bool
}

func (c *stdinConn) Write(b []byte) (int, error) { return c.written.Write(b) }
func (c *stdinConn) CloseWrite() error           { c.closedWrite = true; return nil }
func (c *stdinConn) Close() error                { return nil }
func (c *stdinConn) SetDeadline(time.Time) error { return nil }

type secretRecordingClient struct {
	*sandbox.MockDockerClient
	execOpts    container.ExecOptions
	execAttach  container.ExecAttachOptions
	conn        *stdinConn
	execExit    int
	execErr     error
	inspectErr  error
	removeCalls int
	order       []string
}

func (c *secretRecordingClient) ContainerStart(ctx context.Context, id string, o container.StartOptions) error {
	c.order = append(c.order, "start")
	return c.MockDockerClient.ContainerStart(ctx, id, o)
}

func (c *secretRecordingClient) ContainerRemove(ctx context.Context, id string, o container.RemoveOptions) error {
	c.removeCalls++
	return c.MockDockerClient.ContainerRemove(ctx, id, o)
}

func (c *secretRecordingClient) ContainerExecCreate(ctx context.Context, id string, o container.ExecOptions) (types.IDResponse, error) {
	c.order = append(c.order, "exec")
	c.execOpts = o
	if c.execErr != nil {
		return types.IDResponse{}, c.execErr
	}
	return types.IDResponse{ID: "exec-secret"}, nil
}

func (c *secretRecordingClient) ContainerExecAttach(ctx context.Context, id string, o container.ExecAttachOptions) (types.HijackedResponse, error) {
	c.execAttach = o
	c.conn = &stdinConn{}
	return types.HijackedResponse{Conn: c.conn, Reader: bufio.NewReader(strings.NewReader(""))}, nil
}

func (c *secretRecordingClient) ContainerExecInspect(ctx context.Context, id string) (container.ExecInspect, error) {
	return container.ExecInspect{ExitCode: c.execExit}, c.inspectErr
}

func newSecretClient() *secretRecordingClient {
	return &secretRecordingClient{MockDockerClient: &sandbox.MockDockerClient{}}
}

func runSecretDraftWith(t *testing.T, cli *secretRecordingClient) (*ProjectRouteHandle, error) {
	t.Helper()
	mgr := NewProjectSandboxManager(cli, nil, "https://gateway.invalid").(*projectSandboxManager)
	return mgr.ensureProjectContainer(context.Background(), &models.Project{ID: "p1"}, "atm-runtime:test")
}

func runSecretDraft(t *testing.T) (*secretRecordingClient, *ProjectRouteHandle) {
	t.Helper()
	cli := newSecretClient()
	route, err := runSecretDraftWith(t, cli)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	return cli, route
}

// The secret travels over exec stdin: docker cp cannot write into a read-only rootfs or into a
// tmpfs mount, and stdin keeps the password out of argv, env and docker inspect.
func TestProjectSecret_DeliveredOverStdinAsContainerUser(t *testing.T) {
	cli, route := runSecretDraft(t)
	if cli.execOpts.User != "1000:1000" {
		t.Errorf("exec user = %q; the file must be created by the opencode user (1000:1000)", cli.execOpts.User)
	}
	if !cli.execOpts.AttachStdin {
		t.Error("exec must attach stdin")
	}
	cmd := strings.Join(cli.execOpts.Cmd, " ")
	for _, want := range []string{"umask 077", "/run/secrets/opencode.tmp", "mv /run/secrets/opencode.tmp /run/secrets/opencode"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("exec command %q missing %q (owner-only, atomically published file)", cmd, want)
		}
	}
	if got := cli.conn.written.String(); got != route.AuthPassword || len(got) != 64 {
		t.Errorf("stdin body does not match generated password (len %d)", len(got))
	}
	if !cli.conn.closedWrite {
		t.Error("stdin must be half-closed so cat terminates")
	}
	if strings.Contains(cmd+strings.Join(cli.execOpts.Env, " "), route.AuthPassword) {
		t.Error("password leaked into exec argv/env")
	}
}

func TestProjectSecret_DeliveryFailureRollsBackWithoutLeak(t *testing.T) {
	cases := map[string]func(*secretRecordingClient){
		"exec create fails": func(c *secretRecordingClient) { c.execErr = errors.New("boom") },
		"non-zero exit":     func(c *secretRecordingClient) { c.execExit = 1 },
		"inspect fails":     func(c *secretRecordingClient) { c.inspectErr = errors.New("boom") },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			cli := newSecretClient()
			setup(cli)
			route, err := runSecretDraftWith(t, cli)
			if err == nil || route != nil {
				t.Fatalf("want error and nil route, got route=%v err=%v", route, err)
			}
			if cli.removeCalls == 0 {
				t.Error("container must be removed when secret delivery fails")
			}
			if cli.conn != nil && strings.Contains(err.Error(), cli.conn.written.String()) && cli.conn.written.Len() > 0 {
				t.Error("error message leaks the password")
			}
		})
	}
}

func TestProjectContainer_NoSecretsInConfig(t *testing.T) {
	cli, route := runSecretDraft(t)
	if len(cli.CreateCalls) != 1 {
		t.Fatalf("create calls = %d", len(cli.CreateCalls))
	}
	call := cli.CreateCalls[0]
	dump := strings.Join(call.Config.Env, "\n") + strings.Join(call.Config.Cmd, " ") + strings.Join(call.Config.Entrypoint, " ")
	for k, v := range call.Config.Labels {
		dump += k + "=" + v
	}
	if strings.Contains(dump, route.AuthPassword) {
		t.Error("OpenCode password visible in docker inspect surface (env/cmd/labels)")
	}
	for _, e := range call.Config.Env {
		name := strings.ToUpper(strings.SplitN(e, "=", 2)[0])
		if strings.HasSuffix(name, "_API_KEY") || strings.HasPrefix(name, "OPENCODE_PASSWORD") || strings.HasPrefix(name, "OPENCODE_SERVER_PASSWORD") {
			t.Errorf("forbidden env %q", name)
		}
	}
	for _, b := range call.HostConfig.Binds {
		if strings.Contains(b, "/.config/opencode") || strings.Contains(b, "/.local/share/opencode") {
			t.Errorf("host opencode profile bind-mounted: %q", b)
		}
	}
}

// The root filesystem is read-only, so OpenCode must be told to keep its data under the state volume;
// otherwise it exits at startup (found live in the Gate 0G run).
func TestProjectContainer_HomeOnWritableStateVolume(t *testing.T) {
	cli, _ := runSecretDraft(t)
	call := cli.CreateCalls[0]
	if !slices.Contains(call.Config.Env, "HOME="+opencodeStateDir) {
		t.Errorf("HOME must point at %s, env=%v", opencodeStateDir, call.Config.Env)
	}
	if !slices.ContainsFunc(call.HostConfig.Binds, func(b string) bool { return strings.HasSuffix(b, ":"+opencodeStateDir+":rw") }) {
		t.Errorf("state volume must be mounted rw at %s, binds=%v", opencodeStateDir, call.HostConfig.Binds)
	}
}

// Gate 0G probe 03: OpenCode's task shells inherit its environment, so bash must be told to scrub it.
func TestProjectContainer_TaskShellScrubsSecrets(t *testing.T) {
	cli, _ := runSecretDraft(t)
	if !slices.Contains(cli.CreateCalls[0].Config.Env, "BASH_ENV="+taskShellScrubFile) {
		t.Errorf("BASH_ENV must point at %s, env=%v", taskShellScrubFile, cli.CreateCalls[0].Config.Env)
	}
	tpl := string(embeddedTemplate)
	for _, want := range []string{taskShellScrubFile, "OPENCODE_PASSWORD", "OPENCODE_CONFIG_CONTENT"} {
		if !strings.Contains(tpl, want) {
			t.Errorf("template does not create the scrub file: missing %q", want)
		}
	}
}

func TestProjectSecret_TmpfsOwnedByContainerUser(t *testing.T) {
	cli, _ := runSecretDraft(t)
	opts := cli.CreateCalls[0].HostConfig.Tmpfs["/run/secrets"]
	for _, want := range []string{"uid=1000", "gid=1000", "mode=0700", "noexec", "nosuid"} {
		if !strings.Contains(opts, want) {
			t.Errorf("/run/secrets tmpfs options %q missing %q (a root-owned 0700 dir is untraversable by UID 1000)", opts, want)
		}
	}
}

func TestProjectSecret_DeliveredAfterStart(t *testing.T) {
	// A stopped container's tmpfs is mounted fresh at start, so the secret must be delivered to the
	// running container (the entrypoint waits for it).
	cli, _ := runSecretDraft(t)
	if strings.Join(cli.order, ",") != "start,exec" {
		t.Errorf("order = %v, want start then exec", cli.order)
	}
}

// Gate 0D: the sandbox resolver must point at a dead, non-loopback address. Loopback upstreams are
// queried from the host's network namespace and would bypass the sandbox firewall.
func TestProjectSandbox_DNSIsDeadNonLoopbackAddress(t *testing.T) {
	cli, _ := runSecretDraft(t)
	dns := cli.CreateCalls[0].HostConfig.DNS
	if len(dns) != 1 || dns[0] != "192.0.2.1" {
		t.Fatalf("sandbox DNS = %v; want exactly [192.0.2.1]", dns)
	}
	if ip := net.ParseIP(dns[0]); ip == nil || ip.IsLoopback() {
		t.Fatalf("sandbox DNS %q must be a non-loopback IP", dns[0])
	}
}

// Gate 0D: a hostname proxy can never resolve inside the sandbox, so provisioning refuses it before any Docker call.
func TestProjectSandbox_HostnameProxyRejectedBeforeDocker(t *testing.T) {
	cli := newSecretClient()
	mgr := NewProjectSandboxManager(cli, sandbox.DefaultEgressPolicy("http://atm-egress-proxy:3128"), "").(*projectSandboxManager)
	if _, err := mgr.ensureProjectContainer(context.Background(), &models.Project{ID: "p1"}, "img"); err == nil {
		t.Fatal("hostname proxy URL must be rejected")
	}
	if len(cli.CreateCalls) != 0 || len(cli.order) != 0 {
		t.Fatal("no Docker call may precede proxy URL validation")
	}
}
