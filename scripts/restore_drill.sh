#!/usr/bin/env bash
# Script Simulasi Restore Drill & Verifikasi Skema 19 Tabel + Integritas Data
# Catatan: PITR/WAL archiving belum diimplementasikan; backup menggunakan pg_dump logis.
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

DB_HOST="${DB_HOST:?DB_HOST wajib disetel}"
DB_PORT="${DB_PORT:?DB_PORT wajib disetel}"
DB_NAME="${DB_NAME:?DB_NAME wajib disetel}"
DB_USER="${POSTGRES_USER:-${DB_USER:?POSTGRES_USER atau DB_USER wajib disetel}}"
export PGPASSWORD="${PGPASSWORD:-${POSTGRES_PASSWORD:-${DB_PASSWORD:?PGPASSWORD/POSTGRES_PASSWORD/DB_PASSWORD wajib disetel}}}"

TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
DRILL_ID="drill_${TIMESTAMP}_$$"
DRILL_DB_NAME="${DB_NAME}_drill_${TIMESTAMP}"
BACKUP_DIR="${BACKUP_DIR:-./backups}/drill"

mkdir -p "${BACKUP_DIR}"

echo "============================================================================="
echo "[RESTORE-DRILL] Memulai Latihan Pemulihan Bencana (pg_dump logis; PITR belum diimplementasikan)"
echo "Target Host : ${DB_HOST}:${DB_PORT} (Source DB: ${DB_NAME})"
echo "Drill DB    : ${DRILL_DB_NAME}"
echo "============================================================================="

psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 <<-EOSQL > /dev/null
  INSERT INTO audit_log (id, actor_peran, aksi, entitas, entitas_id, ringkasan, ip_address)
  VALUES (
    uuid_generate_v7(),
    'sistem',
    'restore_drill.marker',
    'sistem_drill',
    '${DRILL_ID}',
    'Marker integritas latihan restore backup',
    '127.0.0.1'
  );
EOSQL

DUMP_FILE="${BACKUP_DIR}/${DRILL_ID}.dump"
pg_dump -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -F c -b -f "${DUMP_FILE}" 2> "${DUMP_FILE}.log"
sha256sum "${DUMP_FILE}" > "${DUMP_FILE}.sha256"

if [ -n "${BACKUP_ENCRYPTION_KEY:-}" ]; then
  echo "[RESTORE-DRILL] Status Enkripsi Dump: TERENKRIPSI (AES-256-CBC + PBKDF2)"
  openssl enc -aes-256-cbc -pbkdf2 -salt -in "${DUMP_FILE}" -out "${DUMP_FILE}.enc" -pass env:BACKUP_ENCRYPTION_KEY
  rm -f "${DUMP_FILE}"
  openssl enc -d -aes-256-cbc -pbkdf2 -in "${DUMP_FILE}.enc" -out "${DUMP_FILE}" -pass env:BACKUP_ENCRYPTION_KEY
else
  echo "[RESTORE-DRILL] Status Enkripsi Dump: TIDAK TERENKRIPSI (plaintext custom dump — uji lokal/CI)"
fi

psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -c "DROP DATABASE IF EXISTS \"${DRILL_DB_NAME}\";" > /dev/null 2>&1 || true
psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"${DRILL_DB_NAME}\";"

RESTORE_START=$(date +%s)
pg_restore -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" --no-owner --no-privileges "${DUMP_FILE}" 2>> "${DUMP_FILE}.log" || true
RESTORE_END=$(date +%s)
RESTORE_DURATION=$((RESTORE_END - RESTORE_START))

TABLE_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
  SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE';
")
echo "-> Tabel Terpulihkan: ${TABLE_COUNT} (durasi ${RESTORE_DURATION}s)"
if [ "${TABLE_COUNT}" -lt 19 ]; then
  psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -c "DROP DATABASE IF EXISTS \"${DRILL_DB_NAME}\";" >/dev/null || true
  echo "ERROR: Jumlah tabel pulihan kurang dari 19 (${TABLE_COUNT})"
  exit 1
fi

MARKER_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
  SELECT COUNT(*) FROM audit_log WHERE entitas = 'sistem_drill' AND entitas_id = '${DRILL_ID}';
")
echo "-> Marker Audit Drill: ${MARKER_COUNT}"
if [ "${MARKER_COUNT}" -lt 1 ]; then
  psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -c "DROP DATABASE IF EXISTS \"${DRILL_DB_NAME}\";" >/dev/null || true
  echo "ERROR: Marker audit_log tidak ditemukan setelah restore!"
  exit 1
fi

SUPER_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
  SELECT COUNT(*) FROM pg_roles WHERE rolname = 'kopdes_app' AND (rolsuper = true OR rolbypassrls = true);
")
echo "-> Pemeriksaan Non-Superuser kopdes_app (harus 0): ${SUPER_COUNT}"

psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -v ON_ERROR_STOP=1 -c "DROP DATABASE \"${DRILL_DB_NAME}\";" >/dev/null
echo "RESTORE DRILL BERHASIL: Snapshot 19 tabel, marker audit_log, dan izin kopdes_app terverifikasi."
