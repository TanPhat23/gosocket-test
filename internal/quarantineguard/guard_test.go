package quarantineguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// guardedSymbols are runtime-execution entry points that must only be reachable from
// *DisabledDraft functions (or the allowlisted draft-only helpers below) while the Phase 1A
// quarantine is in force. Every name ending in "DisabledDraft" is guarded as well.
var guardedSymbols = map[string]bool{
	"runInDocker":                          true,
	"maybeAutoCreatePR":                    true,
	"InstallDependencies":                  true,
	"InjectSandboxedConfig":                true,
	"RunPreFlightVerification":             true,
	"EnsureGitRepo":                        true,
	"SyncRepoWithRemote":                   true,
	"CaptureSnapshot":                      true,
	"watchSteeringTurn":                    true,
	"finalizeSteeringTurn":                 true,
	"maybePushBranchForSteering":           true,
	"PushBranch":                           true,
	"SyncGitHubRepo":                       true,
	"CreateWorktree":                       true,
	"RemoveWorktree":                       true,
	"CleanupTaskScratch":                   true,
	"listModelsViaHostBinaryDisabledDraft": true,

	// Draft-only helpers found in the call graph: each is called only from a *DisabledDraft
	// function or from another guarded function, so guarding them keeps the allowlist sound.
	"inspectForSubmodules": true, // ensureTaskWorktreeDisabledDraft
	"inspectForGitLFS":     true, // ensureTaskWorktreeDisabledDraft
	"resolveBaseRef":       true, // WorktreeManager.CreateWorktree
}

// allowedCallers lists non-draft functions whose bodies may reference guarded symbols or
// exec.Command, keyed by "<repo-relative file>:<Recv.>Func". Every entry must itself be a guarded
// symbol (enforced by TestRuntimeQuarantine_GuardListIsCurrent), so any new caller of it outside a
// draft is still reported.
var allowedCallers = map[string]string{
	"internal/git/diff.go:CaptureSnapshot":                                    "guarded implementation (git exec)",
	"internal/git/push.go:PushBranch":                                         "guarded implementation (git exec)",
	"internal/git/sync.go:SyncGitHubRepo":                                     "guarded implementation (git exec)",
	"internal/git/worktree.go:WorktreeManager.CreateWorktree":                 "guarded implementation (git exec)",
	"internal/git/worktree.go:WorktreeManager.RemoveWorktree":                 "guarded implementation (git exec)",
	"internal/git/worktree.go:resolveBaseRef":                                 "helper of CreateWorktree (git exec)",
	"internal/runner/task_runner.go:EnsureGitRepo":                            "guarded implementation (git exec)",
	"internal/runner/task_runner.go:RunPreFlightVerification":                 "guarded implementation (verify exec)",
	"internal/runner/task_runner.go:SyncRepoWithRemote":                       "guarded implementation (git exec)",
	"internal/runner/task_runner.go:AgentTaskRunner.maybeAutoCreatePR":        "guarded; calls PushBranch, git exec",
	"internal/runner/workspace_manager.go:inspectForSubmodules":               "helper of ensureTaskWorktreeDisabledDraft (git exec)",
	"internal/runner/workspace_manager.go:inspectForGitLFS":                   "helper of ensureTaskWorktreeDisabledDraft (git exec)",
	"internal/service/task_service.go:taskService.watchSteeringTurn":          "guarded; calls GetSessionMessages, finalizeSteeringTurn",
	"internal/service/task_service.go:taskService.finalizeSteeringTurn":       "guarded; calls CaptureSnapshot, maybePushBranchForSteering",
	"internal/service/task_service.go:taskService.maybePushBranchForSteering": "guarded; calls PushBranch, git exec",
}

// execExemptFiles may call exec.Command/exec.CommandContext anywhere (Phase 1B, uncalled).
var execExemptFiles = map[string]bool{
	"internal/runner/image_builder.go": true,
}

// Known limits: the guard is name-based and syntactic. It does not detect go:linkname, reflection,
// cgo, or a guarded function reached only through an interface value under a different name.

type violation struct {
	pos    token.Position
	caller string
	symbol string
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate guard_test.go")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod: %v", root, err)
	}
	return root
}

