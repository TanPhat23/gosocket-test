package runner

import (
	"context"
	"time"

	"agent-task-manager/internal/sandbox"
)

// newRuntimeImageBuilderForTest injects template bytes and the expected hash.
func newRuntimeImageBuilderForTest(cli sandbox.DockerClient, catalog RuntimeCatalog, templateBytes []byte, expectedSHA string) RuntimeImageBuilder {
	return newRuntimeImageBuilder(cli, catalog, templateBytes, expectedSHA)
}

// Exported aliases for the external runner_test package. Test builds only.
var NewRuntimeImageBuilderForTest = newRuntimeImageBuilderForTest

// EmbeddedTemplateForTest returns a copy of the embedded template bytes.
func EmbeddedTemplateForTest() []byte { return append([]byte(nil), embeddedTemplate...) }

// FixtureCatalogForTest returns a catalog of synthetic, preflight-valid entries.
func FixtureCatalogForTest() RuntimeCatalog {
	mk := func(rt, ver, ext, format string) RuntimeCatalogEntry {
		return RuntimeCatalogEntry{
			Runtime: rt, Version: ver,
			ArchiveURL:    "https://example.invalid/" + rt + "-" + ver + "." + format,
			SHA256:        pfSHA,
			ExtractPath:   ext,
			ArchiveFormat: format,
			InstallLayout: "bin-dir",
		}
	}
	return pfCatalogOf(
		mk("go", "1.27.2", "/usr/local/go", "tar.gz"),
		mk("node", "24.21.0", "/usr/local/node", "tar.xz"),
		mk("python", "3.11.17", "/usr/local/python", "tar.xz"),
		mk("rust", "1.99.0", "/usr/local/rust", "tar.gz"),
	)
}

// PendingFixtureCatalogForTest returns FixtureCatalogForTest's entries with every checksum
// replaced by the PendingSHA256 sentinel.
func PendingFixtureCatalogForTest() RuntimeCatalog {
	c := FixtureCatalogForTest().(*pfCatalog)
	for k, e := range c.entries {
		e.SHA256 = PendingSHA256
		c.entries[k] = e
	}
	return c
}

// PendingBaseRefForTest is a base reference carrying the PendingSHA256 sentinel.
var PendingBaseRefForTest = "alpine:3.24@sha256:" + PendingSHA256

// FixtureBaseRefForTest is a synthetic digest-pinned base reference.
var FixtureBaseRefForTest = pfBase

// RenderDockerfileForTest renders the embedded template through a builder.
func RenderDockerfileForTest(catalog RuntimeCatalog, profile *ResolvedRuntimeProfile) ([]byte, error) {
	b := newRuntimeImageBuilder(nil, catalog, embeddedTemplate, ExpectedTemplateSHA256)
	return b.renderDockerfile(embeddedTemplate, profile)
}

// NewRuntimeImageBuilderWithTimeoutForTest injects a (very short) build timeout.
func NewRuntimeImageBuilderWithTimeoutForTest(cli sandbox.DockerClient, catalog RuntimeCatalog, templateBytes []byte, expectedSHA string, timeout time.Duration) RuntimeImageBuilder {
	b := newRuntimeImageBuilder(cli, catalog, templateBytes, expectedSHA)
	b.buildTimeout = timeout
	return b
}

// InFlightCountForTest returns the number of entries in the in-flight map.
func InFlightCountForTest(rb RuntimeImageBuilder) int {
	b := rb.(*runtimeImageBuilder)
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.inFlight)
}

// InFlightWaitersForTest returns the total waiter count over all in-flight builds.
func InFlightWaitersForTest(rb RuntimeImageBuilder) int {
	b := rb.(*runtimeImageBuilder)
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, f := range b.inFlight {
		f.mu.Lock()
		n += f.waiters
		f.mu.Unlock()
	}
	return n
}

// SetBuildLogfForTest replaces the build log sink and returns a restore function.
func SetBuildLogfForTest(f func(format string, args ...any)) (restore func()) {
	old := setBuildLogf(f)
	return func() { setBuildLogf(old) }
}

// PullPinnedImageForTest exposes the single-ref pre-build pull.
func PullPinnedImageForTest(ctx context.Context, cli sandbox.DockerClient, ref string) error {
	return pullPinnedImage(ctx, cli, ref)
}

// DefaultBuildTimeoutForTest is the production build timeout.
const DefaultBuildTimeoutForTest = defaultBuildTimeout
