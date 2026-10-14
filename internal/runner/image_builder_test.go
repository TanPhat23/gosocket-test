package runner_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/sandbox"
)

type mockDockerClientForBuilder struct {
	mu sync.Mutex

	// Mock ImageList
	imageListFunc  func(ctx context.Context, options image.ListOptions) ([]image.Summary, error)
	imageListCalls []image.ListOptions

	// Mock ImageBuild
	imageBuildFunc   func(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error)
	imageBuildCalls  []types.ImageBuildOptions
	buildTarPayloads [][]byte

	// Mock ImageInspectWithRaw
	imageInspectFunc  func(ctx context.Context, imageID string) (types.ImageInspect, []byte, error)
	imageInspectCalls []string
}

func (m *mockDockerClientForBuilder) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	return container.CreateResponse{}, nil
}
func (m *mockDockerClientForBuilder) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	return nil
}
func (m *mockDockerClientForBuilder) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	return nil, nil
}
func (m *mockDockerClientForBuilder) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockDockerClientForBuilder) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	return nil
}
func (m *mockDockerClientForBuilder) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	return nil
}
func (m *mockDockerClientForBuilder) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	return types.ContainerJSON{}, nil
}

func (m *mockDockerClientForBuilder) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockDockerClientForBuilder) ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	m.mu.Lock()
	m.imageBuildCalls = append(m.imageBuildCalls, options)
	if buildContext != nil {
		data, _ := io.ReadAll(buildContext)
		m.buildTarPayloads = append(m.buildTarPayloads, data)
	}
	fn := m.imageBuildFunc
	m.mu.Unlock() // never hold the mock lock while a (possibly blocking) build func runs
	if fn != nil {
		return fn(ctx, buildContext, options)
	}
	return types.ImageBuildResponse{Body: streamBody(legacyOKStream(""))}, nil
}
func (m *mockDockerClientForBuilder) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	m.mu.Lock()
	m.imageListCalls = append(m.imageListCalls, options)
	fn := m.imageListFunc
	m.mu.Unlock() // never hold the mock lock while a (possibly blocking) list func runs
	if fn != nil {
		return fn(ctx, options)
	}
	return nil, nil
}
func (m *mockDockerClientForBuilder) ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error) {
	m.mu.Lock()
	m.imageInspectCalls = append(m.imageInspectCalls, imageID)
	fn := m.imageInspectFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, imageID)
	}
	return validInspect(imageID, ""), nil, nil
}
func (m *mockDockerClientForBuilder) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	return network.CreateResponse{}, nil
}
func (m *mockDockerClientForBuilder) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	return network.Inspect{}, nil
}
func (m *mockDockerClientForBuilder) NetworkRemove(ctx context.Context, networkID string) error {
	return nil
}
func (m *mockDockerClientForBuilder) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	return volume.Volume{}, nil
}
func (m *mockDockerClientForBuilder) VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error) {
	return volume.Volume{}, nil
}
func (m *mockDockerClientForBuilder) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	return nil
}

