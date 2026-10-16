#!/usr/bin/env bash
set -euo pipefail
umask 077

ENV_FILE=".env"
FORCE=0

for arg in "$@"; do
  case "$arg" in
    --force) FORCE=1 ;;
    *) echo "Unknown option: $arg" >&2; exit 1 ;;
  esac
done

# Require and strictly validate DOMAIN and ACME_EMAIL to prevent newline injection
DOMAIN="${DOMAIN:-}"
ACME_EMAIL="${ACME_EMAIL:-}"

if [ -z "$DOMAIN" ] || [ -z "$ACME_EMAIL" ]; then
  echo "ERROR: DOMAIN and ACME_EMAIL must be explicitly set in the environment." >&2
  exit 1
fi

# Reject newlines or carriage returns in inputs
if [[ "$DOMAIN" =~ [$'\r\n'] ]] || [[ "$ACME_EMAIL" =~ [$'\r\n'] ]]; then
  echo "ERROR: Control or newline characters detected in input parameters." >&2
  exit 1
fi

# Validate FQDN syntax (simple RFC-compliant pattern)
if ! [[ "$DOMAIN" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$ ]]; then
  echo "ERROR: Invalid DOMAIN format. Must be a valid fully-qualified domain name." >&2
  exit 1
fi

# Validate Email syntax
if ! [[ "$ACME_EMAIL" =~ ^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$ ]]; then
  echo "ERROR: Invalid ACME_EMAIL format. Must be a valid RFC-5322 email address." >&2
  exit 1
fi

# Fail closed if target path is a symlink
if [ -L "$ENV_FILE" ]; then
  echo "ERROR: $ENV_FILE is a symbolic link. Aborting for security." >&2
  exit 1
fi

# Validate mandatory template existence fail-closed before generating any secrets
if [ ! -f "deploy/dex/config.prod.yml.template" ]; then
  echo "ERROR: Missing required template: deploy/dex/config.prod.yml.template" >&2
  exit 1
fi

if [ ! -f "deploy/redis/redis.conf.template" ]; then
  echo "ERROR: Missing required template: deploy/redis/redis.conf.template" >&2
  exit 1
fi

if [ ! -f "deploy/redis/redis-healthcheck.sh" ]; then
  echo "ERROR: Missing required healthcheck script: deploy/redis/redis-healthcheck.sh" >&2
  exit 1
fi

# Generate high-entropy secrets into private shell variables first
POSTGRES_DB="agent_tasks"
POSTGRES_USER="agent_user"
POSTGRES_PASSWORD=$(openssl rand -hex 24)
REDIS_PASSWORD=$(openssl rand -hex 24)
SECRET_ENCRYPTION_KEY=$(openssl rand -hex 32)
SESSION_SECRET=$(openssl rand -base64 32)
OIDC_CLIENT_ID="agent-control-plane"
OIDC_CLIENT_SECRET=$(openssl rand -hex 24)

# Setup staging directory for atomic generation and rollback
STAGE_DIR=$(mktemp -d .bootstrap.stage.XXXXXXXXXX)
chmod 0700 "$STAGE_DIR"

cleanup() {
  rm -rf "$STAGE_DIR"
}
trap cleanup EXIT

TMP_ENV="$STAGE_DIR/.env"
DEX_TMP="$STAGE_DIR/config.prod.yml"
REDIS_TMP="$STAGE_DIR/redis.conf"

# 1. Assemble staged .env content
cat <<EOF > "$TMP_ENV"
# Agent Task Manager - Production Environment Configuration
# Generated on: $(date -u +"%Y-%m-%dT%H:%M:%SZ")

DOMAIN=${DOMAIN}
ACME_EMAIL=${ACME_EMAIL}

POSTGRES_DB=${POSTGRES_DB}
POSTGRES_USER=${POSTGRES_USER}
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}

REDIS_PASSWORD=${REDIS_PASSWORD}
SECRET_ENCRYPTION_KEY=${SECRET_ENCRYPTION_KEY}

SESSION_SECRET=${SESSION_SECRET}
OIDC_CLIENT_ID=${OIDC_CLIENT_ID}
OIDC_CLIENT_SECRET=${OIDC_CLIENT_SECRET}

GIT_SSL_NO_VERIFY=false
APP_ENV=production
EOF
chmod 0600 "$TMP_ENV"

