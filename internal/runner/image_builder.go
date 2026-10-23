package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"

	"agent-task-manager/internal/sandbox"
)

// RuntimeImageBuilder coordinates deterministic image building and caching for sandboxes.
type RuntimeImageBuilder interface {
	BuildOrGetImage(ctx context.Context, profile *ResolvedRuntimeProfile) (string, error)
}

type buildResult struct {
	tag string
	err error
}

// defaultBuildTimeout bounds a single image build attempt, cache check and pulls included. A
// cold build of all six toolchains exceeded 15m in a live run; 45m leaves headroom for that.
const defaultBuildTimeout = 45 * time.Minute

// inFlightBuild is one build attempt for one tag. Lock order is always b.mu then flight.mu.
// res is written by the build goroutine before doneCh is closed and read only after it.
type inFlightBuild struct {
	doneCh chan struct{}
	res    buildResult

	mu       sync.Mutex // guards waiters
	waiters  int
	cancelFn context.CancelFunc
}

type runtimeImageBuilder struct {
	dockerCli     sandbox.DockerClient
	catalog       RuntimeCatalog
	templateBytes []byte
	expectedSHA   string
	resolver      RuntimeResolver
	buildTimeout  time.Duration
	gatewayCA     []byte // optional PEM of the gateway's CA certificate, baked into the image trust path
	gatewayCASHA  string

	mu       sync.Mutex
	inFlight map[string]*inFlightBuild
}

// NewRuntimeImageBuilder constructs a new RuntimeImageBuilder.
// The Dockerfile template is embedded in the binary and verified against ExpectedTemplateSHA256
// before every build.
func NewRuntimeImageBuilder(dockerCli sandbox.DockerClient, catalog RuntimeCatalog) RuntimeImageBuilder {
	return newRuntimeImageBuilder(dockerCli, catalog, embeddedTemplate, ExpectedTemplateSHA256)
}

// NewRuntimeImageBuilderWithGatewayCA is NewRuntimeImageBuilder plus the PEM certificate of the CA that
// signs the LLM gateway's TLS certificate (Gate 0G). The certificate is baked into every image and
// exported as NODE_EXTRA_CA_CERTS, and its hash is part of the image identity, so rotating the CA builds
// a new image rather than reusing a stale one. It fails closed on anything that is not CERTIFICATE PEM
// blocks, so a private key can never be baked in.
func NewRuntimeImageBuilderWithGatewayCA(dockerCli sandbox.DockerClient, catalog RuntimeCatalog, caPEM []byte) (RuntimeImageBuilder, error) {
	if err := validateGatewayCA(caPEM); err != nil {
		return nil, err
	}
	b := newRuntimeImageBuilder(dockerCli, catalog, embeddedTemplate, ExpectedTemplateSHA256)
	sum := sha256.Sum256(caPEM)
	b.gatewayCA = append([]byte(nil), caPEM...)
	b.gatewayCASHA = hex.EncodeToString(sum[:])
	return b, nil
}

// validateGatewayCA accepts only one or more well-formed X.509 CERTIFICATE PEM blocks and nothing else.
func validateGatewayCA(pemBytes []byte) error {
	rest := bytes.TrimSpace(pemBytes)
	if len(rest) == 0 {
		return errors.New("gateway CA is empty")
	}
	n := 0
	for len(rest) > 0 {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil || blk.Type != "CERTIFICATE" || len(blk.Headers) != 0 {
			return errors.New("gateway CA must contain only CERTIFICATE PEM blocks")
		}
		if _, err := x509.ParseCertificate(blk.Bytes); err != nil {
			return errors.New("gateway CA contains an invalid certificate")
		}
		rest = bytes.TrimSpace(rest)
		n++
	}
	if n == 0 {
		return errors.New("gateway CA contains no certificate")
	}
	return nil
}

func newRuntimeImageBuilder(dockerCli sandbox.DockerClient, catalog RuntimeCatalog, templateBytes []byte, expectedSHA string) *runtimeImageBuilder {
	return &runtimeImageBuilder{
		dockerCli:     dockerCli,
		catalog:       catalog,
		templateBytes: templateBytes,
		expectedSHA:   expectedSHA,
		resolver:      NewRuntimeResolver(catalog),
		buildTimeout:  defaultBuildTimeout,
		inFlight:      make(map[string]*inFlightBuild),
	}
}

