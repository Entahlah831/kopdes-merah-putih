#!/usr/bin/env bash
# Script Backup Harian Terautentikasi (age + pg_dump + Arsip KYC/Bukti + SHA-256 Manifest)
# PERINGATAN KRITIS OPERASI:
# 1. Server HANYA menyimpan public key penerima di BACKUP_AGE_RECIPIENT (age1...).
# 2. Private key (AGE-SECRET-KEY-...) WAJIB disimpan di luar server (password manager/HSM offline pemilik).
# 3. Kehilangan private key berarti backup TIDAK DAPAT dibuka selamanya.
# 4. Untuk ketahanan penuh off-server, sinkronkan ${BACKUP_DIR}/daily ke penyimpanan remote (mis. rclone copy / restic).
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

APP_ENV="${APP_ENV:-development}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
STORAGE_KYC_DIR="${STORAGE_KYC_DIR:-./storage/kyc}"
STORAGE_BUKTI_DIR="${STORAGE_BUKTI_DIR:-./storage/bukti}"
DB_HOST="${DB_HOST:?DB_HOST wajib disetel}"
DB_PORT="${DB_PORT:?DB_PORT wajib disetel}"
DB_NAME="${DB_NAME:?DB_NAME wajib disetel}"
DB_USER="${POSTGRES_USER:-${DB_USER:?POSTGRES_USER atau DB_USER wajib disetel}}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"
export PGPASSWORD="${PGPASSWORD:-${POSTGRES_PASSWORD:-${DB_PASSWORD:?PGPASSWORD/POSTGRES_PASSWORD/DB_PASSWORD wajib disetel}}}"

TIMESTAMP=$(date -u +"%Y%m%d_%H%M%SZ")
mkdir -p "${BACKUP_DIR}/daily"
STAGE_DIR=$(mktemp -d)
cleanup() {
  rm -rf "${STAGE_DIR}"
}
trap cleanup EXIT

echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Memulai backup terpadu ${DB_NAME} + KYC + Bukti..."
pg_dump -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -F c -b -f "${STAGE_DIR}/database.dump"

mkdir -p "${STORAGE_KYC_DIR}" "${STORAGE_BUKTI_DIR}"
tar -czf "${STAGE_DIR}/kyc_storage.tar.gz" -C "${STORAGE_KYC_DIR}" .
tar -czf "${STAGE_DIR}/bukti_storage.tar.gz" -C "${STORAGE_BUKTI_DIR}" .

cat > "${STAGE_DIR}/metadata.txt" <<EOF
timestamp=${TIMESTAMP}
db_name=${DB_NAME}
app_env=${APP_ENV}
format=kopdes-age-v1
EOF

(
  cd "${STAGE_DIR}"
  sha256sum database.dump kyc_storage.tar.gz bukti_storage.tar.gz metadata.txt > manifest.sha256
)

BUNDLE_TAR="${BACKUP_DIR}/daily/kopdes_${DB_NAME}_${TIMESTAMP}.tar"
tar -cf "${BUNDLE_TAR}" -C "${STAGE_DIR}" database.dump kyc_storage.tar.gz bukti_storage.tar.gz metadata.txt manifest.sha256

if [ -n "${BACKUP_AGE_RECIPIENT:-}" ]; then
  if ! command -v age >/dev/null 2>&1; then
    echo "ERROR: biner 'age' tidak ditemukan padahal BACKUP_AGE_RECIPIENT disetel." >&2
    exit 1
  fi
  ARTIFACT="${BUNDLE_TAR}.age"
  age -r "${BACKUP_AGE_RECIPIENT}" -o "${ARTIFACT}" "${BUNDLE_TAR}"
  rm -f "${BUNDLE_TAR}"
elif [ "${APP_ENV}" = "production" ]; then
  rm -f "${BUNDLE_TAR}"
  echo "ERROR: Pada APP_ENV=production, BACKUP_AGE_RECIPIENT (public key age) wajib disetel!" >&2
  exit 1
else
  echo "PERINGATAN: BACKUP_AGE_RECIPIENT kosong di mode non-production; menghasilkan arsip .tar tanpa enkripsi age." >&2
  ARTIFACT="${BUNDLE_TAR}"
fi

sha256sum "${ARTIFACT}" > "${ARTIFACT}.sha256"
echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Artifact backup tersimpan: ${ARTIFACT} (+ .sha256)."

find "${BACKUP_DIR}/daily" -maxdepth 1 \( -name "kopdes_*.tar*" -o -name "*.dump*" \) -mtime +"${BACKUP_RETENTION_DAYS}" -delete
echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Backup selesai. Unggah artifact ke lokasi off-server (mis. rclone copy)."
