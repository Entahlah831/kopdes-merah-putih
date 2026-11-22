# Daftar Periksa Deployment VPS Staging (`STAGING_CHECKLIST`)

Gunakan daftar periksa ini sebelum mengizinkan UAT pengurus atau pengujian penetrasi di VPS Linux staging (target tipikal **1 VPS, >= 2 vCPU, >= 2 GB RAM**, Ubuntu/Debian + Docker Engine + Docker Compose v2). Langkah instalasi teknis rinci tersedia di [PANDUAN_PASANG_VPS_LINUX.md](PANDUAN_PASANG_VPS_LINUX.md) dan prosedur kunci 85/15 di [KEAMANAN_KUNCI_PEMILIK.md](KEAMANAN_KUNCI_PEMILIK.md).

> **Larangan Keras:** Jangan gunakan `.env.test` / `.env.test.alt` atau akun `seed_test_accounts` di lingkungan staging/produksi yang dapat diakses publik tanpa pembatasan. Jangan pernah menaruh private key `age` (`AGE-SECRET-KEY-...`) di VPS.

---

## 1. Persiapan Host VPS & Jaringan

- [ ] Sistem operasi Linux terbaru terpasang dengan pembaruan keamanan sistem, `docker` (Engine >= 24 + Compose v2), `age`, `psql` client (untuk verifikasi darurat), dan sinkronisasi waktu NTP (`timedatectl status` aktif; wajib agar TOTP MFA RFC 6238 tidak meleset).
- [ ] Firewall host (`ufw` / `nftables` / Security Group): hanya port `22` (SSH dibatasi IP admin / key-only), `80`, dan `443` yang terbuka ke publik.
- [ ] PostgreSQL (`DB_HOST_PORT`), `/readyz`/`WORKER_PORT`, dan UI tester tidak diekspos ke `0.0.0.0`; seluruh binding host di [docker-compose.yml](../docker-compose.yml) mengarah ke `127.0.0.1` kecuali API di belakang reverse proxy HTTPS (`Caddy` / [Caddyfile](../Caddyfile)).
- [ ] Domain HTTPS staging aktif (mis. `https://staging-koperasi.example.id`) dengan sertifikat TLS valid sebelum `APP_ENV=production` / `APP_ENV=staging`.

## 2. Konfigurasi Tunggal `.env` (**44 Variabel <= 45**)

- [ ] Di mesin lokal tepercaya atau sesi aman VPS, generate `.env` dari cetak biru [../.env.example](../.env.example):
  ```bash
  GENERATE_SECRETS=true bash scripts/promote_env.sh .env.example .env
  ```
- [ ] Generate **2 pasang kunci `age` di luar VPS** (backup data `BACKUP_AGE_RECIPIENT` + brankas kunci `VAULT_AGE_RECIPIENT` sesuai [KEAMANAN_KUNCI_PEMILIK.md](KEAMANAN_KUNCI_PEMILIK.md)):
  ```bash
  age-keygen -o kopdes-staging-private.key
  age-keygen -o kopdes-staging-keyvault-private.key
  ```
  Simpan *private key* secara offline; salin **HANYA** `age1...` (*public key*) ke `BACKUP_AGE_RECIPIENT` di `.env` (`KP-10`).
- [ ] Isi sendiri bagian inti 15% (`AES_MASTER_KEY`, `BLIND_INDEX_SECRET`, sandi DB) dan variabel wajib di `.env` (tanpa pernah mengomit ke Git):
  - `APP_ENV=staging` (atau `production` saat uji mode ketat penuh)
  - `ALLOWED_ORIGINS=https://staging-koperasi.example.id` (dilarang `*`)
  - `TRUSTED_PROXIES=<IP_CIDR_PROXY_LOKAL>/32` (rentang sempit `/16`–`/32`)
  - `RATE_LIMIT_RPS=30` (bawaan produksi `1..120 rps`; nilai tinggi mis. `1500` hanya diizinkan pada `APP_ENV=test`)
  - `DB_HOST=postgres`, `DB_USER=kopdes_app`, `DB_SSLMODE=disable` *(jaringan bridge internal Compose)* atau `require` bila lintas host
  - `DB_PASSWORD`, `POSTGRES_PASSWORD`, `AES_MASTER_KEY` (format `v1:k1=<64-hex>`), `BLIND_INDEX_SECRET` (>= 64 hex unik)
  - `COOKIE_SECURE=auto`, `ALLOW_BEARER_AUTH=false`, `MFA_REQUIRED_ROLES=admin,bendahara`
  - Parameter kebijakan bisnis (`STOCK_HOLD_MINUTES`, `QRIS_UNIQUE_DIGITS`, `DUAL_CONTROL_BREAKGLASS_ENABLED`, `ALLOW_PERSONAL_PAYMENT_PILOT`, `PDP_REJECTED_RETENTION_DAYS`, `BUKTI_RETENTION_DAYS`) sesuai keputusan tertulis `KP-01`–`KP-10` di [GO_LIVE_CRITERIA.md](GO_LIVE_CRITERIA.md) & [TUGAS_MANUAL_PEMILIK.md](TUGAS_MANUAL_PEMILIK.md).
