package runner

import (
	"errors"
	"strings"
	"testing"
)

// shippedWant holds the human-approved (AG-2, 2026-10-14) catalog values as independent
// literals. A one-character drift in any shipped value fails TestShippedCatalog_ApprovedValues.
var shippedWant = []RuntimeCatalogEntry{
	{
		Runtime: "go", Version: "1.27.2",
		ArchiveURL:    "https://go.dev/dl/go1.27.2.linux-amd64.tar.gz",
		SHA256:        "ecbadb99091a3f46e31f5f934b068b1864eafa7995211b39eaddf76996045fe5",
		ExtractPath:   "/usr/local/go",
		ArchiveFormat: "tar.gz",
		InstallLayout: "bin-dir",
		IsBaseline:    true,
	},
	{
		Runtime: "node", Version: "24.21.0",
		ArchiveURL:    "https://unofficial-builds.nodejs.org/download/release/v24.21.0/node-v24.21.0-linux-x64-musl.tar.xz",
		SHA256:        "dd5ca647e656c8200c873f45098a16623129c47f2387829f0a15a6de1dac1220",
		ExtractPath:   "/usr/local/node",
		ArchiveFormat: "tar.xz",
		InstallLayout: "bin-dir",
		IsBaseline:    true,
	},
	{
		Runtime: "python", Version: "3.11.17",
		ArchiveURL:    "https://github.com/astral-sh/python-build-standalone/releases/download/20261013/cpython-3.11.17%2B20261013-x86_64-unknown-linux-musl-install_only.tar.gz",
		SHA256:        "51160d0cad601f104c36b3c24fb3498f2cee4ca0134bb75fb9ef150aebf66c81",
		ExtractPath:   "/usr/local/python",
		ArchiveFormat: "tar.gz",
		InstallLayout: "bin-dir",
		IsBaseline:    true,
	},
	{
		Runtime: "java", Version: "25.0.4.1+1",
		ArchiveURL:    "https://github.com/adoptium/temurin25-binaries/releases/download/jdk-25.0.4.1%2B1/OpenJDK25U-jdk_x64_alpine-linux_hotspot_25.0.4.1_1.tar.gz",
		SHA256:        "f18648ee5ce45261f50dc9493fdae7ddebaa2a7a857cafda3e2a448a79c03dee",
		ExtractPath:   "/usr/local/java",
		ArchiveFormat: "tar.gz",
		InstallLayout: "bin-dir",
	},
	{
		Runtime: "rust", Version: "1.99.0",
		ArchiveURL:    "https://static.rust-lang.org/dist/rust-1.99.0-x86_64-unknown-linux-musl.tar.gz",
		SHA256:        "c812c6f52447721fe64cf0feb516769cf758475548a0da25c5b76b65613050a9",
		ExtractPath:   "/usr/local/rust",
		ArchiveFormat: "tar.gz",
		InstallLayout: "rust-installer",
	},
	{
		Runtime: "dotnet", Version: "10.0.401",
		ArchiveURL:    "https://builds.dotnet.microsoft.com/dotnet/Sdk/10.0.401/dotnet-sdk-10.0.401-linux-musl-x64.tar.gz",
		SHA256:        "17db2731521b0d1bda9a26656433edf6c586f7520e2c171089013dba09833e0f",
		ExtractPath:   "/usr/local/dotnet",
		ArchiveFormat: "tar.gz",
		InstallLayout: "dotnet-root",
	},
}

const shippedBaseWant = "alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6"

func shippedAllRuntimes() map[string]string {
	rts := map[string]string{}
	for _, e := range shippedWant {
		rts[e.Runtime] = e.Version
	}
	return rts
}

func shippedProfile(runtimes map[string]string) *ResolvedRuntimeProfile {
	return &ResolvedRuntimeProfile{
		Platform:       "linux/amd64",
		BaseImageRef:   DefaultBaseImageRef,
		OpenCodeDigest: DefaultOpenCodeDigest,
		Runtimes:       runtimes,
	}
}

func TestShippedCatalog_ApprovedValues(t *testing.T) {
	if DefaultBaseImageRef != shippedBaseWant {
		t.Fatalf("DefaultBaseImageRef = %q, want %q", DefaultBaseImageRef, shippedBaseWant)
	}
	cat := NewDefaultRuntimeCatalog()
	if got := len(cat.List()); got != len(shippedWant) {
		t.Fatalf("catalog has %d entries, want %d", got, len(shippedWant))
	}
	for _, want := range shippedWant {
		got, ok := cat.Get(want.Runtime, want.Version)
		if !ok || got == nil {
			t.Fatalf("%s@%s missing", want.Runtime, want.Version)
		}
		if *got != want {
			t.Errorf("%s entry mismatch:\n got: %+v\nwant: %+v", want.Runtime, *got, want)
		}
	}
}

