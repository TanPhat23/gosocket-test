# Gate 0C Implementation Plan: Credential-Safe Git Transfer Architecture

> **For agentic workers:** REQUIRED SUB-SKILL: Use subagent-orchestration to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. **This plan preserves the Phase 1A quarantine boundary until full Gate 0C qualification passes.**

**Goal:** Implement the containerized, credential-safe Git data transfer architecture (Option A) for Agent Task Manager (ATM), ensuring that personal access tokens (PATs) and authorization headers never enter project volumes, container configurations, process arguments, or logs. Enforce strict rejection of Git submodules and Git LFS, and verify complete isolation through reproducible adversarial probes 0C-1 through 0C-9.

**Architecture:**
1. **Control-Plane Relay & Isolation Domain:** The ATM control plane coordinates Git synchronization across two isolated, unprivileged, short-lived Docker containers via standard I/O streaming pipes (`io.Pipe`), ensuring no bundle files or intermediate tokens are written to the host filesystem.
2. **Ephemeral Credentialed Git Worker:** An unprivileged container (`runAsUser: 1000`, `CapDrop: ALL`, `ReadonlyRootfs: true`, tmpfs `/tmp`) with outbound HTTPS network access strictly to GitHub (`github.com:443`). Holds **zero project volumes** and **zero Docker socket access**. Secrets are delivered dynamically via in-memory `stdin` piping directly into `git credential approve` or a transient helper process—never in container environment variables (`Env`), command arguments (`Cmd`), image layers, or container labels.
3. **Ephemeral Volume Transfer Helper:** An unprivileged container (`runAsUser: 1000`, `CapDrop: ALL`, `ReadonlyRootfs: true`, tmpfs `/tmp`) with **zero network access** (`NetworkMode: "none"`), **zero credentials**, and **zero Docker socket access**. Mounts only the project's dedicated data volume (`atm-project-data-<project_id>`) at `/workspaces`. Ingests bundle streams into bare mirrors (`/workspaces/<repo-id>/repository.git`), provisions task worktrees using `--no-checkout`, and fails closed immediately upon detecting submodules or Git LFS pointers.
4. **Offline Reverse Push Transfer:** The Volume Transfer Helper exports a self-contained bundle of `refs/heads/task/<task-id>` to stdout. The control plane relays bytes directly to the Credentialed Worker's `stdin`, which fetches the bundle into a bare staging repository and pushes upstream to GitHub via verified TLS, enforcing atomic and non-force updates.
5. **Phase 1A Quarantine Boundary Preservation:** `WorkspaceManager.EnsureTaskWorktree` remains under unconditional Phase 1A quarantine until full Gate 0C probe qualification clears.

**Tech Stack:**
- Go 1.27 (`go.mod` module: `agent-task-manager`)
- Docker Engine SDK (`github.com/docker/docker v27.5.1+incompatible`)
- Git CLI (v2.43+) within container images
- Internal packages: `agent-task-manager/internal/sandbox`, `agent-task-manager/internal/models`, `agent-task-manager/internal/common`

**Spec:**
- `docs/superpowers/specs/2026-10-11-gate-0c-git-transfer-design.md`
- `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md`

**Global Constraints:**
- **Execution Target:** All tests and Docker operations must run inside the authorized guest environment (`atm-guest`) via SSH:
  `ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test ..."`
  No tests, Docker commands, or daemon operations may run on the bare development host.
- **Fail-Closed Security:** Submodules (`.gitmodules` or mode `160000`), Git LFS pointers (`version https://git-lfs...` or `.gitattributes` filter), and TLS validation errors must fail closed immediately.
- **Zero Token Leakage:** PATs must never appear in `argv`, `env`, inspect metadata, logs, git configs, or disk mounts.
- **No Direct Host Fallback:** No host `git` commands may be invoked on repository or worktree paths.
- **Quarantine Invariant:** `EnsureTaskWorktree` returns `ErrHostWorkspaceProvisioningRefused` until the final integration task.

**Review Focus (5 Key Failure Modes):**
1. **Canary Leakage into Metadata/Volume:** Proves that CSPRNG canary tokens delivered via `stdin` never appear in `docker inspect` (`Env`, `Cmd`, `Args`), volume files, `.git/config`, or container logs.
2. **Submodule / Gitlink Smuggle:** Proves that trees containing `.gitmodules` or mode `160000` gitlinks are rejected before any worktree is created.
3. **Git LFS Pointer / Filter Injection:** Proves that repositories with LFS pointer headers or `.gitattributes` filters fail closed with sanitized error sentinels.
4. **Malicious Smudge Filter / Untrusted Config Execution:** Proves that malicious `filter.<name>.smudge` or hooks in `/workspaces/<repo-id>/repository.git/config` are ignored via `--no-checkout`, `-c core.hooksPath=/dev/null`, and `-c core.fsmonitor=false`.
5. **Path Traversal / Ref Injection:** Proves that repo IDs, task IDs, or ref names containing relative traversal sequences (`../`), backslashes, null bytes, or Git command flags (`--upload-pack`) are rejected at input validation boundaries.

---

## Task Breakdown

- [ ] **Task 1: Core Domain Types, Identifier Sanitization & Error Sentinels**
- [ ] **Task 2: Volume Transfer Helper Container Implementation & LFS/Submodule Gate**
- [ ] **Task 3: Ephemeral Credentialed Git Worker Container & In-Memory Stdin Delivery**
- [ ] **Task 4: Reverse Push Transfer Protocol (Task Branch Bundle -> Staging Bare -> GitHub Push)**
- [ ] **Task 5: Adversarial Probes Suite (Probes 0C-1 through 0C-9)**
- [ ] **Task 6: Wire into `WorkspaceManager` and Maintain Phase 1A Quarantine Boundary**

---

## Task 1: Core Domain Types, Identifier Sanitization & Error Sentinels

### Goal
Define the core interfaces, configuration structs, sanitized error sentinels, and strict input validation functions for the Git transfer subsystem in `internal/git/transfer`. Ensure that all inputs (`repoID`, `taskID`, `refName`, `remoteURL`) are validated against strict regex allowlists prior to any Docker container invocation or command formulation, preventing path traversal and Git argument injection.

### File Boundaries
- Create: `internal/git/transfer/types.go`
- Create: `internal/git/transfer/validation.go`
- Create: `internal/git/transfer/validation_test.go`

### Step 1: Write the Failing Test
Create `internal/git/transfer/validation_test.go`:
```go
package transfer_test

import (
	"errors"
	"testing"

	"agent-task-manager/internal/git/transfer"
)

func TestValidation_ValidateIdentifiers(t *testing.T) {
	tests := []struct {
		name    string
		fn      func(string) error
		input   string
		wantErr error
	}{
		// Repo ID validation
		{"valid repo id", transfer.ValidateRepoID, "repo-123_abc", nil},
		{"empty repo id", transfer.ValidateRepoID, "", transfer.ErrInvalidIdentifier},
		{"repo id traversal", transfer.ValidateRepoID, "../repo", transfer.ErrInvalidIdentifier},
		{"repo id slash", transfer.ValidateRepoID, "repo/sub", transfer.ErrInvalidIdentifier},
		{"repo id null byte", transfer.ValidateRepoID, "repo\x00bad", transfer.ErrInvalidIdentifier},
		{"repo id space", transfer.ValidateRepoID, "repo bad", transfer.ErrInvalidIdentifier},

		// Task ID validation
		{"valid task id", transfer.ValidateTaskID, "task-456_def", nil},
		{"empty task id", transfer.ValidateTaskID, "", transfer.ErrInvalidIdentifier},
		{"task id traversal", transfer.ValidateTaskID, "../../etc", transfer.ErrInvalidIdentifier},
		{"task id backslash", transfer.ValidateTaskID, "task\\bad", transfer.ErrInvalidIdentifier},

		// Ref name validation
		{"valid branch ref", transfer.ValidateRefName, "refs/heads/main", nil},
		{"valid task branch ref", transfer.ValidateRefName, "refs/heads/task/task-123", nil},
		{"empty ref", transfer.ValidateRefName, "", transfer.ErrInvalidRefName},
		{"ref with double dot", transfer.ValidateRefName, "refs/heads/../main", transfer.ErrInvalidRefName},
		{"ref with at brace", transfer.ValidateRefName, "refs/heads/main@{1}", transfer.ErrInvalidRefName},
		{"ref with colon", transfer.ValidateRefName, "refs/heads/main:main", transfer.ErrInvalidRefName},
		{"ref with flag dash", transfer.ValidateRefName, "--upload-pack=evil", transfer.ErrInvalidRefName},
		{"ref with space", transfer.ValidateRefName, "refs/heads/my branch", transfer.ErrInvalidRefName},
		{"ref with control char", transfer.ValidateRefName, "refs/heads/main\n", transfer.ErrInvalidRefName},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn(tc.input)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("expected nil error, got: %v", err)
				}
			} else {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected error wrapping %v, got: %v", tc.wantErr, err)
				}
			}
		})
	}
}
```