func isDraft(name string) bool { return strings.HasSuffix(name, "DisabledDraft") }

func isGuarded(name string) bool { return guardedSymbols[name] || isDraft(name) }

func funcKey(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	typ := fd.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if idx, ok := typ.(*ast.IndexExpr); ok {
		typ = idx.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}

// execLocalNames returns the identifiers under which the file imports os/exec.
func execLocalNames(f *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if path != "os/exec" {
			continue
		}
		if imp.Name != nil {
			names[imp.Name.Name] = true
		} else {
			names["exec"] = true
		}
	}
	return names
}

// scanNode reports every guarded reference inside n: selector uses, direct calls, and any bare
// identifier use (function values assigned, returned, stored in composite literals, passed as
// arguments, or declared as local/package-level vars). It also reports os/exec use through
// Command, CommandContext and the Cmd type, including via a dot import.
func scanNode(fset *token.FileSet, n ast.Node, execNames map[string]bool, execExempt bool, report func(token.Pos, string)) {
	selIdents := map[*ast.Ident]bool{}
	ast.Inspect(n, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.SelectorExpr:
			selIdents[x.Sel] = true
			if pkg, ok := x.X.(*ast.Ident); ok && execNames[pkg.Name] && isExecSymbol(x.Sel.Name) {
				if !execExempt {
					report(x.Pos(), pkg.Name+"."+x.Sel.Name)
				}
				return true
			}
			if isGuarded(x.Sel.Name) {
				report(x.Sel.Pos(), x.Sel.Name)
			}
		case *ast.Ident:
			if selIdents[x] {
				return true
			}
			if execNames["."] && isExecSymbol(x.Name) {
				if !execExempt {
					report(x.Pos(), "exec."+x.Name+" (dot import)")
				}
				return true
			}
			if isGuarded(x.Name) {
				report(x.Pos(), x.Name)
			}
		}
		return true
	})
}

func isExecSymbol(name string) bool {
	return name == "Command" || name == "CommandContext" || name == "Cmd"
}

func scanFile(fset *token.FileSet, rel string, f *ast.File) []violation {
	execNames := execLocalNames(f)
	execExempt := execExemptFiles[rel]
	var out []violation
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Body == nil || isDraft(d.Name.Name) {
				continue
			}
			key := funcKey(d)
			if _, ok := allowedCallers[rel+":"+key]; ok {
				continue
			}
			scanNode(fset, d.Body, execNames, execExempt, func(p token.Pos, sym string) {
				out = append(out, violation{pos: fset.Position(p), caller: key, symbol: sym})
			})
		case *ast.GenDecl:
			// Package-level initializers are never allowed to reference guarded symbols.
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, v := range vs.Values {
					scanNode(fset, v, execNames, execExempt, func(p token.Pos, sym string) {
						out = append(out, violation{pos: fset.Position(p), caller: "<package-level var>", symbol: sym})
					})
				}
			}
		}
	}
	return out
}

func walkSources(t *testing.T, root string, fn func(rel string, fset *token.FileSet, f *ast.File)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}
		fn(rel, fset, f)
		return nil
	})
	if err != nil {
		t.Fatalf("walk sources: %v", err)
	}
}

func TestRuntimeQuarantine_GuardedSymbolsOnlyReachableFromDrafts(t *testing.T) {
	root := repoRoot(t)
	var violations []violation
	files := 0
	walkSources(t, root, func(rel string, fset *token.FileSet, f *ast.File) {
		files++
		for _, v := range scanFile(fset, rel, f) {
			v.pos.Filename = rel
			violations = append(violations, v)
		}
	})
	if files == 0 {
		t.Fatal("no Go sources scanned; repo root detection is broken")
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].pos.Filename != violations[j].pos.Filename {
			return violations[i].pos.Filename < violations[j].pos.Filename
		}
		return violations[i].pos.Line < violations[j].pos.Line
	})
	for _, v := range violations {
		t.Errorf("%s:%d: %s references quarantined %s; move the call into a *DisabledDraft function "+
			"or (only if it is reachable solely from drafts) add it to allowedCallers with a reason",
			v.pos.Filename, v.pos.Line, v.caller, v.symbol)
	}
}

