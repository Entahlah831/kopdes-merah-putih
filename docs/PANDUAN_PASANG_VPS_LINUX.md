# Panduan Memasang Kopdes Merah Putih di VPS Linux (Produksi & Staging)

Panduan ini menuntun pemasangan lengkap sistem **Kopdes Merah Putih** (`backend/` Go Modular Monolith + PostgreSQL 16 + Worker + `frontend/wireframe/dist` + reverse proxy HTTPS `Caddy`) di **1 VPS Linux** (Ubuntu `22.04`/`24.04` LTS atau Debian `12`, minimal **2 vCPU, 2 GB RAM, 20 GB SSD**).

> **Prasyarat Keamanan Wajib:**
> - Pembagian **85% otomasi skrip vs 15% rahasia inti pemilik** dijelaskan lengkap di [KEAMANAN_KUNCI_PEMILIK.md](KEAMANAN_KUNCI_PEMILIK.md).
> - Dilarang keras memakai `.env.test`, `.env.test.alt`, atau `cmd/seed_test_accounts` di VPS produksi.
> - Dilarang menaruh berkas private key `age` (`AGE-SECRET-KEY-...`) di VPS maupun di repositori Git.

---

## 1. Persiapan Server Linux, NTP, Firewall, & Perkakas

Masuk via SSH ke VPS Linux dengan akun non-root berhak `sudo`:

```bash
sudo apt-get update && sudo apt-get upgrade -y
sudo apt-get install -y ca-certificates curl gnupg git age openssl postgresql-client chrony jq ufw
```

### A. Pastikan Jam Server Akurat (Wajib untuk TOTP MFA RFC 6238)
```bash
sudo systemctl enable --now chrony
timedatectl set-timezone Asia/Jakarta
timedatectl status
```

### B. Instal Docker Engine & Docker Compose v2 (Paket Resmi Linux)
Ikuti repositori resmi Docker untuk distro Anda (Ubuntu/Debian), lalu verifikasi:
```bash
docker --version
docker compose version
sudo usermod -aG docker "$USER"
```
*(Login ulang SSH agar keanggotaan grup `docker` aktif.)*

### C. Instal Node.js LTS & Go `1.27+` *(atau build aset di mesin/CI terpercaya lalu unggah `dist/`)*
```bash
# Contoh cek versi bila membangun langsung di VPS:
node -v   # >= v20 LTS (untuk membangun frontend/wireframe/dist)
npm -v
go version # >= go1.27.4 (untuk menjalankan cmd/envcheck)
```

### D. Kunci Firewall VPS (`ufw`)
Hanya buka SSH (`22`), HTTP (`80`), dan HTTPS (`443`). **Jangan** membuka `${DB_HOST_PORT}`, `${API_HOST_PORT}`, maupun `${WORKER_PORT}` ke internet publik karena akses web produksi dilayani lewat reverse proxy `Caddy` (`proxy` service):
```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw allow 443/udp
sudo ufw enable
sudo ufw status verbose
```

---

## 2. Unduh Kode & Bangun Frontend Statis

```bash
sudo mkdir -p /opt/kopdes /var/kopdes-keyvault
sudo chown -R "$USER":"$USER" /opt/kopdes /var/kopdes-keyvault
chmod 700 /var/kopdes-keyvault

git clone <URL_REPO_ANDA> /opt/kopdes
cd /opt/kopdes

# Bangun bundel produksi SPA TypeScript (output -> frontend/wireframe/dist/)
npm --prefix frontend/wireframe ci
npm --prefix frontend/wireframe run build
```

Pastikan direktori `frontend/wireframe/dist/index.html` sudah terbentuk sebelum menyalakan container `proxy`.

---

## 3. Konfigurasi `.env` & 15% Kunci Inti Pemilik

1. Buat `.env` awal dari cetak biru (`.env.example`):
   ```bash
   cd /opt/kopdes
   GENERATE_SECRETS=true bash scripts/promote_env.sh .env.example .env
   chmod 600 .env
   ```
