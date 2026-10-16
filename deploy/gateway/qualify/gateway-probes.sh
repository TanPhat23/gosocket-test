G=http://172.29.0.2:8082/v1/chat/completions
adm(){ curl -s "http://127.0.0.1:9100/$1"; }
sb(){ docker run --rm --network sbA atm-fw-probe2 "$@" 2>&1; }
code(){ sb curl -s -m 8 -o /dev/null -w '%{http_code}' "$@"; }
body(){ printf '{"model":"m1","max_tokens":10,"messages":[{"role":"user","content":"%s"}]}' "$1"; }
sudo truncate -s0 /run/gwq-upstream.log
TA=$(adm "grant?project=A&lease=la1"); TB=$(adm "grant?project=B&lease=lb1")
H="Content-Type: application/json"
echo "== 06 direct provider egress from sandbox (expect fail/timeout)"
for h in api.anthropic.com api.openai.com; do echo "$h -> $(sb curl -s -m 4 -o /dev/null -w '%{http_code}' https://$h || true)"; done
echo "== G1 gateway reachable w/o token (expect 401)"; code -X POST -H "$H" -d "$(body hi)" $G; echo
echo "== G2 host/bridge unreachable (expect 000)"
for t in 172.30.1.1:22 10.0.2.2:22 172.29.0.2:22 172.29.0.2:9100; do echo "$t -> $(sb curl -s -m 3 -o /dev/null -w '%{http_code}' http://$t || true)"; done
echo "== POS echo with valid token (expect 200)"; code -X POST -H "$H" -H "Authorization: Bearer $TA" -d "$(body hi)" $G; echo
echo "== upstream saw:"; sudo tail -1 /run/gwq-upstream.log
echo "== 07 SSRF: other paths/urls/queries/methods (expect 4xx, no upstream hit)"
sudo truncate -s0 /run/gwq-upstream.log
for u in "/v1/chat/completions?url=http://169.254.169.254/" "/v1/embeddings" "/http://169.254.169.254/" "/v1/chat/completions/../../x"; do echo "POST $u -> $(code --path-as-is -X POST -H "$H" -H "Authorization: Bearer $TA" -d "$(body hi)" "http://172.29.0.2:8082$u")"; done
echo "GET -> $(code -H "Authorization: Bearer $TA" $G)"
echo "absolute-form via Host/URL body: model url -> $(code -X POST -H "$H" -H "Authorization: Bearer $TA" -d '{"model":"http://169.254.169.254","messages":[]}' $G)"
echo "bad model -> $(code -X POST -H "$H" -H "Authorization: Bearer $TA" -d '{"model":"evil","messages":[]}' $G)"
echo "upstream hits (expect 0): $(sudo wc -l < /run/gwq-upstream.log)"
echo "== 09 forged / missing"
echo "no auth -> $(code -X POST -H "$H" -d "$(body hi)" $G)"
echo "forged -> $(code -X POST -H "$H" -H "Authorization: Bearer ${TA%.*}.AAAA" -d "$(body hi)" $G)"
echo "garbage -> $(code -X POST -H "$H" -H "Authorization: Bearer xyz" -d "$(body hi)" $G)"
echo "x-api-key valid -> $(code -X POST -H "$H" -H "x-api-key: $TA" -d "$(body hi)" $G)"
echo "== 10 expired / revoked"
TE=$(adm "grant?project=A&lease=lex&ttl=1s"); sleep 2; echo "expired -> $(code -X POST -H "$H" -H "Authorization: Bearer $TE" -d "$(body hi)" $G)"
TR=$(adm "grant?project=A&lease=lrv"); echo "before release -> $(code -X POST -H "$H" -H "Authorization: Bearer $TR" -d "$(body hi)" $G)"; adm "release?lease=lrv"; echo "after release -> $(code -X POST -H "$H" -H "Authorization: Bearer $TR" -d "$(body hi)" $G)"
echo "== 11 cross-project: lease quotas independent"
for i in 1 2 3 4 5 6 7; do echo -n "$(code -X POST -H "$H" -H "Authorization: Bearer $TB" -d "$(body hi)" $G) "; done; echo "(B: 6 allowed then 429)"
echo "A still ok -> $(code -X POST -H "$H" -H "Authorization: Bearer $TA" -d "$(body hi)" $G)"
echo "== 12 quota / clamp"
TQ=$(adm "grant?project=Q&lease=lq"); sudo truncate -s0 /run/gwq-upstream.log
sb curl -s -m 8 -o /dev/null -X POST -H "$H" -H "Authorization: Bearer $TQ" -d '{"model":"m1","max_tokens":999999,"max_completion_tokens":999999,"messages":[{"role":"user","content":"x"}]}' $G
sudo tail -1 /run/gwq-upstream.log
echo "oversize body -> $(code -X POST -H "$H" -H "Authorization: Bearer $TQ" -d "$(body $(head -c 6000 /dev/zero | tr '\0' a))" $G)"
echo "concurrent 10 on fresh lease (quota 6):"; TC=$(adm "grant?project=C&lease=lc")
for i in $(seq 10); do (docker run --rm --network sbA atm-fw-probe2 curl -s -m 8 -o /dev/null -w '%{http_code}\n' -X POST -H "$H" -H "Authorization: Bearer $TC" -d "$(body hi)" $G &) ; done; sleep 12
echo "== 08 redirect"
TD=$(adm "grant?project=D&lease=ld"); sudo truncate -s0 /run/gwq-upstream.log
echo "redirect -> $(code -X POST -H "$H" -H "Authorization: Bearer $TD" -d "$(body MODE:redirect)" $G)"; sudo cat /run/gwq-upstream.log
echo "== 15 error redaction"
sb curl -s -m 8 -i -X POST -H "$H" -H "Authorization: Bearer $TD" -d "$(body MODE:err500)" $G | head -20; echo
echo "429 -> $(code -X POST -H "$H" -H "Authorization: Bearer $TD" -d "$(body MODE:err429)" $G)"
echo "gateway log:"; tail -15 /tmp/gwq-gw.log; echo "provider key in gw log: $(grep -c PROVIDER-SECRET /tmp/gwq-gw.log)  prompt in gw log: $(grep -c SECRET-PROMPT /tmp/gwq-gw.log)"
echo "== 14 stream cancel"
TS=$(adm "grant?project=S&lease=ls"); sudo truncate -s0 /run/gwq-upstream.log
sb timeout 2 curl -s -N -X POST -H "$H" -H "Authorization: Bearer $TS" -d "$(body MODE:stream)" $G | head -c 120; echo
sleep 4; sudo cat /run/gwq-upstream.log | cut -c1-150 | tail -4
echo "== 13 rotation"
sudo truncate -s0 /run/gwq-upstream.log
echo -n 'sk-ROTATED-KEY-9999' | sudo tee /etc/gwq/provider.key >/dev/null
echo "after rotate -> $(code -X POST -H "$H" -H "Authorization: Bearer $TS" -d "$(body hi)" $G)"; sudo tail -1 /run/gwq-upstream.log | cut -c1-200
echo "== 18 plaintext"; echo "plaintext http accepted -> $(code -X POST -H "$H" -H "Authorization: Bearer $TS" -d "$(body hi)" $G)  (EXPECTED FAIL by decision B4)"
