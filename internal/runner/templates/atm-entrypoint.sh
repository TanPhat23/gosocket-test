#!/bin/sh
# Gate 0G: load the per-instance OpenCode password from a tmpfs secret file, unlink it, then exec.
# The password is never accepted from the container environment, never printed, and never put in argv.
set -eu
secret_file="${ATM_SECRET_FILE:-/run/secrets/opencode}"
wait_seconds="${ATM_SECRET_WAIT_SECONDS:-30}"
unset OPENCODE_PASSWORD OPENCODE_SERVER_PASSWORD

i=0
while [ ! -s "$secret_file" ]; do
	i=$((i + 1))
	if [ "$i" -gt $((wait_seconds * 10)) ]; then
		echo "atm-entrypoint: secret file not provisioned; refusing to start" >&2
		exit 78
	fi
	sleep 0.1
done

OPENCODE_PASSWORD="$(cat "$secret_file")"
rm -f "$secret_file"
if [ -z "$OPENCODE_PASSWORD" ]; then
	echo "atm-entrypoint: empty secret; refusing to start" >&2
	exit 78
fi
export OPENCODE_PASSWORD
exec "$@"
