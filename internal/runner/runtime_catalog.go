package runner

// PendingSHA256 marks a catalog entry whose checksum has not been independently verified.
// It fails ^[0-9a-f]{64}$ by construction, so preflight refuses builds that use it.
const PendingSHA256 = "PENDING-INDEPENDENT-VERIFICATION"

// DefaultBaseImageRef is intentionally not a valid digest pin until the base digest is verified.
const DefaultBaseImageRef = "alpine:3.24@sha256:" + PendingSHA256

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
			SHA256:        PendingSHA256,
			ExtractPath:   "/usr/local/go",
			ArchiveFormat: "tar.gz",
			IsBaseline:    true,
		},
		{
			Runtime:       "node",
			Version:       "24.21.0",
			ArchiveURL:    "https://nodejs.org/dist/v24.21.0/node-v24.21.0-linux-x64.tar.xz",
			SHA256:        PendingSHA256,
			ExtractPath:   "/usr/local/node",
			ArchiveFormat: "tar.xz",
			IsBaseline:    true,
		},
		{
			Runtime:       "python",
			Version:       "3.11.17",
			ArchiveURL:    "https://www.python.org/ftp/python/3.11.17/Python-3.11.17.tar.xz",
			SHA256:        PendingSHA256,
			ExtractPath:   "/usr/local/python",
			ArchiveFormat: "tar.xz",
			IsBaseline:    true,
		},
		{
			Runtime:       "java",
			Version:       "25.0.4.1+1",
			ArchiveURL:    "https://github.com/adoptium/temurin25-binaries/releases/download/jdk-25.0.4.1%2B1/OpenJDK25U-jdk_x64_linux_hotspot_25.0.4.1_1.tar.gz",
			SHA256:        PendingSHA256,
			ExtractPath:   "/usr/local/java",
			ArchiveFormat: "tar.gz",
			IsBaseline:    false,
		},
		{
			Runtime:       "rust",
			Version:       "1.99.0",
			ArchiveURL:    "https://static.rust-lang.org/dist/rust-1.99.0-x86_64-unknown-linux-gnu.tar.gz",
			SHA256:        PendingSHA256,
			ExtractPath:   "/usr/local/rust",
			ArchiveFormat: "tar.gz",
			IsBaseline:    false,
		},
		{
			Runtime:       "dotnet",
			Version:       "10.0.401",
			ArchiveURL:    "https://dotnetcli.azureedge.net/dotnet/Sdk/10.0.401/dotnet-sdk-10.0.401-linux-x64.tar.gz",
			SHA256:        PendingSHA256,
			ExtractPath:   "/usr/local/dotnet",
			ArchiveFormat: "tar.gz",
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
