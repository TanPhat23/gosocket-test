package runner

import (
	"context"
	"os"
	"path/filepath"
)

// InstallDependencies detects package manifests in worktreePath (package.json, go.mod)
// and reuses pre-cached dependencies from baseRepoPath via safe filesystem symlinks if available.
// In accordance with host execution eradication standards, it never spawns package managers on the host.
// If dependencies are not pre-cached, it logs that dependency installation will occur inside the container sandbox.
func InstallDependencies(ctx context.Context, worktreePath, baseRepoPath string, logFn func(string)) error {
	if worktreePath == "" {
		return nil
	}

	// 1. Node.js / TypeScript project (package.json)
	pkgJSONPath := filepath.Join(worktreePath, "package.json")
	if _, err := os.Stat(pkgJSONPath); err == nil {
		nodeModulesPath := filepath.Join(worktreePath, "node_modules")

		// If node_modules already exists and is non-empty, nothing to do
		if info, err := os.Stat(nodeModulesPath); err == nil && info.IsDir() {
			return nil
		}

		// Check if baseRepo has a persistent node_modules we can reuse instantly
		if baseRepoPath != "" && baseRepoPath != worktreePath {
			baseNodeModules := filepath.Join(baseRepoPath, "node_modules")
			if bInfo, bErr := os.Stat(baseNodeModules); bErr == nil && bInfo.IsDir() {
				// Symlink base repo node_modules into worktree
				_ = os.Remove(nodeModulesPath)
				if symErr := os.Symlink(baseNodeModules, nodeModulesPath); symErr == nil {
					if logFn != nil {
						logFn("\x1b[32m✔ Attached persistent dependencies from base repository cache.\x1b[0m\r\n")
					}
					return nil
				}
			}
		}

		if logFn != nil {
			logFn("\x1b[36m[Agent Control Plane]\x1b[0m Node.js dependencies not cached on host; installation will occur inside the container sandbox.\r\n")
		}
	}

	// 2. Go project (go.mod)
	goModPath := filepath.Join(worktreePath, "go.mod")
	if _, err := os.Stat(goModPath); err == nil {
		if logFn != nil {
			logFn("\x1b[36m[Agent Control Plane]\x1b[0m Go module dependencies will be resolved inside the container sandbox.\r\n")
		}
	}

	return nil
}
