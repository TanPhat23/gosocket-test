package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runDepsScript(t *testing.T, script string, args ...string) string {
	t.Helper()
	out, err := exec.Command("sh", append([]string{"-c", script, "sh"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeTaskDeps(t *testing.T, worktree, lock string) {
	t.Helper()
	pkg := filepath.Join(worktree, "node_modules", "left-pad")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, body := range map[string]string{filepath.Join(worktree, "package-lock.json"): lock, filepath.Join(pkg, "index.js"): "module.exports = 1"} {
		if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDepsCache_SecondTaskSharesTheFirstTasksNodeModules(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "p1", "tasks", "t1")
	second := filepath.Join(root, "p1", "tasks", "t2")
	cache := depsCacheDir(first)
	writeTaskDeps(t, first, `{"lock":1}`)
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "package-lock.json"), []byte(`{"lock":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if key := runDepsScript(t, seedDepsScript, second, cache); key != "" {
		t.Fatalf("seeded %q from an empty cache", key)
	}
	if key := runDepsScript(t, publishDepsScript, first, cache, "3"); key == "" {
		t.Fatal("first task did not publish its node_modules")
	}
	if key := runDepsScript(t, seedDepsScript, second, cache); key == "" {
		t.Fatal("second task was not seeded from the cache")
	}

	a, errA := os.Stat(filepath.Join(first, "node_modules", "left-pad", "index.js"))
	b, errB := os.Stat(filepath.Join(second, "node_modules", "left-pad", "index.js"))
	if errA != nil || errB != nil || !os.SameFile(a, b) {
		t.Fatalf("files are not shared on disk: %v %v", errA, errB)
	}
}

func TestDepsCache_DifferentLockfileIsNotSeededAndOldEntriesArePruned(t *testing.T) {
	root := t.TempDir()
	cache := depsCacheDir(filepath.Join(root, "p1", "tasks", "t0"))
	for i, lock := range []string{`{"a":1}`, `{"b":2}`, `{"c":3}`} {
		wt := filepath.Join(root, "p1", "tasks", "t"+string(rune('1'+i)))
		writeTaskDeps(t, wt, lock)
		runDepsScript(t, publishDepsScript, wt, cache, "2")
	}
	entries, err := os.ReadDir(cache)
	if err != nil || len(entries) != 2 {
		t.Fatalf("cache entries = %d (%v), want the newest 2", len(entries), err)
	}

	other := filepath.Join(root, "p1", "tasks", "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "package-lock.json"), []byte(`{"z":9}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if key := runDepsScript(t, seedDepsScript, other, cache); key != "" {
		t.Fatalf("seeded %q for a lockfile with no cache entry", key)
	}
	if _, err := os.Stat(filepath.Join(other, "node_modules")); err == nil {
		t.Fatal("node_modules appeared without a matching cache entry")
	}
}
