# Phase 1A Quarantine: Gate Status Map

**Date:** 2026-10-15
**Branch:** `feature/project-scoped-runtime` (HEAD `45ebc19`)
**Kind:** Static, read-only status map built from the repo. No Go tests, Docker or host commands were run for it.
**Superseded in part: see the addendum at the end. Sections 1-6 describe HEAD `45ebc19` only.**
**This is not a readiness claim, a security sign-off, or a recommendation to lift the quarantine.** Lifting is a separate explicit user decision. "Unverified" means it cannot be established from the repo.

## 1. What the quarantine is and what lifts it

The quarantine is unconditional code, not a flag. There is no config key, env var or readiness provider that turns it off.

| Layer | Location | Behavior |
|---|---|---|
| Sentinel | `internal/common/quarantine.go` `ErrRuntimeQuarantined` | Shared error, detected with `errors.Is`. |
| Container | `internal/runner/project_sandbox_manager.go:73-75` `EnsureProjectContainer` | First statement returns `ErrQuarantineActive`. No lock, route cache or Docker call. |
| Task run | `internal/runner/task_runner.go:366-409` `RunTask` | Done-channel bookkeeping, then returns `ErrRuntimeExecutionUnavailable`. |
| Worktree | `internal/runner/workspace_manager.go:56-58` `EnsureTaskWorktree` | First statement returns `ErrHostWorkspaceProvisioningRefused`. |
| Service | `internal/service/task_service.go` (lines 429, 497, 704, 778, 1209, 1237, 1294) | Steering, push, snapshot paths return `ErrRuntimeExecutionUnavailable`. `ReconcileActivePRs` (about line 1401) is a silent no-op. |
| Other | `internal/service/webhook_service.go:400`, `internal/runner/models.go:100` | Merged-PR sync and host-binary model discovery disabled. |
| Queue / API | `internal/queue/worker.go:129`, `internal/api/error_handler.go:99` | `asynq.SkipRetry` (no retry); HTTP 503 "Task execution is temporarily unavailable". |
| Guard | `internal/quarantineguard/guard_test.go` | AST regression test, below. |

Original bodies are kept as unreferenced `*DisabledDraft` functions.

**Guard (`internal/quarantineguard`).** It is a test only, with no runtime code. It parses all non-test `.go` files and fails if a guarded symbol is referenced outside a `*DisabledDraft` function or an `allowedCallers` entry.
- Guarded symbols: `runInDocker`, `EnsureGitRepo`, `SyncRepoWithRemote`, `PushBranch`, `CaptureSnapshot`, `CreateWorktree`, `InstallDependencies`, `RunPreFlightVerification`, `GetSessionMessages` and others (`guardedSymbols`).
- It also blocks `os/exec` `Command`, `CommandContext` and `Cmd` everywhere except `internal/runner/image_builder.go` (`execExemptFiles`).
- It catches alias forms (variable, slice, map, return, package-level, dot-import).
- Stated limits: name-based and syntactic. It does not detect `go:linkname`, reflection, cgo, or a guarded function reached through an interface under a different name.
- Not run for this document. Whether it passes at HEAD is **unverified** here (commit messages say it passed in the sealed guest).

**Condition that lifts it in code:** none exists. Lifting means editing the three first-statement refusals above, the service and webhook short-circuits, and the guard allowlists. The plans say the quarantine stays until the gates clear (`plans/2026-10-13-runtime-safety-repairs.md` Goal; `plans/2026-10-14-phase-1b-image-build-integrity.md` Non-Goals).

## 2. Gate status

Gate definitions come from the self-hosting spec (`specs/2026-10-11-self-hosting-readiness-design.md`, gate table and "Detailed Gate Criteria") plus each gate's own spec. Every gate is UNQUALIFIED in the docs. No doc records a gate as passed other than the scoped 0A/0B probe.

