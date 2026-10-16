#!/usr/bin/env bash
# Prepare the local-dev REAL runtime (see docs/runtime-local-dev.md). Writes only under data/runtime/ and creates
# the egress DMZ network and a local registry. It never touches firewall rules.
#
# Required environment:
#   ATM_GATEWAY_UPSTREAM_URL   e.g. https://api.openai.com/v1/chat/completions
#   ATM_GATEWAY_MODELS         comma separated model ids the gateway may forward
#   PRICE_INPUT_USD_PER_MTOK / PRICE_OUTPUT_USD_PER_MTOK   price per model (same price for every listed model)
# Required file:
#   data/runtime/provider.key  the provider API key (this script never creates or prints it)
set -euo pipefail
umask 077
cd "$(dirname "$0")/.."

: "${ATM_GATEWAY_UPSTREAM_URL:?set ATM_GATEWAY_UPSTREAM_URL}"
: "${ATM_GATEWAY_MODELS:?set ATM_GATEWAY_MODELS}"
: "${PRICE_INPUT_USD_PER_MTOK:?set PRICE_INPUT_USD_PER_MTOK}"
: "${PRICE_OUTPUT_USD_PER_MTOK:?set PRICE_OUTPUT_USD_PER_MTOK}"
QUOTA_USD="${ATM_GATEWAY_QUOTA_USD:-5.00}"
CONTEXT_TOKENS="${ATM_SANDBOX_CONTEXT_TOKENS:-128000}"
MAX_OUT_TOKENS="${ATM_GATEWAY_MAX_OUTPUT_TOKENS:-4096}"
GATEWAY_IP=172.29.0.2
REGISTRY_PORT="${ATM_DEV_REGISTRY_PORT:-5000}"

D=data/runtime
mkdir -p "$D"
[ -s "$D/provider.key" ] || { echo "ERROR: $D/provider.key is missing or empty (put the provider key there; mode 600)." >&2; exit 1; }
chmod 600 "$D/provider.key"
[[ "$ATM_GATEWAY_UPSTREAM_URL" == https://* ]] || { echo "ERROR: ATM_GATEWAY_UPSTREAM_URL must be https." >&2; exit 1; }
command -v openssl >/dev/null && command -v docker >/dev/null || { echo "ERROR: openssl and docker are required." >&2; exit 1; }

# 1. Gateway CA and certificate (SAN = the gateway IP; sandboxes verify it against the CA baked into their image).
if [ ! -s "$D/gateway-ca.pem" ]; then
  openssl ecparam -name prime256v1 -genkey -noout -out "$D/ca.key"
  openssl req -x509 -new -key "$D/ca.key" -sha256 -days 365 -subj "/CN=atm-dev-gateway-ca" -out "$D/gateway-ca.pem"
  openssl ecparam -name prime256v1 -genkey -noout -out "$D/gateway.key"
  openssl req -new -key "$D/gateway.key" -subj "/CN=atm-gateway" -out "$D/gateway.csr"
  printf 'subjectAltName=IP:%s\nextendedKeyUsage=serverAuth\nbasicConstraints=CA:FALSE\n' "$GATEWAY_IP" > "$D/san.ext"
  openssl x509 -req -in "$D/gateway.csr" -CA "$D/gateway-ca.pem" -CAkey "$D/ca.key" -CAcreateserial -days 90 -sha256 -extfile "$D/san.ext" -out "$D/gateway.pem"
  rm -f "$D/gateway.csr" "$D/san.ext" "$D/gateway-ca.srl"
  echo "generated gateway CA and certificate (90 days; delete $D/gateway*.pem to regenerate)"
fi
chmod 644 "$D/gateway-ca.pem" "$D/gateway.pem" "$D/gateway.key" "$D/provider.key" # read by the non-root server user

# 2. Price file (one price for every listed model).
python3 - "$D/prices.json" <<EOF
import json, sys
models = [m.strip() for m in "$ATM_GATEWAY_MODELS".split(",") if m.strip()]
price = {"input_usd_per_mtok": "$PRICE_INPUT_USD_PER_MTOK", "output_usd_per_mtok": "$PRICE_OUTPUT_USD_PER_MTOK"}
json.dump({"models": {m: price for m in models}}, open(sys.argv[1], "w"), indent=2)
EOF

# 3. Local registry + digest-pinned helper/probe image (ATM requires name@sha256:digest and never pulls at run time).
docker inspect atm-dev-registry >/dev/null 2>&1 || docker run -d --restart unless-stopped --name atm-dev-registry -p "127.0.0.1:${REGISTRY_PORT}:5000" registry:2 >/dev/null
REF="localhost:${REGISTRY_PORT}/atm-git-transfer:dev"
docker build -q -t "$REF" deploy/git-transfer >/dev/null
docker push -q "$REF" >/dev/null
PINNED="$(docker inspect --format '{{index .RepoDigests 0}}' "$REF")"
case "$PINNED" in *@sha256:*) ;; *) echo "ERROR: could not resolve a digest for $REF" >&2; exit 1;; esac

