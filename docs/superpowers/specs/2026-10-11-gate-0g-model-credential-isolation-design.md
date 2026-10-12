# Specification: Gate 0G Model Credential Isolation & OpenCode Password Lifecycle Design

**Status:** PROPOSED DESIGN DIRECTION (Informed by User Input 2026-10-12; Architecture & Specification Only)
**Gate Status:** BLOCKED / UNQUALIFIED (Blocks self-hosting production readiness and project runtime LLM mediation)
**Formal Review & Security Sign-off:** PENDING (Requires formal threat review, adversarial qualification probe execution, and operator approval)
**Author:** Documentation Implementation Specialist
**Date:** 2026-10-11 (Updated 2026-10-12 post-security review)
**Target Platform:** Single-Host Linux (`linux/amd64`), Native Docker Engine, Control-Plane Reverse Proxy & Daemon Bootstrap
**Specification File:** `docs/superpowers/specs/2026-10-11-gate-0g-model-credential-isolation-design.md`

---

## 1. Executive Summary & Objective

This specification establishes the architectural design, isolation boundaries, authentication contracts, network egress mediation, and adversarial qualification plan for **Gate 0G: Model Credential Isolation & OpenCode Password Lifecycle**.

Under the target self-hosting operating model defined in `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md`, user projects execute autonomous AI coding workflows inside unprivileged, capability-dropped container sandboxes (`atm-project-<project_id>`). Each sandbox hosts a long-lived OpenCode server instance together with project worktrees, language toolchains, and arbitrary task processes (e.g., compilers, dependency scripts like `npm install` or `pip install`, build runners, test suites, and shell commands).

### Core Security Objectives

1. **Zero Provider API Keys in Sandboxes:**
   Model provider credentials (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, Azure OpenAI keys, AWS Bedrock credentials, Google Gemini keys, etc.) must **never** be injected into, stored in, or transmitted through the project sandbox container, its file systems, mounts, process environments, command arguments, or log streams.
2. **Default-Deny Model Egress & Control-Plane LLM Gateway Mediation:**
   Sandboxed OpenCode instances and task processes must have **zero direct internet egress** to upstream model provider APIs. Instead, all model requests are routed to a strictly scoped, authenticating, validating, rate-limited, and spend-capped **Control-Plane LLM Gateway**.
3. **Project & Active-Attempt Bounded Authorization:**
   Because any process executing within the sandbox shares the container trust boundary and could potentially invoke local network routes, the gateway must strictly validate requests server-side against an active, authenticated project attempt. The gateway capability granted to the sandbox must be short-lived, revocable, per-attempt, rate-limited, and hard-capped on spend and token consumption.
4. **Distinct OpenCode Daemon Password Lifecycle:**
   The password used to secure OpenCode's HTTP API (`OPENCODE_PASSWORD`) is an internal control-plane transport secret, completely distinct from model provider API keys. It must be generated per-instance with high entropy, provisioned without leaking into Docker inspect, command-line arguments, persistent volumes, or daemon log drivers, and analyzed realistically against the same-container process boundary.

> **CRITICAL ARCHITECTURAL NOTICE:**
> This specification documents a recommended design direction informed by user architectural input. It does **not** constitute Gate 0G approval, security sign-off, implementation authorization, or qualification. Gate 0G remains **BLOCKED** and **UNQUALIFIED** until adversarial qualification probes are formally executed and approved. Direct provider egress remains blocked pending Gate 0G resolution.

---

## 2. Current Implementation vs. Proposed Architecture Warning (SEC-0G-06)

> **WARNING: CURRENT CODE DOES NOT IMPLEMENT THIS SPECIFICATION AND MAY EXPOSE HOST CREDENTIALS IF A HOST PROFILE HAS THEM**
> Inspection of current ATM implementation code reveals structural configuration patterns that violate Gate 0G requirements if populated host credentials exist:
> 1. **Host Directory Bind Mounts:** In `internal/runner/task_runner.go:848-853`, `runInDocker` bind-mounts host OpenCode directories into the task container:
>    - `r.cfg.OpenCodeConfigDir` is mounted read-only to `/root/.config/opencode`.
>    - `r.cfg.OpenCodeDataDir` is mounted read-write to `/root/.local/share/opencode`.
>    Per `internal/config/config.go:85-86`, these default to host user paths `/home/ghug/.config/opencode` and `/home/ghug/.local/share/opencode`. Per official OpenCode documentation, `~/.local/share/opencode/auth.json` is the standard auth storage location for provider credentials. Consequently, **if the host profile contains populated credentials, any task process executing inside the container today may inspect host provider credentials**. The presence of credentials in the host environment was not inspected and is environment-dependent; however, the code paths unconditionally mount these host directories into the container.
> 2. **Inspectable Process Environment:** In `internal/runner/task_runner.go:797-805` and `internal/runner/task_runner.go:837`, `OPENCODE_PASSWORD` is injected directly into `containerCfg.Env`, making it permanently inspectable via `docker inspect`.
>
> **Explicit Non-Remediation Boundary:**
> This document specifies target architecture only. It **does not remediate** these codebase defects, modify application files, or authorize task execution against live hosts. Downstream implementation work must excise all host bind mounts, eliminate Docker-env password passing, and strictly prohibit running hostile or untrusted tasks against any host profile containing populated credentials.

