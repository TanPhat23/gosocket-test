package runner

type RuntimeConfigMap map[string]string // runtime -> version string or "false"

type ResolvedRuntimeProfile struct {
	Platform       string            `json:"platform"`
	BaseImageRef   string            `json:"base_image_ref"`
	TemplateRev    string            `json:"template_rev"`
	Runtimes       map[string]string `json:"runtimes"` // "go", "node", "python", "java", "rust", "dotnet"
	OpenCodeDigest string            `json:"opencode_digest"`
}

type RuntimeCatalogEntry struct {
	Runtime     string `json:"runtime"`
	Version     string `json:"version"`
	ArchiveURL  string `json:"archive_url"`
	SHA256      string `json:"sha256"`
	ExtractPath string `json:"extract_path"`
	IsBaseline  bool   `json:"is_baseline"`

	// ArchiveFormat is the archive container ("tar.gz" or "tar.xz").
	ArchiveFormat string `json:"archive_format"`
}

type RuntimeCatalog interface {
	Get(runtime, version string) (*RuntimeCatalogEntry, bool)
	GetBaseline(runtime string) (string, bool)
	List() []RuntimeCatalogEntry
	ValidateVersion(runtime, version string) bool
}

// ImageIdentityScheme is the version tag of the canonical image identity document.
const ImageIdentityScheme = "atm-runtime-identity/v2"

// ImageIdentity is the canonical, versioned document hashed into the image cache tag.
// Field order is part of the canonical encoding; do not reorder.
type ImageIdentity struct {
	Scheme                   string              `json:"scheme"`
	Platform                 string              `json:"platform"`
	BaseImageRef             string              `json:"base_image_ref"`
	OpenCodeDigest           string              `json:"opencode_digest"`
	TemplateSHA256           string              `json:"template_sha256"`
	RenderedDockerfileSHA256 string              `json:"rendered_dockerfile_sha256"`
	Runtimes                 map[string]string   `json:"runtimes"`
	Toolchains               []ToolchainIdentity `json:"toolchains"`
}

// ToolchainIdentity captures every build-relevant input of one toolchain archive.
type ToolchainIdentity struct {
	Runtime       string `json:"runtime"`
	Version       string `json:"version"`
	ArchiveURL    string `json:"archive_url"`
	SHA256        string `json:"sha256"`
	ExtractPath   string `json:"extract_path"`
	ArchiveFormat string `json:"archive_format"`
}
