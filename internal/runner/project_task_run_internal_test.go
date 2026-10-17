package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-task-manager/internal/config"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/sandbox"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// ptrDocker scripts docker exec: handler maps argv to (exit code, output) and every call is recorded.
type ptrDocker struct {
	*sandbox.MockDockerClient
	mu      sync.Mutex
	handler func(argv []string) (int, string)
	calls   [][]string
	workdir []string
	pending map[string]ptrExecResult
	next    int
}

type ptrExecResult struct {
	exit int
	out  string
}

func (d *ptrDocker) ContainerExecCreate(_ context.Context, _ string, o container.ExecOptions) (types.IDResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pending == nil {
		d.pending = map[string]ptrExecResult{}
	}
	d.next++
	id := "exec-" + strconv.Itoa(d.next)
	exit, out := d.handler(o.Cmd)
	d.calls = append(d.calls, o.Cmd)
	d.workdir = append(d.workdir, o.WorkingDir)
	d.pending[id] = ptrExecResult{exit, out}
	return types.IDResponse{ID: id}, nil
}

func (d *ptrDocker) ContainerExecAttach(_ context.Context, id string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
	d.mu.Lock()
	res := d.pending[id]
	d.mu.Unlock()
	var mux bytes.Buffer
	_, _ = stdcopy.NewStdWriter(&mux, stdcopy.Stdout).Write([]byte(res.out))
	c, s := net.Pipe()
	_ = s.Close()
	return types.HijackedResponse{Conn: c, Reader: bufio.NewReader(&mux)}, nil
}

func (d *ptrDocker) ContainerExecInspect(_ context.Context, id string) (container.ExecInspect, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return container.ExecInspect{ExitCode: d.pending[id].exit}, nil
}

func (d *ptrDocker) argvs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for _, c := range d.calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