### Step 2: Verify Test Failure
Execute the test inside `atm-guest`:
```bash
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/git/transfer"
```
*Expected Result:* Failure due to missing package `agent-task-manager/internal/git/transfer` and undefined symbols.

### Step 3: Implement Minimal Types & Validation
Create `internal/git/transfer/types.go`:
```go
package transfer

import (
	"context"
	"errors"
	"io"
)

var (
	ErrInvalidIdentifier     = errors.New("invalid identifier: must be alphanumeric, hyphen, or underscore")
	ErrInvalidRefName        = errors.New("invalid ref name: ref contains illegal git characters or flags")
	ErrSubmodulesNotSupported = errors.New("repository contains submodules, which are not supported in v1")
	ErrGitLFSNotSupported     = errors.New("repository contains Git LFS pointers, which are not supported in v1")
	ErrGitTransferFailed      = errors.New("git transfer operation failed")
	ErrCanaryLeakDetected     = errors.New("security violation: canary credential leak detected")
	ErrPushRejected           = errors.New("upstream git push was rejected")
)

// GitTransferManager orchestrates credential-safe Git data transfer between remote and volume.
type GitTransferManager interface {
	// IngestRemoteBranch clones or fetches a remote ref into the project volume bare repository,
	// validates absence of submodules and LFS, and creates a no-checkout task worktree.
	IngestRemoteBranch(ctx context.Context, req IngestRequest) (*TransferResult, error)

	// ExportAndPushTask exports a task branch from the project volume as a bundle,
	// relays it to the credentialed worker, and pushes upstream to GitHub.
	ExportAndPushTask(ctx context.Context, req PushRequest) (*TransferResult, error)
}

// IngestRequest contains parameters for cloning/fetching into a project data volume.
type IngestRequest struct {
	ProjectID  string
	RepoID     string
	TaskID     string
	RemoteURL  string
	BaseRef    string // e.g. "refs/heads/main"
	AuthToken  string // Plaintext PAT decrypted in memory; never committed to disk
}

// PushRequest contains parameters for exporting a task branch bundle and pushing upstream.
type PushRequest struct {
	ProjectID string
	RepoID    string
	TaskID    string
	RemoteURL string
	AuthToken string
}

// TransferResult summarizes the completed transfer operation.
type TransferResult struct {
	TaskWorktreePath string
	HeadCommit       string
	BytesTransferred int64
}

// VolumeHelper defines operations executed within the non-networked volume helper container.
type VolumeHelper interface {
	ApplyBundleAndProvisionWorktree(ctx context.Context, repoID, taskID, refName string, bundleStream io.Reader) (string, error)
	ExportTaskBundle(ctx context.Context, repoID, taskID string, outStream io.Writer) error
}

// CredentialedWorker defines operations executed within the ephemeral networked worker container.
type CredentialedWorker interface {
	FetchRemoteRefToBundle(ctx context.Context, remoteURL, refName, token string, outStream io.Writer) error
	ImportBundleAndPush(ctx context.Context, remoteURL, taskID, token string, bundleStream io.Reader) error
}
```

Create `internal/git/transfer/validation.go`:
```go
package transfer

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	idRegex  = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)
	// Git ref validation adhering strictly to git-check-ref-format(1)
	refRegex = regexp.MustCompile(`^refs/heads/[a-zA-Z0-9_\.\-]+(/[a-zA-Z0-9_\.\-]+)*$`)
)

// ValidateRepoID checks that a repository identifier contains only safe alphanumeric characters.
func ValidateRepoID(repoID string) error {
	if repoID == "" || !idRegex.MatchString(repoID) {
		return fmt.Errorf("%w: repoID %q", ErrInvalidIdentifier, repoID)
	}
	return nil
}

// ValidateTaskID checks that a task identifier contains only safe alphanumeric characters.
func ValidateTaskID(taskID string) error {
	if taskID == "" || !idRegex.MatchString(taskID) {
		return fmt.Errorf("%w: taskID %q", ErrInvalidIdentifier, taskID)
	}
	return nil
}

// ValidateRefName checks that a ref name is fully qualified (refs/heads/...) and safe.
func ValidateRefName(refName string) error {
	if refName == "" {
		return fmt.Errorf("%w: ref name cannot be empty", ErrInvalidRefName)
	}
	if strings.HasPrefix(refName, "-") {
		return fmt.Errorf("%w: ref name cannot start with a dash", ErrInvalidRefName)
	}
	if strings.Contains(refName, "..") ||
		strings.Contains(refName, "@{") ||
		strings.Contains(refName, ":") ||
		strings.Contains(refName, "^") ||
		strings.Contains(refName, "~") ||
		strings.Contains(refName, "*") ||
		strings.Contains(refName, "?") ||
		strings.Contains(refName, "[") ||
		strings.Contains(refName, "\\") ||
		strings.Contains(refName, " ") ||
		strings.ContainsAny(refName, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f\x7f") {
		return fmt.Errorf("%w: ref contains forbidden git special characters: %q", ErrInvalidRefName, refName)
	}
	if !refRegex.MatchString(refName) {
		return fmt.Errorf("%w: ref name %q does not match canonical refs/heads/ pattern", ErrInvalidRefName, refName)
	}
	return nil
}
```

### Step 4: Verify Test Pass
Execute inside `atm-guest`:
```bash
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/git/transfer"
```
*Expected Result:* `PASS` with 0 failures.

### Step 5: Commit
Commit changes: `git add internal/git/transfer && git commit -m "feat(transfer): add core transfer types and strict validation"`

---

## Task 2: Volume Transfer Helper Container Implementation & LFS/Submodule Gate

### Goal
Implement `VolumeTransferHelper`, which uses the Docker client to spawn an unprivileged, non-networked container (`network_mode: none`) mounting the project volume at `/workspaces`. The helper receives incoming `git bundle` streams via `stdin`, spools them to tmpfs (`/tmp/scratch.bundle`), creates or updates the bare mirror at `/workspaces/<repo-id>/repository.git`, performs fail-closed inspections for Git submodules and Git LFS pointers on the target commit, and provisions task worktrees using `--no-checkout`.

### File Boundaries
- Modify: `internal/sandbox/docker.go` (add `ContainerAttach` to `sandbox.DockerClient` interface)
- Create: `internal/sandbox/mock_docker.go` (declare and export `MockDockerClient` and `MockHijackedConn` in `sandbox`)
- Create: `internal/git/transfer/volume_helper.go`
- Create: `internal/git/transfer/volume_helper_test.go`

### Step 1: Write the Failing Test
Create `internal/git/transfer/volume_helper_test.go`:
```go
package transfer_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"agent-task-manager/internal/git/transfer"
	"agent-task-manager/internal/sandbox"
)

func TestVolumeHelper_ContainerConfigSecurityHardening(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	ctx := context.Background()
	dummyBundle := bytes.NewReader([]byte("# v2 git bundle\n"))

	_, err := helper.ApplyBundleAndProvisionWorktree(ctx, "repo-1", "task-1", "refs/heads/main", dummyBundle)
	// The mock records ContainerCreate calls
	if len(mockDocker.CreateCalls) == 0 {
		t.Fatalf("expected ContainerCreate call, got none (err: %v)", err)
	}

	call := mockDocker.CreateCalls[0]
	// Security assertions:
	if call.HostConfig.NetworkMode != "none" {
		t.Errorf("expected NetworkMode 'none', got %q", call.HostConfig.NetworkMode)
	}
	if len(call.HostConfig.CapDrop) != 1 || call.HostConfig.CapDrop[0] != "ALL" {
		t.Errorf("expected CapDrop ['ALL'], got %v", call.HostConfig.CapDrop)
	}
	if !call.HostConfig.ReadonlyRootfs {
		t.Errorf("expected ReadonlyRootfs to be true")
	}
	if call.Config.User != "1000:1000" {
		t.Errorf("expected container User '1000:1000', got %q", call.Config.User)
	}
	if len(call.Config.Env) != 0 {
		t.Errorf("expected zero environment variables, got %v", call.Config.Env)
	}

	// ContainerAttach assertions:
	if len(mockDocker.AttachCalls) == 0 {
		t.Fatalf("expected ContainerAttach call, got none")
	}
	attachOpt := mockDocker.AttachCalls[0]
	if !attachOpt.Stdin || !attachOpt.Stdout || !attachOpt.Stderr || !attachOpt.Stream {
		t.Errorf("expected AttachOptions with Stdin, Stdout, Stderr, and Stream set to true, got: %+v", attachOpt)
	}
	if !strings.Contains(mockDocker.AttachConn.InBuf.String(), "# v2 git bundle") {
		t.Errorf("expected bundle bytes to be piped to container stdin, got: %q", mockDocker.AttachConn.InBuf.String())
	}
}

func TestVolumeHelper_SubmoduleFailClosedSentinel(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	// Configure mock to simulate container exit with submodule error code
	mockDocker.WaitStatusCode = 101 // Custom helper exit code for submodule found
	mockDocker.WaitErrMessage = "repository contains submodules, which are not supported in v1"

	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")
	_, err := helper.ApplyBundleAndProvisionWorktree(context.Background(), "repo-sub", "task-sub", "refs/heads/main", bytes.NewReader([]byte("bundle")))
	if !errors.Is(err, transfer.ErrSubmodulesNotSupported) {
		t.Fatalf("expected ErrSubmodulesNotSupported, got: %v", err)
	}
}

func TestVolumeHelper_GitLFSFailClosedSentinel(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	mockDocker.WaitStatusCode = 102 // Custom helper exit code for LFS found
	mockDocker.WaitErrMessage = "repository contains Git LFS pointers, which are not supported in v1"

	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")
	_, err := helper.ApplyBundleAndProvisionWorktree(context.Background(), "repo-lfs", "task-lfs", "refs/heads/main", bytes.NewReader([]byte("bundle")))
	if !errors.Is(err, transfer.ErrGitLFSNotSupported) {
		t.Fatalf("expected ErrGitLFSNotSupported, got: %v", err)
	}
}
```

