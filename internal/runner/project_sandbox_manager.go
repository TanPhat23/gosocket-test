package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/gateway"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/sandbox"
)

// sandboxDNS is the only upstream the sandbox's embedded Docker resolver may use. It is RFC 5737
// TEST-NET-1: nothing answers there, so lookups fail and package managers resolve through the proxy's
// CONNECT instead. It must not be loopback: Docker forwards loopback upstreams from the host's network
// namespace, which bypasses the sandbox firewall (verified live on Docker 29.1.3, 2026-10-16). A
// non-loopback upstream is queried from the sandbox's own namespace, where the Gate 0D rules drop it.
const sandboxDNS = sandbox.SandboxDeadDNS

// opencodeUID is the unprivileged user the project container runs OpenCode as.
const opencodeUID = 1000

// opencodeStateDir is the writable state volume mount and OpenCode's HOME (the root filesystem is read-only).
const opencodeStateDir = "/var/lib/opencode"

// taskShellScrubFile is baked into the image (Dockerfile.project.template). Bash sources it in every non-interactive
// task shell and unsets the OpenCode password and gateway token, which OpenCode itself needs in its own environment.
const taskShellScrubFile = "/usr/local/lib/atm-scrub.sh"

// ErrQuarantineActive is returned by EnsureProjectContainer while project container
// provisioning is disabled. It wraps common.ErrRuntimeQuarantined.
var ErrQuarantineActive = fmt.Errorf("%w", common.ErrRuntimeQuarantined)

// ProjectRouteHandle contains routing, network, and dynamic credentials for an active project container.
type ProjectRouteHandle struct {
	ProjectID    string `json:"project_id"`
	ContainerID  string `json:"container_id"`
	NetworkID    string `json:"network_id"`
	IPAddress    string `json:"ip_address"`
	Port         int    `json:"port"`
	EndpointURL  string `json:"endpoint_url"`
	AuthPassword string `json:"-"`
	Generation   int64  `json:"generation"`
}

// GatewayBroker mints and revokes the capability tokens sandboxes use to reach the LLM gateway.
// *gateway.Broker satisfies it.
type GatewayBroker interface {
	Grant(projectID, leaseID string) (gateway.Grant, error)
	Release(leaseID string)
}

// GatewayAccess is the optional Gate 0G wiring: when set, each project container gets a lease-scoped
// token and an OpenCode config that points only at the gateway, delivered over exec stdin.
type GatewayAccess struct {
	Broker        GatewayBroker
	Models        []string
	ContextTokens int
	OutputTokens  int
}

// DevHostAuthOptions configures developer mode host-authenticated OpenCode mounting into project sandboxes.
type DevHostAuthOptions struct {
	Enabled     bool
	OpenCodeBin string
	ConfigDir   string
	DataDir     string
}

// ProjectSandboxManager manages project sandbox container lifecycle, isolation, and routing.
type ProjectSandboxManager interface {
	EnsureProjectContainer(ctx context.Context, project *models.Project, imageTag string) (*ProjectRouteHandle, error)
	StopProjectContainer(ctx context.Context, projectID string, gracePeriod time.Duration) error
	GetProjectRoute(ctx context.Context, projectID string) (*ProjectRouteHandle, bool, error)
}

type projectSandboxManager struct {
	dockerCli    sandbox.DockerClient
	egressPolicy *sandbox.EgressPolicy
	gatewayURL   string
	gw           *GatewayAccess
	devHostAuth  DevHostAuthOptions
	// egressVerify, when set, proves the operator's firewall on the project's own sandbox network before any
	// sandbox container is created there (Gate 0D fail-closed contract). An error aborts provisioning.
	egressVerify func(ctx context.Context, network string) error

	mu     sync.RWMutex
	routes map[string]*ProjectRouteHandle
}

// NewProjectSandboxManager returns a new instance of ProjectSandboxManager.
func NewProjectSandboxManager(
	dockerCli sandbox.DockerClient,
	egressPolicy *sandbox.EgressPolicy,
	gatewayURL string,
) ProjectSandboxManager {
	return &projectSandboxManager{
		dockerCli:    dockerCli,
		egressPolicy: egressPolicy,
		gatewayURL:   gatewayURL,
		routes:       make(map[string]*ProjectRouteHandle),
	}
}

// NewProjectSandboxManagerWithGateway is NewProjectSandboxManager plus lease-scoped gateway credentials.
// A nil gw behaves exactly like NewProjectSandboxManager.
func NewProjectSandboxManagerWithGateway(
	dockerCli sandbox.DockerClient,
	egressPolicy *sandbox.EgressPolicy,
	gatewayURL string,
	gw *GatewayAccess,
) ProjectSandboxManager {
	return &projectSandboxManager{
		dockerCli:    dockerCli,
		egressPolicy: egressPolicy,
		gatewayURL:   gatewayURL,
		gw:           gw,
		routes:       make(map[string]*ProjectRouteHandle),
	}
}

