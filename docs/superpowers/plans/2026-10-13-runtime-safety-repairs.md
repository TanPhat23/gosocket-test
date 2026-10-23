# Phase 1A Runtime Safety Containment Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use subagent-orchestration to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement immediate, unconditional fail-closed runtime quarantine across entry points, eliminating unsafe host provisioning, container launches, and fallback pathways. Acknowledge and enforce an intentional total outage of runtime task execution until qualification gates clear.

**Architecture:**
1. **Unconditional Quarantine Entry Refusal:** `projectSandboxManager.EnsureProjectContainer` and `AgentTaskRunner.RunTask` unconditionally return explicit sentinel quarantine errors on statement 1 before acquiring mutexes, inspecting cached routes, performing Docker operations, syncing git repositories, creating worktrees, or altering file permissions.
2. **Unconditional Host Workspace Provisioning Refusal:** `workspaceManager.EnsureTaskWorktree` unconditionally refuses host git worktree provisioning on statement 1. Ephemeral scratch cleanup (`CleanupTaskScratch`) and read-only status methods remain intact and safe.
3. **Elimination of Unsafe Fallbacks:** All host git execution fallbacks, container launch sequences, and host worktree creations are excised from active execution paths. `RunTaskAsync` reliably propagates quarantine errors to callers.
4. **Scope Separation (Phase 1A vs. Phase 1B):** Phase 1A strictly establishes containment and quarantine. Cryptographic cache tag expansion, private template/catalog identity hashing, Docker JSON build stream error parsing, and catalog checksum validation are deferred to Phase 1B as a mandatory prerequisite blocker prior to any sandbox container reactivation.

**Tech Stack:** Go 1.27, Docker Engine SDK (`github.com/docker/docker v27.5.1+incompatible`), Git CLI.

**Spec:**
- `docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md`
- `docs/superpowers/specs/2026-10-11-gate-0c-git-transfer-design.md`
- `docs/superpowers/specs/2026-10-11-gate-0d-egress-confinement-design.md`
- `docs/superpowers/specs/2026-10-11-gate-0g-model-credential-isolation-design.md`

**Global Constraints:**
- **Intentional Containment Outage:** This plan introduces an intentional runtime execution outage. All task execution fails closed immediately. Review and user approval are required before execution.
- **Zero Host Mutation:** Source edits occur directly in the workspace worktree. All Go test, race, build, and diff checks must run inside a separately authorized isolated Linux guest/build container; no tests or Docker daemon calls run on the bare development host.
- **No Boolean Toggles or Fake Readiness:** Quarantine is unconditional on entry. No boolean switches, environment overrides, or fake readiness providers are permitted.
- **Preservation of Unrelated Dirty Changes:** Unrelated dirty edits in the working tree are preserved. Unsafe method bodies in `project_sandbox_manager.go`, `task_runner.go`, and `workspace_manager.go` are excised or short-circuited as required for quarantine.

**Review Focus:**
1. Unconditional refusal in `EnsureProjectContainer`: Proves zero Docker calls and zero cached-route returns even when routes are populated.
2. Unconditional refusal in `RunTask`: Proves immediate error return with zero host git commands, zero worktree mutations, and zero Docker operations across both nil and non-nil `projectSandboxMgr`.
3. Error propagation in `RunTaskAsync`: Proves the done channel delivers `ErrRuntimeExecutionUnavailable`.
4. Unconditional refusal in `EnsureTaskWorktree`: Proves zero filesystem directory creations in the workspace root.
5. Legacy test expectation migration: Verifies that legacy success tests in test suites are updated to assert quarantine errors rather than skipped or removed.

---

## Task 0: Isolated Guest Environment Prerequisites & Authorization

- [ ] **Step 1: User & Architecture Approval:** Secure explicit user approval for the intentional Phase 1A runtime execution outage and the use of an isolated Linux guest environment for test execution.
- [ ] **Step 2: Guest Isolation Hygiene:** Ensure the repository snapshot is transferred to the isolated guest without forwarding host Docker sockets (`/var/run/docker.sock`), host git credentials, or host secrets. Source code editing occurs in the repository worktree, but all compilation, test execution, and static checks are executed solely within the authorized isolated guest.

---

## Task 1: Unconditional Fail-Closed Quarantine & Host Fallback Elimination

### Goal
Enforce unconditional fail-closed refusal across `ProjectSandboxManager`, `AgentTaskRunner`, and `WorkspaceManager`. Ensure that runtime container execution and host workspace provisioning fail closed on their very first statement before any side effect (network creation, volume creation, container creation, container inspection, cached-route lookup, host git operations, worktree creation, or chmod). Update legacy success tests to assert the quarantine errors, proving that no unsafe execution pathways remain active.

