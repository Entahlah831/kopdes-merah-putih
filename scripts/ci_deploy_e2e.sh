#!/usr/bin/env bash
# Uji Deployment Nyata Linux: docker compose up dari nol (.env.test.alt),
# postgres:16-alpine + api + worker, bootstrap admin, 1 alur pembayaran cookie-primary,
# serta backup.sh + restore_drill.sh menggunakan age asli.
set -euo pipefail

ENV_FILE=".env.test.alt"
OUT_DIR="${OUT_DIR:-reports/ci_deploy}"
mkdir -p "${OUT_DIR}"

set -a
# shellcheck disable=SC1090
source "${ENV_FILE}"
set +a

TMP_ROOT=$(mktemp -d)
KYC_TMP="${TMP_ROOT}/kyc"
BUKTI_TMP="${TMP_ROOT}/bukti"
BACKUP_TMP="${TMP_ROOT}/backups"
AGE_KEYFILE="${TMP_ROOT}/ci-age-private.key"
mkdir -p "${KYC_TMP}" "${BUKTI_TMP}" "${BACKUP_TMP}"

cleanup() {
  docker compose --env-file "${ENV_FILE}" --profile app --profile worker logs --no-color > "${OUT_DIR}/compose_logs.txt" 2>&1 || true
  docker compose --env-file "${ENV_FILE}" --profile app --profile worker down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf "${TMP_ROOT}"
}
trap cleanup EXIT

LOG_FILE="${OUT_DIR}/deploy_e2e_summary.txt"
exec > >(tee -a "${LOG_FILE}") 2>&1

echo "=== [1/6] Validasi & Boot Docker Compose dari Nol (.env.test.alt) ==="
docker compose --env-file "${ENV_FILE}" --profile app --profile worker config --quiet
docker compose --env-file "${ENV_FILE}" --profile app --profile worker down -v --remove-orphans >/dev/null 2>&1 || true
docker compose --env-file "${ENV_FILE}" --profile app --profile worker up -d --build

echo "=== [2/6] Menunggu API, Database PG16, & Worker Sehat ==="
API_BASE="http://127.0.0.1:${API_HOST_PORT}"
READY_JSON=""
for _ in $(seq 1 45); do
  if READY_JSON=$(curl -fsS "${API_BASE}/readyz" 2>/dev/null); then
    if echo "${READY_JSON}" | grep -q '"database":"CONNECTED"'; then
      break
    fi
  fi
  sleep 2
done
echo "API /readyz: ${READY_JSON}"
echo "${READY_JSON}" | grep -q '"database":"CONNECTED"'

WORKER_HEALTH=""
for _ in $(seq 1 15); do
  if WORKER_HEALTH=$(docker compose --env-file "${ENV_FILE}" --profile app --profile worker exec -T worker curl -fsS "http://127.0.0.1:${WORKER_PORT}/healthz" 2>/dev/null); then
    break
  fi
  sleep 1
done
echo "Worker /healthz: ${WORKER_HEALTH}"
echo "${WORKER_HEALTH}" | grep -q '"status":"UP"'

ROLE_CHECK=$(docker compose --env-file "${ENV_FILE}" --profile app --profile worker exec -T postgres \
  psql -U kopdes_migrator -d "${DB_NAME}" -t -A -c "SELECT rolname || '|super=' || rolsuper || '|bypassrls=' || rolbypassrls FROM pg_roles WHERE rolname = 'kopdes_app';")
echo "Role DB Runtime: ${ROLE_CHECK}"
[ "${ROLE_CHECK}" = "kopdes_app|super=f|bypassrls=f" ]

echo "=== [3/6] Bootstrap Admin Pertama & Verifikasi Alur Pembayaran Penuh (Cookie HttpOnly + MFA) ==="
ADMIN_EMAIL="ketua@kopdes-deploy.local"
ADMIN_PASS="DeployKetuaKu4t_2026!"
docker compose --env-file "${ENV_FILE}" --profile app --profile worker exec -T api \
  /app/kopdes admin create --email "${ADMIN_EMAIL}" --nama "Ketua Deploy" --hp "081211112222" --password "${ADMIN_PASS}"

