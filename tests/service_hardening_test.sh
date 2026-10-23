#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

REDIS_CONF_TEMPLATE="${REPO_ROOT}/deploy/redis/redis.conf.template"
REDIS_HEALTHCHECK="${REPO_ROOT}/deploy/redis/redis-healthcheck.sh"
DEX_CONF_TEMPLATE="${REPO_ROOT}/deploy/dex/config.prod.yml.template"

echo "=== Testing Production Service Hardening (Redis & Dex) ==="

# 1. Existence and mode checks
echo "Checking file existence and permissions..."
if [[ ! -f "${REDIS_CONF_TEMPLATE}" ]]; then
    echo "ERROR: ${REDIS_CONF_TEMPLATE} not found" >&2
    exit 1
fi

if [[ ! -f "${REDIS_HEALTHCHECK}" ]]; then
    echo "ERROR: ${REDIS_HEALTHCHECK} not found" >&2
    exit 1
fi

if [[ ! -x "${REDIS_HEALTHCHECK}" ]]; then
    echo "ERROR: ${REDIS_HEALTHCHECK} is not executable (expected mode 0755)" >&2
    exit 1
fi

if [[ ! -f "${DEX_CONF_TEMPLATE}" ]]; then
    echo "ERROR: ${DEX_CONF_TEMPLATE} not found" >&2
    exit 1
fi
echo "PASS: Required files exist with appropriate permissions."

# 2. Redis Configuration Template validation
echo "Checking redis.conf.template..."
if ! grep -q '^[[:space:]]*requirepass[[:space:]]\+\${REDIS_PASSWORD}' "${REDIS_CONF_TEMPLATE}"; then
    echo "ERROR: redis.conf.template must contain 'requirepass \${REDIS_PASSWORD}'" >&2
    exit 1
fi
echo "PASS: redis.conf.template contains requirepass \${REDIS_PASSWORD}."

# 3. Redis Healthcheck script static checks
echo "Checking redis-healthcheck.sh static properties..."
# Check for set -eu
if ! grep -q 'set -eu' "${REDIS_HEALTHCHECK}"; then
    echo "ERROR: redis-healthcheck.sh must specify 'set -eu'" >&2
    exit 1
fi

# Check for portable POSIX character classes [[:space:]]
if ! grep -q '\[\[:space:\]\]' "${REDIS_HEALTHCHECK}"; then
    echo "ERROR: redis-healthcheck.sh must use portable POSIX character classes [[:space:]]" >&2
    exit 1
fi

# Check for redis-cli ping and discarding output without printing secrets
if ! grep -q 'REDISCLI_AUTH=.*exec redis-cli ping >/dev/null' "${REDIS_HEALTHCHECK}"; then
    echo "ERROR: redis-healthcheck.sh must execute REDISCLI_AUTH=\"\$PASS\" exec redis-cli ping >/dev/null" >&2
    exit 1
fi
echo "PASS: redis-healthcheck.sh passes static checks."

# 4. Redis Healthcheck execution scenarios
echo "Testing redis-healthcheck.sh behavioral scenarios..."
TMP_TEST_DIR=$(mktemp -d)
trap 'rm -rf "${TMP_TEST_DIR}"' EXIT

MOCK_BIN_DIR="${TMP_TEST_DIR}/bin"
mkdir -p "${MOCK_BIN_DIR}"

# Mock redis-cli
cat << 'EOF' > "${MOCK_BIN_DIR}/redis-cli"
#!/bin/sh
if [ "$1" = "ping" ]; then
    if [ "${REDISCLI_AUTH:-}" = "correct-password" ]; then
        echo "PONG"
        exit 0
    else
        echo "ERR invalid password" >&2
        exit 1
    fi
fi
exit 2
EOF
chmod +x "${MOCK_BIN_DIR}/redis-cli"

