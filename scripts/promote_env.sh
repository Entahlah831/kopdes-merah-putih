#!/usr/bin/env bash
# Menyinkronkan berkas .env target dari .env.example tanpa menimpa rahasia yang sudah terisi.
set -euo pipefail

TEMPLATE="${1:-.env.example}"
TARGET="${2:-.env}"
GENERATE_SECRETS="${GENERATE_SECRETS:-false}"

if [ ! -f "${TEMPLATE}" ]; then
  echo "ERROR: Template ${TEMPLATE} tidak ditemukan." >&2
  exit 1
fi

declare -A EXISTING=()
if [ -f "${TARGET}" ]; then
  while IFS= read -r line || [ -n "${line}" ]; do
    trimmed=$(printf '%s' "${line}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    [[ -z "${trimmed}" || "${trimmed}" == \#* ]] && continue
    if [[ "${trimmed}" == *=* ]]; then
      k="${trimmed%%=*}"
      v="${trimmed#*=}"
      EXISTING["${k}"]="${v}"
    fi
  done < "${TARGET}"
fi

TMP_OUT=$(mktemp)
trap 'rm -f "${TMP_OUT}"' EXIT

while IFS= read -r line || [ -n "${line}" ]; do
  trimmed=$(printf '%s' "${line}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
  if [[ -z "${trimmed}" || "${trimmed}" == \#* || "${line}" != *=* ]]; then
    printf '%s\n' "${line}" >> "${TMP_OUT}"
    continue
  fi
  k="${line%%=*}"
  tpl_val="${line#*=}"
  if [[ -n "${EXISTING[${k}]:-}" ]]; then
    printf '%s=%s\n' "${k}" "${EXISTING[${k}]}" >> "${TMP_OUT}"
  elif [[ "${GENERATE_SECRETS}" == "true" && -z "${tpl_val}" ]] && [[ "${k}" =~ ^(JWT_SECRET|AES_MASTER_KEY|BLIND_INDEX_SECRET|QRIS_SECRET|METRICS_TOKEN|DB_PASSWORD|POSTGRES_PASSWORD|KOPDES_APP_PASSWORD|BACKUP_ENCRYPTION_KEY)$ ]]; then
    gen=$(openssl rand -hex 32)
    printf '%s=%s\n' "${k}" "${gen}" >> "${TMP_OUT}"
  else
    printf '%s=%s\n' "${k}" "${tpl_val}" >> "${TMP_OUT}"
  fi
done < "${TEMPLATE}"

mv "${TMP_OUT}" "${TARGET}"
chmod 600 "${TARGET}"
echo "Berkas ${TARGET} diperbarui dari ${TEMPLATE} (rahasia aman dan tidak dicetak)."
