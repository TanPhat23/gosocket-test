package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ComputeImageIdentity builds the canonical identity document for a runtime image and
// returns it together with its cache tag ("atm-runtime:" + hex sha256 of the canonical JSON).
//
// Both the raw template bytes and the rendered Dockerfile bytes are required, so a partial
// tag cannot be computed. Every requested runtime must exist in the catalog (no fallback).
// This function performs no validation of checksum or reference formats; that is preflight's job.
func ComputeImageIdentity(profile *ResolvedRuntimeProfile, catalog RuntimeCatalog, templateBytes, rendered []byte) (ImageIdentity, string, error) {
	if profile == nil {
		return ImageIdentity{}, "", errors.New("nil runtime profile")
	}
	if catalog == nil {
		return ImageIdentity{}, "", errors.New("nil runtime catalog")
	}
	if len(templateBytes) == 0 {
		return ImageIdentity{}, "", errors.New("empty template bytes")
	}
	if len(rendered) == 0 {
		return ImageIdentity{}, "", errors.New("empty rendered dockerfile bytes")
	}

	names := make([]string, 0, len(profile.Runtimes))
	for name := range profile.Runtimes {
		names = append(names, name)
	}
	sort.Strings(names)

	runtimes := make(map[string]string, len(profile.Runtimes))
	toolchains := make([]ToolchainIdentity, 0, len(names))
	for _, name := range names {
		ver := profile.Runtimes[name]
		entry, ok := catalog.Get(name, ver)
		if !ok || entry == nil {
			return ImageIdentity{}, "", fmt.Errorf("runtime %q version %q is not in the catalog", name, ver)
		}
		runtimes[name] = ver
		toolchains = append(toolchains, ToolchainIdentity{
			Runtime:       entry.Runtime,
			Version:       entry.Version,
			ArchiveURL:    entry.ArchiveURL,
			SHA256:        entry.SHA256,
			ExtractPath:   entry.ExtractPath,
			ArchiveFormat: entry.ArchiveFormat,
		})
	}

	tplSum := sha256.Sum256(templateBytes)
	renSum := sha256.Sum256(rendered)
	id := ImageIdentity{
		Scheme:                   ImageIdentityScheme,
		Platform:                 profile.Platform,
		BaseImageRef:             profile.BaseImageRef,
		OpenCodeDigest:           profile.OpenCodeDigest,
		TemplateSHA256:           hex.EncodeToString(tplSum[:]),
		RenderedDockerfileSHA256: hex.EncodeToString(renSum[:]),
		Runtimes:                 runtimes,
		Toolchains:               toolchains,
	}

	canonical, err := json.Marshal(id)
	if err != nil {
		return ImageIdentity{}, "", fmt.Errorf("failed to marshal image identity: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return id, "atm-runtime:" + hex.EncodeToString(sum[:]), nil
}