func (m *mockDockerClientForBuilder) CopyToContainer(ctx context.Context, containerID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error {
	return nil
}

// expectedTagFor mirrors the builder's render step (same TemplateData, same catalog
// order) and computes the full-identity tag the builder is expected to use.
func expectedTagFor(t *testing.T, catalog runner.RuntimeCatalog, profile *runner.ResolvedRuntimeProfile) string {
	t.Helper()
	tplBytes := runner.EmbeddedTemplateForTest()
	rendered, err := runner.RenderDockerfileForTest(catalog, profile)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_, tag, err := runner.ComputeImageIdentity(profile, catalog, tplBytes, rendered)
	if err != nil {
		t.Fatalf("ComputeImageIdentity: %v", err)
	}
	return tag
}

// Synthetic, obviously fake image ID.
var fakeImageID = "sha256:" + strings.Repeat("cd", 32)

func streamBody(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }

// legacyOKStream is a successful legacy-builder stream; auxID is optional.
func legacyOKStream(auxID string) string {
	s := `{"stream":"Step 1/2 : FROM base\n"}` + "\n" + `{"stream":"Successfully built\n"}` + "\n"
	if auxID != "" {
		s += `{"aux":{"ID":"` + auxID + `"}}` + "\n"
	}
	return s
}

// validInspect returns an inspect result matching what the builder must require for tag.
func validInspect(tag, _ string) types.ImageInspect {
	return types.ImageInspect{
		ID:           fakeImageID,
		Os:           "linux",
		Architecture: "amd64",
		Config: &container.Config{Labels: map[string]string{
			"io.atm.runtime.identity":        strings.TrimPrefix(tag, "atm-runtime:"),
			"io.atm.runtime.identity-scheme": "v3",
		}},
	}
}

func newTestBuilder(cli *mockDockerClientForBuilder, catalog runner.RuntimeCatalog) runner.RuntimeImageBuilder {
	return runner.NewRuntimeImageBuilderForTest(cli, catalog, runner.EmbeddedTemplateForTest(), runner.ExpectedTemplateSHA256)
}

func TestRuntimeImageBuilder_CachedImageReturnsImmediately(t *testing.T) {
	catalog := runner.FixtureCatalogForTest()
	profile := &runner.ResolvedRuntimeProfile{
		Platform:       "linux/amd64",
		BaseImageRef:   runner.FixtureBaseRefForTest,
		TemplateRev:    "v1",
		OpenCodeDigest: runner.DefaultOpenCodeDigest,
		Runtimes: map[string]string{
			"go": "1.27.2",
		},
	}

	expectedTag := expectedTagFor(t, catalog, profile)

	mockDocker := &mockDockerClientForBuilder{
		imageListFunc: func(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
			return []image.Summary{
				{RepoTags: []string{expectedTag}},
			}, nil
		},
	}

	builder := newTestBuilder(mockDocker, catalog)
	tag, err := builder.BuildOrGetImage(context.Background(), profile)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if tag != expectedTag {
		t.Errorf("expected tag %q, got %q", expectedTag, tag)
	}

	mockDocker.mu.Lock()
	defer mockDocker.mu.Unlock()
	if len(mockDocker.imageBuildCalls) != 0 {
		t.Errorf("expected 0 ImageBuild calls on cache hit, got %d", len(mockDocker.imageBuildCalls))
	}
}

func TestRuntimeImageBuilder_UncachedImageBuildsTarAndCallsImageBuild(t *testing.T) {
	catalog := runner.FixtureCatalogForTest()
	profile := &runner.ResolvedRuntimeProfile{
		Platform:       "linux/amd64",
		BaseImageRef:   runner.FixtureBaseRefForTest,
		TemplateRev:    "v1",
		OpenCodeDigest: runner.DefaultOpenCodeDigest,
		Runtimes: map[string]string{
			"go":   "1.27.2",
			"node": "24.21.0",
		},
	}

	expectedTag := expectedTagFor(t, catalog, profile)

	buildCalled := false
	mockDocker := &mockDockerClientForBuilder{
		imageListFunc: func(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
			return nil, nil // cache miss
		},
		imageBuildFunc: func(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
			buildCalled = true
			if len(options.Tags) == 0 || options.Tags[0] != expectedTag {
				t.Errorf("expected build tag %q, got %v", expectedTag, options.Tags)
			}
			if !options.Remove {
				t.Errorf("expected Remove: true")
			}
			return types.ImageBuildResponse{
				Body: streamBody(legacyOKStream("")),
			}, nil
		},
	}

	builder := newTestBuilder(mockDocker, catalog)
	tag, err := builder.BuildOrGetImage(context.Background(), profile)
	if err != nil {
		t.Fatalf("expected build success, got: %v", err)
	}

	if tag != expectedTag {
		t.Errorf("expected tag %q, got %q", expectedTag, tag)
	}
	if !buildCalled {
		t.Errorf("expected ImageBuild to be called")
	}

	mockDocker.mu.Lock()
	defer mockDocker.mu.Unlock()
	if len(mockDocker.buildTarPayloads) == 0 || len(mockDocker.buildTarPayloads[0]) == 0 {
		t.Errorf("expected non-empty tar payload sent to ImageBuild")
	}
}

func TestRuntimeImageBuilder_ConcurrentCallsCoalesceSingleBuild(t *testing.T) {
	catalog := runner.FixtureCatalogForTest()
	profile := &runner.ResolvedRuntimeProfile{
		Platform:       "linux/amd64",
		BaseImageRef:   runner.FixtureBaseRefForTest,
		TemplateRev:    "v1",
		OpenCodeDigest: runner.DefaultOpenCodeDigest,
		Runtimes: map[string]string{
			"rust": "1.99.0",
		},
	}

	expectedTag := expectedTagFor(t, catalog, profile)

	buildStarted := make(chan struct{})
	continueBuild := make(chan struct{})
	var buildCount int
	var buildMu sync.Mutex

	mockDocker := &mockDockerClientForBuilder{
		imageListFunc: func(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
			return nil, nil // miss
		},
		imageBuildFunc: func(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
			buildMu.Lock()
			buildCount++
			buildMu.Unlock()
			close(buildStarted)
			<-continueBuild
			return types.ImageBuildResponse{
				Body: streamBody(legacyOKStream("")),
			}, nil
		},
	}

	builder := newTestBuilder(mockDocker, catalog)

	var wg sync.WaitGroup
	errs := make([]error, 3)
	tags := make([]string, 3)

	// Caller 1 starts build
	wg.Add(1)
	go func() {
		defer wg.Done()
		tags[0], errs[0] = builder.BuildOrGetImage(context.Background(), profile)
	}()

	select {
	case <-buildStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("ImageBuild was never called")
	}

	// Callers 2 and 3 join concurrent in-flight build
	for i := 1; i < 3; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			tags[idx], errs[idx] = builder.BuildOrGetImage(context.Background(), profile)
		}()
	}

	// Give goroutines a moment to reach builder awaiting channel
	time.Sleep(50 * time.Millisecond)

	// Allow the single build to complete
	close(continueBuild)
	waitDone := make(chan struct{})
	go func() { wg.Wait(); close(waitDone) }()
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("callers did not return after the build completed")
	}

	for i := 0; i < 3; i++ {
		if errs[i] != nil {
			t.Errorf("caller %d got error: %v", i, errs[i])
		}
		if tags[i] != expectedTag {
			t.Errorf("caller %d got tag %q, expected %q", i, tags[i], expectedTag)
		}
	}

	buildMu.Lock()
	defer buildMu.Unlock()
	if buildCount != 1 {
		t.Errorf("expected exactly 1 ImageBuild call, got %d", buildCount)
	}
}

func TestRuntimeImageBuilder_DetachedContextLeavesBuildRunningForOtherCallers(t *testing.T) {
	catalog := runner.FixtureCatalogForTest()
	profile := &runner.ResolvedRuntimeProfile{
		Platform:       "linux/amd64",
		BaseImageRef:   runner.FixtureBaseRefForTest,
		TemplateRev:    "v1",
		OpenCodeDigest: runner.DefaultOpenCodeDigest,
		Runtimes: map[string]string{
			"python": "3.11.17",
		},
	}

	expectedTag := expectedTagFor(t, catalog, profile)

	buildStarted := make(chan struct{})
	continueBuild := make(chan struct{})

	mockDocker := &mockDockerClientForBuilder{
		imageListFunc: func(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
			return nil, nil // miss
		},
		imageBuildFunc: func(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
			close(buildStarted)
			select {
			case <-continueBuild:
				return types.ImageBuildResponse{
					Body: streamBody(legacyOKStream("")),
				}, nil
			case <-ctx.Done():
				return types.ImageBuildResponse{}, ctx.Err()
			}
		},
	}

	builder := newTestBuilder(mockDocker, catalog)

	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2 := context.Background()

	var wg sync.WaitGroup
	var tag2 string
	var err1, err2 error

	// Caller 1 starts build with cancellable ctx
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err1 = builder.BuildOrGetImage(ctx1, profile)
	}()

	<-buildStarted

	// Caller 2 joins build
	wg.Add(1)
	go func() {
		defer wg.Done()
		tag2, err2 = builder.BuildOrGetImage(ctx2, profile)
	}()

	time.Sleep(30 * time.Millisecond)

	// Cancel Caller 1's context. Build should NOT cancel because Caller 2 is still waiting.
	cancel1()

	time.Sleep(30 * time.Millisecond)

	// Now allow build to complete successfully
	close(continueBuild)
	wg.Wait()

	if err1 == nil {
		t.Errorf("expected caller 1 to receive context cancellation error, got nil")
	}
	if err2 != nil {
		t.Errorf("expected caller 2 to succeed, got error: %v", err2)
	}
	if tag2 != expectedTag {
		t.Errorf("expected caller 2 to get tag %q, got %q", expectedTag, tag2)
	}
}