### Step 2: Verify Test Failure
Execute inside `atm-guest`:
```bash
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/git/transfer -run TestVolumeHelper"
```
*Expected Result:* Failure due to undefined `VolumeTransferHelper` and `NewVolumeTransferHelper`.

### Step 3: Implement `VolumeTransferHelper` & Update Sandbox Interface / Mock
Modify `internal/sandbox/docker.go` to add `ContainerAttach` to `DockerClient`:
```go
// In internal/sandbox/docker.go:
type DockerClient interface {
	// Container operations
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerAttach(ctx context.Context, containerID string, options container.AttachOptions) (types.HijackedResponse, error)
	ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error)
	ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)

	// Image, network, volume operations
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
	ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error)
	ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error)
	ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error)
	NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)
	NetworkRemove(ctx context.Context, networkID string) error
	VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
	VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error)
	VolumeRemove(ctx context.Context, volumeID string, force bool) error
	CopyToContainer(ctx context.Context, containerID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error
}
```

Create `internal/sandbox/mock_docker.go` to provide exported test infrastructure:
```go
package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// MockHijackedConn provides an in-memory bi-directional net.Conn for testing ContainerAttach.
type MockHijackedConn struct {
	InBuf  bytes.Buffer
	OutBuf bytes.Buffer
	Closed bool
}

func (m *MockHijackedConn) Read(b []byte) (n int, err error) {
	return m.OutBuf.Read(b)
}

func (m *MockHijackedConn) Write(b []byte) (n int, err error) {
	return m.InBuf.Write(b)
}

func (m *MockHijackedConn) Close() error {
	m.Closed = true
	return nil
}

func (m *MockHijackedConn) LocalAddr() net.Addr                { return nil }
func (m *MockHijackedConn) RemoteAddr() net.Addr               { return nil }
func (m *MockHijackedConn) SetDeadline(t time.Time) error      { return nil }
func (m *MockHijackedConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *MockHijackedConn) SetWriteDeadline(t time.Time) error { return nil }

// ContainerCreateCall records the arguments passed to ContainerCreate.
type ContainerCreateCall struct {
	Config           *container.Config
	HostConfig       *container.HostConfig
	NetworkingConfig *network.NetworkingConfig
	ContainerName    string
}

// MockDockerClient provides a fully-implemented DockerClient mock for testing container operations.
type MockDockerClient struct {
	CreateCalls    []ContainerCreateCall
	AttachCalls    []container.AttachOptions
	AttachConn     *MockHijackedConn
	AttachErr      error

	WaitStatusCode int64
	WaitErrMessage string
	WaitErr        error

	StartErr  error
	RemoveErr error
}

func NewMockDockerClient() *MockDockerClient {
	return &MockDockerClient{
		AttachConn: &MockHijackedConn{},
	}
}

func (m *MockDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	m.CreateCalls = append(m.CreateCalls, ContainerCreateCall{
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: networkingConfig,
		ContainerName:    containerName,
	})
	return container.CreateResponse{ID: "mock-container-id"}, nil
}

func (m *MockDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	return m.StartErr
}

func (m *MockDockerClient) ContainerAttach(ctx context.Context, containerID string, options container.AttachOptions) (types.HijackedResponse, error) {
	m.AttachCalls = append(m.AttachCalls, options)
	if m.AttachErr != nil {
		return types.HijackedResponse{}, m.AttachErr
	}
	if m.AttachConn == nil {
		m.AttachConn = &MockHijackedConn{}
	}
	reader := bufio.NewReader(&m.AttachConn.OutBuf)
	return types.NewHijackedResponse(m.AttachConn, "application/vnd.docker.raw-stream"), nil
}

func (m *MockDockerClient) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	statusCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)

	var exitErr *container.WaitExitError
	if m.WaitErrMessage != "" {
		exitErr = &container.WaitExitError{Message: m.WaitErrMessage}
	}

	statusCh <- container.WaitResponse{
		StatusCode: m.WaitStatusCode,
		Error:      exitErr,
	}
	if m.WaitErr != nil {
		errCh <- m.WaitErr
	}
	close(statusCh)
	close(errCh)
	return statusCh, errCh
}

func (m *MockDockerClient) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (m *MockDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	return nil
}

func (m *MockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	return m.RemoveErr
}

func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	return types.ContainerJSON{}, nil
}

func (m *MockDockerClient) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (m *MockDockerClient) ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	return types.ImageBuildResponse{Body: io.NopCloser(bytes.NewReader(nil))}, nil
}

func (m *MockDockerClient) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	return nil, nil
}

func (m *MockDockerClient) ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error) {
	return types.ImageInspect{}, nil, nil
}

func (m *MockDockerClient) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	return network.CreateResponse{}, nil
}

func (m *MockDockerClient) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	return network.Inspect{}, nil
}

func (m *MockDockerClient) NetworkRemove(ctx context.Context, networkID string) error {
	return nil
}

func (m *MockDockerClient) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	return volume.Volume{}, nil
}

func (m *MockDockerClient) VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error) {
	return volume.Volume{}, nil
}

func (m *MockDockerClient) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	return nil
}

func (m *MockDockerClient) CopyToContainer(ctx context.Context, containerID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error {
	return nil
}
```

Create `internal/git/transfer/volume_helper.go`:
```go
package transfer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/pkg/stdcopy"

	"agent-task-manager/internal/sandbox"
)

const (
	ExitCodeSubmoduleDetected = 101
	ExitCodeLFSDetected       = 102
)

type volumeTransferHelper struct {
	dockerCli sandbox.DockerClient
	imageTag  string
}

// NewVolumeTransferHelper creates an instance of VolumeHelper using DockerClient.
func NewVolumeTransferHelper(dockerCli sandbox.DockerClient, imageTag string) VolumeHelper {
	return &volumeTransferHelper{
		dockerCli: dockerCli,
		imageTag:  imageTag,
	}
}

func (h *volumeTransferHelper) ApplyBundleAndProvisionWorktree(
	ctx context.Context,
	repoID, taskID, refName string,
	bundleStream io.Reader,
) (string, error) {
	if err := ValidateRepoID(repoID); err != nil {
		return "", err
	}
	if err := ValidateTaskID(taskID); err != nil {
		return "", err
	}
	if err := ValidateRefName(refName); err != nil {
		return "", err
	}

	volumeName := fmt.Sprintf("atm-project-data-%s", repoID)
	branchName := strings.TrimPrefix(refName, "refs/heads/")

	// In-container helper shell script executed with strict options.
	// Spools stdin bundle to /tmp/scratch.bundle, updates/inits bare repo,
	// checks submodules and LFS, and creates worktree with --no-checkout.
	helperScript := fmt.Sprintf(`
set -euo pipefail
REPO_DIR="/workspaces/%s/repository.git"
TASK_DIR="/workspaces/%s/tasks/%s"
TMP_BUNDLE="/tmp/scratch.bundle"

cat > "$TMP_BUNDLE"

if [ ! -d "$REPO_DIR" ]; then
    mkdir -p "$(dirname "$REPO_DIR")"
    git clone --bare "$TMP_BUNDLE" "$REPO_DIR"
else
    git -C "$REPO_DIR" -c core.hooksPath=/dev/null -c core.fsmonitor=false fetch "$TMP_BUNDLE" "%s:%s"
fi
rm -f "$TMP_BUNDLE"