// TemplateData passed to Dockerfile.project.template. OpenCodeImageRef is always built by
// openCodeImageRef, the same function that names the image pulled before the build.
type TemplateData struct {
	Platform         string
	BaseImageRef     string
	OpenCodeImageRef string
	Toolchains       []RuntimeCatalogEntry
	GatewayCASHA256  string // empty when no gateway CA is baked in
}

// Image label keys and value. The scheme label value is the short literal "v3". It names the
// label format and is deliberately not the ImageIdentityScheme constant
// ("atm-runtime-identity/v3"), which versions the canonical identity document that is hashed
// into the tag. Both are bumped together when the identity scheme changes.
const (
	labelRuntimeIdentity       = "io.atm.runtime.identity"
	labelRuntimeIdentityScheme = "io.atm.runtime.identity-scheme"
	labelIdentitySchemeValue   = "v3"
)

// BuildOrGetImage validates, renders and identifies the image first, with zero Docker calls:
// nil guard, validateProfile, template integrity, render, ComputeImageIdentity. Only then does
// it coalesce on an in-flight attempt, whose goroutine lists the cache, verifies a hit or builds.
// b.mu is never held across a Docker call.
func (b *runtimeImageBuilder) BuildOrGetImage(ctx context.Context, profile *ResolvedRuntimeProfile) (string, error) {
	if profile == nil {
		return "", fmt.Errorf("nil runtime profile")
	}
	if err := validateProfile(profile, b.catalog); err != nil {
		return "", err
	}
	tmplContent, err := loadVerifiedTemplate(b.templateBytes, b.expectedSHA)
	if err != nil {
		return "", err
	}
	dockerfileBytes, err := b.renderDockerfile(tmplContent, profile)
	if err != nil {
		return "", err
	}
	_, tag, err := ComputeImageIdentity(profile, b.catalog, tmplContent, dockerfileBytes)
	if err != nil {
		return "", fmt.Errorf("failed to compute cache tag: %w", err)
	}
	// The exact images the rendered Dockerfile references; pulled only on a cache miss.
	pullRefs := []string{profile.BaseImageRef, openCodeImageRef(profile.OpenCodeDigest)}

	b.mu.Lock()
	if flight, exists := b.inFlight[tag]; exists {
		flight.mu.Lock()
		flight.waiters++
		flight.mu.Unlock()
		b.mu.Unlock()
		return b.awaitFlight(ctx, flight, tag)
	}

	// The attempt outlives any single caller, so it is detached from ctx but always bounded.
	buildCtx, buildCancel := context.WithTimeout(context.Background(), b.buildTimeout)
	flight := &inFlightBuild{
		doneCh:   make(chan struct{}),
		waiters:  1,
		cancelFn: buildCancel,
	}
	b.inFlight[tag] = flight
	b.mu.Unlock()

	go func() {
		defer close(flight.doneCh)
		defer buildCancel() // release the timeout timer on every exit path
		resTag, resErr := b.resolveBounded(buildCtx, dockerfileBytes, tag, pullRefs)
		if resErr != nil && buildCtx.Err() != nil {
			// Surface why the attempt was aborted (timeout or cancellation).
			resErr = fmt.Errorf("%w: %w", resErr, buildCtx.Err())
		}

		b.mu.Lock()
		flight.res = buildResult{tag: resTag, err: resErr}
		// Only remove our own entry: a replacement flight may already own this tag.
		if b.inFlight[tag] == flight {
			delete(b.inFlight, tag)
		}
		b.mu.Unlock()
	}()

	return b.awaitFlight(ctx, flight, tag)
}