func uncachedProfile() (runner.RuntimeCatalog, *runner.ResolvedRuntimeProfile) {
	return runner.FixtureCatalogForTest(), &runner.ResolvedRuntimeProfile{
		Platform:       "linux/amd64",
		BaseImageRef:   runner.FixtureBaseRefForTest,
		TemplateRev:    "v1",
		OpenCodeDigest: runner.DefaultOpenCodeDigest,
		Runtimes:       map[string]string{"go": "1.27.2"},
	}
}

func buildWith(t *testing.T, body string, inspect func(tag string) (types.ImageInspect, error)) (*mockDockerClientForBuilder, string, string, error) {
	t.Helper()
	catalog, profile := uncachedProfile()
	expectedTag := expectedTagFor(t, catalog, profile)
	m := &mockDockerClientForBuilder{
		imageBuildFunc: func(ctx context.Context, bc io.Reader, o types.ImageBuildOptions) (types.ImageBuildResponse, error) {
			return types.ImageBuildResponse{Body: streamBody(body)}, nil
		},
	}
	if inspect != nil {
		m.imageInspectFunc = func(ctx context.Context, id string) (types.ImageInspect, []byte, error) {
			ii, err := inspect(expectedTag)
			return ii, nil, err
		}
	}
	tag, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
	return m, expectedTag, tag, err
}

func requireBuildFailed(t *testing.T, tag string, err error) {
	t.Helper()
	if tag != "" {
		t.Errorf("a failed build must not return a tag, got %q", tag)
	}
	if !errors.Is(err, runner.ErrImageBuildFailed) {
		t.Fatalf("expected ErrImageBuildFailed, got %v", err)
	}
}

func TestBuilder_BuildStreamErrorReturnsNoTag(t *testing.T) {
	m, _, tag, err := buildWith(t, legacyOKStream("")+`{"errorDetail":{"message":"boom"},"error":"boom"}`+"\n", nil)
	requireBuildFailed(t, tag, err)
	if len(m.imageInspectCalls) != 0 {
		t.Errorf("must not inspect after a stream error, got %d calls", len(m.imageInspectCalls))
	}
}

func TestBuilder_PostBuildInspectLabelMismatchFails(t *testing.T) {
	for name, mut := range map[string]func(*types.ImageInspect){
		"wrong identity": func(i *types.ImageInspect) { i.Config.Labels["io.atm.runtime.identity"] = strings.Repeat("0", 64) },
		"wrong scheme":   func(i *types.ImageInspect) { i.Config.Labels["io.atm.runtime.identity-scheme"] = "v1" },
		"no labels":      func(i *types.ImageInspect) { i.Config.Labels = nil },
		"nil config":     func(i *types.ImageInspect) { i.Config = nil },
	} {
		t.Run(name, func(t *testing.T) {
			_, _, tag, err := buildWith(t, legacyOKStream(""), func(tg string) (types.ImageInspect, error) {
				ii := validInspect(tg, "")
				mut(&ii)
				return ii, nil
			})
			requireBuildFailed(t, tag, err)
		})
	}
}

func TestBuilder_PostBuildInspectWrongArchFails(t *testing.T) {
	for name, mut := range map[string]func(*types.ImageInspect){
		"arch": func(i *types.ImageInspect) { i.Architecture = "arm64" },
		"os":   func(i *types.ImageInspect) { i.Os = "windows" },
	} {
		t.Run(name, func(t *testing.T) {
			_, _, tag, err := buildWith(t, legacyOKStream(""), func(tg string) (types.ImageInspect, error) {
				ii := validInspect(tg, "")
				mut(&ii)
				return ii, nil
			})
			requireBuildFailed(t, tag, err)
		})
	}
}

func TestBuilder_PostBuildAuxIDMismatchFails(t *testing.T) {
	other := "sha256:" + strings.Repeat("ef", 32)
	_, _, tag, err := buildWith(t, legacyOKStream(other), func(tg string) (types.ImageInspect, error) {
		return validInspect(tg, ""), nil
	})
	requireBuildFailed(t, tag, err)
}

func TestBuilder_PostBuildMatchingAuxIDSucceeds(t *testing.T) {
	_, want, tag, err := buildWith(t, legacyOKStream(fakeImageID), func(tg string) (types.ImageInspect, error) {
		return validInspect(tg, ""), nil
	})
	if err != nil || tag != want {
		t.Fatalf("tag=%q err=%v, want %q", tag, err, want)
	}
}

func TestBuilder_PostBuildMissingAuxInspectSucceeds(t *testing.T) {
	trace := `{"id":"moby.buildkit.trace","aux":{"vertexes":[]}}` + "\n"
	m, want, tag, err := buildWith(t, trace, func(tg string) (types.ImageInspect, error) {
		return validInspect(tg, ""), nil
	})
	if err != nil || tag != want {
		t.Fatalf("tag=%q err=%v, want %q", tag, err, want)
	}
	if len(m.imageInspectCalls) != 1 || m.imageInspectCalls[0] != want {
		t.Errorf("expected one inspect of %q, got %v", want, m.imageInspectCalls)
	}
}

func TestBuilder_PostBuildMissingAuxInspectFails(t *testing.T) {
	trace := `{"id":"moby.buildkit.trace","aux":{"vertexes":[]}}` + "\n"
	_, _, tag, err := buildWith(t, trace, func(tg string) (types.ImageInspect, error) {
		ii := validInspect(tg, "")
		ii.Architecture = "arm64"
		return ii, nil
	})
	requireBuildFailed(t, tag, err)
}

