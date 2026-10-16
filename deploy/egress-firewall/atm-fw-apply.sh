#!/bin/sh
# Operator-owned and idempotent. Run as root after docker.service (see atm-fw.service).
# ATM never runs this and never holds CAP_NET_ADMIN. Adjust the pool, proxy IP and gateway IP to your host.
# Tested in a QEMU VM only (see docs/superpowers/plans/2026-10-16-gate-0d-operator-handoff.md section 9).
set -eu
POOL=172.30.0.0/16 PROXY=172.28.200.10 GATEWAY=172.29.0.2
ip link show gwdummy >/dev/null 2>&1 || { ip link add gwdummy type dummy; ip addr add "$GATEWAY/32" dev gwdummy; }
ip link set gwdummy up
nft delete table inet atm_confinement 2>/dev/null || true
nft -f /etc/atm/atm.nft
ipt() { t=""; [ "$1" = raw ] && { t="-t raw"; shift; }; c=$1; shift; iptables $t -C "$c" "$@" 2>/dev/null || iptables $t -I "$c" 1 "$@"; }
# Docker drops sandbox->proxy in raw PREROUTING (direct-access protection) and in its bridge isolation.
# An nft accept cannot override either, so both need an explicit accept ahead of Docker's rules.
ipt raw PREROUTING -s "$POOL" -d "$PROXY" -p tcp --dport 3128 -j ACCEPT
ipt DOCKER-USER -s "$PROXY" -d "$POOL" -p tcp --sport 3128 -m conntrack --ctstate ESTABLISHED -j ACCEPT
ipt DOCKER-USER -s "$POOL" -d "$PROXY" -p tcp --dport 3128 -j ACCEPT
