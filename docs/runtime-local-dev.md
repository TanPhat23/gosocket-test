# Running real tasks on local Docker Compose

Default `docker compose up` keeps the simulated runtime (`ATM_DEV_FAKE_RUNTIME=true`). This opt-in override runs
tasks in real, firewalled project containers with real OpenCode and the LLM gateway.

**Status: untested as a whole on a real host.** The pieces were qualified separately in VMs (0C git transfer, 0D
firewall, 0G gateway) and the container run was live-tested against a scripted fake LLM. Gate 0F is NOT qualified. The
files here (compose override, script, proxy config) were written without running them against a host Docker daemon.
Expect to debug. Use a disposable or dedicated machine.

## Not yet exercised live
PR creation, the firewall proof inside the server, the image builder, skills/MCP injection, a real LLM, verification
retry turns. The first real run is the first test of these.

## 1. Host prerequisites (operator, once)
1. `/etc/docker/daemon.json`: `{ "default-address-pools": [ { "base": "172.30.0.0/16", "size": 24 } ] }`, restart Docker.
   This affects every user-defined network on the host.
2. Install the firewall from `deploy/egress-firewall` (set `WAN_IFACE` in `atm.nft` first; run `nft -c -f` before loading).
3. Add the two things the stock ruleset does not cover for this topology, because the gateway here is a container
   (172.29.0.2 on the `atm_gateway` bridge), not a host-local address:
   - in `atm.nft`, `chain forward`, before the `ip saddr $SANDBOX_POOL drop` line:
     `ip saddr $SANDBOX_POOL ip daddr $GATEWAY_IP tcp dport $GATEWAY_PORT ct direction original ct state new,established accept`
     and `ip saddr $GATEWAY_IP ip daddr $SANDBOX_POOL tcp sport $GATEWAY_PORT ct direction reply ct state established accept`
   - Docker's own isolation drops sandbox to proxy/gateway traffic, so (see `gate-0d-operator-handoff.md` section 9)
     add accepts ahead of it, for the proxy and likewise for the gateway:
     `iptables -t raw -I PREROUTING 1 -s 172.30.0.0/16 -d 172.28.200.10 -p tcp --dport 3128 -j ACCEPT`
     `iptables -I DOCKER-USER 1 -s 172.30.0.0/16 -d 172.28.200.10 -p tcp --dport 3128 -j ACCEPT`
     `iptables -I DOCKER-USER 2 -s 172.28.200.10 -d 172.30.0.0/16 -p tcp --sport 3128 -m conntrack --ctstate ESTABLISHED -j ACCEPT`
     and the same three with `172.29.0.2` and port `8082`. These are not persistent and need re-checking after Docker
     recreates networks.
4. Start a canary listener on the host address the script prints (`python3 -m http.server 9999 --bind <host-ip>`).
   The firewall proof requires the proxy reachable and the canary NOT reachable from a sandbox.

## 2. Configure
Put the provider key in `data/runtime/provider.key`, then:

    ATM_GATEWAY_UPSTREAM_URL=https://<provider>/v1/chat/completions \
    ATM_GATEWAY_MODELS=<model-id> PRICE_INPUT_USD_PER_MTOK=<n> PRICE_OUTPUT_USD_PER_MTOK=<n> \
    scripts/runtime-dev-setup.sh

It generates a gateway CA and certificate (SAN 172.29.0.2), a price file and a token secret, runs a local registry on
`127.0.0.1:5000` to get a digest-pinned helper/probe image, creates the `atm_egress_dmz` network, and writes
`data/runtime/runtime.env`. Everything under `data/` is gitignored.

## 3. Run

    docker compose -f docker-compose.yml -f docker-compose.runtime.yml up -d
    docker compose logs -f control-plane

Startup fails closed with a message naming the bad setting. Per-project container creation fails if the firewall proof
fails (check the firewall and canary first). You also need a GitHub integration on the project, since the run ingests
and pushes through the transfer helper.

## Back to the simulated runtime
`docker compose up -d` without the override. Known limits: the model comes from the gateway config (no per-task
model); the dev proxy has no SNI or private-address checks; `EnsureTaskWorktree` still refuses.
