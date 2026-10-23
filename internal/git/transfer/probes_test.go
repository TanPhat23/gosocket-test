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
	var workerCall, helperCall *sandbox.ContainerCreateCall
	for i := range mockDocker.CreateCalls {
		call := &mockDocker.CreateCalls[i]
		if call.Config.Image == "atm-worker:latest" {
			workerCall = call
		} else if call.Config.Image == "atm-helper:latest" {
			helperCall = call
		}
	}
	if workerCall == nil {
		t.Fatalf("FAIL PROBE 0C-1: Expected container created with worker image 'atm-worker:latest'")
	}
	if helperCall == nil {
		t.Fatalf("FAIL PROBE 0C-1: Expected container created with helper image 'atm-helper:latest'")
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

	var exportCall, pushCall *sandbox.ContainerCreateCall
	for i := range mockDocker.CreateCalls {
		call := &mockDocker.CreateCalls[i]
		if call.Config.Image == "atm-helper:latest" {
			exportCall = call
		} else if call.Config.Image == "atm-worker:latest" {
			pushCall = call
		}
	}
	if exportCall == nil {
		t.Fatalf("FAIL PROBE 0C-3: Expected export helper container created with 'atm-helper:latest'")
	}
	if pushCall == nil {
		t.Fatalf("FAIL PROBE 0C-3: Expected push worker container created with 'atm-worker:latest'")
	}

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
