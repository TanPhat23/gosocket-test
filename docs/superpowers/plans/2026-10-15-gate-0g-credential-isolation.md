# Gate 0G Implementation Plan: Model Credential Isolation & OpenCode Password Lifecycle

**Status:** Stage 2 library implemented (`internal/gateway`, not wired). Gate 0G QUALIFIED 2026-10-16 on VM evidence by explicit user sign-off (see end of document). Phase 1A quarantine stays active.
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
| B3 | **Changed 2026-10-16 (user): dollar spend cap from an operator price file.** `ATM_GATEWAY_PRICE_FILE` must price every allowed model (fail closed); `ATM_GATEWAY_QUOTA_USD` is required. Each request reserves an upper bound (body bytes bound input tokens, clamped `max_tokens` bounds output), then settles to provider-reported usage and refunds on a non-2xx. | Closed in code (`internal/gateway/pricing.go`, `budget.go`, `usage.go`). Limits: streaming settlement needs the provider to honour `stream_options.include_usage` (the gateway forces it on); a response with no usage stays charged at the reservation bound; prices are operator-maintained and can go stale. |
| B4 | **Changed 2026-10-16 (user): TLS with a private CA baked into the project image.** The gateway requires `ATM_GATEWAY_TLS_CERT_FILE`/`_KEY_FILE`, validates them, serves TLS 1.3 only and refuses to run without them. `NewRuntimeImageBuilderWithGatewayCA` adds the CA certificate (public only, CERTIFICATE blocks only) to the image with `NODE_EXTRA_CA_CERTS` and a hash label, so the image tag changes with the CA. | Closed in code. Limits: the gateway certificate is for an IP SAN and the operator owns issuance, renewal and rotation; the builder with the CA is not yet constructed in production code (quarantine). Upstream (gateway -> provider) stays https-only. |
| B1 | Still open: host operator / Gate 0D must allow only `sandbox bridge -> gateway:8082`. ATM must not touch firewall rules. Operator handoff (topology, example rule, 5-point verification, env table): `2026-10-15-gate-0g-operator-firewall-handoff.md`. | **Blocks enabling the gateway and any gate qualification.** The gateway code now ships disabled and refuses to start without the operator's attestation (`internal/gateway/serve.go`). |
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
6. TLS-only listener (PROBE-18): **built 2026-10-16** after the B4 change (see B4).

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

Update: `internal/gateway.ServiceFromEnv` (env config, listener, firewall attestation guard) is committed but not wired into `cmd/server`. Remaining Stage 3: server wiring, per-attempt token minting and revocation, sandbox base-URL delivery. All wait on B1.

## B1 decision update (user, 2026-10-15): firewall optional via explicit opt-out

The user runs the host, so they are the host operator for B1, and chose not to require the firewall setup to use the gateway.

| Item | Decision |
|---|---|
| Behaviour | **Implemented.** The gateway starts without `ATM_GATEWAY_FIREWALL_ATTESTATION=sandbox-bridge-to-gateway-only` and logs a `WARNING` at startup (falls back to the standard logger if none is given). With the attestation it starts quietly. The other startup guards (https upstream, no wildcard bind, key/secret/limits) are unchanged. |
| Opt-out / production | No separate opt-out variable and no production-mode refusal (the user chose warn-and-start). A production refusal could be added later. |
| Risk accepted | A compromised sandbox may reach other host services and other projects' sandboxes. Capability tokens now cross the link inside TLS 1.3 (B4), so a passive observer on the bridge no longer sees them. Tolerable only where the host holds nothing sensitive. |
| Gate 0G | Was BLOCKED / UNQUALIFIED when this row was written; qualified 2026-10-16 by user sign-off (see end of document). The spec requires the restricted route (SEC-0G-03), so running with the opt-out never qualifies the gate. |
| Quarantine | Phase 1A unchanged. Lifting it needs gates 0C, 0D, 0G, 0F and a separate explicit user decision. |
| ATM and firewall rules | Unchanged: ATM does not modify firewall rules or receive `NET_ADMIN`. The opt-out removes a startup check only. |

B1 is therefore no longer a blocker for *using* the gateway in development, but it still blocks gate qualification. The operator handoff doc stays valid for anyone who later wants the restricted route.

## Stage 3 wiring and live probe (2026-10-16)

Committed `db22464`, `81bda33`: gateway startup in `cmd/server` (only with `ATM_GATEWAY_ENABLED=true`), `GatewayAccess`/`NewProjectSandboxManagerWithGateway`, lease-scoped token (lease = container ID) released on stop and on every failed provisioning, OpenCode config delivered over exec stdin to `/run/secrets/opencode-config` before the password, entrypoint extended and `ExpectedEntrypointSHA256` re-pinned to `87f8f33f…`.

