package runner

import (
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
		}
	}
	return pfCatalogOf(
		mk("go", "1.27.2", "/usr/local/go", "tar.gz"),
		mk("node", "24.21.0", "/usr/local/node", "tar.xz"),
		mk("python", "3.11.17", "/usr/local/python", "tar.xz"),
		mk("rust", "1.99.0", "/usr/local/rust", "tar.gz"),
	)
}

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
	old := buildLogf
	buildLogf = f
	return func() { buildLogf = old }
}
