#!/bin/bash
# usage: full.sh off|on   (off = ATM nft table removed + Docker isolation lifted for controls)
phase=$1
P=atm-project-probe
docker rm -f $P >/dev/null 2>&1
docker run -d --name $P --network sbA --ip 172.30.1.50 --dns 192.0.2.1 --cap-drop=ALL --security-opt no-new-privileges:true --read-only \
  -e HTTP_PROXY=http://172.28.200.10:3128 -e HTTPS_PROXY=http://172.28.200.10:3128 -e http_proxy=http://172.28.200.10:3128 -e https_proxy=http://172.28.200.10:3128 \
  atm-fw-probe2 sleep 3600 >/dev/null
hits(){ wc -l < /run/canary.log; }
x(){ id=$1; shift; b=$(hits); docker exec $P "$@" >/tmp/o.txt 2>&1; rc=$?; a=$(hits); printf "%-10s %-4s exit=%-3s canary_hits=%s  %s\n" "$id" "$phase" "$rc" "$((a-b))" "$(tr '\n' ' ' </tmp/o.txt | cut -c1-90)"; }
echo "=== phase $phase ==="
x NEG-0D-01 curl --noproxy "*" -m 3 -sS http://172.30.1.1:9999/canary
x NEG-0D-02 curl --noproxy "*" -m 3 -sS http://172.30.1.1:8080/health
x NEG-0D-03 nc -z -v -w 3 172.30.1.1 2222
x NEG-0D-04 nc -z -v -w 3 172.28.0.2 5432
x NEG-0D-05 curl --noproxy "*" -m 3 -sS http://172.30.2.2:80
x NEG-0D-06 curl --noproxy "*" -m 3 -sS http://198.51.100.50:80/canary
x NEG-0D-07 env -u HTTP_PROXY -u HTTPS_PROXY -u http_proxy -u https_proxy curl -m 3 -sS http://198.51.100.50:80/canary
b=$(hits); docker exec $P sh -c 'echo x | nc -u -w 2 198.51.100.50 5353'; rc=$?; a=$(hits); printf "%-10s %-4s exit=%-3s canary_hits=%s\n" NEG-0D-11 $phase $rc $((a-b))
# NEG-0D-12: raw DNS; count packets carrying the qname that leave the WAN interface
sudo timeout 8 tcpdump -i enp0s2 -nn -A -l 'port 53' >/tmp/dns-wan.txt 2>/dev/null &
sleep 1
docker exec $P dig @127.0.0.11 secret.example.com +time=2 +tries=1 >/tmp/o.txt 2>&1; rc=$?
docker exec $P dig @192.0.2.1 secret2.example.com +time=2 +tries=1 >/dev/null 2>&1
sleep 8; wait
printf "%-10s %-4s dig_exit=%s wan_dns_packets_with_qname=%s  %s\n" NEG-0D-12 $phase $rc "$(grep -c -E 'secret2?\.' /tmp/dns-wan.txt)" "$(grep -m1 -E 'timed out|no servers|status:' /tmp/o.txt | cut -c1-60)"
# NEG-0D-13: proxy reverse connection to a sandbox listener
b=$(hits); docker exec atm-egress-proxy sh -c 'nc -z -w 3 172.30.1.9 80'; rc=$?; printf "%-10s %-4s exit=%s\n" NEG-0D-13 $phase $rc
for d in github.com api.anthropic.com example.com; do
  id=$(case $d in github.com) echo NEG-0D-08;; api.anthropic.com) echo NEG-0D-09;; *) echo NEG-0D-10;; esac)
  x $id curl -m 8 -sS -o /dev/null https://$d
done
i=1
for u in https://registry.npmjs.org/express https://pypi.org/pypi/requests/json https://files.pythonhosted.org/packages/source/p/pip/pip-24.0.tar.gz https://proxy.golang.org/github.com/gin-gonic/gin/@v/list https://index.crates.io/co/nf/config https://api.nuget.org/v3/index.json https://repo1.maven.org/maven2/org/apache/commons/commons-lang3/maven-metadata.xml; do
  x POS-0D-0$i curl -f -s -S -m 25 -o /dev/null -w "http=%{http_code}" $u; i=$((i+1))
done
docker rm -f $P >/dev/null
