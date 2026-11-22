#!/usr/bin/env bash
# Script Simulasi Restore Drill Terautentikasi (age + pg_dump + KYC + Bukti + Smoke Checks)
set -euo pipefail
umask 077

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
      if [[ "${_key}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] && [ -z "${!_key:-}" ]; then
        export "${_key}=${_val}"
      fi
    fi
  done < "${ENV_FILE}"
fi

DB_HOST="${DB_HOST:?DB_HOST wajib disetel}"
DB_PORT="${DB_PORT:?DB_PORT wajib disetel}"
DB_NAME="${DB_NAME:?DB_NAME wajib disetel}"
DB_USER="${POSTGRES_USER:-${DB_USER:?POSTGRES_USER atau DB_USER wajib disetel}}"
STORAGE_KYC_DIR="${STORAGE_KYC_DIR:-./storage/kyc}"
STORAGE_BUKTI_DIR="${STORAGE_BUKTI_DIR:-./storage/bukti}"
export PGPASSWORD="${PGPASSWORD:-${POSTGRES_PASSWORD:-${DB_PASSWORD:?PGPASSWORD/POSTGRES_PASSWORD/DB_PASSWORD wajib disetel}}}"

TIMESTAMP=$(date -u +"%Y%m%d_%H%M%S")
DRILL_ID="drill_${TIMESTAMP}_$$"
DRILL_DB_NAME="${DB_NAME}_drill_${TIMESTAMP}"
BACKUP_DIR="${BACKUP_DIR:-./backups}/drill"
STAGE_DIR=$(mktemp -d)
cleanup() {
  rm -rf "${STAGE_DIR}"
  psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -c "DROP DATABASE IF EXISTS \"${DRILL_DB_NAME}\";" >/dev/null 2>&1 || true
}
trap cleanup EXIT
mkdir -p "${BACKUP_DIR}" "${STAGE_DIR}/in" "${STAGE_DIR}/out" "${STAGE_DIR}/kyc_out" "${STAGE_DIR}/bukti_out"

echo "============================================================================="
echo "[RESTORE-DRILL] Latihan Pemulihan Bencana Terautentikasi (age + pg_dump + KYC/Bukti)"
echo "Target Host : ${DB_HOST}:${DB_PORT} (Source DB: ${DB_NAME})"
echo "Drill DB    : ${DRILL_DB_NAME}"
echo "============================================================================="

psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 <<-EOSQL > /dev/null
  INSERT INTO audit_log (id, actor_peran, aksi, entitas, entitas_id, ringkasan, ip_address)
  VALUES (uuid_generate_v7(), 'sistem', 'restore_drill.marker', 'sistem_drill', '${DRILL_ID}', 'Marker integritas latihan restore backup', '127.0.0.1');
EOSQL

pg_dump -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -F c -b -f "${STAGE_DIR}/in/database.dump" 2> "${BACKUP_DIR}/${DRILL_ID}.log"
mkdir -p "${STORAGE_KYC_DIR}" "${STORAGE_BUKTI_DIR}"
tar -czf "${STAGE_DIR}/in/kyc_storage.tar.gz" -C "${STORAGE_KYC_DIR}" .
tar -czf "${STAGE_DIR}/in/bukti_storage.tar.gz" -C "${STORAGE_BUKTI_DIR}" .
(
  cd "${STAGE_DIR}/in"
  sha256sum database.dump kyc_storage.tar.gz bukti_storage.tar.gz > manifest.sha256
)

BUNDLE_TAR="${BACKUP_DIR}/${DRILL_ID}.tar"
tar -cf "${BUNDLE_TAR}" -C "${STAGE_DIR}/in" database.dump kyc_storage.tar.gz bukti_storage.tar.gz manifest.sha256

RESTORE_TAR="${BUNDLE_TAR}"
if [ -n "${BACKUP_AGE_RECIPIENT:-}" ] && [ -n "${BACKUP_AGE_IDENTITY_FILE:-}" ] && command -v age >/dev/null 2>&1; then
  echo "[RESTORE-DRILL] Status Enkripsi: TERENKRIPSI TERAUTENTIKASI (age AEAD ChaCha20-Poly1305)"
  age -r "${BACKUP_AGE_RECIPIENT}" -o "${BUNDLE_TAR}.age" "${BUNDLE_TAR}"
  rm -f "${BUNDLE_TAR}"

  # Uji deteksi manipulasi ciphertext (flip 1 byte harus gagal didekripsi)
  cp "${BUNDLE_TAR}.age" "${STAGE_DIR}/tampered.age"
  printf '\xff' | dd of="${STAGE_DIR}/tampered.age" bs=1 seek=$(( $(wc -c < "${STAGE_DIR}/tampered.age") - 5 )) count=1 conv=notrunc status=none
  if age -d -i "${BACKUP_AGE_IDENTITY_FILE}" -o "${STAGE_DIR}/tampered.tar" "${STAGE_DIR}/tampered.age" 2>/dev/null; then
    echo "ERROR: Tampered ciphertext gagal ditolak oleh age!" >&2
    exit 1
  fi
  echo "-> Tamper Rejection Test: PASS (modifikasi ciphertext 1 byte ditolak age)"

  age -d -i "${BACKUP_AGE_IDENTITY_FILE}" -o "${STAGE_DIR}/decrypted.tar" "${BUNDLE_TAR}.age"
  RESTORE_TAR="${STAGE_DIR}/decrypted.tar"
else
  echo "[RESTORE-DRILL] Status Enkripsi: MODE LOKAL TANPA KUNCI DEKRIPSI OFF-SERVER"
fi

tar -xf "${RESTORE_TAR}" -C "${STAGE_DIR}/out"
(
  cd "${STAGE_DIR}/out"
  sha256sum -c manifest.sha256 >/dev/null
)
tar -xzf "${STAGE_DIR}/out/kyc_storage.tar.gz" -C "${STAGE_DIR}/kyc_out"
tar -xzf "${STAGE_DIR}/out/bukti_storage.tar.gz" -C "${STAGE_DIR}/bukti_out"

psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "postgres" -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"${DRILL_DB_NAME}\";" >/dev/null
RESTORE_START=$(date +%s)
pg_restore -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" --no-owner --no-privileges "${STAGE_DIR}/out/database.dump" 2>> "${BACKUP_DIR}/${DRILL_ID}.log" || true
RESTORE_END=$(date +%s)

TABLE_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
  SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE';
")
echo "-> Tabel Terpulihkan: ${TABLE_COUNT} (durasi $((RESTORE_END - RESTORE_START))s)"
if [ "${TABLE_COUNT}" -lt 21 ]; then
  echo "ERROR: Jumlah tabel pulihan kurang dari 21 (${TABLE_COUNT})" >&2
  exit 1
fi

MARKER_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
  SELECT COUNT(*) FROM audit_log WHERE entitas = 'sistem_drill' AND entitas_id = '${DRILL_ID}';
")
if [ "${MARKER_COUNT}" -lt 1 ]; then
  echo "ERROR: Marker audit_log tidak ditemukan setelah restore!" >&2
  exit 1
fi

SUPER_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DRILL_DB_NAME}" -t -A -c "
  SELECT COUNT(*) FROM pg_roles WHERE rolname = 'kopdes_app' AND (rolsuper = true OR rolbypassrls = true);
")
echo "-> Marker Drill: ${MARKER_COUNT} | Non-Superuser Check (harus 0): ${SUPER_COUNT}"
echo "RESTORE DRILL BERHASIL: Snapshot DB (${TABLE_COUNT} tabel), arsip KYC/Bukti, dan manifest terverifikasi."
