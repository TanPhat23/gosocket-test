set -e
sudo mkdir -p /etc/gwq && sudo chmod 755 /tmp/gwq
cd /etc/gwq && sudo /tmp/gwq certs /etc/gwq && sudo chmod 644 /etc/gwq/*.pem /etc/gwq/upstream.key /etc/gwq/gateway.key
echo '{"models":{"m1":{"input_usd_per_mtok":"1","output_usd_per_mtok":"2"}}}' | sudo tee /etc/gwq/prices.json >/dev/null
sudo cp /etc/gwq/ca.pem /usr/local/share/ca-certificates/gwq.crt && sudo update-ca-certificates >/dev/null 2>&1
grep -q upstream.test /etc/hosts || echo "127.0.0.1 upstream.test" | sudo tee -a /etc/hosts >/dev/null
sudo rm -f /run/gwq-upstream.log
(sudo setsid nohup /tmp/gwq upstream /etc/gwq </dev/null >/tmp/gwq-up.log 2>&1 &)
echo -n 'sk-PROVIDER-SECRET-0123456789' | sudo tee /etc/gwq/provider.key >/dev/null; sudo chmod 600 /etc/gwq/provider.key
(sudo setsid nohup env ATM_GATEWAY_ENABLED=true ATM_GATEWAY_LISTEN_ADDR=172.29.0.2:8082 ATM_GATEWAY_MODELS=m1 \
 ATM_GATEWAY_UPSTREAM_URL=https://upstream.test:9443/v1/chat/completions ATM_GATEWAY_PROVIDER_KEY_FILE=/etc/gwq/provider.key \
 ATM_GATEWAY_TLS_CERT_FILE=/etc/gwq/gateway.pem ATM_GATEWAY_TLS_KEY_FILE=/etc/gwq/gateway.key ATM_GATEWAY_PRICE_FILE=/etc/gwq/prices.json ATM_GATEWAY_QUOTA_USD=1.00 \
 ATM_GATEWAY_TOKEN_SECRET=0123456789abcdef0123456789abcdef0123 ATM_GATEWAY_QUOTA_REQUESTS=6 ATM_GATEWAY_QUOTA_TOKENS=200000 ATM_GATEWAY_MAX_OUTPUT_TOKENS=100 \
 ATM_GATEWAY_MAX_BODY_BYTES=${GWQ_MAX_BODY:-4096} ATM_GATEWAY_IDLE_TIMEOUT=3s ATM_GATEWAY_FIREWALL_ATTESTATION=sandbox-bridge-to-gateway-only /tmp/gwq gateway </dev/null >/tmp/gwq-gw.log 2>&1 &)
sleep 2; cat /tmp/gwq-gw.log /tmp/gwq-up.log; sudo ss -ltnp | grep -E "8082|9443|9100"