- [ ] Jalankan validasi ketat tanpa kebocoran rahasia:
  ```bash
  go -C backend run ./cmd/envcheck --strict --ports --no-listen --env-file "${PWD}/.env"
  go -C backend run ./cmd/envcheck --compare .env.example .env
  ```

## 3. Boot Layanan Compose, Role DB & Akun Pengurus Awal

- [ ] Bangun UI produksi (`npm --prefix frontend/wireframe ci && npm --prefix frontend/wireframe run build`), verifikasi sintaks Compose, lalu nyalakan DB + API + Worker + Caddy Proxy:
  ```bash
  docker compose --env-file .env --profile app --profile worker --profile proxy config --quiet
  KOPDES_SITE_ADDRESS="https://staging-koperasi.example.id" docker compose --env-file .env --profile app --profile worker --profile proxy up -d --build
  ```
- [ ] Verifikasi kontainer berjalan sehat (`docker compose --env-file .env ps`) dan API non-root:
  ```bash
  curl -fsS "http://127.0.0.1:${API_HOST_PORT}/healthz"
  curl -fsS "http://127.0.0.1:${API_HOST_PORT}/readyz"
  docker compose --env-file .env exec -T worker curl -fsS "http://127.0.0.1:${WORKER_PORT}/healthz"
  ```
  Pastikan `/readyz` mengembalikan `"database":"CONNECTED"` dan `"worker_degraded":false`.
- [ ] Pastikan role database runtime non-superuser berlaku:
  ```bash
  docker compose --env-file .env exec -T postgres psql -U kopdes_migrator -d "${DB_NAME}" -c "SELECT rolname, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'kopdes_app';"
  ```
  Nilai `rolsuper` dan `rolbypassrls` **wajib `f`** (*false*).
- [ ] Buat akun Admin pertama & aktifkan MFA TOTP:
  ```bash
  docker compose --env-file .env exec -T api /app/kopdes --env-file /app/.env admin create \
    --email ketua@koperasi.desa.id --nama "Ketua Koperasi" --hp 08120000001 --password "<SANDI_KUAT>"
  ```
- [ ] Login via browser (HTTPS), selesaikan enrollment MFA 2-langkah, catat 8 `recovery_codes` di tempat aman, lalu buat rekening QRIS koperasi dan aktifkan pada `PUT /api/v1/pengaturan`.

## 4. Uji Operasional, Latihan Pengurus, Backup `age`, & Pemulihan Darurat

- [ ] Jalankan 1 siklus UAT penuh di browser (`Daftar + Upload 3 KYC -> Approve -> Aktivasi -> Checkout + Upload Bukti -> Verifikasi Bendahara -> Selesai`) dan cek volume terpisah: berkas `.enc` tersimpan terpisah tanpa plaintext PNG/JPG header.
- [ ] Laksanakan pelatihan langsung pengurus inti mengikuti [PANDUAN_PENGURUS.md](PANDUAN_PENGURUS.md) dan selesaikan semua 6 latihan pada [LEMBAR_PELATIHAN_PENGURUS.md](LEMBAR_PELATIHAN_PENGURUS.md).
- [ ] Jalankan backup harian nyata & verifikasi artifact `.tar.age`:
  ```bash
  ENV_FILE=.env bash scripts/backup.sh
  ```
- [ ] Latih pemulihan menggunakan private key sementara/terpisah (lalu hapus private key dari mesin bila hanya dipinjamkan untuk drill) dan pastikan uji tamper 1 byte gagal:
  ```bash
  ENV_FILE=.env BACKUP_AGE_IDENTITY_FILE=/path/aman/kopdes-staging-private.key bash scripts/restore_drill.sh
  VAULT_AGE_IDENTITY_FILE=/path/aman/kopdes-staging-keyvault-private.key bash scripts/restore_keys.sh --vault /var/kopdes-keyvault/kopdes_keyvault_*.tar.age --drill
  ```
- [ ] Uji runbook pemulihan akses admin & staf (`reset-password`, `reset-mfa`) di staging (memastikan CLI mencabut sesi aktif & menulis `audit_log`), lalu daftarkan ulang MFA admin.

## 5. Tabel Persetujuan Staging (`Sign-Off`)

| Tahap | Pelaksana | Tanggal | Commit SHA | Bukti Terlampir |
| :--- | :--- | :---: | :---: | :--- |
| Validasi `.env` 44 Var & Boot Compose | | | | `envcheck`, `/readyz` |
| Uji Backup `.tar.age` & `restore_drill.sh` | | | | Log RTO & 21 tabel |
| Pelatihan & UAT Pengurus (Semua Peran Inti) | | | | [LEMBAR_PELATIHAN_PENGURUS.md](LEMBAR_PELATIHAN_PENGURUS.md) |
| Penutupan Keputusan Pemilik Awal (`KP-01`..`KP-10`) & Siap Pentest | | | | Laporan CI ([PERFORMANCE_K6.md](PERFORMANCE_K6.md), [ZAP_TRIAGE.md](security/ZAP_TRIAGE.md)) + [GO_LIVE_CRITERIA.md](GO_LIVE_CRITERIA.md) |