// resolveBounded returns when resolveImage does or when ctx ends, whichever is first, so the
// build timeout bounds every caller even if a Docker call ignores ctx or a response body's
// Read ignores Close. An abandoned resolveImage goroutine runs on until that step returns; its
// result is discarded and every later Docker call it makes receives the already-done ctx.
// When both channels are ready the choice is random; either way the attempt has hit its bound.
func (b *runtimeImageBuilder) resolveBounded(ctx context.Context, dockerfileBytes []byte, tag string, pullRefs []string) (string, error) {
	resCh := make(chan buildResult, 1) // buffered: an abandoned sender never blocks
	go func() {
		t, err := b.resolveImage(ctx, dockerfileBytes, tag, pullRefs)
		resCh <- buildResult{tag: t, err: err}
	}()
	select {
	case res := <-resCh:
		return res.tag, res.err
	case <-ctx.Done():
		return "", buildFail("docker step did not return after cancellation")
	}
}

// resolveImage runs inside a flight, with no builder lock held. A cache hit is verified and
// never rebuilt or retagged over; an absent tag is built after its pinned images are pulled.
func (b *runtimeImageBuilder) resolveImage(ctx context.Context, dockerfileBytes []byte, tag string, pullRefs []string) (string, error) {
	listFilters := filters.NewArgs(filters.Arg("reference", tag))
	images, err := b.dockerCli.ImageList(ctx, image.ListOptions{Filters: listFilters})
	if err != nil {
		return "", buildFail("failed to list docker images: " + err.Error())
	}
	for _, img := range images {
		for _, repoTag := range img.RepoTags {
			if repoTag == tag {
				return b.verifyCachedImage(ctx, tag)
			}
		}
	}
	return b.executeBuild(ctx, dockerfileBytes, tag, pullRefs)
}

// verifyCachedImage fails closed with ErrImageIdentityMismatch unless the image carrying tag
// has the identity labels for tag and the required platform. Engine detail is only logged.
func (b *runtimeImageBuilder) verifyCachedImage(ctx context.Context, tag string) (string, error) {
	identity := strings.TrimPrefix(tag, "atm-runtime:")
	inspected, _, err := b.dockerCli.ImageInspectWithRaw(ctx, tag)
	if err != nil {
		buildLogf("cached runtime image inspect failed: %s", sanitizeBuildDetail(err.Error()))
		return "", ErrImageIdentityMismatch
	}
	if inspected.Config == nil ||
		inspected.Config.Labels[labelRuntimeIdentity] != identity ||
		inspected.Config.Labels[labelRuntimeIdentityScheme] != labelIdentitySchemeValue ||
		inspected.Os != "linux" || inspected.Architecture != "amd64" {
		return "", ErrImageIdentityMismatch
	}
	return tag, nil
}

func (b *runtimeImageBuilder) awaitFlight(ctx context.Context, flight *inFlightBuild, tag string) (string, error) {
	select {
	case <-flight.doneCh:
		return flight.res.tag, flight.res.err
	case <-ctx.Done():
		// b.mu first (lock order b.mu -> flight.mu) so a joiner can never attach to a
		// flight whose last waiter is leaving: removal and decrement are one critical section.
		b.mu.Lock()
		flight.mu.Lock()
		flight.waiters--
		abandoned := flight.waiters <= 0
		flight.mu.Unlock()
		if abandoned && b.inFlight[tag] == flight {
			delete(b.inFlight, tag)
		}
		b.mu.Unlock()

		if abandoned {
			// All callers left: abort the underlying build (no lock held).
			flight.cancelFn()
		}
		return "", ctx.Err()
	}
}

// renderDockerfile renders already-verified template bytes. A runtime missing from the
// catalog fails closed; there is no fallback entry.
func (b *runtimeImageBuilder) renderDockerfile(verifiedTemplate []byte, profile *ResolvedRuntimeProfile) ([]byte, error) {
	runtimeNames := make([]string, 0, len(profile.Runtimes))
	for name := range profile.Runtimes {
		runtimeNames = append(runtimeNames, name)
	}
	sort.Strings(runtimeNames)

	var toolchains []RuntimeCatalogEntry
	for _, name := range runtimeNames {
		ver := profile.Runtimes[name]
		entry, ok := b.catalog.Get(name, ver)
		if !ok || entry == nil {
			return nil, fmt.Errorf("runtime %q version %q is not in the catalog", name, ver)
		}
		toolchains = append(toolchains, *entry)
	}

	return executeTemplate(verifiedTemplate, TemplateData{
		Platform:         profile.Platform,
		BaseImageRef:     profile.BaseImageRef,
		OpenCodeImageRef: openCodeImageRef(profile.OpenCodeDigest),
		Toolchains:       toolchains,
		GatewayCASHA256:  b.gatewayCASHA,
	})
}