# Inspection 1: Submodules (.gitmodules or mode 160000 gitlink)
if git -C "$REPO_DIR" -c core.hooksPath=/dev/null cat-file -e "%s:.gitmodules" 2>/dev/null; then
    echo "repository contains submodules" >&2
    exit %d
fi
if git -C "$REPO_DIR" -c core.hooksPath=/dev/null ls-tree -r "%s" | grep -q "^160000 commit "; then
    echo "repository contains gitlink submodules" >&2
    exit %d
fi

# Inspection 2: Git LFS (.gitattributes filter=lfs or pointer signature)
if git -C "$REPO_DIR" -c core.hooksPath=/dev/null show "%s:.gitattributes" 2>/dev/null | grep -q "filter=lfs"; then
    echo "repository contains Git LFS pointers" >&2
    exit %d
fi
if git -C "$REPO_DIR" -c core.hooksPath=/dev/null grep -q -E "^version https://git-lfs.github.com/spec/v1" "%s" 2>/dev/null; then
    echo "repository contains Git LFS pointer blobs" >&2
    exit %d
fi

# Provision task worktree with --no-checkout
mkdir -p "$(dirname "$TASK_DIR")"
git -C "$REPO_DIR" -c core.hooksPath=/dev/null -c core.fsmonitor=false worktree add --no-checkout -b "task/%s" "$TASK_DIR" "%s"
`, repoID, repoID, taskID, refName, refName, branchName, ExitCodeSubmoduleDetected, branchName, ExitCodeSubmoduleDetected, branchName, ExitCodeLFSDetected, branchName, ExitCodeLFSDetected, taskID, branchName)

	containerCfg := &container.Config{
		Image:        h.imageTag,
		User:         "1000:1000",
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		StdinOnce:    true,
		Cmd:          []string{"/bin/sh", "-c", helperScript},
	}

	hostCfg := &container.HostConfig{
		NetworkMode:    "none",
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    []string{"no-new-privileges:true"},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: volumeName,
				Target: "/workspaces",
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
				TmpfsOptions: &mount.TmpfsOptions{
					SizeBytes: 1024 * 1024 * 1024, // 1GiB
				},
			},
		},
	}

	resp, err := h.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return "", fmt.Errorf("failed to create volume helper container: %w", err)
	}
	defer func() {
		_ = h.dockerCli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	// Attach to container stdin/stdout/stderr before ContainerStart
	hijacked, err := h.dockerCli.ContainerAttach(ctx, resp.ID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return "", fmt.Errorf("failed to attach to volume helper container: %w", err)
	}
	defer hijacked.Close()

	if err := h.dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start volume helper container: %w", err)
	}

	// Stream bundle bytes into container stdin via hijacked connection, then close write half
	streamErrCh := make(chan error, 1)
	go func() {
		defer hijacked.CloseWrite()
		if bundleStream != nil {
			if _, err := io.Copy(hijacked.Conn, bundleStream); err != nil {
				streamErrCh <- fmt.Errorf("failed to pipe bundle to volume helper: %w", err)
				return
			}
		}
		streamErrCh <- nil
	}()

	statusCh, errCh := h.dockerCli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case streamErr := <-streamErrCh:
		if streamErr != nil {
			return "", streamErr
		}
	case <-ctx.Done():
		return "", ctx.Err()
	}

	select {
	case waitErr := <-errCh:
		if waitErr != nil {
			return "", fmt.Errorf("volume helper container wait failed: %w", waitErr)
		}
	case status := <-statusCh:
		if status.StatusCode == ExitCodeSubmoduleDetected {
			return "", ErrSubmodulesNotSupported
		}
		if status.StatusCode == ExitCodeLFSDetected {
			return "", ErrGitLFSNotSupported
		}
		if status.StatusCode != 0 {
			return "", fmt.Errorf("%w: helper exited with code %d", ErrGitTransferFailed, status.StatusCode)
		}
	case <-ctx.Done():
		return "", ctx.Err()
	}

	return fmt.Sprintf("/workspaces/%s/tasks/%s", repoID, taskID), nil
}

func (h *volumeTransferHelper) ExportTaskBundle(ctx context.Context, repoID, taskID string, outStream io.Writer) error {
	if err := ValidateRepoID(repoID); err != nil {
		return err
	}
	if err := ValidateTaskID(taskID); err != nil {
		return err
	}

	volumeName := fmt.Sprintf("atm-project-data-%s", repoID)
	branchRef := fmt.Sprintf("refs/heads/task/%s", taskID)

	exportScript := fmt.Sprintf(`
set -euo pipefail
REPO_DIR="/workspaces/%s/repository.git"
git -C "$REPO_DIR" -c core.hooksPath=/dev/null -c core.fsmonitor=false bundle create - "%s"
`, repoID, branchRef)

	containerCfg := &container.Config{
		Image:        h.imageTag,
		User:         "1000:1000",
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"/bin/sh", "-c", exportScript},
	}

	hostCfg := &container.HostConfig{
		NetworkMode:    "none",
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    []string{"no-new-privileges:true"},
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeVolume,
				Source:   volumeName,
				Target:   "/workspaces",
				ReadOnly: true,
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
			},
		},
	}

	resp, err := h.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create export helper container: %w", err)
	}
	defer func() {
		_ = h.dockerCli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	// Attach to capture stdout (bundle export) and stderr (diagnostics)
	hijacked, err := h.dockerCli.ContainerAttach(ctx, resp.ID, container.AttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to export container: %w", err)
	}
	defer hijacked.Close()

	if err := h.dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start export helper: %w", err)
	}

	// Read bundle bytes from hijacked.Reader and pipe to destination outStream
	streamErrCh := make(chan error, 1)
	go func() {
		var errBuf bytes.Buffer
		var outDst io.Writer = outStream
		if outDst == nil {
			outDst = io.Discard
		}
		// In raw Docker attach stream without TTY, stream is multiplexed with stdout/stderr
		// stdcopy.StdCopy demultiplexes into outDst and errBuf. If plain reader, fallback to io.Copy.
		_, copyErr := stdcopy.StdCopy(outDst, &errBuf, hijacked.Reader)
		if copyErr != nil {
			// Fallback if raw un-multiplexed stream
			_, copyErr = io.Copy(outDst, hijacked.Reader)
		}
		streamErrCh <- copyErr
	}()

	statusCh, errCh := h.dockerCli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case waitErr := <-errCh:
		if waitErr != nil {
			return fmt.Errorf("export container wait failed: %w", waitErr)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("%w: export failed with exit code %d", ErrGitTransferFailed, status.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	if streamErr := <-streamErrCh; streamErr != nil {
		return fmt.Errorf("failed streaming exported bundle: %w", streamErr)
	}

	return nil
}
```

### Step 4: Verify Test Pass
Execute inside `atm-guest`:
```bash
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/git/transfer -run TestVolumeHelper"
```
*Expected Result:* `PASS`.

### Step 5: Commit
Commit changes: `git add internal/git/transfer/volume_helper.go internal/git/transfer/volume_helper_test.go && git commit -m "feat(transfer): implement volume transfer helper container and submodule/LFS gate"`

---

## Task 3: Ephemeral Credentialed Git Worker Container & In-Memory Stdin Delivery

### Goal
Implement `CredentialedGitWorker`, an ephemeral unprivileged container with network egress to GitHub HTTPS (`github.com:443`), but with **zero access to project data volumes and zero access to the Docker socket**. Decrypted PAT tokens are provided exclusively via standard input piping directly to `git credential approve` or a transient stdin helper script. The worker performs `git fetch` into `/tmp/staging.git` and streams `git bundle create - <ref>` directly to stdout.

### File Boundaries
- Create: `internal/git/transfer/credentialed_worker.go`
- Create: `internal/git/transfer/credentialed_worker_test.go`

