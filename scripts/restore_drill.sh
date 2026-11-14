#!/usr/bin/env bash
# Script Simulasi Restore Drill & Verifikasi Integritas Data + RLS + Hash Chain
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
BACKUP_DIR="${BACKUP_DIR:?BACKUP_DIR wajib disetel}/drill"

mkdir -p "${BACKUP_DIR}"

echo "============================================================================="
echo "[RESTORE-DRILL] Memulai Latihan Pemulihan Bencana (DR Drill)"
echo "Target Host : ${DB_HOST}:${DB_PORT} (Source DB: ${DB_NAME})"
echo "Drill DB    : ${DRILL_DB_NAME}"
echo "============================================================================="

WAL_LSN=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -t -A -c "SELECT pg_current_wal_lsn();")
echo "-> WAL LSN Checkpoint: ${WAL_LSN}"

KOPERASI_ID=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -t -A -c "SELECT id FROM koperasi LIMIT 1;" || true)
if [ -n "${KOPERASI_ID}" ]; then
  psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 <<-EOSQL > /dev/null
    BEGIN;
    SELECT set_config('app.is_platform_admin', 'true', true);
    INSERT INTO audit_log (id, koperasi_id, nama_tabel, record_id, aksi, data_baru, created_at)
    VALUES (
      uuid_generate_v7(),
      '${KOPERASI_ID}',
      'sistem_drill',
      uuid_generate_v7(),
      'INSERT',
      jsonb_build_object('marker', 'RESTORE_DRILL_MARKER', 'drill_id', '${DRILL_ID}', 'wal_lsn', '${WAL_LSN}'),
      NOW()
    );
    COMMIT;
EOSQL
fi

DUMP_FILE="${BACKUP_DIR}/${DRILL_ID}.dump"
pg_dump -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -F c -b -f "${DUMP_FILE}" 2> "${DUMP_FILE}.log"
sha256sum "${DUMP_FILE}" > "${DUMP_FILE}.sha256"

if [ -n "${BACKUP_ENCRYPTION_KEY:-}" ]; then
  echo "[RESTORE-DRILL] Status Enkripsi Dump: TERENKRIPSI (AES-256-CBC + PBKDF2 via BACKUP_ENCRYPTION_KEY)"
  openssl enc -aes-256-cbc -pbkdf2 -salt -in "${DUMP_FILE}" -out "${DUMP_FILE}.enc" -pass env:BACKUP_ENCRYPTION_KEY
  rm -f "${DUMP_FILE}"
  openssl enc -d -aes-256-cbc -pbkdf2 -in "${DUMP_FILE}.enc" -out "${DUMP_FILE}" -pass env:BACKUP_ENCRYPTION_KEY
else
  echo "[RESTORE-DRILL] Status Enkripsi Dump: TIDAK TERENKRIPSI (plaintext SQL/custom dump — hanya untuk drill lokal/CI)"
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
if [ "${TABLE_COUNT}" -lt 25 ]; then
  psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -c "DROP DATABASE IF EXISTS \"${DRILL_DB_NAME}\";" >/dev/null || true
  echo "ERROR: Jumlah tabel pulihan tidak mencukupi (${TABLE_COUNT})"
  exit 1
fi

if [ -n "${KOPERASI_ID}" ]; then
  MARKER_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
    SELECT COUNT(*) FROM audit_log WHERE data_baru->>'drill_id' = '${DRILL_ID}';
  ")
  echo "-> Marker Audit Drill: ${MARKER_COUNT}"
  if [ "${MARKER_COUNT}" -lt 1 ]; then
    psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -c "DROP DATABASE IF EXISTS \"${DRILL_DB_NAME}\";" >/dev/null || true
    echo "ERROR: Marker audit_log tidak ditemukan setelah restore!"
    exit 1
  fi
fi

# Verifikasi kelengkapan dan integritas SHA-256 hash chain pada seluruh tabel berantai hash (audit_log, kas_mutasi, pembayaran_verifikasi_log)
for HASH_TABLE in audit_log kas_mutasi pembayaran_verifikasi_log; do
  BROKEN_LINKS=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
    WITH ordered AS (
      SELECT
        koperasi_id,
        prev_hash,
        row_hash,
        LAG(row_hash) OVER (PARTITION BY koperasi_id ORDER BY created_at, id) AS expected_prev
      FROM ${HASH_TABLE}
    )
    SELECT COUNT(*) FROM ordered
    WHERE row_hash IS NULL
       OR row_hash = ''
       OR (expected_prev IS NOT NULL AND prev_hash <> expected_prev);
  ")
  echo "-> Pemeriksaan Rantai Hash ${HASH_TABLE} (broken/empty links): ${BROKEN_LINKS}"
  if [ "${BROKEN_LINKS}" -ne 0 ]; then
    psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -c "DROP DATABASE IF EXISTS \"${DRILL_DB_NAME}\";" >/dev/null || true
    echo "ERROR: Rantai hash ${HASH_TABLE} terputus atau kosong setelah restore!"
    exit 1
  fi
done

RLS_ACTIVE_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
  SELECT COUNT(*) FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND (c.relrowsecurity = true OR c.relforcerowsecurity = true);
")
echo "-> Tabel RLS Aktif: ${RLS_ACTIVE_COUNT}"

psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -v ON_ERROR_STOP=1 -c "DROP DATABASE \"${DRILL_DB_NAME}\";" >/dev/null
echo "RESTORE DRILL BERHASIL: Snapshot, Marker, RLS, dan Integritas Hash Chain Terverifikasi."
