#!/usr/bin/env bash
# =============================================================================
# Kopdes Merah Putih - Automated Backup & PITR Script
# =============================================================================
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/var/backups/kopdes}"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-kopdes_db}"
DB_USER="${DB_USER:-kopdes_app}"

mkdir -p "${BACKUP_DIR}/daily"
mkdir -p "${BACKUP_DIR}/wal_archive"

echo "[$(date +'%Y-%m-%dT%H:%M:%S')] Memulai proses backup database ${DB_NAME}..."

# 1. Snapshot Logical Backup Terkompresi (pg_dump -Fc)
DUMP_FILE="${BACKUP_DIR}/daily/${DB_NAME}_${TIMESTAMP}.dump"
pg_dump -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" \
  -F c -b -v -f "${DUMP_FILE}"

echo "[$(date +'%Y-%m-%dT%H:%M:%S')] Snapshot tersimpan di: ${DUMP_FILE}"

# 2. Hitung Checksum SHA-256 untuk verifikasi integritas data
sha256sum "${DUMP_FILE}" > "${DUMP_FILE}.sha256"
echo "[$(date +'%Y-%m-%dT%H:%M:%S')] Checksum SHA-256 dihasilkan: $(cat "${DUMP_FILE}.sha256")"

# 3. Retensi Backup: Hapus backup yang lebih lama dari 30 hari
find "${BACKUP_DIR}/daily" -name "*.dump" -mtime +30 -delete
find "${BACKUP_DIR}/daily" -name "*.sha256" -mtime +30 -delete

echo "[$(date +'%Y-%m-%dT%H:%M:%S')] Backup selesai dengan sukses."
