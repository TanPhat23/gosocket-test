# Phase 1B: Image-Build Integrity Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use subagent-orchestration (or superpowers:subagent-driven-development) to implement this plan task by task. Steps use checkbox (`- [ ]`) syntax. **This plan does not lift the Phase 1A quarantine.**

**Date:** 2026-10-14
**Status:** DRAFT. Not approved. Needs user review and answers to the open questions before any implementation.
**Predecessor:** `docs/superpowers/plans/2026-10-13-runtime-safety-repairs.md` (Phase 1A). Its section "Mandatory Phase 1B Prerequisite Blockers (Deferred)" lists items 1–5; this plan covers all five.
**Spec:** `docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md` §4.1 (Deterministic Build Recipe and Fingerprinting).
**Findings source:** `docs/superpowers/verification/2026-10-13-interrupted-implementation-review.md` §2.2 item 5.
**Decisions recorded 2026-10-14 (from the user's instruction to implement; offline mocked work only):**
- Q1: BuildKit (`BuilderBuildKit`) with post-build `ImageInspectWithRaw` label, OS and arch verification. Live builds stay blocked (AG-3/AG-4 need separate operator approval; Q3 egress unresolved).
- Test environment: `atm-guest` over SSH, sealed (`restrict=on`). No Go tests on the bare host and no host Docker use. The guest had no Go; the host's Go toolchain and a `go mod vendor` snapshot were copied in (coordinator action, disclosed).
- Task 4b mandatory (15m default timeout proposed; Q11 value still to be confirmed).
- Task 3 uses `go:embed` with a pinned-SHA golden test (implies Q9 move into `internal/runner/templates/`).
- Coordinator working assumptions, NOT user decisions, reversible: Q2 remove the `# syntax` line; Q8 fail closed on cache-hit label mismatch; Q10 leave `TemplateRev` field untouched (identity uses the template SHA instead).
- Known limit: the sealed guest has no `gcc`, so `-race` cannot run there.

**Coordinator spot-check (2026-10-14):** facts 1, 2, 4, 5, 8 and 11 in the appendix were re-read against the worktree and hold.

---

## 1. Goals and Non-Goals

### Goals
1. **G1, full-identity cache tag.** The tag is the SHA-256 of a canonical, versioned identity document. The document covers the platform, the digest-pinned base image, the template content hash, the rendered Dockerfile hash, the OpenCode digest, the resolved runtime map, and the URL, SHA-256 and extract path of every toolchain. Two profiles that differ in any build-relevant input never share a tag.
2. **G2, catalog checksums enforced, fail closed.** Every catalog entry carries either a real, independently verified SHA-256 or an explicit "pending" sentinel. A sentinel, empty or malformed value makes the build refuse **before** `ImageList` or `ImageBuild` is called.
3. **G3, verified downloads in the Dockerfile.** The template downloads each archive to a file, checks it with `sha256sum -c`, and only then extracts it. There is no `curl | tar` and no conditional skip of a toolchain.
4. **G4, Docker build stream parsed.** Every JSON message is decoded. An `error` or `errorDetail` field, malformed JSON, truncation, oversize output or a missing result becomes a sanitized error. Success also requires a post-build `ImageInspectWithRaw` check of the identity label and the platform.
5. **G5, template verified before build.** The template bytes are integrity-checked before parsing (embedded in the binary, with a pinned SHA-256 golden test). Template execution fails closed on missing keys or invalid checksums.
6. **G6, cache hits verified.** A cached image is reused only if its identity label matches the computed identity.

### Non-Goals (explicitly out of scope)
- Lifting the Phase 1A quarantine. `EnsureProjectContainer` (`internal/runner/project_sandbox_manager.go:73-75`), `RunTask` and `EnsureTaskWorktree` stay unconditional refusals.
- Wiring `RuntimeImageBuilder` into `cmd/server/main.go`. No production caller exists today; the only callers are in `internal/runner/image_builder_test.go`.
- Changing `internal/runner/task_runner.go`, `cmd/server/main.go`, `internal/runner/project_sandbox_manager.go`, `internal/sandbox/egress_filter.go`, or any compose/deploy file other than `deploy/sandbox/Dockerfile.project.template`.
- Gate 0B base-OS composition (Alpine/musl vs Debian/glibc) and toolchain ABI or layout correctness (see Q4–Q6). Phase 1B makes broken entries fail closed; it does not make them work.
- Pinning OS packages. `apk add` (template lines 5-14) uses a floating index. This is a residual risk (Q7).
- Cosign or Notary provenance for the OpenCode image (still open in Gate 0B).
- Build-time network egress policy (Q3). ATM must never modify host firewall rules or hold `NET_ADMIN`; host operators own that policy. Build-time egress is NOT authorized or resolved by this plan; quarantine does NOT confine privileged daemon builds, and offline unit tests (`--network none`, no socket inside container) do NOT authorize launching host Docker builds. All build egress decisions (Q3) remain blocked pending explicit host operator approval before any live build. Residual build egress risk is NOT accepted now; runtime remains unconditionally quarantined regardless of code or test passes.

### Dependencies for lifting the quarantine (a separate gated decision, not this plan)
Lifting the quarantine requires **all** of the following, and then a separate explicit user decision:
- **Phase 1B** (this plan), complete and reviewed.
- **Gate 0C:** credential-safe `git bundle` transfer into non-networked volume helpers.
- **Gate 0D:** default-deny egress confinement. Host operators own firewall policy; ATM never modifies firewall rules and never receives `NET_ADMIN`.
- **Gate 0G:** OpenCode and model credential isolation, and the OpenCode password lifecycle.
- **Gate 0F:** production compose and deployment.

> **Readiness disclaimer:** none of these is evidence of runtime readiness: a passing marker test, a green unit suite, a single successful live build, or a single-port canary. Completing Phase 1B shows only that the image-build path fails closed on the five listed defects. It says nothing about container isolation, egress, credentials or git transfer.

---

## 2. Threat Model Summary

| # | Threat | Current exposure (verified) | Phase 1B control |
|---|---|---|---|
| T1 | **Cache collision or stale image reuse.** A profile with a different base, template or OpenCode digest reuses another profile's image. | `ComputeCacheTag` hashes only `profile.Runtimes` (`runtime_resolver.go:188-211`, marshal at :203). The existing test at `runtime_resolver_test.go:365-379` asserts the same tag for a profile with empty Platform, BaseImageRef, TemplateRev and OpenCodeDigest. | Full identity document with a scheme version; label check on every cache hit. |
| T2 | **Tampered or substituted upstream archive** (mirror or CDN compromise, MITM via redirect, a release replaced upstream). | `SHA256: ""` on all six entries (`runtime_catalog.go:24,32,40,48,56,64`); `curl -fsSL ... \| tar` (template :29). | Pinned SHA-256 in the catalog, checked in the Dockerfile with `sha256sum -c`; `--proto '=https'`. |
| T3 | **Silent build failure treated as success**, so a downstream consumer runs a missing or partial image. | The stream is drained to `io.Discard` (`image_builder.go:230-233`) and the tag is returned unconditionally (:235). | Stream parsing, post-build inspect, sanitized error. |
| T4 | **Template tampered on disk.** Whoever can write `templatePath` controls a build that runs as root. | The template is read from an arbitrary path with no integrity check (`image_builder.go:152`; constructor argument at :51). | `go:embed`, a pinned template SHA-256 golden test, and the template hash in the identity. |
| T5 | **Toolchain silently dropped.** A profile requests a runtime and the image lacks it. | An uncataloged runtime falls back to an entry with no URL (`image_builder.go:174-179`); the template skips entries without a URL (`{{if .ArchiveURL}}`, template :27). | Preflight rejects uncataloged runtimes; the conditional is removed; `missingkey=error`. |
| T6 | **Floating base or frontend image.** | `FROM alpine:3.24` (template :2) is a tag, not a digest, and ignores `profile.BaseImageRef`; the resolver hardcodes `"alpine:3.24"` (`runtime_resolver.go:179`); `# syntax=docker/dockerfile:1` (template :1) is a floating frontend. | Base taken from `{{.BaseImageRef}}`, which must match `name@sha256:<64hex>`; the `# syntax` line removed or digest-pinned (Q2). |
| T7 | **Template injection through catalog or profile strings** (newlines, quotes or shell metacharacters in a URL or path). | Values are interpolated raw into `RUN` (template :28-31). | Strict per-field regex validation in preflight. |
| T8 | **Platform mismatch.** A wrong-architecture image is cached under an amd64 identity. | `ImageBuildOptions` sets only Tags and Remove (`image_builder.go:220-223`); no Platform. | `Platform` set on the build; Os and Architecture checked on inspect. |

Trust assumptions: the Docker daemon, and anyone with access to its socket, is root-equivalent and **trusted**. Phase 1B does not defend against a malicious daemon operator. The cache-hit label check is a consistency control, not a security boundary against socket holders.

---

## 3. Design Overview

### 3.1 Image identity document (scheme v2)
A new type in `internal/runner/runtime_types.go`:

```go
type ImageIdentity struct {
    Scheme                   string              `json:"scheme"`          // "atm-runtime-identity/v2"
    Platform                 string              `json:"platform"`        // must be "linux/amd64"
    BaseImageRef             string              `json:"base_image_ref"`  // name@sha256:<64hex>
    OpenCodeDigest           string              `json:"opencode_digest"` // sha256:<64hex>
    TemplateSHA256           string              `json:"template_sha256"` // hex of raw template bytes
    RenderedDockerfileSHA256 string              `json:"rendered_dockerfile_sha256"`
    Runtimes                 map[string]string   `json:"runtimes"`
    Toolchains               []ToolchainIdentity `json:"toolchains"`      // sorted by Runtime
}
type ToolchainIdentity struct{ Runtime, Version, ArchiveURL, SHA256, ExtractPath, ArchiveFormat string }
```

Tag = `atm-runtime:` + hex(sha256(json.Marshal(identity))). The encoding is canonical: struct field order is fixed, `encoding/json` sorts map keys, and a golden test pins the exact bytes.

**Signature decision:** `RuntimeResolver.ComputeCacheTag(profile)` cannot see the template bytes. Replace it with a pure function in a new file, `internal/runner/image_identity.go`:
`ComputeImageIdentity(profile *ResolvedRuntimeProfile, catalog RuntimeCatalog, templateBytes, rendered []byte) (ImageIdentity, string, error)`.
Its only callers are tests (`image_builder_test.go:138,183,241,329`, `runtime_resolver_test.go:351,372`). Recommendation: **remove** `ComputeCacheTag` from the interface entirely, so nobody can compute a partial tag by accident.

### 3.2 Builder order of operations (new `BuildOrGetImage`)
1. `validateProfile(profile)`. Checks the platform, the base digest format and the OpenCode digest format. Every runtime must be in the catalog (no fallback). Every entry must pass `validateCatalogEntry`:
   - SHA-256 is 64 lowercase hex characters and not the pending sentinel.
   - URL starts with `https://` and contains no whitespace, quotes, `$`, backticks or newlines.
   - `ExtractPath` matches `^/usr/local/[a-z]+$`.
   - `ArchiveFormat` is a known value.
2. `loadVerifiedTemplate()`: the embedded bytes must hash to `ExpectedTemplateSHA256`.
3. Render with `template.New(...).Option("missingkey=error").Funcs({"requireSHA256": ...})`.
4. Compute the identity and the tag.
5. **Only now**, check in-flight coalescing, then `ImageList`. On a cache hit, `ImageInspectWithRaw` must show labels `io.atm.runtime.identity == <hex>` and `io.atm.runtime.identity-scheme == v2`. A mismatch returns `ErrImageIdentityMismatch`: fail closed, with no automatic rebuild over the existing image (Q8).
6. Build with `Tags`, `Remove: true`, `ForceRemove: true`, `Platform: "linux/amd64"` and `Labels: {identity, scheme}`. Pin `Version` once Q1 is answered.
7. Run `consumeBuildStream(body)`, then `ImageInspectWithRaw(tag)`. The stream parsing rules are:
   - Streaming JSON must be complete and valid; incomplete or truncated JSON fails closed.
   - An empty stream or stream with no valid JSON objects is rejected.
   - An aux image ID in the stream is optional (BuildKit may emit trace events instead of aux image IDs, or older daemons may differ; reviewer claims that aux is universally mandatory are incorrect and rejected).
   - Valid EOF with no aux ID is accepted IF AND ONLY IF post-build `ImageInspectWithRaw` authoritatively confirms matching identity labels, `Os == "linux"`, and `Architecture == "amd64"` under pinned builder support.
   - If an aux image ID is emitted in the stream, it must match the inspected image ID exactly; any mismatch fails closed.

Steps 1–4 make **zero Docker calls**. Tests prove this with a failing Docker client, as the existing quarantine tests do.

### 3.3 Sentinels
In `internal/runner/runtime_catalog.go`:
```go
const PendingSHA256 = "PENDING-INDEPENDENT-VERIFICATION" // fails ^[0-9a-f]{64}$ by construction
const DefaultBaseImageRef = "alpine:3.24@sha256:" + PendingSHA256 // fails the digest regex
```
New errors, each with fixed text and wrapping a common sentinel: `ErrCatalogEntryUnverified`, `ErrInvalidRuntimeProfile`, `ErrTemplateIntegrity`, `ErrImageBuildFailed`, `ErrImageIdentityMismatch`. Build-stream detail goes to the logs only, truncated to 512 bytes with control characters stripped. The returned error text stays fixed.

### 3.4 Checksum acquisition procedure (no invented values)
Apply this to every catalog entry and to the base image digest:
1. **Primary source:** the upstream signed or official checksum publication. For each artifact:
   - **Go:** go.dev's published SHA-256 list.
   - **Node.js:** `SHASUMS256.txt`, **with its GPG signature verified against the Node release keys**.
   - **Python:** python.org's release-manager signature (Sigstore or GPG) and its published hash.
   - **Java (Adoptium):** the `.sha256.txt` file and its GPG signature.
   - **Rust:** the `.sha256` file, with the `.asc` checked against the Rust signing key.
   - **.NET:** Microsoft's release JSON. Upstream publishes SHA-512, so derive the SHA-256 locally from the verified file.
   - **Base image:** the registry manifest-list digest and the platform manifest digest.
2. **Independent second source:** a separate download over a different network path or by a different operator. Compute `sha256sum` locally and compare it byte for byte with step 1. For the image digest, run a second `docker buildx imagetools inspect` or `crane digest` from a separate host.
3. **Record:** commit a verification record at `docs/superpowers/verification/2026-10-XX-phase-1b-catalog-checksums.md`. It lists the URL, the upstream checksum source URL, the signature key fingerprint, both observed hashes, the operator names, the date and the tool versions.
4. **Approval:** the user, or a user-designated approver who did not fetch the value, signs off in the record before the sentinel is replaced. The agent executing this plan **must not** fetch or fill in checksums itself.
5. Until approval, entries keep `PendingSHA256` and builds are refused with `ErrCatalogEntryUnverified`. This is the intended state at the end of Phase 1B unless AG-2 has run.

---

## 4. Environment and Verification Command

All Go commands run in a throwaway container with no network and a pre-populated, read-only module cache. No Docker socket is mounted, and nothing is built from host state.

> **CRITICAL PRE-REQUISITE NOTICE (AG-0 Prerequisite):**
> The command below contains explicit placeholders (`<APPROVED_DIGEST>`, `atm-gomodcache`). These are **non-runnable prerequisites**, NOT pre-approved or ready-to-run commands.
> - An operator-prepared environment with an actual digest-pinned Go compiler image and populated local read-only module cache volume is required.
> - No agent is authorized to fetch, fabricate, or guess digests or run host docker commands to prepare this environment without explicit AG-0 operator signoff.
> - The presence of `--network none` and omitting docker socket inside the test container provides local unit isolation only; it **does NOT authorize host docker execution** or imply any relaxation of quarantine.

```sh
docker run --rm --network none --read-only --tmpfs /tmp:rw,size=2g \
  --cap-drop ALL --security-opt no-new-privileges:true \
  -v "$PWD":/src:ro -v atm-gomodcache:/go/pkg/mod:ro \
  -e GOFLAGS=-mod=readonly -e GOPROXY=off -e GOTOOLCHAIN=local -e GOCACHE=/tmp/gocache \
  golang@sha256:<APPROVED_DIGEST> \
  sh -c 'cp -a /src /tmp/w && cd /tmp/w && go test -count=1 <PKG> -run "<REGEX>"'
```

`VERIFY(<regex>)` in the tasks below means this command with `<PKG>=./internal/runner/`.

Mutation checks use the same container. Apply each mutation inside `/tmp/w` only (for example with `sed -i` on the copy), run the tests, expect them to **FAIL**, and let the container discard the copy. Never apply a mutation to the worktree.

(Current dev-host note: the existing harness omits `no-new-privileges` because exec fails with it under this host's AppArmor profile. That harness is a test harness, not a product sandbox.)

**Live Docker image builds are never part of VERIFY.** They happen only at approval gates AG-3 and AG-4 (Section 7).

---

## 5. Tasks (TDD)

Every task runs in four phases:
- **RED:** write the tests, run VERIFY, and confirm they fail for the stated reason.
- **GREEN:** implement until VERIFY passes.
- **REGRESSION:** run the full `go test -count=1 ./...` and `-race ./internal/runner/...` in the container.
- **MUTATION:** each listed mutation must turn at least one new test red.

> **Scope Separation Notice:**
> - **Required Now (Implementation/Code & Offline Verification):** Safe builder configuration, engine options, template timeout, robust stream parsing, pointer-compare flight lifecycle (Task 4b), and offline mock-based unit tests.
> - **Deferred / Blocked (Artifact ABI, Live Egress, & Production Environments):** Pinned artifact base digests, live network egress decisions, guest approvals, and production deployment wiring. Do NOT secretly approve embed moves (Q9) or TemplateRev decisions (Q10); these are user decision gates. Recommendations below are proposals only.

### Task 0: Approvals and hygiene
- [ ] The user approves this plan, the throwaway-container command (Section 4), and the blocked decisions on Q1, Q2, Q3, Q9, Q10, and Q11. Note: Recommendations provided in this document are proposals only and do not constitute approval.
- [ ] Record that the worktree contains many unrelated dirty and untracked files. `internal/runner/image_builder*.go`, `runtime_*.go` and `project_sandbox_*` are untracked. The executor stages **only** the files listed in each task's boundaries and preserves all dirty files untouched.
- [ ] Baseline: run the full suite once in the container and record which tests pass and fail, so later regressions can be attributed.

### Task 1: Full-identity cache tag (Defect 1)
**Create:** `internal/runner/image_identity.go`, `internal/runner/image_identity_test.go`.
**Modify:**
- `internal/runner/runtime_types.go`: add `ImageIdentity`, `ToolchainIdentity`, and an `ArchiveFormat` field on `RuntimeCatalogEntry`.
- `internal/runner/runtime_resolver.go`:
  - remove `ComputeCacheTag` (:21 and :188-211);
  - replace the hardcoded `BaseImageRef: "alpine:3.24"` at :179 with `DefaultBaseImageRef`;
  - derive `TemplateRev` from `ExpectedTemplateSHA256` or drop it (Q10).
- `internal/runner/runtime_resolver_test.go`: rewrite subtest "8" (:338-379) and update :67-68.

- [ ] **RED tests:**
  - `TestImageIdentity_EachFieldChangesTag`: table-driven, starting from a valid base identity. Mutate each of these and assert the tag changes every time:
    - Platform, BaseImageRef, OpenCodeDigest, TemplateSHA256 and RenderedDockerfileSHA256;
    - one runtime version;
    - adding or removing a runtime;
    - each toolchain field (URL, SHA256, ExtractPath, ArchiveFormat).
  - `TestImageIdentity_DeterministicAcrossMapOrder`: identical inputs built with different map insertion orders give the same tag, over 100 repetitions.
  - `TestImageIdentity_GoldenCanonicalBytes`: pins the JSON bytes and tag for a fixture. The fixture uses test-only fake hex values, clearly labelled as fixtures and not real checksums.
  - `TestImageIdentity_SchemeVersionPresent`: a v2 tag differs from the legacy runtimes-only hash for the same runtimes. This guards against reusing old images.
  - Remove the assertion at `runtime_resolver_test.go:376` that profiles differing in non-runtime fields share a tag, and replace it with the inverse.
- [ ] **GREEN:** implement `ComputeImageIdentity` as described in §3.1.
- [ ] **Acceptance:** no tag can be computed without both the template bytes and the rendered bytes; `grep -rn ComputeCacheTag internal/` finds nothing.
- [ ] **MUTATION:** each of these must fail at least one test:
  - (a) drop `Platform` from the struct (EachField fails);
  - (b) hash only `Runtimes`;
  - (c) skip sorting `Toolchains` (the determinism test fails).
- [ ] VERIFY(`TestImageIdentity|TestRuntimeResolver`).

### Task 2: Catalog checksum and format preflight (Defect 2)
**Create:** `internal/runner/image_preflight.go`, `internal/runner/image_preflight_test.go`.
**Modify:**
- `internal/runner/runtime_catalog.go`:
  - replace `SHA256: ""` at :24, :32, :40, :48, :56 and :64 with `PendingSHA256`;
  - add an `ArchiveFormat` (`"tar.gz"` or `"tar.xz"`) matching each URL suffix. The node URL at :31 is `.tar.xz`.
- `internal/runner/runtime_catalog_test.go`.

- [ ] **RED tests:**
  - `TestValidateCatalogEntry_RejectsPendingEmptyMalformed`. Rejected values:
    - `""` and `PendingSHA256`;
    - 63 or 65 hex characters;
    - uppercase hex;
    - a `sha256:` prefix;
    - non-hex characters;
    - leading or trailing whitespace.
  - `TestValidateCatalogEntry_RejectsUnsafeURLAndPath`:
    - URLs rejected: `http://`, an empty URL, and URLs containing `"`, `'`, `` ` ``, `$`, `\n`, a space or `;`;
    - ExtractPath values rejected: `/`, `/usr/local/../etc`, `/usr/local/go;rm`, and relative paths.
  - `TestValidateCatalogEntry_ArchiveFormatMatchesURL`: a `.tar.xz` URL with format `tar.gz` is rejected.
  - `TestDefaultCatalog_AllEntriesFailClosedUntilVerified`: iterates over `List()`. Every entry is either a valid 64-hex value backed by the verification record (enforced at AG-2 review) or exactly `PendingSHA256`. **No empty strings.**
  - `TestValidateProfile_RejectsUncatalogedRuntime`: covers the fallback at `image_builder.go:174-179`.
  - `TestValidateProfile_RejectsUnpinnedBaseAndDigest`. Rejected:
    - `alpine:3.24` and `alpine@sha256:PENDING...`;
    - an empty OpenCode digest (this removes the silent default at `image_builder.go:182-185`);
    - any Platform other than `linux/amd64`.
- [ ] **GREEN:** implement `validateCatalogEntry`, `validateProfile`, the sentinels and the errors.
- [ ] **Acceptance:** with the default catalog as shipped (all entries pending), preflight refuses every profile.
- [ ] **MUTATION:** each of these must fail at least one test:
  - (a) relax the regex to `^[0-9a-fA-F]{64}$` (the uppercase test fails);
  - (b) treat an empty value as "skip verification";
  - (c) restore the uncataloged fallback.
- [ ] VERIFY(`TestValidateCatalogEntry|TestValidateProfile|TestDefaultCatalog|TestRuntimeCatalog`).

### Task 3: Template integrity and verified downloads (Defects 3 and 5)
**Move:** `deploy/sandbox/Dockerfile.project.template` to `internal/runner/templates/Dockerfile.project.template`, because `go:embed` cannot reference `../deploy` (alternative in Q9).
**Create:** `internal/runner/image_template.go`, `internal/runner/image_template_test.go`.
**Modify:** `internal/runner/image_builder.go`: remove `templatePath` (:43, :51, :152) and change the constructor signature.

Template changes, by current line number:
- :1: remove `# syntax=docker/dockerfile:1`, or pin it by digest (Q2).
- :2: `FROM {{.BaseImageRef}}`.
- :27 and :32: remove `{{if .ArchiveURL}}` and its `{{end}}`. Every toolchain becomes mandatory.
- :28-30: replace with:
  ```
  RUN set -eu; \
      mkdir -p {{.ExtractPath}}; \
      curl --proto '=https' --tlsv1.2 -fsSL -o /tmp/atm-toolchain.archive "{{.ArchiveURL}}"; \
      echo "{{requireSHA256 .SHA256}}  /tmp/atm-toolchain.archive" | sha256sum -c -; \
      tar -x{{tarFlag .ArchiveFormat}}f /tmp/atm-toolchain.archive -C {{.ExtractPath}} --strip-components=1; \
      rm -f /tmp/atm-toolchain.archive; \
      chown -R root:root {{.ExtractPath}}
  ```
  `tarFlag` maps `tar.gz` to `z` and `tar.xz` to `J`; anything else is an error. The `echo | sha256sum -c` pipeline is safe because `sha256sum` is its last command, so its exit status is the pipeline's.
- Remove the unused `Runtimes` map from `TemplateData` (`image_builder.go:67`), or keep it only if tests need it.

- [ ] **RED tests:**
  - `TestEmbeddedTemplate_MatchesPinnedSHA256`: `sha256(embedded) == ExpectedTemplateSHA256`. The constant is updated deliberately, in the same reviewed commit as any template change.
  - `TestLoadVerifiedTemplate_RejectsTamperedBytes`: an injected template with a modified byte returns `ErrTemplateIntegrity` before `template.Parse`.
  - `TestRenderedDockerfile_NoPipeToTar`: no rendered line matches `curl[^\n]*\|\s*tar`.
  - `TestRenderedDockerfile_ChecksumPrecedesExtract`: for every toolchain, `sha256sum -c` appears before `tar -x` in the same RUN, and the rendered SHA equals the catalog value.
  - `TestRenderedDockerfile_FromUsesPinnedBase`: the `FROM` line equals `FROM <BaseImageRef>` and contains `@sha256:`.
  - `TestRenderedDockerfile_NoFloatingSyntaxDirective`.
  - `TestRender_FailsClosedOnInvalidSHA`: bypasses preflight and calls the template function directly. Rendering errors for `""` and for `PendingSHA256`.
  - `TestRender_MissingKeyErrors`.
  - `TestRender_EveryRequestedToolchainPresent`: N runtimes in, N `# Toolchain:` blocks out.
  - Remove the fixture template `setupTestTemplate` (`image_builder_test.go:108-122`). Tests instead use the embedded template with a fake-but-valid fixture catalog, injected through a test-only constructor in an internal test file: `newRuntimeImageBuilderForTest(cli, catalog, templateBytes, expectedSHA)`.
- [ ] **GREEN:** implement `go:embed`, the hash check, `missingkey=error`, the template functions and the template edits.
- [ ] **MUTATION:** each of these must fail at least one test:
  - (a) restore `curl | tar` (NoPipeToTar fails);
  - (b) restore `{{if .ArchiveURL}}` (EveryRequestedToolchainPresent fails with a fixture that has no URL);
  - (c) make `requireSHA256` return its input unchanged;
  - (d) skip the template hash check (the tampered-bytes test fails);
  - (e) hardcode `FROM alpine:3.24`.
- [ ] VERIFY(`TestEmbeddedTemplate|TestLoadVerifiedTemplate|TestRender`).

### Task 4: Build-stream parsing and post-build verification (Defect 4)
**Create:** `internal/runner/build_stream.go`, `internal/runner/build_stream_test.go`.
**Modify:** `internal/runner/image_builder.go` (:219-235).

`consumeBuildStream(r io.Reader, maxBytes int64) (auxImageID string, err error)`:
- Decodes with a `json.Decoder` over `io.LimitReader(r, maxBytes+1)` into `jsonmessage.JSONMessage` from `github.com/docker/docker/pkg/jsonmessage`, which the module already includes at v27.5.1.
- Fails closed on any of:
  - `Error != nil`;
  - a non-empty `ErrorMessage` (deprecated, but still emitted);
  - a decode error or incomplete/truncated JSON stream;
  - empty stream or stream containing no JSON messages;
  - exceeding the byte cap;
  - context cancellation.
- Records the aux `{"ID":"sha256:..."}` if one is present.
- Note on aux: aux is NOT universally emitted across builder implementations (e.g. BuildKit emits `moby.buildkit.trace` events rather than `{"ID":"..."}`). Streams with valid JSON and clean EOF without aux ID are valid at the stream level; downstream image verification requires post-build `ImageInspectWithRaw` authoritatively confirming identity labels, Os, and Architecture. If an aux ID is emitted, it MUST match the post-build inspected image ID.

- [ ] **RED tests** (stream fixtures are hand-written JSON lines):
  - `TestBuildStream_ErrorDetailFails` and `TestBuildStream_DeprecatedErrorFieldFails`, each on its own.
  - `TestBuildStream_ErrorAfterSuccessfulStepsFails`: the error is the last line, after many `stream` lines.
  - `TestBuildStream_MalformedJSONFails` and `TestBuildStream_TruncatedMidObjectFails`.
  - `TestBuildStream_OversizeFails`: input is the cap plus one byte.
  - `TestBuildStream_EmptyStreamRejected`: empty stream or stream with no JSON rejects with error.
  - `TestBuildStream_ValidLegacyAuxID`: parses legacy stream with aux ID correctly.
  - `TestBuildStream_ValidBuildKitTrace`: parses BuildKit trace stream with clean EOF and no aux ID without error.
  - `TestBuildStream_IncompleteJSONErrors`: truncated JSON stream mid-message returns decode/truncation error.
  - `TestBuildStream_ReaderErrorPropagates`: an `io.Reader` that returns a non-EOF error.
  - `TestBuildStream_ErrorTextSanitized`: `error.Error()` is the fixed text; the logged detail is at most 512 bytes and has control and ANSI characters stripped.
  - Builder level:
    - `TestBuilder_BuildStreamErrorReturnsNoTag`: the mock `ImageBuild` returns an `errorDetail` body. `BuildOrGetImage` returns `""` with `errors.Is(err, ErrImageBuildFailed)`.
    - `TestBuilder_PostBuildInspectLabelMismatchFails`.
    - `TestBuilder_PostBuildInspectWrongArchFails`.
    - `TestBuilder_PostBuildAuxIDMismatchFails`: aux ID present in stream but differs from inspect fails closed.
    - `TestBuilder_PostBuildMissingAuxInspectSucceeds`: valid stream without aux succeeds if inspect matches labels/Os/Arch under pinned builder support.
    - `TestBuilder_PostBuildMissingAuxInspectFails`: stream without aux fails if inspect labels or architecture fail.
    - `TestBuilder_PostBuildInspectErrorFails`.
    - `TestBuilder_BuildOptionsPinned`: `Platform == "linux/amd64"`, the identity labels are present, `Remove` and `ForceRemove` are set, and `Version` matches the gated builder decision.
  - Update the existing tests at `image_builder_test.go:168-225`, `227-313` and `315-399`. Their mocks return an empty body (`io.LimitReader(nil,0)`), and the default `ImageInspectWithRaw` (:82-84) returns no labels. They must now supply a valid inspect response; without it they correctly fail.
- [ ] **GREEN:** implement stream parsing and the post-build inspect.
- [ ] **MUTATION:** each of these must fail at least one test:
  - (a) restore `io.Copy(io.Discard, ...)` (the ErrorDetail tests fail);
  - (b) check only `Error` and not `ErrorMessage` (the deprecated-field test fails);
  - (c) remove the inspect (the label-mismatch test fails);
  - (d) ignore decode errors (the malformed-JSON test fails);
  - (e) accept empty stream (empty stream test fails).
- [ ] VERIFY(`TestBuildStream|TestBuilder_`).

### Task 4b: Safe in-flight lifecycle, bounded build timeout, and cancellation (Defect 7 / Risk 7)
**Modify:** `internal/runner/image_builder.go`, `internal/runner/image_builder_test.go`.

**Architecture & Safety Requirements:**
- **Eliminate unbounded `context.Background()`**: Replace raw `context.Background()` with a bounded timeout context using an approved, configurable default timeout (proposal: 15 minutes default, subject to user/operator decision in Q11, never silently accepted).
- **Safe inFlight cancellation without pointer-drop race**: When all waiters cancel (`waiters == 0`), do NOT delete `b.inFlight[tag]` by tag name alone. Any map deletion on cancellation or completion MUST compare the inFlight pointer (`if b.inFlight[tag] == flight { delete(b.inFlight, tag) }`) under `b.mu.Lock()`.
- **Accurate waiter accounting**: Under `flight.mu`, increment `waiters` on join, decrement on caller cancel. If `waiters == 0`, cancel underlying build context AND safely remove flight from map under `b.mu` via pointer comparison so subsequent callers do not join an abandoned/cancelled flight.
- **Old completion must not kill newer flight**: When an old or cancelled background goroutine finishes, its completion cleanup (`flight.res = ...; close(flight.doneCh)`) must NOT delete a replacement `inFlight[tag]` if a new flight was started for that tag. Deletion must check pointer identity under mutex.
- **Old done closes don't kill replacement**: Closing `flight.doneCh` signals only the waiters of that specific flight instance.
- **Deadlock-free and clean body teardown**: No mutex holds across stream reading or cancellation callbacks. Cancellation must close or abort the build stream response body cleanly.

- [ ] **RED tests:**
  - `TestBuilder_InFlight_DeterministicBlockedFakeStream`: Mock stream blocks indefinitely; verify inFlight coalescing and correct initial waiter count.
  - `TestBuilder_InFlight_CancelLastWaiterCleansMap`: When all active waiters cancel, the build context is cancelled AND the inFlight map entry is removed under mutex, preventing future callers from joining an abandoned flight.
  - `TestBuilder_InFlight_NewCallerStartsFreshAfterCancel`: A new caller arriving after previous flight is cancelled starts a fresh build rather than inheriting `context.Canceled`.
  - `TestBuilder_InFlight_OldCompletionDoesNotRemoveNewFlight`: Trigger replacement flight while old goroutine completes slowly; verify old completion does not delete the new flight pointer from `b.inFlight`.
  - `TestBuilder_InFlight_FirstWaiterCancelOtherContinues`: Multiple waiters join; when the first waiter's context is cancelled, remaining waiters still receive the completed build result without interruption (waiter count accurately decrements to 1, build continues).
  - `TestBuilder_InFlight_ConfigurableTimeoutTriggers`: Verify build context expires when bounded timeout is exceeded.
  - `TestBuilder_InFlight_CancelClosesBody`: Cancelling build context aborts stream and closes reader body without leaking goroutines or connections.
  - `TestBuilder_InFlight_NoDeadlockOnConcurrentCancelAndFinish`: High-concurrency race test verifying no deadlocks between cancellation callbacks and build completion under `-race`.
- [ ] **GREEN:** implement pointer-compare deletion, waiter lifecycle, cancellation cleanup, and bounded timeout context in `internal/runner/image_builder.go`.
- [ ] **MUTATION:** each of these must fail at least one test:
  - (a) delete map entry by tag without pointer comparison (old completion test fails);
  - (b) leave map entry intact when waiters drop to 0 (new caller after cancel fails);
  - (c) use raw unbounded context without timeout (timeout test fails);
  - (d) cancel underlying build when `waiters > 0` on first waiter exit (other continues test fails).
- [ ] VERIFY(`TestBuilder_InFlight`).

### Task 5: Preflight ordering and cache-hit verification (ties G2 and G6 together)
**Modify:** `internal/runner/image_builder.go` (:70-130), `internal/runner/image_builder_test.go`.

- [ ] **RED tests:**
  - `TestBuilder_PreflightFailureMakesZeroDockerCalls`: a strict failing Docker client, using the same pattern as `project_sandbox_quarantine_internal_test.go:25-112`, with the default catalog as shipped (all `PendingSHA256`). Expects `ErrCatalogEntryUnverified` and zero Docker calls.
  - `TestBuilder_TemplateIntegrityFailureMakesZeroDockerCalls`.
  - `TestBuilder_CacheHitRequiresMatchingLabel`: `ImageList` returns the tag, but inspect returns a wrong or missing label. Expects `ErrImageIdentityMismatch`, and `ImageBuild` is never called.
  - `TestBuilder_CacheHitWithMatchingLabelReturnsTag` (replaces :124-166).
  - The coalescing (:227) and detached-cancel (:315) concurrency tests keep passing.
- [ ] **GREEN:** reorder the builder steps per §3.2.
- [ ] **MUTATION:** each of these must fail at least one test:
  - (a) move `ImageList` before preflight (ZeroDockerCalls fails);
  - (b) skip the label check on a cache hit.
- [ ] VERIFY(`TestBuilder_|TestRuntimeImageBuilder_`).

### Task 6: Quarantine regression lock and documentation
**Files:**
- `docs/superpowers/plans/2026-10-10-runtime-image-configuration.md`: status note on Task 3, line 283.
- `docs/superpowers/plans/2026-10-13-runtime-safety-repairs.md`: mark the Phase 1B items "implemented, quarantine still active".

No Go changes.
- [ ] Re-run these tests; all must still pass unchanged:
  - `TestEnsureProjectContainer_Quarantine*`;
  - `TestStopProjectContainer_UnchangedByQuarantine`;
  - the quarantine tests in task_runner, workspace_manager, task_service and worker, and the `quarantineguard` package.
- [ ] `git diff --stat` shows **no** changes to `project_sandbox_manager.go`, `task_runner.go`, `workspace_manager.go` or `cmd/server/main.go`.
- [ ] In the container, all pass: the full suite with race, `go vet ./internal/runner/...`, `go build ./cmd/server`, and `git diff --check`.

---

## 6. Test Plan Summary
- **Unit:** Tasks 1–5 (including mandatory Task 4b) as listed. All Docker interaction is mocked. Fixture checksums are obviously synthetic (for example `strings.Repeat("ab", 32)`) and appear only in `_test.go` files.
- **Negative, fail-closed:** the shipped catalog with a pending base digest is refused with zero Docker calls (Task 5). This is the expected production state until AG-2.
- **Mutation:** about 25 named mutations across Tasks 1–5 (including Task 4b), and each must produce at least one red test. Record the results in the PR as a table mapping each mutation to its failing test. A mutation that turns nothing red blocks merge until a test is added.
- **Race:** `go test -race ./internal/runner/...`. The builder has a mutex and an in-flight map.
- **Not covered by Phase 1B tests:**
  - real network downloads;
  - real daemon behaviour;
  - BuildKit vs legacy builder stream formats (AG-3);
  - whether the toolchains actually work inside the image (AG-4).

---

## 7. Rollout and Approval Gates

| Gate | What | Who approves | Notes |
|---|---|---|---|
| AG-0 | Plan approval and decisions on Q1, Q2 and Q9 | User | Before Task 1. |
| AG-1 | Code review of Tasks 1–6 by an independent, security-focused reviewer | User and reviewer | Unit and mutation evidence attached. **Not readiness evidence.** |
| AG-2 | Checksum and base-digest population per §3.4 | User, or a designee who is not the fetcher | A separate commit that only replaces `PendingSHA256` values and adds the verification record. Agents do not fetch values. |
| AG-3 | **Live image build** on the dedicated qualified guest (the Gate 0A host) ONLY, not the dev host. One negative build with a deliberately wrong SHA, which must fail with `ErrImageBuildFailed`, and one positive build. Approved for execution on AG-3 qualified guest ONLY. | **Explicit separate operator approval** | Q3 egress decision and approval must precede any live build. Build-time egress is NOT authorized by default. Record which stream format was observed. |
| AG-4 | In-image toolchain smoke test (`go version`, `node --version`, …) under `--network none --read-only --cap-drop ALL --security-opt no-new-privileges:true` | Explicit operator approval | Expected to expose the Q4–Q6 failures. A pass is still **not** readiness. |
| AG-5 | Decision to lift the quarantine | User | Out of scope; requires Phase 1B, 0C, 0D, 0G and 0F. |

**Sequencing with other plans:**
- Two plans share `task_runner.go` and `cmd/server/main.go`:
  - `2026-10-11-deployment-and-hardening.md`: Task 2 (removing `runOnHost` from `task_runner.go`) and Task 3 (preflight wiring in `cmd/server/main.go`);
  - `2026-10-10-runtime-image-configuration.md`: Tasks 6 and 7 (`task_runner.go`, `cmd/server/main.go`).
- Phase 1B touches **neither** file, so it can run in parallel with both plans.
- The reverse constraint: whichever plan later wires `NewRuntimeImageBuilder` into `cmd/server/main.go` (runtime plan Task 6) must use the **new** constructor signature from Task 3 and must land after Phase 1B. It must not edit `main.go` concurrently with hardening Task 3; only one owner at a time.
- Phase 1B changes the `NewRuntimeImageBuilder` signature and removes `RuntimeResolver.ComputeCacheTag`. Any in-flight branch that references either must rebase.
- Phase 1B must land after Phase 1A, because it relies on the quarantine staying in place. It must not modify Phase 1A's quarantine tests.

---

## 8. Risks
1. **Every build fails until AG-2.** This is intentional (fail closed). Mitigation: clear documentation, and the quarantine already blocks execution.
2. **The catalog may be unusable even with correct checksums.** Checksums prove the bytes are authentic, not that they work. These entries will surface at AG-4 (Q4–Q6):
   - the python URL (`runtime_catalog.go:39`) is a *source* tarball;
   - the rust dist tarball (:55) needs `install.sh`, not `--strip-components` extraction;
   - node, java, dotnet and rust are glibc builds on an Alpine/musl base (template :2, with only `gcompat` at :14).
3. **Stream formats differ.** Through the API, BuildKit emits a `moby.buildkit.trace` aux message, not `{"ID":...}`. Mitigation: success rests on the inspect and label check, the aux ID is compared only when present, and the builder version is pinned per Q1.
4. **The pinned template hash is self-referential** (it lives in the same repo). It adds review friction and runtime tamper detection (via embed), not protection against a malicious committer. Code review remains the control.
5. **The `apk add` index floats** (template :5-14). The image is not byte-reproducible, and the identity does not cover package versions. This is a residual risk until Q7.
6. **Build-time egress unconfined on default daemons.** Standard daemon builds run root shell steps with default network bridge access. Checksums limit what is extracted, but not what a compromised base image or package could reach during the build. Quarantine does NOT confine privileged daemon builds. Consequently, no build-time egress residual risk is accepted in Phase 1B; resolution of Q3 requires explicit host operator egress decision and approval before any live builds occur on AG-3.
7. **In-flight lifecycle and timeout bug (Addressed in Task 4b).** Pre-existing implementation issues:
   - after every waiter cancels (`image_builder.go:142-145`), deleting by tag alone risks racing with replacement flights, or leaving orphaned entries;
   - builds used unbounded `context.Background()`.
   Task 4b addresses this via pointer-comparison map deletion, accurate waiter counts under mutex, and bounded approved timeout contexts.
8. **The worktree is dirty.** There are many unrelated modified and untracked files, and they could be staged by accident. Mitigation: per-task file boundaries, and `git add` with explicit paths only.

---

## 9. Open Questions for the User
- **Q1 (UNDECIDED GATE):** Pin `ImageBuildOptions.Version` to `BuilderV1` or to `BuilderBuildKit`?
  - **Status:** **UNDECIDED GATE (no default chosen).** Both options have deprecation and operational caveats: `BuilderV1` is deprecated by upstream Docker and may lose compatibility in future releases (e.g. Docker 29+); `BuilderBuildKit` requires session management protocols, emits traces rather than aux IDs, and lacks drop-in parity without session plumbing.
  - Automatic fallback or guessing Docker 29 compatibility is strictly rejected. Unit tests using mocks do not constitute qualification.
  - Decision must be explicitly made by user/host operator prior to live build tests; live testing is gated to qualified guest approved at AG-3 ONLY.
  - Recommendation for consideration only (not approval): verify inspect-based identity parsing against both stream formats offline before selecting a builder version.
- **Q2:** Remove `# syntax=docker/dockerfile:1`, or pin it as `docker/dockerfile:1@sha256:<verified>`? (Recommendation only: removal avoids floating syntax resolver).
- **Q3 (UNDECIDED GATE - HOST OPERATOR APPROVAL REQUIRED):** Which model for build-time egress?
  - **Status:** **UNDECIDED GATE.** Quarantine does NOT confine privileged daemon builds. No residual build-time network egress risk is accepted in this plan.
  - Offline tests running with `--network none` and no mounted socket do NOT authorize launching host Docker builds.
  - Any live build requires explicit host operator egress decision and approval prior to execution.
  - Options for operator review:
    - Control-plane prefetch: ATM downloads and verifies toolchain archives in Go under controlled network egress, packages them in context, and builds with `NetworkMode: "none"`. (Requires pre-baked base image to avoid floating `apk add`).
    - Operator-managed egress containment: Host network namespace / daemon firewall policy configured entirely by host operator.
- **Q4:** Python: switch to a relocatable binary distribution, build from source inside the image, or use the Alpine package? The choice changes the URL, the checksum source and the layout.
- **Q5:** Rust: run `install.sh --prefix` instead of `--strip-components`, or use the musl target tarball?
- **Q6:** Gate 0B base OS: stay on Alpine and source musl builds of node, java and dotnet where they exist, or move to Debian with an OpenCode multi-stage copy? Phase 1B is neutral on this, but the AG-2 checksums depend on the artifacts chosen, so decide **before** AG-2.
- **Q7:** Pin apk package versions (`pkg=x.y.z-rN`) with a snapshot repository now, or later? And should package versions be part of the identity document?
- **Q8:** On a cache-hit label mismatch: fail closed and require an operator `docker rmi` (recommended), or rebuild and retag automatically?
- **Q9 (USER DECISION):** Template location: move it into `internal/runner/templates/` for `go:embed` (recommended proposal only, NOT secretly approved), or keep it in `deploy/sandbox/` with a runtime path and a pinned-hash check?
- **Q10 (USER DECISION):** Drop `TemplateRev` (`runtime_types.go:8`), or set it to the template's SHA-256? (Recommendation proposal only, NOT secretly approved).
- **Q11:** In-flight cancellation safety and bounded timeout: Resolved in plan via mandatory Task 4b; timeout duration default proposed (15m) subject to user confirmation.
- **Q12:** Who is the designated independent approver for the AG-2 checksum records?

---

## Appendix: file:line facts relied on
1. `internal/runner/runtime_resolver.go:188-211`: `ComputeCacheTag` marshals only `profile.Runtimes` (:203) and hashes the result (:208-209).
2. `internal/runner/runtime_resolver_test.go:365-379`: `profile2` sets only `Runtimes`, yet the test asserts `tag1 == tag2` against the fully populated `profile1` (:376). The test encodes Defect 1 and must be inverted.
3. `internal/runner/runtime_resolver.go:177-183`: the resolver hardcodes `Platform "linux/amd64"`, `BaseImageRef "alpine:3.24"` (a tag, not a digest; :179), `TemplateRev "v1"` and `DefaultOpenCodeDigest`. `runtime_resolver_test.go:67-68` asserts `alpine:3.24`.
4. `internal/runner/runtime_catalog.go:24,32,40,48,56,64`: `SHA256: ""` on all six entries. The node URL (:31) is `.tar.xz`, the python URL (:39) is a source tarball, and rust (:55) is a dist tarball.
5. `deploy/sandbox/Dockerfile.project.template`:
   - :1: a floating `# syntax` directive;
   - :2: `FROM alpine:3.24`, ignoring BaseImageRef;
   - :5-14: a floating `apk add`;
   - :27: `{{if .ArchiveURL}}` silently skips toolchains;
   - :29: `curl -fsSL "{{.ArchiveURL}}" | tar -xz`, which uses the gzip flag even for node's `.xz`.
6. `internal/runner/image_builder.go:152`: the template is read from `b.templatePath` with no integrity check. :157 parses it without `missingkey=error`.
7. `internal/runner/image_builder.go:172-179`: an uncataloged runtime falls back to an entry with no URL or SHA. :182-185: an empty OpenCode digest silently defaults, *after* the tag has been computed (:76).
8. `internal/runner/image_builder.go:220-223`: `ImageBuildOptions` sets only `Tags` and `Remove`; no Platform, Labels or Version. :230-233 is `io.Copy(io.Discard, resp.Body)`, and :235 is an unconditional `return tag, nil`.
9. `internal/runner/image_builder.go`:
   - :91-106: a cache hit trusts any image whose RepoTags match the tag, with no label or inspect check;
   - :109: the build context is `context.Background()`, with no timeout;
   - :142-145: cancelling when the waiter count reaches 0 leaves the map entry in place until the goroutine finishes.
10. `internal/runner/project_sandbox_manager.go:73-75`: `EnsureProjectContainer` unconditionally returns `ErrQuarantineActive`. The quarantine tests are at `project_sandbox_quarantine_internal_test.go:123-222`.
11. No non-test code calls `NewRuntimeImageBuilder` or `BuildOrGetImage` (grep over `cmd/` and `internal/`).
12. `github.com/docker/docker v27.5.1+incompatible` (go.mod:7) provides:
    - `pkg/jsonmessage.JSONMessage`, with `Error *JSONError` (`errorDetail`), `ErrorMessage` (`error`, deprecated) and `Aux`;
    - `types.ImageBuildOptions`, with `Platform`, `Labels`, `Version`, `PullParent` and `NoCache`.
13. `internal/sandbox/docker.go:31-33`: the `DockerClient` interface already exposes `ImageBuild`, `ImageList` and `ImageInspectWithRaw`; no interface change is needed.
14. `internal/runner/image_builder_test.go`: the mock at :71 and the per-test mocks return empty build bodies, and :82-84 returns an empty inspect.
15. Spec `docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md` §4.1 requires:
    - a digest-pinned base image;
    - pinned OS packages;
    - checksum-verified archives;
    - a fingerprint covering the platform, base digest, template revision, package checksums, OpenCode digest and runtime map.
16. Untracked: `image_builder*.go`, `runtime_*.go`, `project_sandbox_*`, `workspace_manager*.go` and the template. Modified: `task_runner.go`, `cmd/server/main.go`.
17. None of the specs for gates 0D, 0F or 0G mention build-time egress, hence Q3.
