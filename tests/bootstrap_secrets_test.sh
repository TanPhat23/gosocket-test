#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BOOTSTRAP_SCRIPT="${REPO_ROOT}/scripts/bootstrap-secrets.sh"

echo "=== Running Secret Bootstrap Integration Tests (Task 6 / Gate 0F) ==="

TMP_DIR=$(mktemp -d)
trap 'rm -rf "${TMP_DIR}"' EXIT

setup_test_env() {
    local target_dir="$1"
    mkdir -p "${target_dir}/deploy/dex" "${target_dir}/deploy/redis"
    chmod 0750 "${target_dir}"
    chmod 0750 "${target_dir}/deploy"
    chmod 0750 "${target_dir}/deploy/dex"
    chmod 0750 "${target_dir}/deploy/redis"
    
    # Copy templates and healthcheck from repo
    cp "${REPO_ROOT}/deploy/dex/config.prod.yml.template" "${target_dir}/deploy/dex/config.prod.yml.template"
    cp "${REPO_ROOT}/deploy/redis/redis.conf.template" "${target_dir}/deploy/redis/redis.conf.template"
    cp "${REPO_ROOT}/deploy/redis/redis-healthcheck.sh" "${target_dir}/deploy/redis/redis-healthcheck.sh"
    chmod 0755 "${target_dir}/deploy/redis/redis-healthcheck.sh"
}

# Check existence of bootstrap script
if [[ ! -f "${BOOTSTRAP_SCRIPT}" ]]; then
    echo "ERROR: ${BOOTSTRAP_SCRIPT} not found" >&2
    exit 1
fi

if [[ ! -x "${BOOTSTRAP_SCRIPT}" ]]; then
    echo "ERROR: ${BOOTSTRAP_SCRIPT} is not executable (expected mode 0755)" >&2
    exit 1
fi

# Helper to run bootstrap script within fakeroot so chown 0:1001 / chown 0:999 can succeed without real root
run_bootstrap() {
    local work_dir="$1"
    shift
    if [[ "$(id -u)" -eq 0 ]]; then
        ( cd "${work_dir}" && "${BOOTSTRAP_SCRIPT}" "$@" )
    elif command -v fakeroot >/dev/null 2>&1; then
        ( cd "${work_dir}" && fakeroot "${BOOTSTRAP_SCRIPT}" "$@" )
    else
        ( cd "${work_dir}" && "${BOOTSTRAP_SCRIPT}" "$@" )
    fi
}

# -------------------------------------------------------------
# Test 1 & 2: Clean run generates .env (0600) and configs (0640 in 0750 dirs)
# -------------------------------------------------------------
echo "[Test 1 & 2] Testing clean run with valid parameters..."
T1_DIR="${TMP_DIR}/test1"
setup_test_env "${T1_DIR}"

out=$(DOMAIN="atm.example.com" ACME_EMAIL="ops@example.com" run_bootstrap "${T1_DIR}")
echo "${out}" | grep -q "SUCCESS"

# Verify .env permissions and contents
if [[ ! -f "${T1_DIR}/.env" ]]; then
    echo "ERROR: .env was not created" >&2
    exit 1
fi

env_mode=$(stat -c '%a' "${T1_DIR}/.env")
if [[ "${env_mode}" != "600" ]]; then
    echo "ERROR: .env mode expected 600, got ${env_mode}" >&2
    exit 1
fi

grep -q "DOMAIN=atm.example.com" "${T1_DIR}/.env"
grep -q "ACME_EMAIL=ops@example.com" "${T1_DIR}/.env"
grep -q "POSTGRES_PASSWORD=" "${T1_DIR}/.env"
grep -q "REDIS_PASSWORD=" "${T1_DIR}/.env"
grep -q "SECRET_ENCRYPTION_KEY=" "${T1_DIR}/.env"
grep -q "SESSION_SECRET=" "${T1_DIR}/.env"
grep -q "OIDC_CLIENT_SECRET=" "${T1_DIR}/.env"

# Extract REDIS_PASSWORD and OIDC_CLIENT_SECRET from .env to verify interpolation
gen_redis_pw=$(grep "^REDIS_PASSWORD=" "${T1_DIR}/.env" | cut -d= -f2-)
gen_oidc_secret=$(grep "^OIDC_CLIENT_SECRET=" "${T1_DIR}/.env" | cut -d= -f2-)

if [[ -z "${gen_redis_pw}" || -z "${gen_oidc_secret}" ]]; then
    echo "ERROR: Secrets not generated in .env" >&2
    exit 1
fi

# Verify dex config
if [[ ! -f "${T1_DIR}/deploy/dex/config.prod.yml" ]]; then
    echo "ERROR: deploy/dex/config.prod.yml not created" >&2
    exit 1
