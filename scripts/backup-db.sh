#!/usr/bin/env bash
set -euo pipefail

# Database backup script for Agent Task Manager
BACKUP_DIR="${BACKUP_DIR:-./backups}"
TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
BACKUP_FILE="${BACKUP_DIR}/agent_tasks_${TIMESTAMP}.dump"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-agent-postgres}"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_DB="${POSTGRES_DB:-agent_tasks}"

mkdir -p "${BACKUP_DIR}"

echo "Starting backup of database '${POSTGRES_DB}' from container '${POSTGRES_CONTAINER}'..."
docker exec -t "${POSTGRES_CONTAINER}" pg_dump -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -F c -f "/tmp/backup_${TIMESTAMP}.dump"
docker cp "${POSTGRES_CONTAINER}:/tmp/backup_${TIMESTAMP}.dump" "${BACKUP_FILE}"
docker exec "${POSTGRES_CONTAINER}" rm "/tmp/backup_${TIMESTAMP}.dump"

echo "Backup completed successfully: ${BACKUP_FILE}"
ls -lh "${BACKUP_FILE}"