| Gate | Definition (doc) | Required evidence | In repo today | Missing | Owner / host action |
|---|---|---|---|---|---|
| 0A NNP on native Docker | `verification/2026-10-11-runtime-qualification-gates-0a-0b.md` §2 | `NoNewPrivs:1` under `--cap-drop=ALL --read-only` on non-Snap Docker | Passing probe on the Ubuntu 24.04 QEMU/KVM guest only (exit 0). Snap Docker recorded incompatible. | Any production host is unqualified. The self-hosting spec marks the status "Doc Stale". | Host operator picks a native, non-Snap Docker. |
| 0B OpenCode image | same doc §3 | Digest, ABI, startup, loopback auth | `ghcr.io/anomalyco/opencode:2.0.26` amd64 `sha256:ea6c5d68...db255`. `--version`, `serve --help`, 401 and 200 auth probes passed under `--network none` in the guest. | Cosign/Notary provenance, CPU floor (needs AVX/SSE4/BMI; hangs on `qemu64`), password lifecycle (see 0G). Alpine-to-Debian composition is partly addressed by Phase 1B (musl catalog archives, `dc72d73`); doc status of that is **unverified**. | Operator sets the CPU baseline. |
| 0C Git transfer | `specs/2026-10-11-gate-0c-git-transfer-design.md` header, §5, §9 | Probes 1-9: clone/push round-trip, canary leakage scan, TLS-insecurity negative, submodule/LFS rejection, malicious volume config, path traversal, network confinement | Design only (**PROPOSED, UNQUALIFIED**). No bundle/helper code in `internal/runner`. `internal/git` still passes the token via `-c http.extraHeader=` in argv (`sync.go:56,66,121`, `push.go:31`), but those functions are quarantine-guarded. | Helper container, all nine probes, user review and Checkpoint A. | ATM. No host action. |
| 0D Egress | `specs/2026-10-11-gate-0d-egress-confinement-design.md` §3-§8 | Negative probes (host, LAN, metadata, peer bridges), positive registry probes, DNS test, fail-closed launch contract | Design user-approved 2026-10-12 as design only, **UNQUALIFIED**. **Update 2026-10-16:** the full spec 8.1 matrix (POS-0D-01..07, NEG-0D-01..15) ran in a QEMU VM with positive controls; results, defects found and operator requirements are in `plans/2026-10-16-gate-0d-operator-handoff.md` section 9. Evidence complete for a VM. **QUALIFIED 2026-10-16 on VM evidence by explicit user sign-off** ("this is enough for the 0D"); a run on the real host is still recommended before production. This does not lift the quarantine: 0C needs its Checkpoint A review, 0F and 0G remain open, and lifting needs a separate explicit go-ahead. `internal/sandbox/egress_filter.go` has IP helpers and proxy env only. `project_sandbox_manager.go:118` creates the bridge with `Internal: false` (inside a quarantined function). | Forward-proxy DMZ, nftables ruleset, probes. Unresolved blocker: how ATM verifies firewall health without `NET_ADMIN` (canary probe vs `/etc/atm/firewall-ready` attestation, §4.2, §10.2). `--dns=127.0.0.1` untested. Residual risks: HTTPS CONNECT exfiltration, shared-proxy blast radius. | **Host operator** owns, deploys and persists the firewall (§4.1). ATM must not edit rules or hold `CAP_NET_ADMIN`. |
| 0E Zero host execution | self-hosting spec gate table; `plans/2026-10-11-deployment-and-hardening.md` Task 2 | `runOnHost`, host package calls and `sslVerify=false` removed | `runOnHost` has no hit in non-test Go. `internal/git/sync.go` and `push.go` have no `sslVerify` in the working tree, but those edits are uncommitted (HEAD still has them: 5 and 3 matches). `dependencies.go` is documented as never spawning host package managers. Host paths are reachable only via quarantined or `*DisabledDraft` code. | Committed 0E change, review, and a decision on whether the quarantine-guarded draft code counts as removed. Legacy `docker-compose.yml:132` still defaults `GIT_SSL_NO_VERIFY` to `true`. | Deployment plan owner. |
| 0F Production deployment | `specs/2026-10-11-gate-0f-production-deployment-design.md` §3-§8 (**untracked**) | Probes 0F-1 to 0F-4 on a clean staging host: Traefik without docker.sock, default-secret rejection, bootstrap atomicity/symlink/rollback, key-format validation | Working tree only, uncommitted: `docker-compose.prod.yml`, `scripts/bootstrap-secrets.sh`, `deploy/{dex,redis}/`, `deploy/traefik/*`, `LICENSE`, `internal/config/config.go`+tests, `tests/*.sh`, `cmd/server/main.go` preflight call. The spec says the probes have not been executed (§8 notice). Whether the `tests/*.sh` scripts pass is **unverified**. | Review, commit, probe execution on a clean host, Checkpoints A/B. MIT license was approved 2026-10-13 (spec §7.1). | Deployment plan owner. Production firewall is a host-operator matter. |
| 0G Model credentials and OpenCode password | `specs/2026-10-11-gate-0g-model-credential-isolation-design.md` §5, §7, §10 | Adversarial matrix: provider keys unreachable by task code, password never in logs or argv | Live-qualified on a VM; **QUALIFIED 2026-10-16 by user sign-off** (library built, not wired). Draft `project_sandbox_manager.go:205-225` writes the password with `CopyToContainer` to `/run/secrets`. No gateway code. | Gateway, dynamic-password mechanism (§7.4), matrix. Draft secret injection defect from `verification/2026-10-13-interrupted-implementation-review.md` §2.1 item 2: no consumer in the Dockerfile and a root-owned `0600` file under UID 1000. Whether that is fixed is **unverified**; `Dockerfile.project.template` is untracked. Gate 0D route reconciliation is a hard dependency (§6.2). | ATM plus operator approval. |
| 1B image-build integrity | `plans/2026-10-14-phase-1b-image-build-integrity.md`; deferred items 1-5 in `plans/2026-10-13-runtime-safety-repairs.md` | Full-identity tag, checksum preflight, verified template, stream parsing, bounded builds | Committed in `c1838b3`, `dc72d73`, `8d334e9`, `df37f36`, `5fc9275`. Real AG-2 checksums and Alpine digest pinned (`dc72d73`, human-approved 2026-10-14 per the commit message). Live builds (default and six-toolchain profiles) ran in a disposable VM (commit `8d334e9` message). | No repo document records the live-build evidence, so it is traceable only through commit messages. The builder is not wired into `cmd/server`. Build-time egress (Q3) is not authorized. Floating `apk` index (Q7) and publisher provenance remain open. | Build egress needs host-operator approval. |