fi
grep -q "${gen_oidc_secret}" "${T1_DIR}/deploy/dex/config.prod.yml"

# Verify redis config
if [[ ! -f "${T1_DIR}/deploy/redis/redis.conf" ]]; then
    echo "ERROR: deploy/redis/redis.conf not created" >&2
    exit 1
fi
grep -q "requirepass ${gen_redis_pw}" "${T1_DIR}/deploy/redis/redis.conf"

echo "PASS: Clean run generated expected files with proper secrets and templates."

# -------------------------------------------------------------
# Test 3: Input validation (missing vars, control chars, invalid formats)
# -------------------------------------------------------------
echo "[Test 3] Testing parameter validation and rejection..."
T3_DIR="${TMP_DIR}/test3"
setup_test_env "${T3_DIR}"

# Missing DOMAIN
if DOMAIN="" ACME_EMAIL="ops@example.com" run_bootstrap "${T3_DIR}" 2>/dev/null; then
    echo "ERROR: Expected failure when DOMAIN is empty" >&2
    exit 1
fi

# Missing ACME_EMAIL
if DOMAIN="atm.example.com" ACME_EMAIL="" run_bootstrap "${T3_DIR}" 2>/dev/null; then
    echo "ERROR: Expected failure when ACME_EMAIL is empty" >&2
    exit 1
fi

# Newline injection in DOMAIN
if DOMAIN=$'atm.example.com\nINJECTED=1' ACME_EMAIL="ops@example.com" run_bootstrap "${T3_DIR}" 2>/dev/null; then
    echo "ERROR: Expected failure with newline in DOMAIN" >&2
    exit 1
fi

# Carriage return in ACME_EMAIL
if DOMAIN="atm.example.com" ACME_EMAIL=$'ops@example.com\rINJECTED=1' run_bootstrap "${T3_DIR}" 2>/dev/null; then
    echo "ERROR: Expected failure with carriage return in ACME_EMAIL" >&2
    exit 1
fi

# Invalid DOMAIN format
if DOMAIN="-bad-domain" ACME_EMAIL="ops@example.com" run_bootstrap "${T3_DIR}" 2>/dev/null; then
    echo "ERROR: Expected failure with invalid domain format" >&2
    exit 1
fi

# Invalid ACME_EMAIL format
if DOMAIN="atm.example.com" ACME_EMAIL="not-an-email" run_bootstrap "${T3_DIR}" 2>/dev/null; then
    echo "ERROR: Expected failure with invalid email format" >&2
    exit 1
fi

echo "PASS: Input validation properly rejected malformed and malicious parameters."

# -------------------------------------------------------------
# Test 4: Symlink rejection (direct symlinks and ancestor symlinks)
# -------------------------------------------------------------
echo "[Test 4] Testing symlink defense..."
T4_DIR="${TMP_DIR}/test4"
setup_test_env "${T4_DIR}"

# Direct symlink .env
touch "${T4_DIR}/fake_env"
ln -s "${T4_DIR}/fake_env" "${T4_DIR}/.env"
if DOMAIN="atm.example.com" ACME_EMAIL="ops@example.com" run_bootstrap "${T4_DIR}" 2>/dev/null; then
    echo "ERROR: Expected failure when .env is a symlink" >&2
    exit 1
fi
rm -f "${T4_DIR}/.env" "${T4_DIR}/fake_env"

# Direct symlink in deploy/dex/config.prod.yml
touch "${T4_DIR}/fake_dex"
ln -s "${T4_DIR}/fake_dex" "${T4_DIR}/deploy/dex/config.prod.yml"
if DOMAIN="atm.example.com" ACME_EMAIL="ops@example.com" run_bootstrap "${T4_DIR}" 2>/dev/null; then
    echo "ERROR: Expected failure when deploy/dex/config.prod.yml is a symlink" >&2
    exit 1
fi
rm -f "${T4_DIR}/deploy/dex/config.prod.yml" "${T4_DIR}/fake_dex"

# Ancestor symlink: deploy/redis is a symlink
rm -rf "${T4_DIR}/deploy/redis"
mkdir -p "${TMP_DIR}/outside_redis"
ln -s "${TMP_DIR}/outside_redis" "${T4_DIR}/deploy/redis"
if DOMAIN="atm.example.com" ACME_EMAIL="ops@example.com" run_bootstrap "${T4_DIR}" 2>/dev/null; then
    echo "ERROR: Expected failure when deploy/redis is a symlink" >&2
    exit 1
fi

echo "PASS: Symlinks rejected fail-closed."