// NewProjectSandboxManagerWithDevHostAuth returns a ProjectSandboxManager configured with developer host authentication.
func NewProjectSandboxManagerWithDevHostAuth(
	dockerCli sandbox.DockerClient,
	opts DevHostAuthOptions,
) ProjectSandboxManager {
	return &projectSandboxManager{
		dockerCli:   dockerCli,
		devHostAuth: opts,
		routes:      make(map[string]*ProjectRouteHandle),
	}
}

// EnsureProjectContainer ensures a hardened project container is running for the given project.
//
// The Phase 1A quarantine was lifted on the operator's explicit instruction (2026-10-16) with Gate 0F not
// qualified; see the gate status doc. Provisioning still fails closed: the egress firewall proof, the gateway
// lease and the password delivery must all succeed before a route is returned.
func (m *projectSandboxManager) EnsureProjectContainer(ctx context.Context, project *models.Project, imageTag string) (*ProjectRouteHandle, error) {
	return m.ensureProjectContainer(ctx, project, imageTag)
}

// ensureProjectContainer provisions the hardened project container, delivers the gateway config and password,
// and records the route.
func (m *projectSandboxManager) ensureProjectContainer(ctx context.Context, project *models.Project, imageTag string) (*ProjectRouteHandle, error) {
	if project == nil || project.ID == "" {
		return nil, errors.New("project and project.ID are required")
	}
	if m.egressPolicy != nil {
		if err := sandbox.ValidateForwardProxyURL(m.egressPolicy.ForwardProxyURL); err != nil {
			return nil, err
		}
	}
	if imageTag == "" {
		return nil, errors.New("imageTag is required")
	}

	m.mu.RLock()
	existingRoute, ok := m.routes[project.ID]
	m.mu.RUnlock()

	if ok && existingRoute != nil {
		inspect, err := m.dockerCli.ContainerInspect(ctx, existingRoute.ContainerID)
		if err == nil && inspect.State != nil && inspect.State.Running {
			return existingRoute, nil
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check under write lock
	if existingRoute, ok := m.routes[project.ID]; ok && existingRoute != nil {
		inspect, err := m.dockerCli.ContainerInspect(ctx, existingRoute.ContainerID)
		if err == nil && inspect.State != nil && inspect.State.Running {
			return existingRoute, nil
		}
	}

	containerName := "atm-project-" + project.ID
	netName := "atm-net-" + project.ID
	if m.devHostAuth.Enabled {
		netName = "agent_ingress"
	}
	dataVolume := sandbox.ProjectDataVolumeName(project.ID)
	stateVolume := "atm-project-state-" + project.ID

	// 1. Ensure bridge network exists
	netResp, err := m.dockerCli.NetworkCreate(ctx, netName, network.CreateOptions{
		Driver:     "bridge",
		Attachable: true,
		Internal:   false, // Forward proxy DMZ communication requires external forwarding through egress policy
	})
	networkID := netResp.ID
	if err != nil {
		// Network might already exist
		inspect, inspErr := m.dockerCli.NetworkInspect(ctx, netName, network.InspectOptions{})
		if inspErr != nil {
			return nil, fmt.Errorf("failed to create/inspect project network %s: %w", netName, err)
		}
		networkID = inspect.ID
	}

	// 1b. Gate 0D: no sandbox starts on a network whose firewall has not been proven from inside it.
	if m.egressVerify != nil {
		if err := m.egressVerify(ctx, netName); err != nil {
			return nil, err
		}
	}

	// 2. Ensure volumes exist
	if _, err := m.dockerCli.VolumeCreate(ctx, volume.CreateOptions{Name: dataVolume}); err != nil {
		return nil, fmt.Errorf("failed to create project data volume %s: %w", dataVolume, err)
	}
	if _, err := m.dockerCli.VolumeCreate(ctx, volume.CreateOptions{Name: stateVolume}); err != nil {
		return nil, fmt.Errorf("failed to create project state volume %s: %w", stateVolume, err)
	}

	// 3. Generate dynamic password for OpenCode (crypto random hex)
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random secret: %w", err)
	}
	opencodePassword := hex.EncodeToString(randomBytes)

	// 4. Construct container environment without plain model API keys or auth password
	env := make([]string, 0)
	if m.egressPolicy != nil {
		env = append(env, sandbox.BuildProxyEnv(m.egressPolicy)...)
	}
	if m.gatewayURL != "" {
		env = append(env, "ATM_LLM_GATEWAY_URL="+m.gatewayURL)
	}
	// The root filesystem is read-only: OpenCode's data, config, cache and logs (XDG dirs under $HOME) must
	// land on the state volume, otherwise it exits at startup with a FileSystem.makeDirectory error.
	env = append(env, "OPENCODE_SERVER_PORT=4096", "HOME="+opencodeStateDir, "BASH_ENV="+taskShellScrubFile)
	if m.devHostAuth.Enabled {
		env = append(env, "OPENCODE_SERVER_HOST=0.0.0.0")
	}

	// 5. Container & Host Configuration
	containerCfg := &container.Config{
		Image: imageTag,
		Env:   env,
	}

	pidsLimit := int64(256)
	binds := []string{
		fmt.Sprintf("%s:/workspaces:rw", dataVolume),
		fmt.Sprintf("%s:%s:rw", stateVolume, opencodeStateDir),
	}
	tmpfs := map[string]string{
		"/tmp":         "rw,noexec,nosuid,size=512m",
		"/run/secrets": "rw,noexec,nosuid,size=1m,uid=1000,gid=1000,mode=0700",
	}

	if m.devHostAuth.Enabled {
		if m.devHostAuth.ConfigDir != "" {
			binds = append(binds, fmt.Sprintf("%s:%s/.config/opencode:ro", m.devHostAuth.ConfigDir, opencodeStateDir))
		}
		if m.devHostAuth.DataDir != "" {
			binds = append(binds, fmt.Sprintf("%s:%s/.local/share/opencode:rw", m.devHostAuth.DataDir, opencodeStateDir))
		}

		tmpfs[fmt.Sprintf("%s/.local/state", opencodeStateDir)] = "rw,noexec,nosuid,size=64m,uid=1000,gid=1000,mode=0700"
		tmpfs[fmt.Sprintf("%s/.cache", opencodeStateDir)] = "rw,noexec,nosuid,size=128m,uid=1000,gid=1000,mode=0700"
	}

	var secOpt []string
	if !m.devHostAuth.Enabled {
		secOpt = []string{"no-new-privileges:true"}
	}

	hostCfg := &container.HostConfig{
		ReadonlyRootfs: true,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    secOpt,
		Binds:          binds,
		Tmpfs:          tmpfs,
		Resources: container.Resources{
			Memory:    4 * 1024 * 1024 * 1024, // 4GB
			NanoCPUs:  2 * 1e9,                // 2 CPUs
			PidsLimit: &pidsLimit,
		},
		PortBindings:    nil,
		PublishAllPorts: false,
		NetworkMode:     container.NetworkMode(netName),
		DNS:             []string{sandboxDNS},
	}

	netConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			netName: {},
		},
	}

	// 6. Create container
	createResp, err := m.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, netConfig, nil, containerName)
	if err != nil {
		if strings.Contains(err.Error(), "Conflict") || strings.Contains(err.Error(), "already in use") {
			_ = m.dockerCli.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
			createResp, err = m.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, netConfig, nil, containerName)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to create project container %s: %w", containerName, err)
		}
	}
	containerID := createResp.ID

	// Helper for fail-closed rollback
	rollback := func(cause error) (*ProjectRouteHandle, error) {
		_ = m.dockerCli.ContainerStop(ctx, containerID, container.StopOptions{})
		_ = m.dockerCli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
		m.releaseGateway(containerID)
		delete(m.routes, project.ID)
		return nil, cause
	}

	// 7. Start container
	if err := m.dockerCli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return rollback(fmt.Errorf("failed to start project container: %w", err))
	}

	// 7b. Gateway config goes first: the entrypoint treats the password file as the ready signal.
	if err := m.deliverGatewayConfig(ctx, project.ID, containerID); err != nil {
		return rollback(err)
	}

	// 8. Deliver OPENCODE_PASSWORD over exec stdin into the running container's tmpfs.
	// It must follow start: a stopped container's tmpfs is mounted fresh at start, discarding earlier writes.
	// The entrypoint waits for the file, loads it into its own environment and unlinks it.
	if err := m.deliverSecret(ctx, containerID, opencodePassword); err != nil {
		return rollback(err)
	}

	// 9. Inspect container to resolve IP address
	inspect, err := m.dockerCli.ContainerInspect(ctx, containerID)
	if err != nil {
		return rollback(fmt.Errorf("failed to inspect project container after start: %w", err))
	}

	var ipAddress string
	if inspect.NetworkSettings != nil {
		if netSettings, ok := inspect.NetworkSettings.Networks[netName]; ok {
			ipAddress = netSettings.IPAddress
		}
		if ipAddress == "" {
			ipAddress = inspect.NetworkSettings.IPAddress
		}
	}

	port := 4096
	endpointURL := fmt.Sprintf("http://%s:%d", ipAddress, port)
	if m.devHostAuth.Enabled {
		endpointURL = fmt.Sprintf("http://%s:%d", containerName, port)
	}

	routeHandle := &ProjectRouteHandle{
		ProjectID:    project.ID,
		ContainerID:  containerID,
		NetworkID:    networkID,
		IPAddress:    ipAddress,
		Port:         port,
		EndpointURL:  endpointURL,
		AuthPassword: opencodePassword,
		Generation:   time.Now().UnixNano(),
	}

	m.routes[project.ID] = routeHandle
	return routeHandle, nil
}

