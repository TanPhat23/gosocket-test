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

func TestCredentialedWorker_ImportBundleAndPush_SecurityAndPiping(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	worker := transfer.NewCredentialedGitWorker(mockDocker, "atm-worker:latest")

	canaryToken := "ghp_ATM_CANARY_PUSH_TOKEN_0123456789abcdef"
	ctx := context.Background()
	dummyBundle := bytes.NewReader([]byte("# v2 git bundle\npush-data"))

	err := worker.ImportBundleAndPush(ctx, "https://github.com/org/repo.git", "task-123", canaryToken, dummyBundle)
	if err != nil {
		t.Fatalf("expected ImportBundleAndPush to succeed, got: %v", err)
	}

	if len(mockDocker.CreateCalls) == 0 {
		t.Fatalf("expected ContainerCreate call")
	}

	call := mockDocker.CreateCalls[0]

	// Assert zero tokens in Env
	for _, env := range call.Config.Env {
		if strings.Contains(env, canaryToken) {
			t.Errorf("canary token leaked into container Env: %q", env)
		}
	}

	// Assert zero tokens in Cmd
	for _, cmd := range call.Config.Cmd {
		if strings.Contains(cmd, canaryToken) {
			t.Errorf("canary token leaked into container Cmd: %q", cmd)
		}
	}

	// Assert stdin piping contains canary token and bundle data
	inBuf := mockDocker.AttachConn.InBuf.String()
	if !strings.Contains(inBuf, canaryToken) {
		t.Errorf("expected canary token in container stdin")
	}
	if !strings.Contains(inBuf, "# v2 git bundle\npush-data") {
		t.Errorf("expected bundle payload in container stdin")
	}
}

func TestCredentialedWorker_Validation(t *testing.T) {
	mockDocker := sandbox.NewMockDockerClient()
	worker := transfer.NewCredentialedGitWorker(mockDocker, "atm-worker:latest")
	ctx := context.Background()

	// Invalid remote URL
	if err := worker.FetchRemoteRefToBundle(ctx, "ftp://example.com/repo.git", "refs/heads/main", "tok", nil); err == nil {
		t.Errorf("expected error for ftp url")
	}
	if err := worker.FetchRemoteRefToBundle(ctx, "https://user:pass@github.com/repo.git", "refs/heads/main", "tok", nil); err == nil {
		t.Errorf("expected error for user embedded url")
	}

	// Invalid ref
	if err := worker.FetchRemoteRefToBundle(ctx, "https://github.com/org/repo.git", "-flags", "tok", nil); err == nil {
		t.Errorf("expected error for invalid ref name")
	}

	// Invalid task ID for push
	if err := worker.ImportBundleAndPush(ctx, "https://github.com/org/repo.git", "task;rm -rf", "tok", nil); err == nil {
		t.Errorf("expected error for invalid task ID")
	}
}