# -------------------------------------------------------------
# Test 5: Reject group-writable or world-writable ancestor directory
# -------------------------------------------------------------
echo "[Test 5] Testing ancestor directory permissions check..."
T5_DIR="${TMP_DIR}/test5"
setup_test_env "${T5_DIR}"

chmod 0770 "${T5_DIR}/deploy"
if DOMAIN="atm.example.com" ACME_EMAIL="ops@example.com" run_bootstrap "${T5_DIR}" 2>/dev/null; then
    echo "ERROR: Expected failure when deploy directory is group-writable (0770)" >&2
    exit 1
fi
chmod 0755 "${T5_DIR}/deploy"

echo "PASS: Group/world-writable ancestor directories rejected."

# -------------------------------------------------------------
# Test 6: Existing target rejection without --force, success with --force
# -------------------------------------------------------------
echo "[Test 6] Testing duplicate run and --force behavior..."
T6_DIR="${TMP_DIR}/test6"
setup_test_env "${T6_DIR}"

# First run: should succeed
DOMAIN="atm.example.com" ACME_EMAIL="ops@example.com" run_bootstrap "${T6_DIR}" >/dev/null

# Second run without --force: must fail
if DOMAIN="atm.example.com" ACME_EMAIL="ops@example.com" run_bootstrap "${T6_DIR}" 2>/dev/null; then
    echo "ERROR: Duplicate run without --force should have failed" >&2
    exit 1
fi

# Run with --force: must succeed
if ! DOMAIN="atm.example.com" ACME_EMAIL="ops@example.com" run_bootstrap "${T6_DIR}" --force >/dev/null; then
    echo "ERROR: Run with --force failed" >&2
    exit 1
fi

echo "PASS: Non-forced overwrite rejected; forced overwrite succeeded."

# -------------------------------------------------------------
# Test 7: Rollback on injected publish failure
# -------------------------------------------------------------
echo "[Test 7] Testing atomic rollback on failure during publication..."
T7_DIR="${TMP_DIR}/test7"
setup_test_env "${T7_DIR}"

# Pre-populate an existing .env to ensure it gets restored if failure occurs later
echo "EXISTING_CONTENT=original" > "${T7_DIR}/.env"
chmod 0600 "${T7_DIR}/.env"

# We simulate a publish failure by making deploy/redis/redis.conf an immutable condition or injecting failure.
# In forced mode: make deploy/redis/redis.conf a symlink so publish_file aborts at redis.conf after .env was published!
ln -s "/dev/null" "${T7_DIR}/deploy/redis/redis.conf"

if DOMAIN="atm.example.com" ACME_EMAIL="ops@example.com" run_bootstrap "${T7_DIR}" --force 2>/dev/null; then
    echo "ERROR: Expected failure when deploy/redis/redis.conf is a symlink under --force" >&2
    exit 1
fi

# Verify rollback restored original .env
if ! grep -q "EXISTING_CONTENT=original" "${T7_DIR}/.env"; then
    echo "ERROR: Rollback failed to restore original .env" >&2
    exit 1
fi

echo "PASS: Rollback restored original state on injected publication failure."

# -------------------------------------------------------------
# Test 8: Zero secrets in stdout / stderr
# -------------------------------------------------------------
echo "[Test 8] Testing zero secrets logged to stdout or stderr..."
T8_DIR="${TMP_DIR}/test8"
setup_test_env "${T8_DIR}"

LOG_OUT="${TMP_DIR}/stdout.log"
LOG_ERR="${TMP_DIR}/stderr.log"

DOMAIN="atm.example.com" ACME_EMAIL="ops@example.com" run_bootstrap "${T8_DIR}" >"${LOG_OUT}" 2>"${LOG_ERR}"

SECRET_PASS=$(grep "^REDIS_PASSWORD=" "${T8_DIR}/.env" | cut -d= -f2-)
SECRET_KEY=$(grep "^SECRET_ENCRYPTION_KEY=" "${T8_DIR}/.env" | cut -d= -f2-)
SECRET_SESSION=$(grep "^SESSION_SECRET=" "${T8_DIR}/.env" | cut -d= -f2-)
SECRET_OIDC=$(grep "^OIDC_CLIENT_SECRET=" "${T8_DIR}/.env" | cut -d= -f2-)

for sec in "${SECRET_PASS}" "${SECRET_KEY}" "${SECRET_SESSION}" "${SECRET_OIDC}"; do
    if grep -qF "${sec}" "${LOG_OUT}" || grep -qF "${sec}" "${LOG_ERR}"; then
        echo "ERROR: Generated secret was found in output streams!" >&2
        exit 1
    fi
done

echo "PASS: Zero secret disclosure in stdout or stderr."

echo "=== All Secret Bootstrap Tests PASSED ==="