func TestBuilder_PostBuildInspectErrorFails(t *testing.T) {
	_, _, tag, err := buildWith(t, legacyOKStream(""), func(string) (types.ImageInspect, error) {
		return types.ImageInspect{}, errors.New("no such image")
	})
	requireBuildFailed(t, tag, err)
}

func TestBuilder_BuildOptionsPinned(t *testing.T) {
	m, want, _, err := buildWith(t, legacyOKStream(""), nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(m.imageBuildCalls) != 1 {
		t.Fatalf("expected 1 build call, got %d", len(m.imageBuildCalls))
	}
	o := m.imageBuildCalls[0]
	if o.Platform != "linux/amd64" {
		t.Errorf("Platform = %q", o.Platform)
	}
	if !o.Remove || !o.ForceRemove {
		t.Errorf("Remove=%v ForceRemove=%v, both must be true", o.Remove, o.ForceRemove)
	}
	if o.Version != types.BuilderBuildKit {
		t.Errorf("Version = %q, want BuildKit", o.Version)
	}
	if len(o.Tags) != 1 || o.Tags[0] != want {
		t.Errorf("Tags = %v, want [%s]", o.Tags, want)
	}
	if o.Labels["io.atm.runtime.identity"] != strings.TrimPrefix(want, "atm-runtime:") ||
		o.Labels["io.atm.runtime.identity-scheme"] != "v3" || len(o.Labels) != 2 {
		t.Errorf("Labels = %v", o.Labels)
	}
}

// ---- Task 4b: in-flight lifecycle, bounded timeout, cancellation ----

// fakeStream is a build-response body that blocks in Read until released. If honorClose is
// true, Close also unblocks Read (with an error), like a closed network body. All
// synchronisation is by channel.
type fakeStream struct {
	honorClose bool
	release    chan struct{}
	closed     chan struct{}
	readDone   chan struct{}

	relOnce, closeOnce, doneOnce sync.Once
	data                         io.Reader
}

func newFakeStream(honorClose bool) *fakeStream {
	return &fakeStream{
		honorClose: honorClose,
		release:    make(chan struct{}),
		closed:     make(chan struct{}),
		readDone:   make(chan struct{}),
		data:       strings.NewReader(legacyOKStream("")),
	}
}

func (f *fakeStream) Read(p []byte) (int, error) {
	if f.honorClose {
		select {
		case <-f.release:
		case <-f.closed:
			f.doneOnce.Do(func() { close(f.readDone) })
			return 0, io.ErrClosedPipe
		}
	} else {
		<-f.release
	}
	n, err := f.data.Read(p)
	if err != nil {
		f.doneOnce.Do(func() { close(f.readDone) })
	}
	return n, err
}

func (f *fakeStream) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

func (f *fakeStream) Release() { f.relOnce.Do(func() { close(f.release) }) }

// streamHarness hands out one fakeStream per ImageBuild call.
type streamHarness struct {
	t          *testing.T
	mock       *mockDockerClientForBuilder
	builder    runner.RuntimeImageBuilder
	catalog    runner.RuntimeCatalog
	profile    *runner.ResolvedRuntimeProfile
	tag        string
	honorClose bool

	mu      sync.Mutex
	streams []*fakeStream
	started chan *fakeStream
}

func newStreamHarness(t *testing.T, honorClose bool, timeout time.Duration) *streamHarness {
	t.Helper()
	catalog, profile := uncachedProfile()
	h := &streamHarness{t: t, catalog: catalog, profile: profile, honorClose: honorClose,
		started: make(chan *fakeStream, 1024), tag: expectedTagFor(t, catalog, profile)}
	h.mock = &mockDockerClientForBuilder{
		imageBuildFunc: func(ctx context.Context, bc io.Reader, o types.ImageBuildOptions) (types.ImageBuildResponse, error) {
			fs := newFakeStream(h.honorClose)
			h.mu.Lock()
			h.streams = append(h.streams, fs)
			h.mu.Unlock()
			select { // never block the builder if a test does not drain started
			case h.started <- fs:
			default:
			}
			return types.ImageBuildResponse{Body: fs}, nil
		},
	}
	if timeout > 0 {
		h.builder = runner.NewRuntimeImageBuilderWithTimeoutForTest(h.mock, catalog, runner.EmbeddedTemplateForTest(), runner.ExpectedTemplateSHA256, timeout)
	} else {
		h.builder = newTestBuilder(h.mock, catalog)
	}
	return h
}

type buildOutcome struct {
	tag string
	err error
}

func (h *streamHarness) call(ctx context.Context) <-chan buildOutcome {
	ch := make(chan buildOutcome, 1)
	go func() {
		tag, err := h.builder.BuildOrGetImage(ctx, h.profile)
		ch <- buildOutcome{tag, err}
	}()
	return ch
}

func (h *streamHarness) nextStream() *fakeStream {
	h.t.Helper()
	select {
	case fs := <-h.started:
		return fs
	case <-time.After(10 * time.Second):
		h.t.Fatal("timed out waiting for ImageBuild to start")
		return nil
	}
}

func (h *streamHarness) buildCalls() int {
	h.mock.mu.Lock()
	defer h.mock.mu.Unlock()
	return len(h.mock.imageBuildCalls)
}

// waitUntil spins (yielding) until cond holds; the deadline only bounds a failing run.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		runtime.Gosched()
	}
}

func recv(t *testing.T, ch <-chan buildOutcome) buildOutcome {
	t.Helper()
	select {
	case o := <-ch:
		return o
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for BuildOrGetImage to return")
		return buildOutcome{}
	}
}

func requireClosed(t *testing.T, what string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: channel was not closed", what)
	}
}

