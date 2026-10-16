G=http://172.29.0.2:8082
PW1=$(head -c32 /dev/urandom | od -An -tx1 | tr -d ' \n')
name=atm-project-p1
start(){ # $1 password
  docker rm -f $name >/dev/null 2>&1; docker volume rm -f atm-p1-data atm-p1-state >/dev/null 2>&1
  docker volume create atm-p1-data >/dev/null; docker volume create atm-p1-state >/dev/null
  docker run -d --name $name --network sbA --read-only --cap-drop ALL --security-opt no-new-privileges:true \
    --tmpfs /tmp:rw,noexec,nosuid,size=512m --tmpfs /run/secrets:rw,noexec,nosuid,size=1m,uid=1000,gid=1000,mode=0700 \
    -v atm-p1-data:/workspaces -v atm-p1-state:/var/lib/opencode --pids-limit 256 --memory 4g \
    -e HOME=/var/lib/opencode -e OPENCODE_SERVER_PORT=4096 -e ATM_LLM_GATEWAY_URL=$G/v1 atm-0g-sandbox >/dev/null
  CID=$(docker inspect -f '{{.Id}}' $name)
  TOK=$(curl -s "http://127.0.0.1:9100/grant?project=p1&lease=$CID")
  /tmp/gwq4 config $G/v1 $TOK | docker exec -i -u 1000:1000 $name sh -c 'umask 077 && cat > /run/secrets/opencode-config.tmp && mv /run/secrets/opencode-config.tmp /run/secrets/opencode-config'
  printf %s "$1" | docker exec -i -u 1000:1000 $name sh -c 'umask 077 && cat > /run/secrets/opencode.tmp && mv /run/secrets/opencode.tmp /run/secrets/opencode'
  IP=$(docker inspect -f '{{(index .NetworkSettings.Networks "sbA").IPAddress}}' $name)
  for i in $(seq 40); do docker exec $name curl -s -m 2 -o /dev/null http://127.0.0.1:4096/ && break; sleep 1; done
}
start $PW1
echo "container $name ip=$IP token-prefix=${TOK:0:12}"
echo "== 01 env"; docker exec $name env | sort | grep -iE "key|secret|token|pass|PROVIDER|sk-" ; echo "(matches above; expect none)"
echo "== 02 /proc environ (all pids), provider key + sk- scan"
docker exec $name sh -c 'for p in /proc/[0-9]*; do tr "\0" "\n" < $p/environ 2>/dev/null | sed "s|^|$(basename $p): |"; done' > /tmp/environ.txt
echo "pids: $(docker exec $name sh -c 'ls -d /proc/[0-9]* | wc -l')"
echo "provider key hits: $(grep -c 'PROVIDER-SECRET\|ROTATED-KEY' /tmp/environ.txt)  sk- hits: $(grep -c 'sk-' /tmp/environ.txt)"
echo "names of sensitive vars present:"; grep -oE "^[0-9]+: [A-Z_]*(PASS|KEY|TOKEN|SECRET|CONFIG)[A-Z_]*=" /tmp/environ.txt | sort -u
echo "== 04 inspect"; docker inspect $name > /tmp/inspect.json
echo "password in inspect: $(grep -c "$PW1" /tmp/inspect.json)  token in inspect: $(grep -c "$TOK" /tmp/inspect.json)  provider key: $(grep -c PROVIDER-SECRET /tmp/inspect.json)  OPENCODE_ vars: $(grep -o 'OPENCODE_[A-Z_]*' /tmp/inspect.json | sort -u | tr '\n' ' ')"
echo "== 05 logs"; docker logs $name > /tmp/logs.txt 2>&1; echo "log bytes: $(wc -c < /tmp/logs.txt) password: $(grep -c "$PW1" /tmp/logs.txt) token: $(grep -c "$TOK" /tmp/logs.txt)"; head -5 /tmp/logs.txt
echo "secret files left: $(docker exec $name ls -A /run/secrets | wc -l)"
echo "== 16 bad password (auth is enforced on /api/* only)"
c(){ docker exec $name curl -s -m 5 -o /dev/null -w '%{http_code}' "$@"; }
oc(){ docker exec $name curl -s -m 90 "$@"; }
A=http://127.0.0.1:4096/api/session
echo "no auth -> $(c $A)  wrong -> $(c -u opencode:wrong $A)  right -> $(c -u opencode:$PW1 $A)  (positive control: right must be 200)"
SID=ses_gwprobe0001
oc -u opencode:$PW1 -X POST -H 'Content-Type: application/json' -d "{\"id\":\"$SID\",\"title\":\"t\",\"location\":{\"directory\":\"/workspaces\"}}" $A -w ' [create %{http_code}]\n' | head -c 300
echo "== E2E prompt opencode -> gateway -> fake upstream (echo)"
sudo truncate -s0 /run/gwq-upstream.log
oc -u opencode:$PW1 -X POST -H 'Content-Type: application/json' -d '{"text":"say hi"}' $A/$SID/prompt -w ' [prompt %{http_code}]\n' | head -c 300; sleep 8
sudo cut -c1-260 /run/gwq-upstream.log | head -4
echo "== 03 task child env: model asks opencode to run printenv"
sudo truncate -s0 /run/gwq-upstream.log
oc -u opencode:$PW1 -X POST -H 'Content-Type: application/json' -d '{"text":"MODE:tool run printenv"}' $A/$SID/prompt -w ' [prompt %{http_code}]\n' | head -c 300; sleep 15
echo "upstream saw (flags = what the tool result sent back to the model contained):"; sudo grep '"kind":"flags"' /run/gwq-upstream.log | cut -c1-500
echo "== 17 restart/rotate password"
docker stop $name >/dev/null; curl -s "http://127.0.0.1:9100/release?lease=$CID"
echo "old token after release -> $(curl -s -m 5 -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK" -d '{"model":"m1","messages":[]}' $G/v1/chat/completions)"
PW2=$(head -c32 /dev/urandom | od -An -tx1 | tr -d ' \n'); start $PW2
echo "new container: old pw -> $(c -u opencode:$PW1 http://127.0.0.1:4096/api/session)  new pw -> $(c -u opencode:$PW2 http://127.0.0.1:4096/api/session)"
echo "== 06 from the real sandbox container: direct egress"
docker exec $name sh -c 'curl -s -m 4 -o /dev/null -w "%{http_code}\n" https://api.anthropic.com; echo rc=$?'
docker exec $name sh -c 'curl -s -m 4 -o /dev/null -w "%{http_code} gateway(no token)\n" -X POST http://172.29.0.2:8082/v1/chat/completions'
docker rm -f $name >/dev/null