---

## 3. Threat Model & Trust Boundaries

### 3.1 Trust Boundary Definitions

To analyze credential security correctly, the system is partitioned into three distinct trust zones:

```
+---------------------------------------------------------------------------------------------------+
| ZONE 0: TRUSTED CONTROL PLANE & HOST                                                              |
|                                                                                                   |
|  - Holds raw Model Provider API Keys (Anthropic, OpenAI, etc.) in encrypted vault/memory.        |
|  - Hosts ATM Control Plane API & Database.                                                        |
|  - Hosts Control-Plane LLM Gateway (authenticating, rate-limiting, egress-permitting proxy).      |
|  - Host Docker Daemon & Host OS Kernel (enforces cgroups, namespaces, seccomp, AppArmor).         |
+---------------------------------------------------------------------------------------------------+
                                         |
                       Internal Bridge / Dedicated Network
                       (Default-Deny Host Packet Filtering; TLS Hop)
                                         |
                                         v
+---------------------------------------------------------------------------------------------------+
| ZONE 1: SEMI-TRUSTED RUNTIME AGENT DAEMON (OpenCode Server Process)                               |
|                                                                                                   |
|  - Proposed Target Sandbox Architecture: Runs as unprivileged user `opencode` (UID 1000).         |
|  - Holds dynamic `OPENCODE_PASSWORD` (or session auth token) for HTTP Basic auth.                 |
|  - Configured with custom provider `baseURL` pointing to Control-Plane LLM Gateway over TLS.      |
|  - Holds short-lived Attempt Gateway Capability Token (NOT a provider API key).                   |
|  - Executes agent orchestration logic and dispatches tool execution commands.                     |
+---------------------------------------------------------------------------------------------------+
                                         |
                   SHARED CONTAINER TRUST BOUNDARY (Target: Shared UID 1000, Mounts, Network)
                                         |
                                         v
+---------------------------------------------------------------------------------------------------+
| ZONE 2: UNTRUSTED TASK & THIRD-PARTY WORKLOAD PROCESSES                                          |
|                                                                                                   |
|  - Repository source code, build scripts (`Makefile`, `build.rs`, `setup.py`, `package.json`).     |
|  - Dependency installation hooks (`npm postinstall`, arbitrary downloaded binaries).              |
|  - Test suites, linters, compilers, and user shell commands dispatched during task execution.     |
|  - Untrusted LLM-generated code running under guidance of autonomous turns.                       |
+---------------------------------------------------------------------------------------------------+
```

### 3.2 Realistic Security Boundary Analysis: Zone 1 vs. Zone 2

In the proposed target sandbox architecture, where the OpenCode server daemon and the task execution commands are assumed to run inside the same container under the same Linux user (`UID 1000`):

1. **The Shared Process Namespace Threat Assumption:**
   If task commands run in the same PID namespace under the same UID as the OpenCode server daemon, any child command or malicious script (e.g. from an `npm postinstall` hook) can:
   - Read `/proc/<opencode_pid>/environ` (permitted for processes with matching UID).
   - Inspect `/proc/<opencode_pid>/cmdline`.
   - Attach debugging tools or issue `ptrace` system calls (unless blocked by seccomp/Yama ptrace scope).
   - Read writable shared directories (e.g. `/tmp`, persistent project volumes, or shared Unix sockets).
2. **The Illusion of Child-Process Stripping:**
   Sanitizing environment variables prior to `execve` (stripping keys when launching a child shell) only protects against passive inheritance by well-behaved child processes. It provides **zero defense** against active, adversarial inspection of the parent process memory, parent `/proc` filesystem, or shared files.
3. **The Shared Network Namespace Reality:**
   Any network route or socket accessible to the OpenCode daemon is physically accessible to every process running in that container's network namespace. A malicious task script can issue requests to whatever local port or gateway endpoint OpenCode can reach.

**Fundamental Architectural Conclusion:**
Processes within the same project sandbox share a single trust boundary under the target runtime assumption. Therefore:
- Long-lived provider API keys must **never** be placed inside Zone 1 or Zone 2.
- Protecting provider keys requires keeping them entirely in Zone 0 (outside the sandbox).
- Any capability granted to Zone 1 must be assumed discoverable or callable by Zone 2, requiring strict server-side scoping, rate limiting, and spend caps.

---

## 4. Evaluation of Model Credential Protection Options