func (d *ptrDocker) ran(sub string) bool {
	for _, c := range d.argvs() {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

type ptrProjectRepo struct {
	repository.ProjectRepository
	p *models.Project
}

func (r *ptrProjectRepo) GetByID(context.Context, string) (*models.Project, error) { return r.p, nil }

type ptrTaskRepo struct{ repository.TaskRepository }

func (ptrTaskRepo) Update(context.Context, *models.Task) error { return nil }

type ptrIntegRepo struct {
	repository.IntegrationRepository
	list []models.ProjectIntegration
}

func (r *ptrIntegRepo) ListByProjectID(context.Context, string) ([]models.ProjectIntegration, error) {
	return r.list, nil
}

type ptrCredRepo struct {
	repository.CredentialRepository
	enc string
}

func (r *ptrCredRepo) GetByID(context.Context, string) (*models.Credential, error) {
	return &models.Credential{EncryptedData: r.enc}, nil
}

type ptrPR struct {
	integrations.PullRequestProvider
	branch string
	n      int
}

func (p *ptrPR) CreatePullRequest(_ context.Context, _ *models.ProjectIntegration, _ string, branch, _, _ string) (string, error) {
	p.n++
	p.branch = branch
	return "https://example.invalid/pr/1", nil
}

type ptrSandbox struct {
	ProjectSandboxManager
	called  int
	imgSeen string
	noRoute bool
}

func (s *ptrSandbox) EnsureProjectContainer(_ context.Context, p *models.Project, img string) (*ProjectRouteHandle, error) {
	s.called++
	s.imgSeen = img
	return &ProjectRouteHandle{ProjectID: p.ID, ContainerID: "c1", EndpointURL: "http://127.0.0.1:4096", AuthPassword: "pw"}, nil
}

// ptrOC reports the outcomes in order, one per prompt, with an UpdatedAt that advances on every prompt.
type ptrOC struct {
	OpenCodeClient
	outcomes []string
	prompts  []string
	bound    string
	updated  time.Time
	status   string

	mu         sync.Mutex
	childPolls int
	msgs       []OpenCodeMessageDTO
	children   []OpenCodeSessionDTO
	childMsgs  map[string][]OpenCodeMessageDTO
}

func (o *ptrOC) EnsureSessionAtLocation(_ context.Context, _, _, dir string) error {
	o.bound = dir
	return nil
}

func (o *ptrOC) GetSession(_ context.Context, id string) (*OpenCodeSessionDTO, error) {
	u := o.updated
	return &OpenCodeSessionDTO{ID: id, UpdatedAt: &u, Status: o.status}, nil
}

func (o *ptrOC) PromptSession(_ context.Context, _, p string) error {
	o.prompts = append(o.prompts, p)
	o.updated = o.updated.Add(time.Second)
	o.status = o.outcomes[len(o.prompts)-1]
	return nil
}

type ptrFixture struct {
	r     *AgentTaskRunner
	dk    *ptrDocker
	sb    *ptrSandbox
	oc    *ptrOC
	tm    *fakeTransferMgr
	pr    *ptrPR
	task  *models.Task
	atmpt *models.TaskAttempt
}

// newPtrFixture builds a runner whose docker handler answers a Go project (verify = go test ./...).
// verifyExits is consumed per verification run; changes controls `git diff --cached --quiet`.
func newPtrFixture(t *testing.T, outcomes []string, verifyExits []int, changes bool) *ptrFixture {
	t.Helper()
	cipher, err := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := cipher.Encrypt("ghp_testtoken")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATM_VERIFY_CMD", "")
	f := &ptrFixture{
		sb:    &ptrSandbox{},
		oc:    &ptrOC{outcomes: outcomes, updated: time.Unix(1000, 0), status: "succeeded"},
		tm:    &fakeTransferMgr{},
		pr:    &ptrPR{},
		task:  &models.Task{ID: "t1", ProjectID: "p1", Title: "Add thing", Prompt: "do it"},
		atmpt: &models.TaskAttempt{ID: "a1"},
	}
	verifyRun := 0
	f.dk = &ptrDocker{MockDockerClient: sandbox.NewMockDockerClient()}
	f.dk.handler = func(argv []string) (int, string) {
		joined := strings.Join(argv, " ")
		switch {
		case strings.Contains(joined, "go.mod"):
			return 0, "go test ./...\n"
		case joined == "sh -c go test ./...":
			code := 0
			if verifyRun < len(verifyExits) {
				code = verifyExits[verifyRun]
			}
			verifyRun++
			return code, "FAIL: TestX\n"
		case strings.Contains(joined, "rev-parse"):
			return 0, "basesha\n"
		case strings.Contains(joined, "diff --cached --quiet"):
			if changes {
				return 1, ""
			}
			return 0, ""
		case strings.Contains(joined, "--numstat"):
			return 0, "3\t1\ta.go\n-\t-\timg.png\n"
		}
		return 0, ""
	}
	f.r = &AgentTaskRunner{
		cfg:         &config.Config{MaxVerificationRetries: 3, VerificationTimeoutSeconds: 30},
		projectRepo: &ptrProjectRepo{p: &models.Project{ID: "p1"}},
		taskRepo:    ptrTaskRepo{},
		dockerCli:   f.dk,
		integRepo: &ptrIntegRepo{list: []models.ProjectIntegration{{
			ID: "i1", ProjectID: "p1", Provider: models.IntegrationGitHub, Enabled: true, ExternalID: "org/repo", CredentialID: "c",
		}}},
		credRepo:          &ptrCredRepo{enc: enc},
		cipher:            cipher,
		ghProvider:        f.pr,
		projectSandboxMgr: f.sb,
		repoTransfer:      NewTaskRepoTransfer(f.tm),
		projectImage:      "img@sha256:abc",
		sessionPoll:       time.Millisecond,
		projectClientFor:  func(string, string) (OpenCodeClient, error) { return f.oc, nil },
	}
	return f
}

func (f *ptrFixture) run(t *testing.T) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return f.r.runTaskInProjectContainer(ctx, f.task, f.atmpt)
}

func TestProjectTaskRun_HappyPathCommitsPushesAndOpensPR(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, []int{0}, true)
	if err := f.run(t); err != nil {
		t.Fatal(err)
	}
	if f.tm.ingest == nil || f.tm.ingest.AuthToken != "ghp_testtoken" || f.tm.ingest.RepoID != "p1" || f.tm.ingest.BaseRef != "refs/heads/main" {
		t.Fatalf("ingest %+v", f.tm.ingest)
	}
	if f.sb.imgSeen != "img@sha256:abc" || f.oc.bound != "/workspaces/p1/tasks/t1" {
		t.Fatalf("image %q bound %q", f.sb.imgSeen, f.oc.bound)
	}
	if !f.dk.ran("add -A") || !f.dk.ran("commit -q -m Add thing") {
		t.Fatalf("expected add and commit, got %v", f.dk.argvs())
	}
	for _, c := range f.dk.argvs() {
		if strings.HasPrefix(c, "git ") && !strings.Contains(c, "core.hooksPath=/dev/null") {
			t.Fatalf("git call without hardening: %s", c)
		}
	}
	for _, w := range f.dk.workdir {
		if w != "/workspaces/p1/tasks/t1" {
			t.Fatalf("exec ran outside the task worktree: %q", w)
		}
	}
	if f.tm.push == nil || f.tm.push.RemoteURL != "https://github.com/org/repo.git" || f.pr.n != 1 || f.pr.branch != "task/t1" {
		t.Fatalf("push %+v prs %d branch %q", f.tm.push, f.pr.n, f.pr.branch)
	}
	if f.task.BranchName == nil || *f.task.BranchName != "task/t1" {
		t.Fatalf("branch not recorded")
	}
}

