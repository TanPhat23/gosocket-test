package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"
)

// goldenTemplateSHA256 is derived independently with `sha256sum` of
// templates/Dockerfile.project.template. Update it deliberately, in the same reviewed change
// as any template edit. It must equal ExpectedTemplateSHA256.
const goldenTemplateSHA256 = "dea936655ad7b110a317e155b223ddd0d62d4fb91d34b5f8527f82c850a7ffc4"

func tplFixtureCatalog() *pfCatalog {
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
	)
}

func tplProfile(runtimes map[string]string) *ResolvedRuntimeProfile {
	return &ResolvedRuntimeProfile{
		Platform:       "linux/amd64",
		BaseImageRef:   pfBase,
		OpenCodeDigest: pfOpenCode,
		Runtimes:       runtimes,
	}
}

func renderFixture(t *testing.T, runtimes map[string]string) string {
	t.Helper()
	b := newRuntimeImageBuilder(nil, tplFixtureCatalog(), embeddedTemplate, ExpectedTemplateSHA256)
	verified, err := loadVerifiedTemplate(b.templateBytes, b.expectedSHA)
	if err != nil {
		t.Fatalf("loadVerifiedTemplate: %v", err)
	}
	out, err := b.renderDockerfile(verified, tplProfile(runtimes))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return string(out)
}

var allRuntimes = map[string]string{"go": "1.27.2", "node": "24.21.0", "python": "3.11.17"}

func TestEmbeddedTemplate_MatchesPinnedSHA256(t *testing.T) {
	sum := sha256.Sum256(embeddedTemplate)
	got := hex.EncodeToString(sum[:])
	if got != goldenTemplateSHA256 {
		t.Fatalf("embedded template sha256 = %s, golden = %s", got, goldenTemplateSHA256)
	}
	if ExpectedTemplateSHA256 != goldenTemplateSHA256 {
		t.Fatalf("ExpectedTemplateSHA256 = %q, golden = %q", ExpectedTemplateSHA256, goldenTemplateSHA256)
	}
}

func TestLoadVerifiedTemplate_AcceptsMatchingBytes(t *testing.T) {
	raw := []byte("FROM {{.BaseImageRef}}\n")
	sum := sha256.Sum256(raw)
	got, err := loadVerifiedTemplate(raw, hex.EncodeToString(sum[:]))
	if err != nil || string(got) != string(raw) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLoadVerifiedTemplate_RejectsTamperedBytes(t *testing.T) {
	raw := []byte("FROM {{.BaseImageRef}}\n")
	sum := sha256.Sum256(raw)
	expected := hex.EncodeToString(sum[:])
	tampered := append([]byte(nil), raw...)
	tampered[0] ^= 0x01
	// An unparseable template proves the integrity error wins over template.Parse.
	for _, in := range [][]byte{tampered, []byte("{{ unterminated"), nil} {
		out, err := loadVerifiedTemplate(in, expected)
		if !errors.Is(err, ErrTemplateIntegrity) {
			t.Fatalf("expected ErrTemplateIntegrity for %q, got %v", in, err)
		}
		if out != nil {
			t.Fatalf("expected no bytes on failure")
		}
	}
}

func TestLoadVerifiedTemplate_RejectsBadExpectedHash(t *testing.T) {
	for _, exp := range []string{"", "STUB", strings.ToUpper(strings.Repeat("ab", 32))} {
		if _, err := loadVerifiedTemplate([]byte("x"), exp); !errors.Is(err, ErrTemplateIntegrity) {
			t.Fatalf("expected ErrTemplateIntegrity for expected=%q, got %v", exp, err)
		}
	}
}

func TestBuilder_TamperedTemplateFailsBeforeAnyDockerCall(t *testing.T) {
	tampered := append([]byte(nil), embeddedTemplate...)
	tampered = append(tampered, '#')
	b := newRuntimeImageBuilder(nil, tplFixtureCatalog(), tampered, ExpectedTemplateSHA256)
	// nil docker client: any Docker call would panic.
	_, err := b.BuildOrGetImage(t.Context(), tplProfile(map[string]string{"go": "1.27.2"}))
	if !errors.Is(err, ErrTemplateIntegrity) {
		t.Fatalf("expected ErrTemplateIntegrity, got %v", err)
	}
}

func TestRenderedDockerfile_NoPipeToTar(t *testing.T) {
	out := renderFixture(t, allRuntimes)
	re := regexp.MustCompile(`curl[^\n]*\|\s*tar`)
	if re.MatchString(out) {
		t.Fatalf("rendered Dockerfile pipes curl to tar:\n%s", out)
	}
}

func TestRenderedDockerfile_ChecksumPrecedesExtract(t *testing.T) {
	out := renderFixture(t, allRuntimes)
	blocks := strings.Split(out, "# Toolchain: ")[1:]
	if len(blocks) != 3 {
		t.Fatalf("expected 3 toolchain blocks, got %d", len(blocks))
	}
	want := pfSHA + "  /tmp/atm-toolchain.archive"
	for _, blk := range blocks {
		sumIdx := strings.Index(blk, "sha256sum -c")
		tarIdx := strings.Index(blk, "tar -x")
		if sumIdx < 0 || tarIdx < 0 || sumIdx > tarIdx {
			t.Fatalf("sha256sum -c must precede tar -x in block:\n%s", blk)
		}
		if !strings.Contains(blk, want) {
			t.Fatalf("block lacks catalog checksum line %q:\n%s", want, blk)
		}
		if !strings.Contains(blk, "--proto '=https' --tlsv1.2") {
			t.Fatalf("block lacks https-only curl flags:\n%s", blk)
		}
		if !strings.Contains(blk, "set -eu;") {
			t.Fatalf("block lacks set -eu:\n%s", blk)
		}
	}
	if !strings.Contains(blocks[0], "tar -xzf") || !strings.Contains(blocks[1], "tar -xJf") {
		t.Fatalf("tar flags wrong: go block %q node block %q", blocks[0], blocks[1])
	}
}

func TestRenderedDockerfile_FromUsesPinnedBase(t *testing.T) {
	out := renderFixture(t, allRuntimes)
	var from []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "FROM ") {
			from = append(from, line)
		}
	}
	if len(from) != 1 || from[0] != "FROM "+pfBase || !strings.Contains(from[0], "@sha256:") {
		t.Fatalf("unexpected FROM lines: %q", from)
	}
}