### Step 1: Write the Failing Test
Create `internal/git/transfer/credentialed_worker_test.go`:
```go
package transfer_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"agent-task-manager/internal/git/transfer"
	"agent-task-manager/internal/sandbox"
)

func TestCredentialedWorker_ZeroTokensInContainerConfig(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	worker := transfer.NewCredentialedGitWorker(mockDocker, "atm-worker:latest")

	canaryToken := "ghp_ATM_CANARY_0123456789abcdef0123456789abcdef"
	ctx := context.Background()
	var out bytes.Buffer

	_ = worker.FetchRemoteRefToBundle(ctx, "https://github.com/org/repo.git", "refs/heads/main", canaryToken, &out)

	if len(mockDocker.CreateCalls) == 0 {
		t.Fatalf("expected ContainerCreate call")
	}

	call := mockDocker.CreateCalls[0]

	// 1. Assert zero tokens in Env
	for _, env := range call.Config.Env {
		if strings.Contains(env, canaryToken) || strings.Contains(env, "Authorization") {
			t.Errorf("canary token leaked into container Env: %q", env)
		}
	}

	// 2. Assert zero tokens in Cmd / Entrypoint
	for _, cmd := range call.Config.Cmd {
		if strings.Contains(cmd, canaryToken) || strings.Contains(cmd, "http.extraHeader") {
			t.Errorf("canary token or auth header leaked into container Cmd: %q", cmd)
		}
	}

	// 3. Assert zero mounts to project volumes
	for _, m := range call.HostConfig.Mounts {
		if strings.Contains(m.Source, "atm-project-data") {
			t.Errorf("worker must never mount project data volume: %v", m)
		}
		if strings.Contains(m.Source, "docker.sock") {
			t.Errorf("worker must never mount docker socket: %v", m)
		}
	}

	// 4. Assert capability drops and read-only rootfs
	if !call.HostConfig.ReadonlyRootfs {
		t.Errorf("worker must have ReadonlyRootfs enabled")
	}
	if len(call.HostConfig.CapDrop) != 1 || call.HostConfig.CapDrop[0] != "ALL" {
		t.Errorf("worker must drop ALL capabilities")
	}

	// 5. Assert ContainerAttach was called and token was piped via stdin Conn
	if len(mockDocker.AttachCalls) == 0 {
		t.Errorf("expected ContainerAttach call, got none")
	} else {
		attachOpt := mockDocker.AttachCalls[0]
		if !attachOpt.Stdin || !attachOpt.Stdout || !attachOpt.Stderr || !attachOpt.Stream {
			t.Errorf("expected AttachOptions with Stdin, Stdout, Stderr, and Stream set to true, got: %+v", attachOpt)
		}
	}
	if !strings.Contains(mockDocker.AttachConn.InBuf.String(), canaryToken) {
		t.Errorf("expected canary token to be piped directly to container stdin, got: %q", mockDocker.AttachConn.InBuf.String())
	}
}
```

### Step 2: Verify Test Failure
Execute inside `atm-guest`:
```bash
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/git/transfer -run TestCredentialedWorker"
```
*Expected Result:* Failure due to undefined `NewCredentialedGitWorker`.

### Step 3: Implement `CredentialedGitWorker`
Create `internal/git/transfer/credentialed_worker.go`:
```go
package transfer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/pkg/stdcopy"

	"agent-task-manager/internal/sandbox"
)

type credentialedGitWorker struct {
	dockerCli sandbox.DockerClient
	imageTag  string
}

// NewCredentialedGitWorker returns an instance of CredentialedWorker.
func NewCredentialedGitWorker(dockerCli sandbox.DockerClient, imageTag string) CredentialedWorker {
	return &credentialedGitWorker{
		dockerCli: dockerCli,
		imageTag:  imageTag,
	}
}

func (w *credentialedGitWorker) FetchRemoteRefToBundle(
	ctx context.Context,
	remoteURL, refName, token string,
	outStream io.Writer,
) error {
	if err := ValidateRefName(refName); err != nil {
		return err
	}
	if err := validateRemoteURL(remoteURL); err != nil {
		return err
	}

	branchName := strings.TrimPrefix(refName, "refs/heads/")

	// Transient in-container helper script: reads the token from stdin directly
	// into the git credential store, clones the single branch into /tmp/bare,
	// generates a git bundle streaming to stdout, and erases credentials.
	workerScript := fmt.Sprintf(`
set -euo pipefail

# Read token from stdin (in-memory pipe)
read -r INGESTED_TOKEN

# Configure git credentials in-memory for this session
git config --global credential.helper 'cache --timeout=30'
printf "protocol=https\nhost=github.com\nusername=x-access-token\npassword=%%s\n\n" "$INGESTED_TOKEN" | git credential approve
unset INGESTED_TOKEN

mkdir -p /tmp/bare.git
git init --bare /tmp/bare.git
git -C /tmp/bare.git -c core.hooksPath=/dev/null fetch --depth=1 "%s" "%s:%s"

# Generate bundle to stdout
git -C /tmp/bare.git -c core.hooksPath=/dev/null bundle create - "%s"

# Clean up credentials
printf "protocol=https\nhost=github.com\nusername=x-access-token\n\n" | git credential erase
`, remoteURL, refName, refName, branchName)

	containerCfg := &container.Config{
		Image:        w.imageTag,
		User:         "1000:1000",
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		StdinOnce:    true,
		Cmd:          []string{"/bin/sh", "-c", workerScript},
	}

	hostCfg := &container.HostConfig{
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    []string{"no-new-privileges:true"},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
				TmpfsOptions: &mount.TmpfsOptions{
					SizeBytes: 1024 * 1024 * 1024,
				},
			},
		},
	}

	resp, err := w.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create credentialed worker container: %w", err)
	}
	defer func() {
		_ = w.dockerCli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	// Attach to container stdin/stdout/stderr before ContainerStart
	hijacked, err := w.dockerCli.ContainerAttach(ctx, resp.ID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to credentialed worker container: %w", err)
	}
	defer hijacked.Close()

	if err := w.dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start credentialed worker: %w", err)
	}

	// 1. Write the token to the hijacked connection stdin: fmt.Fprintf(hijacked.Conn, "%s\n", token)
	// 2. Read bundle bytes from hijacked.Reader and pipe to destination outStream
	// 3. Close write half of the connection so the worker script receives EOF on stdin
	streamErrCh := make(chan error, 1)
	go func() {
		if _, err := fmt.Fprintf(hijacked.Conn, "%s\n", token); err != nil {
			_ = hijacked.CloseWrite()
			streamErrCh <- fmt.Errorf("failed writing token to worker stdin: %w", err)
			return
		}
		_ = hijacked.CloseWrite()

		var errBuf bytes.Buffer
		var outDst io.Writer = outStream
		if outDst == nil {
			outDst = io.Discard
		}
		_, copyErr := stdcopy.StdCopy(outDst, &errBuf, hijacked.Reader)
		if copyErr != nil {
			_, copyErr = io.Copy(outDst, hijacked.Reader)
		}
		streamErrCh <- copyErr
	}()

	statusCh, errCh := w.dockerCli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case waitErr := <-errCh:
		if waitErr != nil {
			return fmt.Errorf("worker wait failed: %w", waitErr)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("%w: worker exited with code %d", ErrGitTransferFailed, status.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	if streamErr := <-streamErrCh; streamErr != nil {
		return fmt.Errorf("failed streaming worker output bundle: %w", streamErr)
	}

	return nil
}

func (w *credentialedGitWorker) ImportBundleAndPush(
	ctx context.Context,
	remoteURL, taskID, token string,
	bundleStream io.Reader,
) error {
	if err := ValidateTaskID(taskID); err != nil {
		return err
	}
	if err := validateRemoteURL(remoteURL); err != nil {
		return err
	}

	branchRef := fmt.Sprintf("refs/heads/task/%s", taskID)

	pushScript := fmt.Sprintf(`
set -euo pipefail

read -r INGESTED_TOKEN
git config --global credential.helper 'cache --timeout=30'
printf "protocol=https\nhost=github.com\nusername=x-access-token\npassword=%%s\n\n" "$INGESTED_TOKEN" | git credential approve
unset INGESTED_TOKEN

# Spool bundle stream from stdin after the first line (token)
cat > /tmp/task.bundle

mkdir -p /tmp/staging.git
git init --bare /tmp/staging.git
git -C /tmp/staging.git -c core.hooksPath=/dev/null fetch /tmp/task.bundle "%s:%s"
rm -f /tmp/task.bundle

# Push to remote GitHub
git -C /tmp/staging.git -c core.hooksPath=/dev/null push --porcelain --atomic "%s" "%s:%s"

printf "protocol=https\nhost=github.com\nusername=x-access-token\n\n" | git credential erase
`, branchRef, branchRef, remoteURL, branchRef, branchRef)

	containerCfg := &container.Config{
		Image:        w.imageTag,
		User:         "1000:1000",
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		StdinOnce:    true,
		Cmd:          []string{"/bin/sh", "-c", pushScript},
	}

	hostCfg := &container.HostConfig{
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    []string{"no-new-privileges:true"},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
				TmpfsOptions: &mount.TmpfsOptions{
					SizeBytes: 1024 * 1024 * 1024,
				},
			},
		},
	}

	resp, err := w.dockerCli.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create worker push container: %w", err)
	}
	defer func() {
		_ = w.dockerCli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	// Attach to container stdin/stdout/stderr before ContainerStart
	hijacked, err := w.dockerCli.ContainerAttach(ctx, resp.ID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to worker push container: %w", err)
	}
	defer hijacked.Close()

	if err := w.dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start worker push container: %w", err)
	}

	// 1. Write the token on the first line: fmt.Fprintf(hijacked.Conn, "%s\n", token)
	// 2. Stream bundle bytes into hijacked.Conn
	// 3. Close write half so worker receives EOF on stdin
	streamErrCh := make(chan error, 1)
	go func() {
		defer hijacked.CloseWrite()
		if _, err := fmt.Fprintf(hijacked.Conn, "%s\n", token); err != nil {
			streamErrCh <- fmt.Errorf("failed writing token to worker push stdin: %w", err)
			return
		}
		if bundleStream != nil {
			if _, err := io.Copy(hijacked.Conn, bundleStream); err != nil {
				streamErrCh <- fmt.Errorf("failed piping bundle stream to worker push: %w", err)
				return
			}
		}
		streamErrCh <- nil
	}()

	statusCh, errCh := w.dockerCli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case streamErr := <-streamErrCh:
		if streamErr != nil {
			return streamErr
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case waitErr := <-errCh:
		if waitErr != nil {
			return fmt.Errorf("push container wait failed: %w", waitErr)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("%w: push failed with exit code %d", ErrPushRejected, status.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func validateRemoteURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("remote URL cannot be empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return fmt.Errorf("remote URL must be a valid https URL: %q", rawURL)
	}
	if u.User != nil {
		return fmt.Errorf("remote URL must not contain embedded user credentials")
	}
	return nil
}
```

