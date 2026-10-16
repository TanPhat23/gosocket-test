# Gate 0D: Operator Handoff (forward proxy and host firewall)

**Status:** handoff only. Nothing here has been applied, compiled with `nft -c`, or run against a real host or Docker daemon. ATM never changes firewall rules and never holds `NET_ADMIN`; the host operator owns this policy. Gate 0D stays UNQUALIFIED, and the Phase 1A quarantine stays active until Gates 0C, 0D, 0F and 0G are qualified and the user makes a separate explicit decision. This is not a security sign-off.

Source of truth for the design: `specs/2026-10-11-gate-0d-egress-confinement-design.md` (approved as a design only). This document turns it into things the operator can do, and lists where the spec and the code disagree.

## 1. Read this first: the spec and the code do not match

| # | Finding | Consequence | Who decides |
|---|---|---|---|
| D1 | The spec's ruleset matches interfaces `atm-proj-*`. The code (`project_sandbox_manager.go`, `netName := "atm-net-" + project.ID`) creates a Docker network with **no `com.docker.network.bridge.name` option**, so Docker names the Linux interface `br-<12 hex>`. No interface ever matches `atm-proj-*`. | The spec ruleset as written matches nothing and would fail open. | Operator picks the matching method (section 3). Optionally ATM sets a bridge name (needs a code change; interface names are limited to 15 characters, so not the project UUID). |
| D2 | The spec puts the DMZ at `172.30.200.0/24` and sandboxes in `172.30.x.0/24`. The 0G handoff proposes a sandbox pool `172.30.0.0/16`. A `/16` pool contains the DMZ. | Overlap. | This document moves the DMZ to `172.28.200.0/24`, outside the pool. |
| D3 | The 0G handoff puts the gateway accept in a **separate table at priority -10**, and the 0D ruleset drops `iifname "atm-proj-*"` in INPUT at priority 0. In nftables an `accept` ends only its own chain; the packet still traverses the later base chain and is then dropped. | With both installed as written, sandboxes cannot reach the gateway. | Fixed here: one table, accept before drop (section 4). Treat this table as superseding the 0G example. |
| D4 (fixed in draft, see 7a) | The draft set no container DNS, so sandboxes get Docker's embedded resolver `127.0.0.11`, which forwards to the host's resolvers. | DNS tunnelling to an attacker domain works even with every TCP/UDP rule in place (probe NEG-0D-12 would fail). | **ATM code change:** set `HostConfig.DNS` to `127.0.0.1` (spec candidate, needs probing) or an inert resolver. Not done: the manager is quarantined. |
| D5 (fixed in draft, see 7a) | `sandbox.DefaultEgressPolicy` was documented with `http://atm-egress-proxy:3128`, a **hostname**. Sandboxes are not on the DMZ and (with D4 fixed) have no working DNS. | The proxy URL must be an IP, e.g. `http://172.28.200.10:3128`. | Operator supplies the IP; ATM passes it as the policy's `ForwardProxyURL`. |
| D6 | No proxy container, config, or compose service exists in the repo. | Nothing to run yet. | Section 5 has a sketch. |
| D7 | The spec's fail-closed contract (how ATM verifies the firewall without root) is an open blocker. | ATM can start a sandbox with no firewall today. | User and operator choose Contract 1 (canary probe) or 2 (readiness file) before the quarantine can lift. |
| D8 | Nothing in ATM calls `NewProjectSandboxManager`/`...WithGateway` yet (quarantine), so `NetworkCreate` runs with `Internal: false` and no subnet. | Subnets come from Docker's pool, which is why the pool must be dedicated (section 2). | Operator. |

## 2. Docker daemon settings

Dedicated pool so sandbox subnets are a known range (affects every user-defined network Docker creates on that host, so do not share the host with unrelated Compose projects, or give those their own explicit subnets):

```json
{ "default-address-pools": [ { "base": "172.30.0.0/16", "size": 24 } ] }
```

Create the egress DMZ yourself, with a fixed interface name and subnet (operator-created, not by ATM):

```sh
docker network create --driver bridge --subnet 172.28.200.0/24 \
  -o com.docker.network.bridge.name=atm-egress-dmz atm_egress_dmz
```

Gateway host-local IP (shared with Gate 0G): `172.29.0.2` on a dummy interface, as in the 0G handoff.

## 3. Choosing how to recognise sandbox traffic

Pick one. Section 4 uses option A.