API_BASE="${API_BASE}" ADMIN_EMAIL="${ADMIN_EMAIL}" ADMIN_PASS="${ADMIN_PASS}" UI_ORIGIN="http://127.0.0.1:${UI_PORT}" python3 - <<'PY'
import base64, hashlib, hmac, http.cookiejar, json, os, struct, time, urllib.error, urllib.request, zlib

API_BASE = os.environ["API_BASE"]
ORIGIN = os.environ["UI_ORIGIN"]
ADMIN_EMAIL = os.environ["ADMIN_EMAIL"]
ADMIN_PASS = os.environ["ADMIN_PASS"]

def totp_code(secret_b32: str, offset_sec: int = 0) -> str:
    padded = secret_b32.strip().upper()
    padded += "=" * ((8 - len(padded) % 8) % 8)
    key = base64.b32decode(padded)
    counter = int((time.time() + offset_sec) // 30)
    digest = hmac.new(key, struct.pack(">Q", counter), hashlib.sha1).digest()
    offset = digest[-1] & 0x0F
    code = (struct.unpack(">I", digest[offset:offset + 4])[0] & 0x7FFFFFFF) % 1000000
    return f"{code:06d}"

def make_png(seed_byte: int) -> bytes:
    def chunk(tag: bytes, data: bytes) -> bytes:
        return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
    ihdr = struct.pack(">IIBBBBB", 4, 4, 8, 6, 0, 0, 0)
    row = bytes([0] + [seed_byte, (seed_byte + 40) % 255, (seed_byte + 90) % 255, 255] * 4)
    idat = zlib.compress(row * 4)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IDAT", idat) + chunk(b"IEND", b"")

class Client:
    def __init__(self):
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))

    def req(self, method: str, path: str, body=None, headers=None):
        hdrs = {"Origin": ORIGIN, "X-Requested-With": "ci-deploy-e2e"}
        data = None
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            hdrs["Content-Type"] = "application/json"
        if headers:
            hdrs.update(headers)
        r = urllib.request.Request(f"{API_BASE}{path}", data=data, headers=hdrs, method=method)
        try:
            with self.opener.open(r, timeout=15) as resp:
                return resp.status, json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            raise RuntimeError(f"{method} {path} -> HTTP {e.code}: {e.read().decode('utf-8', 'replace')}")

    def upload(self, path: str, fields: dict, file_field: str, filename: str, raw: bytes, headers=None):
        boundary = "----KopdesDeployBoundary7890"
        parts = []
        for k, v in fields.items():
            parts.append(f"--{boundary}\r\nContent-Disposition: form-data; name=\"{k}\"\r\n\r\n{v}\r\n".encode())
        parts.append(
            f"--{boundary}\r\nContent-Disposition: form-data; name=\"{file_field}\"; filename=\"{filename}\"\r\nContent-Type: image/png\r\n\r\n".encode()
            + raw + b"\r\n"
        )
        parts.append(f"--{boundary}--\r\n".encode())
        payload = b"".join(parts)
        hdrs = {
            "Origin": ORIGIN,
            "X-Requested-With": "ci-deploy-e2e",
            "Content-Type": f"multipart/form-data; boundary={boundary}",
        }
        if headers:
            hdrs.update(headers)
        r = urllib.request.Request(f"{API_BASE}{path}", data=payload, headers=hdrs, method="POST")
        try:
            with self.opener.open(r, timeout=15) as resp:
                return resp.status, json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            raise RuntimeError(f"UPLOAD {path} -> HTTP {e.code}: {e.read().decode('utf-8', 'replace')}")

