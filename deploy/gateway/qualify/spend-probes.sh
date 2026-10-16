# Gate 0G probe 12 (spend cap), run against a replacement gateway on :8082 (the firewall admits only that port) whose cap is 500 micro-USD
# (ATM_GATEWAY_QUOTA_USD=0.0005) and whose request quota is high, so only the spend cap can stop it.
# m1 is priced $1/Mtok in, $2/Mtok out. Each request reserves about (body bytes + 100 max_tokens * 2) = ~300 micro-USD.
# The fake provider reports small usage, so a request settles far below its reservation.
G=https://172.29.0.2:8082/v1/chat/completions
CA=/etc/gwq/gw-ca.pem
adm(){ curl -s "http://127.0.0.1:9101/$1"; }
code(){ docker run --rm --network sbA -v $CA:/gw-ca.pem:ro atm-fw-probe2 curl --cacert /gw-ca.pem -s -m 8 -o /dev/null -w '%{http_code}' "$@"; }
body(){ printf '{"model":"m1","max_tokens":100,"messages":[{"role":"user","content":"%s"}]}' "$1"; }
H="Content-Type: application/json"
TA=$(adm "grant?project=S&lease=spend-settle"); TB=$(adm "grant?project=S&lease=spend-bound"); TC=$(adm "grant?project=S&lease=spend-conc")
echo "== settle: 25 requests with reported usage (each settles to the reported usage, a few tens of micro-USD; the ~300 reservation bound alone would stop after 1)"
for i in $(seq 25); do echo -n "$(code -X POST -H "$H" -H "Authorization: Bearer $TA" -d "$(body hi)" $G) "; done; echo
echo "== bound: provider reports no usage, so the conservative reservation stays charged"
for i in 1 2 3; do echo -n "$(code -X POST -H "$H" -H "Authorization: Bearer $TB" -d "$(body MODE:nousage)" $G) "; done; echo "(expect 200 429 429)"
echo "== a different lease still has its own cap"
echo "other lease -> $(code -X POST -H "$H" -H "Authorization: Bearer $TC" -d "$(body hi)" $G)"
