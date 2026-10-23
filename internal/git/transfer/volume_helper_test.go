package transfer_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
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

func TestVolumeHelper_DiffTaskIsReadOnlyAndOffline(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	var out bytes.Buffer
	if err := helper.DiffTask(context.Background(), "repo-1", "task-1", "refs/heads/main", &out); err != nil {
		t.Fatalf("DiffTask: %v", err)
	}
	if len(mockDocker.CreateCalls) != 1 {
		t.Fatalf("expected one container, got %d", len(mockDocker.CreateCalls))
	}
	call := mockDocker.CreateCalls[0]
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

	volumeMounted := false
	for _, m := range call.HostConfig.Mounts {
		if m.Target == "/workspaces" {
			volumeMounted = true
			if !m.ReadOnly {
				t.Errorf("expected the project volume to be mounted read-only")
			}
		}
	}
	if !volumeMounted {
		t.Errorf("expected the project volume at /workspaces")
	}

	hasBase := false
	for _, e := range call.Config.Env {
		if e == "ATM_BASE_REF=refs/heads/main" {
			hasBase = true
		}
	}
	if !hasBase {
		t.Errorf("expected the base ref in ATM_BASE_REF, got %v", call.Config.Env)
	}
	script := strings.Join(call.Config.Cmd, " ")
	for _, want := range []string{`refs/atm/base/task-1`, `exec git`, `diff --no-ext-diff --no-textconv`} {
		if !strings.Contains(script, want) {
			t.Errorf("diff script missing %q", want)
		}
	}
}

func TestVolumeHelper_DiffTaskRejectsUnsafeBaseRefBeforeAnyContainer(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	err := helper.DiffTask(context.Background(), "repo-1", "task-1", "refs/heads/a..b", &bytes.Buffer{})
	if !errors.Is(err, transfer.ErrInvalidRefName) {
		t.Fatalf("expected ErrInvalidRefName, got %v", err)
	}
	if len(mockDocker.CreateCalls) != 0 {
		t.Errorf("an invalid base ref must not start a container")
	}
}

func TestVolumeHelper_ProvisionPinsTheBaseCommitOnce(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	_, _ = helper.ApplyBundleAndProvisionWorktree(context.Background(), "repo-1", "task-1", "refs/heads/main",
		bytes.NewReader([]byte("# v2 git bundle\n")))
	if len(mockDocker.CreateCalls) == 0 {
		t.Fatalf("expected a provisioning container")
	}
	script := strings.Join(mockDocker.CreateCalls[0].Config.Cmd, " ")
	if !strings.Contains(script, `rev-parse -q --verify "refs/atm/base/task-1"`) {
		t.Errorf("provisioning must check for an existing base pin before writing one")
	}
	if !strings.Contains(script, `update-ref "refs/atm/base/task-1" "refs/heads/main"`) {
		t.Errorf("provisioning must pin the base ref it was given")
	}
}

// A base branch moved locally (off upstream) must not make later ingests fail as non-fast-forward.
func TestVolumeHelper_IngestForcesTheBaseBranchFetch(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	_, _ = helper.ApplyBundleAndProvisionWorktree(context.Background(), "repo-1", "task-1", "refs/heads/main",
		bytes.NewReader([]byte("# v2 git bundle\n")))
	if len(mockDocker.CreateCalls) == 0 {
		t.Fatalf("expected a provisioning container")
	}
	script := strings.Join(mockDocker.CreateCalls[0].Config.Cmd, " ")
	if !strings.Contains(script, `fetch -u "$TMP_BUNDLE" "+refs/heads/main:refs/heads/main"`) {
		t.Errorf("ingest must force the base branch fetch")
	}
}