func TestProjectTaskRun_NoChangesPushesNothing(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, []int{0}, false)
	if err := f.run(t); err != nil {
		t.Fatal(err)
	}
	if f.dk.ran("commit") || f.tm.push != nil || f.pr.n != 0 {
		t.Fatalf("no-change run committed or pushed: %v push=%v prs=%d", f.dk.argvs(), f.tm.push, f.pr.n)
	}
}

func TestProjectTaskRun_VerificationFailureRepromptsThenPasses(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded", "succeeded"}, []int{1, 0}, true)
	if err := f.run(t); err != nil {
		t.Fatal(err)
	}
	if len(f.oc.prompts) != 2 || f.oc.prompts[0] != "do it" || !strings.Contains(f.oc.prompts[1], "Pre-flight verification failed") {
		t.Fatalf("prompts %q", f.oc.prompts)
	}
}

func TestProjectTaskRun_PersistentVerificationFailureFailsAndPushesNothing(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded", "succeeded", "succeeded"}, []int{1, 1, 1}, true)
	err := f.run(t)
	if err == nil || !strings.Contains(err.Error(), "verification failed after 3 iterations") {
		t.Fatalf("got %v", err)
	}
	if f.atmpt.FailureReason == nil || f.dk.ran("commit") || f.tm.push != nil || f.pr.n != 0 {
		t.Fatalf("failed run must not commit or push (reason=%v)", f.atmpt.FailureReason)
	}
}

func TestProjectTaskRun_FailedSessionStopsBeforeCommit(t *testing.T) {
	f := newPtrFixture(t, []string{"failed"}, nil, true)
	err := f.run(t)
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("got %v", err)
	}
	if f.dk.ran("add -A") || f.tm.push != nil {
		t.Fatalf("committed or pushed after a failed session: %v", f.dk.argvs())
	}
}