func TestShippedCatalog_EveryEntryPassesPreflight(t *testing.T) {
	cat := NewDefaultRuntimeCatalog()
	for _, e := range cat.List() {
		if err := validateCatalogEntry(e); err != nil {
			t.Errorf("%s: validateCatalogEntry: %v", e.Runtime, err)
		}
		if err := validateProfile(shippedProfile(map[string]string{e.Runtime: e.Version}), cat); err != nil {
			t.Errorf("%s: validateProfile: %v", e.Runtime, err)
		}
	}
}

func TestShippedCatalog_DefaultResolvedProfilePassesPreflight(t *testing.T) {
	cat := NewDefaultRuntimeCatalog()
	profile, err := NewRuntimeResolver(cat).ResolveMultiRepo(t.Context(), nil, nil)
	if err != nil {
		t.Fatalf("resolve default profile: %v", err)
	}
	if err := validateProfile(profile, cat); err != nil {
		t.Fatalf("default profile refused: %v", err)
	}
	if err := validateProfile(shippedProfile(shippedAllRuntimes()), cat); err != nil {
		t.Fatalf("all-runtimes profile refused: %v", err)
	}
}

func renderShipped(t *testing.T) (string, map[string]string) {
	t.Helper()
	b := newRuntimeImageBuilder(nil, NewDefaultRuntimeCatalog(), embeddedTemplate, ExpectedTemplateSHA256)
	verified, err := loadVerifiedTemplate(b.templateBytes, b.expectedSHA)
	if err != nil {
		t.Fatalf("loadVerifiedTemplate: %v", err)
	}
	out, err := b.renderDockerfile(verified, shippedProfile(shippedAllRuntimes()))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	blocks := map[string]string{}
	for _, blk := range strings.Split(string(out), "# Toolchain: ")[1:] {
		blocks[strings.Fields(blk)[0]] = blk
	}
	return string(out), blocks
}

func TestShippedCatalog_RenderInstallsEveryToolchain(t *testing.T) {
	out, blocks := renderShipped(t)
	if strings.Contains(out, PendingSHA256) || strings.Contains(out, "PENDING") {
		t.Fatalf("rendered Dockerfile carries the pending sentinel:\n%s", out)
	}
	if !strings.Contains(out, "FROM "+shippedBaseWant+"\n") {
		t.Fatalf("rendered Dockerfile lacks pinned base")
	}
	if len(blocks) != len(shippedWant) {
		t.Fatalf("got %d toolchain blocks, want %d", len(blocks), len(shippedWant))
	}
	for _, e := range shippedWant {
		blk, ok := blocks[e.Runtime]
		if !ok {
			t.Fatalf("no block for %s", e.Runtime)
		}
		for _, want := range []string{
			`-o /tmp/atm-toolchain.archive "` + e.ArchiveURL + `"`,
			`echo "` + e.SHA256 + `  /tmp/atm-toolchain.archive" | sha256sum -c -`,
		} {
			if !strings.Contains(blk, want) {
				t.Errorf("%s block lacks %q:\n%s", e.Runtime, want, blk)
			}
		}
		sumIdx, tarIdx := strings.Index(blk, "sha256sum -c"), strings.Index(blk, "tar -x")
		if sumIdx < 0 || tarIdx < 0 || sumIdx > tarIdx {
			t.Errorf("%s: sha256sum -c must precede tar -x:\n%s", e.Runtime, blk)
		}
	}

	for _, rt := range []string{"go", "node", "python", "java"} {
		ext := "/usr/local/" + rt
		blk := blocks[rt]
		if !strings.Contains(blk, "-C "+ext+" --strip-components=1;") {
			t.Errorf("%s: missing direct extract into %s:\n%s", rt, ext, blk)
		}
		if !strings.Contains(blk, `ENV PATH="`+ext+`/bin:${PATH}"`) {
			t.Errorf("%s: missing bin PATH:\n%s", rt, blk)
		}
		if strings.Contains(blk, "install.sh") || strings.Contains(blk, "DOTNET_ROOT") {
			t.Errorf("%s: bin-dir block has foreign layout steps:\n%s", rt, blk)
		}
	}
	if !strings.Contains(blocks["node"], "tar -xJf") || !strings.Contains(blocks["go"], "tar -xzf") {
		t.Errorf("wrong tar flags")
	}

	rust := blocks["rust"]
	for _, want := range []string{
		"tar -xzf /tmp/atm-toolchain.archive -C /tmp/atm-toolchain-src --strip-components=1;",
		"(cd /tmp/atm-toolchain-src && ./install.sh --prefix=/usr/local/rust --disable-ldconfig --without=rust-docs);",
		"rm -rf /tmp/atm-toolchain-src;",
		`ENV PATH="/usr/local/rust/bin:${PATH}"`,
	} {
		if !strings.Contains(rust, want) {
			t.Errorf("rust block lacks %q:\n%s", want, rust)
		}
	}
	if strings.Contains(rust, "-C /usr/local/rust") {
		t.Errorf("rust archive must not be extracted directly into the prefix:\n%s", rust)
	}
	if strings.Index(rust, "sha256sum -c") > strings.Index(rust, "install.sh") {
		t.Errorf("rust: checksum must precede install.sh")
	}

	dotnet := blocks["dotnet"]
	for _, want := range []string{
		"tar -xzf /tmp/atm-toolchain.archive -C /usr/local/dotnet --strip-components=1;",
		`ENV DOTNET_ROOT="/usr/local/dotnet"`,
		`ENV PATH="/usr/local/dotnet:${PATH}"`,
	} {
		if !strings.Contains(dotnet, want) {
			t.Errorf("dotnet block lacks %q:\n%s", want, dotnet)
		}
	}
	if strings.Contains(dotnet, "/usr/local/dotnet/bin") || strings.Contains(dotnet, "install.sh") {
		t.Errorf("dotnet block has wrong layout:\n%s", dotnet)
	}
}