func TestBuilder_InFlight_DeterministicBlockedFakeStream(t *testing.T) {
	h := newStreamHarness(t, true, 0)
	c1 := h.call(context.Background())
	fs := h.nextStream()
	c2 := h.call(context.Background())
	waitUntil(t, "two waiters", func() bool { return runner.InFlightWaitersForTest(h.builder) == 2 })

	if n := runner.InFlightCountForTest(h.builder); n != 1 {
		t.Errorf("expected 1 in-flight build, got %d", n)
	}
	if n := h.buildCalls(); n != 1 {
		t.Errorf("expected exactly one ImageBuild call, got %d", n)
	}
	fs.Release()
	for i, c := range []<-chan buildOutcome{c1, c2} {
		o := recv(t, c)
		if o.err != nil || o.tag != h.tag {
			t.Errorf("caller %d: tag=%q err=%v", i+1, o.tag, o.err)
		}
	}
	if n := runner.InFlightCountForTest(h.builder); n != 0 {
		t.Errorf("in-flight map must be empty after completion, got %d", n)
	}
}

func TestBuilder_InFlight_CancelLastWaiterCleansMap(t *testing.T) {
	h := newStreamHarness(t, true, 0)
	ctx, cancel := context.WithCancel(context.Background())
	c := h.call(ctx)
	fs := h.nextStream()
	waitUntil(t, "one waiter", func() bool { return runner.InFlightWaitersForTest(h.builder) == 1 })

	cancel()
	o := recv(t, c)
	if !errors.Is(o.err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", o.err)
	}
	if n := runner.InFlightCountForTest(h.builder); n != 0 {
		t.Errorf("abandoned flight must be removed from the map, got %d entries", n)
	}
	requireClosed(t, "build body after last waiter left", fs.closed)
}

func TestBuilder_InFlight_NewCallerStartsFreshAfterCancel(t *testing.T) {
	h := newStreamHarness(t, false, 0) // old stream stays stuck: its goroutine is still alive
	ctx, cancel := context.WithCancel(context.Background())
	c1 := h.call(ctx)
	old := h.nextStream()
	waitUntil(t, "one waiter", func() bool { return runner.InFlightWaitersForTest(h.builder) == 1 })
	cancel()
	if o := recv(t, c1); !errors.Is(o.err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", o.err)
	}

	c2 := h.call(context.Background())
	fresh := h.nextStream()
	if fresh == old {
		t.Fatal("new caller must start a fresh build")
	}
	fresh.Release()
	o := recv(t, c2)
	if o.err != nil || o.tag != h.tag {
		t.Fatalf("new caller must not inherit the cancelled flight: tag=%q err=%v", o.tag, o.err)
	}
	if n := h.buildCalls(); n != 2 {
		t.Errorf("expected 2 ImageBuild calls, got %d", n)
	}
	old.Release()
}

func TestBuilder_InFlight_OldCompletionDoesNotRemoveNewFlight(t *testing.T) {
	h := newStreamHarness(t, false, 0) // old goroutine only finishes when we release it
	ctx, cancel := context.WithCancel(context.Background())
	c1 := h.call(ctx)
	old := h.nextStream()
	waitUntil(t, "one waiter", func() bool { return runner.InFlightWaitersForTest(h.builder) == 1 })
	cancel()
	recv(t, c1)

	c2 := h.call(context.Background())
	fresh := h.nextStream()
	waitUntil(t, "fresh flight registered", func() bool { return runner.InFlightWaitersForTest(h.builder) == 1 })

	// The old, cancelled goroutine now completes while the replacement flight is live.
	old.Release()
	requireClosed(t, "old stream read finished", old.readDone)
	// readDone is signalled before the old goroutine's completion cleanup; the old flight's
	// doneCh is not observable here, so let the scheduler run it before asserting.
	for i := 0; i < 1000; i++ {
		runtime.Gosched()
	}
	if n := runner.InFlightCountForTest(h.builder); n != 1 {
		t.Fatalf("old completion removed the replacement flight: %d entries", n)
	}
	// A third caller must still coalesce onto the replacement flight.
	c3 := h.call(context.Background())
	waitUntil(t, "third caller joined", func() bool { return runner.InFlightWaitersForTest(h.builder) == 2 })
	if n := h.buildCalls(); n != 2 {
		t.Errorf("expected 2 ImageBuild calls, got %d", n)
	}
	fresh.Release()
	for i, c := range []<-chan buildOutcome{c2, c3} {
		if o := recv(t, c); o.err != nil || o.tag != h.tag {
			t.Errorf("caller %d: tag=%q err=%v", i+2, o.tag, o.err)
		}
	}
}

func TestBuilder_InFlight_FirstWaiterCancelOtherContinues(t *testing.T) {
	h := newStreamHarness(t, true, 0)
	ctx1, cancel1 := context.WithCancel(context.Background())
	c1 := h.call(ctx1)
	fs := h.nextStream()
	c2 := h.call(context.Background())
	waitUntil(t, "two waiters", func() bool { return runner.InFlightWaitersForTest(h.builder) == 2 })

	cancel1()
	if o := recv(t, c1); !errors.Is(o.err, context.Canceled) {
		t.Fatalf("expected context.Canceled for first waiter, got %v", o.err)
	}
	if n := runner.InFlightWaitersForTest(h.builder); n != 1 {
		t.Errorf("expected 1 remaining waiter, got %d", n)
	}
	select {
	case <-fs.closed:
		t.Fatal("build was cancelled while another waiter remained")
	default:
	}
	if n := runner.InFlightCountForTest(h.builder); n != 1 {
		t.Errorf("flight must stay registered, got %d entries", n)
	}
	fs.Release()
	if o := recv(t, c2); o.err != nil || o.tag != h.tag {
		t.Fatalf("remaining waiter: tag=%q err=%v", o.tag, o.err)
	}
}

func TestBuilder_InFlight_ConfigurableTimeoutTriggers(t *testing.T) {
	h := newStreamHarness(t, true, 20*time.Millisecond)
	callerCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := h.call(callerCtx)
	fs := h.nextStream()

	o := recv(t, c)
	if callerCtx.Err() != nil {
		t.Fatalf("caller context expired before the build timeout fired (err=%v)", o.err)
	}
	if !errors.Is(o.err, context.DeadlineExceeded) {
		t.Fatalf("expected build timeout (DeadlineExceeded), got %v", o.err)
	}
	if o.tag != "" {
		t.Errorf("a timed-out build must not return a tag, got %q", o.tag)
	}
	requireClosed(t, "body closed on timeout", fs.closed)
	if n := runner.InFlightCountForTest(h.builder); n != 0 {
		t.Errorf("timed-out flight must be removed, got %d entries", n)
	}
}

