#!/usr/bin/env bash
set -euo pipefail
umask 077

# Verifikasi & pemulihan brankas kunci kriptografi Kopdes Merah Putih.
# Mendukung --drill (uji dekripsi + manifest SHA-256 + uji tamper 1-byte tanpa menulis .env)
# dan --apply-env <target_env> (memulihkan kunci ke berkas target di luar repo dengan hak akses 600).
# Skrip ini TIDAK PERNAH mencetak nilai plaintext rahasia ke stdout.

VAULT_FILE="${VAULT_FILE:-}"
KEY_VAULT_DIR="${KEY_VAULT_DIR:-}"
IDENTITY_FILE="${VAULT_AGE_IDENTITY_FILE:-${BACKUP_AGE_IDENTITY_FILE:-}}"
DRILL_MODE="false"
APPLY_ENV=""

usage() {
  cat <<'EOF'
Pemakaian:
  VAULT_AGE_IDENTITY_FILE=/media/usb/vault.key bash scripts/restore_keys.sh --vault /var/kopdes-keyvault/kopdes_keyvault_*.tar.age --drill
  VAULT_AGE_IDENTITY_FILE=/media/usb/vault.key bash scripts/restore_keys.sh --vault /var/kopdes-keyvault/kopdes_keyvault_*.tar.age --apply-env /opt/kopdes/.env.restored
Opsi:
  --vault <file.tar.age>     Arsip brankas terenkripsi (bila kosong, mengambil terbaru di KEY_VAULT_DIR)
  --vault-dir <dir>          Direktori brankas untuk mencari file terbaru
  --identity <keyfile>       Berkas private key age pemilik (WAJIB di luar repo Git)
  --drill                    Uji tamper 1-byte + verifikasi dekripsi & integritas tanpa menimpa .env
  --apply-env <target_file>  Tulis kunci pulihan ke berkas .env target (ditolak bila tracked oleh Git)
  -h, --help                 Tampilkan bantuan ini
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --vault) VAULT_FILE="${2:-}"; shift 2 ;;
    --vault-dir) KEY_VAULT_DIR="${2:-}"; shift 2 ;;
    --identity) IDENTITY_FILE="${2:-}"; shift 2 ;;
    --drill) DRILL_MODE="true"; shift ;;
    --apply-env) APPLY_ENV="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Opsi tidak dikenal: $1" >&2; usage >&2; exit 2 ;;
  esac
done

for cmd in age tar sha256sum mktemp; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Perkakas wajib tidak ditemukan: $cmd" >&2
    exit 1
  fi
done

if [[ -z "$VAULT_FILE" && -n "$KEY_VAULT_DIR" ]]; then
  VAULT_FILE=$(ls -1t "${KEY_VAULT_DIR}"/kopdes_keyvault_*.tar.age 2>/dev/null | head -n 1 || true)
fi
if [[ -z "$VAULT_FILE" || ! -f "$VAULT_FILE" ]]; then
  echo "Arsip brankas .tar.age tidak ditemukan." >&2
  exit 1
fi

if [[ -z "$IDENTITY_FILE" || ! -f "$IDENTITY_FILE" ]]; then
  echo "Private key identity age (--identity / VAULT_AGE_IDENTITY_FILE) wajib tersedia." >&2
  exit 1
fi

ABS_ID=$(cd "$(dirname "$IDENTITY_FILE")" && pwd -P)/$(basename "$IDENTITY_FILE")
if git rev-parse --show-toplevel >/dev/null 2>&1; then
  ABS_REPO=$(cd "$(git rev-parse --show-toplevel)" && pwd -P)
  case "$ABS_ID" in
    "${ABS_REPO}/"*)
      echo "DITOLAK: Berkas private key age tidak boleh disimpan di dalam repositori Git!" >&2
      exit 1
      ;;
  esac
fi

if [[ -f "${VAULT_FILE}.sha256" ]]; then
  EXPECTED_SHA=$(awk '{print $1}' "${VAULT_FILE}.sha256")
  ACTUAL_SHA=$(sha256sum "$VAULT_FILE" | awk '{print $1}')
  if [[ "$EXPECTED_SHA" != "$ACTUAL_SHA" ]]; then
    echo "GAGAL INTEGRITAS: SHA-256 arsip brankas tidak cocok (${ACTUAL_SHA} != ${EXPECTED_SHA})." >&2
    exit 1
  fi
fi

WORK_DIR=$(mktemp -d)
chmod 700 "$WORK_DIR"
cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

if [[ "$DRILL_MODE" == "true" ]]; then
  TAMPERED="${WORK_DIR}/tampered.tar.age"
  cp "$VAULT_FILE" "$TAMPERED"
  printf '\xff' | dd of="$TAMPERED" bs=1 seek=32 count=1 conv=notrunc 2>/dev/null || true
  if age -d -i "$IDENTITY_FILE" -o "${WORK_DIR}/tampered.tar.gz" "$TAMPERED" 2>/dev/null; then
    echo "GAGAL UJI TAMPER: arsip yang dimodifikasi 1-byte seharusnya ditolak oleh age!" >&2
    exit 1
  fi
