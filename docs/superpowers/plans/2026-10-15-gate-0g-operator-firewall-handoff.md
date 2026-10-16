# Gate 0G: Operator Handoff for Blocker B1 (sandbox → gateway firewall route)

**Status:** optional; the gateway starts without it (with a warning). Handoff only. Nothing here has been applied or tested against a real firewall. ATM never changes firewall rules and never gets `NET_ADMIN`; the host operator owns this policy. Gate 0G stays BLOCKED and the Phase 1A quarantine stays active until the operator completes this and Gates 0C, 0D, 0F and a separate explicit user decision are done. This document is not a security sign-off.

## 1. What is needed

Sandbox containers must be able to reach the model gateway and **nothing else on the host**, over plain HTTP (accepted risk B4: capability tokens cross the bridge in cleartext, so the route must stay this narrow):

```
sandbox bridge(s)  ──TCP──►  gateway listener (one IP:port)      ALLOW
sandbox bridge(s)  ──────►  every other host address/port        DROP
sandbox A ◄──────► sandbox B (cross-project)                     DROP
```

Today's rules drop the route entirely (finding SEC-0G-03), which is why the gateway cannot be used.

## 2. Facts about how ATM creates sandbox networks (from `internal/runner/project_sandbox_manager.go`)

- One Docker bridge **per project**, named `atm-net-<projectID>`, created with `Driver: bridge`, `Internal: false`.
- Docker assigns each bridge a subnet and an interface name `br-<id>`; neither is stable or known in advance.
- Consequence: a rule cannot name one bridge. The operator must match the sandboxes some stable way. Two options, operator's choice:
  1. **Dedicated address pool (recommended).** Give Docker a pool used only for ATM sandboxes (`default-address-pools` in `daemon.json`, e.g. base `172.30.0.0/16`, size 24) and match that source range. Note this pool applies to every user-defined network Docker creates on that host.
  2. Match `iifname "br-*"` and accept that it covers every Docker bridge on the host.

## 3. Proposed topology (needs operator confirmation)

Bridges have different gateway IPs, so the listener cannot be "the bridge IP". Proposal: put the gateway on one **dedicated host-local IP**, for example `172.29.0.2` on a `dummy` interface, outside the sandbox pool. Every sandbox reaches it through its own bridge gateway; the traffic is delivered locally, so the **INPUT** chain is the one that matters.

```
ip link add atm-gw0 type dummy && ip addr add 172.29.0.2/32 dev atm-gw0 && ip link set atm-gw0 up
```

Sandboxes then use `http://172.29.0.2:8082/v1` as the model base URL. (If the gateway instead runs in its own container, the same policy goes in the `DOCKER-USER` chain on FORWARD; say so and this section needs rewriting.)

## 4. Example rule (nftables; adapt, do not paste blindly)

```nft
table inet atm_gateway {
  chain input {
    type filter hook input priority -10; policy accept;
    # sandboxes -> the gateway, and only the gateway
    ip saddr 172.30.0.0/16 ip daddr 172.29.0.2 tcp dport 8082 accept
    # (add an accept here for the egress forward proxy if it is host-local: Gate 0D owns that rule)
    # everything else from sandboxes to this host
    ip saddr 172.30.0.0/16 drop
  }
}
```

Also confirm, per Docker defaults, that traffic between different `atm-net-*` bridges is dropped (separate bridges are isolated unless something routes them) and that the existing Gate 0D egress rules are kept.

Caveats I could not check from here: interaction with whatever `iptables-nft`/Docker already installed, rule priority against Docker's chains, and whether the egress proxy lives on the host.

## 5. Verification the operator should run (before and after)

Run from a throwaway container attached to a sandbox network (a command the operator runs; ATM does not touch the host Docker daemon):

