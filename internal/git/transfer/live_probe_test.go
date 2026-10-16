//go:build live

package transfer_test

import (
	"bytes"
	"context"
	"errors"
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
//	ATM_LIVE_IMAGE      worker/helper image that trusts the fixture CA and ships /workspaces owned by 1000:1000
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

// liveManager wires the real volume helper and worker to the daemon. Both use ATM_LIVE_IMAGE.
func liveManager(t *testing.T) transfer.GitTransferManager {
	t.Helper()
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	image := liveEnv(t, "ATM_LIVE_IMAGE")
	hc := hostsClient{DockerClient: cli, ip: liveEnv(t, "ATM_LIVE_FIXTURE_IP")}
	return transfer.NewGitTransferManager(transfer.NewVolumeTransferHelper(cli, image), transfer.NewCredentialedGitWorker(hc, image))
}

// liveID returns a fresh identifier and removes the repo's data volume when the test ends.
func liveID(t *testing.T, prefix string) string {
	t.Helper()
	id := fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("docker", "volume", "rm", "-f", "atm-project-data-"+id).Run() })
	return id
}

// inVolume runs a shell command as 1000:1000 with the repo's data volume at /workspaces.
func inVolume(t *testing.T, repoID, script string) (string, error) {
	t.Helper()
	out, err := exec.Command("docker", "run", "--rm", "--user", "1000:1000", "--network", "none",
		"-v", "atm-project-data-"+repoID+":/workspaces", os.Getenv("ATM_LIVE_IMAGE"), "sh", "-c", script).CombinedOutput()
	return string(out), err
}

func ingest(t *testing.T, mgr transfer.GitTransferManager, repoID, taskID, ref string) (*transfer.TransferResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return mgr.IngestRemoteBranch(ctx, transfer.IngestRequest{
		ProjectID: "p", RepoID: repoID, TaskID: taskID, RemoteURL: liveRemote, BaseRef: ref,
		AuthToken: liveEnv(t, "ATM_LIVE_TOKEN"),
	})
}

