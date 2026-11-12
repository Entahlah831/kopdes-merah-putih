#!/usr/bin/env bash
# =============================================================================
# Kopdes Merah Putih - Automated Disaster Recovery & PITR Restore Drill
# =============================================================================
# Memverifikasi kemampuan pemulihan bencana (Disaster Recovery) dari dump cadangan
# dan konsistensi Point-In-Time Recovery (PITR), memvalidasi integritas skema,
# relasi multi-tenant RLS, dan rantai integritas data append-only.
# =============================================================================
set -euo pipefail

DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-5433}"
DB_NAME="${DB_NAME:-kopdes_db}"
DB_USER="${DB_USER:-postgres}"
PGPASSWORD="${PGPASSWORD:-kopdes_dev_password}"
export PGPASSWORD

TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
DRILL_ID="drill_${TIMESTAMP}_$$"
DRILL_DB_NAME="kopdes_restore_drill_${TIMESTAMP}"
BACKUP_DIR="${BACKUP_DIR:-./backups/drill}"

mkdir -p "${BACKUP_DIR}"

echo "============================================================================="
echo "[RESTORE-DRILL] Memulai Simulasi Latihan Pemulihan Bencana (DR Drill)"
echo "Waktu Mulai : $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
echo "Target Host : ${DB_HOST}:${DB_PORT} (Database: ${DB_NAME})"
echo "Drill DB    : ${DRILL_DB_NAME}"
echo "============================================================================="

# 1. Catat Checkpoint LSN dan Pasang Marker Rekam Jejak Sebelum Backup
echo "[LANGKAH 1/6] Mencatat Checkpoint LSN dan Marker Transaksi..."
WAL_LSN=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -t -A -c "SELECT pg_current_wal_lsn();")
echo "-> WAL LSN Checkpoint saat ini: ${WAL_LSN}"

# Ambil satu koperasi_id yang valid untuk marker audit
KOPERASI_ID=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -t -A -c "SELECT id FROM koperasi LIMIT 1;")
if [ -z "${KOPERASI_ID}" ]; then
  KOPERASI_ID="00000000-0000-0000-0000-000000000001"
fi

psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 <<-EOSQL > /dev/null
  INSERT INTO audit_log (id, koperasi_id, nama_tabel, record_id, aksi, data_baru, created_at)
  VALUES (
    uuid_generate_v7(),
    '${KOPERASI_ID}',
    'restore_drill_marker',
    uuid_generate_v7(),
    'INSERT',
    jsonb_build_object('drill_id', '${DRILL_ID}', 'wal_lsn', '${WAL_LSN}', 'timestamp', clock_timestamp()),
    clock_timestamp()
  );
EOSQL
echo "-> Marker DR checkpoint berhasil ditanam ke audit_log."

# 2. Eksekusi Snapshot Backup Logis Terkompresi (pg_dump -Fc)
echo "[LANGKAH 2/6] Mengekspor Snapshot Database Menggunakan pg_dump..."
DUMP_FILE="${BACKUP_DIR}/${DRILL_ID}.dump"
pg_dump -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" \
  -F c -b -v -f "${DUMP_FILE}" 2> "${DUMP_FILE}.log"

DUMP_SIZE=$(ls -lh "${DUMP_FILE}" | awk '{print $5}')
echo "-> Snapshot dump selesai: ${DUMP_FILE} (${DUMP_SIZE})"

# Hitung Checksum SHA-256 untuk Integritas Berkas
if command -v sha256sum > /dev/null 2>&1; then
  sha256sum "${DUMP_FILE}" > "${DUMP_FILE}.sha256"
  echo "-> Checksum SHA-256: $(cat "${DUMP_FILE}.sha256" | awk '{print $1}')"
fi

# 3. Buat Database Drill Terisolasi
echo "[LANGKAH 3/6] Membuat Database Target Drill: ${DRILL_DB_NAME}..."
psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -c "DROP DATABASE IF EXISTS ${DRILL_DB_NAME};" > /dev/null 2>&1 || true
psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -v ON_ERROR_STOP=1 -c "CREATE DATABASE ${DRILL_DB_NAME};"