| Evaluation Criterion | Option A: Control-Plane LLM Gateway (RECOMMENDED) | Option B: Provider Key in OpenCode with Child-Env Stripping (REJECTED) | Option C: Local Container Secret Broker Daemon (REJECTED) |
|---|---|---|---|
| **Location of Raw Provider Keys** | Outside sandbox (Zone 0 control plane only). | Inside container (Zone 1 OpenCode process memory/env). | Inside container (local broker daemon) or host-mounted socket. |
| **Exposure to `/proc/<pid>/environ` & Memory** | **Zero exposure.** Keys never enter container memory or process tree. | **High Risk.** Same-UID processes can read parent `/proc/<pid>/environ` or dump memory. | Moderate/High. Broker holds keys or tokens in container memory. |
| **Resistance to Malicious Dependencies (`postinstall`)** | Complete protection against raw key exfiltration. (Workload can only consume attempt-scoped budget). | **Zero protection.** Malicious script extracts long-lived provider key and exfiltrates it. | Weak. Malicious script talks to local broker socket and requests keys or signatures. |
| **Egress Filter Compatibility (Gate 0D)** | **Compatible only after narrow exception qualified.** Not reachable under baseline Gate 0D rules; requires dedicated exception without opening host ports. | Poor fit. Requires punching FQDN holes for Anthropic, OpenAI, etc., in egress firewall. | Mixed. Still requires local socket exposure or external network access. |
| **Abuse & Spend Containment** | Strong. Control plane enforces per-attempt and per-project token/rate/spend hard caps. | None. Exfiltrated key allows unbounded out-of-band usage against provider account. | Partial. Broker could enforce rate limits, but adds complex IPC inside container. |
| **Recommendation** | **RECOMMENDED DESIGN DIRECTION** | **REJECTED AS AN ISOLATION BOUNDARY** | **REJECTED (Adds IPC complexity without solving same-UID trust boundary)** |

### Detailed Evaluation Rationale

#### Option A: Control-Plane LLM Gateway (Recommended Design Direction)
- **Mechanism:** OpenCode inside the sandbox is configured via provider configuration (`baseURL`) to send model completions to an internal control-plane endpoint over TLS (e.g. `https://atm-gateway.internal:8082/v1`). The ATM gateway validates attempt authentication, enforces project quotas, injects the real provider API key from secure control-plane storage, calls the upstream model provider over HTTPS, and streams the response back.
- **Why it Wins:** Raw provider credentials never cross into the container. Even full container compromise yields zero provider secrets. The threat is reduced from unconstrained account-level credential theft to bounded, attempt-scoped quota consumption.

#### Option B: Scoped OpenCode Environment with Child-Process Stripping (Rejected)
- **Mechanism:** The ATM control plane injects `ANTHROPIC_API_KEY` into the OpenCode daemon startup environment. OpenCode's subprocess runner strips these environment variables before invoking bash tools or task scripts.
- **Why it Fails:** This option falsely treats child-process environment inheritance as an isolation boundary. Under Linux, any process running with the same UID (target UID 1000) can read `/proc/<pid>/environ` of its sibling or parent processes unless strict PID namespace isolation, ptrace blocking, and Yama LSM protections are flawlessly configured. Furthermore, memory scraping, core dumps, and shell inspection easily leak the key. Stripping child environments is necessary hygiene, but **cannot serve as a security boundary**. Option B is rejected.

#### Option C: Local In-Sandbox Secret Broker Daemon (Rejected)
- **Mechanism:** A local broker daemon runs alongside OpenCode to sign requests or mint short-lived tokens.
- **Why it Fails:** A local daemon sharing the container environment faces the exact same trust-boundary problem: untrusted task code can query the broker over loopback or Unix domain sockets to perform unauthorized model requests, or inspect the broker process directly. Option C adds daemon management overhead without eliminating the need for external network mediation.

---

## 5. Control-Plane Model Gateway Specification

### 5.1 Architectural Role & Narrow Purpose

The Control-Plane LLM Gateway is a **narrow, purpose-built model API adapter**, strictly distinct from a generic HTTP forward/CONNECT proxy (such as the Gate 0D package registry proxy) and strictly distinct from a general ATM management API.

- It accepts strictly formatted model invocation requests (e.g., standard OpenAI/Anthropic completion and chat protocols).
- It strictly forbids arbitrary destination routing, generic HTTP forwarding, or `CONNECT` tunneling.
- It validates all requests against active project and attempt state stored in the control plane.
- It injects upstream credentials outside the container and terminates upstream TLS securely.

### 5.2 Invariant Security Properties & Gateway Controls

1. **Fixed Upstream Allowlist & Zero-Redirect Policy (SEC-0G-02):**
   The gateway connects only to an explicit, operator-configured allowlist of verified model providers (e.g., `api.anthropic.com`, `api.openai.com`). Upstream URLs are statically configured in the control plane.
   - **Strict Redirect Denial:** Automatic redirect following is **disabled** at the HTTP transport/client layer (e.g. `CheckRedirect: func(...) error { return http.ErrUseLastResponse }` or returning an explicit error).
   - **Terminal Upstream Error on 3xx:** All 3xx redirect status codes (`301`, `302`, `303`, `307`, `308`) returned by upstream targets are treated as terminal upstream failures. The gateway **never** follows redirects and **never** replays or forwards injected provider credentials to a redirected destination.
2. **Zero SSRF & Protocol Hardening:**
   The gateway does not accept arbitrary destination hostnames or ports from the caller. Callers specify only logical provider and model identifiers (e.g. `provider: "anthropic"`, `model: "claude-3-7-sonnet-20250219"`).
3. **Server-Side Project & Active-Attempt Authorization:**
   The gateway must **never** trust caller-supplied project IDs or attempt IDs in headers or request bodies. Authorization is derived strictly from a cryptographically signed, short-lived Gateway Capability Token presented by the sandbox.
