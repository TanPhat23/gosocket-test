package runner

import (
	"errors"
	"strings"
	"testing"
)

// All hex values here are synthetic fixtures, NOT real checksums.
var (
	pfSHA      = strings.Repeat("ab", 32)
	pfBase     = "alpine:3.24@sha256:" + strings.Repeat("cd", 32)
	pfOpenCode = "sha256:" + strings.Repeat("ef", 32)
)

func pfValidEntry() RuntimeCatalogEntry {
	return RuntimeCatalogEntry{
		Runtime:       "go",
		Version:       "1.27.2",
		ArchiveURL:    "https://go.dev/dl/go1.27.2.linux-amd64.tar.gz",
		SHA256:        pfSHA,
		ExtractPath:   "/usr/local/go",
		ArchiveFormat: "tar.gz",
		InstallLayout: "bin-dir",
	}
}

type pfCatalog struct {
	entries map[string]RuntimeCatalogEntry
}

func (c *pfCatalog) Get(rt, ver string) (*RuntimeCatalogEntry, bool) {
	e, ok := c.entries[rt+"@"+ver]
	if !ok {
		return nil, false
	}
	return &e, true
}
func (c *pfCatalog) GetBaseline(string) (string, bool)   { return "", false }
func (c *pfCatalog) List() []RuntimeCatalogEntry         { return nil }
func (c *pfCatalog) ValidateVersion(string, string) bool { return false }

func pfCatalogOf(entries ...RuntimeCatalogEntry) *pfCatalog {
	c := &pfCatalog{entries: map[string]RuntimeCatalogEntry{}}
	for _, e := range entries {
		c.entries[e.Runtime+"@"+e.Version] = e
	}
	return c
}

func pfValidProfile() *ResolvedRuntimeProfile {
	return &ResolvedRuntimeProfile{
		Platform:       "linux/amd64",
		BaseImageRef:   pfBase,
		OpenCodeDigest: pfOpenCode,
		Runtimes:       map[string]string{"go": "1.27.2"},
	}
}

func TestValidateCatalogEntry_AcceptsValid(t *testing.T) {
	if err := validateCatalogEntry(pfValidEntry()); err != nil {
		t.Fatalf("expected valid entry accepted, got %v", err)
	}
	e := pfValidEntry()
	e.ArchiveURL = "https://nodejs.org/dist/v24.21.0/node-v24.21.0-linux-x64.tar.xz"
	e.ArchiveFormat = "tar.xz"
	e.ExtractPath = "/usr/local/node"
	if err := validateCatalogEntry(e); err != nil {
		t.Fatalf("expected valid tar.xz entry accepted, got %v", err)
	}
}

func TestValidateCatalogEntry_RejectsPendingEmptyMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"pending sentinel": PendingSHA256,
		"63 hex":           strings.Repeat("a", 63),
		"65 hex":           strings.Repeat("a", 65),
		"uppercase":        strings.ToUpper(pfSHA),
		"sha256 prefix":    "sha256:" + pfSHA,
		"non-hex":          strings.Repeat("g", 64),
		"leading space":    " " + pfSHA,
		"trailing space":   pfSHA + " ",
		"trailing newline": pfSHA + "\n",
	}
	for name, sum := range cases {
		t.Run(name, func(t *testing.T) {
			e := pfValidEntry()
			e.SHA256 = sum
			err := validateCatalogEntry(e)
			if err == nil {
				t.Fatalf("expected rejection for %q", sum)
			}
			if !errors.Is(err, ErrCatalogEntryUnverified) {
				t.Fatalf("expected ErrCatalogEntryUnverified, got %v", err)
			}
			if sum != "" && strings.Contains(err.Error(), sum) {
				t.Fatalf("error must not echo the untrusted value: %v", err)
			}
		})
	}
}