## 3. Adjacent work (not gates)

- `4930a59` requires auth and team roles on exposed routes and both WebSockets. `45ebc19` gates local login, adds CSRF and an origin allowlist (`internal/config/security.go`), revalidates WebSockets, and scopes PATs by team. Both claim sealed-guest tests, which cannot be confirmed from the repo. No doc ties them to a gate. They reduce control-plane exposure and do not satisfy 0C, 0D or 0G.
- `c1838b3` also added task authorization (`internal/service/task_authz_test.go`).

## 4. Uncommitted working-tree files from other plans

Modified: `cmd/server/main.go`, `deploy/traefik/{dynamic,traefik}.yml`, `internal/config/{config,config_test}.go`, `internal/git/{push,sync,sync_test}.go`.

Untracked, belonging to the deployment-and-hardening, self-hosting-readiness and other plan owners:
- `LICENSE`, `docker-compose.prod.yml`, `scripts/bootstrap-secrets.sh`, `tests/`, `deploy/dex/config.prod.yml.template`, `deploy/redis/`, `deploy/sandbox/Dockerfile.project.template`
- Docs: the Gate 0F spec, the self-hosting-readiness spec and plan, the deployment-and-hardening plan, the interrupted-implementation review, and the claude/opencode project-search and coordinator-compaction plans.

The Gate 0F design is therefore not in git history. The interrupted-implementation review (which blocks implementation) is also untracked. `deploy/sandbox/Dockerfile.project.template` is untracked while `internal/runner/templates/Dockerfile.project.template` is committed, so two templates exist. Which is authoritative is **unverified** (the embedded one is the one pinned by the Phase 1B golden test).