4. **Short-Lived Revocable Gateway Capability:**
   When ATM dispatches a task attempt, it mints a short-lived capability token (e.g., HMAC-SHA256 signed JWT or random 32-byte opaque token in Redis) bound to `{project_id, task_id, attempt_id, expires_at}` with a maximum lifetime matching the task timeout (e.g., 60 minutes). When the attempt completes, fails, or is cancelled, the token is immediately invalidated in the control plane.
5. **Strip Caller-Supplied Upstream Credentials:**
   The gateway must strip any incoming `Authorization`, `x-api-key`, or `proxy-authorization` headers provided by the client, replacing them solely with the control-plane-managed secret for the targeted upstream provider.
6. **Conservative Pre-Allocation, In-Flight Clamping & Hard Spend Enforcement (SEC-0G-01):**
   Because untrusted code sharing the project sandbox could issue requests to the local gateway endpoint, and because streaming usage data often arrives only at stream completion, a simple point estimate cannot provide a guaranteed hard financial spend cap. To prevent concurrent streaming spend bypasses and maintain hard bounds:
   - **Atomic Conservative Upper-Bound Reservation:** Before dispatching an upstream request, the gateway atomically calculates and reserves a conservative upper-bound budget based on the model's pricing: input prompt token cost (conservatively bounded) plus the enforced maximum output token cost for the request. If the remaining project or attempt budget cannot cover this full conservative reservation across all active in-flight streams, the request is rejected immediately with HTTP 429/402 without contacting the provider.
   - **Enforced Per-Request Completion Caps:** The gateway strictly enforces, clamps, or overrides `max_tokens` / `max_completion_tokens` on every outbound request according to model/provider limits, establishing a deterministic maximum output token ceiling.
   - **Active In-Flight Stream Accounting & Cancellation:** The gateway actively tracks concurrent in-flight streams per attempt. If cumulative consumption (reserved plus committed) reaches the quota boundary, or if a provider exceeds enforceable bounds, active upstream connections are immediately aborted and cancelled.
   - **Post-Stream Reconciliation:** Upon stream completion or termination, actual token usage is reconciled against the reservation, releasing unconsumed reserve back to the attempt budget.
   - **Fail-Closed on Unboundable Providers/Models:** If prompt cost, token usage, or output ceilings cannot be safely bounded prior to dispatch for a specific provider or model, the gateway must **fail closed** for that provider/model, or enforce strictly a token/request quota rather than claiming a guaranteed financial spend cap. The exact tokenizer and pricing catalog implementation remains open engineering work; this architecture specifies the bounding contract rather than claiming an existing pricing engine is already operational.
7. **Strict Request & Streaming Bounds:**
   - Maximum request payload size (e.g., 20 MB for large context windows).
   - Maximum request timeout and streaming idle timeout (e.g., 30s idle disconnect).
   - Reliable handling of client cancellation: if the sandbox closes the connection, the gateway immediately cancels the upstream HTTP/SSE request to halt provider spend.
8. **Metadata-Only & Redacted Logging:**
   Gateway request and response logs must record only operational metadata (project ID, attempt ID, provider, model name, prompt token count, completion token count, latency, HTTP status code). Gateway logs must **never** record raw prompts, completions, system instructions, or upstream API keys.
9. **Fail-Closed Upstream TLS Validation:**
   The gateway must strictly validate upstream X.509 certificates against trusted system root CAs. Certificate validation bypasses (`InsecureSkipVerify: true`) are strictly forbidden.

### 5.3 Proposed Request & Data Flow Diagram (SEC-0G-04)

```
+---------------------------------------------------------------------------------------------------+
| CONTAINER SANDBOX (atm-project-<project_id>)                                                      |
|                                                                                                   |
|  [OpenCode Server Daemon (v2.0.26)]                                                               |
|    Configured baseURL: "https://atm-gateway.internal:8082/v1"                                    |
|    Auth Header: "Bearer <attempt_gateway_capability_token>"                                       |
|               |                                                                                   |
|               | HTTPS POST /v1/chat/completions (Strict TLS Endpoint Validation)                  |
|               v                                                                                   |
+---------------|-----------------------------------------------------------------------------------+
                | (Dedicated Docker Isolated Network / Forward Confinement; Encrypted TLS Hop)
                v
+---------------------------------------------------------------------------------------------------+
| CONTROL-PLANE LLM GATEWAY (Zone 0: Trusted Control Plane, Port 8082)                              |
|                                                                                                   |
|  1. Terminate TLS: Verify mutual TLS or control-plane cert with sandbox trust store.             |
|  2. Authenticate Token: Validate Bearer token signature, expiry, and revocation state.            |
|  3. Resolve Context: Extract project_id and attempt_id from token state (reject untrusted input). |
|  4. Verify Attempt State: Confirm task attempt is currently in status 'running'.                 |
|  5. Atomic Upper-Bound Reservation: Reserve input + max_tokens budget (SEC-0G-01).               |
|  6. Sanitize Request: Clamp max_tokens; strip inbound headers; validate model in catalog.        |
|  7. Inject Upstream Secret: Load encrypted provider API key from Control Plane Vault.             |
|  8. Dispatch Upstream: Forward HTTPS request to allowlisted provider (No Redirects, SEC-0G-02).   |
|  9. Stream & Reconcile: Stream chunks; abort on client cancel/quota hit; reconcile actual tokens. |
|               |                                                                                   |
|               | Outbound HTTPS (Strict TLS Verification, Redirects Disabled)                     |
|               v                                                                                   |
+---------------------------------------------------------------------------------------------------+
| UPSTREAM MODEL PROVIDER (e.g., https://api.anthropic.com or https://api.openai.com)               |
+---------------------------------------------------------------------------------------------------+
```

