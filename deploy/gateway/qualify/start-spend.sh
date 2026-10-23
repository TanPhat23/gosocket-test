# Replacement gateway for the spend-cap probe (stop the normal gateway first; the firewall admits only :8082): cap 500 micro-USD, request/token quotas high.
(sudo setsid nohup env GWQ_ADMIN=127.0.0.1:9101 ATM_GATEWAY_ENABLED=true ATM_GATEWAY_LISTEN_ADDR=172.29.0.2:8082 ATM_GATEWAY_MODELS=m1 \
 ATM_GATEWAY_UPSTREAM_URL=https://upstream.test:9443/v1/chat/completions ATM_GATEWAY_PROVIDER_KEY_FILE=/etc/gwq/provider.key \
 ATM_GATEWAY_TLS_CERT_FILE=/etc/gwq/gateway.pem ATM_GATEWAY_TLS_KEY_FILE=/etc/gwq/gateway.key ATM_GATEWAY_PRICE_FILE=/etc/gwq/prices.json ATM_GATEWAY_QUOTA_USD=0.0005 \
 ATM_GATEWAY_TOKEN_SECRET=0123456789abcdef0123456789abcdef0123 ATM_GATEWAY_QUOTA_REQUESTS=1000 ATM_GATEWAY_QUOTA_TOKENS=100000000 ATM_GATEWAY_MAX_OUTPUT_TOKENS=100 \
 ATM_GATEWAY_MAX_BODY_BYTES=4096 ATM_GATEWAY_IDLE_TIMEOUT=3s ATM_GATEWAY_FIREWALL_ATTESTATION=sandbox-bridge-to-gateway-only /tmp/gwq gateway </dev/null >/tmp/gwq-gw2.log 2>&1 &)
sleep 2; cat /tmp/gwq-gw2.log; sudo ss -ltnp | grep -E "8083|9101"
