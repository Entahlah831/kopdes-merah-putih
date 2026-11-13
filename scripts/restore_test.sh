#!/usr/bin/env bash
# Verifikasi cepat restorasi dari file backup terbaru (terparameterisasi env)
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
export PGPASSWORD="${PGPASSWORD:-${POSTGRES_PASSWORD:-${DB_PASSWORD:?PGPASSWORD/POSTGRES_PASSWORD/DB_PASSWORD wajib disetel}}}"

RESTORE_DB_NAME="${DB_NAME}_restore_test_$$"
LATEST_BACKUP=$(ls -t "${BACKUP_DIR}/daily"/*.dump* 2>/dev/null | grep -v '\.sha256$' | head -n 1 || true)
if [ -z "${LATEST_BACKUP}" ]; then
  echo "ERROR: Tidak ditemukan file backup di ${BACKUP_DIR}/daily"
  exit 1
fi

sha256sum -c "${LATEST_BACKUP}.sha256"

WORK_DUMP="${LATEST_BACKUP}"
if [[ "${LATEST_BACKUP}" == *.enc ]]; then
  WORK_DUMP="${LATEST_BACKUP%.enc}.dec"
  openssl enc -d -aes-256-cbc -pbkdf2 -in "${LATEST_BACKUP}" -out "${WORK_DUMP}" -pass env:BACKUP_ENCRYPTION_KEY
fi

createdb -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" "${RESTORE_DB_NAME}"
pg_restore -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${RESTORE_DB_NAME}" --no-owner --no-privileges "${WORK_DUMP}" || true

TABLE_COUNT=$(psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${RESTORE_DB_NAME}" -t -A -c "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public';")
echo "Jumlah tabel ter-restore: ${TABLE_COUNT}"

dropdb -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" "${RESTORE_DB_NAME}"
if [[ "${LATEST_BACKUP}" == *.enc ]]; then
  rm -f "${WORK_DUMP}"
fi

if [ "${TABLE_COUNT}" -lt 25 ]; then
  echo "ERROR: Jumlah tabel ter-restore tidak lengkap (${TABLE_COUNT} < 25)"
  exit 1
fi

echo "Verifikasi restore backup BERHASIL!"
