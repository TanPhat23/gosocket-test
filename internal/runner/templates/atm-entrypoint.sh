#!/bin/sh
# Gate 0G: load the per-instance OpenCode password from a tmpfs secret file, unlink it, then exec.
# The password is never accepted from the container environment, never printed, and never put in argv.
# An optional gateway config file (OpenCode JSON holding a capability token) is loaded the same way. The
# controller writes it before the password, so the password file doubles as the "ready" signal.
set -eu
secret_file="${ATM_SECRET_FILE:-/run/secrets/opencode}"
wait_seconds="${ATM_SECRET_WAIT_SECONDS:-30}"
config_file="${ATM_CONFIG_FILE:-/run/secrets/opencode-config}"
unset OPENCODE_PASSWORD OPENCODE_SERVER_PASSWORD OPENCODE_CONFIG_CONTENT

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
if [ -s "$config_file" ]; then
	OPENCODE_CONFIG_CONTENT="$(cat "$config_file")"
	rm -f "$config_file"
	export OPENCODE_CONFIG_CONTENT
fi
exec "$@"