The shared-file hazard from the review §3 applies: `cmd/server/main.go` and `internal/runner/task_runner.go` are edited by both the runtime and deployment plans.

## 5. Contradictions between docs, and between docs and code

1. `plans/2026-10-13-runtime-safety-repairs.md` line 167 says the checksums are "all still sentinels" and the race detector is unverified. Commit `dc72d73` pins real checksums, and its message and the `5fc9275`/`df37f36` commits report `-race` runs.
2. `plans/2026-10-14-phase-1b-image-build-integrity.md` is headed "DRAFT. Not approved" and describes "Live builds stay blocked". The code is committed, and commit `8d334e9` reports live builds in a disposable VM.
3. The same plan says its Non-Goals leave `image_builder*.go` untracked and uncalled. They are now tracked and `execExemptFiles` still lists the file.
4. `specs/2026-10-11-gate-0c-git-transfer-design.md` §8 cites `sync.go` lines 59 and 29 and `-c http.sslVerify=false` retries. At HEAD the retries exist. In the working tree they are removed (uncommitted), and the token still travels as `http.extraHeader` in argv.
5. Quarantine messages and comments cite "Gates 0C/0D/0G". The roadmap lists 0E and 0F as release-blocking too, and 0A/0B as scoped only. The gate list is not uniform across docs.
6. `specs/2026-10-11-gate-0f-production-deployment-design.md` §8 says the code has not begun. The working tree already holds the compose file, bootstrap script and config code.
7. The 0A/0B report says "Tasks 1-9 stay blocked". The review (2026-10-13) says to halt all implementation. Commits since then (`c1838b3` onward) implement code. The docs do not record a Checkpoint A/B sign-off for this. Whether one occurred is **unverified**.
8. `plans/2026-10-13-runtime-safety-repairs.md` shows its Task 1 sentinel text as "pending Gate 0C/0D/0G qualification". The code's `ErrRuntimeQuarantined` message is the generic "runtime execution is temporarily disabled".

## 6. Not established

- Whether the guard and the full suite pass at HEAD right now (not run).
- Any Checkpoint A/B or independent security sign-off.
- Gate 0D host firewall: no operator policy is present in the repo.
- Provenance and signatures for the OpenCode image and toolchain archives beyond the pinned checksums.

## Addendum (later on 2026-10-15)

Not a re-audit. It records what changed since `45ebc19`; the sections above are unchanged and are stale where noted.

- **Gate 0G (2026-10-16 live run, QUALIFIED on VM evidence by user sign-off the same day):** the spec §10 matrix ran in a QEMU VM behind the Gate 0D firewall. all probes pass (11 only approximated); 03 initially failed and was fixed the same day with a `BASH_ENV` scrub (hygiene, not a boundary). B3 (dollar spend cap from an operator price file) and B4 (TLS 1.3 listener, private CA baked into the image) were changed by the user later that day, built, and re-run live over TLS; the user then signed 0G off ("ok sign this off"), scope 0G only. One startup defect (read-only rootfs, no HOME) was fixed. Details: `plans/2026-10-15-gate-0g-credential-isolation.md`.
- **Gate 0G (earlier):** the gateway core, the env-configured service, the per-container token broker, the OpenCode config builder and the operator firewall handoff are committed (`3e66d92`, `e878cff`, `df93ea1`, `69a479b`, `a1118f8`, `e0c62c3`). Nothing calls the broker or starts the gateway, because `cmd/server/main.go` is not wired. Decision B1 (user, 2026-10-15): the firewall attestation is optional; the gateway starts without it and logs a warning. Gate 0G stays **BLOCKED / UNQUALIFIED**: there is no restricted sandbox-to-gateway route.
- **Gate 0C:** the §2 statement "No bundle/helper code in `internal/runner`" is stale; `e88ffe6` wires a transfer helper. No probe results are recorded, so 0C stays **UNQUALIFIED**.
- **Quarantine:** unchanged. `RunTask` still returns `ErrRuntimeExecutionUnavailable`, and PR create/sync/diff return 503 after the permission check. Observed on the rebuilt docker compose stack: a task ends `failed` with the reason "runtime execution is temporarily disabled" and no sandbox is started.
- **Authorization:** PR create and PR sync now require `RoleMember` (`e14935e`); diff stays viewer-readable. Session tokens without an issued-at time are rejected (`a4512f8`). `/ws/events` caches task visibility per subscriber for 30 seconds (`b0bf58e`).
- **UI:** failed tasks show the latest attempt's failure reason (`78e0e74`), and the 503 session banner says execution is disabled (`a1d3f5e`, `a4cab49`).