func TestRenderedDockerfile_NoFloatingSyntaxDirective(t *testing.T) {
	out := renderFixture(t, allRuntimes)
	if strings.Contains(out, "# syntax") || strings.Contains(out, "#syntax") {
		t.Fatalf("rendered Dockerfile contains a syntax directive")
	}
	if strings.Contains(out, "alpine:3.24\n") {
		t.Fatalf("rendered Dockerfile contains a floating base")
	}
}

func TestRender_FailsClosedOnInvalidSHA(t *testing.T) {
	for _, bad := range []string{"", PendingSHA256, strings.Repeat("AB", 32), strings.Repeat("ab", 31), "x\n" + pfSHA} {
		if _, err := requireSHA256(bad); err == nil {
			t.Fatalf("requireSHA256(%q) should fail", bad)
		}
		entry := pfValidEntry()
		entry.SHA256 = bad
		_, err := executeTemplate(embeddedTemplate, TemplateData{
			BaseImageRef: pfBase, OpenCodeDigest: pfOpenCode, Toolchains: []RuntimeCatalogEntry{entry},
		})
		if err == nil {
			t.Fatalf("template execution with sha %q should fail", bad)
		}
	}
	if got, err := requireSHA256(pfSHA); err != nil || got != pfSHA {
		t.Fatalf("valid sha rejected: %q %v", got, err)
	}
}

func TestTarFlag(t *testing.T) {
	for in, want := range map[string]string{"tar.gz": "z", "tar.xz": "J"} {
		if got, err := tarFlag(in); err != nil || got != want {
			t.Fatalf("tarFlag(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "zip", "tar.bz2", "tar.gz; rm -rf /"} {
		if _, err := tarFlag(in); err == nil {
			t.Fatalf("tarFlag(%q) should fail", in)
		}
	}
}

func TestRender_MissingKeyErrors(t *testing.T) {
	_, err := executeTemplate([]byte("FROM {{.NoSuchField}}\n"), TemplateData{})
	if err == nil {
		t.Fatalf("unknown field must fail")
	}
	// missingkey=error applies to maps; verify the option is honoured on a map-typed lookup.
	tmpl, perr := newDockerfileTemplate([]byte("{{.absent}}"))
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, map[string]string{}); err == nil {
		t.Fatalf("missing map key must fail with missingkey=error, got %q", sb.String())
	}
}

func TestRender_EveryRequestedToolchainPresent(t *testing.T) {
	for _, rts := range []map[string]string{
		{"go": "1.27.2"},
		{"go": "1.27.2", "node": "24.21.0"},
		allRuntimes,
	} {
		out := renderFixture(t, rts)
		if n := strings.Count(out, "# Toolchain: "); n != len(rts) {
			t.Fatalf("%d runtimes in, %d toolchain blocks out:\n%s", len(rts), n, out)
		}
		if n := strings.Count(out, "sha256sum -c"); n != len(rts) {
			t.Fatalf("%d runtimes in, %d checksum steps out", len(rts), n)
		}
	}
}

func TestRender_EntryWithoutURLStillRendersMandatoryBlock(t *testing.T) {
	// A catalog entry with no ArchiveURL must not be silently skipped by the template.
	entry := pfValidEntry()
	entry.ArchiveURL = ""
	out, err := executeTemplate(embeddedTemplate, TemplateData{
		BaseImageRef: pfBase, OpenCodeDigest: pfOpenCode, Toolchains: []RuntimeCatalogEntry{entry},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(out), "sha256sum -c") {
		t.Fatalf("toolchain with empty URL was silently dropped:\n%s", out)
	}
}

func TestRender_UncatalogedRuntimeFailsClosed(t *testing.T) {
	b := newRuntimeImageBuilder(nil, tplFixtureCatalog(), embeddedTemplate, ExpectedTemplateSHA256)
	_, err := b.renderDockerfile(embeddedTemplate, tplProfile(map[string]string{"ruby": "3.3.0"}))
	if err == nil {
		t.Fatalf("uncataloged runtime must fail closed")
	}
}
