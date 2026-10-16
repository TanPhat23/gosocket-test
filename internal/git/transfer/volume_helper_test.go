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
