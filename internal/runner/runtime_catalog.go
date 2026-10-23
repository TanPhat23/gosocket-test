package runner

// PendingSHA256 marks a catalog entry whose checksum has not been independently verified.
// It fails ^[0-9a-f]{64}$ by construction, so preflight refuses builds that use it.
const PendingSHA256 = "PENDING-INDEPENDENT-VERIFICATION"

// DefaultBaseImageRef is the digest-pinned Alpine (musl) base image.
// The base digest and every toolchain URL and SHA256 below were approved by the human
// AG-2 approver on 2026-10-14. Change them only through the same approval.
const DefaultBaseImageRef = "alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6"

const DefaultOpenCodeDigest = "sha256:ea6c5d68d8d230923d2627bc793e7fa6a03f8e47c253b50397eb92bbde6db255"

type defaultRuntimeCatalog struct {
	entries   map[string]map[string]RuntimeCatalogEntry // runtime -> version -> entry
	baselines map[string]string                         // runtime -> baseline version (or "false")
	list      []RuntimeCatalogEntry
}

// NewDefaultRuntimeCatalog constructs the pinned default RuntimeCatalog with approved versions:
// go: 1.27.2 (baseline: true)
// node: 24.21.0 (baseline: true)
// python: 3.11.17 (baseline: true)
// java: 25.0.4.1+1 (baseline: false, default "false")
// rust: 1.99.0 (baseline: false, default "false")
// dotnet: 10.0.401 (baseline: false, default "false")
func NewDefaultRuntimeCatalog() RuntimeCatalog {
	entries := []RuntimeCatalogEntry{
		{
			Runtime:       "go",
			Version:       "1.27.2",
			ArchiveURL:    "https://go.dev/dl/go1.27.2.linux-amd64.tar.gz",
			SHA256:        "ecbadb99091a3f46e31f5f934b068b1864eafa7995211b39eaddf76996045fe5",
			ExtractPath:   "/usr/local/go",
			ArchiveFormat: "tar.gz",
			InstallLayout: "bin-dir",
			IsBaseline:    true,
		},
		{
			Runtime:       "node",
			Version:       "24.21.0",
			ArchiveURL:    "https://unofficial-builds.nodejs.org/download/release/v24.21.0/node-v24.21.0-linux-x64-musl.tar.xz",
			SHA256:        "dd5ca647e656c8200c873f45098a16623129c47f2387829f0a15a6de1dac1220",
			ExtractPath:   "/usr/local/node",
			ArchiveFormat: "tar.xz",
			InstallLayout: "bin-dir",
			IsBaseline:    true,
		},
		{
			Runtime:       "python",
			Version:       "3.11.17",
			ArchiveURL:    "https://github.com/astral-sh/python-build-standalone/releases/download/20261013/cpython-3.11.17%2B20261013-x86_64-unknown-linux-musl-install_only.tar.gz",
			SHA256:        "51160d0cad601f104c36b3c24fb3498f2cee4ca0134bb75fb9ef150aebf66c81",
			ExtractPath:   "/usr/local/python",
			ArchiveFormat: "tar.gz",
			InstallLayout: "bin-dir",
			IsBaseline:    true,
		},
		{
			Runtime:       "java",
			Version:       "25.0.4.1+1",
			ArchiveURL:    "https://github.com/adoptium/temurin25-binaries/releases/download/jdk-25.0.4.1%2B1/OpenJDK25U-jdk_x64_alpine-linux_hotspot_25.0.4.1_1.tar.gz",
			SHA256:        "f18648ee5ce45261f50dc9493fdae7ddebaa2a7a857cafda3e2a448a79c03dee",
			ExtractPath:   "/usr/local/java",
			ArchiveFormat: "tar.gz",
			InstallLayout: "bin-dir",
			IsBaseline:    false,
		},
		{
			Runtime:       "rust",
			Version:       "1.99.0",
			ArchiveURL:    "https://static.rust-lang.org/dist/rust-1.99.0-x86_64-unknown-linux-musl.tar.gz",
			SHA256:        "c812c6f52447721fe64cf0feb516769cf758475548a0da25c5b76b65613050a9",
			ExtractPath:   "/usr/local/rust",
			ArchiveFormat: "tar.gz",
			InstallLayout: "rust-installer",
			IsBaseline:    false,
		},
		{
			Runtime:       "dotnet",
			Version:       "10.0.401",
			ArchiveURL:    "https://builds.dotnet.microsoft.com/dotnet/Sdk/10.0.401/dotnet-sdk-10.0.401-linux-musl-x64.tar.gz",
			SHA256:        "17db2731521b0d1bda9a26656433edf6c586f7520e2c171089013dba09833e0f",
			ExtractPath:   "/usr/local/dotnet",
			ArchiveFormat: "tar.gz",
			InstallLayout: "dotnet-root",
			IsBaseline:    false,
		},
	}

	entryMap := make(map[string]map[string]RuntimeCatalogEntry)
	for _, e := range entries {
		if entryMap[e.Runtime] == nil {
			entryMap[e.Runtime] = make(map[string]RuntimeCatalogEntry)
		}
		entryMap[e.Runtime][e.Version] = e
	}

	baselines := map[string]string{
		"go":     "1.27.2",
		"node":   "24.21.0",
		"python": "3.11.17",
		"java":   "false",
		"rust":   "false",
		"dotnet": "false",
	}

	return &defaultRuntimeCatalog{
		entries:   entryMap,
		baselines: baselines,
		list:      entries,
	}
}

func (c *defaultRuntimeCatalog) Get(runtime, version string) (*RuntimeCatalogEntry, bool) {
	verMap, ok := c.entries[runtime]
	if !ok {
		return nil, false
	}
	entry, ok := verMap[version]
	if !ok {
		return nil, false
	}
	return &entry, true
}

func (c *defaultRuntimeCatalog) GetBaseline(runtime string) (string, bool) {
	val, ok := c.baselines[runtime]
	return val, ok
}

func (c *defaultRuntimeCatalog) List() []RuntimeCatalogEntry {
	result := make([]RuntimeCatalogEntry, len(c.list))
	copy(result, c.list)
	return result
}

func (c *defaultRuntimeCatalog) ValidateVersion(runtime, version string) bool {
	// A runtime is only valid if it's one of the known catalog runtimes.
	if _, ok := c.baselines[runtime]; !ok {
		return false
	}
	// "false" is a valid override value for any recognized runtime.
	if version == "false" {
		return true
	}
	// Otherwise, must match an exact version registered for this runtime.
	_, ok := c.Get(runtime, version)
	return ok
}