func TestValidateCatalogEntry_RejectsUnsafeURLAndPath(t *testing.T) {
	badURLs := map[string]string{
		"http":      "http://go.dev/dl/go.tar.gz",
		"empty":     "",
		"dquote":    `https://go.dev/dl/"go.tar.gz`,
		"squote":    "https://go.dev/dl/'go.tar.gz",
		"backtick":  "https://go.dev/dl/`id`.tar.gz",
		"dollar":    "https://go.dev/dl/$HOME.tar.gz",
		"newline":   "https://go.dev/dl/go.tar.gz\nRUN evil",
		"space":     "https://go.dev/dl/go .tar.gz",
		"semicolon": "https://go.dev/dl/go.tar.gz;rm",
		"tab":       "https://go.dev/dl/go.tar.gz\t",
	}
	for name, u := range badURLs {
		t.Run("url/"+name, func(t *testing.T) {
			e := pfValidEntry()
			e.ArchiveURL = u
			err := validateCatalogEntry(e)
			if err == nil {
				t.Fatalf("expected rejection for url %q", u)
			}
			if !errors.Is(err, ErrInvalidRuntimeProfile) {
				t.Fatalf("expected ErrInvalidRuntimeProfile, got %v", err)
			}
			if u != "" && strings.Contains(err.Error(), u) {
				t.Fatalf("error must not echo the url: %v", err)
			}
		})
	}
	badPaths := map[string]string{
		"root":      "/",
		"traversal": "/usr/local/../etc",
		"injection": "/usr/local/go;rm",
		"relative":  "usr/local/go",
		"empty":     "",
		"other dir": "/opt/go",
		"uppercase": "/usr/local/Go",
		"nested":    "/usr/local/go/x",
		"trailing":  "/usr/local/go\n",
	}
	for name, p := range badPaths {
		t.Run("path/"+name, func(t *testing.T) {
			e := pfValidEntry()
			e.ExtractPath = p
			err := validateCatalogEntry(e)
			if err == nil {
				t.Fatalf("expected rejection for path %q", p)
			}
			if !errors.Is(err, ErrInvalidRuntimeProfile) {
				t.Fatalf("expected ErrInvalidRuntimeProfile, got %v", err)
			}
		})
	}
}

func TestValidateCatalogEntry_ArchiveFormatMatchesURL(t *testing.T) {
	cases := []struct {
		name, url, format string
		ok                bool
	}{
		{"gz ok", "https://x.example/a.tar.gz", "tar.gz", true},
		{"xz ok", "https://x.example/a.tar.xz", "tar.xz", true},
		{"xz url gz format", "https://x.example/a.tar.xz", "tar.gz", false},
		{"gz url xz format", "https://x.example/a.tar.gz", "tar.xz", false},
		{"empty format", "https://x.example/a.tar.gz", "", false},
		{"unknown format", "https://x.example/a.zip", "zip", false},
		{"unknown suffix", "https://x.example/a.zip", "tar.gz", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := pfValidEntry()
			e.ArchiveURL = tc.url
			e.ArchiveFormat = tc.format
			err := validateCatalogEntry(e)
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatalf("expected rejection")
				}
				if !errors.Is(err, ErrInvalidRuntimeProfile) {
					t.Fatalf("expected ErrInvalidRuntimeProfile, got %v", err)
				}
			}
		})
	}
}

func TestDefaultCatalog_EntriesWellFormed(t *testing.T) {
	entries := NewDefaultRuntimeCatalog().List()
	if len(entries) == 0 {
		t.Fatal("expected default catalog entries")
	}
	for _, e := range entries {
		if !sha256HexRe.MatchString(e.SHA256) {
			t.Errorf("%s: shipped SHA256 must be 64 lowercase hex", e.Runtime)
		}
		if e.ArchiveFormat != "tar.gz" && e.ArchiveFormat != "tar.xz" {
			t.Errorf("%s: unexpected archive format %q", e.Runtime, e.ArchiveFormat)
		}
		if !strings.HasSuffix(e.ArchiveURL, "."+e.ArchiveFormat) {
			t.Errorf("%s: archive format %q does not match URL suffix", e.Runtime, e.ArchiveFormat)
		}
	}
}

func TestValidateProfile_AcceptsFullyVerifiedFixture(t *testing.T) {
	if err := validateProfile(pfValidProfile(), pfCatalogOf(pfValidEntry())); err != nil {
		t.Fatalf("expected accept, got %v", err)
	}
}

