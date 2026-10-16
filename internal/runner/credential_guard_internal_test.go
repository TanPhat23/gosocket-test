package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/sandbox"
)

var credentialLiteral = regexp.MustCompile(`(?i)(_API_KEY|OPENCODE_(SERVER_)?PASSWORD)`)

// Reviewed uses of credential-looking string literals in non-test code of the sandbox-facing packages.
// A new entry means a new place a credential name can reach a container or process: review it for Gate 0G.
var credentialLiteralAllowlist = map[string][]string{
	"task_runner.go": {"_API_KEY", "OPENCODE_PASSWORD="}, // scrubSandboxEnv suffix check; host-side BuildProcessEnv (scrubbed before container use)
	"models.go":      {"OPENCODE_PASSWORD="},             // host-process env for the OpenCode CLI, never a container
}

func TestCredentialNamesOnlyInReviewedPlaces(t *testing.T) {
	for _, dir := range []string{".", "../sandbox"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			node, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			base := filepath.Base(f)
			ast.Inspect(node, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || !credentialLiteral.MatchString(lit.Value) {
					return true
				}
				if !slices.Contains(credentialLiteralAllowlist[base], strings.Trim(lit.Value, "\"`")) {
					t.Errorf("%s: unreviewed credential-name literal %s", f, lit.Value)
				}
				return true
			})
		}
	}
}

func TestProjectPassword_UniqueAndDiscardedOnStop(t *testing.T) {
	cli := &secretRecordingClient{MockDockerClient: &sandbox.MockDockerClient{}}
	mgr := NewProjectSandboxManager(cli, nil, "").(*projectSandboxManager)
	ctx := context.Background()
	pw := map[string]bool{}
	for _, id := range []string{"a", "b"} {
		r, err := mgr.ensureProjectContainerDisabledDraft(ctx, &models.Project{ID: id}, "img")
		if err != nil || len(r.AuthPassword) != 64 {
			t.Fatalf("draft %s: %v len=%d", id, err, len(r.AuthPassword))
		}
		pw[r.AuthPassword] = true
	}
	if err := mgr.StopProjectContainer(ctx, "a", 0); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := mgr.GetProjectRoute(ctx, "a"); ok {
		t.Error("route (and its password) must be discarded on stop")
	}
	r, err := mgr.ensureProjectContainerDisabledDraft(ctx, &models.Project{ID: "a"}, "img")
	if err != nil {
		t.Fatal(err)
	}
	pw[r.AuthPassword] = true
	if len(pw) != 3 {
		t.Errorf("passwords must be unique per instance and never reused after restart; got %d distinct of 3", len(pw))
	}
}

func TestProjectPassword_NeverLoggedOrSerialized(t *testing.T) {
	var logs bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(prev)
	var sink []string
	prevSink := setBuildLogf(func(f string, a ...any) { sink = append(sink, fmt.Sprintf(f, a...)) })
	defer setBuildLogf(prevSink)

	_, route := runSecretDraft(t)
	js, _ := json.Marshal(route)
	if strings.Contains(string(js), route.AuthPassword) {
		t.Error("ProjectRouteHandle JSON exposes the password")
	}
	if strings.Contains(logs.String(), route.AuthPassword) || strings.Contains(strings.Join(sink, "\n"), route.AuthPassword) {
		t.Error("password reached a log sink")
	}
}