# 2. Render dependent service configurations using the exact same generated secrets
export DOMAIN OIDC_CLIENT_SECRET REDIS_PASSWORD
envsubst '${DOMAIN} ${OIDC_CLIENT_SECRET}' < deploy/dex/config.prod.yml.template > "$DEX_TMP"
# Dex runs as UID 1001: restricted mode 0640 (requires operator group 1001 or 0750 dir)
chmod 0640 "$DEX_TMP"

envsubst '${REDIS_PASSWORD}' < deploy/redis/redis.conf.template > "$REDIS_TMP"
# Redis runs as UID 999: restricted mode 0640 (requires operator group 999 or 0750 dir)
chmod 0640 "$REDIS_TMP"

chmod 0755 "deploy/redis/redis-healthcheck.sh"

# 3. Preflight check: reject symlinks in targets and all ancestor directories
TARGET_FILES=("$ENV_FILE" "deploy/dex/config.prod.yml" "deploy/redis/redis.conf")
PARENT_DIRS=("./" "deploy" "deploy/dex" "deploy/redis")

# Fail closed if any target or parent directory component is a symbolic link
for pdir in "${PARENT_DIRS[@]}"; do
  if [ -L "$pdir" ]; then
    echo "ERROR: Ancestor directory $pdir is a symbolic link. Aborting for security." >&2
    exit 1
  fi
done

for target in "${TARGET_FILES[@]}"; do
  if [ -L "$target" ]; then
    echo "ERROR: Target path $target is a symbolic link. Aborting for security." >&2
    exit 1
  fi
done

if [ "$FORCE" -ne 1 ]; then
  for target in "${TARGET_FILES[@]}"; do
    if [ -e "$target" ]; then
      echo "ERROR: Target file $target already exists. Aborting without modifying existing configurations. Use --force to overwrite." >&2
      exit 1
    fi
  done
fi

# 4. Operator ownership and directory permission verification:
# Dex runs as UID 1001, Redis runs as UID 999.
OPERATOR_UID=$(id -u)
mkdir -p deploy/redis deploy/dex
chmod 0750 deploy/dex deploy/redis

# Enforce operator ownership and non-writable permissions on parent directory components (POSIX mode bits)
for pdir in "./" "deploy" "deploy/dex" "deploy/redis"; do
  powner=$(stat -c '%u' "$pdir")
  if [ "$powner" -ne "$OPERATOR_UID" ] && [ "$powner" -ne 0 ]; then
    echo "ERROR: Ancestor directory $pdir is owned by untrusted UID $powner (expected $OPERATOR_UID or 0). Aborting." >&2
    exit 1
  fi
  # Verify directory has no group-write (0020) or other-write (0002) permissions
  pmode=$(stat -c '%a' "$pdir")
  if [ "$(( 0$pmode & 0022 ))" -ne 0 ]; then
    echo "ERROR: Ancestor directory $pdir is group-writable or world-writable (mode $pmode). Aborting for security." >&2
    exit 1
  fi
done

# Attempt group assignment on staging templates and parent service directories
if [ "$OPERATOR_UID" -eq 0 ]; then
  chown 0:1001 deploy/dex "$DEX_TMP"
  chown 0:999 deploy/redis "$REDIS_TMP"
else
  # Unprivileged operator must possess effective group membership in GID 1001 and 999
  if ! chgrp 1001 "$DEX_TMP" deploy/dex 2>/dev/null; then
    echo "ERROR: Host permission prerequisite failed: cannot assign GID 1001 to Dex configuration or deploy/dex directory." >&2
    echo "Operator prerequisite: add user to GID 1001/999 or run privileged setup: sudo chgrp 1001 deploy/dex && sudo chgrp 999 deploy/redis" >&2
    exit 1
  fi
  if ! chgrp 999 "$REDIS_TMP" deploy/redis 2>/dev/null; then
    echo "ERROR: Host permission prerequisite failed: cannot assign GID 999 to Redis configuration or deploy/redis directory." >&2
    echo "Operator prerequisite: add user to GID 1001/999 or run privileged setup: sudo chgrp 1001 deploy/dex && sudo chgrp 999 deploy/redis" >&2
    exit 1
  fi
fi

# Confirm group search and read permissions fail-closed for both directories and configuration files
if [ "$(stat -c '%g' deploy/dex)" != "1001" ] || [ "$(stat -c '%a' deploy/dex)" != "750" ]; then
  echo "ERROR: Dex directory permission verification failed (expected GID 1001, mode 0750 on deploy/dex)." >&2
  exit 1
