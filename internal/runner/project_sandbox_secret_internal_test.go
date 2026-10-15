package runner

import (
	"archive/tar"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/sandbox"
)

// Gate 0G: these tests drive the (quarantined, unreferenced) provisioning draft directly so the
// secret-delivery contract is pinned before the quarantine can ever be lifted.

type secretRecordingClient struct {
	*sandbox.MockDockerClient
	copyDst     string
	copyContent []byte
	order       []string
}

func (c *secretRecordingClient) ContainerStart(ctx context.Context, id string, o container.StartOptions) error {
	c.order = append(c.order, "start")
	return c.MockDockerClient.ContainerStart(ctx, id, o)
}

func (c *secretRecordingClient) CopyToContainer(ctx context.Context, id, dst string, content io.Reader, opts container.CopyToContainerOptions) error {
	c.order = append(c.order, "copy")
	c.copyDst = dst
	b, err := io.ReadAll(content)
	c.copyContent = b
	return err
}

func runSecretDraft(t *testing.T) (*secretRecordingClient, *ProjectRouteHandle) {
	t.Helper()
	cli := &secretRecordingClient{MockDockerClient: &sandbox.MockDockerClient{}}
	mgr := NewProjectSandboxManager(cli, nil, "https://gateway.invalid").(*projectSandboxManager)
	route, err := mgr.ensureProjectContainerDisabledDraft(context.Background(), &models.Project{ID: "p1"}, "atm-runtime:test")
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	return cli, route
}

func TestProjectSecret_ReadableByContainerUser(t *testing.T) {
	cli, route := runSecretDraft(t)
	if cli.copyDst != "/run/secrets" {
		t.Fatalf("secret copied to %q", cli.copyDst)
	}
	tr := tar.NewReader(strings.NewReader(string(cli.copyContent)))
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar: %v", err)
	}
	if hdr.Name != "opencode" {
		t.Errorf("name = %q", hdr.Name)
	}
	if hdr.Uid != 1000 || hdr.Gid != 1000 {
		t.Errorf("secret owned by %d:%d; the opencode user (1000:1000) cannot read a root-owned file", hdr.Uid, hdr.Gid)
	}
	if hdr.Mode&0o077 != 0 || hdr.Mode&0o400 == 0 {
		t.Errorf("mode = %o; want owner-read only", hdr.Mode)
	}
	body, _ := io.ReadAll(tr)
	if string(body) != route.AuthPassword || len(body) != 64 {
		t.Errorf("secret body does not match generated password (len %d)", len(body))
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

func TestProjectSecret_TmpfsOwnedByContainerUser(t *testing.T) {
	cli, _ := runSecretDraft(t)
	opts := cli.CreateCalls[0].HostConfig.Tmpfs["/run/secrets"]
	for _, want := range []string{"uid=1000", "gid=1000", "mode=0700", "noexec", "nosuid"} {
		if !strings.Contains(opts, want) {
			t.Errorf("/run/secrets tmpfs options %q missing %q (a root-owned 0700 dir is untraversable by UID 1000)", opts, want)
		}
	}
}

func TestProjectSecret_CopiedAfterStart(t *testing.T) {
	// Files copied into a stopped container's tmpfs are discarded when the tmpfs is mounted at start,
	// so the secret must be delivered to the running container (the entrypoint waits for it).
	cli, _ := runSecretDraft(t)
	if strings.Join(cli.order, ",") != "start,copy" {
		t.Errorf("order = %v, want start then copy", cli.order)
	}
}