func TestBuilder_InFlight_CancelClosesBody(t *testing.T) {
	h := newStreamHarness(t, true, 0)
	ctx, cancel := context.WithCancel(context.Background())
	c := h.call(ctx)
	fs := h.nextStream()
	waitUntil(t, "one waiter", func() bool { return runner.InFlightWaitersForTest(h.builder) == 1 })
	cancel()
	recv(t, c)
	// The stream reader must be aborted (Read returned) and the body closed.
	requireClosed(t, "body closed", fs.closed)
	requireClosed(t, "stream read aborted", fs.readDone)
}

func TestBuilder_InFlight_NoDeadlockOnConcurrentCancelAndFinish(t *testing.T) {
	h := newStreamHarness(t, true, 0)
	for round := 0; round < 20; round++ {
		const callers = 4
		var wg sync.WaitGroup
		cancels := make([]context.CancelFunc, callers)
		outs := make([]buildOutcome, callers)
		stop := make(chan struct{})
		relDone := make(chan struct{})
		go func() { // releaser: finish every stream created so far, repeatedly
			defer close(relDone)
			for {
				h.mu.Lock()
				ss := append([]*fakeStream(nil), h.streams...)
				h.mu.Unlock()
				for _, s := range ss {
					s.Release()
				}
				select {
				case <-stop:
					return
				default:
					runtime.Gosched()
				}
			}
		}()
		for i := 0; i < callers; i++ {
			ctx, cancel := context.WithCancel(context.Background())
			cancels[i] = cancel
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				tag, err := h.builder.BuildOrGetImage(ctx, h.profile)
				outs[i] = buildOutcome{tag, err}
			}(i)
		}
		for i := 0; i < callers-1; i++ { // keep the last caller alive
			go cancels[i]()
		}
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatal("deadlock: callers did not return")
		}
		close(stop)
		<-relDone
		cancels[callers-1]()
		for i, o := range outs {
			switch {
			case o.err == nil && o.tag == h.tag:
			case errors.Is(o.err, context.Canceled) && o.tag == "":
			default:
				t.Fatalf("round %d caller %d: tag=%q err=%v", round, i, o.tag, o.err)
			}
		}
		if outs[callers-1].err != nil {
			t.Fatalf("round %d: the never-cancelled caller failed: %v", round, outs[callers-1].err)
		}
	}
}

// ---- Task 5: preflight ordering and cache-hit verification ----

// strictDockerClient fails the test on, and counts, every image call the builder can make.
// The embedded nil interface makes any other method panic, which also flags an unexpected call.
type strictDockerClient struct {
	sandbox.DockerClient
	t     *testing.T
	calls atomic.Int64
}

func (s *strictDockerClient) fail(method string) {
	s.calls.Add(1)
	s.t.Errorf("unexpected Docker call: %s", method)
}

func (s *strictDockerClient) ImageList(ctx context.Context, o image.ListOptions) ([]image.Summary, error) {
	s.fail("ImageList")
	return nil, errors.New("unexpected docker call")
}

func (s *strictDockerClient) ImageBuild(ctx context.Context, r io.Reader, o types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	s.fail("ImageBuild")
	return types.ImageBuildResponse{}, errors.New("unexpected docker call")
}

func (s *strictDockerClient) ImageInspectWithRaw(ctx context.Context, id string) (types.ImageInspect, []byte, error) {
	s.fail("ImageInspectWithRaw")
	return types.ImageInspect{}, nil, errors.New("unexpected docker call")
}

func requireZeroCalls(t *testing.T, s *strictDockerClient) {
	t.Helper()
	if n := s.calls.Load(); n != 0 {
		t.Errorf("expected zero Docker calls, got %d", n)
	}
}

func TestBuilder_PreflightFailureMakesZeroDockerCalls(t *testing.T) {
	pending := runner.PendingFixtureCatalogForTest() // every checksum is PendingSHA256

	t.Run("pending catalog, pinned base: unverified checksum", func(t *testing.T) {
		s := &strictDockerClient{t: t}
		b := runner.NewRuntimeImageBuilderForTest(s, pending, runner.EmbeddedTemplateForTest(), runner.ExpectedTemplateSHA256)
		_, profile := uncachedProfile()
		tag, err := b.BuildOrGetImage(context.Background(), profile)
		if !errors.Is(err, runner.ErrCatalogEntryUnverified) || tag != "" {
			t.Fatalf("tag=%q err=%v, want ErrCatalogEntryUnverified", tag, err)
		}
		requireZeroCalls(t, s)
	})

	t.Run("pending catalog, pending base ref: invalid profile", func(t *testing.T) {
		s := &strictDockerClient{t: t}
		b := runner.NewRuntimeImageBuilderForTest(s, pending, runner.EmbeddedTemplateForTest(), runner.ExpectedTemplateSHA256)
		_, profile := uncachedProfile()
		profile.BaseImageRef = runner.PendingBaseRefForTest
		tag, err := b.BuildOrGetImage(context.Background(), profile)
		if !errors.Is(err, runner.ErrInvalidRuntimeProfile) || tag != "" {
			t.Fatalf("tag=%q err=%v, want ErrInvalidRuntimeProfile", tag, err)
		}
		requireZeroCalls(t, s)
	})
}

func TestBuilder_InvalidProfileMakesZeroDockerCalls(t *testing.T) {
	for name, mut := range map[string]func(*runner.ResolvedRuntimeProfile){
		"unpinned base":        func(p *runner.ResolvedRuntimeProfile) { p.BaseImageRef = "alpine:3.24" },
		"empty opencode":       func(p *runner.ResolvedRuntimeProfile) { p.OpenCodeDigest = "" },
		"malformed opencode":   func(p *runner.ResolvedRuntimeProfile) { p.OpenCodeDigest = "sha256:zz" },
		"wrong platform":       func(p *runner.ResolvedRuntimeProfile) { p.Platform = "linux/arm64" },
		"uncataloged runtime":  func(p *runner.ResolvedRuntimeProfile) { p.Runtimes = map[string]string{"go": "9.9.9"} },
		"unknown runtime name": func(p *runner.ResolvedRuntimeProfile) { p.Runtimes = map[string]string{"cobol": "1"} },
	} {
		t.Run(name, func(t *testing.T) {
			s := &strictDockerClient{t: t}
			catalog, profile := uncachedProfile()
			mut(profile)
			b := runner.NewRuntimeImageBuilderForTest(s, catalog, runner.EmbeddedTemplateForTest(), runner.ExpectedTemplateSHA256)
			tag, err := b.BuildOrGetImage(context.Background(), profile)
			if !errors.Is(err, runner.ErrInvalidRuntimeProfile) || tag != "" {
				t.Fatalf("tag=%q err=%v, want ErrInvalidRuntimeProfile", tag, err)
			}
			requireZeroCalls(t, s)
		})
	}
}

