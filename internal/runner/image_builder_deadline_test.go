package runner_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"

	"agent-task-manager/internal/runner"
)

// Every Docker step of BuildOrGetImage is blocked in turn, with a caller context that never
// expires, so only the builder's own timeout can end the call. Each step is blocked two ways:
//   - cooperative: the call returns ctx.Err() on cancellation and a body's Read unblocks on
//     Close, which is how the real Docker client behaves;
//   - stuck: the call ignores ctx and a body's Read ignores Close until the test releases it.
//
// In both cases the call must return within deadlineBound of the builder timeout firing.
const (
	deadlineBuildTimeout = 200 * time.Millisecond
	deadlineBound        = 2 * time.Second
)

// stepBlocker blocks one Docker step. reached is closed when the step is entered; aborted
// when a cooperative step was unblocked by ctx or Close; release unblocks a stuck step
// (called from t.Cleanup so no goroutine outlives the test).
type stepBlocker struct {
	cooperative bool
	reached     chan struct{}
	aborted     chan struct{}
	release     chan struct{}
	reachOnce   sync.Once
	abortOnce   sync.Once
	relOnce     sync.Once
}

func newStepBlocker(t *testing.T, cooperative bool) *stepBlocker {
	s := &stepBlocker{cooperative: cooperative, reached: make(chan struct{}), aborted: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() { s.relOnce.Do(func() { close(s.release) }) })
	return s
}

func (s *stepBlocker) enter() { s.reachOnce.Do(func() { close(s.reached) }) }

func (s *stepBlocker) abort() { s.abortOnce.Do(func() { close(s.aborted) }) }

// wait blocks like a Docker API call: cooperative returns ctx.Err(), stuck ignores ctx.
func (s *stepBlocker) wait(ctx context.Context) error {
	s.enter()
	if s.cooperative {
		<-ctx.Done()
		s.abort()
		return ctx.Err()
	}
	<-s.release
	return errors.New("released")
}

// body returns a response body whose Read blocks: cooperative unblocks on Close, stuck only
// on release (it ignores both Close and ctx).
func (s *stepBlocker) body() io.ReadCloser {
	return &blockingBody{s: s, closed: make(chan struct{})}
}

type blockingBody struct {
	s         *stepBlocker
	closed    chan struct{}
	closeOnce sync.Once
}

func (b *blockingBody) Read(p []byte) (int, error) {
	b.s.enter()
	if b.s.cooperative {
		select {
		case <-b.closed:
			b.s.abort()
			return 0, io.ErrClosedPipe
		case <-b.s.release:
		}
	} else {
		<-b.s.release
	}
	return 0, errors.New("released")
}

