# Static Implementation Review & Recovery Audit Record

**Date:** 2026-10-13  
**Auditor:** Subagent Implementation Reviewer  
**Audit Scope:** Static-only codebase, configuration, and documentation review. Strictly non-runtime; does not constitute dynamic host qualification, live container verification, or test execution.  
**Disposition:** BLOCKED. Product code implementation, Compose modifications, host operations, and next-phase implementation are stopped pending resolution of architectural security blockers and independent qualification.

---

## 1. Executive Summary & Review Scope

Following an interrupted implementation run across Tasks 1–6, an adversarial static review was conducted against the current worktree state. Prior coordinator records erroneously claimed that Checkpoints A and B had been completed and authorized implementation, and erroneously marked qualification and feature verification steps complete.

This audit establishes:
1. **User Direction Preserved:** The user's planning inputs are valid and preserved (architecture direction looks okay for planning, MIT license chosen, candidate catalog versions selected, Dex in-memory storage chosen for v1, and subagent execution model chosen).
2. **Distinction Between Direction and Verification:** User design consensus does not constitute independent security sign-off, dynamic qualification, host mutation authorization, or resolution of unresolved security mechanics.
3. **Draft Implementation Status:** Tasks 1–5 and partial Task 6 code units exist as unreviewed implementation drafts; they have not passed isolated qualification or independent security review, and several contain critical architectural defects.
4. **Plan Disjointness Correction:** The project-scoped runtime plan and deployment hardening plan are **not** globally disjoint; they share critical execution files (`internal/runner/task_runner.go`, `cmd/server/main.go`) and require sequenced, coordinated ownership.
5. **Hard Stop:** No further implementation, staging, committing, or host operations may proceed until the blockers identified below are resolved.

---

## 2. Blockers & Defect Findings

The following exact findings from the quality defect report are recorded as blocking issues:

### 2.1 Critical Severity Findings

1. **`internal/runner/project_sandbox_manager.go:101-145` & `internal/sandbox/egress_filter.go`**
   - **Finding:** Launches containers on a standard bridge network with `Internal: false` without verifying external host packet filtering or firewall readiness.
   - **Analysis:** `egress_filter.go` provides only IP helpers and proxy environment variables; it does not implement kernel-level default-deny confinement or host firewall rules. The container has direct network bridge routing without validated L3/L4 egress confinement.
   - **Status:** Blocks Gate 0D and Task 4 clearance.

2. **`internal/runner/project_sandbox_manager.go:191-218` & `deploy/sandbox/Dockerfile.project.template`**
   - **Finding:** Copies the secret via tar archive (`CopyToContainer`) to `/run/secrets/opencode` in a stopped container before starting it. The archive is written with root ownership and `0600` permissions.
   - **Analysis:** No daemon entrypoint or consumption mechanism exists in `Dockerfile.project.template` to read this file into OpenCode's environment. The container runs as non-root UID 1000, which cannot read a root-owned `0600` file. Furthermore, same-UID secrecy within the container is not guaranteed. The dynamic password delivery lifecycle remains unqualified and non-functional.
   - **Status:** Blocks Gate 0G and Task 4 clearance.

3. **`internal/runner/task_runner.go:455-632, 710-768` & `internal/runner/workspace_manager.go:67-155`**
   - **Finding:** Task execution in `task_runner.go` and repository synchronization in `workspace_manager.go` retain host git operations and host execution paths.
   - **Analysis:** The Gate 0C bundle streaming transfer helper (`git bundle` stream into non-networked container helper) is missing. Host execution fallbacks have not been eradicated from all task runner workflows.
   - **Status:** Blocks Gate 0C, Gate 0E, and Task 5/6 clearance.

### 2.2 High Severity Findings

4. **`cmd/server/main.go:200-210` & `internal/runner/task_runner.go`**
   - **Finding:** `cmd/server/main.go` lacks wiring for the new `ProjectSandboxManager`.
   - **Analysis:** `task_runner.go` continues to invoke legacy `runInDocker` and host bind mounts even when manager options are supplied. Integration is incomplete and fractured.
   - **Status:** Blocks Task 6 completion.

5. **`internal/runner/runtime_resolver.go:188-210`, `internal/runner/image_builder.go:220-234`, & `internal/runner/runtime_catalog.go`**
   - **Finding:** `ComputeCacheTag` hashes only the runtime mapping, ignoring the full profile (base image ref, template revision, platform, OpenCode digest).
   - **Analysis:** `image_builder.go:220-234` discards the Docker JSON build error stream, masking build failures. Baseline catalog entries in `runtime_catalog.go` have empty `SHA256` checksum fields, and `Dockerfile.project.template` uses raw `curl | tar` commands without checksum verification.
   - **Status:** Blocks Task 2 and Task 3 qualification.

### 2.3 Style & Static Check Findings

6. **`git diff --check` Whitespace Violations**
   - **Files:**
     - `internal/config/config_test.go:424`
     - `internal/runner/opencode_client_test.go:540`
     - `internal/runner/task_runner_test.go:1704`
   - **Finding:** Extra blank line at end of file (EOF).
   - **Disposition:** Recorded as static check findings; no source file edits performed in accordance with documentation-only constraints.

---

## 3. Disjointness Analysis: Runtime Plan vs. Deployment Plan

Previous planning documents claimed that `docs/superpowers/plans/2026-10-10-runtime-image-configuration.md` and `docs/superpowers/plans/2026-10-11-deployment-and-hardening.md` had strictly disjoint file boundaries.

Static analysis confirms this is false:
- `internal/runner/task_runner.go` is touched by both the Runtime Plan (Task 6: session routing and Basic auth) and the Hardening Plan (Task 2: removal of `runOnHost`).
- `cmd/server/main.go` is touched by both the Runtime Plan (Task 1: model migration; Task 7: idle coordinator) and the Hardening Plan (Task 3: startup preflight validation).

**Resolution:** The plans cannot be executed in parallel or treated as independent islands. They must be sequenced with explicit ownership handoffs, ensuring changes to shared files do not collide or overwrite security assertions.

---

## 4. Prior Test Execution Caveat

Previous runs executed Go unit tests directly on the bare development host.
- Per roadmap and spec constraints (§Global Constraints), **bare host execution does not count as approved isolated qualification**.
- Happy-path unit tests with mock clients do not validate container confinement, firewall behavior, or the OpenCode dynamic password lifecycle.
- All tasks marked green based on bare-host test runs are reset to pending isolated qualification.

---

## 5. Next Steps & Recovery Sequence

1. **Halt All Implementation:** Strictly no product code modifications, Compose edits, or test execution.
2. **Bounded Review 1 (Gate 0G Password Lifecycle):** Design an executable, same-UID safe entrypoint consumption mechanism for OpenCode authentication before writing sandbox container code.
3. **Bounded Review 2 (Gate 0D Egress Confinement):** Design host firewall / packet filtering integration to enforce default-deny egress on container bridges.
4. **Bounded Review 3 (Gate 0C Git Transfer):** Implement the isolated bundle transfer helper to eliminate host git execution in `task_runner.go` and `workspace_manager.go`.
5. **Shared File Reconciliation:** Sequence edits between Runtime Task 6 and Hardening Task 2/3 for `task_runner.go` and `cmd/server/main.go`.
6. **Formal Security Sign-Off:** Submit reconciled designs for formal Checkpoint A/B review prior to resuming implementation.
