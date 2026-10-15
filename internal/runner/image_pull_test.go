package runner_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"

	"agent-task-manager/internal/runner"
)

// expectedOpenCodeRef is written out independently of the production constant so a change
// to the pinned OpenCode repo:tag must be made deliberately in both places.
var expectedOpenCodeRef = "ghcr.io/anomalyco/opencode:2.0.26@" + runner.DefaultOpenCodeDigest

const pullEngineSecret = "SECRET-PULL-ENGINE-DETAIL"

func TestBuilder_PullsBothPinnedImagesBeforeBuild(t *testing.T) {
	catalog, profile := uncachedProfile()
	m := &mockDockerClientForBuilder{}
	want := expectedTagFor(t, catalog, profile)
	tag, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
	if err != nil || tag != want {
		t.Fatalf("tag=%q err=%v, want %q", tag, err, want)
	}
	refs, opts, order := m.pulls()
	wantRefs := []string{runner.FixtureBaseRefForTest, expectedOpenCodeRef}
	if len(refs) != len(wantRefs) || refs[0] != wantRefs[0] || refs[1] != wantRefs[1] {
		t.Fatalf("pulled refs = %v, want %v", refs, wantRefs)
	}
	for i, o := range opts {
		if o.Platform != "linux/amd64" || o.All || o.RegistryAuth != "" || o.PrivilegeFunc != nil {
			t.Errorf("pull %d options = %+v, want only Platform linux/amd64", i, o)
		}
	}
	wantOrder := []string{"pull:" + wantRefs[0], "pull:" + wantRefs[1], "build"}
	if strings.Join(order, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("call order = %v, want %v", order, wantOrder)
	}
}

// dockerfileFromTar extracts the Dockerfile from a recorded build context.
func dockerfileFromTar(t *testing.T, payload []byte) string {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(payload))
	for {
		hdr, err := tr.Next()
		if err != nil {
			t.Fatalf("no Dockerfile in build context: %v", err)
		}
		if hdr.Name == "Dockerfile" {
			b, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read Dockerfile: %v", err)
			}
			return string(b)
		}
	}
}

func TestBuilder_RenderedOpenCodeRefEqualsPulledRef(t *testing.T) {
	catalog, profile := uncachedProfile()
	m := &mockDockerClientForBuilder{}
	if _, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile); err != nil {
		t.Fatalf("build: %v", err)
	}
	refs, _, _ := m.pulls()
	if len(refs) != 2 || len(m.buildTarPayloads) != 1 {
		t.Fatalf("pulls=%v builds=%d", refs, len(m.buildTarPayloads))
	}
	df := dockerfileFromTar(t, m.buildTarPayloads[0])
	copies := regexp.MustCompile(`(?m)^COPY --from=(\S+) `).FindAllStringSubmatch(df, -1)
	if len(copies) != 1 || copies[0][1] != refs[1] {
		t.Fatalf("COPY --from refs = %v, want exactly [%s]", copies, refs[1])
	}
	froms := regexp.MustCompile(`(?m)^FROM (\S+)$`).FindAllStringSubmatch(df, -1)
	if len(froms) != 1 || froms[0][1] != refs[0] {
		t.Fatalf("FROM refs = %v, want exactly [%s]", froms, refs[0])
	}
}

