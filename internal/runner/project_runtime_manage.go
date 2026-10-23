package runner

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"agent-task-manager/internal/models"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// Container states reported by ProjectRuntimeStatus.
const (
	RuntimeStateRunning  = "running"
	RuntimeStateStarting = "starting" // a start is in flight, or the container is up but not running yet
	RuntimeStateStopped  = "stopped"  // the container exists but is not running
	RuntimeStateAbsent   = "absent"   // no container has been created for the project yet
)

const (
	runtimeStopGrace    = 10 * time.Second
	runtimeLogsMaxBytes = 256 << 10
	runtimeLogsMaxTail  = 2000
)

// ProjectRuntimeInfo is what the management page shows about a project's container.
type ProjectRuntimeInfo struct {
	State            string     `json:"state"`
	ContainerID      string     `json:"container_id,omitempty"` // short form
	Image            string     `json:"image,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CPUPercent       *float64   `json:"cpu_percent,omitempty"`
	MemoryBytes      *uint64    `json:"memory_bytes,omitempty"`
	MemoryLimitBytes *uint64    `json:"memory_limit_bytes,omitempty"`
}

// statsReader is the optional one-shot stats call of the real Docker client; the narrow DockerClient interface
// does not carry it, so stats are simply left out when the client has no such method.
type statsReader interface {
	ContainerStatsOneShot(ctx context.Context, containerID string) (container.StatsResponseReader, error)
}

func projectContainerName(projectID string) string { return "atm-project-" + projectID }

// ProjectRuntimeStatus reports the state of the project's container, looked up by name so it is right even after the
// control plane restarted and lost its route cache.
func (r *AgentTaskRunner) ProjectRuntimeStatus(ctx context.Context, project models.Project) (*ProjectRuntimeInfo, error) {
	if r.dockerCli == nil || r.projectSandboxMgr == nil {
		return nil, ErrRuntimeExecutionUnavailable
	}
	info := &ProjectRuntimeInfo{State: RuntimeStateAbsent}
	inspect, err := r.dockerCli.ContainerInspect(ctx, projectContainerName(project.ID))
	if err != nil {
		if isDockerNotFound(err) {
			info.State = r.startingOr(project.ID, RuntimeStateAbsent)
			return info, nil
		}
		return nil, err
	}
	if inspect.ContainerJSONBase != nil {
		info.ContainerID = shortID(inspect.ID)
		if st := inspect.State; st != nil {
			info.State = RuntimeStateStopped
			if st.Running {
				info.State = RuntimeStateRunning
				if t, err := time.Parse(time.RFC3339Nano, st.StartedAt); err == nil {
					info.StartedAt = &t
				}
			}
		}
	}
	if inspect.Config != nil {
		info.Image = inspect.Config.Image
	}
	info.State = r.startingOr(project.ID, info.State)
	if info.State == RuntimeStateRunning {
		r.addStats(ctx, inspect.ID, info)
	}
	return info, nil
}

// startingOr reports "starting" while a background start of the project is in flight and the container is not up.
func (r *AgentTaskRunner) startingOr(projectID, state string) string {
	if state == RuntimeStateRunning {
		return state
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.warming[projectID] {
		return RuntimeStateStarting
	}
	return state
}

func (r *AgentTaskRunner) addStats(ctx context.Context, containerID string, info *ProjectRuntimeInfo) {
	sr, ok := r.dockerCli.(statsReader)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resp, err := sr.ContainerStatsOneShot(ctx, containerID)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var s container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return
	}
	// Docker's own formula: the CPU used between the two samples over the system's, scaled by the CPU count.
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	if cpuDelta >= 0 && sysDelta > 0 {
		cpus := float64(s.CPUStats.OnlineCPUs)
		if cpus == 0 {
			cpus = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
		}
		pct := cpuDelta / sysDelta * cpus * 100
		info.CPUPercent = &pct
	}
	// The page cache is reclaimable, so it is not counted as the container's use (what `docker stats` shows).
	used := s.MemoryStats.Usage
	if cache, ok := s.MemoryStats.Stats["inactive_file"]; ok && cache < used {
		used -= cache
	}
	limit := s.MemoryStats.Limit
	info.MemoryBytes, info.MemoryLimitBytes = &used, &limit
}

// StopProjectRuntime stops the project's container. A task still running in it fails; the caller decides whether that
// is acceptable.
func (r *AgentTaskRunner) StopProjectRuntime(ctx context.Context, project models.Project) error {
	if r.dockerCli == nil || r.projectSandboxMgr == nil {
		return ErrRuntimeExecutionUnavailable
	}
	// Drops the route and the gateway lease when this process started the container.
	if err := r.projectSandboxMgr.StopProjectContainer(ctx, project.ID, runtimeStopGrace); err != nil && !isDockerNotFound(err) {
		return err
	}
	// A container left over from before a control-plane restart has no route, so stop it by name as well.
	timeout := int(runtimeStopGrace.Seconds())
	err := r.dockerCli.ContainerStop(ctx, projectContainerName(project.ID), container.StopOptions{Timeout: &timeout})
	if err != nil && !isDockerNotFound(err) {
		return err
	}
	return nil
}

// RestartProjectRuntime stops the project's container and starts it again in the background.
func (r *AgentTaskRunner) RestartProjectRuntime(ctx context.Context, project models.Project) error {
	if err := r.StopProjectRuntime(ctx, project); err != nil {
		return err
	}
	return r.WarmProjectRuntime(project)
}

// ProjectRuntimeLogs returns the last tail lines of the container's output, capped in size.
func (r *AgentTaskRunner) ProjectRuntimeLogs(ctx context.Context, project models.Project, tail int) (string, error) {
	if r.dockerCli == nil || r.projectSandboxMgr == nil {
		return "", ErrRuntimeExecutionUnavailable
	}
	if tail <= 0 || tail > runtimeLogsMaxTail {
		tail = 200
	}
	rc, err := r.dockerCli.ContainerLogs(ctx, projectContainerName(project.ID), container.LogsOptions{
		ShowStdout: true, ShowStderr: true, Tail: strconv.Itoa(tail), Timestamps: true,
	})
	if err != nil {
		if isDockerNotFound(err) {
			return "", nil
		}
		return "", err
	}
	defer rc.Close()

	// A container without a TTY multiplexes stdout and stderr; split them back into one stream of lines.
	var out cappedBuffer
	out.max = runtimeLogsMaxBytes
	// An error here only means the stream was cut short; what was read is still shown.
	_, _ = stdcopy.StdCopy(&out, &out, rc)
	return strings.ToValidUTF8(out.buf.String(), "�"), nil
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func isDockerNotFound(err error) bool {
	return err != nil && (strings.Contains(strings.ToLower(err.Error()), "no such container") ||
		strings.Contains(strings.ToLower(err.Error()), "not found"))
}
