package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"

	"agent-task-manager/internal/gateway"
	"agent-task-manager/internal/models"
)

type fakeBroker struct {
	grantErr error
	grants   []string
	released []string
}

func (b *fakeBroker) Grant(projectID, leaseID string) (gateway.Grant, error) {
	if b.grantErr != nil {
		return gateway.Grant{}, b.grantErr
	}
	b.grants = append(b.grants, projectID+"/"+leaseID)
	return gateway.Grant{Token: "tok-secret-value", BaseURL: "http://172.29.0.2:8082/v1"}, nil
}

func (b *fakeBroker) Release(leaseID string) { b.released = append(b.released, leaseID) }

// gwClient records every exec command and stdin body, in order.
type gwClient struct {
	*secretRecordingClient
	cmds []string
	// failExecN makes the Nth exec (1-based) exit non-zero.
	failExecN int
	bodies    []string
}

func newGWClient() *gwClient { return &gwClient{secretRecordingClient: newSecretClient()} }

func (c *gwClient) ContainerExecInspect(ctx context.Context, id string) (container.ExecInspect, error) {
	if c.conn != nil {
		c.bodies = append(c.bodies, c.conn.written.String())
	}
	c.cmds = append(c.cmds, strings.Join(c.execOpts.Cmd, " "))
	if c.failExecN > 0 && len(c.cmds) == c.failExecN {
		return container.ExecInspect{ExitCode: 1}, nil
	}
	return c.secretRecordingClient.ContainerExecInspect(ctx, id)
}

func gwManager(cli *gwClient, b GatewayBroker) *projectSandboxManager {
	return NewProjectSandboxManagerWithGateway(cli, nil, "", &GatewayAccess{
		Broker: b, Models: []string{"m1"}, ContextTokens: 1000, OutputTokens: 100,
	}).(*projectSandboxManager)
}

// Config first, password second: the entrypoint treats the password file as the ready signal.
func TestProjectGateway_ConfigDeliveredBeforePasswordViaStdinOnly(t *testing.T) {
	cli, b := newGWClient(), &fakeBroker{}
	route, err := gwManager(cli, b).ensureProjectContainerDisabledDraft(context.Background(), &models.Project{ID: "p1"}, "img")
	if err != nil {
		t.Fatal(err)
	}
	if len(cli.cmds) != 2 || !strings.Contains(cli.cmds[0], "/run/secrets/opencode-config") || !strings.Contains(cli.cmds[1], "mv /run/secrets/opencode.tmp /run/secrets/opencode") {
		t.Fatalf("delivery order wrong: %q", cli.cmds)
	}
	if !strings.Contains(cli.bodies[0], "tok-secret-value") || cli.bodies[1] != route.AuthPassword {
		t.Error("config must carry the token; second body must be the password")
	}
	if len(b.grants) != 1 || b.grants[0] != "p1/"+route.ContainerID {
		t.Errorf("grant must be keyed by container id: %v", b.grants)
	}
	for _, c := range cli.CreateCalls {
		if strings.Contains(strings.Join(c.Config.Env, " "), "tok-secret-value") {
			t.Error("token leaked into container env")
		}
	}
}

func TestProjectGateway_ReleasedOnStopAndOnDeliveryFailure(t *testing.T) {
	cli, b := newGWClient(), &fakeBroker{}
	m := gwManager(cli, b)
	route, err := m.ensureProjectContainerDisabledDraft(context.Background(), &models.Project{ID: "p1"}, "img")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.StopProjectContainer(context.Background(), "p1", 0); err != nil {
		t.Fatal(err)
	}
	if len(b.released) != 1 || b.released[0] != route.ContainerID {
		t.Errorf("stop must release the lease: %v", b.released)
	}

	cli2, b2 := newGWClient(), &fakeBroker{}
	cli2.execErr = errors.New("boom")
	if _, err := gwManager(cli2, b2).ensureProjectContainerDisabledDraft(context.Background(), &models.Project{ID: "p2"}, "img"); err == nil {
		t.Fatal("expected failure")
	}
	if len(b2.released) == 0 {
		t.Error("failed provisioning must release the lease")
	}
}

func TestProjectGateway_GrantFailureFailsClosed(t *testing.T) {
	cli, b := newGWClient(), &fakeBroker{grantErr: errors.New("nope")}
	if _, err := gwManager(cli, b).ensureProjectContainerDisabledDraft(context.Background(), &models.Project{ID: "p1"}, "img"); err == nil {
		t.Fatal("grant failure must abort provisioning")
	}
	if cli.removeCalls == 0 {
		t.Error("container must be removed on grant failure")
	}
}

// The config was already delivered when the password step fails, so rollback must revoke the lease.
func TestProjectGateway_PasswordDeliveryFailureAfterConfigReleasesLease(t *testing.T) {
	cli, b := newGWClient(), &fakeBroker{}
	cli.failExecN = 2
	if _, err := gwManager(cli, b).ensureProjectContainerDisabledDraft(context.Background(), &models.Project{ID: "p1"}, "img"); err == nil {
		t.Fatal("expected failure")
	}
	if len(b.grants) != 1 || len(b.released) == 0 {
		t.Errorf("grants=%v released=%v; rollback must release the lease", b.grants, b.released)
	}
}
