package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/sandbox"
)

// opencodeUID is the unprivileged user the project container runs OpenCode as.
const opencodeUID = 1000

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

// EnsureProjectContainer ensures a hardened project container is running for the given project.
//
// Container provisioning is disabled pending Phase 1B and Gates 0C/0D/0G. The refusal is
// unconditional and is the first statement: no validation, lock, route cache, or Docker call precedes it.
func (m *projectSandboxManager) EnsureProjectContainer(ctx context.Context, project *models.Project, imageTag string) (*ProjectRouteHandle, error) {
	return nil, ErrQuarantineActive
}

// ensureProjectContainerDisabledDraft is the original provisioning draft, intentionally unreferenced.
// It is disabled pending Phase 1B/Gates 0C/0D/0G.
func (m *projectSandboxManager) ensureProjectContainerDisabledDraft(ctx context.Context, project *models.Project, imageTag string) (*ProjectRouteHandle, error) {
	if project == nil || project.ID == "" {
		return nil, errors.New("project and project.ID are required")
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
	dataVolume := "atm-project-data-" + project.ID
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
	env = append(env, "OPENCODE_SERVER_PORT=4096")

	// 5. Container & Host Configuration
	containerCfg := &container.Config{
		Image: imageTag,
		Env:   env,
	}

	pidsLimit := int64(256)
	hostCfg := &container.HostConfig{
		ReadonlyRootfs: true,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges:true"},
		Binds: []string{
			fmt.Sprintf("%s:/workspaces:rw", dataVolume),
			fmt.Sprintf("%s:/var/lib/opencode:rw", stateVolume),
		},
		Tmpfs: map[string]string{
			"/tmp":         "rw,noexec,nosuid,size=512m",
			"/run/secrets": "rw,noexec,nosuid,size=1m,uid=1000,gid=1000,mode=0700",
		},
		Resources: container.Resources{
			Memory:    4 * 1024 * 1024 * 1024, // 4GB
			NanoCPUs:  2 * 1e9,                // 2 CPUs
			PidsLimit: &pidsLimit,
		},
		PortBindings:    nil,
		PublishAllPorts: false,
		NetworkMode:     container.NetworkMode(netName),
	}

	netConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			netName: {},
		},
	}

	// 6. Create container
	createResp, err := m.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, netConfig, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to create project container %s: %w", containerName, err)
	}
	containerID := createResp.ID

	// Helper for fail-closed rollback
	rollback := func(cause error) (*ProjectRouteHandle, error) {
		_ = m.dockerCli.ContainerStop(ctx, containerID, container.StopOptions{})
		_ = m.dockerCli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
		delete(m.routes, project.ID)
		return nil, cause
	}

	// 7. Start container
	if err := m.dockerCli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return rollback(fmt.Errorf("failed to start project container: %w", err))
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
	const script = "umask 077 && cat > /run/secrets/opencode.tmp && mv /run/secrets/opencode.tmp /run/secrets/opencode"
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