Verified in `atm-guest` (offline, vendored): `go test ./internal/runner ./internal/gateway ./internal/quarantineguard` pass, 4 new `TestProjectGateway_*` tests, mutation checks on stop-release, rollback-release and config delivery all KILLED. `-race` not run (no cgo in guest).

Live probe in `atm-build` (Docker 29.1.3, OpenCode 2.0.26, read-only rootfs, cap-drop ALL, tmpfs secrets): delivery order config then password works; both files are consumed and unlinked; `OPENCODE_CONFIG_CONTENT` and `OPENCODE_PASSWORD` are set in PID 1 environ (accepted B6 risk); token and password absent from `docker inspect` and `docker logs`; without a config file no `OPENCODE_CONFIG_CONTENT` is set.

Not done: no caller builds the sandbox manager with a broker (quarantine), sandbox-facing gateway URL setting, full end-to-end through a real gateway. Gate 0G stays **BLOCKED / UNQUALIFIED**: SEC-0G-03 needs the restricted route (B1) and the quarantine is unchanged.

## Live qualification run (2026-10-16, QEMU VM evidence)

Ran the spec §10 matrix (0G-PROBE-01..18) on the Gate 0D firewall VM (`atm-fw`, ruleset on, sandbox bridge `sbA` 172.30.1.0/24). The gateway was the real `gateway.ServiceFromEnv` + `Service.Run` wiring, listening on `172.29.0.2:8082` with `ATM_GATEWAY_FIREWALL_ATTESTATION=sandbox-bridge-to-gateway-only`, in front of a fake TLS provider (`upstream.test`, private CA in the VM trust store, no `InsecureSkipVerify`). Sandbox-side probes ran from containers on `sbA`. Container probes used the OpenCode 2.0.26 image built from the project template and a container created like the quarantined `ensureProjectContainerDisabledDraft` (read-only rootfs, cap-drop ALL, no-new-privileges, tmpfs `/run/secrets`, config then password over exec stdin). The quarantine was not bypassed: no repo code path provisions containers. Fixtures: `deploy/gateway/qualify/`.