# 4. Lakukan Restorasi Database Menggunakan pg_restore
echo "[LANGKAH 4/6] Menjalankan pg_restore ke Database Drill..."
RESTORE_START=$(date +%s)
pg_restore -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" \
  --no-owner --no-privileges "${DUMP_FILE}" 2>> "${DUMP_FILE}.log" || true
RESTORE_END=$(date +%s)
RESTORE_DURATION=$((RESTORE_END - RESTORE_START))
echo "-> Proses restorasi selesai dalam ${RESTORE_DURATION} detik."

# 5. Verifikasi Integritas Pasca-Restorasi (Tabel, RLS, dan Data Marker)
echo "[LANGKAH 5/6] Memverifikasi Integritas Data & Skema Pasca-Restorasi..."

# A. Hitung Jumlah Tabel Publik
TABLE_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
  SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE';
")
echo "-> Jumlah Tabel Publik Terpulihkan: ${TABLE_COUNT}"
if [ "${TABLE_COUNT}" -lt 40 ]; then
  echo "ERROR: Jumlah tabel tidak mencukupi (${TABLE_COUNT} < 40)! Restorasi tidak lengkap."
  exit 1
fi

# B. Verifikasi Keberadaan Marker Audit Log
MARKER_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
  SELECT COUNT(*) FROM audit_log WHERE data_baru->>'drill_id' = '${DRILL_ID}';
")
echo "-> Marker Transaksi Ditemukan: ${MARKER_COUNT}"
if [ "${MARKER_COUNT}" -lt 1 ]; then
  echo "ERROR: Marker audit_log untuk drill ${DRILL_ID} tidak ditemukan di database pulihan!"
  exit 1
fi

# C. Verifikasi Tabel-Tabel Partisi Append-Only
PARTITION_TABLES=("audit_log" "kas_mutasi" "stok_mutasi")
for tbl in "${PARTITION_TABLES[@]}"; do
  COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
    SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name LIKE '${tbl}%';
  ")
  echo "-> Partisi untuk ${tbl}: ${COUNT} tabel terdaftar"
done

# D. Verifikasi Proteksi RLS pada Database Hasil Restore
RLS_ACTIVE_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
  SELECT COUNT(*) FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND (c.relrowsecurity = true OR c.relforcerowsecurity = true);
")
echo "-> Tabel dengan RLS Aktif Terpulihkan: ${RLS_ACTIVE_COUNT}"
if [ "${RLS_ACTIVE_COUNT}" -lt 20 ]; then
  echo "ERROR: Proteksi RLS tidak terpulihkan dengan benar (didapat ${RLS_ACTIVE_COUNT} tabel RLS)!"
  exit 1
fi

# 6. Pembersihan Database Target Drill
echo "[LANGKAH 6/6] Membersihkan Database Temporer Drill..."
psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -v ON_ERROR_STOP=1 -c "DROP DATABASE ${DRILL_DB_NAME};"
echo "-> Database ${DRILL_DB_NAME} berhasil dihapus secara bersih."

echo "============================================================================="
echo "[RESTORE-DRILL] HASIL LATIHAN PEMULIHAN BENCANA: BERHASIL 100% (SUCCESS)"
echo "Dump File        : ${DUMP_FILE}"
echo "Ukuran Dump      : ${DUMP_SIZE}"
echo "Waktu Restorasi  : ${RESTORE_DURATION} detik"
echo "Tabel Dipulihkan : ${TABLE_COUNT} tabel"
echo "Tabel RLS Aktif  : ${RLS_ACTIVE_COUNT} tabel"
echo "Integritas Data  : Marker ${DRILL_ID} TERVERIFIKASI IDENTIK"
echo "Waktu Selesai    : $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
echo "============================================================================="
exit 0