func TestValidateProfile_RejectsEmptyRuntimes(t *testing.T) {
	for name, rts := range map[string]map[string]string{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			p := pfValidProfile()
			p.Runtimes = rts
			err := validateProfile(p, pfCatalogOf(pfValidEntry()))
			if !errors.Is(err, ErrInvalidRuntimeProfile) {
				t.Fatalf("expected ErrInvalidRuntimeProfile, got %v", err)
			}
		})
	}
}

func TestValidateCatalogEntry_RejectsUnsafeRuntimeAndVersion(t *testing.T) {
	bad := map[string]string{
		"empty":         "",
		"newline":       "go\nRUN evil",
		"trailing nl":   "1.27.2\n",
		"space":         "1.27 2",
		"leading space": " go",
		"tab":           "go\t",
		"leading dot":   ".go",
		"leading dash":  "-go",
		"dollar":        "go$HOME",
		"semicolon":     "go;rm",
		"quote":         `go"`,
		"non-ascii":     "g\u00f6",
		"too long":      strings.Repeat("a", 65),
	}
	for name, v := range bad {
		t.Run("runtime/"+name, func(t *testing.T) {
			e := pfValidEntry()
			e.Runtime = v
			err := validateCatalogEntry(e)
			if !errors.Is(err, ErrInvalidRuntimeProfile) {
				t.Fatalf("expected ErrInvalidRuntimeProfile, got %v", err)
			}
			if v != "" && strings.Contains(err.Error(), v) {
				t.Fatalf("error must not echo the value: %v", err)
			}
		})
		t.Run("version/"+name, func(t *testing.T) {
			e := pfValidEntry()
			e.Version = v
			err := validateCatalogEntry(e)
			if !errors.Is(err, ErrInvalidRuntimeProfile) {
				t.Fatalf("expected ErrInvalidRuntimeProfile, got %v", err)
			}
		})
	}
	for _, v := range []string{"go", "1.27.2", "24.21.0", "1.0.0-rc.1", "3.11.17+build5", strings.Repeat("a", 64)} {
		e := pfValidEntry()
		e.Runtime, e.Version = v, v
		if err := validateCatalogEntry(e); err != nil {
			t.Errorf("expected %q accepted, got %v", v, err)
		}
	}
}

func TestValidateProfile_NilInputs(t *testing.T) {
	if err := validateProfile(nil, pfCatalogOf()); !errors.Is(err, ErrInvalidRuntimeProfile) {
		t.Fatalf("nil profile: got %v", err)
	}
	if err := validateProfile(pfValidProfile(), nil); !errors.Is(err, ErrInvalidRuntimeProfile) {
		t.Fatalf("nil catalog: got %v", err)
	}
}

func TestValidateProfile_RejectsUncatalogedRuntime(t *testing.T) {
	cat := pfCatalogOf(pfValidEntry())
	for name, rts := range map[string]map[string]string{
		"unknown runtime": {"ruby": "3.3.0"},
		"unknown version": {"go": "9.9.9"},
		"mixed":           {"go": "1.27.2", "ruby": "3.3.0"},
	} {
		t.Run(name, func(t *testing.T) {
			p := pfValidProfile()
			p.Runtimes = rts
			err := validateProfile(p, cat)
			if !errors.Is(err, ErrInvalidRuntimeProfile) {
				t.Fatalf("expected ErrInvalidRuntimeProfile, got %v", err)
			}
		})
	}
}

func TestValidateProfile_RejectsInvalidCatalogEntryOfRequestedRuntime(t *testing.T) {
	e := pfValidEntry()
	e.SHA256 = PendingSHA256
	err := validateProfile(pfValidProfile(), pfCatalogOf(e))
	if !errors.Is(err, ErrCatalogEntryUnverified) {
		t.Fatalf("expected ErrCatalogEntryUnverified, got %v", err)
	}
	e = pfValidEntry()
	e.SHA256 = ""
	err = validateProfile(pfValidProfile(), pfCatalogOf(e))
	if !errors.Is(err, ErrCatalogEntryUnverified) {
		t.Fatalf("empty sha must not skip verification, got %v", err)
	}
}

