#!/usr/bin/env bash
set -euo pipefail

# Check if staticcheck is installed
if ! command -v staticcheck &> /dev/null; then
    if [ -x "$(go env GOPATH)/bin/staticcheck" ]; then
        STATICCHECK="$(go env GOPATH)/bin/staticcheck"
    else
        echo "==> staticcheck not found. Installing latest staticcheck..."
        go install honnef.co/go/tools/cmd/staticcheck@latest
        STATICCHECK="$(go env GOPATH)/bin/staticcheck"
    fi
else
    STATICCHECK="staticcheck"
fi

echo "==> Running staticcheck on ./cmd/... and ./internal/..."
$STATICCHECK ./cmd/... ./internal/...
echo "==> staticcheck passed cleanly! (0 findings)"
