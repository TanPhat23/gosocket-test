#!/usr/bin/env bash
set -euo pipefail

# Find repository root
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LICENSE_FILE="${REPO_ROOT}/LICENSE"

echo "Checking for LICENSE file at: ${LICENSE_FILE}"

if [[ ! -f "${LICENSE_FILE}" ]]; then
    echo "ERROR: LICENSE file does not exist at ${LICENSE_FILE}" >&2
    exit 1
fi

echo "Verifying MIT license content..."

if ! grep -q "Permission is hereby granted, free of charge" "${LICENSE_FILE}"; then
    echo "ERROR: Standard MIT license grant clause missing in ${LICENSE_FILE}" >&2
    exit 1
fi

if ! grep -q "Agent Task Manager Contributors" "${LICENSE_FILE}"; then
    echo "ERROR: Expected copyright notice missing in ${LICENSE_FILE}" >&2
    exit 1
fi

echo "SUCCESS: LICENSE file exists and contains valid MIT license text."
exit 0