- **A. Source pool (recommended).** Match `ip saddr 172.30.0.0/16` (and `ip daddr` for replies). Works with Docker's random `br-*` names. A sandbox runs with `--cap-drop ALL`, so it cannot spoof a source address.
- **B. Interface name.** Needs ATM to set `com.docker.network.bridge.name` (code change, 15-character limit, e.g. a short hash such as `atmp-1a2b3c4d`). Then the spec's wildcard `iifname "atmp-*"` works.

IPv6: disable it on sandbox bridges (spec section 3.2). The ruleset uses `table inet`, so the `drop` rules also cover IPv6 from pool-sourced interfaces; add the per-interface `disable_ipv6` sysctl for each bridge, never the global one.

## 4. Proposed ruleset (adapt; run `nft -c -f` first)

Adjusts the spec's section 7 for D1 and D3. `policy accept` everywhere: only ATM traffic is restricted, so unrelated host and Docker forwarding is untouched.

```nft
table inet atm_confinement {
    define SANDBOX_POOL = 172.30.0.0/16
    define DMZ_NET      = 172.28.200.0/24
    define PROXY_IP     = 172.28.200.10
    define PROXY_PORT   = 3128
    define GATEWAY_IP   = 172.29.0.2
    define GATEWAY_PORT = 8082
    define WAN_IFACE    = "eth0"            # operator: set the real uplink

    chain forward {
        type filter hook forward priority 0; policy accept;

        ct state invalid ip saddr { $SANDBOX_POOL, $DMZ_NET } drop

        # sandbox -> proxy only (and its replies)
        ip saddr $SANDBOX_POOL ip daddr $PROXY_IP tcp dport $PROXY_PORT ct direction original ct state new,established accept
        ip saddr $PROXY_IP ip daddr $SANDBOX_POOL tcp sport $PROXY_PORT ct direction reply ct state established accept

        # proxy -> WAN: HTTPS and DNS only
        iifname "atm-egress-dmz" oifname $WAN_IFACE tcp dport 443 ct direction original ct state new,established accept
        iifname "atm-egress-dmz" oifname $WAN_IFACE udp dport 53  ct direction original ct state new,established accept
        iifname "atm-egress-dmz" oifname $WAN_IFACE tcp dport 53  ct direction original ct state new,established accept
        iifname $WAN_IFACE oifname "atm-egress-dmz" ct direction reply ct state established accept

        # everything else from a sandbox or the DMZ is dropped: direct WAN, peer sandboxes, other bridges
        ip saddr $SANDBOX_POOL drop
        iifname "atm-egress-dmz" drop
    }

    chain input {
        type filter hook input priority 0; policy accept;

        # Gate 0G: the one host service a sandbox may reach. Must come before the drop (finding D3).
        ip saddr $SANDBOX_POOL ip daddr $GATEWAY_IP tcp dport $GATEWAY_PORT accept

        # nothing else on the host, including the bridge gateway IP, SSH, Docker, Postgres, Redis, Dex, the API
        ip saddr $SANDBOX_POOL drop
        iifname "atm-egress-dmz" drop
    }
}
```

Things I could not check from here (a real operator must):
- IPv6: the ruleset has no IPv6 accept for sandboxes, but its `ip saddr` matches only see IPv4. IPv6 from sandbox bridges is therefore not dropped by this table and relies on the per-bridge `disable_ipv6` sysctl; add an explicit `meta nfproto ipv6` drop for the sandbox interfaces if you want it enforced in the firewall too (option B makes that easy).
- Order against Docker's own `DOCKER-USER`/`FORWARD` iptables-nft chains. Docker accepts established flows early in its chains; verify ours is evaluated, for example with `nft monitor trace`.
- Persistence: reload the ruleset after Docker restarts and verify it survives (spec section 4.1).
- Existing connections in conntrack when you insert the rules (spec section 7 note): flush with `conntrack -F` in a maintenance window.
- If the gateway runs in a container rather than on the host dummy IP, the accept belongs in `forward` (and `DOCKER-USER`), not `input`.

## 5. Forward proxy (sketch, untested)

Requirements from the spec (section 5):
- Attached only to `atm_egress_dmz`, fixed IP `172.28.200.10`, port 3128. Not on any sandbox bridge, not on `agent_backend`.
- `CONNECT` to port 443 only; cleartext `GET`/`HEAD` only to allowlisted registry hosts; no SOCKS, no UDP/QUIC.
- Exact-FQDN allowlist, no wildcards, no CDN-wide entries. Redirects to unlisted hosts get 403.
- Match `CONNECT` host against the TLS SNI; reject tunnels whose SNI cannot be validated (ECH).
- Reject any hostname resolving to private, loopback, link-local (incl. `169.254.0.0/16`), shared (`100.64.0.0/10`), multicast, ULA, or IPv4-mapped IPv6 of those. This defeats DNS rebinding.
- Container hardening as for sandboxes: non-root, `--cap-drop ALL`, `no-new-privileges`, read-only rootfs.