### File Boundaries
- Modify: `internal/runner/project_sandbox_manager.go`
- Modify: `internal/runner/task_runner.go`
- Modify: `internal/runner/workspace_manager.go`
- Create: `internal/runner/project_sandbox_quarantine_internal_test.go`
- Modify: `internal/runner/project_sandbox_manager_test.go`
- Modify: `internal/runner/task_runner_test.go`
- Modify: `internal/runner/workspace_manager_test.go`

### Exact Proposed Code Changes

In `internal/runner/project_sandbox_manager.go`:
```go
import (
	"errors"
	// existing imports...
)

// ErrQuarantineActive is returned when project sandbox runtime execution is blocked pending qualification.
var ErrQuarantineActive = errors.New("runtime sandbox quarantined: execution blocked pending Gate 0C/0D/0G qualification")

func (m *projectSandboxManager) EnsureProjectContainer(ctx context.Context, project *models.Project, imageTag string) (*ProjectRouteHandle, error) {
	// First-statement unconditional quarantine: fail before locks, route cache hits, or Docker inspections.
	return nil, ErrQuarantineActive
}
```

In `internal/runner/task_runner.go`:
```go
import (
	"errors"
	// existing imports...
)

// ErrRuntimeExecutionUnavailable is returned when task execution cannot proceed safely without sandboxing.
var ErrRuntimeExecutionUnavailable = errors.New("runtime execution unavailable: project-scoped sandbox quarantined pending Gate 0C/0D/0G qualification")

func (r *AgentTaskRunner) RunTask(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) (retErr error) {
	if task == nil {
		return errors.New("task cannot be nil")
	}

	r.mu.Lock()
	if r.doneChans == nil {
		r.doneChans = make(map[string][]chan error)
	}
	if r.running == nil {
		r.running = make(map[string]bool)
	}
	if r.completed == nil {
		r.completed = make(map[string]error)
	}
	r.running[task.ID] = true
	delete(r.completed, task.ID)
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.running[task.ID] = false
		r.completed[task.ID] = retErr
		chans := r.doneChans[task.ID]
		delete(r.doneChans, task.ID)
		r.mu.Unlock()

		for _, ch := range chans {
			select {
			case ch <- retErr:
			default:
			}
			close(ch)
		}
	}()

	// First-statement quarantine refusal: refuse execution immediately without checking project, repo, or host git.
	return ErrRuntimeExecutionUnavailable
}
```

In `internal/runner/workspace_manager.go`:
```go
import (
	"errors"
	// existing imports...
)

// ErrHostWorkspaceProvisioningRefused is returned because host git provisioning is prohibited.
var ErrHostWorkspaceProvisioningRefused = errors.New("workspace provisioning refused: host git operations disabled pending Gate 0C isolated helper")

func (m *workspaceManager) EnsureTaskWorktree(ctx context.Context, projectID string, task *models.Task, repoName string, baseCommit string) (string, error) {
	// First-statement unconditional refusal: fail before path sanitization, os.Stat, git inspections, or worktree add.
	return "", ErrHostWorkspaceProvisioningRefused
}
```

### Exact Proposed Test Code

Create `internal/runner/project_sandbox_quarantine_internal_test.go`:
```go
package runner

import (
	"context"
	"errors"
	"testing"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/sandbox"
)

type forbiddenDockerCallSpy struct {
	sandbox.DockerClient
}

func TestProjectSandboxManager_Quarantine_BypassesCachedRouteAndDocker(t *testing.T) {
	ctx := context.Background()
	spy := &forbiddenDockerCallSpy{}
	mgr := NewProjectSandboxManager(spy, nil, "").(*projectSandboxManager)

	// Pre-populate route cache to prove quarantine refuses before returning cached route
	mgr.routes["proj-cached"] = &ProjectRouteHandle{
		ProjectID:   "proj-cached",
		ContainerID: "cont-cached-123",
		EndpointURL: "http://127.0.0.1:4096",
	}

	project := &models.Project{ID: "proj-cached"}
	handle, err := mgr.EnsureProjectContainer(ctx, project, "atm-project-runtime:test")

	if !errors.Is(err, ErrQuarantineActive) {
		t.Fatalf("expected ErrQuarantineActive, got %v", err)
	}
	if handle != nil {
		t.Fatalf("expected nil handle on quarantine refusal, got %+v", handle)
	}
}
```