#### In-Sandbox Capability vs. Network Transport Invariants (SEC-0G-04)
- **Mandatory TLS Transport:** The network hop from sandbox to gateway must use TLS with strict certificate and endpoint validation. Plaintext HTTP carrying bearer capability tokens over Docker bridge networks is strictly prohibited.
- **Same-Project Process Boundary Reality:** Transport TLS protects credentials against network eavesdropping and external bridge interception. However, **within the container**, any process running under `UID 1000` (target architecture) that can access OpenCode configuration or network sockets can invoke this capability. Therefore, the bearer capability does not constitute a cross-process secrecy boundary within the sandbox. The system relies entirely on server-side active-attempt state, rate limits, atomic budget reservations, and hard spend caps to constrain same-project abuse.

---

## 6. Relationship to Gate 0D Egress Confinement & Route Reconciliation Blocker (SEC-0G-03)

Gate 0D (`docs/superpowers/specs/2026-10-11-gate-0d-egress-confinement-design.md`) establishes a hybrid default-deny egress model:
- Host firewall rules block all direct outbound L3/L4 traffic from project sandbox bridges.
- Outbound HTTP(S) traffic for dependency package managers (npm, PyPI, Crates.io, Go proxy) is routed through a dedicated forward proxy in an isolated DMZ (`atm-egress-proxy:3128`) enforcing an exact FQDN allowlist.

### 6.1 Explicit Separation of Responsibilities

1. **Model Gateway is NOT a Package Proxy:**
   The Gate 0D forward proxy must **not** serve as the model gateway, and must **not** hold or inject model provider credentials. The forward proxy is an unauthenticated L7 CONNECT proxy for public package downloads.
2. **Direct Model Provider Egress Remains Blocked:**
   The Gate 0D forward proxy FQDN allowlist must **not** include `api.anthropic.com`, `api.openai.com`, or any other model provider endpoints. Direct outbound connections from the sandbox to model APIs remain blocked.

### 6.2 Hard Gate 0D Route Reconciliation Blocker (SEC-0G-03)

Under the approved Gate 0D architecture, host firewall rules drop all traffic from project sandbox bridges except traffic explicitly destined for the package proxy (`atm-egress-proxy:3128`), and strictly drop all traffic to host INPUT, host ports, and control-plane infrastructure. Consequently:
- **Current Gate 0D Rules Drop LLM Gateway Traffic:** Under the current baseline Gate 0D rules, packets from the sandbox to the proposed LLM gateway will be dropped by default.
- **Mandatory Gate 0D Contract Addition:** Before Gate 0G can be approved, qualified, or implemented, Gate 0D must explicitly define, review, and qualify a narrow routing exception permitting:
  `source: project_sandbox_bridge -> destination: dedicated_gateway_service_ip:8082`
- **Strict Invariants for the Gateway Route:**
  - Must strictly drop all traffic to host INPUT (gateway IP, SSH `22`, Docker daemon).
  - Must strictly drop all traffic to control-plane services (PostgreSQL `5432`, Redis `6379`, Dex `5556`, ATM Control Plane API `8080`).
  - Must preserve default-deny drops for all direct internet and cloud metadata (`169.254.169.254`) traffic.
- **Unresolved Topology Status:** The exact network addressing, virtual bridge design, interface names, and firewall rules remain an open architectural item. This specification deliberately does **not** invent IP addresses, interface names, or final `nftables` commands, as they must be formally reconciled and qualified under Gate 0D.

---

## 7. OpenCode Server Password Lifecycle (`OPENCODE_PASSWORD`)

### 7.1 Fundamental Distinction from Provider Credentials

It is essential to distinguish the OpenCode server password from model provider credentials:
- **Model Provider API Keys (`ANTHROPIC_API_KEY`, etc.):** Protect access and billing to upstream commercial LLMs. They must **never** enter the sandbox container under any circumstances.
- **OpenCode Server Password (`OPENCODE_PASSWORD`):** Protects the HTTP API exposed by the OpenCode server process inside the sandbox against unauthorized control-plane commands. It is an internal transport secret between the ATM control plane and the OpenCode instance.

### 7.2 Upstream Documentation & Candidate Version Behavior

A critical version distinction exists regarding OpenCode server authentication:
- **Pinned Candidate Source (`v2.0.26`):**
  As verified in the runtime image specification (`docs/superpowers/specs/2026-10-10-runtime-image-configuration-design.md` §5.1), OpenCode `v2.0.26` inspects the environment variable `OPENCODE_PASSWORD` and enforces HTTP Basic authentication using the static username `opencode` (`Authorization: Basic <base64("opencode:" + password)>`).