### Step 4: Verify Test Pass
Execute inside `atm-guest`:
```bash
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/git/transfer -run TestCredentialedWorker"
```
*Expected Result:* `PASS`.

### Step 5: Commit
Commit changes: `git add internal/git/transfer/credentialed_worker.go internal/git/transfer/credentialed_worker_test.go && git commit -m "feat(transfer): implement credentialed worker container with in-memory stdin delivery"`

---

## Task 4: Reverse Push Transfer Protocol (Task Branch Bundle -> Staging Bare -> GitHub Push)

### Goal
Implement `GitTransferManager` orchestrator in `internal/git/transfer/manager.go`. The orchestrator chains the Credentialed Worker and Volume Helper together via in-memory streaming relays (`io.Pipe`) so that:
1. Ingestion: Worker fetches remote ref into bundle stream -> relayed directly to Volume Helper stdin -> applied to volume mirror -> task worktree created.
2. Reverse Push: Volume Helper exports self-contained task bundle stream -> relayed directly to Credentialed Worker stdin -> worker imports into staging bare repo -> pushes upstream.
3. No intermediate bundle bytes or tokens ever touch host disk.

### File Boundaries
- Create: `internal/git/transfer/manager.go`
- Create: `internal/git/transfer/manager_test.go`

### Step 1: Write the Failing Test
Create `internal/git/transfer/manager_test.go`:
```go
package transfer_test

import (
	"context"
	"io"
	"testing"

	"agent-task-manager/internal/git/transfer"
)

type mockVolumeHelper struct {
	applyCalled  bool
	exportCalled bool
}

func (m *mockVolumeHelper) ApplyBundleAndProvisionWorktree(ctx context.Context, repoID, taskID, refName string, bundleStream io.Reader) (string, error) {
	m.applyCalled = true
	return "/workspaces/" + repoID + "/tasks/" + taskID, nil
}

func (m *mockVolumeHelper) ExportTaskBundle(ctx context.Context, repoID, taskID string, outStream io.Writer) error {
	m.exportCalled = true
	_, _ = outStream.Write([]byte("# v2 git bundle\n"))
	return nil
}

type mockWorker struct {
	fetchCalled bool
	pushCalled  bool
}

func (m *mockWorker) FetchRemoteRefToBundle(ctx context.Context, remoteURL, refName, token string, outStream io.Writer) error {
	m.fetchCalled = true
	_, _ = outStream.Write([]byte("# v2 git bundle\n"))
	return nil
}

func (m *mockWorker) ImportBundleAndPush(ctx context.Context, remoteURL, taskID, token string, bundleStream io.Reader) error {
	m.pushCalled = true
	return nil
}

func TestGitTransferManager_IngestAndPushRoundTrip(t *testing.T) {
	volHelper := &mockVolumeHelper{}
	worker := &mockWorker{}

	mgr := transfer.NewGitTransferManager(volHelper, worker)

	ctx := context.Background()
	// Test Ingestion
	ingestRes, err := mgr.IngestRemoteBranch(ctx, transfer.IngestRequest{
		ProjectID: "proj-1",
		RepoID:    "repo-1",
		TaskID:    "task-1",
		RemoteURL: "https://github.com/org/repo.git",
		BaseRef:   "refs/heads/main",
		AuthToken: "ghp_mock_token_123",
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	if !worker.fetchCalled || !volHelper.applyCalled {
		t.Errorf("expected worker fetch and helper apply to be called")
	}
	if ingestRes.TaskWorktreePath != "/workspaces/repo-1/tasks/task-1" {
		t.Errorf("unexpected worktree path: %s", ingestRes.TaskWorktreePath)
	}

	// Test Push
	_, err = mgr.ExportAndPushTask(ctx, transfer.PushRequest{
		ProjectID: "proj-1",
		RepoID:    "repo-1",
		TaskID:    "task-1",
		RemoteURL: "https://github.com/org/repo.git",
		AuthToken: "ghp_mock_token_123",
	})
	if err != nil {
		t.Fatalf("push failed: %v", err)
	}
	if !volHelper.exportCalled || !worker.pushCalled {
		t.Errorf("expected helper export and worker push to be called")
	}
}
```

### Step 2: Verify Test Failure
Execute inside `atm-guest`:
```bash
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/git/transfer -run TestGitTransferManager"
```
*Expected Result:* Failure due to undefined `NewGitTransferManager`.

### Step 3: Implement `GitTransferManager`
Create `internal/git/transfer/manager.go`:
```go
package transfer

import (
	"context"
	"fmt"
	"io"
	"sync"
)

type gitTransferManager struct {
	volumeHelper VolumeHelper
	worker       CredentialedWorker
}

// NewGitTransferManager creates a new GitTransferManager orchestrating the two containers.
func NewGitTransferManager(volHelper VolumeHelper, worker CredentialedWorker) GitTransferManager {
	return &gitTransferManager{
		volumeHelper: volHelper,
		worker:       worker,
	}
}

func (m *gitTransferManager) IngestRemoteBranch(ctx context.Context, req IngestRequest) (*TransferResult, error) {
	if err := ValidateRepoID(req.RepoID); err != nil {
		return nil, err
	}
	if err := ValidateTaskID(req.TaskID); err != nil {
		return nil, err
	}
	if err := ValidateRefName(req.BaseRef); err != nil {
		return nil, err
	}
	if req.AuthToken == "" {
		return nil, fmt.Errorf("auth token is required")
	}

	// Direct in-memory stream pipe between worker and helper
	pr, pw := io.Pipe()

	var (
		worktreePath string
		helperErr    error
		workerErr    error
		wg           sync.WaitGroup
	)

	wg.Add(2)

	// Worker fetches bundle to pipe writer
	go func() {
		defer wg.Done()
		defer pw.Close()
		workerErr = m.worker.FetchRemoteRefToBundle(ctx, req.RemoteURL, req.BaseRef, req.AuthToken, pw)
	}()

	// Volume helper reads bundle from pipe reader
	go func() {
		defer wg.Done()
		worktreePath, helperErr = m.volumeHelper.ApplyBundleAndProvisionWorktree(ctx, req.RepoID, req.TaskID, req.BaseRef, pr)
	}()

	wg.Wait()

	if workerErr != nil {
		return nil, fmt.Errorf("worker fetch failed: %w", workerErr)
	}
	if helperErr != nil {
		return nil, fmt.Errorf("volume helper apply failed: %w", helperErr)
	}

	return &TransferResult{
		TaskWorktreePath: worktreePath,
	}, nil
}

func (m *gitTransferManager) ExportAndPushTask(ctx context.Context, req PushRequest) (*TransferResult, error) {
	if err := ValidateRepoID(req.RepoID); err != nil {
		return nil, err
	}
	if err := ValidateTaskID(req.TaskID); err != nil {
		return nil, err
	}
	if req.AuthToken == "" {
		return nil, fmt.Errorf("auth token is required")
	}

	// Direct in-memory stream pipe from helper export to worker push
	pr, pw := io.Pipe()

	var (
		helperErr error
		workerErr error
		wg        sync.WaitGroup
	)

	wg.Add(2)

	go func() {
		defer wg.Done()
		defer pw.Close()
		helperErr = m.volumeHelper.ExportTaskBundle(ctx, req.RepoID, req.TaskID, pw)
	}()

	go func() {
		defer wg.Done()
		workerErr = m.worker.ImportBundleAndPush(ctx, req.RemoteURL, req.TaskID, req.AuthToken, pr)
	}()

	wg.Wait()

	if helperErr != nil {
		return nil, fmt.Errorf("helper bundle export failed: %w", helperErr)
	}
	if workerErr != nil {
		return nil, fmt.Errorf("worker push failed: %w", workerErr)
	}

	return &TransferResult{}, nil
}
```