In `internal/runner/task_runner_test.go`:
```go
func TestRunTask_Quarantine_RefusesNilAndNonNilSandboxManager(t *testing.T) {
	ctx := context.Background()

	t.Run("Nil projectSandboxMgr refuses without host fallback", func(t *testing.T) {
		ar := runner.NewAgentTaskRunner(
			nil, nil, nil, nil, nil, nil, nil, nil,
		)
		task := &models.Task{ID: "task-nil-mgr", ProjectID: "proj-1"}
		attempt := &models.TaskAttempt{ID: "att-1", TaskID: task.ID}

		err := ar.RunTask(ctx, task, attempt)
		if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
			t.Fatalf("expected ErrRuntimeExecutionUnavailable, got %v", err)
		}
	})

	t.Run("Non-nil projectSandboxMgr refuses without host fallback", func(t *testing.T) {
		mockMgr := runner.NewProjectSandboxManager(&forbiddenDockerCallSpy{}, nil, "")
		ar := runner.NewAgentTaskRunner(
			nil, nil, nil, nil, nil, nil, nil, nil,
			runner.WithRunnerProjectSandboxManager(mockMgr),
		)
		task := &models.Task{ID: "task-non-nil-mgr", ProjectID: "proj-1"}
		attempt := &models.TaskAttempt{ID: "att-2", TaskID: task.ID}

		err := ar.RunTask(ctx, task, attempt)
		if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
			t.Fatalf("expected ErrRuntimeExecutionUnavailable, got %v", err)
		}
	})
}

func TestRunTaskAsync_Quarantine_PropagatesError(t *testing.T) {
	ctx := context.Background()
	ar := runner.NewAgentTaskRunner(
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	task := &models.Task{ID: "task-async", ProjectID: "proj-async"}
	attempt := &models.TaskAttempt{ID: "att-async", TaskID: task.ID}

	doneCh := ar.RunTaskAsync(ctx, task, attempt)
	err := <-doneCh
	if !errors.Is(err, runner.ErrRuntimeExecutionUnavailable) {
		t.Fatalf("expected ErrRuntimeExecutionUnavailable from doneCh, got %v", err)
	}
}
```

In `internal/runner/workspace_manager_test.go`:
```go
func TestWorkspaceManager_HostRefusal_ZeroFilesystemMutations(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	scratchRoot := t.TempDir()

	wm := runner.NewWorkspaceManager(workspaceRoot, scratchRoot)
	task := &models.Task{ID: "task-refusal"}
	path, err := wm.EnsureTaskWorktree(ctx, "proj-1", task, "repo", "main")

	if !errors.Is(err, runner.ErrHostWorkspaceProvisioningRefused) {
		t.Fatalf("expected ErrHostWorkspaceProvisioningRefused, got %v", err)
	}
	if path != "" {
		t.Fatalf("expected empty worktree path, got %q", path)
	}

	wsEntries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		t.Fatalf("failed to read workspaceRoot: %v", err)
	}
	if len(wsEntries) != 0 {
		t.Fatalf("expected workspaceRoot to be empty, found %d entries", len(wsEntries))
	}
}
```

### Legacy Success Test Migrations
Rather than deleting or skipping tests, update existing test expectations to assert the quarantine refusal errors:

1. **`internal/runner/project_sandbox_manager_test.go`**:
   - `EnsureProjectContainer creates hardened container with correct configs` -> assert `errors.Is(err, runner.ErrQuarantineActive)`.
   - `EnsureProjectContainer returns cached route handle when container is already running` -> assert `errors.Is(err, runner.ErrQuarantineActive)`.
   - `Fail-closed cleanup when CopyToContainer fails` -> assert `errors.Is(err, runner.ErrQuarantineActive)`.

2. **`internal/runner/workspace_manager_test.go`**:
   - `TestWorkspaceManager_EnsureTaskWorktree_ValidProvisioning` -> assert `errors.Is(err, runner.ErrHostWorkspaceProvisioningRefused)`.
   - `TestWorkspaceManager_EnsureTaskWorktree_NoCheckout` -> assert `errors.Is(err, runner.ErrHostWorkspaceProvisioningRefused)`.
   - `TestWorkspaceManager_SubmoduleFailClosed` -> assert `errors.Is(err, runner.ErrHostWorkspaceProvisioningRefused)`.
   - `TestWorkspaceManager_GitLFSFailClosed` -> assert `errors.Is(err, runner.ErrHostWorkspaceProvisioningRefused)`.
   - `TestWorkspaceManager_PathSanitization` -> assert `errors.Is(err, runner.ErrHostWorkspaceProvisioningRefused)`.
   - `TestWorkspaceManager_ZeroCredentialsInGitConfig` -> assert `errors.Is(err, runner.ErrHostWorkspaceProvisioningRefused)`.