func TestTemplate_ApkLineHasDotnetAlpineDeps(t *testing.T) {
	out := renderFixture(t, map[string]string{"go": "1.27.2"})
	start := strings.Index(out, "RUN apk add --no-cache")
	if start < 0 {
		t.Fatalf("no apk line")
	}
	end := strings.Index(out[start:], "\n\n")
	apk := strings.Fields(strings.ReplaceAll(out[start:start+end], "\\", " "))
	have := map[string]bool{}
	for _, f := range apk {
		have[f] = true
	}
	for _, pkg := range []string{"icu-libs", "libintl", "libssl3", "zlib", "libgcc", "libstdc++", "ca-certificates"} {
		if !have[pkg] {
			t.Errorf("apk line lacks %s: %v", pkg, apk)
		}
	}
}

func TestValidateCatalogEntry_InstallLayout(t *testing.T) {
	for _, ok := range []string{"bin-dir", "rust-installer", "dotnet-root"} {
		e := pfValidEntry()
		e.InstallLayout = ok
		if err := validateCatalogEntry(e); err != nil {
			t.Errorf("layout %q rejected: %v", ok, err)
		}
	}
	for name, bad := range map[string]string{
		"empty":     "",
		"unknown":   "zip-dir",
		"uppercase": "BIN-DIR",
		"space":     " bin-dir",
		"newline":   "bin-dir\nRUN evil",
		"command":   "bin-dir; rm -rf /",
	} {
		t.Run(name, func(t *testing.T) {
			e := pfValidEntry()
			e.InstallLayout = bad
			err := validateCatalogEntry(e)
			if !errors.Is(err, ErrInvalidRuntimeProfile) {
				t.Fatalf("expected ErrInvalidRuntimeProfile, got %v", err)
			}
			if bad != "" && strings.Contains(err.Error(), bad) {
				t.Fatalf("error echoes value: %v", err)
			}
		})
	}
}

func TestRender_FailsClosedOnUnknownInstallLayout(t *testing.T) {
	for _, bad := range []string{"", "zip-dir", "bin-dir\nRUN evil"} {
		e := pfValidEntry()
		e.InstallLayout = bad
		_, err := executeTemplate(embeddedTemplate, TemplateData{
			BaseImageRef: pfBase, OpenCodeDigest: pfOpenCode, Toolchains: []RuntimeCatalogEntry{e},
		})
		if err == nil {
			t.Fatalf("render with layout %q should fail", bad)
		}
	}
}

// The sentinel machinery must still refuse pending values; these use fixtures, not the
// shipped catalog, which is now fully verified.
func TestPendingSentinel_FixtureCatalogRefused(t *testing.T) {
	var entries []RuntimeCatalogEntry
	for _, e := range shippedWant {
		e.SHA256 = PendingSHA256
		entries = append(entries, e)
	}
	cat := pfCatalogOf(entries...)
	for _, e := range entries {
		err := validateProfile(shippedProfile(map[string]string{e.Runtime: e.Version}), cat)
		if !errors.Is(err, ErrCatalogEntryUnverified) {
			t.Errorf("%s: expected ErrCatalogEntryUnverified, got %v", e.Runtime, err)
		}
	}
	p := shippedProfile(map[string]string{"go": "1.27.2"})
	p.BaseImageRef = "alpine:3.24@sha256:" + PendingSHA256
	if err := validateProfile(p, NewDefaultRuntimeCatalog()); !errors.Is(err, ErrInvalidRuntimeProfile) {
		t.Fatalf("pending base ref must be refused, got %v", err)
	}
}
