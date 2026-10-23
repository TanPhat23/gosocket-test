package runner

import (
	"context"
	"fmt"

	"agent-task-manager/internal/models"
)

type RepositoryManifest struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Runtimes      map[string]interface{} `json:"runtimes"` // string version or bool false
}

type RuntimeResolver interface {
	ResolveMultiRepo(ctx context.Context, manifests map[string]*RepositoryManifest, project *models.Project) (*ResolvedRuntimeProfile, error)
}

type runtimeResolver struct {
	catalog RuntimeCatalog
}

func NewRuntimeResolver(catalog RuntimeCatalog) RuntimeResolver {
	return &runtimeResolver{
		catalog: catalog,
	}
}

func (r *runtimeResolver) ResolveMultiRepo(ctx context.Context, manifests map[string]*RepositoryManifest, project *models.Project) (*ResolvedRuntimeProfile, error) {
	// 1. Validate all manifests
	for _, m := range manifests {
		if m == nil {
			continue
		}
		if m.SchemaVersion != 1 {
			return nil, fmt.Errorf("unsupported schema version: %d (expected 1)", m.SchemaVersion)
		}
		for rt, val := range m.Runtimes {
			// Validate runtime name against catalog
			if _, ok := r.catalog.GetBaseline(rt); !ok {
				return nil, fmt.Errorf("unknown runtime: %s", rt)
			}

			// Validate value: bool false or string
			switch v := val.(type) {
			case bool:
				if v { // true is invalid
					return nil, fmt.Errorf("invalid runtime value for %s", rt)
				}
			case string:
				if v != "false" && !r.catalog.ValidateVersion(rt, v) {
					return nil, fmt.Errorf("uncataloged version %q for runtime %s", v, rt)
				}
			default:
				return nil, fmt.Errorf("invalid runtime value for %s", rt)
			}
		}
	}

	// 2. Validate project UI/DB overrides
	var projectOverrides map[string]string
	if project != nil && project.RuntimeConfig != nil {
		projectOverrides = make(map[string]string)
		for rt, rawVal := range project.RuntimeConfig {
			if _, ok := r.catalog.GetBaseline(rt); !ok {
				return nil, fmt.Errorf("unknown runtime: %s", rt)
			}
			switch v := rawVal.(type) {
			case bool:
				if v {
					return nil, fmt.Errorf("invalid runtime value for %s", rt)
				}
				projectOverrides[rt] = "false"
			case string:
				if v != "false" && !r.catalog.ValidateVersion(rt, v) {
					return nil, fmt.Errorf("uncataloged version %q for runtime %s", v, rt)
				}
				projectOverrides[rt] = v
			default:
				return nil, fmt.Errorf("invalid runtime value for %s", rt)
			}
		}
	}

	// 3. Multi-repo union logic
	// Start with baseline runtimes
	// baseline runtimes: go: 1.27.2, node: 24.21.0, python: 3.11.17, java: false, rust: false, dotnet: false
	workingRuntimes := make(map[string]string)
	for _, entry := range r.catalog.List() {
		if base, ok := r.catalog.GetBaseline(entry.Runtime); ok {
			workingRuntimes[entry.Runtime] = base
		}
	}

	// Aggregate repo requests
	// For each runtime: collect repo requirements
	// If a repo requests a valid version (e.g. rust: "1.99.0"), and another repo specifies rust: false, positive requirement wins.
	// If two repos request different positive versions for the same runtime (go: 1.27.2 vs go: 1.26.0), fail closed immediately with conflict UNLESS project override exists.
	repoVersions := make(map[string]string) // runtime -> active positive version requested by repos

	for _, m := range manifests {
		if m == nil {
			continue
		}
		for rt, val := range m.Runtimes {
			var verStr string
			switch v := val.(type) {
			case bool:
				if !v {
					verStr = "false"
				}
			case string:
				verStr = v
			}

			if verStr == "false" {
				// repo specifies false. If we don't already have an entry or positive version, we can note it,
				// but positive version wins over false.
				if _, ok := repoVersions[rt]; !ok {
					repoVersions[rt] = "false"
				}
			} else {
				// positive requirement
				current, ok := repoVersions[rt]
				if !ok || current == "false" {
					repoVersions[rt] = verStr
				} else if current != verStr {
					// Check if explicit project override exists for this runtime
					if projectOverrides != nil {
						if _, hasOverride := projectOverrides[rt]; hasOverride {
							// Project override resolves conflict
							continue
						}
					}
					return nil, fmt.Errorf("runtime conflict for %s: %s vs %s", rt, current, verStr)
				}
			}
		}
	}

	// Apply repo versions to working runtimes
	for rt, ver := range repoVersions {
		if ver != "false" {
			workingRuntimes[rt] = ver
		} else {
			// If all repos that specified rt specified "false", and no repo requested positive version:
			// does repo false disable baseline if not requested positive?
			// "For each repo manifest:
			//   - If a repo requests a valid version (e.g. rust: "1.99.0"), and another repo specifies rust: false, the positive requirement wins.
			//   - If two repos request different positive versions..."
			// Notice: if repo specifies "false" for baseline, or is baseline overridden?
			// Wait, if repo specifies "false" and no project override:
			workingRuntimes[rt] = "false"
		}
	}

	// Apply project UI/DB overrides
	if projectOverrides != nil {
		for rt, ver := range projectOverrides {
			workingRuntimes[rt] = ver
		}
	}

	// Final active versions: exclude runtimes whose value is "false"
	finalRuntimes := make(map[string]string)
	for rt, ver := range workingRuntimes {
		if ver != "" && ver != "false" {
			finalRuntimes[rt] = ver
		}
	}

	profile := &ResolvedRuntimeProfile{
		Platform:       "linux/amd64",
		BaseImageRef:   DefaultBaseImageRef,
		TemplateRev:    "v1",
		OpenCodeDigest: DefaultOpenCodeDigest,
		Runtimes:       finalRuntimes,
	}

	return profile, nil
}
