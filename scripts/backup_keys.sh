#!/usr/bin/env bash
set -euo pipefail
umask 077

# Backup brankas kunci kriptografi Kopdes Merah Putih (85% terotomasi).
# 15% inti tetap menjadi kendali pemilik: pembuatan entropi akar & penyimpanan
# private key age brankas (VAULT_AGE_RECIPIENT) di luar VPS/Git.
# Larangan keras: skrip ini TIDAK PERNAH mencetak nilai rahasia ke stdout/log.

ENV_FILE="${ENV_FILE:-.env}"
KEY_VAULT_DIR="${KEY_VAULT_DIR:-}"
RECIPIENT="${VAULT_AGE_RECIPIENT:-}"
USE_BACKUP_RECIPIENT="false"
INCLUDE_DB="false"

usage() {
  cat <<'EOF'
Pemakaian:
  ENV_FILE=/opt/kopdes/.env KEY_VAULT_DIR=/var/kopdes-keyvault VAULT_AGE_RECIPIENT=age1... bash scripts/backup_keys.sh
Opsi:
  --env-file <path>          Berkas .env sumber (default: .env)
  --out-dir <dir>            Direktori tujuan brankas kunci (WAJIB di luar repo Git)
  --recipient <age1...>      Public key age khusus brankas kunci (disarankan berbeda dari BACKUP_AGE_RECIPIENT)
  --use-backup-recipient     Izinkan fallback ke BACKUP_AGE_RECIPIENT bila belum memisahkan kunci brankas
  --include-db               Sertakan DB_PASSWORD & POSTGRES_PASSWORD ke dalam brankas terenkripsi
  -h, --help                 Tampilkan bantuan ini
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env-file) ENV_FILE="${2:-}"; shift 2 ;;
    --out-dir) KEY_VAULT_DIR="${2:-}"; shift 2 ;;
    --recipient) RECIPIENT="${2:-}"; shift 2 ;;
    --use-backup-recipient) USE_BACKUP_RECIPIENT="true"; shift ;;
    --include-db) INCLUDE_DB="true"; shift ;;
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

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ENV_FILE tidak ditemukan: $ENV_FILE" >&2
  exit 1
fi

if [[ -z "$KEY_VAULT_DIR" ]]; then
  echo "KEY_VAULT_DIR / --out-dir wajib diisi (wajib menunjuk direktori aman di luar repositori Git)." >&2
  exit 1
fi

