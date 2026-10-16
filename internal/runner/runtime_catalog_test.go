package runner_test

import (
	"testing"

	"agent-task-manager/internal/runner"
)

func TestRuntimeCatalog(t *testing.T) {
	catalog := runner.NewDefaultRuntimeCatalog()
	if catalog == nil {
		t.Fatalf("expected non-nil default runtime catalog")
	}

	t.Run("DefaultOpenCodeDigest constant", func(t *testing.T) {
		const expectedDigest = "sha256:ea6c5d68d8d230923d2627bc793e7fa6a03f8e47c253b50397eb92bbde6db255"
		if runner.DefaultOpenCodeDigest != expectedDigest {
			t.Errorf("expected DefaultOpenCodeDigest=%s, got %s", expectedDigest, runner.DefaultOpenCodeDigest)
		}
	})

	t.Run("Baseline versions", func(t *testing.T) {
		expectedBaselines := map[string]string{
			"go":     "1.27.2",
			"node":   "24.21.0",
			"python": "3.11.17",
			"java":   "false",
			"rust":   "false",
			"dotnet": "false",
		}

		for rt, expectedVer := range expectedBaselines {
			ver, ok := catalog.GetBaseline(rt)
			if !ok {
				t.Errorf("expected runtime %s to have baseline", rt)
			}
			if ver != expectedVer {
				t.Errorf("expected runtime %s baseline to be %s, got %s", rt, expectedVer, ver)
			}
		}

		// Unknown runtime baseline
		if _, ok := catalog.GetBaseline("ruby"); ok {
			t.Errorf("expected unknown runtime ruby to not have baseline")
		}
	})

	t.Run("Catalog entries retrieval", func(t *testing.T) {
		expectedVersions := map[string]struct {
			version    string
			isBaseline bool
		}{
			"go":     {version: "1.27.2", isBaseline: true},
			"node":   {version: "24.21.0", isBaseline: true},
			"python": {version: "3.11.17", isBaseline: true},
			"java":   {version: "25.0.4.1+1", isBaseline: false},
			"rust":   {version: "1.99.0", isBaseline: false},
			"dotnet": {version: "10.0.401", isBaseline: false},
		}

		for rt, exp := range expectedVersions {
			entry, ok := catalog.Get(rt, exp.version)
			if !ok || entry == nil {
				t.Fatalf("expected entry for %s@%s to exist", rt, exp.version)
			}
			if entry.Runtime != rt {
				t.Errorf("expected entry runtime %s, got %s", rt, entry.Runtime)
			}
			if entry.Version != exp.version {
				t.Errorf("expected entry version %s, got %s", exp.version, entry.Version)
			}
			if entry.IsBaseline != exp.isBaseline {
				t.Errorf("expected entry %s isBaseline=%v, got %v", rt, exp.isBaseline, entry.IsBaseline)
			}
		}

		// Non-existent versions
		if entry, ok := catalog.Get("go", "1.20.0"); ok || entry != nil {
			t.Errorf("expected go@1.20.0 to not exist")
		}
		if entry, ok := catalog.Get("unknown", "1.0.0"); ok || entry != nil {
			t.Errorf("expected unknown@1.0.0 to not exist")
		}
	})

	t.Run("List all catalog entries", func(t *testing.T) {
		entries := catalog.List()
		if len(entries) != 6 {
			t.Fatalf("expected 6 catalog entries, got %d", len(entries))
		}
	})

	t.Run("ValidateVersion", func(t *testing.T) {
		validCases := []struct {
			runtime string
			version string
		}{
			{"go", "1.27.2"},
			{"go", "false"},
			{"node", "24.21.0"},
			{"node", "false"},
			{"python", "3.11.17"},
			{"python", "false"},
			{"java", "25.0.4.1+1"},
			{"java", "false"},
			{"rust", "1.99.0"},
			{"rust", "false"},
			{"dotnet", "10.0.401"},
			{"dotnet", "false"},
		}

		for _, tc := range validCases {
			if !catalog.ValidateVersion(tc.runtime, tc.version) {
				t.Errorf("expected runtime %s with version %s to be valid", tc.runtime, tc.version)
			}
		}

		invalidCases := []struct {
			runtime string
			version string
		}{
			{"go", "1.26.0"},
			{"go", "latest"},
			{"go", ""},
			{"ruby", "3.2.0"},
			{"ruby", "false"},
			{"java", "17"},
			{"unknown", "false"},
		}

		for _, tc := range invalidCases {
			if catalog.ValidateVersion(tc.runtime, tc.version) {
				t.Errorf("expected runtime %s with version %s to be invalid", tc.runtime, tc.version)
			}
		}
	})
}