| Probe | Result | Evidence |
|---|---|---|
| 01 env | PASS | `docker exec env` shows no key, token or password. |
| 02 `/proc/*/environ` | PASS for provider key; password and gateway token present in PID 1 only | 4 pids scanned, zero provider-key or `sk-` hits. `OPENCODE_PASSWORD` and `OPENCODE_CONFIG_CONTENT` (capability token) are in PID 1 environ (accepted B6). |
| 03 task child env | FAIL before the fix, **PASS after** (see Fix for probe 03) | A model-requested `shell` tool call ran `printenv`; the output (and so the model provider's request body) contained `OPENCODE_PASSWORD=` and `OPENCODE_CONFIG_CONTENT=` with the capability token. No provider key. This is a consequence of B6: OpenCode passes its own environment to tool children. |
| 04 inspect | PASS | No password, token or key; only `OPENCODE_SERVER_PORT`. |
| 05 logs | PASS | 40 bytes (`server listening`), no secrets; secret files consumed and unlinked. |
| 06 direct provider egress | PASS | `api.anthropic.com`, `api.openai.com` time out from `sbA` and from the real sandbox container. |
| 07 SSRF | PASS | Query strings, other paths, `GET`, URL-valued or unlisted model all 403/404/405; zero upstream requests. |
| 08 redirect | PASS | Upstream 302 gave 502 to the client; zero hits on the redirect target; key not forwarded. |
| 09 forged/missing token | PASS | 401 for none, forged, garbage. Positive control 200 (Bearer and `x-api-key`). |
| 10 expired/revoked | PASS | 1s token 401 after expiry; released token 200 then 401. |
| 11 cross-project | PASS (approximated) | Quotas are per lease: B exhausted at 6 then 429, A unaffected. Requests carry no project field, so "Project A token for Project B context" reduces to lease isolation. |
| 12 quota / spend | PASS (live, TLS) | `max_tokens` and `max_completion_tokens` clamped to 100; 413 over body cap; 10 concurrent on a 6-request lease gave exactly 6x200 and 4x429. Spend cap, live on a gateway with `ATM_GATEWAY_QUOTA_USD=0.0005` and high request quotas: 18 requests with reported usage settled and succeeded, then 429s (the reservation bound alone, about 300 micro-USD, would have stopped after one); a provider response without usage kept the reservation charged, so the sequence was 200, 429, 429; a second lease had its own cap. Unit tests kill 5 mutations (no refund on provider error, no TLS listener, zero cost bound, no settle, TLS 1.2 allowed). |
| 13 key rotation | PASS | File rewritten, next request carried the new key, no restart. |
| 14 stream cancel | PASS | Client abort after 2s; upstream saw its request context cancelled after 8 chunks of 40. |
| 15 error redaction | PASS | Upstream 500 with prompt/key text gave a generic 502 `upstream error`; 429 stayed 429. Gateway log holds status and ids only (zero key or prompt hits). |
| 16 bad password | PASS | `/api/session`: no auth 401, wrong 401, right 200. Auth is enforced on `/api/*` only (known). |
| 17 rotation | PASS | New container with new password: old 401, new 200. Released gateway token 401. |
| 18 plaintext | PASS (live, TLS) | Plain HTTP to the TLS port returns 400 and no token reaches the log; a client without the gateway CA gets a certificate failure (000); TLS 1.2 is refused; connecting by a name that is not in the certificate fails. OpenCode (Bun) honours `NODE_EXTRA_CA_CERTS`: the end-to-end prompt through the TLS gateway succeeded and the printenv task leaked nothing. Negative control, same image without the CA: `UNABLE_TO_VERIFY_LEAF_SIGNATURE`, no request reached the provider. |

Also verified: end to end, OpenCode prompt, gateway, fake upstream returns streamed content with the real key injected and output tokens clamped. A sandbox reaches `172.29.0.2:8082` (401 without a token), and `172.29.0.2:22`, `:9100` (admin), `10.0.2.2:22` and the bridge gateway IP time out.

### Defects found
1. **Project container could not start on the read-only rootfs.** OpenCode exited with `FileSystem.makeDirectory (/home/opencode/.local/share/opencode/log)`: nothing pointed its `HOME` at the state volume, and the image did not create `/var/lib/opencode` for the opencode user. Fixed in the draft (`HOME=/var/lib/opencode`) and in `Dockerfile.project.template` (`mkdir` and `chown` `/var/lib/opencode`); verified live. The template is hash-pinned, so `ExpectedTemplateSHA256` (and its golden test) were recomputed locally from the edited file in the same change. New regression test `TestProjectContainer_HomeOnWritableStateVolume`; mutating `HOME=` makes it fail. Full `go test ./...` passes in `atm-guest`.
2. **Task children inherit the OpenCode password and the gateway token (probe 03).** Not fixed: OpenCode needs the password in its own environment, and B6 accepts same-UID exposure. New consequence worth a decision: the `printenv` output goes to the model provider.

### Status
Gate 0G stays **UNQUALIFIED** until the user explicitly signs it off; this change does not do that. B3 and B4 are now closed in code and re-run live (2026-10-16). Still open: 11 is only approximated (requests carry no project field), streaming usage settlement depends on the provider honouring `include_usage`, and the fixtures set `BASH_ENV` themselves, as the production container draft does. Evidence came from a QEMU VM, not a production host. The quarantine is unchanged.

### Fix for probe 03 (2026-10-16, user decision: accept B6 and scrub)
OpenCode runs the shell tool as `/bin/bash` directly under PID 1, so it inherits the OpenCode environment. `SHELL=` is ignored (live-tested, still leaked). Non-interactive bash sources `BASH_ENV`, so the image now carries a root-owned read-only `/usr/local/lib/atm-scrub.sh` (`unset OPENCODE_PASSWORD OPENCODE_SERVER_PASSWORD OPENCODE_CONFIG_CONTENT`) and the container env sets `BASH_ENV` to it. Live, same `printenv` task as before: leaked before, no `OPENCODE_PASSWORD` or `OPENCODE_CONFIG_CONTENT` after (image-baked file, env set as the draft does). Template hash pin recomputed again; `TestProjectContainer_TaskShellScrubsSecrets` fails when `BASH_ENV` is mutated; full `go test ./...` passes in `atm-guest`.

Limits, stated plainly: this is hygiene, not a boundary. Task code runs as the same UID, so it can still read `/proc/1/environ` (accepted B6) and can run a shell that skips `BASH_ENV` (`sh`, `bash --norc` does not skip it, but `env BASH_ENV= bash`/`sh` do). It stops the accidental and the model-driven `printenv` path that sent the secrets to the model provider. Gate 0G remains **UNQUALIFIED**; open items are 18 (plain HTTP, B4) and 12 (quota, not a spend cap, B3).

### Sign-off (2026-10-16)
The user reviewed the live matrix, the B3/B4 changes and their live re-run, and signed off Gate 0G ("ok sign this off"). Gate 0G is **QUALIFIED on VM evidence**. Scope: 0G only. It accepts the recorded deviations (B1 optional firewall attestation, B2 provider key from server env or file, B6 same-UID password exposure plus the task-shell scrub, which is hygiene not a boundary) and the open limits (probe 11 approximated; streaming spend depends on `include_usage`). It does not cover 0F, does not lift the quarantine, and does not wire anything: no sandbox manager is built with a broker and no production code constructs the builder with the gateway CA. Lifting needs 0F qualified and a separate explicit go-ahead.
