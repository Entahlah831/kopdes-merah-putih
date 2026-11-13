#!/usr/bin/env bash
# Script Backup Harian PostgreSQL 16 Kopdes Merah Putih (terparameterisasi dari environment)
set -euo pipefail

if [ -n "${ENV_FILE:-}" ] && [ -f "${ENV_FILE}" ]; then
  while IFS= read -r _line || [ -n "${_line}" ]; do
    _line="${_line%$'\r'}"
    case "${_line}" in
      ''|\#*) continue ;;
    esac
    if [[ "${_line}" == *=* ]]; then
      _key="${_line%%=*}"
      _val="${_line#*=}"
      _key="${_key#"${_key%%[![:space:]]*}"}"
      _key="${_key%"${_key##*[![:space:]]}"}"
      if [[ "${_key}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
        export "${_key}=${_val}"
      fi
    fi
  done < "${ENV_FILE}"
fi

BACKUP_DIR="${BACKUP_DIR:?BACKUP_DIR wajib disetel}"
DB_HOST="${DB_HOST:?DB_HOST wajib disetel}"
DB_PORT="${DB_PORT:?DB_PORT wajib disetel}"
DB_NAME="${DB_NAME:?DB_NAME wajib disetel}"
DB_USER="${POSTGRES_USER:-${DB_USER:?POSTGRES_USER atau DB_USER wajib disetel}}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"
export PGPASSWORD="${PGPASSWORD:-${POSTGRES_PASSWORD:-${DB_PASSWORD:?PGPASSWORD/POSTGRES_PASSWORD/DB_PASSWORD wajib disetel}}}"

TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
mkdir -p "${BACKUP_DIR}/daily" "${BACKUP_DIR}/wal_archive"

echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Memulai backup database ${DB_NAME} (${DB_HOST}:${DB_PORT})..."
DUMP_FILE="${BACKUP_DIR}/daily/${DB_NAME}_${TIMESTAMP}.dump"
pg_dump -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -F c -b -f "${DUMP_FILE}"

if [ -n "${BACKUP_ENCRYPTION_KEY:-}" ]; then
  openssl enc -aes-256-cbc -pbkdf2 -salt -in "${DUMP_FILE}" -out "${DUMP_FILE}.enc" -pass env:BACKUP_ENCRYPTION_KEY
  rm -f "${DUMP_FILE}"
  ARTIFACT="${DUMP_FILE}.enc"
else
  ARTIFACT="${DUMP_FILE}"
fi

sha256sum "${ARTIFACT}" > "${ARTIFACT}.sha256"
echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Artifact tersimpan: ${ARTIFACT} (SHA-256 dicatat)."

find "${BACKUP_DIR}/daily" -name "*.dump*" -mtime +"${BACKUP_RETENTION_DAYS}" -delete
echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Backup selesai."