// Live 0C-1 (full path): remote -> worker bundle -> helper -> volume repo and task worktree, then
// export the task branch and push it back. Also scans the volume for the token (the volume half of 0C-2).
func TestLive0C1_IngestAndPushThroughVolume(t *testing.T) {
	mgr := liveManager(t)
	repoID, taskID := liveID(t, "repo"), fmt.Sprintf("t%d", time.Now().UnixNano())
	res, err := ingest(t, mgr, repoID, taskID, "refs/heads/main")
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if want := "/workspaces/" + repoID + "/tasks/" + taskID; res.TaskWorktreePath != want {
		t.Fatalf("worktree path %q, want %q", res.TaskWorktreePath, want)
	}
	out, err := inVolume(t, repoID, fmt.Sprintf(
		`git -C /workspaces/%[1]s/repository.git rev-parse --verify refs/heads/task/%[2]s && test -f /workspaces/%[1]s/tasks/%[2]s/.git && echo WORKTREE_OK`, repoID, taskID))
	if err != nil || !strings.Contains(out, "WORKTREE_OK") {
		t.Fatalf("volume state: %v\n%s", err, out)
	}
	if out, _ := inVolume(t, repoID, "grep -rl -- \""+liveEnv(t, "ATM_LIVE_TOKEN")+"\" /workspaces; echo SCANNED"); !strings.Contains(out, "SCANNED") || strings.Contains(out, "/workspaces/") {
		t.Fatalf("token found in the project volume (or scan failed): %s", out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := mgr.ExportAndPushTask(ctx, transfer.PushRequest{
		ProjectID: "p", RepoID: repoID, TaskID: taskID, RemoteURL: liveRemote, AuthToken: liveEnv(t, "ATM_LIVE_TOKEN"),
	}); err != nil {
		t.Fatalf("export and push: %v", err)
	}
	refs, err := exec.Command("docker", "exec", liveEnv(t, "ATM_LIVE_FIXTURE"), "git", "-C", "/data/org/repo.git", "for-each-ref", "--format=%(refname)").CombinedOutput()
	if err != nil || !strings.Contains(string(refs), "refs/heads/task/"+taskID) {
		t.Fatalf("pushed task branch missing on fixture: %v\n%s", err, refs)
	}
}

func TestLive0C5_SubmoduleRejected(t *testing.T) {
	if _, err := ingest(t, liveManager(t), liveID(t, "sub"), "t1", "refs/heads/withsub"); !errors.Is(err, transfer.ErrSubmodulesNotSupported) {
		t.Fatalf("want ErrSubmodulesNotSupported, got %v", err)
	}
}

func TestLive0C6_LFSRejected(t *testing.T) {
	for _, ref := range []string{"refs/heads/withlfs", "refs/heads/lfsptr"} {
		if _, err := ingest(t, liveManager(t), liveID(t, "lfs"), "t1", ref); !errors.Is(err, transfer.ErrGitLFSNotSupported) {
			t.Errorf("%s: want ErrGitLFSNotSupported, got %v", ref, err)
		}
	}
}

// Live 0C-7: a sandbox controls the project volume, so it can plant hooks, a smudge filter, an
// fsmonitor command and a hooksPath in repository.git. The next ingest (the fetch path) and worktree
// creation must run none of them.
func TestLive0C7_PlantedRepoConfigNotExecuted(t *testing.T) {
	mgr := liveManager(t)
	repoID := liveID(t, "evil")
	if _, err := ingest(t, mgr, repoID, "t1", "refs/heads/main"); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	plant := fmt.Sprintf(`set -e
R=/workspaces/%s/repository.git
mkdir -p $R/hooks /workspaces/evilhooks
for h in reference-transaction post-checkout post-merge pre-auto-gc post-index-change; do
  printf '#!/bin/sh\ntouch /workspaces/PWNED_hook_%%s\n' $h > $R/hooks/$h; chmod +x $R/hooks/$h
  cp $R/hooks/$h /workspaces/evilhooks/$h
done
git -C $R config core.hooksPath /workspaces/evilhooks
git -C $R config filter.evil.smudge 'touch /workspaces/PWNED_smudge; cat'
git -C $R config filter.evil.process 'touch /workspaces/PWNED_process'
git -C $R config core.fsmonitor 'touch /workspaces/PWNED_fsmonitor'
git -C $R config core.sshCommand 'touch /workspaces/PWNED_ssh'
echo PLANTED`, repoID)
	if out, err := inVolume(t, repoID, plant); err != nil || !strings.Contains(out, "PLANTED") {
		t.Fatalf("plant: %v\n%s", err, out)
	}
	// The evil branch carries ".gitattributes: * filter=evil"; fetching it and adding a worktree must not run anything.
	if _, err := ingest(t, mgr, repoID, "t2", "refs/heads/evil"); err != nil {
		t.Logf("second ingest failed (acceptable: fail closed): %v", err)
	}
	out, _ := inVolume(t, repoID, "ls /workspaces; echo LISTED")
	if !strings.Contains(out, "LISTED") {
		t.Fatalf("could not list volume: %s", out)
	}
	if strings.Contains(out, "PWNED") {
		t.Fatalf("planted repository config was executed: %s", out)
	}

	// Positive control: an ordinary checkout of the same branch with the planted config does run it,
	// so the absence above is the helper's doing, not inert plants.
	ctl, _ := inVolume(t, repoID, fmt.Sprintf(`git -C /workspaces/%s/repository.git fetch -q /dev/null 2>/dev/null; git -C /workspaces/%s/repository.git worktree add -q /workspaces/ctl evil >/dev/null 2>&1; ls /workspaces`, repoID, repoID))
	if !strings.Contains(ctl, "PWNED") {
		t.Fatalf("positive control: plain git did not run the planted config, so this probe proves nothing: %s", ctl)
	}
}

// probeClient swaps the volume helper's command for a network probe while keeping the helper's real
// HostConfig, so the result reflects the confinement Docker actually applies to the helper.
type probeClient struct{ sandbox.DockerClient }

func (p probeClient) ContainerCreate(ctx context.Context, c *container.Config, hc *container.HostConfig, nc *network.NetworkingConfig, pl *ocispec.Platform, name string) (container.CreateResponse, error) {
	c.Cmd = []string{"/bin/sh", "-c", `{
echo "IFACES: $(sed 1,2d /proc/net/dev | cut -d: -f1 | tr -d ' ' | tr '\n' ' ')"
echo "ROUTES: $(sed 1d /proc/net/route | wc -l)"
if nc -w3 -z 1.1.1.1 80; then echo "EGRESS: REACHED"; else echo "EGRESS: BLOCKED"; fi
} > /workspaces/NETPROBE`}
	return p.DockerClient.ContainerCreate(ctx, c, hc, nc, pl, name)
}

// Live 0C-9: the volume helper has no network beyond loopback and no route, and egress fails.
func TestLive0C9_HelperHasNoNetwork(t *testing.T) {
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	repoID := liveID(t, "net")
	mgr := transfer.NewGitTransferManager(transfer.NewVolumeTransferHelper(probeClient{cli}, liveEnv(t, "ATM_LIVE_IMAGE")), liveWorker(t, liveEnv(t, "ATM_LIVE_IMAGE")))
	_, _ = ingest(t, mgr, repoID, "t1", "refs/heads/main") // the real ingest fails: the probe replaced the helper script
	out, err := inVolume(t, repoID, "cat /workspaces/NETPROBE")
	if err != nil {
		t.Fatalf("probe output missing (helper did not run the probe?): %v\n%s", err, out)
	}
	t.Log(out)
	if !strings.Contains(out, "IFACES: lo \n") && !strings.Contains(out, "IFACES: lo\n") {
		t.Errorf("helper has a non-loopback interface:\n%s", out)
	}
	if !strings.Contains(out, "ROUTES: 0") || !strings.Contains(out, "EGRESS: BLOCKED") {
		t.Errorf("helper has routes or reached the internet:\n%s", out)
	}
}