func TestProjectTaskRun_StaleTerminalOutcomeIsNotMistakenForCompletion(t *testing.T) {
	// A session that has not advanced past its pre-prompt UpdatedAt must keep being polled until ctx ends.
	f := newPtrFixture(t, []string{"succeeded"}, []int{0}, true)
	stale := &ptrStaleOC{ptrOC: f.oc}
	f.r.projectClientFor = func(string, string) (OpenCodeClient, error) { return stale, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := f.r.runTaskInProjectContainer(ctx, f.task, f.atmpt)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
	if f.dk.ran("add -A") {
		t.Fatal("committed on a stale outcome")
	}
}

// ptrStaleOC never advances UpdatedAt after a prompt.
type ptrStaleOC struct{ *ptrOC }

func (o *ptrStaleOC) PromptSession(context.Context, string, string) error { return nil }

func TestProjectTaskRun_RequiresIntegrationAndConfigBeforeTouchingAnything(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, nil, true)
	f.r.integRepo = &ptrIntegRepo{}
	if err := f.run(t); err == nil || f.tm.ingest != nil || f.sb.called != 0 {
		t.Fatalf("no integration must fail before ingest/container: err=%v ingest=%v called=%d", err, f.tm.ingest, f.sb.called)
	}
	f = newPtrFixture(t, []string{"succeeded"}, nil, true)
	f.r.projectImage = ""
	if err := f.run(t); err == nil || f.tm.ingest != nil || f.sb.called != 0 {
		t.Fatalf("missing image must fail first: err=%v", err)
	}
	f = newPtrFixture(t, []string{"succeeded"}, nil, true)
	f.r.repoTransfer = nil
	if err := f.run(t); err == nil || f.sb.called != 0 {
		t.Fatalf("missing transfer must fail first: err=%v", err)
	}
}

func TestProjectTaskRun_AutoPRDisabledStillCommitsButDoesNotPush(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, []int{0}, true)
	f.r.integRepo = &ptrIntegRepo{list: []models.ProjectIntegration{{
		ID: "i1", ProjectID: "p1", Provider: models.IntegrationGitHub, Enabled: true, ExternalID: "org/repo", CredentialID: "c",
		Config: map[string]any{"auto_pr": false},
	}}}
	if err := f.run(t); err != nil {
		t.Fatal(err)
	}
	if !f.dk.ran("commit") || f.tm.push != nil || f.pr.n != 0 {
		t.Fatalf("auto_pr=false: commit=%v push=%v prs=%d", f.dk.ran("commit"), f.tm.push, f.pr.n)
	}
}

func TestParseNumstat(t *testing.T) {
	files, a, d := parseNumstat("3\t1\ta.go\n-\t-\timg.png\n10\t0\tb/c.go\n")
	if files != 3 || a != 13 || d != 1 {
		t.Fatalf("got %d files +%d -%d", files, a, d)
	}
	if f, a, d := parseNumstat(""); f != 0 || a != 0 || d != 0 {
		t.Fatalf("empty: %d %d %d", f, a, d)
	}
}

func TestExecInContainer_ReturnsExitCodeAndCapsOutput(t *testing.T) {
	d := &ptrDocker{MockDockerClient: sandbox.NewMockDockerClient()}
	d.handler = func([]string) (int, string) { return 7, strings.Repeat("x", containerExecOutputCap+100) }
	res, err := execInContainer(context.Background(), d, "c", "/w", []string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 7 || len(res.Output) != containerExecOutputCap {
		t.Fatalf("exit %d len %d", res.ExitCode, len(res.Output))
	}
}

func (o *ptrOC) ListChildSessions(context.Context, string) ([]OpenCodeSessionDTO, error) {
	o.mu.Lock()
	o.childPolls++
	defer o.mu.Unlock()
	return o.children, nil
}

type ptrResolver struct {
	RuntimeResolver
	proj *models.Project
	err  error
}

func (r *ptrResolver) ResolveMultiRepo(_ context.Context, m map[string]*RepositoryManifest, p *models.Project) (*ResolvedRuntimeProfile, error) {
	r.proj = p
	if len(m) != 0 {
		panic("manifests are not read yet")
	}
	return &ResolvedRuntimeProfile{}, r.err
}

type ptrBuilder struct {
	tag string
	err error
	n   int
}

func (b *ptrBuilder) BuildOrGetImage(context.Context, *ResolvedRuntimeProfile) (string, error) {
	b.n++
	return b.tag, b.err
}

func TestProjectTaskRun_UsesBuiltImageForResolvedProfile(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, []int{0}, true)
	f.r.projectImage = ""
	res, b := &ptrResolver{}, &ptrBuilder{tag: "built@sha256:def"}
	f.r.imageResolver, f.r.imageBuilder = res, b
	if err := f.run(t); err != nil {
		t.Fatal(err)
	}
	if f.sb.imgSeen != "built@sha256:def" || res.proj == nil || res.proj.ID != "p1" || b.n != 1 {
		t.Fatalf("image %q proj %v builds %d", f.sb.imgSeen, res.proj, b.n)
	}
}