func (b *blockingBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

type deadlineStep struct {
	name    string
	cached  bool // ImageList reports the tag, so the cache-hit verify path runs
	install func(m *mockDockerClientForBuilder, s *stepBlocker)
}

func deadlineSteps() []deadlineStep {
	return []deadlineStep{
		{name: "ImageList", install: func(m *mockDockerClientForBuilder, s *stepBlocker) {
			m.imageListFunc = func(ctx context.Context, o image.ListOptions) ([]image.Summary, error) {
				return nil, s.wait(ctx)
			}
		}},
		{name: "CacheHitInspect", cached: true, install: func(m *mockDockerClientForBuilder, s *stepBlocker) {
			m.imageInspectFunc = func(ctx context.Context, id string) (types.ImageInspect, []byte, error) {
				return types.ImageInspect{}, nil, s.wait(ctx)
			}
		}},
		{name: "ImagePullCall", install: func(m *mockDockerClientForBuilder, s *stepBlocker) {
			m.imagePullFunc = func(ctx context.Context, ref string, o image.PullOptions) (io.ReadCloser, error) {
				return nil, s.wait(ctx)
			}
		}},
		{name: "ImagePullBodyRead", install: func(m *mockDockerClientForBuilder, s *stepBlocker) {
			m.imagePullFunc = func(ctx context.Context, ref string, o image.PullOptions) (io.ReadCloser, error) {
				return s.body(), nil
			}
		}},
		{name: "ImageBuildCall", install: func(m *mockDockerClientForBuilder, s *stepBlocker) {
			m.imageBuildFunc = func(ctx context.Context, bc io.Reader, o types.ImageBuildOptions) (types.ImageBuildResponse, error) {
				return types.ImageBuildResponse{}, s.wait(ctx)
			}
		}},
		{name: "ImageBuildBodyRead", install: func(m *mockDockerClientForBuilder, s *stepBlocker) {
			m.imageBuildFunc = func(ctx context.Context, bc io.Reader, o types.ImageBuildOptions) (types.ImageBuildResponse, error) {
				return types.ImageBuildResponse{Body: s.body()}, nil
			}
		}},
		{name: "PostBuildInspect", install: func(m *mockDockerClientForBuilder, s *stepBlocker) {
			m.imageInspectFunc = func(ctx context.Context, id string) (types.ImageInspect, []byte, error) {
				return types.ImageInspect{}, nil, s.wait(ctx)
			}
		}},
	}
}

func TestBuilder_DeadlineBoundsEveryDockerStep(t *testing.T) {
	for _, step := range deadlineSteps() {
		for _, mode := range []struct {
			name        string
			cooperative bool
		}{{"cooperative", true}, {"stuck", false}} {
			t.Run(step.name+"/"+mode.name, func(t *testing.T) {
				t.Parallel()
				catalog, profile := uncachedProfile()
				tag := expectedTagFor(t, catalog, profile)
				m := &mockDockerClientForBuilder{}
				if step.cached {
					m.imageListFunc = func(ctx context.Context, o image.ListOptions) ([]image.Summary, error) {
						return []image.Summary{{RepoTags: []string{tag}}}, nil
					}
				}
				s := newStepBlocker(t, mode.cooperative)
				step.install(m, s)
				b := runner.NewRuntimeImageBuilderWithTimeoutForTest(m, catalog, runner.EmbeddedTemplateForTest(), runner.ExpectedTemplateSHA256, deadlineBuildTimeout)

				done := make(chan buildOutcome, 1)
				start := time.Now()
				go func() {
					gotTag, err := b.BuildOrGetImage(context.Background(), profile)
					done <- buildOutcome{gotTag, err}
				}()
				var o buildOutcome
				select {
				case o = <-done:
				case <-time.After(deadlineBuildTimeout + deadlineBound):
					t.Fatalf("BuildOrGetImage still blocked %v after start (timeout %v)", time.Since(start), deadlineBuildTimeout)
				}
				requireClosed(t, "blocked step was reached", s.reached)
				if mode.cooperative {
					// The step itself must see the cancellation (ctx passed through, body
					// closed), not merely be outrun by the builder's bound.
					requireClosed(t, "cooperative step aborted by ctx or Close", s.aborted)
				}
				if o.tag != "" {
					t.Errorf("a timed-out call must not return a tag, got %q", o.tag)
				}
				if !errors.Is(o.err, context.DeadlineExceeded) {
					t.Errorf("expected DeadlineExceeded, got %v", o.err)
				}
				if !errors.Is(o.err, runner.ErrImageBuildFailed) && !errors.Is(o.err, runner.ErrImageIdentityMismatch) {
					t.Errorf("expected a fixed builder error, got %v", o.err)
				}
				if n := runner.InFlightCountForTest(b); n != 0 {
					t.Errorf("timed-out flight must be removed, got %d entries", n)
				}
			})
		}
	}
}

// A cold all-toolchain build was observed to exceed 15m; the default leaves headroom.
func TestRuntimeImageBuilder_DefaultBuildTimeoutIs45Minutes(t *testing.T) {
	if runner.DefaultBuildTimeoutForTest != 45*time.Minute {
		t.Fatalf("default build timeout = %v, want 45m", runner.DefaultBuildTimeoutForTest)
	}
}