- **Current Upstream Documentation (as of 2026-10-12):**
  Official documentation fetched from `https://opencode.ai/docs/server/` documents HTTP Basic authentication configured via `OPENCODE_SERVER_PASSWORD`. Furthermore, `https://opencode.ai/docs/providers/` documents provider configuration utilizing custom `baseURL` parameters.
- **Specification Invariant:**
  Implementations must adhere strictly to the verified behavior of the pinned version (`v2.0.26`), while verifying compatibility with both `OPENCODE_PASSWORD` and `OPENCODE_SERVER_PASSWORD` during qualification.

### 7.3 Mandatory Lifecycle & Confidentiality Requirements

1. **High Entropy Per-Instance Generation:**
   The control plane must generate a cryptographically strong, random 32-byte secret (e.g. `crypto/rand` hex or base64url encoded) for each project sandbox container instance. No shared, static, or default passwords are permitted.
2. **Prohibited Exposure Vectors:**
   The password must **never** appear in:
   - Docker container environment variables inspectable via `docker inspect` (e.g. `container.Config.Env`).
   - Container startup command-line arguments inspectable via `ps aux` or `docker inspect`.
   - Container image layers or Dockerfile definitions.
   - Container stdout or stderr captured by Docker log drivers (e.g. `/var/lib/docker/containers/.../*-json.log`).
   - Task worktree files or persistent project volumes (`atm-project-data-<id>`).
   - Task-child process environments spawned by OpenCode.
3. **One-Time Provisioning & Memory-Only Retention:**
   The secret must be retrieved or consumed once at startup and held only in process memory by the OpenCode daemon and the ATM control-plane runner.
4. **Immediate Revocation & Fail-Closed Lifecycle:**
   When a project container is stopped, terminated, or restarted, the password must be discarded. Subsequent requests using old credentials must fail immediately with HTTP 401.

### 7.4 Comparison of Dynamic Password Provisioning Candidates (SEC-0G-05)

To prevent leaking the password into `docker inspect` or persisted Docker logs, three private bootstrap candidates are compared:

