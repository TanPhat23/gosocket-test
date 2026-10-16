#!/bin/sh
# deploy/redis/redis-healthcheck.sh (mode 0755)
set -eu

CONF="${CONF:-/usr/local/etc/redis/redis.conf}"
if [ ! -f "$CONF" ]; then
  echo "REDIS HEALTHCHECK ERROR: Configuration file not found" >&2
  exit 1
fi

# Extract all uncommented requirepass lines using portable POSIX whitespace matching
MATCHES=$(grep -E '^[[:space:]]*requirepass[[:space:]]+' "$CONF" || true)
COUNT=$(echo "$MATCHES" | grep -c -E '^[[:space:]]*requirepass[[:space:]]+' || true)

if [ "$COUNT" -ne 1 ]; then
  echo "REDIS HEALTHCHECK ERROR: Expected exactly 1 requirepass directive, found $COUNT" >&2
  exit 1
fi

# Parse the password argument safely without echoing secret values
PASS=$(echo "$MATCHES" | awk '{print $2}')
if [ -z "$PASS" ]; then
  echo "REDIS HEALTHCHECK ERROR: Extracted requirepass directive is empty" >&2
  exit 1
fi

# Execute authenticated ping; output is discarded to prevent secret or status leak
REDISCLI_AUTH="$PASS" exec redis-cli ping >/dev/null
