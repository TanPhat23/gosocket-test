# Gate 0G Implementation Plan: Model Credential Isolation & OpenCode Password Lifecycle

**Status:** Stage 2 library implemented (`internal/gateway`, not wired). Gate 0G remains BLOCKED / UNQUALIFIED. Phase 1A quarantine stays active. This plan is not a security sign-off.
**Spec:** `docs/superpowers/specs/2026-10-11-gate-0g-model-credential-isolation-design.md`
**Method:** TDD (red, minimal change, green, mutation check). All Go tests run in `atm-guest`; never on the host; never touch the host Docker daemon.

## Stage 0: done (committed)

| Commit | Change | Evidence |
|---|---|---|
| `15ed300` | Secret tar entry owned 1000:1000, mode 0400 (was root-owned, unreadable by `opencode`). Tests pin: no password / `*_API_KEY` / host profile bind in container config. | `project_sandbox_secret_internal_test.go`; mutation (password back in env) killed |
| `951d4d7` | `runInDocker`: removed host OpenCode config/data bind mounts; `scrubSandboxEnv` drops `*_API_KEY` and `*PASSWORD*`. | `sandbox_credential_isolation_internal_test.go`; mutation killed |
| `4e29ca5` | Hash-pinned `atm-entrypoint.sh` (waits for secret, loads into own env, unlinks, fails closed), added to build context; `/run/secrets` tmpfs uid/gid 1000; secret copied AFTER start (a stopped container's tmpfs discards copies); template ENTRYPOINT/CMD, hash re-pinned. | `entrypoint_*_test.go`, `project_sandbox_secret_internal_test.go`; tmpfs mutation killed |

**Unverified (no Docker in the sealed guest):** that `CopyToContainer` into a running container's tmpfs works with `ReadonlyRootfs`; that OpenCode 2.0.26 accepts `serve --hostname 0.0.0.0 --port 4096` and reads `OPENCODE_PASSWORD`. Both need a live-daemon probe (a disposable VM like the former `atm-build`), not the host daemon.

**Residual risk (spec §7.5):** any process running as UID 1000 in the container can read the password from `/proc/<pid>/environ` of the OpenCode process. Stage 0 only removes Docker-inspect, argv, log, image-layer and volume exposure. It does not make the password secret from task code.

## Blockers (must be decided before Stage 2+)

| ID | Blocker | Owner/decision needed |
|---|---|---|
| B1 | Gate 0D must qualify a narrow `sandbox bridge -> gateway:8082` route; today's rules drop it (SEC-0G-03). | Operator / Gate 0D |
| B2 | Provider-key store and encryption at rest not selected (spec §8.6). | User |
| B3 | Pricing catalog + tokenizer for atomic budget reservation (SEC-0G-01); fallback is a request/token quota with fail-closed on unboundable models. | User |
| B4 | TLS design for sandbox->gateway (cert issuance, trust store in image). | User |
| B5 | OpenCode 2.0.26 custom `baseURL` and SSE behaviour unverified (§8.1, §8.2). | Live probe |
| B6 | Same-UID password exposure: accept, or run task commands as a different UID / PID namespace than the OpenCode daemon. | User |

## Blocker decisions (user, 2026-10-15)

| ID | Decision | Consequence |
|---|---|---|
| B6 | **Accepted risk.** Same-UID password exposure stays; the agent already holds the OpenCode session. | Closed. Documented, not mitigated. Task code can read the OpenCode password. |
| B2 | Provider key comes from server env/secret via `Config.Key`, read per request (rotation without restart). No DB key store. | Closed for v1. |
| B3 | Per-attempt request count + token ceiling; no pricing table. Reservation is `len(body) + clamped max_tokens`, never refunded. | Closed for v1. Not a dollar cap; over-counts rather than under-counts. |
| B4 | Plain HTTP on the dedicated sandbox bridge (deviates from spec §5/PROBE-18, which require TLS). | Accepted risk: capability tokens cross the bridge in cleartext. Only safe if B1 keeps the route narrow (sandbox bridge -> gateway only). Upstream (gateway -> provider) is https-only and enforced in `NewHandler`. |
| B1 | Still open: host operator / Gate 0D must allow only `sandbox bridge -> gateway:8082`. ATM must not touch firewall rules. | **Blocks Stage 3 and any gate qualification.** |
| B5 | Still open: needs a live probe of OpenCode 2.0.26 (custom `baseURL`, SSE, `serve --hostname 0.0.0.0`, tmpfs `CopyToContainer`) in a disposable VM with Docker. | **Blocks Stage 3.** |

## Stage 1: remaining no-blocker hardening

1. DONE: removed unused `OpenCodeDataDir` / `OPENCODE_DATA_DIR`. `OpenCodeConfigDir` stays: `config.go` reads `service.json` from it.
2. DONE: static guard `TestCredentialNamesOnlyInReviewedPlaces` (AST scan of `internal/runner` and `internal/sandbox`; new credential-name literals need a reviewed allowlist entry).
3. DONE: `TestProjectPassword_NeverLoggedOrSerialized` (log and build-log sinks, JSON of the route handle).
4. DONE: `TestProjectPassword_UniqueAndDiscardedOnStop`.

Mutants killed: new `FOO_API_KEY` literal in `internal/sandbox`; password logged in the provisioning draft; fixed (non-random) password.

## Stage 2: gateway core (`internal/gateway`): DONE as an unwired library

Each item is test-first; probes map to spec §10.
1. Capability token: HMAC-signed `{project_id, task_id, attempt_id, exp}`, server-side revocation set; reject forged/expired/revoked/cross-project (0G-PROBE-09/10/11).
2. Request validation: fixed provider/model allowlist, no caller-supplied destination, strip inbound `Authorization`/`x-api-key`/`proxy-authorization`, body size cap, `max_tokens` clamp (PROBE-07).
3. Upstream client: no redirects (3xx is terminal), strict TLS, key injected from the store, never logged (PROBE-08/13/15).
4. Streaming: cancel upstream on client abort; idle timeout (PROBE-14).
5. Budget: atomic reservation and reconciliation; fail closed when unbounded (PROBE-12).
6. ~~TLS-only listener (PROBE-18)~~: dropped per B4 decision; the listener is not part of this package.

Implemented: `token.go` (HMAC capability tokens, revocation), `budget.go` (atomic ledger), `handler.go` (auth via `Authorization: Bearer` or `x-api-key`, path/method/model allowlists, body cap, `max_tokens` clamp, header allowlist, key injection, no-redirect client, sanitized upstream errors, streaming with client-abort cancel and idle timeout). Not implemented: provider-specific request translation, usage reconciliation, a listener, and any wiring. Mutation table (guest copy only, all killed): redirect follow, quota reserve removed, model check removed, caller `Authorization` forwarded, no clamp, upstream error passthrough, idle timer off/never reset, expiry ignored, revocation ignored, signature ignored, quota off-by-one, method check removed, query allowed, http upstream allowed.

## Stage 3: wiring (after Stage 2 and B1)

Mint a token per task attempt, revoke on completion/cancel; point OpenCode `baseURL` at the gateway; keep Docker env free of tokens where possible (deliver via the same secret-file path). Quarantine lift is a separate explicit decision requiring gates 0C, 0D, 0F, 0G.

## Verification per stage

`go vet ./...`, `go test ./... -count=1 -shuffle=on`, `-race` on touched packages, and a recorded mutation table (production line mutated in the guest copy only; each mutant must be killed).

## B5 live probe results (2026-10-15, OpenCode 2.0.26, disposable VM `atm-build`)

Image `ghcr.io/anomalyco/opencode:2.0.26` (`sha256:15b62c74…ca71`, Alpine, entrypoint `opencode`).

Verified:
- `opencode serve --hostname 0.0.0.0 --port 4096` runs as UID 1000 with `--read-only --cap-drop ALL --no-new-privileges` plus tmpfs mounts.
- Password env var is `OPENCODE_PASSWORD` (fallback `OPENCODE_SERVER_PASSWORD`). Auth is HTTP Basic, user `opencode`, enforced only on `/api/*` (401 without or with a wrong password). Non-`/api` paths serve the web UI unauthenticated; that is expected. **Stage 3: the ATM OpenCode client must use Basic auth with user `opencode` on `/api/*`.** Checked: the existing runner client (`internal/runner/opencode_client.go`, `doRequest`) already sends Basic auth as user `opencode` and only calls `/api/*` paths, so no client change is needed.
- The repo entrypoint consumes and unlinks the secret file; the password is absent from `docker logs`, `docker inspect` Env/Args. It remains readable from PID 1 `/proc` environ (accepted B6 risk).
- A custom provider `baseURL` works via `OPENCODE_CONFIG_CONTENT` (`npm: @ai-sdk/openai-compatible`, `options{baseURL, apiKey}`). OpenCode sends `POST <baseURL>/chat/completions` with `Authorization: Bearer <apiKey>` and consumes SSE correctly. The provider package is bundled; no npm fetch and no sandbox internet needed.
- Request bodies carry `max_completion_tokens` (not `max_tokens`), which the gateway clamps. Bodies are large, so `MaxBodyBytes` must be configured far above the 4096 used in tests.
- Gateway requirements: serve `/v1/chat/completions` (OpenAI-compatible) and accept the capability token as `Authorization: Bearer`.

## Defect found and fixed: secret delivery via `CopyToContainer`

Commit `4e29ca5` delivered the password with `docker cp`. Live probe: it fails with "container rootfs is marked read-only" when `ReadonlyRootfs` is set (production sets it), and into a tmpfs mount of a running container it reports success but writes to the hidden underlying layer, so the entrypoint timed out (exit 78, fail-closed but non-functional).

Fix: `deliverSecret` execs `sh -c "umask 077 && cat > /run/secrets/opencode.tmp && mv … /run/secrets/opencode"` as `1000:1000` with the password on stdin (never argv/env/inspect); the rename is atomic. Verified live with read-only rootfs and `cap-drop ALL`. Delivery follows start and failure rolls the container back; the exec-after-start race is covered by the entrypoint's 30s wait. Unit tests and 9 mutants (no-mv, no-umask, wrong user, no stdin, no CloseWrite, skipped exit check, password in Cmd, deliver-before-start, no rollback) pin the contract. `CopyToContainer` is no longer used by the runner but remains on the `DockerClient` interface.

Status: Gate 0G remains blocked and unqualified. B1 (host operator or Gate 0D must allow only `sandbox bridge -> gateway:8082`) is the only open blocker for Stage 3 wiring. The Phase 1A quarantine is unchanged.
