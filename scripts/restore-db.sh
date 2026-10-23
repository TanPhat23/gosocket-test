#!/usr/bin/env bash
set -euo pipefail

# Database restore script for Agent Task Manager
if [ $# -lt 1 ]; then
  echo "Usage: $0 <path_to_backup_file.dump>"
  exit 1
fi

BACKUP_FILE="$1"
if [ ! -f "${BACKUP_FILE}" ]; then
  echo "Error: Backup file '${BACKUP_FILE}' not found."
  exit 1
fi

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-agent-postgres}"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_DB="${POSTGRES_DB:-agent_tasks}"

TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
REMOTE_TEMP="/tmp/restore_${TIMESTAMP}.dump"

echo "Copying '${BACKUP_FILE}' to container '${POSTGRES_CONTAINER}:${REMOTE_TEMP}'..."
docker cp "${BACKUP_FILE}" "${POSTGRES_CONTAINER}:${REMOTE_TEMP}"

echo "Restoring database '${POSTGRES_DB}' in container '${POSTGRES_CONTAINER}'..."
# pg_restore: --clean drops database objects before recreating them; --if-exists prevents errors on missing objects
docker exec -t "${POSTGRES_CONTAINER}" pg_restore -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" --clean --if-exists "${REMOTE_TEMP}" || {
  echo "Warning: pg_restore completed with non-zero exit or minor warnings."
}

docker exec "${POSTGRES_CONTAINER}" rm "${REMOTE_TEMP}"
echo "Database restore process finished."