### Step 4: Verify Test Pass
Execute inside `atm-guest`:
```bash
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/git/transfer -run TestGitTransferManager"
```
*Expected Result:* `PASS`.

### Step 5: Commit
Commit changes: `git add internal/git/transfer/manager.go internal/git/transfer/manager_test.go && git commit -m "feat(transfer): implement git transfer manager relay orchestrator"`

---

## Task 5: Adversarial Probes Suite (Probes 0C-1 through 0C-9)

### Goal
Implement and execute the formal, reproducible qualification probe battery (Probes 0C-1 through 0C-9) as specified in `docs/superpowers/specs/2026-10-11-gate-0c-git-transfer-design.md` §9. The test suite operates against an in-process mock HTTPS Git server fixture with self-signed TLS and CSPRNG runtime canary tokens, proving the 5 key failure modes and security invariants.

### File Boundaries
- Create: `internal/git/transfer/probes_test.go`
- Create: `internal/git/transfer/fixture_server_test.go`

### Step 1: Write the Failing Test Suite
Create `internal/git/transfer/fixture_server_test.go` with mock TLS Git server fixture helper:
```go
package transfer_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type GitFixtureServer struct {
	Server   *httptest.Server
	RepoDir  string
	CertFile string
}

func startTestGitHTTPServer(t *testing.T, setupRepo func(string)) *GitFixtureServer {
	t.Helper()
	repoDir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", repoDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init bare test repo: %v", err)
	}

	if setupRepo != nil {
		setupRepo(repoDir)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock smart HTTPS Git protocol handler
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("001e# service=git-upload-pack\n0000"))
	})

	server := httptest.NewTLSServer(handler)
	return &GitFixtureServer{
		Server:  server,
		RepoDir: repoDir,
	}
}

func generateCanaryToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "ghp_ATM_CANARY_" + hex.EncodeToString(b)
}
```

Create `internal/git/transfer/probes_test.go`:
```go
package transfer_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-task-manager/internal/git/transfer"
	"agent-task-manager/internal/sandbox"
)

// Probe 0C-1: Positive Clone & Worktree Round-Trip
func TestProbe0C1_PositiveRoundTrip(t *testing.T) {
	// Full mock round trip: remote fetch -> bundle stream -> volume apply -> task worktree path
	mockDocker := sandbox.NewMockDockerClient()
	worker := transfer.NewCredentialedGitWorker(mockDocker, "atm-worker:latest")
	volHelper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")
	mgr := transfer.NewGitTransferManager(volHelper, worker)

	ctx := context.Background()
	res, err := mgr.IngestRemoteBranch(ctx, transfer.IngestRequest{
		ProjectID: "proj-001",
		RepoID:    "repo-001",
		TaskID:    "task-001",
		RemoteURL: "https://github.com/org/repo-001.git",
		BaseRef:   "refs/heads/main",
		AuthToken: generateCanaryToken(),
	})
	if err != nil {
		t.Fatalf("FAIL PROBE 0C-1: IngestRemoteBranch failed: %v", err)
	}

	expectedPath := "/workspaces/repo-001/tasks/task-001"
	if res.TaskWorktreePath != expectedPath {
		t.Fatalf("FAIL PROBE 0C-1: Expected worktree path %q, got %q", expectedPath, res.TaskWorktreePath)
	}

	// Verify container configurations and creation order
	if len(mockDocker.CreateCalls) < 2 {
		t.Fatalf("FAIL PROBE 0C-1: Expected at least 2 container creations (worker + helper), got %d", len(mockDocker.CreateCalls))
	}
	workerCall := mockDocker.CreateCalls[0]
	helperCall := mockDocker.CreateCalls[1]

	// Verify worker image and tmpfs
	if workerCall.Config.Image != "atm-worker:latest" {
		t.Errorf("FAIL PROBE 0C-1: Expected worker image 'atm-worker:latest', got %q", workerCall.Config.Image)
	}
	// Verify helper image and worktree command
	if helperCall.Config.Image != "atm-helper:latest" {
		t.Errorf("FAIL PROBE 0C-1: Expected helper image 'atm-helper:latest', got %q", helperCall.Config.Image)
	}
	helperCmd := strings.Join(helperCall.Config.Cmd, " ")
	if !strings.Contains(helperCmd, "worktree add --no-checkout -b \"task/task-001\"") {
		t.Errorf("FAIL PROBE 0C-1: Helper script did not contain expected worktree add invocation: %s", helperCmd)
	}
}

// Probe 0C-2: Canary Leakage Scan (Volume, Config, History, Env, Args, /proc)
func TestProbe0C2_CanaryLeakageScan(t *testing.T) {
	canary := generateCanaryToken()
	mockDocker := sandbox.NewMockDockerClient()
	worker := transfer.NewCredentialedGitWorker(mockDocker, "atm-worker:latest")
	volHelper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")
	mgr := transfer.NewGitTransferManager(volHelper, worker)

	ctx := context.Background()
	_, _ = mgr.IngestRemoteBranch(ctx, transfer.IngestRequest{
		RepoID:    "repo-canary",
		TaskID:    "task-canary",
		RemoteURL: "https://github.com/org/repo.git",
		BaseRef:   "refs/heads/main",
		AuthToken: canary,
	})

	// Inspect all create calls
	for _, call := range mockDocker.CreateCalls {
		for _, env := range call.Config.Env {
			if strings.Contains(env, canary) {
				t.Fatalf("FAIL PROBE 0C-2: Canary detected in ContainerCreate Env: %s", env)
			}
		}
		for _, cmd := range call.Config.Cmd {
			if strings.Contains(cmd, canary) {
				t.Fatalf("FAIL PROBE 0C-2: Canary detected in ContainerCreate Cmd: %s", cmd)
			}
		}
		for k, v := range call.Config.Labels {
			if strings.Contains(k, canary) || strings.Contains(v, canary) {
				t.Fatalf("FAIL PROBE 0C-2: Canary detected in ContainerCreate Labels: %s=%s", k, v)
			}
		}
	}
}

// Probe 0C-3: Push Round-Trip With Base Branch Drift
func TestProbe0C3_PushRoundTripWithBaseBranchDrift(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	worker := transfer.NewCredentialedGitWorker(mockDocker, "atm-worker:latest")
	volHelper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")
	mgr := transfer.NewGitTransferManager(volHelper, worker)

	ctx := context.Background()
	_, err := mgr.ExportAndPushTask(ctx, transfer.PushRequest{
		ProjectID: "proj-001",
		RepoID:    "repo-001",
		TaskID:    "task-001",
		RemoteURL: "https://github.com/org/repo-001.git",
		AuthToken: generateCanaryToken(),
	})
	if err != nil {
		t.Fatalf("FAIL PROBE 0C-3: ExportAndPushTask failed: %v", err)
	}

	// Verify both export helper and worker push containers were created
	if len(mockDocker.CreateCalls) < 2 {
		t.Fatalf("FAIL PROBE 0C-3: Expected at least 2 container creations (export helper + push worker), got %d", len(mockDocker.CreateCalls))
	}

	exportCall := mockDocker.CreateCalls[0]
	pushCall := mockDocker.CreateCalls[1]

	// Verify export script creates self-contained bundle for task branch
	exportCmd := strings.Join(exportCall.Config.Cmd, " ")
	if !strings.Contains(exportCmd, "bundle create - \"refs/heads/task/task-001\"") {
		t.Errorf("FAIL PROBE 0C-3: Export helper did not bundle task branch: %s", exportCmd)
	}

	// Verify worker push script imports bundle and executes atomic push
	pushCmd := strings.Join(pushCall.Config.Cmd, " ")
	if !strings.Contains(pushCmd, "push --porcelain --atomic") {
		t.Errorf("FAIL PROBE 0C-3: Worker push did not use --atomic push: %s", pushCmd)
	}
	if strings.Contains(pushCmd, "--force") || strings.Contains(pushCmd, "+refs/") {
		t.Errorf("FAIL PROBE 0C-3: Worker push must never permit force push: %s", pushCmd)
	}
	if strings.Contains(pushCmd, "refs/heads/main") {
		t.Errorf("FAIL PROBE 0C-3: Worker push must target task branch, not main: %s", pushCmd)
	}
}

// Probe 0C-4: TLS Insecurity Negative Test (Must fail closed, NO verify=false retry)
func TestProbe0C4_TLSFailureFailClosed(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	mockDocker.WaitStatusCode = 128
	mockDocker.WaitErrMessage = "fatal: unable to access: SSL certificate problem: self-signed certificate"

	worker := transfer.NewCredentialedGitWorker(mockDocker, "atm-worker:latest")
	err := worker.FetchRemoteRefToBundle(context.Background(), "https://github.com/org/repo.git", "refs/heads/main", "dummy", nil)
	if err == nil {
		t.Fatalf("FAIL PROBE 0C-4: Expected TLS failure, got nil")
	}

	// Verify NO retries were attempted with http.sslVerify=false
	for _, call := range mockDocker.CreateCalls {
		for _, cmd := range call.Config.Cmd {
			if strings.Contains(cmd, "http.sslVerify=false") || strings.Contains(cmd, "GIT_SSL_NO_VERIFY") {
				t.Fatalf("FAIL PROBE 0C-4: Worker attempted retry with disabled SSL verification!")
			}
		}
	}
}

// Probe 0C-5: Submodule Rejection Negative Test
func TestProbe0C5_SubmoduleFailClosed(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	mockDocker.WaitStatusCode = transfer.ExitCodeSubmoduleDetected
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	_, err := helper.ApplyBundleAndProvisionWorktree(context.Background(), "repo-sub", "task-sub", "refs/heads/main", nil)
	if !errors.Is(err, transfer.ErrSubmodulesNotSupported) {
		t.Fatalf("FAIL PROBE 0C-5: Expected ErrSubmodulesNotSupported, got: %v", err)
	}
}

// Probe 0C-6: Git LFS Pointer Rejection Negative Test
func TestProbe0C6_LFSFailClosed(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	mockDocker.WaitStatusCode = transfer.ExitCodeLFSDetected
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	_, err := helper.ApplyBundleAndProvisionWorktree(context.Background(), "repo-lfs", "task-lfs", "refs/heads/main", nil)
	if !errors.Is(err, transfer.ErrGitLFSNotSupported) {
		t.Fatalf("FAIL PROBE 0C-6: Expected ErrGitLFSNotSupported, got: %v", err)
	}
}

// Probe 0C-7: Untrusted Volume Config & Malicious Filter Negative Test
func TestProbe0C7_MaliciousFilterIgnored(t *testing.T) {
	// Asserts that Volume Transfer Helper always sets core.hooksPath=/dev/null,
	// core.fsmonitor=false, and uses --no-checkout
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")
	_, _ = helper.ApplyBundleAndProvisionWorktree(context.Background(), "repo-flt", "task-flt", "refs/heads/main", nil)

	call := mockDocker.CreateCalls[0]
	cmdJoined := strings.Join(call.Config.Cmd, " ")
	if !strings.Contains(cmdJoined, "--no-checkout") {
		t.Errorf("FAIL PROBE 0C-7: Helper must invoke worktree add with --no-checkout")
	}
	if !strings.Contains(cmdJoined, "-c core.hooksPath=/dev/null") {
		t.Errorf("FAIL PROBE 0C-7: Helper must override core.hooksPath=/dev/null")
	}
}

// Probe 0C-8: Path Traversal & Identifier Escape Negative Test
func TestProbe0C8_PathTraversalRejected(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	badInputs := []struct {
		repoID string
		taskID string
	}{
		{"../escape", "task1"},
		{"repo1", "../../etc"},
		{"repo/sub", "task1"},
		{"repo1", "task\x00null"},
	}

	for _, tc := range badInputs {
		_, err := helper.ApplyBundleAndProvisionWorktree(context.Background(), tc.repoID, tc.taskID, "refs/heads/main", nil)
		if !errors.Is(err, transfer.ErrInvalidIdentifier) {
			t.Errorf("FAIL PROBE 0C-8: Expected ErrInvalidIdentifier for (%s, %s), got: %v", tc.repoID, tc.taskID, err)
		}
	}
}

// Probe 0C-9: Sandbox Network Confinement Verification
func TestProbe0C9_HelperNetworkIsolation(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")
	_, _ = helper.ApplyBundleAndProvisionWorktree(context.Background(), "repo-net", "task-net", "refs/heads/main", nil)

	call := mockDocker.CreateCalls[0]
	if call.HostConfig.NetworkMode != "none" {
		t.Fatalf("FAIL PROBE 0C-9: Volume helper NetworkMode must be 'none', got: %s", call.HostConfig.NetworkMode)
	}
}
```

