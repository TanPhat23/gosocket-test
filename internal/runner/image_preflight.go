package runner

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Preflight errors have fixed text. Callers match them with errors.Is. Messages never echo
// untrusted values; only a fixed field name from this package is appended.
var (
	ErrCatalogEntryUnverified = errors.New("runtime catalog entry is not verified")
	ErrInvalidRuntimeProfile  = errors.New("runtime profile is invalid")
	// ErrImageIdentityMismatch is returned when an image already carrying the expected tag
	// cannot be proven to be the image built for that identity. It is never retried or rebuilt over.
	ErrImageIdentityMismatch = errors.New("cached runtime image identity mismatch")
)

var (
	sha256HexRe      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	extractPathRe    = regexp.MustCompile(`^/usr/local/[a-z]+$`)
	baseImageRefRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9./_-]*(:[A-Za-z0-9._-]+)?@sha256:[0-9a-f]{64}$`)
	openCodeDigestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	toolchainTokenRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
)

// maxToolchainTokenLen bounds a catalog runtime name or version.
const maxToolchainTokenLen = 64

// requiredPlatform is the only platform the image build supports.
const requiredPlatform = "linux/amd64"

func unverified(field string) error {
	return fmt.Errorf("%w: field %s", ErrCatalogEntryUnverified, field)
}

func invalid(field string) error {
	return fmt.Errorf("%w: field %s", ErrInvalidRuntimeProfile, field)
}

// validateCatalogEntry fails closed on a pending, empty or malformed checksum and on any
// URL, extract path, archive format, install layout, runtime name or version that could be unsafe to
// interpolate into a Dockerfile.
func validateCatalogEntry(entry RuntimeCatalogEntry) error {
	if !isSafeToolchainToken(entry.Runtime) {
		return invalid("runtime")
	}
	if !isSafeToolchainToken(entry.Version) {
		return invalid("version")
	}
	if !sha256HexRe.MatchString(entry.SHA256) {
		return unverified("sha256")
	}
	url := entry.ArchiveURL
	if !strings.HasPrefix(url, "https://") || len(url) == len("https://") ||
		strings.ContainsAny(url, "\"'`$;\\") || strings.IndexFunc(url, isUnsafeURLRune) >= 0 {
		return invalid("archive_url")
	}
	if !extractPathRe.MatchString(entry.ExtractPath) {
		return invalid("extract_path")
	}
	if entry.ArchiveFormat != "tar.gz" && entry.ArchiveFormat != "tar.xz" {
		return invalid("archive_format")
	}
	if !strings.HasSuffix(url, "."+entry.ArchiveFormat) {
		return invalid("archive_format")
	}
	if !isKnownInstallLayout(entry.InstallLayout) {
		return invalid("install_layout")
	}
	return nil
}

// Install layouts are a closed set; each selects one fixed branch of the Dockerfile template.
const (
	installLayoutBinDir        = "bin-dir"
	installLayoutRustInstaller = "rust-installer"
	installLayoutDotnetRoot    = "dotnet-root"
)

// isKnownInstallLayout reports whether s is exactly one of the supported install layouts.
// There is no default: an empty layout is rejected.
func isKnownInstallLayout(s string) bool {
	switch s {
	case installLayoutBinDir, installLayoutRustInstaller, installLayoutDotnetRoot:
		return true
	}
	return false
}

// isSafeToolchainToken reports whether s is a bounded token with no whitespace or shell
// metacharacters, so it cannot break out of the template's "# Toolchain:" comment line.
func isSafeToolchainToken(s string) bool {
	return len(s) <= maxToolchainTokenLen && toolchainTokenRe.MatchString(s)
}

// isUnsafeURLRune reports whitespace and control characters.
func isUnsafeURLRune(r rune) bool {
	return r <= 0x20 || r == 0x7f || r == 0x85 || r == 0xa0 || r == 0x2028 || r == 0x2029
}

// validateProfile checks the platform, the digest-pinned base image and OpenCode digest, and
// requires at least one runtime, each of which must exist in the catalog (no fallback) and
// pass validateCatalogEntry. It performs no Docker calls.
func validateProfile(profile *ResolvedRuntimeProfile, catalog RuntimeCatalog) error {
	if profile == nil {
		return invalid("profile")
	}
	if catalog == nil {
		return invalid("catalog")
	}
	if profile.Platform != requiredPlatform {
		return invalid("platform")
	}
	if !baseImageRefRe.MatchString(profile.BaseImageRef) {
		return invalid("base_image_ref")
	}
	if !openCodeDigestRe.MatchString(profile.OpenCodeDigest) {
		return invalid("opencode_digest")
	}
	if len(profile.Runtimes) == 0 {
		return invalid("runtimes")
	}
	for name, version := range profile.Runtimes {
		entry, ok := catalog.Get(name, version)
		if !ok || entry == nil {
			return invalid("runtimes")
		}
		if err := validateCatalogEntry(*entry); err != nil {
			return err
		}
	}
	return nil
}