// Removing a worktree needs the volume writable, but the container stays offline and drops every capability. It
// must remove the checkout and prune the registration, and it must leave the task branch and base pin alone.
func TestVolumeHelper_RemoveTaskWorkspaceDeletesOnlyTheCheckout(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	if err := helper.RemoveTaskWorkspace(context.Background(), "repo-1", "task-1"); err != nil {
		t.Fatalf("RemoveTaskWorkspace: %v", err)
	}
	if len(mockDocker.CreateCalls) != 1 {
		t.Fatalf("expected one container, got %d", len(mockDocker.CreateCalls))
	}
	call := mockDocker.CreateCalls[0]
	if call.HostConfig.NetworkMode != "none" || !call.HostConfig.ReadonlyRootfs {
		t.Errorf("expected an offline, read-only root, got network=%q readonly=%v", call.HostConfig.NetworkMode, call.HostConfig.ReadonlyRootfs)
	}
	if len(call.HostConfig.CapDrop) != 1 || call.HostConfig.CapDrop[0] != "ALL" {
		t.Errorf("expected CapDrop ['ALL'], got %v", call.HostConfig.CapDrop)
	}
	for _, m := range call.HostConfig.Mounts {
		if m.Target == "/workspaces" && m.ReadOnly {
			t.Errorf("worktree removal needs the volume writable")
		}
	}
	script := strings.Join(call.Config.Cmd, " ")
	for _, want := range []string{`worktree remove --force "$TASK_DIR"`, `worktree prune`, `/workspaces/repo-1/tasks/task-1`} {
		if !strings.Contains(script, want) {
			t.Errorf("removal script missing %q", want)
		}
	}
	for _, forbidden := range []string{"branch -D", "update-ref -d", "refs/atm"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("removal must not touch %q", forbidden)
		}
	}
}

// Trimming dependencies is offline and removes node_modules only: no worktree or branch operation.
func TestVolumeHelper_RemoveTaskDependenciesDeletesOnlyNodeModules(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	if err := helper.RemoveTaskDependencies(context.Background(), "repo-1", "task-1"); err != nil {
		t.Fatalf("RemoveTaskDependencies: %v", err)
	}
	if len(mockDocker.CreateCalls) != 1 {
		t.Fatalf("expected one container, got %d", len(mockDocker.CreateCalls))
	}
	call := mockDocker.CreateCalls[0]
	if call.HostConfig.NetworkMode != "none" || !call.HostConfig.ReadonlyRootfs {
		t.Errorf("expected an offline, read-only root, got network=%q readonly=%v", call.HostConfig.NetworkMode, call.HostConfig.ReadonlyRootfs)
	}
	script := strings.Join(call.Config.Cmd, " ")
	if !strings.Contains(script, `rm -rf "/workspaces/repo-1/tasks/task-1/node_modules"`) {
		t.Errorf("script must remove the task's node_modules, got %q", script)
	}
	for _, forbidden := range []string{"worktree", "branch", "update-ref", "git "} {
		if strings.Contains(script, forbidden) {
			t.Errorf("dependency removal must not run %q", forbidden)
		}
	}
	if err := helper.RemoveTaskDependencies(context.Background(), "repo-1", "../etc"); err == nil || len(mockDocker.CreateCalls) != 1 {
		t.Errorf("a path-like task ID must be rejected before any container starts")
	}
}

func TestVolumeHelper_RemoveTaskWorkspaceRejectsBadIDsBeforeAnyContainer(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	if err := helper.RemoveTaskWorkspace(context.Background(), "repo-1", "../etc"); err == nil {
		t.Fatalf("expected a path-like task ID to be rejected")
	}
	if len(mockDocker.CreateCalls) != 0 {
		t.Errorf("a rejected ID must not start a container")
	}
}

// A task whose history shares no commit with its base must still produce a diff: the script falls back to the
// difference between the two trees when there is no merge base.
func TestVolumeHelper_DiffTaskFallsBackToTreeDiffWithoutAMergeBase(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	if err := helper.DiffTask(context.Background(), "repo-1", "task-1", "refs/heads/main", &bytes.Buffer{}); err != nil {
		t.Fatalf("DiffTask: %v", err)
	}
	script := strings.Join(mockDocker.CreateCalls[0].Config.Cmd, " ")
	if !strings.Contains(script, `merge-base "$BASE_REF" "$TASK_REF"`) {
		t.Errorf("diff must look for a merge base first")
	}
	if !strings.Contains(script, `diff --no-ext-diff --no-textconv --no-color "$BASE_REF" "$TASK_REF"`) {
		t.Errorf("diff must fall back to the tree-to-tree form when there is no merge base")
	}
}