func TestValidateProfile_RejectsUnpinnedBaseAndDigest(t *testing.T) {
	mut := map[string]func(p *ResolvedRuntimeProfile){
		"tag only base":        func(p *ResolvedRuntimeProfile) { p.BaseImageRef = "alpine:3.24" },
		"pending base":         func(p *ResolvedRuntimeProfile) { p.BaseImageRef = "alpine:3.24@sha256:" + PendingSHA256 },
		"pending base nontag":  func(p *ResolvedRuntimeProfile) { p.BaseImageRef = "alpine@sha256:" + PendingSHA256 },
		"empty base":           func(p *ResolvedRuntimeProfile) { p.BaseImageRef = "" },
		"uppercase base":       func(p *ResolvedRuntimeProfile) { p.BaseImageRef = "alpine:3.24@sha256:" + strings.Repeat("AB", 32) },
		"short base digest":    func(p *ResolvedRuntimeProfile) { p.BaseImageRef = "alpine:3.24@sha256:" + strings.Repeat("ab", 31) },
		"base with newline":    func(p *ResolvedRuntimeProfile) { p.BaseImageRef = pfBase + "\nRUN x" },
		"base with space":      func(p *ResolvedRuntimeProfile) { p.BaseImageRef = "alpine :3@sha256:" + strings.Repeat("ab", 32) },
		"empty opencode":       func(p *ResolvedRuntimeProfile) { p.OpenCodeDigest = "" },
		"opencode no prefix":   func(p *ResolvedRuntimeProfile) { p.OpenCodeDigest = strings.Repeat("ef", 32) },
		"opencode uppercase":   func(p *ResolvedRuntimeProfile) { p.OpenCodeDigest = "sha256:" + strings.Repeat("EF", 32) },
		"opencode pending":     func(p *ResolvedRuntimeProfile) { p.OpenCodeDigest = "sha256:" + PendingSHA256 },
		"opencode trailing nl": func(p *ResolvedRuntimeProfile) { p.OpenCodeDigest += "\n" },
		"platform empty":       func(p *ResolvedRuntimeProfile) { p.Platform = "" },
		"platform arm64":       func(p *ResolvedRuntimeProfile) { p.Platform = "linux/arm64" },
		"platform windows":     func(p *ResolvedRuntimeProfile) { p.Platform = "windows/amd64" },
		"platform case":        func(p *ResolvedRuntimeProfile) { p.Platform = "Linux/amd64" },
	}
	cat := pfCatalogOf(pfValidEntry())
	for name, f := range mut {
		t.Run(name, func(t *testing.T) {
			p := pfValidProfile()
			f(p)
			err := validateProfile(p, cat)
			if !errors.Is(err, ErrInvalidRuntimeProfile) {
				t.Fatalf("expected ErrInvalidRuntimeProfile, got %v", err)
			}
		})
	}
}

func TestPreflightErrors_FixedTextAndSentinels(t *testing.T) {
	if ErrCatalogEntryUnverified == nil || ErrInvalidRuntimeProfile == nil {
		t.Fatal("sentinels must be defined")
	}
	if errors.Is(ErrCatalogEntryUnverified, ErrInvalidRuntimeProfile) {
		t.Fatal("sentinels must be distinct")
	}
	if ErrCatalogEntryUnverified.Error() == "" || ErrInvalidRuntimeProfile.Error() == "" {
		t.Fatal("sentinels need fixed message text")
	}
	// Hostile values must never appear in error text.
	p := pfValidProfile()
	p.BaseImageRef = "evil\x1b[31m$(id)"
	err := validateProfile(p, pfCatalogOf(pfValidEntry()))
	if err == nil || strings.Contains(err.Error(), "evil") || strings.ContainsAny(err.Error(), "\x1b\n") {
		t.Fatalf("error must not echo hostile value: %v", err)
	}
}

func TestPendingBaseImageRef_Rejected(t *testing.T) {
	p := pfValidProfile()
	p.BaseImageRef = "alpine:3.24@sha256:" + PendingSHA256
	if err := validateProfile(p, pfCatalogOf(pfValidEntry())); !errors.Is(err, ErrInvalidRuntimeProfile) {
		t.Fatalf("pending base must be refused, got %v", err)
	}
}