// TestRuntimeQuarantine_GuardListIsCurrent fails if a guarded name or allowlist entry no longer
// exists, so a rename cannot silently disable the guard.
func TestRuntimeQuarantine_GuardListIsCurrent(t *testing.T) {
	root := repoRoot(t)
	declared := map[string]bool{}
	walkSources(t, root, func(rel string, fset *token.FileSet, f *ast.File) {
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				declared[fd.Name.Name] = true
				declared[rel+":"+funcKey(fd)] = true
			}
		}
	})
	for name := range guardedSymbols {
		if !declared[name] {
			t.Errorf("guarded symbol %s is no longer declared in non-test source; update guardedSymbols", name)
		}
	}
	for key := range allowedCallers {
		if !declared[key] {
			t.Errorf("allowlisted caller %s no longer exists; remove it from allowedCallers", key)
		}
		name := key[strings.LastIndexAny(key, ":.")+1:]
		if !isGuarded(name) {
			t.Errorf("allowlisted caller %s is not itself guarded, so its own callers would go unchecked; add %s to guardedSymbols", key, name)
		}
	}
	for rel := range execExemptFiles {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("exec-exempt file %s no longer exists: %v", rel, err)
		}
	}
}

// TestRuntimeQuarantine_ScannerCatchesAliasForms proves the scanner itself flags the reference
// shapes that could otherwise bypass the guard. Each snippet must produce at least one violation.
func TestRuntimeQuarantine_ScannerCatchesAliasForms(t *testing.T) {
	cases := map[string]string{
		"local var alias":         "package p\nfunc Live() { f := EnsureGitRepo; _ = f }\n",
		"local var decl":          "package p\nfunc Live() { var f = EnsureGitRepo; _ = f }\n",
		"package-level var":       "package p\nvar zz = EnsureGitRepo\n",
		"slice literal":           "package p\nfunc Live() { _ = []func(string) error{EnsureGitRepo} }\n",
		"return value":            "package p\nfunc Live() func(string) error { return EnsureGitRepo }\n",
		"map literal value":       "package p\nfunc Live() { _ = map[string]any{\"a\": CaptureSnapshot} }\n",
		"selector alias":          "package p\nfunc Live() { f := r.EnsureGitRepo; _ = f }\n",
		"direct call":             "package p\nfunc Live() { PushBranch() }\n",
		"draft reference":         "package p\nfunc Live() { fooDisabledDraft() }\n",
		"exec.Command":            "package p\nimport \"os/exec\"\nfunc Live() { exec.Command(\"x\") }\n",
		"exec.Cmd literal":        "package p\nimport \"os/exec\"\nfunc Live() { _ = &exec.Cmd{Path: \"x\"} }\n",
		"renamed exec import":     "package p\nimport ex \"os/exec\"\nfunc Live() { ex.CommandContext(nil, \"x\") }\n",
		"dot-imported exec":       "package p\nimport . \"os/exec\"\nfunc Live() { Command(\"x\") }\n",
		"dot-imported exec.Cmd":   "package p\nimport . \"os/exec\"\nfunc Live() { _ = &Cmd{} }\n",
		"package-level exec call": "package p\nimport \"os/exec\"\nvar c = exec.Command(\"x\")\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "internal/x/y.go", src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := scanFile(fset, "internal/x/y.go", f); len(got) == 0 {
				t.Fatalf("scanner missed a bypass shape:\n%s", src)
			}
		})
	}

	clean := map[string]string{
		"draft body may reference guarded": "package p\nfunc fooDisabledDraft() { EnsureGitRepo(\"x\"); PushBranch() }\n",
		"declaring a guarded name":         "package p\nfunc EnsureGitRepo(string) error { return nil }\n",
		"unrelated code":                   "package p\nfunc Live() int { return 1 }\n",
	}
	for name, src := range clean {
		t.Run("clean/"+name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "internal/x/y.go", src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := scanFile(fset, "internal/x/y.go", f); len(got) != 0 {
				t.Fatalf("scanner false positive: %+v", got)
			}
		})
	}
}
