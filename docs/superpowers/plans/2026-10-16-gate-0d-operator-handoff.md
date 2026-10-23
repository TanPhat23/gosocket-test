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
| D7 | The spec's fail-closed contract (how ATM verifies the firewall without root) is an open blocker. | ATM can start a sandbox with no firewall today. | Decided: Contract 1 (canary probe), implemented as `sandbox.VerifyEgressFirewall` but not yet called; see section 8. |
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
- Decided 2026-10-16: option A (source pool), so no bridge-name change. Fail-closed verification (D7) is implemented as `sandbox.VerifyEgressFirewall` (section 8) and still needs to be called before sandbox start.

## 8. Decisions

Decided by the owner on 2026-10-16, taking the recommendations:

1. **Match by source pool (A).** The operator writes rules against the sandbox pool CIDR (`172.28.200.0/24`-style, see D2). No bridge-name code change.
2. **Squid, built and run by the host operator,** as an IP-only forward proxy on a fixed address (`http://<ip>:<port>`, validated by `sandbox.ValidateForwardProxyURL`).
3. **Fail-closed contract 1, canary probe.** Implemented ATM-side as `sandbox.VerifyEgressFirewall` (`internal/sandbox/egress_verify.go`). It runs a throwaway container with sandbox-grade hardening on the sandbox network and passes only if the control (the proxy `ip:port`) is reachable and every canary `ip:port` is not. Exit mapping: 0 pass, 10 `ErrEgressOpen`, 3 `ErrEgressControlUnreachable`, anything else (no `nc`, Docker failure, timeout, bad config) is an error. The operator supplies the canaries: listeners on host and LAN addresses that must not be reachable from sandboxes (never `169.254.169.254` or public IPs). IPv4 only. The function is **not called** anywhere yet; calling it before every sandbox start is part of lifting the quarantine.
4. **Gateway stays on the host dummy IP.** The ruleset must accept sandbox traffic to the gateway listener before the drop rules (D3).

Still open:

5. Which disposable host runs the qualification probes. It needs root, Docker, nftables and an outside canary; `atm-build` has no sudo, so the operator either provides such a host or runs the probes and returns the output.

Verification of decision 3: unit tests in `atm-guest` (exit-code mapping, hardening, positional arguments, config rejected before any Docker call) and a live test in `atm-build` (`TestLiveVerifyEgressFirewall`) covering pass, reachable canary and unreachable control against real listeners. That checks the probe, not an nftables ruleset.

## 9. Live test in a QEMU VM (2026-10-16) — NOT a security sign-off

Host: disposable Ubuntu 24.04 VM (`atm-fw`), Docker 29.1.3, nftables 1.0.9, iptables-nft, Squid in a container on `atm_egress_dmz` (172.28.200.0/24), sandbox networks `sbA`/`sbB` from pool 172.30.0.0/16, gateway dummy IP 172.29.0.2. The VM's own slirp gateway stood in for the WAN. IPv6, a real LAN, a real multi-NIC host and Docker upgrades were not tested. The VM, not a production host, so this does not qualify 0D.

**Defect found in section 4 as written: the proxy is unreachable.** Docker blocks the path in two places that an nft ruleset cannot override (any drop wins):