func TestBuilder_NilProfileMakesZeroDockerCalls(t *testing.T) {
	s := &strictDockerClient{t: t}
	b := runner.NewRuntimeImageBuilderForTest(s, runner.FixtureCatalogForTest(), runner.EmbeddedTemplateForTest(), runner.ExpectedTemplateSHA256)
	if tag, err := b.BuildOrGetImage(context.Background(), nil); err == nil || tag != "" {
		t.Fatalf("tag=%q err=%v, want an error", tag, err)
	}
	requireZeroCalls(t, s)
}

func TestBuilder_TemplateIntegrityFailureMakesZeroDockerCalls(t *testing.T) {
	s := &strictDockerClient{t: t}
	catalog, profile := uncachedProfile()
	tampered := append(runner.EmbeddedTemplateForTest(), '#')
	b := runner.NewRuntimeImageBuilderForTest(s, catalog, tampered, runner.ExpectedTemplateSHA256)
	tag, err := b.BuildOrGetImage(context.Background(), profile)
	if !errors.Is(err, runner.ErrTemplateIntegrity) || tag != "" {
		t.Fatalf("tag=%q err=%v, want ErrTemplateIntegrity", tag, err)
	}
	requireZeroCalls(t, s)
}

func TestBuilder_CacheHitRequiresMatchingLabel(t *testing.T) {
	for name, mut := range map[string]func(*types.ImageInspect){
		"wrong identity": func(i *types.ImageInspect) { i.Config.Labels["io.atm.runtime.identity"] = strings.Repeat("0", 64) },
		"missing identity": func(i *types.ImageInspect) {
			delete(i.Config.Labels, "io.atm.runtime.identity")
		},
		"wrong scheme":   func(i *types.ImageInspect) { i.Config.Labels["io.atm.runtime.identity-scheme"] = "v1" },
		"missing scheme": func(i *types.ImageInspect) { delete(i.Config.Labels, "io.atm.runtime.identity-scheme") },
		"full scheme const": func(i *types.ImageInspect) {
			i.Config.Labels["io.atm.runtime.identity-scheme"] = runner.ImageIdentityScheme
		},
		"no labels":  func(i *types.ImageInspect) { i.Config.Labels = nil },
		"nil config": func(i *types.ImageInspect) { i.Config = nil },
		"wrong arch": func(i *types.ImageInspect) { i.Architecture = "arm64" },
		"wrong os":   func(i *types.ImageInspect) { i.Os = "windows" },
	} {
		t.Run(name, func(t *testing.T) {
			catalog, profile := uncachedProfile()
			tag := expectedTagFor(t, catalog, profile)
			m := &mockDockerClientForBuilder{
				imageListFunc: func(ctx context.Context, o image.ListOptions) ([]image.Summary, error) {
					return []image.Summary{{RepoTags: []string{tag}}}, nil
				},
				imageInspectFunc: func(ctx context.Context, id string) (types.ImageInspect, []byte, error) {
					ii := validInspect(tag, "")
					mut(&ii)
					return ii, nil, nil
				},
			}
			got, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
			if got != "" || !errors.Is(err, runner.ErrImageIdentityMismatch) {
				t.Fatalf("tag=%q err=%v, want ErrImageIdentityMismatch", got, err)
			}
			if n := len(m.imageBuildCalls); n != 0 {
				t.Errorf("ImageBuild must never be called over an existing tag, got %d calls", n)
			}
		})
	}
}

func TestBuilder_CacheHitInspectErrorFailsClosed(t *testing.T) {
	catalog, profile := uncachedProfile()
	tag := expectedTagFor(t, catalog, profile)
	m := &mockDockerClientForBuilder{
		imageListFunc: func(ctx context.Context, o image.ListOptions) ([]image.Summary, error) {
			return []image.Summary{{RepoTags: []string{tag}}}, nil
		},
		imageInspectFunc: func(ctx context.Context, id string) (types.ImageInspect, []byte, error) {
			return types.ImageInspect{}, nil, errors.New("daemon says no")
		},
	}
	got, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
	if got != "" || !errors.Is(err, runner.ErrImageIdentityMismatch) {
		t.Fatalf("tag=%q err=%v, want ErrImageIdentityMismatch", got, err)
	}
	if strings.Contains(err.Error(), "daemon says no") {
		t.Errorf("engine detail must not leak into the returned error: %v", err)
	}
	if len(m.imageBuildCalls) != 0 {
		t.Errorf("ImageBuild must never be called, got %d", len(m.imageBuildCalls))
	}
}

func TestBuilder_CacheHitWithMatchingLabelReturnsTag(t *testing.T) {
	catalog, profile := uncachedProfile()
	tag := expectedTagFor(t, catalog, profile)
	m := &mockDockerClientForBuilder{
		imageListFunc: func(ctx context.Context, o image.ListOptions) ([]image.Summary, error) {
			return []image.Summary{{RepoTags: []string{"other:tag", tag}}}, nil
		},
	}
	got, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
	if err != nil || got != tag {
		t.Fatalf("tag=%q err=%v, want %q", got, err, tag)
	}
	if len(m.imageBuildCalls) != 0 {
		t.Errorf("ImageBuild must not be called on a verified cache hit, got %d", len(m.imageBuildCalls))
	}
	if len(m.imageInspectCalls) != 1 || m.imageInspectCalls[0] != tag {
		t.Errorf("expected exactly one inspect of %q, got %v", tag, m.imageInspectCalls)
	}
	if len(m.imageListCalls) != 1 {
		t.Errorf("expected one ImageList call, got %d", len(m.imageListCalls))
	}
}