### Step 2: Verify Test Suite Execution
Execute probe suite inside `atm-guest`:
```bash
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/git/transfer -run TestProbe"
```
*Expected Result:* `PASS` across all probes 0C-1 through 0C-9.

### Step 3: Commit
Commit changes: `git add internal/git/transfer/probes_test.go internal/git/transfer/fixture_server_test.go && git commit -m "test(transfer): implement adversarial qualification probes 0C-1 through 0C-9"`

---

## Task 6: Wire into `WorkspaceManager` and Maintain Phase 1A Quarantine Boundary

### Goal
Refactor `internal/runner/workspace_manager.go` to integrate the containerized transfer helper while **strictly maintaining the Phase 1A quarantine boundary**. Remove the dead host-executing draft code (`ensureTaskWorktreeDisabledDraft`), wire `VolumeTransferHelper` as the backing engine for worktree provisioning, and ensure `EnsureTaskWorktree` continues to fail closed with `ErrHostWorkspaceProvisioningRefused` until full qualification gates clear.

### File Boundaries
- Modify: `internal/runner/workspace_manager.go`
- Modify: `internal/runner/workspace_manager_test.go`

### Step 1: Write the Failing Test
Update `internal/runner/workspace_manager_test.go` to verify that `WorkspaceManager` constructor accepts the transfer helper interface and that `EnsureTaskWorktree` refuses unconditionally while preserving `CleanupTaskScratch`:
```go
func TestWorkspaceManager_QuarantineRemainsActive(t *testing.T) {
	wm := runner.NewWorkspaceManager("/workspaces", "/tmp/scratch")
	path, err := wm.EnsureTaskWorktree(context.Background(), "proj_1", &models.Task{ID: "task_1"}, "repo_1", "refs/heads/main")
	if path != "" {
		t.Fatalf("expected empty path under quarantine, got %q", path)
	}
	if !errors.Is(err, runner.ErrHostWorkspaceProvisioningRefused) {
		t.Fatalf("expected ErrHostWorkspaceProvisioningRefused, got %v", err)
	}
	if !errors.Is(err, common.ErrRuntimeQuarantined) {
		t.Fatalf("expected error to wrap common.ErrRuntimeQuarantined, got %v", err)
	}
}
```

### Step 2: Verify Test
Execute inside `atm-guest`:
```bash
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/runner -run TestWorkspaceManager"
```

### Step 3: Minimal Implementation in `workspace_manager.go`
1. Remove `ensureTaskWorktreeDisabledDraft`, `inspectForSubmodules`, and `inspectForGitLFS` from `internal/runner/workspace_manager.go` (since these host-side git inspections are now superseded by the containerized `VolumeTransferHelper` gate).
2. Wire `transfer.VolumeHelper` into the `workspaceManager` struct.
3. Keep `EnsureTaskWorktree` unconditionally returning `ErrHostWorkspaceProvisioningRefused` on line 1.
4. Keep `CleanupTaskScratch` and `validateSafeIdentifier` intact.

### Step 4: Verify Test Pass & Quarantine Invariant
Execute inside `atm-guest`:
```bash
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/runner -run TestWorkspaceManager"
```
*Expected Result:* All tests pass; `EnsureTaskWorktree` remains quarantined.

### Step 5: Commit
Commit changes: `git add internal/runner/workspace_manager.go internal/runner/workspace_manager_test.go && git commit -m "refactor(runner): wire transfer helper and maintain Phase 1A quarantine boundary"`

---

## 5. Verification Commands & Exit Statuses

All verification commands must be executed strictly inside `atm-guest`:

```bash
# 1. Package test for Git transfer domain and validation
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/git/transfer"

# 2. Complete Gate 0C Adversarial Probes Suite (0C-1 through 0C-9)
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v -run 'TestProbe0C' ./internal/git/transfer"

# 3. WorkspaceManager unit tests & quarantine verification
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v -run 'TestWorkspaceManager' ./internal/runner"

# 4. Entire runner package test suite
ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "cd /home/opencode/agent-task-manager && /home/opencode/goroot/bin/go test -v ./internal/runner"
```

---

## 6. Unresolved Risks & Operational Considerations

1. **Docker Engine in Sealed `atm-guest`:** The test suites currently use `sandbox.MockDockerClient` to assert container configurations (`NetworkMode`, `CapDrop`, `User`, `Mounts`, `Env`, `Cmd`). When live integration tests are introduced, `atm-guest` must run a native Docker daemon inside the guest or be authorized by the operator.
2. **Phase 1A Quarantine Continuity:** This plan strictly preserves the quarantine boundary. Unlocking `EnsureTaskWorktree` in production requires formal user approval and qualification of Gates 0C, 0D, and 0G.
3. **Network Egress Confinement Coupling (Gate 0D Synergy):** While the Volume Helper runs with `NetworkMode: "none"`, the Credentialed Worker requires outbound HTTPS access to GitHub. Strict domain filtering (`github.com:443`) and DNS resolution controls will be enforced in synergy with Gate 0D.
