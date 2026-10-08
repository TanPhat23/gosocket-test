package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"agent-task-manager/internal/runner"
)

func TestInstallDependencies_EmptyDir(t *testing.T) {
	err := runner.InstallDependencies(context.Background(), "", "", nil)
	if err != nil {
		t.Fatalf("expected nil error for empty dir: %v", err)
	}

	tmpDir := t.TempDir()
	err = runner.InstallDependencies(context.Background(), tmpDir, "", nil)
	if err != nil {
		t.Fatalf("expected nil error for dir without manifests: %v", err)
	}
}

func TestInstallDependencies_NodeProject_Fallback(t *testing.T) {
	tmpDir := t.TempDir()
	pkgJSON := `{"name":"test-pkg","version":"1.0.0"}`
	_ = os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(pkgJSON), 0644)

	logged := false
	err := runner.InstallDependencies(context.Background(), tmpDir, "", func(msg string) {
		logged = true
	})
	if err != nil {
		t.Fatalf("InstallDependencies failed: %v", err)
	}
	if !logged {
		t.Errorf("expected installation log message")
	}

	// Verify node_modules was created
	info, statErr := os.Stat(filepath.Join(tmpDir, "node_modules"))
	if statErr != nil || !info.IsDir() {
		t.Errorf("expected node_modules directory to be created")
	}
}

func TestInstallDependencies_PersistentCacheSymlink(t *testing.T) {
	baseDir := t.TempDir()
	worktreeDir := t.TempDir()

	// Create persistent node_modules in baseDir
	baseNodeModules := filepath.Join(baseDir, "node_modules")
	_ = os.MkdirAll(filepath.Join(baseNodeModules, "dummy-pkg"), 0755)

	// In worktree, place package.json
	_ = os.WriteFile(filepath.Join(worktreeDir, "package.json"), []byte(`{"name":"worktree-pkg"}`), 0644)

	logged := false
	err := runner.InstallDependencies(context.Background(), worktreeDir, baseDir, func(msg string) {
		logged = true
	})
	if err != nil {
		t.Fatalf("InstallDependencies failed: %v", err)
	}
	if !logged {
		t.Errorf("expected log message indicating cache reuse")
	}

	// Verify worktree node_modules is symlinked to baseDir
	worktreeNodeModules := filepath.Join(worktreeDir, "node_modules")
	lInfo, lErr := os.Lstat(worktreeNodeModules)
	if lErr != nil {
		t.Fatalf("expected worktree node_modules to exist: %v", lErr)
	}
	if lInfo.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected node_modules to be a symlink to persistent cache")
	}

	target, _ := os.Readlink(worktreeNodeModules)
	if target != baseNodeModules {
		t.Errorf("expected symlink target %s, got %s", baseNodeModules, target)
	}
}
