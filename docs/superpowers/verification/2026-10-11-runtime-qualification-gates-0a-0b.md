# Gate 0A & Gate 0B Runtime Qualification Verification Report

- **Date:** 2026-10-11
- **Status:** QUALIFIED FOR DEDICATED GUEST PROBE ONLY (NOT GENERAL PRODUCTION READINESS)
- **Qualification Scope:** Dedicated Ubuntu 24.04 QEMU/KVM Guest Environment only
- **Candidate Image:** `ghcr.io/anomalyco/opencode:2.0.26` (candidate amd64 manifest `sha256:ea6c5d68d8d230923d2627bc793e7fa6a03f8e47c253b50397eb92bbde6db255`)

---

## 1. Executive Summary & Qualification Boundaries

This document records the durable, redacted technical qualification evidence for **Gate 0A** (Native Linux Docker No-New-Privileges execution) and **Gate 0B** (OpenCode candidate image digest, ABI, startup, and loopback authentication behavior).

### Strict Limitations & Boundaries
- **Environment Confinement:** Qualification applies **strictly and exclusively** to the dedicated Ubuntu 24.04 QEMU/KVM guest probe environment described herein.
- **Incompatible Hosts:** Host Snap Docker Engine is verified **incompatible** with mandatory `no-new-privileges:true` (exiting with status 255 `operation not permitted`). This verification does **not** qualify Snap Docker or arbitrary production hosts.
- **Blockers Preserved:** This qualification does **not** mark the broader self-hosting implementation as ready or unblock Tasks 1–9 of the runtime implementation plan.
- **Open Release Blockers:** Gates 0C (credential-safe Git transfer), 0D (default-deny egress filtering and allowlist), 0E, 0F, 0G, publisher signature/provenance verification, dynamic OpenCode password lifecycle management (avoiding persisted Docker logs / stdout scraping), and runtime catalog approval remain open and actively block implementation.

---

## 2. Gate 0A Verification: Native Non-Snap Linux Docker NNP Qualification

### 2.1. Environment Specifications
- **Guest OS Distribution:** Ubuntu 24.04 LTS (noble-server-cloudimg-amd64)
- **Guest Kernel:** Linux 6.8.0-142-generic #142-Ubuntu SMP PREEMPT_DYNAMIC x86_64
- **Container Engine:** Docker Engine CE (official apt repository)
  - `docker-ce`: `5:29.8.0-1~ubuntu.24.04~noble`
  - `docker-ce-cli`: `5:29.8.0-1~ubuntu.24.04~noble`
  - `containerd.io`: `2.3.4-2~ubuntu.24.04~noble`
  - `runc`: `1.5.1` (commit `v1.5.1-0-g8f2685a4`)

### 2.2. Exact Qualification Probe Execution
- **Preloaded Test Image:** `alpine:latest` (`sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6`)
- **Execution Command:**
  ```bash
  docker run --rm --pull=never --network none --read-only --cap-drop=ALL \
    --security-opt no-new-privileges:true --user 65534:65534 \
    --pids-limit 16 --memory 64m --cpus 0.5 \
    --entrypoint /bin/sh alpine:latest \
    -c 'grep "^NoNewPrivs:" /proc/self/status; grep -q "^NoNewPrivs:[[:space:]]*1$" /proc/self/status; exec /bin/true'
  ```
- **Observed Output:**
  ```text
  NoNewPrivs:	1
  ```
- **Exit Status:** `0`
- **Result:** **PASSED** on the dedicated Ubuntu 24.04 QEMU/KVM guest. The mandatory `no-new-privileges:true` requirement is satisfied without weakening AppArmor, seccomp, or privilege confinement.

---

## 3. Gate 0B Verification: OpenCode Image, ABI, Startup & Authentication

