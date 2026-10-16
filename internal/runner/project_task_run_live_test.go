//go:build live

package runner

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	dockerclient "github.com/docker/docker/client"

	"agent-task-manager/internal/models"
)

// Live probe for the container-based task run. It drives runTaskInProjectContainer against a real
// project container running real OpenCode behind the Gate 0G gateway (deploy/gateway/qualify). The sandbox network is
// not reachable from the VM host, so the OpenCode API is reached with `docker exec ... curl`.
//
//	ATM_LIVE_CONTAINER  running project container (started like deploy/gateway/qualify/container-probes.sh)
//	ATM_LIVE_PW         its OpenCode password
//	ATM_LIVE_WORKTREE   a git repo inside it, owned by uid 1000, with a clean tree and one commit
//
// The git transfer and push legs are covered by the Gate 0C live probes; here the transfer is a stub that returns
// ATM_LIVE_WORKTREE and auto_pr is off.

type execCurlTransport struct{ container string }

func (e execCurlTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	args := []string{"exec", "-i", e.container, "curl", "-s", "-i", "--http1.1", "-m", "300", "-H", "Expect:", "-X", req.Method}
	for k, vs := range req.Header {
		for _, v := range vs {
			args = append(args, "-H", k+": "+v)
		}
	}
	args = append(args, "--data-binary", "@-", req.URL.String())
	cmd := exec.CommandContext(req.Context(), "docker", args...)
	if req.Body != nil {
		cmd.Stdin = req.Body
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(&out), req)
	if err != nil {
		return nil, err
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body = io.NopCloser(bytes.NewReader(b))
	return resp, nil
}

func TestLiveProjectTaskRun_RealOpenCodeCommitsAChange(t *testing.T) {
	ctr, pw, wt := os.Getenv("ATM_LIVE_CONTAINER"), os.Getenv("ATM_LIVE_PW"), os.Getenv("ATM_LIVE_WORKTREE")
	if ctr == "" || pw == "" || wt == "" {
		t.Skip("ATM_LIVE_CONTAINER, ATM_LIVE_PW and ATM_LIVE_WORKTREE are required")
	}
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	verify, wantErr := "test -f live-hello.txt", false
	if os.Getenv("ATM_LIVE_EXPECT_VERIFY_FAIL") != "" {
		verify, wantErr = "test -f never-created.txt", true
	}
	f := newPtrFixture(t, []string{"succeeded"}, nil, true)
	t.Setenv("ATM_VERIFY_CMD", verify)
	f.r.cfg.MaxVerificationRetries = 1
	f.r.dockerCli = cli
	f.r.sessionPoll = 2 * time.Second
	f.r.projectSandboxMgr = liveSandbox{id: ctr, pw: pw}
	f.r.projectClientFor = func(url, pw string) (OpenCodeClient, error) {
		return NewProjectOpenCodeClient(url, pw, &http.Client{Transport: execCurlTransport{container: ctr}, Timeout: 320 * time.Second})
	}
	f.tm.worktree = wt
	f.r.integRepo.(*ptrIntegRepo).list[0].Config = models.JSONMap{"auto_pr": false}
	f.task.Prompt = "MODE:tool CMD[touch live-hello.txt]"

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	err = f.r.runTaskInProjectContainer(ctx, f.task, f.atmpt)
	if wantErr {
		if err == nil {
			t.Fatal("a failing verification command must fail the run")
		}
		t.Logf("failed as expected: %v", err)
		out, _ := exec.Command("docker", "exec", ctr, "git", "-C", wt, "log", "--oneline").CombinedOutput()
		if strings.Contains(string(out), "Add thing") {
			t.Fatalf("a run whose verification failed must not commit:\n%s", out)
		}
		return
	}
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out, err := exec.Command("docker", "exec", ctr, "git", "-C", wt, "log", "--stat", "-1", "--format=%s").CombinedOutput()
	t.Logf("last commit:\n%s", out)
	if err != nil || !strings.Contains(string(out), "live-hello.txt") {
		t.Fatalf("the agent's file was not committed: %v", err)
	}
}

type liveSandbox struct {
	ProjectSandboxManager
	id, pw string
}

func (s liveSandbox) EnsureProjectContainer(_ context.Context, p *models.Project, _ string) (*ProjectRouteHandle, error) {
	return &ProjectRouteHandle{ProjectID: p.ID, ContainerID: s.id, EndpointURL: "http://127.0.0.1:4096", AuthPassword: s.pw}, nil
}
