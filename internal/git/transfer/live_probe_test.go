//go:build live

package transfer_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"agent-task-manager/internal/git/transfer"
	"agent-task-manager/internal/sandbox"
)

// Live Gate 0C probes: they drive the real credentialed worker against a real Docker daemon and an
// HTTPS smart-HTTP git fixture (internal/git/transfer/testdata/livefixture). Run only on a disposable host via
// the script in the Gate 0C plan; they need ATM_LIVE_* variables and are excluded from normal builds.
//
//	ATM_LIVE_IMAGE      worker image that trusts the fixture CA
//	ATM_LIVE_IMAGE_NOCA same image without the fixture CA (TLS must fail closed)
//	ATM_LIVE_FIXTURE_IP the fixture container's IP, mapped to github.com for the worker
//	ATM_LIVE_TOKEN      the fixture's auth token (a runtime canary)
//	ATM_LIVE_FIXTURE    fixture container name (for ref inspection)

const liveRemote = "https://github.com/org/repo.git"

type hostsClient struct {
	sandbox.DockerClient
	ip      string
	created chan string // optional: receives each created container ID
}

func (h hostsClient) ContainerCreate(ctx context.Context, c *container.Config, hc *container.HostConfig, nc *network.NetworkingConfig, p *ocispec.Platform, name string) (container.CreateResponse, error) {
	hc.ExtraHosts = append(hc.ExtraHosts, "github.com:"+h.ip)
	resp, err := h.DockerClient.ContainerCreate(ctx, c, hc, nc, p, name)
	if err == nil && h.created != nil {
		h.created <- resp.ID
	}
	return resp, err
}

func liveEnv(t *testing.T, k string) string {
	t.Helper()
	v := os.Getenv(k)
	if v == "" {
		t.Skipf("%s not set", k)
	}
	return v
}

func liveWorker(t *testing.T, image string) transfer.CredentialedWorker {
	t.Helper()
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	return transfer.NewCredentialedGitWorker(hostsClient{DockerClient: cli, ip: liveEnv(t, "ATM_LIVE_FIXTURE_IP")}, image)
}