def login_and_setup_mfa(email: str, password: str) -> Client:
    c = Client()
    st, j = c.req("POST", "/api/v1/auth/login", {"identifier": email, "password": password})
    assert st == 200 and j["data"].get("session_token", "") == "", f"Cookie-primary expected no bearer token, got {j}"
    assert j["data"]["scope"] == "mfa_setup", f"Expected mfa_setup for {email}, got {j}"
    _, ms1 = c.req("POST", "/api/v1/auth/mfa/setup", {})
    sec = ms1["data"]["secret"]
    st2, ms2 = c.req("POST", "/api/v1/auth/mfa/setup", {
        "setup_token": ms1["data"]["setup_token"],
        "code": totp_code(sec)
    })
    assert st2 == 200 and ms2["data"]["scope"] == "full"
    return c

admin = login_and_setup_mfa(ADMIN_EMAIL, ADMIN_PASS)
_, rek = admin.req("POST", "/api/v1/pembayaran", {
    "action": "create_rekening",
    "nama_merchant": "Kopdes CI Deploy",
    "nmid": "ID102003004099",
    "jenis_pemilik": "badan_hukum",
    "nama_bank": "BRI",
    "nomor_rekening": "000101009999",
    "nama_pemilik": "Koperasi Merah Putih",
})
_, _ = admin.req("PUT", "/api/v1/pengaturan", {
    "nama_koperasi": "Koperasi Merah Putih Deploy",
    "alamat_koperasi": "Balai Desa Staging",
    "telepon_koperasi": "081211112222",
    "qris_unique_digits": 4,
    "ongkir_tetap": 2000,
    "active_rekening_qris_id": rek["data"]["id"],
})
_, prod = admin.req("POST", "/api/v1/katalog/produk", {
    "sku": "CI-BERAS-01",
    "nama": "Beras Medium 5kg",
    "satuan": "karung",
    "harga_umum": 65000,
    "harga_anggota": 60000,
    "stok": 10,
    "status": "aktif",
})
prod_id = prod["data"]["id"]

BEND_EMAIL = "bendahara@kopdes-deploy.local"
BEND_PASS = "BendaharaDeploy_2026!"
admin.req("POST", "/api/v1/users", {
    "email": BEND_EMAIL,
    "no_hp": "081233334444",
    "nama_lengkap": "Bendahara Deploy",
    "peran": "bendahara",
    "password": BEND_PASS,
})

anon = Client()
_, reg = anon.req("POST", "/api/v1/public/pendaftaran", {
    "nama_lengkap": "Warga Deploy CI",
    "email": "warga@kopdes-deploy.local",
    "nik": "3201010101990001",
    "no_hp": "081255556666",
    "alamat": "RT 01 RW 02 Desa Uji",
    "pekerjaan": "Petani",
})
reg_id = reg["data"]["id"]
up_tok = reg["data"]["upload_token"]
for idx, tipe in enumerate(["ktp", "selfie_ktp", "kk"]):
    anon.upload(f"/api/v1/public/pendaftaran/{reg_id}/dokumen", {"tipe_dokumen": tipe}, "file", f"{tipe}.png", make_png(30 + idx * 50), headers={"X-Upload-Token": up_tok})

_, dec = admin.req("POST", f"/api/v1/pendaftaran/{reg_id}/verifikasi", {"keputusan": "disetujui", "catatan": "KTP & KK lengkap"})
act_code = dec["data"]["activation_code"]

member = Client()
MEMBER_PASS = "WargaDeployKu4t_2026!"
member.req("POST", "/api/v1/auth/password", {"code": act_code, "new_password": MEMBER_PASS})
st_ml, j_ml = member.req("POST", "/api/v1/auth/login", {"identifier": "warga@kopdes-deploy.local", "password": MEMBER_PASS})
assert st_ml == 200 and j_ml["data"]["scope"] == "full"

_, co = member.req("POST", "/api/v1/toko/checkout", {
    "items": [{"produk_id": prod_id, "kuantitas": 2}],
    "metode_pengiriman": "ambil_sendiri",
    "metode_pembayaran": "qris",
    "nama_penerima": "Warga Deploy CI",
    "hp_penerima": "081255556666",
}, headers={"Idempotency-Key": "deploy-co-1"})
ord_id = co["data"]["id"]

