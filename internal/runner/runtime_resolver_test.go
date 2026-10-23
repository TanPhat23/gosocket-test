package runner_test

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/runner"
)

// extendedCatalog allows testing multiple valid versions for conflict isolation.
type extendedCatalog struct {
	runner.RuntimeCatalog
	extraVersions map[string]map[string]bool
}

func newExtendedCatalog(base runner.RuntimeCatalog, extra map[string][]string) runner.RuntimeCatalog {
	ev := make(map[string]map[string]bool)
	for rt, vers := range extra {
		ev[rt] = make(map[string]bool)
		for _, v := range vers {
			ev[rt][v] = true
		}
	}
	return &extendedCatalog{
		RuntimeCatalog: base,
		extraVersions:  ev,
	}
}

func (e *extendedCatalog) ValidateVersion(runtime, version string) bool {
	if e.RuntimeCatalog.ValidateVersion(runtime, version) {
		return true
	}
	if vers, ok := e.extraVersions[runtime]; ok {
		return vers[version]
	}
	return false
}

func TestRuntimeResolver(t *testing.T) {
	catalog := runner.NewDefaultRuntimeCatalog()
	resolver := runner.NewRuntimeResolver(catalog)

	t.Run("1. Single repo with no manifest resolves to catalog baseline", func(t *testing.T) {
		project := &models.Project{
			RuntimeConfig: models.JSONMap{},
		}
		manifests := map[string]*runner.RepositoryManifest{
			"repo-a": nil,
		}

		profile, err := resolver.ResolveMultiRepo(context.Background(), manifests, project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if profile == nil {
			t.Fatalf("expected non-nil profile")
		}

		if profile.Platform != "linux/amd64" {
			t.Errorf("expected platform linux/amd64, got %s", profile.Platform)
		}
		if profile.BaseImageRef != runner.DefaultBaseImageRef {
			t.Errorf("expected base image %s, got %s", runner.DefaultBaseImageRef, profile.BaseImageRef)
		}
		if profile.TemplateRev != "v1" {
			t.Errorf("expected template rev v1, got %s", profile.TemplateRev)
		}
		if profile.OpenCodeDigest != runner.DefaultOpenCodeDigest {
			t.Errorf("expected opencode digest %s, got %s", runner.DefaultOpenCodeDigest, profile.OpenCodeDigest)
		}

		expectedRuntimes := map[string]string{
			"go":     "1.27.2",
			"node":   "24.21.0",
			"python": "3.11.17",
		}
		if !reflect.DeepEqual(profile.Runtimes, expectedRuntimes) {
			t.Errorf("expected runtimes %v, got %v", expectedRuntimes, profile.Runtimes)
		}
	})

	t.Run("2. Multi-repo union: Repo A specifies rust: 1.99.0, Repo B specifies rust: false; resolved profile includes rust: 1.99.0", func(t *testing.T) {
		project := &models.Project{
			RuntimeConfig: models.JSONMap{},
		}
		manifests := map[string]*runner.RepositoryManifest{
			"repo-a": {
				SchemaVersion: 1,
				Runtimes: map[string]interface{}{
					"rust": "1.99.0",
				},
			},
			"repo-b": {
				SchemaVersion: 1,
				Runtimes: map[string]interface{}{
					"rust": false,
				},
			},
		}

		profile, err := resolver.ResolveMultiRepo(context.Background(), manifests, project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if profile == nil {
			t.Fatalf("expected non-nil profile")
		}

		if profile.Runtimes["rust"] != "1.99.0" {
			t.Errorf("expected rust version 1.99.0, got %s", profile.Runtimes["rust"])
		}
		if profile.Runtimes["go"] != "1.27.2" {
			t.Errorf("expected go version 1.27.2, got %s", profile.Runtimes["go"])
		}
		if profile.Runtimes["node"] != "24.21.0" {
			t.Errorf("expected node version 24.21.0, got %s", profile.Runtimes["node"])
		}
		if profile.Runtimes["python"] != "3.11.17" {
			t.Errorf("expected python version 3.11.17, got %s", profile.Runtimes["python"])
		}
	})

	t.Run("3. Multi-repo conflict: With custom/extended catalog test fixture having go: 1.26.0 and go: 1.27.2, Repo A specifies go: 1.27.2 and Repo B specifies go: 1.26.0; resolver fails closed with conflict error", func(t *testing.T) {
		extCat := newExtendedCatalog(catalog, map[string][]string{
			"go": {"1.26.0"},
		})
		extResolver := runner.NewRuntimeResolver(extCat)

		project := &models.Project{
			RuntimeConfig: models.JSONMap{},
		}
		manifests := map[string]*runner.RepositoryManifest{
			"repo-a": {
				SchemaVersion: 1,
				Runtimes: map[string]interface{}{
					"go": "1.27.2",
				},
			},
			"repo-b": {
				SchemaVersion: 1,
				Runtimes: map[string]interface{}{
					"go": "1.26.0",
				},
			},
		}

		profile, err := extResolver.ResolveMultiRepo(context.Background(), manifests, project)
		if err == nil {
			t.Fatalf("expected conflict error, got nil")
		}
		if profile != nil {
			t.Fatalf("expected nil profile on conflict, got %v", profile)
		}
		if !strings.Contains(err.Error(), "runtime conflict for go: 1.27.2 vs 1.26.0") &&
			!strings.Contains(err.Error(), "runtime conflict for go: 1.26.0 vs 1.27.2") {
			t.Errorf("expected conflict error for go, got: %v", err)
		}
	})

	t.Run("4. Project UI/DB override resolves multi-repo conflict: project override go: 1.27.2 succeeds", func(t *testing.T) {
		extCat := newExtendedCatalog(catalog, map[string][]string{
			"go": {"1.26.0"},
		})
		extResolver := runner.NewRuntimeResolver(extCat)

		project := &models.Project{
			RuntimeConfig: models.JSONMap{
				"go": "1.27.2",
			},
		}
		manifests := map[string]*runner.RepositoryManifest{
			"repo-a": {
				SchemaVersion: 1,
				Runtimes: map[string]interface{}{
					"go": "1.27.2",
				},
			},
			"repo-b": {
				SchemaVersion: 1,
				Runtimes: map[string]interface{}{
					"go": "1.26.0",
				},
			},
		}

		profile, err := extResolver.ResolveMultiRepo(context.Background(), manifests, project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if profile == nil {
			t.Fatalf("expected non-nil profile")
		}
		if profile.Runtimes["go"] != "1.27.2" {
			t.Errorf("expected go 1.27.2, got %s", profile.Runtimes["go"])
		}
	})

	t.Run("5. Invalid manifest: schemaVersion != 1 or unknown runtime key fails closed immediately; UI override does NOT forgive invalid manifest", func(t *testing.T) {
		project := &models.Project{
			RuntimeConfig: models.JSONMap{
				"go": "1.27.2",
			},
		}

		// 5a. SchemaVersion != 1
		invalidSchemaManifests := map[string]*runner.RepositoryManifest{
			"repo-a": {
				SchemaVersion: 2,
				Runtimes: map[string]interface{}{
					"go": "1.27.2",
				},
			},
		}
		profile, err := resolver.ResolveMultiRepo(context.Background(), invalidSchemaManifests, project)
		if err == nil {
			t.Fatalf("expected schema error, got nil")
		}
		if profile != nil {
			t.Fatalf("expected nil profile, got %v", profile)
		}
		if !strings.Contains(err.Error(), "unsupported schema version: 2 (expected 1)") {
			t.Errorf("expected 'unsupported schema version: 2 (expected 1)', got: %v", err)
		}

		// 5b. Unknown runtime key
		unknownRuntimeManifests := map[string]*runner.RepositoryManifest{
			"repo-a": {
				SchemaVersion: 1,
				Runtimes: map[string]interface{}{
					"unknown_lang": "1.0.0",
				},
			},
		}
		profile, err = resolver.ResolveMultiRepo(context.Background(), unknownRuntimeManifests, project)
		if err == nil {
			t.Fatalf("expected unknown runtime error, got nil")
		}
		if profile != nil {
			t.Fatalf("expected nil profile, got %v", profile)
		}
		if !strings.Contains(err.Error(), "unknown runtime: unknown_lang") {
			t.Errorf("expected 'unknown runtime: unknown_lang', got: %v", err)
		}

		// 5c. Invalid runtime value type (e.g. bool true)
		invalidValueManifests := map[string]*runner.RepositoryManifest{
			"repo-a": {
				SchemaVersion: 1,
				Runtimes: map[string]interface{}{
					"go": true,
				},
			},
		}
		profile, err = resolver.ResolveMultiRepo(context.Background(), invalidValueManifests, project)
		if err == nil {
			t.Fatalf("expected invalid value error, got nil")
		}
		if profile != nil {
			t.Fatalf("expected nil profile, got %v", profile)
		}
		if !strings.Contains(err.Error(), "invalid runtime value for go") {
			t.Errorf("expected 'invalid runtime value for go', got: %v", err)
		}
	})

	t.Run("6. Uncataloged version: Repo specifying uncataloged version fails closed under catalog validation", func(t *testing.T) {
		project := &models.Project{
			RuntimeConfig: models.JSONMap{},
		}
		manifests := map[string]*runner.RepositoryManifest{
			"repo-a": {
				SchemaVersion: 1,
				Runtimes: map[string]interface{}{
					"go": "9.99.99",
				},
			},
		}
		profile, err := resolver.ResolveMultiRepo(context.Background(), manifests, project)
		if err == nil {
			t.Fatalf("expected uncataloged version error, got nil")
		}
		if profile != nil {
			t.Fatalf("expected nil profile, got %v", profile)
		}
		if !strings.Contains(err.Error(), `uncataloged version "9.99.99" for runtime go`) {
			t.Errorf(`expected uncataloged version error for go, got: %v`, err)
		}
	})

	t.Run("7. Project UI/DB override set to false disables runtime across entire project", func(t *testing.T) {
		project := &models.Project{
			RuntimeConfig: models.JSONMap{
				"go": false,
			},
		}
		manifests := map[string]*runner.RepositoryManifest{
			"repo-a": {
				SchemaVersion: 1,
				Runtimes: map[string]interface{}{
					"go": "1.27.2",
				},
			},
		}
		profile, err := resolver.ResolveMultiRepo(context.Background(), manifests, project)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if profile == nil {
			t.Fatalf("expected non-nil profile")
		}
		if _, exists := profile.Runtimes["go"]; exists {
			t.Errorf("go runtime should be disabled and absent from resolved runtimes")
		}

		// Also verify string "false" override
		projectStrFalse := &models.Project{
			RuntimeConfig: models.JSONMap{
				"node": "false",
			},
		}
		profileStr, err := resolver.ResolveMultiRepo(context.Background(), nil, projectStrFalse)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if profileStr == nil {
			t.Fatalf("expected non-nil profile")
		}
		if _, exists := profileStr.Runtimes["node"]; exists {
			t.Errorf("node runtime should be disabled and absent from resolved runtimes")
		}
	})

	t.Run("8. ComputeImageIdentity outputs canonical format atm-runtime:<64_hex_chars> and covers non-runtime fields", func(t *testing.T) {
		profile := &runner.ResolvedRuntimeProfile{
			Platform:       "linux/amd64",
			BaseImageRef:   "alpine:3.24",
			TemplateRev:    "v1",
			OpenCodeDigest: runner.DefaultOpenCodeDigest,
			Runtimes: map[string]string{
				"python": "3.11.17",
				"go":     "1.27.2",
				"node":   "24.21.0",
			},
		}
		tpl, rendered := []byte("template-fixture"), []byte("rendered-fixture")

		_, tag1, err := runner.ComputeImageIdentity(profile, catalog, tpl, rendered)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		matched, err := regexp.MatchString(`^atm-runtime:[0-9a-f]{64}$`, tag1)
		if err != nil {
			t.Fatalf("regex error: %v", err)
		}
		if !matched {
			t.Errorf("tag format should match atm-runtime:<64_hex_chars>, got %s", tag1)
		}

		// Same runtimes but every non-runtime field empty: the tag MUST differ
		// (the legacy runtimes-only tag shared one tag across such profiles).
		profile2 := &runner.ResolvedRuntimeProfile{
			Runtimes: map[string]string{
				"go":     "1.27.2",
				"node":   "24.21.0",
				"python": "3.11.17",
			},
		}
		_, tag2, err := runner.ComputeImageIdentity(profile2, catalog, tpl, rendered)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tag1 == tag2 {
			t.Errorf("profiles differing in platform/base/opencode must not share a tag: %s", tag1)
		}
	})
}