2. **Bagian 15% Inti Pemilik (di perangkat offline pribadi pemilik — lihat [KEAMANAN_KUNCI_PEMILIK.md](KEAMANAN_KUNCI_PEMILIK.md)):**
   - Bangkitkan **2 pasang kunci `age` terpisah** di mesin pribadi:
     1. Pasangan harian (`kopdes-daily-backup-private.key`) -> ambil public key (`age1...`) untuk `BACKUP_AGE_RECIPIENT`.
     2. Pasangan brankas kunci (`kopdes-keyvault-private.key`) -> simpan public key (`age1...`) secara terpisah untuk `VAULT_AGE_RECIPIENT`.
   - Bangkitkan entropi root milik sendiri (`echo "v1:k1=$(openssl rand -hex 32)"`, `openssl rand -hex 32`) dan isi nilai rahasia inti di `/opt/kopdes/.env`:
     - `AES_MASTER_KEY=v1:k1=<64-hex-buatan-pemilik>`
     - `BLIND_INDEX_SECRET=<64-hex-buatan-pemilik>`
     - `DB_PASSWORD=<sandi-unik-kopdes_app>`
     - `POSTGRES_PASSWORD=<sandi-unik-postgres>`
     - `BACKUP_AGE_RECIPIENT=age1<public-key-backup-harian>`
3. **Setel parameter lingkungan & domain HTTPS di `/opt/kopdes/.env` (44 variabel tetap):**
   - `APP_ENV=production`
   - `ALLOWED_ORIGINS=https://koperasi.desaanda.id` *(harus persis sama dengan origin HTTPS publik)*
   - `TRUSTED_PROXIES=127.0.0.1/32,172.16.0.0/12` *(sesuaikan rentang bridge Docker lokal / IP proxy tepercaya; dilarang `/0`)*
   - `RATE_LIMIT_RPS=30` *(rentang produksi `1..120`)*
   - `DB_HOST=postgres`
   - `DB_USER=kopdes_app`
   - `DB_SSLMODE=disable` *(antar-kontainer pada jaringan internal terisolasi `kopdes_internal`, atau `require`/`verify-full` bila DB terpisah host)*
   - `COOKIE_SECURE=auto`
   - `ALLOW_BEARER_AUTH=false`
   - `MFA_REQUIRED_ROLES=admin,bendahara`
   - Pastikan kebijakan bisnis `KP-01`..`KP-10` disetel sesuai [GO_LIVE_CRITERIA.md](GO_LIVE_CRITERIA.md).
4. **Validasi ketat 44 variabel (tanpa membocorkan rahasia):**
   ```bash
   go -C backend run ./cmd/envcheck --strict --ports --no-listen --env-file "${PWD}/.env"
   go -C backend run ./cmd/envcheck --compare .env.example .env
   ```

---

## 4. Menyalakan Layanan Compose + Caddy HTTPS (`same-origin`)

[Caddyfile](../Caddyfile) membaca alamat situs dari variabel environment host `KOPDES_SITE_ADDRESS` (tidak menambah variabel ke `.env` Go agar tetap 44 variabel). Caddy menyajikan `frontend/wireframe/dist`, `/config.json` dengan `api_public_url: ""` *(same-origin)*, dan mem-proxy `/api/*`, `/healthz`, `/readyz` ke container `api`.

```bash
cd /opt/kopdes
set -a && source .env && set +a
export KOPDES_SITE_ADDRESS="https://koperasi.desaanda.id"

docker compose --env-file .env --profile app --profile worker --profile proxy config --quiet
docker compose --env-file .env --profile app --profile worker --profile proxy up -d --build
```

*(Catatan: jika menggunakan Cloudflare Proxy di depan VPS, pastikan mode SSL Cloudflare adalah **Full (strict)** dan tambahkan IP CIDR proxy di `TRUSTED_PROXIES` atau jalankan proxy lokal sesuai kebijakan Cloudflare/Caddy Anda.)*

---

## 5. Verifikasi Kesehatan Layanan, Role Non-Superuser, & Bootstrap Admin

1. **Cek kontainer & endpoint kesehatan:**
   ```bash
   docker compose --env-file .env ps
   curl -fsS "https://koperasi.desaanda.id/healthz"
   curl -fsS "https://koperasi.desaanda.id/readyz"
   ```
   Pastikan `/readyz` menampilkan `"database":"CONNECTED"` dan `"worker_degraded":false`.
2. **Verifikasi role PostgreSQL non-superuser:**
   ```bash
   docker compose --env-file .env exec -T postgres \
     psql -p "${DB_PORT}" -U kopdes_migrator -d "${DB_NAME}" \
     -c "SELECT rolname, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'kopdes_app';"
   ```
   Kolom `rolsuper` dan `rolbypassrls` wajib `f`.