| # | Check | Before | After (required) |
|---|---|---|---|
| 1 | `curl -m5 -s -o /dev/null -w '%{http_code}' -X POST http://172.29.0.2:8082/v1/chat/completions` | timeout | `401` (gateway reached, no token) |
| 2 | same container → any other host port, e.g. `:22`, `:5432`, the ATM API port | timeout/refused | timeout/refused |
| 3 | container on project A's bridge → a container on project B's bridge | blocked | blocked |
| 4 | on the host: `ss -ltn \| grep 8082` | none | listens only on `172.29.0.2:8082`, not `0.0.0.0` / `::` |
| 5 | a request with a *forged* token | n/a | `401`, never reaches upstream |

All five must hold. Record the output next to the commit that turns the gateway on.

## 6. Turning the gateway on (after section 5 passes)

The gateway ships **disabled**. When enabled, `internal/gateway.ServiceFromEnv` starts even without the firewall attestation but logs a `WARNING` (user decision 2026-10-15); setting the attestation after section 5 passes silences it. Gate 0G is not qualified until the restricted route exists.

| Variable | Required | Notes |
|---|---|---|
| `ATM_GATEWAY_ENABLED` | yes | `true`; empty/`false` = off; anything else is an error |
| `ATM_GATEWAY_FIREWALL_ATTESTATION` | no | exactly `sandbox-bridge-to-gateway-only`; set it only after section 5 passes. Any other value or absence logs a warning |
| `ATM_GATEWAY_LISTEN_ADDR` | yes | explicit `host:port`, e.g. `172.29.0.2:8082`; wildcard, empty host and port 0 are rejected |
| `ATM_GATEWAY_UPSTREAM_URL` | yes | `https` only |
| `ATM_GATEWAY_PROVIDER_KEY` **or** `ATM_GATEWAY_PROVIDER_KEY_FILE` | exactly one | the file is re-read per request, so rotation needs no restart (decision B2) |
| `ATM_GATEWAY_TOKEN_SECRET` | yes | at least 32 bytes; signs per-attempt capability tokens |
| `ATM_GATEWAY_MODELS` | yes | comma-separated allowlist |
| `ATM_GATEWAY_KEY_HEADER` / `ATM_GATEWAY_KEY_PREFIX` | no | default `Authorization` / `Bearer `; a custom header gets no prefix unless given |
| `ATM_GATEWAY_MAX_BODY_BYTES`, `_MAX_OUTPUT_TOKENS`, `_QUOTA_REQUESTS`, `_QUOTA_TOKENS`, `_IDLE_TIMEOUT` | no | defaults 1 MiB, 4096, 200, 500000, 60s; all must be positive (decision B3) |

Only the OpenAI-compatible `POST /v1/chat/completions` is forwarded. There is no provider translation layer, so the upstream must speak that API.

## 7. What is still not done (Stage 3 remainder)

- **Not wired:** `cmd/server/main.go` and `internal/config` are being edited by another session, so the gateway is not started by the server yet. Intended call site:
  ```go
  gw, err := gateway.ServiceFromEnv(os.Getenv, os.ReadFile, log.Printf)
  if err != nil { log.Fatalf("gateway: %v", err) }
  if gw != nil {
      ln, err := gw.Listen() // handle err
      go func() { _ = gw.Run(ctx, ln) }()
  }
  ```
- **Built, not called by anything:** `Service.NewBroker(baseURL, ttl)` with `Grant(project, lease)` (mints a token for one project container's lease) and `Release(lease)` (revokes it and forgets quota usage; idempotent), plus `OpenCodeConfig(...)`, which renders the verified `OPENCODE_CONFIG_CONTENT` JSON (one `gw` provider, no real provider key).
- **Decision (user, 2026-10-15): one token per project container**, minted at container start and released at `StopProject`. The quota is therefore shared by every task the container serves; there is no per-attempt audit trail at the gateway. The ttl must cover the container's maximum lifetime or a refresh step is needed.
- **Not done:** delivering the config via the exec-stdin secret path (never Docker env, which `docker inspect` exposes) and calling `Grant`/`Release` from the runner, which waits on the quarantine and the `main.go` wiring.
- **Blocked on the quarantine:** none of this can run model traffic while Phase 1A is active, by design.