Allowlist (all "candidate, pending security review"): `registry.npmjs.org`, `registry.yarnpkg.com`, `pypi.org`, `files.pythonhosted.org`, `proxy.golang.org`, `sum.golang.org`, `crates.io`, `index.crates.io`, `static.crates.io`, `api.nuget.org`, `repo1.maven.org`, `repo.maven.apache.org`. Explicitly blocked: `github.com`, `api.github.com` (Gate 0C), model providers (Gate 0G), metadata endpoints.

Squid shape, unverified (the SNI check needs `ssl_bump peek/splice` and a Squid built with OpenSSL, and the private-IP rejection needs an ACL on resolved addresses or an external helper; Envoy is the alternative):

```
http_port 3128
acl registries dstdomain registry.npmjs.org registry.yarnpkg.com pypi.org files.pythonhosted.org proxy.golang.org sum.golang.org crates.io index.crates.io static.crates.io api.nuget.org repo1.maven.org repo.maven.apache.org
acl SSL_ports port 443
acl CONNECT method CONNECT
http_access deny CONNECT !SSL_ports
http_access deny !registries
http_access allow registries
http_access deny all
```

The choice of proxy implementation is open (spec section 3.1 says "Squid / Envoy"). It needs a user/operator decision before anyone builds it.

## 6. What ATM will need once these exist (not done, quarantine)

- Build `sandbox.DefaultEgressPolicy("http://172.28.200.10:3128")` from operator config instead of a hostname.
- Set container DNS to an inert resolver (D4) and verify package managers still work through `CONNECT` (spec section 6.2).
- Fail closed if the firewall cannot be verified (D7). Probes must use only canary listeners, never `169.254.169.254` or public IPs.
- Optionally set a short bridge name (option B).

## 7. Verification the operator runs

On a disposable VM or host with native Docker (not the production host), from a throwaway container on a sandbox network, with a **positive control for every negative probe** (show the canary is reachable with the ruleset removed, then blocked with it loaded). Use the spec's matrix: POS-0D-01..07 (registries reachable through the proxy) and NEG-0D-01..15 (spec section 8.1). Add these two for the gateway interaction:

| # | Check | Required |
|---|---|---|
| G1 | sandbox container → `http://172.29.0.2:8082/v1/chat/completions` | reaches the gateway (401 without a token); proves D3 is fixed |
| G2 | sandbox container → host SSH, ATM API, Postgres, bridge gateway IP | timeout |

Record outputs next to the commit that turns the gateway or runtime on.

## 7a. ATM-side changes made (2026-10-16, quarantined draft only)

- **D4 done in the draft:** sandbox containers get `DNS: [192.0.2.1]` (RFC 5737 TEST-NET-1). Live check on Docker 29.1.3 in `atm-build`: default config resolves public names through `127.0.0.11`; with `--dns 127.0.0.1` or `--dns 192.0.2.1` lookups fail. **`127.0.0.1` was rejected** because Docker forwards loopback upstreams from the host's network namespace: on a host with a resolver on `127.0.0.1:53` (dnsmasq, etc.) lookups would succeed and bypass the sandbox firewall. With a non-loopback upstream the query leaves from the sandbox namespace, where `ip saddr $SANDBOX_POOL drop` stops it. The operator must therefore keep that forward drop; probe NEG-0D-12 should capture on the WAN that no query to 192.0.2.1 leaves.
- **D5 done in the draft:** `sandbox.ValidateForwardProxyURL` requires `http://<ip>:<port>`; the draft refuses a hostname proxy before any Docker call.
- Tests: `TestProjectSandbox_DNSIsDeadNonLoopbackAddress`, `TestProjectSandbox_HostnameProxyRejectedBeforeDocker`, `TestValidateForwardProxyURL`; both production lines mutation-checked (killed). Full `go test ./...` passes in `atm-guest`.
- Not done: bridge name (option B, awaiting decision), fail-closed firewall verification (D7, awaiting decision).

## 8. Decisions needed

1. Match sandbox traffic by source pool (A) or by bridge name (B, needs a code change).
2. Proxy implementation (Squid or Envoy) and who builds and runs it.
3. Fail-closed verification contract (canary probe or readiness file).
4. Whether the gateway stays on the host dummy IP or moves into a container.
5. Which disposable host runs the qualification probes (this machine's `atm-build` VM has Docker but no nftables ruleset or WAN canary set up).