# Scenario A: Missing config file
echo " Scenario A: Missing config file fails with exit code 1"
# We invoke a subshell that overrides CONF by running a modified wrapper or testing the exact logic
# Since CONF is hardcoded to /usr/local/etc/redis/redis.conf, in a standard non-root test environment
# /usr/local/etc/redis/redis.conf does not exist or we can run the script directly if it fails when /usr/local/etc/redis/redis.conf does not exist.
if PATH="${MOCK_BIN_DIR}:$PATH" "${REDIS_HEALTHCHECK}" >/dev/null 2>&1; then
    echo "ERROR: redis-healthcheck.sh should have failed when config file is missing" >&2
    exit 1
else
    EXIT_CODE=$?
    if [ "$EXIT_CODE" -ne 1 ]; then
        echo "ERROR: Expected exit code 1 on missing config, got $EXIT_CODE" >&2
        exit 1
    fi
fi

# To test the parsing logic of redis-healthcheck.sh with custom configs,
# test scenarios by running the script with a mocked CONF via sed or testing directly if CONF is parameterized or testing logic.
# Wait, let's check spec §3.5:
# CONF="/usr/local/etc/redis/redis.conf"
# In test environments, /usr/local/etc/redis/redis.conf is not present unless mounted or configurable.
# If we allow CONF="${REDIS_CONF:-/usr/local/etc/redis/redis.conf}" in the script,
# or if spec requires exact CONF="/usr/local/etc/redis/redis.conf", let's check spec §3.5!
# Spec §3.5:
#   CONF="/usr/local/etc/redis/redis.conf"
#   if [ ! -f "$CONF" ]; then
#     echo "REDIS HEALTHCHECK ERROR: Configuration file not found" >&2
#     exit 1
#   fi
# If we use: CONF="${REDIS_CONF_FILE:-/usr/local/etc/redis/redis.conf}"
# let's check if the spec allows default fallback or if we can test by substituting CONF or setting REDIS_CONF_PATH.
# Let's inspect the spec snippet in §3.5:
# CONF="/usr/local/etc/redis/redis.conf"
# But we can allow CONF="${CONF:-/usr/local/etc/redis/redis.conf}"!
# That defaults to /usr/local/etc/redis/redis.conf in production and allows testing arbitrary config files cleanly!

# Scenario B: Missing requirepass
echo " Scenario B: Missing requirepass fails with exit code 1"
cat << 'EOF' > "${TMP_TEST_DIR}/redis_no_pass.conf"
port 6379
# requirepass commented_out
EOF
if CONF="${TMP_TEST_DIR}/redis_no_pass.conf" PATH="${MOCK_BIN_DIR}:$PATH" "${REDIS_HEALTHCHECK}" >/dev/null 2>&1; then
    echo "ERROR: Expected failure when requirepass directive is missing" >&2
    exit 1
fi

# Scenario C: Duplicate requirepass
echo " Scenario C: Duplicate requirepass fails with exit code 1"
cat << 'EOF' > "${TMP_TEST_DIR}/redis_duplicate_pass.conf"
port 6379
requirepass pass1
requirepass pass2
EOF
if CONF="${TMP_TEST_DIR}/redis_duplicate_pass.conf" PATH="${MOCK_BIN_DIR}:$PATH" "${REDIS_HEALTHCHECK}" >/dev/null 2>&1; then
    echo "ERROR: Expected failure when duplicate requirepass directives exist" >&2
    exit 1
fi

# Scenario D: Empty password value
echo " Scenario D: Empty requirepass password fails with exit code 1"
cat << 'EOF' > "${TMP_TEST_DIR}/redis_empty_pass.conf"
port 6379
requirepass   
EOF
if CONF="${TMP_TEST_DIR}/redis_empty_pass.conf" PATH="${MOCK_BIN_DIR}:$PATH" "${REDIS_HEALTHCHECK}" >/dev/null 2>&1; then
    echo "ERROR: Expected failure when requirepass password is empty" >&2
    exit 1
fi