This addendum is not a readiness claim, a security sign-off, or a recommendation to lift the quarantine.

## Gate 0C live probe run (2026-10-16, disposable VM `atm-build`, Docker 29.1.3)

Harness: `internal/git/transfer/live_probe_test.go` (build tag `live`) drives the real `CredentialedWorker` against a real daemon and `internal/git/transfer/testdata/livefixture` (HTTPS smart-HTTP git server, own CA, Basic auth with a runtime canary token). Worker image: `alpine:3.20` + `git`, run as `1000:1000`, read-only rootfs, `cap-drop ALL`. The harness maps `github.com` to the fixture with `ExtraHosts`; nothing else about the worker is changed.

Defects the live run found (the mock-based probes 0C-1..9 all passed with them present):
1. `HOME` was `/` on a read-only rootfs, so `git config --global` failed and every transfer exited 255. Fixed: worker containers set `HOME=/tmp` (the tmpfs) and `GIT_TERMINAL_PROMPT=0`.
2. Scripts ended with `git credential erase`, which is not a git subcommand (exit 129), so every transfer failed after doing its work. Fixed: `git credential reject`.
3. `git init --bare` printed to stdout, which is the bundle stream. Fixed: `git init -q`.

Results after the fixes:
| Probe | Result |
|---|---|
| Live 0C-1 fetch → bundle | PASS: bundle verifies, token absent from the stream, fixture saw the token only over TLS |
| Live 0C-3 push task branch | PASS: `refs/heads/task/<id>` lands on the fixture; a second push of a diverged branch was rejected (no force) |
| Live 0C-2 running-worker canary scan | PASS: with the fixture holding upload-pack open, the token is absent from `docker inspect`, every process's argv and environment, and every file under `/tmp`. Positive control: a mutant that put the token in the worker env failed this probe (and the mock 0C-2) |
| Live 0C-4 untrusted certificate | PASS: fails, no bundle, fixture logged a TLS handshake error and no HTTP request |
| Unit/mock suite `internal/git/transfer` | PASS (test binary run in `atm-build`) |

### Second live run (same day): volume helper and the full manager path

Two more defects found and fixed:
4. The worker fetched `--depth=1`. A bundle cut from a shallow repo omits the parents without declaring them, so cloning it fails ("Failed to traverse parents") for any branch with more than one commit. The first run passed only because the fixture repo had one commit. Fixed: full-history fetch of the branch.
5. A new named volume is `root:root 0755`, so the helper (UID 1000, no capabilities) could not write. Docker copies the image's `/workspaces` ownership into an empty volume on first mount, so this is an **image contract**: the helper image must ship `/workspaces` owned by `1000:1000` (the project runtime image already does). The helper now fails fast with `ErrVolumeNotWritable` (exit 103) instead of a bare exit 1; verified live with a non-conforming image.

| Probe | Result |
|---|---|
| Live 0C-1 full path (worker → helper → volume repo + task worktree → export → push) | PASS; the token is absent from every file in the project volume (volume half of 0C-2) |
| Live 0C-5 submodule (`.gitmodules` + gitlink) | PASS: `ErrSubmodulesNotSupported` |
| Live 0C-6 LFS (`.gitattributes filter=lfs`; pointer blob without attributes) | PASS: `ErrGitLFSNotSupported` for both |
| Live 0C-7 planted hooks, `core.hooksPath`, smudge/process filters, `core.fsmonitor`, `core.sshCommand` in the volume repo, then re-ingest a `* filter=evil` branch | PASS: nothing ran. Positive control: plain `git worktree add` with the same config did run them |