# 4. Egress DMZ network with the fixed interface name atm.nft expects.
docker network inspect atm_egress_dmz >/dev/null 2>&1 || docker network create --driver bridge --subnet 172.28.200.0/24 \
  -o com.docker.network.bridge.name=atm-egress-dmz atm_egress_dmz >/dev/null

# 5. Canary: a host address the firewall must keep sandboxes away from. Needs a listener (see the doc).
HOST_IP="${ATM_CANARY_HOST_IP:-$(ip -4 route get 1.1.1.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p' | head -1)}"
[ -n "$HOST_IP" ] || { echo "ERROR: could not detect the host IPv4; set ATM_CANARY_HOST_IP." >&2; exit 1; }

if [ ! -s "$D/token.secret" ]; then openssl rand -hex 32 > "$D/token.secret"; fi

cat > "$D/runtime.env" <<EOF
ATM_PROJECT_RUNTIME_ENABLED=true
ATM_GATEWAY_ENABLED=true
ATM_GATEWAY_LISTEN_ADDR=${GATEWAY_IP}:8082
ATM_SANDBOX_GATEWAY_URL=https://${GATEWAY_IP}:8082/v1
ATM_GATEWAY_CA_FILE=/etc/atm/gateway-ca.pem
ATM_GATEWAY_TLS_CERT_FILE=/etc/atm/gateway.pem
ATM_GATEWAY_TLS_KEY_FILE=/etc/atm/gateway.key
ATM_GATEWAY_PROVIDER_KEY_FILE=/etc/atm/provider.key
ATM_GATEWAY_PRICE_FILE=/etc/atm/prices.json
ATM_GATEWAY_UPSTREAM_URL=${ATM_GATEWAY_UPSTREAM_URL}
ATM_GATEWAY_MODELS=${ATM_GATEWAY_MODELS}
ATM_GATEWAY_QUOTA_USD=${QUOTA_USD}
ATM_GATEWAY_MAX_OUTPUT_TOKENS=${MAX_OUT_TOKENS}
ATM_GATEWAY_TOKEN_SECRET=$(cat "$D/token.secret")
ATM_SANDBOX_CONTEXT_TOKENS=${CONTEXT_TOKENS}
ATM_SANDBOX_PROXY_URL=http://172.28.200.10:3128
ATM_EGRESS_VERIFY_IMAGE=${PINNED}
ATM_EGRESS_CANARIES=${HOST_IP}:9999
ATM_GIT_TRANSFER_ENABLED=true
ATM_GIT_TRANSFER_IMAGE=${PINNED}
EOF
chmod 600 "$D/runtime.env"

cat <<EOF
Done. Wrote $D/runtime.env (secrets inside; gitignored).
Pinned helper/probe image: $PINNED
Canary: ${HOST_IP}:9999 (start a listener, e.g.: python3 -m http.server 9999 --bind ${HOST_IP})
Next: install the host firewall (docs/runtime-local-dev.md), then
  docker compose -f docker-compose.yml -f docker-compose.runtime.yml up -d
EOF