_, pay_res = member.req("POST", "/api/v1/pembayaran", {
    "jenis_transaksi": "pesanan",
    "metode": "qris",
    "pesanan_id": ord_id,
}, headers={"Idempotency-Key": "deploy-pay-1"})
pay_obj = pay_res["data"]["pembayaran"]
pay_id = pay_obj["id"]

member.upload("/api/v1/pembayaran", {"pembayaran_id": pay_id}, "file", "bukti.png", make_png(200))

bend = login_and_setup_mfa(BEND_EMAIL, BEND_PASS)
_, ver = bend.req("POST", f"/api/v1/pembayaran/{pay_id}/verifikasi", {
    "keputusan": "setuju",
    "nominal_dibayar": pay_obj["nominal_total"],
    "alasan": "Transfer QRIS cocok di deploy CI",
})
assert ver["data"]["pembayaran"]["status"] == "lunas", ver

for next_st in ("diproses", "siap_diambil", "selesai"):
    _, upd = admin.req("PATCH", f"/api/v1/pesanan/{ord_id}/status", {"status": next_st})
    assert upd["data"]["status"] == next_st

_, cat = member.req("GET", "/api/v1/katalog/produk")
matched = [p for p in cat["data"] if p["id"] == prod_id][0]
assert matched["stok"] == 8 and matched["stok_tertahan"] == 0, matched
print(f"DEPLOY E2E PASS: order={co['data']['nomor_pesanan']} payment={pay_obj['nomor_pembayaran']} total={pay_obj['nominal_total']} status=lunas->selesai stok=10->8")
PY

echo "=== [4/6] Menyalin Blob Storage Terenkripsi dari Volume Compose & Mengecek Ciphertext ==="
docker compose --env-file "${ENV_FILE}" --profile app --profile worker cp api:/app/storage/kyc/. "${KYC_TMP}/"
docker compose --env-file "${ENV_FILE}" --profile app --profile worker cp api:/app/storage/bukti/. "${BUKTI_TMP}/"
KYC_FILES=$(find "${KYC_TMP}" -type f | wc -l)
BUKTI_FILES=$(find "${BUKTI_TMP}" -type f | wc -l)
echo "Berkas .enc ditemukan: KYC=${KYC_FILES} Bukti=${BUKTI_FILES}"
[ "${KYC_FILES}" -ge 3 ]
[ "${BUKTI_FILES}" -ge 1 ]

echo "=== [5/6] Backup Terenkripsi age Asli & Restore Drill ==="
age-keygen -o "${AGE_KEYFILE}" 2>/dev/null
AGE_PUB=$(grep '^# public key: ' "${AGE_KEYFILE}" | awk '{print $4}')
ENV_FILE="${ENV_FILE}" DB_HOST=127.0.0.1 POSTGRES_USER=kopdes_migrator \
  BACKUP_DIR="${BACKUP_TMP}" STORAGE_KYC_DIR="${KYC_TMP}" STORAGE_BUKTI_DIR="${BUKTI_TMP}" \
  BACKUP_AGE_RECIPIENT="${AGE_PUB}" bash scripts/backup.sh
ls -lh "${BACKUP_TMP}/daily/"

ENV_FILE="${ENV_FILE}" DB_HOST=127.0.0.1 POSTGRES_USER=kopdes_migrator \
  BACKUP_DIR="${BACKUP_TMP}" STORAGE_KYC_DIR="${KYC_TMP}" STORAGE_BUKTI_DIR="${BUKTI_TMP}" \
  BACKUP_AGE_RECIPIENT="${AGE_PUB}" BACKUP_AGE_IDENTITY_FILE="${AGE_KEYFILE}" \
  bash scripts/restore_drill.sh

echo "=== [6/6] Status Akhir Kontainer ==="
docker compose --env-file "${ENV_FILE}" --profile app --profile worker ps
echo "CI DEPLOY E2E + AGE BACKUP/RESTORE PASS"