0C-9 PASS live (2026-10-16, `atm-build`): the volume helper's real HostConfig, with its command swapped for a probe, sees only `lo`, zero routes, and `nc 1.1.1.1:80` fails. The same probe on the default bridge sees `eth0`, two routes and reaches the host (positive control), and a bridge-mode mutant of the helper fails the test.

0C-8 PASS live: traversal IDs, `;`-injection, `--upload-pack=` and `$(id)` refs are refused with `ErrInvalidIdentifier` or `ErrInvalidRefName`, and no container or volume is created. 0C-9 covers the volume helper only, not the credentialed worker, which needs egress by design.

**Full live suite against the shipped image (2026-10-16):** all of 0C-1..0C-9 pass in `atm-build` using `deploy/git-transfer/Dockerfile` (digest-pinned Alpine, git, CA bundle, `/workspaces` 1000:1000) as the helper and worker, with only the fixture CA layered on top. Evidence for Checkpoint A is complete; what remains for 0C is your review, the `github.com`-only credential limit, and a caller that passes the project ID as `RepoID`.

Findings recorded, not changed (need a decision):
- **Volume name mismatch:** the transfer helper uses `atm-project-data-<repoID>`; the project sandbox mounts `atm-project-data-<projectID>`. Unless the two IDs are equal, a running sandbox never sees the ingested repository.
- **No helper/worker image is defined in the repo** (`atm-helper:latest`/`atm-worker:latest` are placeholders). It needs git, CA certificates, and `/workspaces` owned by `1000:1000`; it should be pinned like the Phase 1B images.
- Credentials are approved for `host=github.com` only (from the first run). Known limitation: credentials are approved for `host=github.com` only, so any other host (GitHub Enterprise, a mirror) gets no credentials. 

**Checkpoint A, Gate 0C (2026-10-16):** the user reviewed the 0C design and the live evidence above and approved it ("I approve this"). Gate 0C is **QUALIFIED on VM evidence** by that sign-off. Scope of the approval: Gate 0C only. It does not cover 0F or 0G (0G stays unqualified, awaiting its own sign-off), it does not resolve the three findings above (volume name mismatch, `github.com`-only credentials, unpinned helper/worker image names), which remain follow-ups before wiring, and it does not lift the quarantine. `TaskRepoTransfer` is still not wired into `RunTask`. Lifting needs 0F and 0G qualified and a separate explicit go-ahead.

**Sign-off, Gate 0G (2026-10-16):** the user signed off 0G ("ok sign this off") after the live matrix and the B3/B4 re-run. 0G is **QUALIFIED on VM evidence**. Scope: 0G only; the accepted deviations and open limits are listed under "Sign-off" in `plans/2026-10-15-gate-0g-credential-isolation.md`. 0D and 0C were already qualified; **0F still needs a clean staging host**. The quarantine stays ON: the gateway, sandbox manager and CA-baked builder are not wired into `RunTask`, and lifting needs 0F qualified plus a separate explicit go-ahead.

**0C follow-ups, status (2026-10-16):** (1) volume name mismatch: already fixed in `a9258f7`. The transfer helper and the sandbox both derive the name from `sandbox.ProjectDataVolumeName`, and `TaskRepoTransfer` passes the project ID as `RepoID`. (2) `github.com`-only credentials: kept on purpose. `githubRemoteURL` only builds `https://github.com/<owner>/<repo>.git` and the credential helper is bound to that host, so a token can never be offered to another host; the integrations layer only supports GitHub. Supporting another host is a new design, not a fix. (3) Unpinned helper/worker images: `ATM_GIT_TRANSFER_IMAGE` must be digest-pinned (`TaskRepoTransferFromEnv` refuses otherwise); the `atm-helper:latest` and `atm-worker:latest` strings were only placeholders in tests and are not used in production code.

