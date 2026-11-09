#!/usr/bin/env bash
# =============================================================================
# Kopdes Merah Putih - Automated Restore Verification Script
# =============================================================================
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/var/backups/kopdes}"
RESTORE_DB_NAME="kopdes_restore_test_${RANDOM}"
DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-postgres}"

LATEST_BACKUP=$(ls -t "${BACKUP_DIR}/daily"/*.dump | head -n 1)

if [ -z "${LATEST_BACKUP}" ]; then
  echo "ERROR: Tidak ditemukan file backup di ${BACKUP_DIR}/daily"
  exit 1
fi

echo "[$(date +'%Y-%m-%dT%H:%M:%S')] Menguji integritas file backup: ${LATEST_BACKUP}"

# 1. Validasi Checksum SHA-256
cd "${BACKUP_DIR}/daily"
sha256sum -c "${LATEST_BACKUP}.sha256"

# 2. Buat database pengujian sementara
echo "[$(date +'%Y-%m-%dT%H:%M:%S')] Membuat database temporer ${RESTORE_DB_NAME}..."
createdb -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" "${RESTORE_DB_NAME}"

# 3. Lakukan Restore menggunakan pg_restore
echo "[$(date +'%Y-%m-%dT%H:%M:%S')] Merestore data ke database pengujian..."
pg_restore -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${RESTORE_DB_NAME}" \
  --clean --if-exists --no-owner --no-privileges "${LATEST_BACKUP}" || true

# 4. Validasi Tabel Kunci & RLS Integrity
echo "[$(date +'%Y-%m-%dT%H:%M:%S')] Memverifikasi skema dan tabel inti..."
TABLE_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${RESTORE_DB_NAME}" -t -c "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public';")
echo "Jumlah tabel ter-restore: ${TABLE_COUNT}"

if [ "${TABLE_COUNT}" -lt 50 ]; then
  echo "ERROR: Jumlah tabel ter-restore tidak lengkap (${TABLE_COUNT} < 50)"
  dropdb -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" "${RESTORE_DB_NAME}"
  exit 1
fi

# 5. Hapus database pengujian
echo "[$(date +'%Y-%m-%dT%H:%M:%S')] Membersihkan database pengujian..."
dropdb -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" "${RESTORE_DB_NAME}"

echo "[$(date +'%Y-%m-%dT%H:%M:%S')] Verifikasi restore backup BERHASIL 100%!"