### 3.1. Candidate Image Artifact & ABI Composition
- **Candidate Image Reference:** `ghcr.io/anomalyco/opencode:2.0.26`
- **Candidate Linux/amd64 Manifest Digest:** `sha256:ea6c5d68d8d230923d2627bc793e7fa6a03f8e47c253b50397eb92bbde6db255`
- **OCI Index Digest:** `sha256:15b62c742c702b9750b7636cf3979823c653ef212e66ab217376ca0f32c1ca71`
- **Base OS:** Alpine Linux v3.24 (Alpine Linux 3.24.2, musl libc)
- **Binary Architecture & Path:** `/usr/local/bin/opencode` (`linux/amd64`, standalone compiled Bun binary)
- **Dynamic Linkage (`ldd`):**
  - `/lib/ld-musl-x86_64.so.1`
  - `libstdc++.so.6 => /usr/lib/libstdc++.so.6`
  - `libc.musl-x86_64.so.1 => /lib/ld-musl-x86_64.so.1`
  - `libgcc_s.so.1 => /usr/lib/libgcc_s.so.1`
- **Provenance Status:** Upstream SLSA provenance contains empty builder ID and unverified dependencies; publisher cosign/notary cryptographic signature is unverified. Candidate digest is pinned strictly as a content hash.

### 3.2. CLI Confinement Smoke Probes
Executed strictly under: `--pull=never --network none --read-only --cap-drop=ALL --security-opt no-new-privileges:true --user 65534:65534 --pids-limit 100 --memory 1g --cpus 1.0 --tmpfs /tmp:rw,noexec,nosuid,size=64m --tmpfs /home/nobody:rw,noexec,nosuid,size=64m`.
- **Command:** `opencode --version`
  - **Output:** `opencode v2.0.26`
  - **Exit Status:** `0`
- **Command:** `opencode serve --help`
  - **Output:** Start the v2 API and web server (`--hostname`, `--port`, `--cors`, `--service`, `--stdio`)
  - **Exit Status:** `0`

### 3.3. HTTP Loopback Authentication Probes
Probing executed on in-container loopback `127.0.0.1:4096` with `--network none` and `--read-only`:
1. **Unauthenticated Challenge (`GET /api/info`):**
   - **Status:** `401 Unauthorized`
   - **Header:** `www-authenticate: Basic realm="Secure Area"`
   - **Body:** `{"_tag":"UnauthorizedError","message":"Authentication required"}`
2. **Synthetic Bearer Header (`Authorization: Bearer test-dummy` on `GET /api/info`):**
   - **Status:** `401 Unauthorized`
3. **Unauthenticated Challenge (`GET /api/session`):**
   - **Status:** `401 Unauthorized`
   - **Header:** `www-authenticate: Basic realm="Secure Area"`
4. **Synthetic Bearer Header (`Authorization: Bearer test-dummy` on `GET /api/session`):**
   - **Status:** `401 Unauthorized`
5. **Authorized Positive Basic Auth Probe (`GET /api/info`):**
   - Executed under strictly verified container flags: `--log-driver none`, `--network none`, `--read-only`, zero filesystem binds, tmpfs-only throwaway scratch, non-root user `65534:65534`.
   - Temporary server-generated password held only in transient container tmpfs memory; zero secret bytes echoed or logged.
   - **Request:** `GET /api/info` with HTTP Basic credentials (`opencode:<server_password>`).
   - **Output:** `positive_auth_status=200`
   - **Exec Exit Status:** `0`
   - **Cleanup:** Container immediately removed (`docker rm -f`), zero containers retained.

---

## 4. Unresolved Risks & Architecture Gaps

1. **Publisher Signature & Provenance:** No cryptographic Cosign/Notary publisher signature verified for `anomalyco/opencode:2.0.26`.
2. **Production Password Lifecycle:** In-container execution generated passwords to stdout; production architecture requires a deterministic, non-persisted credential injection mechanism without log scraping.
3. **Control-Plane Routing & Network Egress:** Production default-deny egress controls (Gate 0D) and credential-safe repository transfer (Gate 0C) remain unvalidated.
4. **Hardware CPU Instruction Requirements:** Standalone Bun binary requires modern x86_64 instruction extensions (hypervisors must pass host CPU features).