1. `iptables -t raw PREROUTING -d <proxy-ip> ! -i atm-egress-dmz -j DROP` (Docker's direct-access protection) drops sandbox-to-proxy packets before routing.
2. Docker's inter-bridge isolation in `FORWARD` drops them next.

Both need an explicit accept ahead of Docker's rules:

```
iptables -t raw -I PREROUTING 1 -s 172.30.0.0/16 -d 172.28.200.10 -p tcp --dport 3128 -j ACCEPT
iptables -I DOCKER-USER 1 -s 172.30.0.0/16 -d 172.28.200.10 -p tcp --dport 3128 -j ACCEPT
iptables -I DOCKER-USER 2 -s 172.28.200.10 -d 172.30.0.0/16 -p tcp --sport 3128 -m conntrack --ctstate ESTABLISHED -j ACCEPT
```

Removing the `DOCKER-USER` pair alone made the proxy time out; the raw rule alone was not enough earlier (DOCKER-USER counters stayed 0 until the raw accept existed). These rules are not persistent by themselves and were not re-checked after Docker recreated the networks (only after a daemon restart, where they and their order survived).

**Matrix from a sandbox container (`--dns 192.0.2.1 --cap-drop ALL --read-only`):**

| Target | no ATM ruleset | handoff ruleset + accepts above |
|---|---|---|
| proxy 172.28.200.10:3128 | blocked (Finding above) | reachable |
| gateway 172.29.0.2:8082 | reachable | reachable |
| peer sandbox on other bridge | blocked | blocked |
| bridge gateway IP host service | reachable | blocked |
| host primary IP host service | reachable | blocked |
| WAN canary (10.0.2.3:53) | reachable | blocked |

Note the peer-sandbox block comes from Docker, not from the ATM ruleset.

**Proxy:** `registry.npmjs.org` via CONNECT returned 200; `github.com`, `registry.npmjs.org:8443`, no-proxy requests (DNS dead) and direct `1.1.1.1:443` all failed.

**`sandbox.VerifyEgressFirewall` against this setup** (4 canaries: bridge gateway, host IP, WAN, peer sandbox): no ruleset gives `ErrEgressOpen`; handoff ruleset gives nil; wrong control port gives `ErrEgressControlUnreachable`; the gateway listed as a canary gives `ErrEgressOpen`. Positive control held for each canary.

Still untested: IPv6, rule persistence across reboot, a rebuilt sandbox network, an outside canary on real hardware, and 0G through this path.

### 9a. Full spec section 8.1 matrix (same VM, later the same day)

Probe container: `debian:bookworm-slim` + curl, dig, netcat-openbsd, UID 1000, `--cap-drop=ALL --read-only --security-opt no-new-privileges`, `--dns 192.0.2.1` (the dead-DNS decision, not `--dns=127.0.0.1`), proxy env set. Canaries are logging listeners (`deploy/egress-firewall/qualify/canary.py`): host ports 9999, 8080, 2222 (reached via the bridge gateway IP), `172.28.0.2:5432` and `198.51.100.50:80/tcp,5353/udp` on dummy interfaces, a TCP:80 listener in a peer sandbox, and a proxy-side connect to a sandbox listener. Run by `deploy/egress-firewall/qualify/full.sh on|off`. No probe touched IMDS or a public endpoint for a negative test.

| ID | Ruleset OFF (positive control) | Ruleset ON |
|---|---|---|
| NEG-0D-01..04 | connected, 1 canary hit each | timeout (exit 28 / nc timeout), 0 hits |
| NEG-0D-05 peer sandbox | connected | timeout |
| NEG-0D-06, 07 WAN fixture TCP | connected, 1 hit each | timeout, 0 hits |
| NEG-0D-11 WAN fixture UDP | 1 UDP hit logged | 0 hits |
| NEG-0D-12 raw DNS | 4 packets carrying the qname on the WAN interface | 0 packets on the WAN interface |
| NEG-0D-13 proxy to sandbox | connected | refused/timeout |
| NEG-0D-08..10 github, anthropic, example.com | n/a (L7) | `CONNECT tunnel failed, response 403` |
| NEG-0D-14 proxy stopped | n/a | POS-0D-01 fails (connect failed); recovers after restart |
| NEG-0D-15 table flushed | verifier returns `ErrEgressOpen` | verifier returns nil |
| POS-0D-01..07 | n/a | all exit 0; 200 except POS-0D-03 (302 redirect, `-f` passes) |

The peer-sandbox and proxy-to-sandbox positive controls needed Docker's own isolation lifted (a raw and `DOCKER-USER` accept for the control phase only, removed afterwards). Those two paths are therefore blocked by Docker as well as by the ruleset (defence in depth). The `POS-0D-03` URL answers 302 to a CDN, not 200 as the spec states.

IPv6: the sandbox network has `EnableIPv6=false`, the container has no inet6 address, and a connect to the host's IPv6 address failed. The ruleset's `ip saddr` matches are IPv4-only, so an IPv6-enabled sandbox network would bypass it. **Never create a sandbox network with IPv6 on a host using this ruleset**, or extend the ruleset first.

### 9b. Defects and operator requirements found

1. Section 4 as written leaves the proxy unreachable (raw + `DOCKER-USER` accepts above; shipped in `deploy/egress-firewall/atm-fw-apply.sh`).
2. **`docker0` shared the sandbox pool.** With `default-address-pools` 172.30.0.0/16, `docker0` took 172.30.0.0/24 and the ruleset dropped its traffic (an image build hung). Set `bip` for `docker0` outside the pool, or keep sandbox networks in a pool nothing else uses.
3. **Nothing persists by default.** After a reboot the dummy interface, nft table, iptables accepts and non-restarting containers were gone. `atm-fw.service` plus `--restart unless-stopped` on the proxy restored all of it with no manual step; after that reboot the verifier passed (with canaries listening) and npm via the proxy returned 200. The firewall comes up after `docker.service`, so there is a window at boot; the pre-launch canary probe is what covers it.
4. **`VerifyEgressFirewall` false pass (fixed).** With no firewall and nothing listening on the canary port, nc fails with an immediate refusal and the probe reported "blocked". Reproduced live (`NO listener + NO firewall -> nil`). The probe now requires each canary connect to run to the timeout (a drop); a fast refusal returns `ErrEgressCanaryAnswered` (exit 11). Re-verified live in all four combinations: firewall on without listeners (nil), on with listeners (nil), off with listeners (`ErrEgressOpen`), off without listeners (`ErrEgressCanaryAnswered`). Consequence: canaries must be DROPped, not REJECTed, and an ICMP-reject rule would fail the check.
5. The 3-second `nc -w` window means a very slow drop and a dropped packet look the same; that is intended.

### 9c. What is and is not established

Established in a QEMU VM: the topology, rules and probe behave as designed, every negative has a demonstrated positive control, the persistence unit works across a reboot, and the ATM-side verifier is no longer fooled by a silent canary. Not established: behaviour on a real multi-NIC production host or LAN, cloud metadata paths (deliberately not probed), IPv6-enabled sandbox networks, Docker upgrades rewriting rule order, rule-order after the sandbox networks are recreated by ATM at runtime, the 0G gateway end to end through this path, and review by someone who owns the production firewall. The packaged apply script differs from the one tested in the VM only by dropping the two fixture dummy interfaces.

Qualification is the operator's and the user's sign-off; this section is the evidence for it. **Signed off by the user on 2026-10-16 as sufficient for Gate 0D** (VM evidence; production-host run still recommended).
