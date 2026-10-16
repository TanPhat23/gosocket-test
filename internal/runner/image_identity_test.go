package runner_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"agent-task-manager/internal/runner"
)

// All hex values below are obviously synthetic test fixtures, NOT real checksums.
var (
	fixBase     = "alpine:3.24@sha256:" + strings.Repeat("ab", 32)
	fixOpenCode = "sha256:" + strings.Repeat("cd", 32)
	fixSHAGo    = strings.Repeat("11", 32)
	fixSHANode  = strings.Repeat("22", 32)
)

type fixtureCatalog struct {
	entries map[string]runner.RuntimeCatalogEntry
}

func (c *fixtureCatalog) Get(rt, ver string) (*runner.RuntimeCatalogEntry, bool) {
	e, ok := c.entries[rt+"@"+ver]
	if !ok {
		return nil, false
	}
	return &e, true
}
func (c *fixtureCatalog) GetBaseline(string) (string, bool)   { return "", false }
func (c *fixtureCatalog) List() []runner.RuntimeCatalogEntry  { return nil }
func (c *fixtureCatalog) ValidateVersion(string, string) bool { return false }

func newFixtureCatalog() *fixtureCatalog {
	return &fixtureCatalog{entries: map[string]runner.RuntimeCatalogEntry{
		"go@1.27.2": {
			Runtime: "go", Version: "1.27.2",
			ArchiveURL: "https://example.invalid/go.tar.gz", SHA256: fixSHAGo,
			ExtractPath: "/usr/local/go", ArchiveFormat: "tar.gz", InstallLayout: "bin-dir",
		},
		"node@24.21.0": {
			Runtime: "node", Version: "24.21.0",
			ArchiveURL: "https://example.invalid/node.tar.xz", SHA256: fixSHANode,
			ExtractPath: "/usr/local/node", ArchiveFormat: "tar.xz", InstallLayout: "bin-dir",
		},
		"go@1.26.0": {
			Runtime: "go", Version: "1.26.0",
			ArchiveURL: "https://example.invalid/go126.tar.gz", SHA256: fixSHAGo,
			ExtractPath: "/usr/local/go", ArchiveFormat: "tar.gz", InstallLayout: "bin-dir",
		},
		"rust@1.99.0": {
			Runtime: "rust", Version: "1.99.0",
			ArchiveURL: "https://example.invalid/rust.tar.xz", SHA256: fixSHANode,
			ExtractPath: "/usr/local/rust", ArchiveFormat: "tar.xz", InstallLayout: "bin-dir",
		},
	}}
}

func fixtureProfile() *runner.ResolvedRuntimeProfile {
	return &runner.ResolvedRuntimeProfile{
		Platform:       "linux/amd64",
		BaseImageRef:   fixBase,
		TemplateRev:    "v1",
		OpenCodeDigest: fixOpenCode,
		Runtimes:       map[string]string{"go": "1.27.2", "node": "24.21.0"},
	}
}

var (
	fixTemplate = []byte("template-fixture")
	fixRendered = []byte("rendered-fixture")
)

func mustTag(t *testing.T, p *runner.ResolvedRuntimeProfile, c runner.RuntimeCatalog, tpl, ren []byte) string {
	t.Helper()
	_, tag, err := runner.ComputeImageIdentity(p, c, tpl, ren)
	if err != nil {
		t.Fatalf("ComputeImageIdentity: %v", err)
	}
	return tag
}