3. **`internal/runner/task_runner_test.go`**:
   - `TestRunTask_VerificationLoop_PassFirstTurn` -> assert `errors.Is(err, runner.ErrRuntimeExecutionUnavailable)`.
   - `TestRunTask_VerificationLoop_FailThenPass` -> assert `errors.Is(err, runner.ErrRuntimeExecutionUnavailable)`.
   - `TestRunTask_VerificationLoop_MaxRetriesExceeded` -> assert `errors.Is(err, runner.ErrRuntimeExecutionUnavailable)`.
   - `TestRunTask_EnsureSessionLocationBinding` -> assert `errors.Is(err, runner.ErrRuntimeExecutionUnavailable)`.
   - `TestRunTask_GitHubIntegrationSyncAndPushBranch` -> assert `errors.Is(err, runner.ErrRuntimeExecutionUnavailable)`.
   - `TestTaskRunner_DockerUnavailable_FailsClosed` -> assert `errors.Is(err, runner.ErrRuntimeExecutionUnavailable)`.
   - `TestTaskRunner_ContainerCreateError_FailsClosed` -> assert `errors.Is(err, runner.ErrRuntimeExecutionUnavailable)`.
   - `TestAgentTaskRunner_InContainerWorktreePathBinding` -> assert `errors.Is(err, runner.ErrRuntimeExecutionUnavailable)`.
   - `TestAgentTaskRunner_NoHostFallbackOnSandboxFailure` -> assert `errors.Is(err, runner.ErrRuntimeExecutionUnavailable)`.

### TDD Execution Steps
- [ ] **Step 1 (RED):** Write `TestProjectSandboxManager_Quarantine_BypassesCachedRouteAndDocker`, `TestRunTask_Quarantine_RefusesNilAndNonNilSandboxManager`, `TestRunTaskAsync_Quarantine_PropagatesError`, and `TestWorkspaceManager_HostRefusal_ZeroFilesystemMutations`. In isolated guest, execute:
  `go test -count=1 ./internal/runner -run "TestProjectSandboxManager_Quarantine|TestRunTask_Quarantine|TestRunTaskAsync_Quarantine|TestWorkspaceManager_HostRefusal"`
  Confirm tests fail.
- [ ] **Step 2 (IMPLEMENT):** Update `EnsureProjectContainer`, `RunTask`, and `EnsureTaskWorktree` with unconditional first-statement refusals and update legacy tests.
- [ ] **Step 3 (GREEN):** In isolated guest, execute:
  `go test -count=1 ./internal/runner -run "TestProjectSandboxManager_Quarantine|TestRunTask_Quarantine|TestRunTaskAsync_Quarantine|TestWorkspaceManager_HostRefusal"`
  Confirm tests pass.
- [ ] **Step 4 (FULL SUITE & RACE):** In isolated guest, execute:
  `go test -count=1 ./...` and `go test -count=1 -race ./internal/runner/...`
  Confirm entire test suite passes under quarantine assertions.

---

## Mandatory Phase 1B Prerequisite Blockers (Deferred)

> **Status (2026-10-14): implemented, quarantine still active.** Items 1–5 were implemented by Phase 1B (`2026-10-14-phase-1b-image-build-integrity.md`, Tasks 1–5 and 4b) and verified offline only: full suite, vet and mutation checks in atm-guest, plus an independent adversarial review whose findings were fixed. Not yet verified: the race detector, live BuildKit on a real engine, and real checksums (all still sentinels). This does not lift the quarantine and is not a security sign-off.

The following items are separated into **Phase 1B** and must be planned and completed before quarantine is lifted:
1. **Cryptographic Profile Cache Tagging:** Expand `ComputeCacheTag` to hash all five profile fields deterministically.
2. **Private Template & Catalog Identity:** Bind the cache tag to the hash of rendered template and catalog toolchain bytes in `runtimeImageBuilder`.
3. **Docker JSON Build Stream Parsing:** Decode progress messages, catch `error` / `errorDetail.Message`, handle EOF / stream truncation, and return fixed sanitized errors.
4. **Catalog SHA256 & Format Preflight:** Validate format, non-empty checksums, and qualified URLs before querying `ImageList` or invoking `ImageBuild`.
5. **Dockerfile Template Verification:** Verify archive SHA256 checksums before unpacking in `Dockerfile.project.template`.

---

## Guest Execution Verification Commands

Execute the following commands strictly inside the authorized isolated Linux guest environment:
1. `go test -count=1 ./...`
2. `go test -count=1 -race ./internal/runner/...`
3. `go build ./cmd/server`
4. `git diff --check`