**Runtime wiring (2026-10-16), still quarantined:** `runner.ProjectRuntimeFromEnv` (enabled by `ATM_PROJECT_RUNTIME_ENABLED=true`, called from `cmd/server`) assembles the sandbox manager and the image builder. The manager gets the gateway broker, the models and limits, and an `https` gateway URL. Before it creates a project's container it runs `sandbox.VerifyEgressFirewall` on that project's own network, using the pinned probe image, the proxy as control and the configured canaries. Provisioning aborts if the proof fails, with no container created and no secret delivered. The builder bakes the gateway CA into the image. Required settings: `ATM_SANDBOX_GATEWAY_URL`, `ATM_GATEWAY_CA_FILE`, `ATM_SANDBOX_CONTEXT_TOKENS`, `ATM_SANDBOX_PROXY_URL`, `ATM_EGRESS_VERIFY_IMAGE` (digest-pinned), `ATM_EGRESS_CANARIES`; anything missing or unsafe stops startup. `EnsureProjectContainer`, `RunTask` and the workspace manager still refuse unconditionally, so this changes configuration validation, not behaviour. The firewall call sits in the disabled provisioning draft, which is what lifting the quarantine will switch on. Not done: `RunTask` still holds the legacy host-worktree draft and does not yet use the project container, the transfer adapter or the builder; that rewrite is part of lifting, with 0F.


**Project-container task run (2026-10-16), still quarantined, not live-tested:** `runTaskInProjectContainerDisabledDraft` (`internal/runner/project_task_run.go`) is the container-based successor to the host draft. It resolves the GitHub integration and token, calls `TaskRepoTransfer.IngestTaskBranch`, `EnsureProjectContainer` (firewall proof, gateway lease, password delivery), binds the OpenCode session to the in-container worktree, then prompts and waits for a terminal session outcome (`succeeded`, `failed` or `interrupted`) newer than the pre-prompt `UpdatedAt`. Verification (`go test ./...`, `npm test`, `cargo test`, `make test`, or `ATM_VERIFY_CMD`), `git add`/`commit` and the diff all run through `docker exec` in the container with hooks and fsmonitor disabled, as the OpenCode user, and never on the host. A passing run is pushed with `PushTaskBranch` and gets a PR through the shared `openTaskPullRequest` helper, which was split out of `maybeAutoCreatePR`. Unit tests use a scripted exec fake, and five mutations were killed (hooks hardening, stale-outcome check, no-change branch, verification gate, failed-session handling). The function is unreferenced and `RunTask` still refuses first, so behaviour is unchanged. Known gaps, all for the lifting change: not run against a real container or real OpenCode (the terminal `outcome` values and the `UpdatedAt` freshness rule come from the OpenCode 2.0.26 schema and my probes, not from a full live run); `RunTask` bookkeeping (done channels, `running`) is not wired to it; the runtime image comes from a plain `projectImage` field, so the CA-baked builder is not yet consumed; skills and MCP server injection, model selection and child-subagent log streaming are not ported; `EnsureTaskWorktree` is not needed because the transfer helper creates the worktree.

**Container run, follow-up (2026-10-16), still quarantined:** the runtime image now comes from `WithRunnerRuntimeImage(resolver, builder)`: the run resolves the project's profile (catalog baseline plus `project.RuntimeConfig`; repository manifests are not read yet) and builds or reuses the CA-baked image, falling back to a fixed `projectImage`. `cmd/server` passes the builder when `ATM_PROJECT_RUNTIME_ENABLED=true`. Skills and MCP server names are written into the container worktree (base64 argument, no shell interpolation) and excluded from the commit via `info/exclude`; the script is tested by running it against a real git repository. Child-subagent log streaming now polls the project container's client (`watchChildSubagents` takes the client as a parameter). Correction to the previous entry: `RunTask` already does the done-channel bookkeeping, so connecting the run is a one-line change at the refusal. Still not ported: model selection (the prompt API call carries no model, so the gateway config decides it). Still not live-tested. Mutations killed: ignored build error, inject-always, no watcher, and an exclude that writes nowhere.