3. **Buat akun Admin pertama (Ketua Koperasi):**
   ```bash
   docker compose --env-file .env exec -T api \
     /app/kopdes --env-file /app/.env admin create \
     --email ketua@desaanda.id --nama "Ketua Koperasi" --hp 08120000001 --password "<SANDI_MIN_12_KARAKTER>"
   ```
4. **Inisialisasi awal lewat browser (`https://koperasi.desaanda.id`):**
   - Login sebagai Admin -> pindai/salin secret TOTP di aplikasi Authenticator -> masukkan kode 6 digit + simpan **8 recovery codes** di brankas pengurus.
   - Masuk ke **08 Pengaturan & Pengguna** untuk mengatur profil koperasi & membuat akun Bendahara dan Petugas Toko.
   - Tambahkan rekening QRIS koperasi resmi & aktifkan di pengaturan.

---

## 6. Menjadwalkan Backup Harian, Brankas Kunci, & Latihan Pemulihan

1. **Segera cadangkan brankas kunci kriptografi ke luar repo (`/var/kopdes-keyvault`) & salin arsip `.tar.age` ke perangkat pemilik:**
   ```bash
   ENV_FILE=/opt/kopdes/.env \
   KEY_VAULT_DIR=/var/kopdes-keyvault \
   VAULT_AGE_RECIPIENT="age1_public_key_brankas_anda" \
   bash /opt/kopdes/scripts/backup_keys.sh --include-db
   ```
2. **Jadwalkan Cron Backup Harian Data (`03:00` WIB)** — worker aplikasi (`pesanan_expire`, `iuran_generate`, `pdp_purge`, `bukti_purge`, `rate_limits_cleanup`, `session_cleanup`) sudah berjalan otomatis di dalam container `worker`:
   ```cron
   0 3 * * * cd /opt/kopdes && ENV_FILE=/opt/kopdes/.env bash /opt/kopdes/scripts/backup.sh >> /var/log/kopdes_backup.log 2>&1
   ```
   Tambahkan sinkronisasi arsip `${BACKUP_DIR}` (`*.tar.age` dan `*.sha256`) ke object storage/server terpisah (mis. `rclone copy`) tiap malam.
3. **Latihan pemulihan berkala (bawa sementara private key dari media offline, lalu hapus kembali sesudahnya):**
   ```bash
   ENV_FILE=/opt/kopdes/.env BACKUP_AGE_IDENTITY_FILE=/mnt/usb/kopdes-daily-backup-private.key bash /opt/kopdes/scripts/restore_drill.sh
   VAULT_AGE_IDENTITY_FILE=/mnt/usb/kopdes-keyvault-private.key bash /opt/kopdes/scripts/restore_keys.sh --vault /var/kopdes-keyvault/kopdes_keyvault_*.tar.age --drill
   ```

---

## 7. Pembaruan Versi, Rotasi Kunci, & Pemulihan Darurat

### Update Aplikasi Tanpa Hilang Data
```bash
cd /opt/kopdes
git pull --ff-only origin <branch_rilis>
npm --prefix frontend/wireframe ci && npm --prefix frontend/wireframe run build
KOPDES_SITE_ADDRESS="https://koperasi.desaanda.id" docker compose --env-file .env --profile app --profile worker --profile proxy up -d --build
```
*(Jalankan migrasi file dekripsi bila diperlukan upgrade skema penyimpanan: `docker compose --env-file .env exec -T api /app/kopdes --env-file /app/.env storage migrate-files`.)*

### Pemulihan Akses Jika Admin/Ketua Kehilangan HP MFA atau Lupa Sandi
Gunakan CLI langsung dari SSH host VPS:
```bash
cd /opt/kopdes
docker compose --env-file .env exec -T api /app/kopdes --env-file /app/.env admin reset-mfa --email ketua@desaanda.id
docker compose --env-file .env exec -T api /app/kopdes --env-file /app/.env admin reset-password --email ketua@desaanda.id --password "<SANDI_DARURAT_MIN_12_KARAKTER>"
```
Semua sesi lama pengguna tersebut otomatis dicabut seketika dan tercatat pada `audit_log`.