func TestBuilder_ImageListErrorReturnsError(t *testing.T) {
	catalog, profile := uncachedProfile()
	m := &mockDockerClientForBuilder{
		imageListFunc: func(ctx context.Context, o image.ListOptions) ([]image.Summary, error) {
			return nil, errors.New("list boom")
		},
	}
	got, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
	if got != "" || err == nil {
		t.Fatalf("tag=%q err=%v, want an error and no tag", got, err)
	}
	if len(m.imageBuildCalls) != 0 {
		t.Errorf("a failed list must not trigger a build, got %d", len(m.imageBuildCalls))
	}
}

// captureBuildLog redirects the build log sink for one test and returns the collected lines.
func captureBuildLog(t *testing.T) func() string {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	restore := runner.SetBuildLogfForTest(func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	})
	t.Cleanup(restore)
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(lines, "\n")
	}
}

func TestBuilder_ImageListErrorDoesNotLeakEngineDetail(t *testing.T) {
	logged := captureBuildLog(t)
	catalog, profile := uncachedProfile()
	m := &mockDockerClientForBuilder{
		imageListFunc: func(ctx context.Context, o image.ListOptions) ([]image.Summary, error) {
			return nil, errors.New("SECRET-ENGINE-DETAIL")
		},
	}
	got, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
	requireBuildFailed(t, got, err)
	if strings.Contains(err.Error(), "SECRET-ENGINE-DETAIL") {
		t.Errorf("engine detail must not leak into the returned error: %v", err)
	}
	if !strings.Contains(logged(), "SECRET-ENGINE-DETAIL") {
		t.Errorf("engine detail should be logged: %q", logged())
	}
}

func TestBuilder_ImageBuildErrorDoesNotLeakEngineDetail(t *testing.T) {
	logged := captureBuildLog(t)
	catalog, profile := uncachedProfile()
	m := &mockDockerClientForBuilder{
		imageBuildFunc: func(ctx context.Context, bc io.Reader, o types.ImageBuildOptions) (types.ImageBuildResponse, error) {
			return types.ImageBuildResponse{}, errors.New("SECRET-ENGINE-DETAIL")
		},
	}
	got, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
	requireBuildFailed(t, got, err)
	if strings.Contains(err.Error(), "SECRET-ENGINE-DETAIL") {
		t.Errorf("engine detail must not leak into the returned error: %v", err)
	}
	if !strings.Contains(logged(), "SECRET-ENGINE-DETAIL") {
		t.Errorf("engine detail should be logged: %q", logged())
	}
	if len(m.imageInspectCalls) != 0 {
		t.Errorf("must not inspect after a failed ImageBuild call, got %d", len(m.imageInspectCalls))
	}
}

func TestBuilder_NonExactRepoTagIsNotACacheHit(t *testing.T) {
	catalog, profile := uncachedProfile()
	tag := expectedTagFor(t, catalog, profile)
	for name, repoTag := range map[string]string{
		"tag with suffix": tag + "-x",
		"other repo":      "other:" + strings.TrimPrefix(tag, "atm-runtime:"),
		"tag prefix":      tag[:len(tag)-1],
	} {
		t.Run(name, func(t *testing.T) {
			m := &mockDockerClientForBuilder{
				imageListFunc: func(ctx context.Context, o image.ListOptions) ([]image.Summary, error) {
					return []image.Summary{{RepoTags: []string{repoTag}}}, nil
				},
			}
			got, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile)
			if err != nil || got != tag {
				t.Fatalf("tag=%q err=%v, want a fresh build of %q", got, err, tag)
			}
			if n := len(m.imageBuildCalls); n != 1 {
				t.Errorf("a non-exact RepoTag must not be a cache hit: want 1 ImageBuild call, got %d", n)
			}
		})
	}
}

// within fails the test if fn does not return in time (a held b.mu would hang it).
func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not complete: b.mu is probably held across a Docker call", what)
	}
}

func TestBuilder_ImageListDoesNotHoldBuilderMutex(t *testing.T) {
	catalog, profile := uncachedProfile()
	listStarted := make(chan struct{}, 4)
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	m := &mockDockerClientForBuilder{
		imageListFunc: func(ctx context.Context, o image.ListOptions) ([]image.Summary, error) {
			listStarted <- struct{}{}
			select {
			case <-gate:
			case <-ctx.Done():
			}
			return nil, ctx.Err()
		},
	}
	b := newTestBuilder(m, catalog)

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	resA := make(chan buildOutcome, 1)
	go func() { tag, err := b.BuildOrGetImage(ctxA, profile); resA <- buildOutcome{tag, err} }()
	select {
	case <-listStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("ImageList never started")
	}

	// Observing the builder state needs b.mu; it must not block behind the slow list.
	within(t, "in-flight observation", func() { _ = runner.InFlightCountForTest(b) })

	// A second caller can join and cancel promptly while the list is still blocked.
	ctxB, cancelB := context.WithCancel(context.Background())
	resB := make(chan buildOutcome, 1)
	go func() { tag, err := b.BuildOrGetImage(ctxB, profile); resB <- buildOutcome{tag, err} }()
	within(t, "second caller join", func() {
		for runner.InFlightWaitersForTest(b) != 2 {
			runtime.Gosched()
		}
	})
	cancelB()
	select {
	case o := <-resB:
		if !errors.Is(o.err, context.Canceled) {
			t.Errorf("second caller: want context.Canceled, got %v", o.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second caller's cancel did not return promptly while ImageList was blocked")
	}

	// The last waiter leaving aborts the blocked list through the flight context.
	cancelA()
	select {
	case o := <-resA:
		if !errors.Is(o.err, context.Canceled) {
			t.Errorf("first caller: want context.Canceled, got %v", o.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first caller did not return after cancel")
	}
	release()
	if len(m.imageBuildCalls) != 0 {
		t.Errorf("no build expected, got %d", len(m.imageBuildCalls))
	}
}
