#!/usr/bin/env bash
# Verifikasi cepat restorasi dari artifact backup terbaru (.tar.age atau .tar)
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

BACKUP_DIR="${BACKUP_DIR:-./backups}"
DB_HOST="${DB_HOST:?DB_HOST wajib disetel}"
DB_PORT="${DB_PORT:?DB_PORT wajib disetel}"
DB_NAME="${DB_NAME:?DB_NAME wajib disetel}"
DB_USER="${POSTGRES_USER:-${DB_USER:?POSTGRES_USER atau DB_USER wajib disetel}}"
export PGPASSWORD="${PGPASSWORD:-${POSTGRES_PASSWORD:-${DB_PASSWORD:?PGPASSWORD/POSTGRES_PASSWORD/DB_PASSWORD wajib disetel}}}"

RESTORE_DB_NAME="${DB_NAME}_restore_test_$$"
STAGE_DIR=$(mktemp -d)
cleanup() {
  rm -rf "${STAGE_DIR}"
  dropdb --if-exists -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" "${RESTORE_DB_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

LATEST_BACKUP=$(ls -t "${BACKUP_DIR}/daily"/kopdes_*.tar* 2>/dev/null | grep -v '\.sha256$' | head -n 1 || true)
if [ -z "${LATEST_BACKUP}" ]; then
  echo "ERROR: Tidak ditemukan file backup kopdes_*.tar* di ${BACKUP_DIR}/daily" >&2
  exit 1
fi

sha256sum -c "${LATEST_BACKUP}.sha256"

TAR_FILE="${LATEST_BACKUP}"
if [[ "${LATEST_BACKUP}" == *.age ]]; then
  if [ -z "${BACKUP_AGE_IDENTITY_FILE:-}" ] || [ ! -f "${BACKUP_AGE_IDENTITY_FILE}" ]; then
    echo "ERROR: BACKUP_AGE_IDENTITY_FILE wajib disetel ke berkas private key off-server untuk mendekripsi ${LATEST_BACKUP}" >&2
    exit 1
  fi
  TAR_FILE="${STAGE_DIR}/bundle.tar"
  age -d -i "${BACKUP_AGE_IDENTITY_FILE}" -o "${TAR_FILE}" "${LATEST_BACKUP}"
fi

mkdir -p "${STAGE_DIR}/out" "${STAGE_DIR}/kyc" "${STAGE_DIR}/bukti"
tar -xf "${TAR_FILE}" -C "${STAGE_DIR}/out"
(
  cd "${STAGE_DIR}/out"
  sha256sum -c manifest.sha256
)
tar -xzf "${STAGE_DIR}/out/kyc_storage.tar.gz" -C "${STAGE_DIR}/kyc"
tar -xzf "${STAGE_DIR}/out/bukti_storage.tar.gz" -C "${STAGE_DIR}/bukti"

createdb -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" "${RESTORE_DB_NAME}"
pg_restore -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${RESTORE_DB_NAME}" --no-owner --no-privileges "${STAGE_DIR}/out/database.dump" || true

TABLE_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${RESTORE_DB_NAME}" -t -A -c "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE';")
echo "Jumlah tabel ter-restore: ${TABLE_COUNT}"
if [ "${TABLE_COUNT}" -lt 21 ]; then
  echo "ERROR: Jumlah tabel ter-restore tidak lengkap (${TABLE_COUNT} < 21)" >&2
  exit 1
fi

echo "Verifikasi restore backup BERHASIL (${TABLE_COUNT} tabel + arsip KYC & Bukti)."
