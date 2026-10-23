package transfer_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-task-manager/internal/git/transfer"
	"agent-task-manager/internal/sandbox"
)

func TestValidateWorkspacePath(t *testing.T) {
	for _, good := range []string{"src/app/page.tsx", "README.md", "a/b/c.txt"} {
		if err := transfer.ValidateWorkspacePath(good); err != nil {
			t.Errorf("%q should be accepted: %v", good, err)
		}
	}
	for _, bad := range []string{"", "/etc/passwd", "../x", "a/../b", "a//b", "a/./b", ".git/config", "a/.git", "a\x00b", strings.Repeat("a", 1025)} {
		if err := transfer.ValidateWorkspacePath(bad); !errors.Is(err, transfer.ErrInvalidWorkspacePath) {
			t.Errorf("%q should be rejected, got %v", bad, err)
		}
	}
}

func TestVolumeHelper_ListWorkspaceFilesIsReadOnlyAndOffline(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	if _, err := helper.ListWorkspaceFiles(context.Background(), "repo-1", "task-1"); err != nil {
		t.Logf("listing output is empty under the mock: %v", err)
	}
	if len(mockDocker.CreateCalls) != 1 {
		t.Fatalf("expected one container, got %d", len(mockDocker.CreateCalls))
	}
	call := mockDocker.CreateCalls[0]
	if call.HostConfig.NetworkMode != "none" || !call.HostConfig.ReadonlyRootfs {
		t.Errorf("expected an offline, read-only root, got network=%q readonly=%v", call.HostConfig.NetworkMode, call.HostConfig.ReadonlyRootfs)
	}
	for _, m := range call.HostConfig.Mounts {
		if m.Target == "/workspaces" && !m.ReadOnly {
			t.Errorf("the file listing must mount the volume read-only")
		}
	}
	script := strings.Join(call.Config.Cmd, " ")
	for _, want := range []string{"GIT_OPTIONAL_LOCKS=0", "ls-files -z --cached --others --exclude-standard", "ls-tree -r -z --name-only"} {
		if !strings.Contains(script, want) {
			t.Errorf("listing script missing %q", want)
		}
	}
}

// The file path reaches the helper only through its environment, never spliced into the script.
func TestVolumeHelper_ReadWorkspaceFilePassesThePathInTheEnvironment(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	_, _ = helper.ReadWorkspaceFile(context.Background(), "repo-1", "task-1", "src/a.go", 1024)
	if len(mockDocker.CreateCalls) != 1 {
		t.Fatalf("expected one container, got %d", len(mockDocker.CreateCalls))
	}
	call := mockDocker.CreateCalls[0]
	env := strings.Join(call.Config.Env, "\n")
	if !strings.Contains(env, "ATM_FILE_PATH=src/a.go") || !strings.Contains(env, "ATM_FILE_MAX=1024") {
		t.Errorf("expected the path and cap in the environment, got %v", call.Config.Env)
	}
	if strings.Contains(strings.Join(call.Config.Cmd, " "), "src/a.go") {
		t.Errorf("the file path must not be spliced into the script")
	}
	if call.HostConfig.NetworkMode != "none" {
		t.Errorf("expected NetworkMode 'none', got %q", call.HostConfig.NetworkMode)
	}
}

func TestVolumeHelper_ReadWorkspaceFileRefusesTraversalBeforeAnyContainer(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	helper := transfer.NewVolumeTransferHelper(mockDocker, "atm-helper:latest")

	_, err := helper.ReadWorkspaceFile(context.Background(), "repo-1", "task-1", "../../etc/passwd", 1024)
	if !errors.Is(err, transfer.ErrInvalidWorkspacePath) {
		t.Fatalf("expected ErrInvalidWorkspacePath, got %v", err)
	}
	if len(mockDocker.CreateCalls) != 0 {
		t.Errorf("a traversal path must not start a container")
	}
}