func TestImageIdentity_EachFieldChangesTag(t *testing.T) {
	baseTag := mustTag(t, fixtureProfile(), newFixtureCatalog(), fixTemplate, fixRendered)

	type tc struct {
		name    string
		profile func(p *runner.ResolvedRuntimeProfile)
		catalog func(c *fixtureCatalog)
		tpl     []byte
		ren     []byte
	}
	setEntry := func(key string, f func(e *runner.RuntimeCatalogEntry)) func(c *fixtureCatalog) {
		return func(c *fixtureCatalog) {
			e := c.entries[key]
			f(&e)
			c.entries[key] = e
		}
	}
	cases := []tc{
		{name: "Platform", profile: func(p *runner.ResolvedRuntimeProfile) { p.Platform = "linux/arm64" }},
		{name: "BaseImageRef", profile: func(p *runner.ResolvedRuntimeProfile) {
			p.BaseImageRef = "alpine:3.24@sha256:" + strings.Repeat("ef", 32)
		}},
		{name: "OpenCodeDigest", profile: func(p *runner.ResolvedRuntimeProfile) {
			p.OpenCodeDigest = "sha256:" + strings.Repeat("ee", 32)
		}},
		{name: "TemplateSHA256", tpl: []byte("template-fixture-changed")},
		{name: "RenderedDockerfileSHA256", ren: []byte("rendered-fixture-changed")},
		{name: "RuntimeVersion", profile: func(p *runner.ResolvedRuntimeProfile) { p.Runtimes["go"] = "1.26.0" }},
		{name: "AddRuntime", profile: func(p *runner.ResolvedRuntimeProfile) { p.Runtimes["rust"] = "1.99.0" }},
		{name: "RemoveRuntime", profile: func(p *runner.ResolvedRuntimeProfile) { delete(p.Runtimes, "node") }},
		{name: "ToolchainURL", catalog: setEntry("go@1.27.2", func(e *runner.RuntimeCatalogEntry) { e.ArchiveURL += "?x=1" })},
		{name: "ToolchainSHA256", catalog: setEntry("go@1.27.2", func(e *runner.RuntimeCatalogEntry) { e.SHA256 = strings.Repeat("99", 32) })},
		{name: "ToolchainExtractPath", catalog: setEntry("go@1.27.2", func(e *runner.RuntimeCatalogEntry) { e.ExtractPath = "/usr/local/go2" })},
		{name: "ToolchainArchiveFormat", catalog: setEntry("go@1.27.2", func(e *runner.RuntimeCatalogEntry) { e.ArchiveFormat = "tar.xz" })},
		{name: "ToolchainInstallLayout", catalog: setEntry("go@1.27.2", func(e *runner.RuntimeCatalogEntry) { e.InstallLayout = "rust-installer" })},
	}

	seen := map[string]string{baseTag: "base"}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := fixtureProfile()
			if c.profile != nil {
				c.profile(p)
			}
			cat := newFixtureCatalog()
			if c.catalog != nil {
				c.catalog(cat)
			}
			tpl, ren := fixTemplate, fixRendered
			if c.tpl != nil {
				tpl = c.tpl
			}
			if c.ren != nil {
				ren = c.ren
			}
			tag := mustTag(t, p, cat, tpl, ren)
			if tag == baseTag {
				t.Fatalf("changing %s did not change the tag", c.name)
			}
			if prev, dup := seen[tag]; dup {
				t.Fatalf("tag for %s collides with %s", c.name, prev)
			}
			seen[tag] = c.name
		})
	}
}

func TestImageIdentity_DeterministicAcrossMapOrder(t *testing.T) {
	names := []string{"go", "node", "rust"}
	vers := map[string]string{"go": "1.27.2", "node": "24.21.0", "rust": "1.99.0"}
	var want string
	for i := 0; i < 100; i++ {
		rt := make(map[string]string)
		// Rotate insertion order each iteration.
		for j := range names {
			n := names[(i+j)%len(names)]
			rt[n] = vers[n]
		}
		p := fixtureProfile()
		p.Runtimes = rt
		tag := mustTag(t, p, newFixtureCatalog(), fixTemplate, fixRendered)
		if i == 0 {
			want = tag
			continue
		}
		if tag != want {
			t.Fatalf("iteration %d: tag %s != %s", i, tag, want)
		}
	}
}