read_env_var() {
  local key="$1"
  local line val
  line=$(grep -E "^${key}=" "$ENV_FILE" | tail -n 1 || true)
  val="${line#*=}"
  val="${val%$'\r'}"
  if [[ "$val" =~ ^\".*\"$ ]] || [[ "$val" =~ ^\'.*\'$ ]]; then
    val="${val:1:${#val}-2}"
  fi
  printf '%s' "$val"
}

sha256_fp() {
  printf '%s' "$1" | sha256sum | awk '{print substr($1,1,12)}'
}

mkdir -p "$KEY_VAULT_DIR"
chmod 700 "$KEY_VAULT_DIR" || true

ABS_OUT=$(cd "$KEY_VAULT_DIR" && pwd -P)
if git rev-parse --show-toplevel >/dev/null 2>&1; then
  ABS_REPO=$(cd "$(git rev-parse --show-toplevel)" && pwd -P)
  case "${ABS_OUT}/" in
    "${ABS_REPO}/"*)
      echo "DITOLAK: KEY_VAULT_DIR (${ABS_OUT}) berada di dalam repositori Git! Simpan brankas kunci di luar repo." >&2
      exit 1
      ;;
  esac
fi

APP_ENV=$(read_env_var "APP_ENV")
AES_MASTER_KEY=$(read_env_var "AES_MASTER_KEY")
AES_PREVIOUS_KEYS=$(read_env_var "AES_PREVIOUS_KEYS")
BLIND_INDEX_SECRET=$(read_env_var "BLIND_INDEX_SECRET")
BACKUP_AGE_RECIPIENT=$(read_env_var "BACKUP_AGE_RECIPIENT")
DB_PASSWORD=$(read_env_var "DB_PASSWORD")
POSTGRES_PASSWORD=$(read_env_var "POSTGRES_PASSWORD")

if [[ -z "$RECIPIENT" && "$USE_BACKUP_RECIPIENT" == "true" ]]; then
  RECIPIENT="$BACKUP_AGE_RECIPIENT"
fi

if [[ -z "$RECIPIENT" ]]; then
  echo "VAULT_AGE_RECIPIENT kosong. Buat kunci brankas pemilik (age-keygen) di luar server, lalu berikan public key (age1...)." >&2
  echo "Atau gunakan --use-backup-recipient jika sementara memakai BACKUP_AGE_RECIPIENT." >&2
  exit 1
fi

if [[ ! "$RECIPIENT" =~ ^age1[0-9a-z]{20,}$ ]]; then
  echo "Format recipient age tidak valid (wajib awalan public key age1...)." >&2
  exit 1
fi

if [[ ${#AES_MASTER_KEY} -lt 32 || ${#BLIND_INDEX_SECRET} -lt 32 ]]; then
  echo "AES_MASTER_KEY atau BLIND_INDEX_SECRET kosong/terlalu pendek di $ENV_FILE." >&2
  exit 1
fi

STAMP=$(date -u +%Y%m%d_%H%M%S)
OUT_ARCHIVE="${ABS_OUT}/kopdes_keyvault_${STAMP}.tar.age"
WORK_DIR=$(mktemp -d)
chmod 700 "$WORK_DIR"
cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

KEYS_OUT="${WORK_DIR}/keys.env"
META_OUT="${WORK_DIR}/metadata.txt"

{
  printf 'APP_ENV=%s\n' "${APP_ENV:-unknown}"
  printf 'AES_MASTER_KEY=%s\n' "$AES_MASTER_KEY"
  printf 'AES_PREVIOUS_KEYS=%s\n' "$AES_PREVIOUS_KEYS"
  printf 'BLIND_INDEX_SECRET=%s\n' "$BLIND_INDEX_SECRET"
  printf 'BACKUP_AGE_RECIPIENT=%s\n' "$BACKUP_AGE_RECIPIENT"
  if [[ "$INCLUDE_DB" == "true" ]]; then
    printf 'DB_PASSWORD=%s\n' "$DB_PASSWORD"
    printf 'POSTGRES_PASSWORD=%s\n' "$POSTGRES_PASSWORD"
  fi
} > "$KEYS_OUT"
chmod 600 "$KEYS_OUT"

{
  printf 'created_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'app_env=%s\n' "${APP_ENV:-unknown}"
  printf 'aes_master_fp=%s\n' "$(sha256_fp "$AES_MASTER_KEY")"
  printf 'blind_index_fp=%s\n' "$(sha256_fp "$BLIND_INDEX_SECRET")"
  printf 'includes_previous_keys=%s\n' "$( [[ -n "$AES_PREVIOUS_KEYS" ]] && echo true || echo false )"
  printf 'includes_db_passwords=%s\n' "$INCLUDE_DB"
} > "$META_OUT"
chmod 600 "$META_OUT"

(cd "$WORK_DIR" && sha256sum keys.env metadata.txt > manifest.sha256)
tar -C "$WORK_DIR" -czf - keys.env metadata.txt manifest.sha256 | age -r "$RECIPIENT" -o "$OUT_ARCHIVE"
chmod 600 "$OUT_ARCHIVE"
sha256sum "$OUT_ARCHIVE" > "${OUT_ARCHIVE}.sha256"
chmod 600 "${OUT_ARCHIVE}.sha256"

echo "BACKUP_KEYS BERHASIL: file=${OUT_ARCHIVE} recipient=${RECIPIENT:0:12}... aes_fp=$(sha256_fp "$AES_MASTER_KEY") blind_fp=$(sha256_fp "$BLIND_INDEX_SECRET") (rahasia disamarkan)"
