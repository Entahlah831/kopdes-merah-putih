#!/usr/bin/env bash
set -euo pipefail
umask 077

# Skrip bantu rotasi AES_MASTER_KEY Kopdes Merah Putih (85% terotomasi, 15% entropi kunci oleh pemilik).
# Skrip ini memindahkan AES_MASTER_KEY lama ke AES_PREVIOUS_KEYS tanpa downtime dekripsi.
# Bagian 15% inti oleh pemilik: membangkitkan sendiri kunci baru 256-bit (mis. openssl rand -hex 32)
# beserta ID versinya (format vX:kY=<64-hex>) dan menyuntikkan via input tersembunyi atau berkas sementara.

ENV_FILE="${ENV_FILE:-.env}"
NEW_KEY_FILE=""
DRY_RUN="false"

usage() {
  cat <<'EOF'
Pemakaian:
  bash scripts/rotate_keys.sh --env-file /opt/kopdes/.env --new-key-file /tmp/owner_new_aes.txt
  bash scripts/rotate_keys.sh --env-file /opt/kopdes/.env             # Mode interaktif: pemilik menempel kunci sendiri (tanpa echo)
Opsi:
  --env-file <path>      Berkas .env produksi/staging target (ditolak mutasi bila file terlacak Git)
  --new-key-file <file>  Berkas 1 baris berisi format v2:k1=<64-hex> buatan pemilik (disarankan chmod 600 & hapus usai pakai)
  --dry-run              Pratinjau rencana rotasi (nilai rahasia disamarkan) tanpa menulis berkas
  -h, --help             Tampilkan bantuan ini
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env-file) ENV_FILE="${2:-}"; shift 2 ;;
    --new-key-file) NEW_KEY_FILE="${2:-}"; shift 2 ;;
    --dry-run) DRY_RUN="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Opsi tidak dikenal: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ENV_FILE tidak ditemukan: $ENV_FILE" >&2
  exit 1
fi

if [[ "$DRY_RUN" != "true" ]] && git ls-files --error-unmatch "$ENV_FILE" >/dev/null 2>&1; then
  echo "DITOLAK: Tidak boleh merotasi berkas yang terlacak oleh Git ($ENV_FILE). Gunakan --dry-run." >&2
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

OLD_MASTER=$(read_env_var "AES_MASTER_KEY")
OLD_PREV=$(read_env_var "AES_PREVIOUS_KEYS")
if [[ -z "$OLD_MASTER" ]]; then
  echo "AES_MASTER_KEY saat ini kosong di $ENV_FILE" >&2
  exit 1
fi

NEW_MASTER=""
if [[ -n "$NEW_KEY_FILE" ]]; then
  if [[ ! -f "$NEW_KEY_FILE" ]]; then
    echo "Berkas --new-key-file tidak ditemukan: $NEW_KEY_FILE" >&2
    exit 1
  fi
  NEW_MASTER=$(head -n 1 "$NEW_KEY_FILE" | tr -d '\r\n ')
else
  printf 'Masukkan AES_MASTER_KEY baru buatan pemilik format ver:id=<64-hex> (input disamarkan): ' >&2
  read -rs NEW_MASTER
  printf '\n' >&2
  NEW_MASTER=$(printf '%s' "$NEW_MASTER" | tr -d '\r\n ')
fi

if [[ ! "$NEW_MASTER" =~ ^v[0-9]+:[a-zA-Z0-9_-]+=[0-9a-fA-F]{64,}$ ]]; then
  echo "Format AES_MASTER_KEY baru tidak valid. Wajib berpola 'v2:k1=<minimal_64_hex>' (contoh bangkitan pemilik: v2:k1=\$(openssl rand -hex 32))." >&2
  exit 1
fi

RAW_HEX="${NEW_MASTER#*=}"
if [[ "$RAW_HEX" =~ ^(0+|f+)$ ]] || [[ "$RAW_HEX" =~ ^(0{16,}|f{16,}|0123456789abcdef) ]]; then
  echo "DITOLAK: Material hex terdeteksi contoh/pola lemah. Gunakan entropi CSPRNG nyata (openssl rand -hex 32)." >&2
  exit 1
fi

NEW_ID="${NEW_MASTER%%=*}"
OLD_ID="${OLD_MASTER%%=*}"
if [[ "$NEW_ID" == "$OLD_ID" ]]; then
  echo "DITOLAK: ID versi kunci baru ($NEW_ID) tidak boleh sama dengan AES_MASTER_KEY aktif ($OLD_ID)." >&2
  exit 1
fi

if [[ "$OLD_MASTER" != *:*=* ]]; then
  OLD_ENTRY="v1:legacy=${OLD_MASTER}"
else
  OLD_ENTRY="$OLD_MASTER"
fi

if [[ -z "$OLD_PREV" ]]; then
  NEXT_PREV="$OLD_ENTRY"
else
  NEXT_PREV="${OLD_ENTRY};${OLD_PREV}"
fi

if [[ "$DRY_RUN" == "true" ]]; then
  echo "ROTATE_KEYS DRY-RUN:"
  echo "  env_file          : $ENV_FILE"
  echo "  old_master_id     : ${OLD_ID} (fp=$(sha256_fp "$OLD_MASTER")) -> dipindahkan ke AES_PREVIOUS_KEYS"
  echo "  new_master_id     : ${NEW_ID} (fp=$(sha256_fp "$NEW_MASTER"))"
  echo "  next_prev_entries : $(printf '%s' "$NEXT_PREV" | awk -F';' '{print NF}')"
  exit 0
fi

WORK_DIR=$(mktemp -d)
chmod 700 "$WORK_DIR"
cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

TMP_ENV="${WORK_DIR}/rotated.env"
grep -vE '^(AES_MASTER_KEY|AES_PREVIOUS_KEYS)=' "$ENV_FILE" > "$TMP_ENV" || true
{
  printf 'AES_MASTER_KEY=%s\n' "$NEW_MASTER"
  printf 'AES_PREVIOUS_KEYS=%s\n' "$NEXT_PREV"
} >> "$TMP_ENV"
chmod 600 "$TMP_ENV"
mv "$TMP_ENV" "$ENV_FILE"
chmod 600 "$ENV_FILE"

echo "ROTATE_KEYS BERHASIL: new_id=${NEW_ID}(fp=$(sha256_fp "$NEW_MASTER")) archived_old=${OLD_ID}(fp=$(sha256_fp "$OLD_MASTER"))"
echo "Langkah lanjutan wajib:"
echo "  1. Segera jalankan scripts/backup_keys.sh untuk mencadangkan rantai kunci baru ke brankas age off-server."
echo "  2. Restart layanan api & worker (docker compose up -d api worker)."
echo "  3. Jalankan 'kopdes storage migrate-files' untuk mengenkripsi ulang seluruh amplop DEK KYC & bukti dengan ${NEW_ID}."
echo "  4. Hapus kunci lama dari AES_PREVIOUS_KEYS hanya setelah seluruh data terkonfirmasi memakai ${NEW_ID}."