fi

age -d -i "$IDENTITY_FILE" -o "${WORK_DIR}/vault.tar.gz" "$VAULT_FILE"
tar -C "$WORK_DIR" -xzf "${WORK_DIR}/vault.tar.gz"
(cd "$WORK_DIR" && sha256sum -c manifest.sha256 >/dev/null)

read_vault_var() {
  local key="$1"
  local line
  line=$(grep -E "^${key}=" "${WORK_DIR}/keys.env" | tail -n 1 || true)
  printf '%s' "${line#*=}"
}

AES_MASTER_KEY=$(read_vault_var "AES_MASTER_KEY")
BLIND_INDEX_SECRET=$(read_vault_var "BLIND_INDEX_SECRET")
AES_PREVIOUS_KEYS=$(read_vault_var "AES_PREVIOUS_KEYS")
BACKUP_AGE_RECIPIENT=$(read_vault_var "BACKUP_AGE_RECIPIENT")
DB_PASSWORD=$(read_vault_var "DB_PASSWORD")
POSTGRES_PASSWORD=$(read_vault_var "POSTGRES_PASSWORD")

if [[ ${#AES_MASTER_KEY} -lt 32 || ${#BLIND_INDEX_SECRET} -lt 32 ]]; then
  echo "Brankas terdekripsi tidak lengkap: AES_MASTER_KEY atau BLIND_INDEX_SECRET invalid." >&2
  exit 1
fi

fp_prefix() {
  printf '%s' "$1" | sha256sum | awk '{print substr($1,1,12)}'
}

if [[ -n "$APPLY_ENV" ]]; then
  if git ls-files --error-unmatch "$APPLY_ENV" >/dev/null 2>&1; then
    echo "DITOLAK: --apply-env tidak boleh menimpa berkas yang terlacak oleh Git ($APPLY_ENV)." >&2
    exit 1
  fi
  TARGET_DIR=$(dirname "$APPLY_ENV")
  mkdir -p "$TARGET_DIR"
  TMP_OUT="${WORK_DIR}/merged.env"
  if [[ -f "$APPLY_ENV" ]]; then
    grep -vE '^(AES_MASTER_KEY|AES_PREVIOUS_KEYS|BLIND_INDEX_SECRET|BACKUP_AGE_RECIPIENT)=' "$APPLY_ENV" > "$TMP_OUT" || true
  else
    : > "$TMP_OUT"
  fi
  {
    printf 'AES_MASTER_KEY=%s\n' "$AES_MASTER_KEY"
    printf 'AES_PREVIOUS_KEYS=%s\n' "$AES_PREVIOUS_KEYS"
    printf 'BLIND_INDEX_SECRET=%s\n' "$BLIND_INDEX_SECRET"
    if [[ -n "$BACKUP_AGE_RECIPIENT" ]]; then
      printf 'BACKUP_AGE_RECIPIENT=%s\n' "$BACKUP_AGE_RECIPIENT"
    fi
  } >> "$TMP_OUT"
  if [[ -n "$DB_PASSWORD" ]]; then
    grep -vE '^DB_PASSWORD=' "$TMP_OUT" > "${TMP_OUT}.2" || true
    printf 'DB_PASSWORD=%s\n' "$DB_PASSWORD" >> "${TMP_OUT}.2"
    mv "${TMP_OUT}.2" "$TMP_OUT"
  fi
  if [[ -n "$POSTGRES_PASSWORD" ]]; then
    grep -vE '^POSTGRES_PASSWORD=' "$TMP_OUT" > "${TMP_OUT}.2" || true
    printf 'POSTGRES_PASSWORD=%s\n' "$POSTGRES_PASSWORD" >> "${TMP_OUT}.2"
    mv "${TMP_OUT}.2" "$TMP_OUT"
  fi
  chmod 600 "$TMP_OUT"
  mv "$TMP_OUT" "$APPLY_ENV"
  chmod 600 "$APPLY_ENV"
  echo "RESTORE_KEYS APPLY BERHASIL: target=${APPLY_ENV} chmod=600 aes_fp=$(fp_prefix "$AES_MASTER_KEY")"
fi

echo "RESTORE_KEYS DRILL BERHASIL: aes_master=***REDACTED***(len=${#AES_MASTER_KEY},fp=$(fp_prefix "$AES_MASTER_KEY")) blind_index=***REDACTED***(len=${#BLIND_INDEX_SECRET},fp=$(fp_prefix "$BLIND_INDEX_SECRET")) tamper_reject=${DRILL_MODE}"