| Dimension | Candidate 1: In-Memory `tmpfs` Secret File | Candidate 2: Private Unix Domain Socket Handshake | Candidate 3: One-Time Startup Pairing Code (`/auth/connect/:code`) |
|---|---|---|---|
| **Mechanism** | Control plane mounts a private, memory-backed `tmpfs` volume (`/run/secrets/opencode:rw,noexec,nosuid,size=1m`) into the container. ATM writes the generated password file prior to OpenCode startup. OpenCode reads the file on boot. | Container starts with OpenCode listening on a private Unix socket, or connects to a private control-plane socket to receive the password via local IPC before opening HTTP ports. | Container starts with OpenCode generating a random pairing code. ATM redeems `/auth/connect/:code` over HTTP to establish a 30-day HMAC session token. |
| **Docker Inspect Safety** | **High.** `tmpfs` contents are not inspectable via `docker inspect`. | **High.** Socket IPC is not inspectable via `docker inspect`. | **High.** Pairing code is generated dynamically; no static password passed via Docker env. |
| **Docker Log Driver Safety** | **High.** No secret emitted to stdout/stderr. | **High.** No secret emitted to stdout/stderr. | **Unspecified / Risk.** In pinned v2.0.26 source ([packages/server/src/pairing.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/server/src/pairing.ts)), the pairing code is generated and returned via API; log exposure is unspecified. Separately, [packages/cli/src/server-process.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/cli/src/server-process.ts) prints `server password ...` to stdout in foreground mode when no password env var is set. Neither can be assumed safe from log capture without verification. |
| **Race & Revocation Posture (SEC-0G-05)** | **Avoids Network Pairing Flow; File Read Race Persists.** Avoids a network pairing race. However, the file remains readable by same-UID task processes until a safe handoff, unlink, or isolation mechanism is proven. Not recommended as fully secure. | **No Network Pairing Flow.** Private IPC handshake. Avoids network pairing race, but requires custom agent IPC support. | **UNSAFE / UNQUALIFIED AS-IS.** The 5-minute single-use pairing window creates a race condition if the code is discoverable or reachable by an in-container process. Redeeming the code issues a 30-day HMAC session token that does not fit per-task/instance revocation. |
| **Compatibility with v2.0.26 Binary** | **Requires Wrapper.** v2.0.26 source reads env. Consuming a file requires an entrypoint wrapper that reads the file into env right before `exec`, leaving `/proc/<pid>/environ` exposure. | **Requires Custom Code.** OpenCode v2.0.26 does not natively support an external control-plane socket handshake. | **Native v2.0.26 Protocol.** Supported in protocol ([packages/server/src/pairing.ts](https://github.com/anomalyco/opencode/blob/v2.0.26/packages/server/src/pairing.ts)), but unsafe due to discoverability/race risks and 30-day token lifetime. |
| **Residual Same-UID Risk** | Any process running with target UID 1000 can read the file or `/proc` environ. | Any process running with target UID 1000 can connect to the Unix socket. | Any process running with target UID 1000 can reach loopback and attempt redemption during the 5-minute window if the code is discoverable. |

### 7.5 Realistic Risk Analysis & Unresolved Decision Status

**The Shared-Process Namespace Threat Assumption:**
Under the target runtime design assumption that OpenCode and task commands share a container namespace under `UID 1000`, **any secret known to the OpenCode process is vulnerable to local inspection** by an adversary capable of executing arbitrary code inside the container (e.g. via `/proc/<pid>/environ`, `/proc/<pid>/mem`, or open file descriptors).

**Decision Status (SEC-0G-05):**
- **Candidate 3 is marked UNSAFE and UNQUALIFIED AS-IS** because code acquisition/race prevention is unproven, stdout logging behavior requires verification, and the 30-day session token does not match per-task/instance revocation.
- **Candidate 1 is NOT recommended as fully secure** because same-UID task processes can inspect `/run/secrets` or `/proc/<pid>/environ` if an entrypoint wrapper is used.
- **No candidate is chosen or approved.** The exact mechanism for dynamic password delivery remains strictly **UNRESOLVED** pending formal qualification and adversary-resistant prototyping.

---

## 8. Open Questions & Technical Dependencies

The following questions must be resolved before drafting an implementation plan or authorizing code changes:

1. **OpenCode v2.0.26 BaseURL Configuration & Compatibility:**
   Does candidate binary `ghcr.io/anomalyco/opencode:2.0.26` cleanly accept custom provider `baseURL` settings (as documented in current OpenCode provider docs) via configuration files (`opencode.json`) or environment flags without attempting direct DNS resolution to upstream provider hostnames?
2. **Streaming Protocol & SSE Compatibility:**
   Does OpenCode v2.0.26 require specific Server-Sent Events (SSE) buffering, chunking, or HTTP/2 mechanics that must be supported by the control-plane gateway reverse proxy?
3. **Gate 0D Egress Reconciliation & Firewall Route (SEC-0G-03):**
   What exact network configuration and host packet filtering rules will be designed under Gate 0D to allow sandbox bridge traffic to reach the gateway while dropping all host INPUT and control-plane ports?
4. **Token Price & Real-Time Accounting Engine (SEC-0G-01):**
   What exact pricing catalog, token estimation tokenizer, and atomic reservation algorithm will the gateway use to compute and reserve budget before dispatching requests?
5. **Dynamic Password Ingestion in Unmodified Container:**
   How can a dynamic secret be ingested by OpenCode v2.0.26 without an entrypoint script that leaves the variable in `/proc/1/environ`?

---

## 9. Non-Goals (Explicitly Out of Scope)

To maintain strict implementation boundaries, the following items are declared **out of scope**:
- **Remote / External MCP (Model Context Protocol) Gateways:**
  Remote MCP integrations, third-party MCP servers over SSE, and external tool hosting are explicitly deferred and out of scope for Gate 0G v1.
- **Generic Control-Plane API Access:**
  The model gateway must not expose or bridge to ATM control-plane REST APIs, database queries, or task lifecycle management.
- **Multi-Tenant Sandboxing within a Single Project:**
  Tasks within the same project share a common project trust boundary and workspace volume. Fine-grained sandboxing between tasks of the same project is not addressed.
- **Direct Provider Credential Pass-Through:**
  No mechanism will be built to allow a project container to hold or negotiate raw upstream provider API keys directly.

---

## 10. Adversarial Qualification Test Matrix (Future Checks)

> **NOTICE:** The tests below represent the required verification battery that must be executed during future Gate 0G qualification testing. **None of these tests have been performed yet; this is a test specification, not a test report.**

| Test ID | Test Category | Target Vector / Probe Command | Expected Behavior / Pass Criteria | Security Failure Condition |
|---|---|---|---|---|
| **0G-PROBE-01** | Process Environment Absence | `docker exec atm-project-<id> env` | Provider keys (`*_API_KEY`) are completely absent from global container environment. | Any provider key appears in container environment output. |
| **0G-PROBE-02** | Process Tree `/proc` Inspection | `docker exec atm-project-<id> cat /proc/<pid>/environ` (for all running pids) | Provider keys are completely absent from all process environment blocks. | Provider key discovered in OpenCode daemon or child process `/proc` block. |
| **0G-PROBE-03** | Task Child Process Sanitation | Run task with command: `printenv \| grep -i "KEY\|SECRET\|TOKEN"` | Task process environment contains zero model provider secrets. Gateway token (if exposed) is strictly bounded. | Long-lived provider API key visible to task script. |
| **0G-PROBE-04** | Docker Metadata & Inspect Secrecy | `docker inspect atm-project-<id>` | No model keys or OpenCode passwords in `Config.Env`, `Args`, or labels. | `OPENCODE_PASSWORD` or provider key visible in JSON inspect metadata. |
| **0G-PROBE-05** | Docker Log Driver Secrecy | `docker logs atm-project-<id>` | Zero secrets, pairing codes, or passwords printed to stdout/stderr. | Authentication tokens or passwords visible in container log streams. |
| **0G-PROBE-06** | Direct Provider Egress Denial | From inside container: `curl -m 3 https://api.anthropic.com` and `curl -m 3 https://api.openai.com` | Connection times out or fails with network unreachable (enforced by Gate 0D firewall). | Connection succeeds directly to public provider IP. |
| **0G-PROBE-07** | Arbitrary Destination SSRF Denial | From container to gateway: `POST /v1/chat/completions` with destination `http://169.254.169.254` or unapproved URL | Gateway rejects request immediately with HTTP 400/403; zero outbound requests dispatched. | Gateway attempts network connection to arbitrary destination or metadata IP. |
| **0G-PROBE-08** | Upstream 3xx Redirect Denial (SEC-0G-02) | Simulate upstream provider returning HTTP 301/302/307 to another host | Gateway client terminates immediately with error; zero requests dispatched to redirected URL; zero credentials forwarded. | Gateway follows redirect or transmits provider API key to redirect target. |
| **0G-PROBE-09** | Forged / Missing Capability Token | From container to gateway: request without `Authorization` or with forged signature | Gateway rejects immediately with HTTP 401 Unauthorized; fail closed. | Gateway processes unauthenticated request. |
| **0G-PROBE-10** | Expired / Cancelled Capability Token | From container to gateway: request using token from a completed/cancelled attempt | Gateway rejects immediately with HTTP 401/403. | Expired or revoked attempt can continue invoking models. |
| **0G-PROBE-11** | Cross-Project Capability Isolation | Use Project A capability token to request models for Project B context | Gateway authorization fails closed (token strictly bound to Project A). | Gateway permits cross-project billing or resource consumption. |
| **0G-PROBE-12** | Atomic Upper-Bound Reservation & Spend Cap (SEC-0G-01) | Simulate attempt exceeding budget or dispatching concurrent streams beyond remaining reserve | Gateway rejects reservation with HTTP 429/402 before upstream dispatch; in-flight streams aborted when quota boundary hit. | Gateway permits streaming spend beyond enforceable quota. |
| **0G-PROBE-13** | Upstream Provider Key Rotation | Rotate provider key in control plane during active system operation | Gateway immediately adopts new key; zero container restart required; no keys leak in error logs. | Gateway fails to rotate or exposes key in retry/error response. |
| **0G-PROBE-14** | Streaming Cancellation Verification | Client initiates streaming completion, then aborts TCP connection after 2 tokens | Gateway immediately terminates upstream HTTPS connection; upstream token consumption halts. | Upstream connection remains open, burning provider tokens after client abort. |
| **0G-PROBE-15** | Error & Log Redaction Probe | Induce upstream 4xx/5xx errors (e.g. invalid request format, upstream rate limit) | Gateway returns sanitized error; gateway logs record status code and metadata only, no raw prompts or keys. | Raw upstream credentials or prompts leak in error body or log output. |
| **0G-PROBE-16** | OpenCode Bad Password Denial | Issue request to OpenCode HTTP API with invalid password | OpenCode returns HTTP 401 Unauthorized; rejects command dispatch. | Request succeeds without valid authentication. |
| **0G-PROBE-17** | Password Rotation & Session Invalidation | Rotate OpenCode password and attempt request with previously issued token | All previous session tokens immediately invalidated (HMAC check fails); HTTP 401 returned. | Old session token continues to authenticate requests. |
| **0G-PROBE-18** | Plaintext Network Eavesdropping Denial (SEC-0G-04) | Attempt HTTP plaintext connection to gateway endpoint | Gateway rejects non-TLS connection; strictly requires TLS with verified endpoint. | Gateway accepts plaintext HTTP carrying bearer tokens. |

---

## 11. Summary of Architectural Decisions & Blocker Status

| Component / Boundary | Architectural Status | Resolution / Path Forward |
|---|---|---|
| **Model Provider Key Isolation** | **RECOMMENDED DESIGN DIRECTION** | Keep all raw provider keys strictly in Zone 0 (Control Plane). Zero keys in sandboxes. |
| **Child-Process Environment Stripping** | **REJECTED AS A SECURITY BOUNDARY** | Stripping child env is basic hygiene but cannot protect keys against same-UID `/proc` inspection. |
| **Network LLM Egress** | **RECOMMENDED DESIGN DIRECTION** | Reverse-proxy all sandbox LLM traffic through a narrow Control-Plane LLM Gateway over TLS. |
| **Direct Model Egress** | **STRICTLY PROHIBITED** | Direct egress to provider APIs remains default-deny; must be blocked at network boundary. |
| **Redirects & Upstream SSRF** | **TERMINAL ERROR (ZERO REDIRECTS)** | Upstream redirects treated as fatal errors; credentials never forwarded (SEC-0G-02). |
| **Spend & Stream Accounting** | **ATOMIC CONSERVATIVE RESERVATION** | Atomically reserve upper-bound prompt+output budget; clamp in-flight streams; fail closed if unbounded (SEC-0G-01). |
| **Gate 0D Route Reconciliation** | **HARD UNRESOLVED BLOCKER** | Gate 0D must explicitly qualify dedicated gateway exception without opening host INPUT (SEC-0G-03). |
| **Dynamic OpenCode Password** | **UNRESOLVED CANDIDATES** | Candidates compared; pairing code marked unsafe; tmpfs not fully safe for same-UID (SEC-0G-05). |
| **Current Codebase Drift** | **WARNING DOCUMENTED** | Bind mounts (`/home/ghug/...`) and env password delivery fail Gate 0G if credentials populated; downstream fix required (SEC-0G-06). |
| **Gate 0G Overall Status** | **BLOCKED / UNQUALIFIED** | Requires adversarial qualification probe execution, operator review, and prototype sign-off. |