func TestImageIdentity_GoldenCanonicalBytes(t *testing.T) {
	id, tag, err := runner.ComputeImageIdentity(fixtureProfile(), newFixtureCatalog(), fixTemplate, fixRendered)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	// Golden bytes: synthetic fixture values only. Hex of "template-fixture" and
	// "rendered-fixture" was derived independently with sha256sum.
	want := `{"scheme":"atm-runtime-identity/v3",` +
		`"platform":"linux/amd64",` +
		`"base_image_ref":"alpine:3.24@sha256:` + strings.Repeat("ab", 32) + `",` +
		`"opencode_digest":"sha256:` + strings.Repeat("cd", 32) + `",` +
		`"template_sha256":"0fa4ae18c9801fe4fbb111d383e27ffbd681aec560925425cc68a7db39d80028",` +
		`"rendered_dockerfile_sha256":"d6a4c11c9cbd42d5be44238d235cfd05ce3efc4251c9a9571ee77766ba26f0d5",` +
		`"runtimes":{"go":"1.27.2","node":"24.21.0"},` +
		`"toolchains":[` +
		`{"runtime":"go","version":"1.27.2","archive_url":"https://example.invalid/go.tar.gz","sha256":"` + fixSHAGo + `","extract_path":"/usr/local/go","archive_format":"tar.gz","install_layout":"bin-dir"},` +
		`{"runtime":"node","version":"24.21.0","archive_url":"https://example.invalid/node.tar.xz","sha256":"` + fixSHANode + `","extract_path":"/usr/local/node","archive_format":"tar.xz","install_layout":"bin-dir"}` +
		`]}`
	if string(got) != want {
		t.Fatalf("canonical bytes changed:\n got: %s\nwant: %s", got, want)
	}
	sum := sha256.Sum256([]byte(want))
	if wantTag := "atm-runtime:" + hex.EncodeToString(sum[:]); tag != wantTag {
		t.Fatalf("tag %s != sha256 of canonical bytes %s", tag, wantTag)
	}
	const pinnedTag = "atm-runtime:778cee0c84deee3b961cbba2a60e688f2d2e80ceab0bad60f884d6ef28f8f854"
	if tag != pinnedTag {
		t.Fatalf("pinned tag mismatch: got %s want %s", tag, pinnedTag)
	}
}

func TestImageIdentity_SchemeVersionPresent(t *testing.T) {
	p := fixtureProfile()
	id, tag, err := runner.ComputeImageIdentity(p, newFixtureCatalog(), fixTemplate, fixRendered)
	if err != nil {
		t.Fatal(err)
	}
	if id.Scheme != "atm-runtime-identity/v3" {
		t.Fatalf("scheme = %q", id.Scheme)
	}
	// Legacy tag: sha256 of only the runtimes map.
	legacyBytes, _ := json.Marshal(p.Runtimes)
	sum := sha256.Sum256(legacyBytes)
	legacy := "atm-runtime:" + hex.EncodeToString(sum[:])
	if tag == legacy {
		t.Fatal("v3 tag equals legacy runtimes-only tag")
	}
}

func TestImageIdentity_ToolchainsSortedByRuntime(t *testing.T) {
	p := fixtureProfile()
	p.Runtimes = map[string]string{"rust": "1.99.0", "node": "24.21.0", "go": "1.27.2"}
	for i := 0; i < 50; i++ {
		id, _, err := runner.ComputeImageIdentity(p, newFixtureCatalog(), fixTemplate, fixRendered)
		if err != nil {
			t.Fatal(err)
		}
		if len(id.Toolchains) != 3 || id.Toolchains[0].Runtime != "go" || id.Toolchains[1].Runtime != "node" || id.Toolchains[2].Runtime != "rust" {
			t.Fatalf("toolchains not sorted: %+v", id.Toolchains)
		}
	}
}

func TestImageIdentity_RejectsIncompleteInputs(t *testing.T) {
	cat := newFixtureCatalog()
	uncataloged := fixtureProfile()
	uncataloged.Runtimes["python"] = "3.11.17"

	cases := []struct {
		name    string
		profile *runner.ResolvedRuntimeProfile
		cat     runner.RuntimeCatalog
		tpl     []byte
		ren     []byte
	}{
		{"nil profile", nil, cat, fixTemplate, fixRendered},
		{"nil catalog", fixtureProfile(), nil, fixTemplate, fixRendered},
		{"nil template", fixtureProfile(), cat, nil, fixRendered},
		{"empty template", fixtureProfile(), cat, []byte{}, fixRendered},
		{"nil rendered", fixtureProfile(), cat, fixTemplate, nil},
		{"empty rendered", fixtureProfile(), cat, fixTemplate, []byte{}},
		{"uncataloged runtime", uncataloged, cat, fixTemplate, fixRendered},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, tag, err := runner.ComputeImageIdentity(c.profile, c.cat, c.tpl, c.ren)
			if err == nil || tag != "" {
				t.Fatalf("expected error and empty tag, got tag=%q err=%v", tag, err)
			}
		})
	}
}

func TestImageIdentity_EmptyRuntimesStillMarshalsDeterministically(t *testing.T) {
	p := fixtureProfile()
	p.Runtimes = map[string]string{}
	a := mustTag(t, p, newFixtureCatalog(), fixTemplate, fixRendered)
	p.Runtimes = nil
	b := mustTag(t, p, newFixtureCatalog(), fixTemplate, fixRendered)
	if a != b {
		t.Fatalf("nil vs empty runtimes differ: %s vs %s", a, b)
	}
}