func TestLive0C1_FetchBundleRoundTripAndNoTokenInBundle(t *testing.T) {
	token := liveEnv(t, "ATM_LIVE_TOKEN")
	w := liveWorker(t, liveEnv(t, "ATM_LIVE_IMAGE"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var buf bytes.Buffer
	if err := w.FetchRemoteRefToBundle(ctx, liveRemote, "refs/heads/main", token, &buf); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("# v2 git bundle")) && !bytes.HasPrefix(buf.Bytes(), []byte("# v3 git bundle")) {
		head := buf.Bytes()
		if len(head) > 80 {
			head = head[:80]
		}
		t.Fatalf("stream is not a git bundle; first bytes: %q", head)
	}
	if bytes.Contains(buf.Bytes(), []byte(token)) {
		t.Fatal("token found in the bundle stream")
	}
	f := filepath.Join(t.TempDir(), "b.bundle")
	if err := os.WriteFile(f, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "bundle", "verify", f).CombinedOutput(); err != nil {
		t.Fatalf("bundle does not verify: %v\n%s", err, out)
	}
}

func TestLive0C4_TLSFailureFailsClosed(t *testing.T) {
	token := liveEnv(t, "ATM_LIVE_TOKEN")
	w := liveWorker(t, liveEnv(t, "ATM_LIVE_IMAGE_NOCA"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var buf bytes.Buffer
	if err := w.FetchRemoteRefToBundle(ctx, liveRemote, "refs/heads/main", token, &buf); err == nil {
		t.Fatal("fetch from an untrusted certificate must fail")
	}
	if buf.Len() != 0 && strings.HasPrefix(buf.String(), "# v") {
		t.Fatal("a bundle was produced despite the TLS failure")
	}
}

func TestLive0C3_PushTaskBranch(t *testing.T) {
	token := liveEnv(t, "ATM_LIVE_TOKEN")
	fixture := liveEnv(t, "ATM_LIVE_FIXTURE")
	w := liveWorker(t, liveEnv(t, "ATM_LIVE_IMAGE"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var base bytes.Buffer
	if err := w.FetchRemoteRefToBundle(ctx, liveRemote, "refs/heads/main", token, &base); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	dir := t.TempDir()
	run := func(args ...string) []byte {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return out
	}
	bf := filepath.Join(dir, "base.bundle")
	if err := os.WriteFile(bf, base.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	run("clone", bf, "w")
	dir = filepath.Join(dir, "w")
	taskID := fmt.Sprintf("live%d", time.Now().UnixNano())
	run("checkout", "-b", "task/"+taskID)
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "task work")
	tb := filepath.Join(t.TempDir(), "task.bundle")
	run("bundle", "create", tb, "refs/heads/task/"+taskID)
	data, err := os.ReadFile(tb)
	if err != nil {
		t.Fatal(err)
	}

	if err := w.ImportBundleAndPush(ctx, liveRemote, taskID, token, bytes.NewReader(data)); err != nil {
		t.Fatalf("push: %v", err)
	}
	out, err := exec.Command("docker", "exec", fixture, "git", "-C", "/data/org/repo.git", "for-each-ref", "--format=%(refname)").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect fixture refs: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "refs/heads/task/"+taskID) || !strings.Contains(string(out), "refs/heads/main") {
		t.Fatalf("fixture refs: %s", out)
	}
}

// Live 0C-2: while a worker is mid-fetch (the slow fixture holds upload-pack), the token must not appear
// in docker inspect (Env, Cmd, Labels), in any process's argv or environment, or in any file under the
// writable tmpfs. The credential-cache daemon holds it in memory only; that is the accepted design.
// Needs ATM_LIVE_SLOW_FIXTURE_IP: a second fixture started with -upload-pack-delay.
func TestLive0C2_RunningWorkerCanaryScan(t *testing.T) {
	token := liveEnv(t, "ATM_LIVE_TOKEN")
	image := liveEnv(t, "ATM_LIVE_IMAGE")
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	created := make(chan string, 1)
	w := transfer.NewCredentialedGitWorker(hostsClient{DockerClient: cli, ip: liveEnv(t, "ATM_LIVE_SLOW_FIXTURE_IP"), created: created}, image)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.FetchRemoteRefToBundle(ctx, liveRemote, "refs/heads/main", token, io.Discard) }()

	id := <-created
	// Wait until git is actually talking to the fixture (its remote-https helper is running).
	deadline := time.Now().Add(20 * time.Second)
	for {
		out, _ := exec.Command("docker", "exec", id, "sh", "-c", "cat /proc/[0-9]*/cmdline 2>/dev/null | tr '\\0' ' '").Output()
		if strings.Contains(string(out), "remote-https") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker never reached the fetch; processes: %s", out)
		}
		time.Sleep(100 * time.Millisecond)
	}

	scans := map[string][]string{
		"docker inspect":     {"docker", "inspect", id},
		"argv of every proc": {"docker", "exec", id, "sh", "-c", "cat /proc/[0-9]*/cmdline 2>/dev/null"},
		"env of every proc":  {"docker", "exec", id, "sh", "-c", "cat /proc/[0-9]*/environ 2>/dev/null"},
		"files under /tmp":   {"docker", "exec", id, "sh", "-c", "find /tmp -type f -exec cat {} + 2>/dev/null"},
	}
	for name, argv := range scans {
		out, _ := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		if len(out) == 0 {
			t.Errorf("%s: empty output, scan proved nothing", name)
		}
		if bytes.Contains(out, []byte(token)) {
			t.Errorf("token found in %s", name)
		}
	}
	if err := <-done; err != nil {
		t.Fatalf("fetch: %v", err)
	}
}