// StopProjectContainer stops the project sandbox container gracefully and invalidates routes.
func (m *projectSandboxManager) StopProjectContainer(ctx context.Context, projectID string, gracePeriod time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	route, ok := m.routes[projectID]
	if !ok || route == nil {
		return nil
	}

	timeoutSec := int(gracePeriod.Seconds())
	stopOpts := container.StopOptions{
		Timeout: &timeoutSec,
	}

	err := m.dockerCli.ContainerStop(ctx, route.ContainerID, stopOpts)
	m.releaseGateway(route.ContainerID)
	delete(m.routes, projectID)
	return err
}

// GetProjectRoute returns the active route handle for a project, if cached and valid.
func (m *projectSandboxManager) GetProjectRoute(ctx context.Context, projectID string) (*ProjectRouteHandle, bool, error) {
	m.mu.RLock()
	route, ok := m.routes[projectID]
	m.mu.RUnlock()

	if !ok || route == nil {
		return nil, false, nil
	}

	return route, true, nil
}

// deliverSecret writes the password to /run/secrets/opencode inside the running container.
// docker cp is unusable here (it fails on a read-only rootfs and lands under tmpfs mounts), so the
// password goes over the exec's stdin: never argv, env or docker inspect. The file is created
// owner-only by the container user and published with an atomic rename, so the entrypoint never
// observes a partial secret. Errors deliberately carry no exec output.
func (m *projectSandboxManager) deliverSecret(ctx context.Context, containerID, password string) error {
	return m.deliverSecretFile(ctx, containerID, "opencode", password)
}

