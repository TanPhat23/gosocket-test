package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestInstallDependencies_NodeProject_NoCacheNote(t *testing.T) {
	tmpDir := t.TempDir()
	pkgJSON := `{"name":"test-pkg","version":"1.0.0"}`
	_ = os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(pkgJSON), 0644)

	var loggedMsg string
	err := runner.InstallDependencies(context.Background(), tmpDir, "", func(msg string) {
		loggedMsg = msg
	})
	if err != nil {
		t.Fatalf("InstallDependencies failed: %v", err)
	}
	if !strings.Contains(loggedMsg, "container sandbox") {
		t.Errorf("expected container sandbox note, got: %q", loggedMsg)
	}

	// Verify host node_modules was NOT created because host execution is removed
	if _, statErr := os.Stat(filepath.Join(tmpDir, "node_modules")); !os.IsNotExist(statErr) {
		t.Errorf("expected node_modules directory NOT to be created on host")
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

func TestInstallDependencies_NoHostExecution(t *testing.T) {
	// Put a fake npm/go in PATH that fails the test if called
	tmpBinDir := t.TempDir()
	trapScript := "#!/bin/sh\necho 'ERROR: HOST PACKAGE MANAGER EXECUTED' >&2\nexit 99\n"
	for _, bin := range []string{"npm", "go"} {
		p := filepath.Join(tmpBinDir, bin)
		if err := os.WriteFile(p, []byte(trapScript), 0755); err != nil {
			t.Fatalf("failed to write trap script: %v", err)
		}
	}
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpBinDir+":"+origPath)

	// Scenario A: package.json without pre-existing cache
	worktreeDirA := t.TempDir()
	_ = os.WriteFile(filepath.Join(worktreeDirA, "package.json"), []byte(`{"name":"worktree-a"}`), 0644)
	var logsA []string
	err := runner.InstallDependencies(context.Background(), worktreeDirA, "", func(msg string) {
		logsA = append(logsA, msg)
	})
	if err != nil {
		t.Fatalf("InstallDependencies failed on node project: %v", err)
	}

	foundContainerMsgA := false
	for _, l := range logsA {
		if strings.Contains(l, "inside the container") || strings.Contains(l, "container sandbox") {
			foundContainerMsgA = true
			break
		}
	}
	if !foundContainerMsgA {
		t.Errorf("expected log message indicating dependency installation will occur inside container sandbox, got: %v", logsA)
	}

	// Scenario B: go.mod project
	worktreeDirB := t.TempDir()
	_ = os.WriteFile(filepath.Join(worktreeDirB, "go.mod"), []byte("module worktree-b\ngo 1.27\n"), 0644)
	var logsB []string
	err = runner.InstallDependencies(context.Background(), worktreeDirB, "", func(msg string) {
		logsB = append(logsB, msg)
	})
	if err != nil {
		t.Fatalf("InstallDependencies failed on go project: %v", err)
	}
}