func TestProjectTaskRun_ImageBuildOrResolveFailureStopsBeforeContainer(t *testing.T) {
	for name, set := range map[string]func(*ptrFixture){
		"build": func(f *ptrFixture) {
			f.r.imageResolver, f.r.imageBuilder = &ptrResolver{}, &ptrBuilder{err: errors.New("boom")}
		},
		"resolve": func(f *ptrFixture) {
			f.r.imageResolver, f.r.imageBuilder = &ptrResolver{err: errors.New("bad")}, &ptrBuilder{}
		},
	} {
		f := newPtrFixture(t, []string{"succeeded"}, nil, true)
		f.r.projectImage = ""
		set(f)
		if err := f.run(t); err == nil || f.sb.called != 0 || f.tm.ingest != nil {
			t.Fatalf("%s: err=%v called=%d", name, err, f.sb.called)
		}
	}
}

func TestProjectTaskRun_InjectsSkillsAndMCPAsBase64AndExcludesThemFromTheCommit(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, []int{0}, true)
	f.task.Skills = []string{"s1"}
	f.task.MCPServers = []string{"m';rm -rf /;'"}
	if err := f.run(t); err != nil {
		t.Fatal(err)
	}
	var inject string
	for _, c := range f.dk.argvs() {
		if strings.Contains(c, "info/exclude") {
			inject = c
		}
	}
	if inject == "" || !strings.Contains(inject, "/opencode.json /.claude/config.json") {
		t.Fatalf("no injection with exclude: %v", f.dk.argvs())
	}
	if strings.Contains(inject, "rm -rf") {
		t.Fatalf("server name reached the shell script unencoded: %s", inject)
	}
	if !strings.Contains(inject, base64.StdEncoding.EncodeToString([]byte(`{"skills":["s1"],"mcp_servers":["m';rm -rf /;'"]}`))) {
		t.Fatalf("opencode.json payload missing: %s", inject)
	}
}

func TestProjectTaskRun_NoSkillsNoInjection(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, []int{0}, true)
	if err := f.run(t); err != nil {
		t.Fatal(err)
	}
	if f.dk.ran("info/exclude") {
		t.Fatal("injected config for a task with no skills or MCP servers")
	}
}

func TestProjectTaskRun_WatchesChildSubagentsOnTheProjectClient(t *testing.T) {
	f := newPtrFixture(t, []string{"succeeded"}, []int{0}, true)
	if err := f.run(t); err != nil {
		t.Fatal(err)
	}
	if f.r.openCodeClient != nil {
		t.Fatal("fixture must not have a host client")
	}
	// The watcher does a final poll when its turn ends, on the project client it was handed.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.oc.mu.Lock()
		n := f.oc.childPolls
		f.oc.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child sessions were never polled on the project client")
}

// The fake docker cannot show what the script does, so run the real script against a real repository.
func TestInjectConfigScript_WritesFilesAndKeepsThemOutOfGitStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "i"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	oc, cl := `{"skills":["s"]}`, `{"skills":["c"]}`
	cmd := exec.Command("sh", "-c", injectConfigScript, "sh",
		base64.StdEncoding.EncodeToString([]byte(oc)), base64.StdEncoding.EncodeToString([]byte(cl)))
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script: %v %s", err, out)
	}
	for path, want := range map[string]string{"opencode.json": oc, ".claude/config.json": cl} {
		got, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", path, got, err)
		}
	}
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("injected files are visible to git add -A: %q %v", out, err)
	}
}