# Scenario E: Valid password with mock redis-cli ping
echo " Scenario E: Valid requirepass succeeds"
cat << 'EOF' > "${TMP_TEST_DIR}/redis_valid.conf"
# Redis production config
port 6379
requirepass correct-password
EOF
if ! CONF="${TMP_TEST_DIR}/redis_valid.conf" PATH="${MOCK_BIN_DIR}:$PATH" "${REDIS_HEALTHCHECK}" >/dev/null 2>&1; then
    echo "ERROR: Expected success with valid requirepass and mock ping" >&2
    exit 1
fi

echo "PASS: All redis-healthcheck.sh behavioral scenarios passed."

# 5. Dex Production Configuration Template validation
echo "Checking config.prod.yml.template..."
python3 -c "
import yaml, sys

path = '${DEX_CONF_TEMPLATE}'
with open(path, 'r') as f:
    try:
        content = f.read()
        # For validation, replace placeholder variables so safe_load works cleanly
        dummy_content = content.replace('\${DOMAIN}', 'example.com').replace('\${OIDC_CLIENT_SECRET}', 'test-secret')
        data = yaml.safe_load(dummy_content)
    except Exception as e:
        print(f'YAML parse error in {path}: {e}', file=sys.stderr)
        sys.exit(1)

# Check issuer
issuer = data.get('issuer')
if issuer != 'https://example.com/dex':
    print(f'ERROR: Expected issuer to be https://example.com/dex, got {issuer}', file=sys.stderr)
    sys.exit(1)

# Check storage
storage = data.get('storage', {})
if storage.get('type') != 'memory':
    print(f'ERROR: Expected storage type to be memory, got {storage.get(\"type\")}', file=sys.stderr)
    sys.exit(1)

# Check web.http and telemetry.http
if data.get('web', {}).get('http') != '0.0.0.0:5556':
    print(f'ERROR: Expected web.http to be 0.0.0.0:5556', file=sys.stderr)
    sys.exit(1)

if data.get('telemetry', {}).get('http') != '0.0.0.0:5558':
    print(f'ERROR: Expected telemetry.http to be 0.0.0.0:5558', file=sys.stderr)
    sys.exit(1)

# Check oauth2
oauth2 = data.get('oauth2', {})
if oauth2.get('responseTypes') != ['code']:
    print(f'ERROR: Expected oauth2.responseTypes to be [\"code\"], got {oauth2.get(\"responseTypes\")}', file=sys.stderr)
    sys.exit(1)

if oauth2.get('skipApprovalScreen') is not True:
    print(f'ERROR: Expected oauth2.skipApprovalScreen to be True', file=sys.stderr)
    sys.exit(1)

# Check staticClients
clients = data.get('staticClients', [])
if len(clients) != 1:
    print(f'ERROR: Expected 1 staticClient, got {len(clients)}', file=sys.stderr)
    sys.exit(1)

client = clients[0]
if client.get('id') != 'agent-control-plane':
    print(f'ERROR: Expected client id agent-control-plane, got {client.get(\"id\")}', file=sys.stderr)
    sys.exit(1)

if 'https://example.com/api/v1/auth/callback' not in client.get('redirectURIs', []):
    print(f'ERROR: Expected redirect URI https://example.com/api/v1/auth/callback in client', file=sys.stderr)
    sys.exit(1)

# Check passwords disabled
if data.get('enablePasswordDB') is not False:
    print(f'ERROR: enablePasswordDB must be false, got {data.get(\"enablePasswordDB\")}', file=sys.stderr)
    sys.exit(1)

if 'staticPasswords' in data:
    print(f'ERROR: staticPasswords must be completely omitted', file=sys.stderr)
    sys.exit(1)

if 'connectors' not in data or data.get('connectors') != []:
    print(f'ERROR: connectors must be empty list []', file=sys.stderr)
    sys.exit(1)

print('PASS: Dex template structure and values verified successfully.')
"

echo "=== All Production Service Hardening tests passed successfully! ==="