func requirePullFailedNoBuild(t *testing.T, m *mockDockerClientForBuilder, tag string, err error) {
	t.Helper()
	requireBuildFailed(t, tag, err)
	if strings.Contains(err.Error(), pullEngineSecret) {
		t.Errorf("engine detail leaked into returned error: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.imageBuildCalls) != 0 {
		t.Errorf("ImageBuild must not run after a failed pull, got %d calls", len(m.imageBuildCalls))
	}
	if len(m.imageInspectCalls) != 0 {
		t.Errorf("no inspect after a failed pull, got %d", len(m.imageInspectCalls))
	}
}

func TestBuilder_PullAPIErrorPreventsBuild(t *testing.T) {
	for _, failIdx := range []int{0, 1} {
		t.Run([]string{"base", "opencode"}[failIdx], func(t *testing.T) {
			logged := captureBuildLog(t)
			catalog, profile := uncachedProfile()
			n := 0
			m := &mockDockerClientForBuilder{
				imagePullFunc: func(ctx context.Context, ref string, o image.PullOptions) (io.ReadCloser, error) {
					defer func() { n++ }()
					if n == failIdx {
						return nil, errors.New(pullEngineSecret)
					}
					return streamBody(pullOKStream), nil
				},
			}
			tag, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
			requirePullFailedNoBuild(t, m, tag, err)
			if refs, _, _ := m.pulls(); len(refs) != failIdx+1 {
				t.Errorf("pulls after failure must stop: got %v", refs)
			}
			if !strings.Contains(logged(), pullEngineSecret) {
				t.Errorf("sanitized engine detail should be logged: %q", logged())
			}
		})
	}
}

func TestBuilder_PullStreamFailuresPreventBuild(t *testing.T) {
	for name, body := range map[string]string{
		"errorDetail":        pullOKStream + `{"errorDetail":{"message":"` + pullEngineSecret + `"},"error":"` + pullEngineSecret + `"}` + "\n",
		"error only":         `{"status":"Pulling"}` + "\n" + `{"error":"` + pullEngineSecret + `"}` + "\n",
		"errorDetail only":   `{"errorDetail":{"message":"` + pullEngineSecret + `"}}` + "\n",
		"truncated json":     `{"status":"Pulling"}` + "\n" + `{"status":"Downl`,
		"garbage":            "not json at all " + pullEngineSecret,
		"empty":              "",
		"null message":       "null\n",
		"json array message": `[1,2]` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			catalog, profile := uncachedProfile()
			m := &mockDockerClientForBuilder{
				imagePullFunc: func(ctx context.Context, ref string, o image.PullOptions) (io.ReadCloser, error) {
					return streamBody(body), nil
				},
			}
			tag, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
			requirePullFailedNoBuild(t, m, tag, err)
			if refs, _, _ := m.pulls(); len(refs) != 1 {
				t.Errorf("first failed pull must stop further pulls: %v", refs)
			}
		})
	}
}

// errAfterReader returns data then a non-EOF read error.
type errAfterReader struct{ r io.Reader }

func (e *errAfterReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err == io.EOF {
		return n, errors.New(pullEngineSecret)
	}
	return n, err
}

func TestBuilder_PullStreamReaderErrorPreventsBuild(t *testing.T) {
	catalog, profile := uncachedProfile()
	m := &mockDockerClientForBuilder{
		imagePullFunc: func(ctx context.Context, ref string, o image.PullOptions) (io.ReadCloser, error) {
			return io.NopCloser(&errAfterReader{r: strings.NewReader(pullOKStream)}), nil
		},
	}
	tag, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
	requirePullFailedNoBuild(t, m, tag, err)
}

// closeRecorder records Close on an otherwise ordinary body.
type closeRecorder struct {
	io.Reader
	closed int
}

func (c *closeRecorder) Close() error { c.closed++; return nil }

func TestBuilder_PullBodiesAreClosed(t *testing.T) {
	catalog, profile := uncachedProfile()
	var bodies []*closeRecorder
	m := &mockDockerClientForBuilder{
		imagePullFunc: func(ctx context.Context, ref string, o image.PullOptions) (io.ReadCloser, error) {
			b := &closeRecorder{Reader: strings.NewReader(pullOKStream)}
			bodies = append(bodies, b)
			return b, nil
		},
	}
	if _, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile); err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 pull bodies, got %d", len(bodies))
	}
	for i, b := range bodies {
		if b.closed == 0 {
			t.Errorf("pull body %d was not closed", i)
		}
	}
}

func TestBuilder_CacheHitDoesNoPull(t *testing.T) {
	catalog, profile := uncachedProfile()
	tag := expectedTagFor(t, catalog, profile)
	m := &mockDockerClientForBuilder{
		imageListFunc: func(ctx context.Context, o image.ListOptions) ([]image.Summary, error) {
			return []image.Summary{{RepoTags: []string{tag}}}, nil
		},
	}
	got, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
	if err != nil || got != tag {
		t.Fatalf("tag=%q err=%v", got, err)
	}
	if refs, _, _ := m.pulls(); len(refs) != 0 {
		t.Fatalf("cache hit must not pull, got %v", refs)
	}
}

func TestBuilder_ImageListErrorDoesNoPull(t *testing.T) {
	catalog, profile := uncachedProfile()
	m := &mockDockerClientForBuilder{
		imageListFunc: func(ctx context.Context, o image.ListOptions) ([]image.Summary, error) {
			return nil, errors.New("list down")
		},
	}
	tag, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
	requireBuildFailed(t, tag, err)
	if refs, _, _ := m.pulls(); len(refs) != 0 {
		t.Fatalf("list failure must not pull, got %v", refs)
	}
}