fi
if [ "$(stat -c '%g' "$DEX_TMP")" != "1001" ] || [ "$(stat -c '%a' "$DEX_TMP")" != "640" ]; then
  echo "ERROR: Dex configuration permission verification failed (expected GID 1001, mode 0640)." >&2
  exit 1
fi

if [ "$(stat -c '%g' deploy/redis)" != "999" ] || [ "$(stat -c '%a' deploy/redis)" != "750" ]; then
  echo "ERROR: Redis directory permission verification failed (expected GID 999, mode 0750 on deploy/redis)." >&2
  exit 1
fi
if [ "$(stat -c '%g' "$REDIS_TMP")" != "999" ] || [ "$(stat -c '%a' "$REDIS_TMP")" != "640" ]; then
  echo "ERROR: Redis configuration permission verification failed (expected GID 999, mode 0640)." >&2
  exit 1
fi

# 5. Staged publication with atomic no-replace / verified replace and rollback
# Notice on Multi-File Atomicity & TOCTOU Boundaries:
# Target directory trees (deploy/dex, deploy/redis) must be operator-owned without write access
# by untrusted principals, ensuring stable ancestor paths during publication.
# Individual file publishing uses kernel-enforced atomic no-replace (ln) or verified atomic replace (mv).
# Across power loss or uncatchable SIGKILL, multi-file publishing cannot be globally guaranteed
# without filesystem-level generation directories or activation pointers.
# To achieve practical recoverability, all preexisting targets are backed up first.
# If any publish step fails, all temporary files and newly created targets are removed,
# and all backups are restored before exiting.
BACKUP_DIR=$(mktemp -d .bootstrap.backup.XXXXXXXXXX)
chmod 0700 "$BACKUP_DIR"
rollback_list=()
newly_created=()
pending_dst_tmps=()

rollback_all() {
  echo "WARNING: Bootstrap publish failed. Rolling back all modified files..." >&2
  local cleanup_failed=0
  local restore_failed=0
  for dtmp in "${pending_dst_tmps[@]}"; do
    if [ -e "$dtmp" ] || [ -L "$dtmp" ]; then
      if ! rm -f "$dtmp" 2>/dev/null; then
        echo "WARNING: Failed to remove temporary secret file: $dtmp" >&2
        cleanup_failed=1
      fi
    fi
  done
  for created in "${newly_created[@]}"; do
    if [ -e "$created" ] || [ -L "$created" ]; then
      if ! rm -f "$created" 2>/dev/null; then
        echo "WARNING: Failed to remove newly created target: $created" >&2
        cleanup_failed=1
      fi
    fi
  done
  for entry in "${rollback_list[@]}"; do
    orig="${entry%%:*}"
    bak="${entry##*:}"
    if [ -f "$bak" ]; then
      if ! mv -f "$bak" "$orig" 2>/dev/null; then
        echo "CRITICAL ERROR: Failed to restore original file $orig from backup $bak!" >&2
        restore_failed=1
      fi
    fi
  done
  if [ "$restore_failed" -eq 1 ] || [ "$cleanup_failed" -eq 1 ]; then
    echo "MANUAL RECOVERY REQUIRED: Preexisting configuration backups preserved in $BACKUP_DIR" >&2
  else
    rm -rf "$BACKUP_DIR"
  fi
  rm -rf "$STAGE_DIR"
}

# Trap failure during publish
trap rollback_all ERR EXIT

# Backup all preexisting targets prior to moving any staged files
for target in "${TARGET_FILES[@]}"; do
  if [ -e "$target" ] || [ -L "$target" ]; then
    # Reject non-regular files (FIFOs, devices, sockets, directories) before attempting backup
    if [ ! -f "$target" ] && [ ! -L "$target" ]; then
      echo "ERROR: Target path $target exists but is not a regular file. Aborting." >&2
      exit 1
    fi
    bfile="$BACKUP_DIR/$(basename "$target").bak"
    cp -p "$target" "$bfile"
    rollback_list+=("$target:$bfile")
  fi
done