func (b *runtimeImageBuilder) executeBuild(ctx context.Context, dockerfileBytes []byte, tag string, pullRefs []string) (string, error) {
	// BuildKit via the plain Engine API has no client session to resolve remote FROM/COPY
	// --from refs, so the digest-pinned images must already be local. A pull failure is final.
	if err := pullPinnedImages(ctx, b.dockerCli, pullRefs); err != nil {
		return "", err
	}

	entrypoint, err := loadVerifiedTemplate(embeddedEntrypoint, ExpectedEntrypointSHA256)
	if err != nil {
		return "", err
	}

	// Create tar archive in memory containing Dockerfile and the pinned entrypoint
	tarBuf := new(bytes.Buffer)
	tw := tar.NewWriter(tarBuf)
	hdr := &tar.Header{
		Name: "Dockerfile",
		Mode: 0644,
		Size: int64(len(dockerfileBytes)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return "", fmt.Errorf("failed to write tar header: %w", err)
	}
	if _, err := tw.Write(dockerfileBytes); err != nil {
		return "", fmt.Errorf("failed to write Dockerfile into tar: %w", err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "atm-entrypoint.sh", Mode: 0755, Size: int64(len(entrypoint))}); err != nil {
		return "", fmt.Errorf("failed to write entrypoint tar header: %w", err)
	}
	if _, err := tw.Write(entrypoint); err != nil {
		return "", fmt.Errorf("failed to write entrypoint into tar: %w", err)
	}
	if len(b.gatewayCA) > 0 {
		if err := tw.WriteHeader(&tar.Header{Name: "atm-gateway-ca.crt", Mode: 0644, Size: int64(len(b.gatewayCA))}); err != nil {
			return "", fmt.Errorf("failed to write gateway CA tar header: %w", err)
		}
		if _, err := tw.Write(b.gatewayCA); err != nil {
			return "", fmt.Errorf("failed to write gateway CA into tar: %w", err)
		}
	}
	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("failed to close tar writer: %w", err)
	}

	// Call ImageBuild. The identity labels let the post-build inspect confirm that the image
	// carrying this tag really is the one built for this identity.
	identity := strings.TrimPrefix(tag, "atm-runtime:")
	buildOpts := types.ImageBuildOptions{
		Tags:        []string{tag},
		Remove:      true,
		ForceRemove: true,
		Platform:    requiredPlatform,
		Version:     types.BuilderBuildKit,
		Labels: map[string]string{
			labelRuntimeIdentity:       identity,
			labelRuntimeIdentityScheme: labelIdentitySchemeValue,
		},
	}
	resp, err := b.dockerCli.ImageBuild(ctx, bytes.NewReader(tarBuf.Bytes()), buildOpts)
	if err != nil {
		return "", buildFail("failed to build image: " + err.Error())
	}
	// Cancellation or timeout must abort a blocked stream read: closing the body unblocks Read.
	stopCloser := context.AfterFunc(ctx, func() { _ = resp.Body.Close() })
	defer stopCloser()
	defer resp.Body.Close()

	auxID, err := consumeBuildStream(resp.Body, maxBuildStreamBytes)
	if err != nil {
		return "", err
	}

	// The post-build inspect is the authority; a stream that ended cleanly proves nothing.
	inspected, _, err := b.dockerCli.ImageInspectWithRaw(ctx, tag)
	if err != nil {
		return "", buildFail("post-build inspect failed: " + err.Error())
	}
	if inspected.Config == nil ||
		inspected.Config.Labels[labelRuntimeIdentity] != identity ||
		inspected.Config.Labels[labelRuntimeIdentityScheme] != labelIdentitySchemeValue {
		return "", buildFail("post-build inspect: identity labels mismatch")
	}
	if inspected.Os != "linux" || inspected.Architecture != "amd64" {
		return "", buildFail("post-build inspect: unexpected os/architecture")
	}
	if auxID != "" && auxID != inspected.ID {
		return "", buildFail("post-build inspect: image ID differs from build output")
	}

	return tag, nil
}