func TestBuilder_CancelDuringPullClosesBodyAndSkipsBuild(t *testing.T) {
	catalog, profile := uncachedProfile()
	started := make(chan *fakeStream, 4)
	m := &mockDockerClientForBuilder{
		imagePullFunc: func(ctx context.Context, ref string, o image.PullOptions) (io.ReadCloser, error) {
			fs := newFakeStream(true)
			started <- fs
			return fs, nil
		},
	}
	b := newTestBuilder(m, catalog)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan buildOutcome, 1)
	go func() {
		tag, err := b.BuildOrGetImage(ctx, profile)
		done <- buildOutcome{tag, err}
	}()
	var fs *fakeStream
	select {
	case fs = <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("pull never started")
	}
	waitUntil(t, "one waiter", func() bool { return runner.InFlightWaitersForTest(b) == 1 })
	cancel()
	o := recv(t, done)
	if !errors.Is(o.err, context.Canceled) || o.tag != "" {
		t.Fatalf("tag=%q err=%v, want context.Canceled", o.tag, o.err)
	}
	requireClosed(t, "pull body closed on cancel", fs.closed)
	requireClosed(t, "pull read aborted", fs.readDone)
	waitUntil(t, "flight cleanup", func() bool { return runner.InFlightCountForTest(b) == 0 })
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.imageBuildCalls) != 0 {
		t.Fatalf("ImageBuild must not run after a cancelled pull, got %d", len(m.imageBuildCalls))
	}
	if len(m.pullRefs) != 1 {
		t.Fatalf("no further pulls after cancel, got %v", m.pullRefs)
	}
}

func TestBuilder_PullTimeoutAbortsWithDeadline(t *testing.T) {
	catalog, profile := uncachedProfile()
	started := make(chan *fakeStream, 4)
	m := &mockDockerClientForBuilder{
		imagePullFunc: func(ctx context.Context, ref string, o image.PullOptions) (io.ReadCloser, error) {
			fs := newFakeStream(true)
			started <- fs
			return fs, nil
		},
		imageBuildFunc: func(ctx context.Context, bc io.Reader, o types.ImageBuildOptions) (types.ImageBuildResponse, error) {
			t.Error("ImageBuild must not run after a timed-out pull")
			return types.ImageBuildResponse{}, errors.New("unexpected")
		},
	}
	b := runner.NewRuntimeImageBuilderWithTimeoutForTest(m, catalog, runner.EmbeddedTemplateForTest(), runner.ExpectedTemplateSHA256, 20*time.Millisecond)
	_, err := b.BuildOrGetImage(context.Background(), profile)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, runner.ErrImageBuildFailed) {
		t.Fatalf("expected ErrImageBuildFailed wrapped with DeadlineExceeded, got %v", err)
	}
	fs := <-started
	requireClosed(t, "pull body closed on timeout", fs.closed)
}

func TestPullPinnedImage_RefusesUnpinnedRefsWithoutDockerCall(t *testing.T) {
	hex64 := strings.Repeat("ab", 32)
	for name, ref := range map[string]string{
		"empty":            "",
		"tag only":         "alpine:3.24",
		"latest":           "alpine:latest",
		"bare repo":        "ghcr.io/anomalyco/opencode",
		"opencode no pin":  "ghcr.io/anomalyco/opencode:2.0.26",
		"short digest":     "alpine:3.24@sha256:abcd",
		"uppercase digest": "alpine:3.24@sha256:" + strings.ToUpper(hex64),
		"sha512":           "alpine:3.24@sha512:" + hex64 + hex64,
		"long digest":      "alpine:3.24@sha256:" + hex64 + "00",
		"trailing newline": "alpine:3.24@sha256:" + hex64 + "\n",
		"leading space":    " alpine:3.24@sha256:" + hex64,
		"digest only":      "@sha256:" + hex64,
	} {
		t.Run(name, func(t *testing.T) {
			m := &mockDockerClientForBuilder{}
			err := runner.PullPinnedImageForTest(context.Background(), m, ref)
			if !errors.Is(err, runner.ErrImageBuildFailed) {
				t.Fatalf("expected ErrImageBuildFailed, got %v", err)
			}
			if refs, _, _ := m.pulls(); len(refs) != 0 {
				t.Fatalf("unpinned ref reached ImagePull: %v", refs)
			}
		})
	}
}

func TestPullPinnedImage_PinnedRefPullsWithPlatform(t *testing.T) {
	ref := "alpine:3.24@sha256:" + strings.Repeat("ab", 32)
	m := &mockDockerClientForBuilder{}
	if err := runner.PullPinnedImageForTest(context.Background(), m, ref); err != nil {
		t.Fatalf("pull: %v", err)
	}
	refs, opts, _ := m.pulls()
	if len(refs) != 1 || refs[0] != ref || opts[0].Platform != "linux/amd64" {
		t.Fatalf("refs=%v opts=%+v", refs, opts)
	}
}
