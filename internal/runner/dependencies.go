package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// InstallDependencies detects package manifests in worktreePath (package.json, go.mod)
// and ensures dependencies are installed. To avoid redundant re-downloading across tasks,
// it shares/caches persistent dependencies from baseRepoPath.
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
			} else {
				// Base repo doesn't have node_modules yet, install in base repo so future tasks can reuse it!
				if logFn != nil {
					logFn("\x1b[36m[Agent Control Plane]\x1b[0m Installing project dependencies to persistent cache (npm install)...\r\n")
				}
				installCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
				defer cancel()

				cmd := exec.CommandContext(installCtx, "npm", "install", "--no-audit", "--no-fund")
				cmd.Dir = baseRepoPath
				if out, err := cmd.CombinedOutput(); err != nil {
					if logFn != nil {
						logFn(fmt.Sprintf("\x1b[33m[Agent Control Plane] Warning: npm install in base repo exited with: %s\x1b[0m\r\n", string(out)))
					}
				} else {
					_ = os.MkdirAll(baseNodeModules, 0777)
					_ = exec.CommandContext(ctx, "chmod", "-R", "0777", baseNodeModules).Run()
					// Now symlink to worktree
					_ = os.Remove(nodeModulesPath)
					if symErr := os.Symlink(baseNodeModules, nodeModulesPath); symErr == nil {
						if logFn != nil {
							logFn("\x1b[32m✔ Project dependencies cached and attached to worktree.\x1b[0m\r\n")
						}
						return nil
					}
				}
			}
		}

		// Fallback: install directly in worktreePath
		if logFn != nil {
			logFn("\x1b[36m[Agent Control Plane]\x1b[0m Installing project dependencies in worktree (npm install)...\r\n")
		}

		installCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(installCtx, "npm", "install", "--no-audit", "--no-fund")
		cmd.Dir = worktreePath
		_ = cmd.Run()
		_ = os.MkdirAll(nodeModulesPath, 0777)
		_ = exec.CommandContext(ctx, "chmod", "-R", "0777", nodeModulesPath).Run()

		if logFn != nil {
			logFn("\x1b[32m✔ Project dependencies installed.\x1b[0m\r\n")
		}
	}

	// 2. Go project (go.mod)
	goModPath := filepath.Join(worktreePath, "go.mod")
	if _, err := os.Stat(goModPath); err == nil {
		if logFn != nil {
			logFn("\x1b[36m[Agent Control Plane]\x1b[0m Downloading Go module dependencies...\r\n")
		}
		goCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(goCtx, "go", "mod", "download")
		cmd.Dir = worktreePath
		_ = cmd.Run()
	}

	return nil
}