// releaseGateway revokes the container's gateway lease. It is a no-op when the gateway is not wired.
func (m *projectSandboxManager) releaseGateway(leaseID string) {
	if m.gw != nil && m.gw.Broker != nil {
		m.gw.Broker.Release(leaseID)
	}
}

// deliverGatewayConfig mints a lease-scoped token (lease = container ID) and writes the OpenCode config
// that embeds it to /run/secrets/opencode-config. The token never appears in env, argv or docker inspect.
// On any failure the lease is released. Errors carry no token or config content.
func (m *projectSandboxManager) deliverGatewayConfig(ctx context.Context, projectID, containerID string) error {
	if m.gw == nil || m.gw.Broker == nil {
		return nil
	}
	grant, err := m.gw.Broker.Grant(projectID, containerID)
	if err != nil {
		return fmt.Errorf("failed to mint gateway token: %w", err)
	}
	cfg, err := gateway.OpenCodeConfig(gateway.OpenCodeConfigParams{
		BaseURL:       grant.BaseURL,
		Token:         grant.Token,
		Models:        m.gw.Models,
		ContextTokens: m.gw.ContextTokens,
		OutputTokens:  m.gw.OutputTokens,
	})
	if err != nil {
		m.gw.Broker.Release(containerID)
		return fmt.Errorf("failed to render gateway config: %w", err)
	}
	if err := m.deliverSecretFile(ctx, containerID, "opencode-config", cfg); err != nil {
		m.gw.Broker.Release(containerID)
		return err
	}
	return nil
}

// deliverSecretFile is the shared stdin-to-tmpfs delivery. name is a fixed internal constant, never user input.
func (m *projectSandboxManager) deliverSecretFile(ctx context.Context, containerID, name, password string) error {
	script := "umask 077 && cat > /run/secrets/" + name + ".tmp && mv /run/secrets/" + name + ".tmp /run/secrets/" + name
	created, err := m.dockerCli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		User:         fmt.Sprintf("%d:%d", opencodeUID, opencodeUID),
		Cmd:          []string{"sh", "-c", script},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return fmt.Errorf("failed to create secret delivery exec: %w", err)
	}
	hijacked, err := m.dockerCli.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return fmt.Errorf("failed to attach secret delivery exec: %w", err)
	}
	defer hijacked.Close()
	if _, err := io.WriteString(hijacked.Conn, password); err != nil {
		return errors.New("failed to write secret to container")
	}
	if err := hijacked.CloseWrite(); err != nil {
		return errors.New("failed to close secret stdin")
	}
	// Draining until EOF means the command has finished.
	_, _ = io.Copy(io.Discard, hijacked.Reader)
	res, err := m.dockerCli.ContainerExecInspect(ctx, created.ID)
	if err != nil {
		return fmt.Errorf("failed to inspect secret delivery exec: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("secret delivery exited with code %d", res.ExitCode)
	}
	return nil
}