publish_file() {
  local src="$1"
  local dst="$2"
  local expected_mode="$3"
  local expected_gid="$4"
  local dst_dir
  dst_dir=$(dirname "$dst")

  # Verify parent directory is not a symlink, is operator-owned, and is not group/other-writable
  if [ -L "$dst_dir" ]; then
    echo "ERROR: Destination directory $dst_dir is a symbolic link. Aborting publication." >&2
    exit 1
  fi
  local powner
  powner=$(stat -c '%u' "$dst_dir")
  if [ "$powner" -ne "$OPERATOR_UID" ] && [ "$powner" -ne 0 ]; then
    echo "ERROR: Destination directory $dst_dir is owned by untrusted UID $powner. Aborting." >&2
    exit 1
  fi
  local pmode
  pmode=$(stat -c '%a' "$dst_dir")
  if [ "$(( 0$pmode & 0022 ))" -ne 0 ]; then
    echo "ERROR: Destination directory $dst_dir is group- or other-writable (mode $pmode). Aborting." >&2
    exit 1
  fi

  # Create destination temporary file in the target directory (guarantees same-filesystem atomic rename)
  local dst_tmp
  dst_tmp=$(mktemp "$dst_dir/.$(basename "$dst").tmp.XXXXXXXXXX")
  pending_dst_tmps+=("$dst_tmp")
  cp -p "$src" "$dst_tmp"

  # Validate dst_tmp is a regular file and strictly retained required mode and group
  if [ ! -f "$dst_tmp" ] || [ -L "$dst_tmp" ]; then
    echo "ERROR: Staged temporary $dst_tmp is not a regular file. Aborting." >&2
    exit 1
  fi
  if [ "$(stat -c '%a' "$dst_tmp")" != "$expected_mode" ]; then
    echo "ERROR: Staged temporary $dst_tmp mode mismatch (got $(stat -c '%a' "$dst_tmp"), expected $expected_mode). Aborting." >&2
    exit 1
  fi
  if [ -n "$expected_gid" ] && [ "$(stat -c '%g' "$dst_tmp")" != "$expected_gid" ]; then
    echo "ERROR: Staged temporary $dst_tmp GID mismatch (got $(stat -c '%g' "$dst_tmp"), expected $expected_gid). Aborting." >&2
    exit 1
  fi

  if [ "$FORCE" -ne 1 ]; then
    # Atomic kernel-enforced no-replace via hard link creation:
    # If $dst already exists (or raced into existence), ln fails atomically without replacing or following symlinks.
    if ! ln "$dst_tmp" "$dst" 2>/dev/null; then
      echo "ERROR: Atomic no-replace failed: destination $dst already exists or link failed." >&2
      exit 1
    fi
    # Register successfully linked destination before fallible cleanup
    newly_created+=("$dst")
    rm -f "$dst_tmp"
  else
    # In forced mode, reject destination if it is a symlink or non-regular file to prevent following links or clobbering devices
    if [ -L "$dst" ]; then
      echo "ERROR: Forced replacement of symbolic link $dst is prohibited." >&2
      exit 1
    fi
    if [ -e "$dst" ] && [ ! -f "$dst" ]; then
      echo "ERROR: Forced replacement of non-regular file $dst is prohibited." >&2
      exit 1
    fi
    if [ ! -e "$dst" ]; then
      newly_created+=("$dst")
    fi
    mv -f "$dst_tmp" "$dst"
  fi

  # Post-publication metadata verification on final target path
  if [ "$(stat -c '%a' "$dst")" != "$expected_mode" ]; then
    echo "ERROR: Published file $dst mode verification failed (got $(stat -c '%a' "$dst"), expected $expected_mode)." >&2
    exit 1
  fi
  if [ -n "$expected_gid" ] && [ "$(stat -c '%g' "$dst")" != "$expected_gid" ]; then
    echo "ERROR: Published file $dst GID verification failed (got $(stat -c '%g' "$dst"), expected $expected_gid)." >&2
    exit 1
  fi
}

publish_file "$TMP_ENV" "$ENV_FILE" "600" ""
publish_file "$DEX_TMP" "deploy/dex/config.prod.yml" "640" "1001"
publish_file "$REDIS_TMP" "deploy/redis/redis.conf" "640" "999"

# Publish succeeded completely: disarm trap and clean up backups
trap - ERR EXIT
rm -rf "$BACKUP_DIR" "$STAGE_DIR"
echo "SUCCESS: Staged and published production .env (mode 0600) and service configurations (mode 0640 in 0750 directories)."
