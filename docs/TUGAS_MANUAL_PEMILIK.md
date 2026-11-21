# Tugas Manual Pemilik Proyek

Dokumen ini berisi langkah-langkah yang **wajib dilakukan langsung oleh pemilik sistem** pada `.env` asli, penyimpanan kunci backup off-server, dan keputusan kebijakan bisnis sebelum go-live (agen dilarang membaca, mengubah, atau mengomit `.env` asli).

## 1. Sinkronisasi `.env` Asli dari `.env.example` (51 Variabel)

Jalankan skrip sinkronisasi agar seluruh 51 kunci variabel terbaru (termasuk `WORKER_PORT`, `WORKER_TICK_INTERVAL`, `SLO_READ_P95_MS`, `SLO_WRITE_P95_MS`, `SLO_MAX_ERROR_RATE`) hadir tanpa menimpa rahasia yang sudah Anda isi:

- **Windows (PowerShell):**
  ```powershell
  .\scripts\promote_env.ps1 -Template .env.example -Target .env -GenerateEmptySecrets
  ```
- **Linux / macOS:**
  ```bash
  GENERATE_SECRETS=true bash scripts/promote_env.sh .env.example .env
  ```

Verifikasi bahwa tidak ada variabel wajib yang kosong atau konflik port:
```bash
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env
go run ./cmd/envcheck --compare .env.example .env
```
*(Seluruh nilai rahasia disamarkan menjadi `***REDACTED***` oleh `envcheck`.)*

---

## 2. Pengisian Kunci Produksi & Aturan Ketat Kunci Backup `age`

> **PERINGATAN KRITIS — KUNCI BACKUP `age` WAJIB DISIMPAN DI LUAR SERVER:**
> - Bangkitkan pasangan kunci `age` di perangkat tepercaya pemilik (di luar VPS): `age-keygen -o kopdes-backup-private.key`.
> - Isi **`BACKUP_AGE_RECIPIENT`** di `.env` VPS **HANYA** dengan *public key* (`age1...`).
> - **JANGAN PERNAH** menaruh berkas `AGE-SECRET-KEY-...` (*private key*) di dalam VPS, `.env`, atau repositori Git!
> - Simpan salinan terenkripsi/fisik *private key* `age` di **minimal 2 tempat aman terpisah milik pemilik** (mis. password manager utama + brankas offline pengurus). **Kehilangan private key berarti seluruh arsip backup `.tar.age` tidak dapat dibuka selamanya.**

Pastikan variabel berikut juga disetel unik dengan CSPRNG (`openssl rand -hex 32`):
- `AES_MASTER_KEY` (>= 32 byte / 64 hex, opsional format `ver:id=material`) & `AES_PREVIOUS_KEYS` (saat rotasi kunci — lihat [ROTASI_KUNCI.md](ROTASI_KUNCI.md) bila tersedia / SOP rotasi di [BACKEND_GUIDE.md](BACKEND_GUIDE.md)).
- `BLIND_INDEX_SECRET` (>= 32 byte / 64 hex).
- `DB_PASSWORD` dan `POSTGRES_PASSWORD` (kata sandi kuat unik).
- `DB_USER=kopdes_app` (role non-superuser aplikasi) dan `DB_SSLMODE=require` / `verify-full` bila memakai koneksi lintas host.
- `COOKIE_SECURE=auto` (memakai cookie `__Host-kopdes_session` otomatis saat HTTPS/proxy TLS di `TRUSTED_PROXIES`), `ALLOW_BEARER_AUTH=false` untuk web SPA.
- `ALLOWED_ORIGINS` berisi origin frontend HTTPS resmi koperasi (dilarang `*` di produksi).

---

## 3. Keputusan Kebijakan Bisnis, Hukum, & Akuntansi (**KEPUTUSAN PEMILIK**)

| Variabel / Kolom | Nilai Bawaan Aman | Status Keputusan |
| :--- | :---: | :--- |
| `STOCK_HOLD_MINUTES` | `60` | **KEPUTUSAN PEMILIK** — lama tahanan stok tertahan saat checkout (`>= PAYMENT_EXPIRY_DURATION` dalam menit). |
| `DUAL_CONTROL_BREAKGLASS_ENABLED` | `false` | **KEPUTUSAN PEMILIK** — aktifkan `true` hanya sementara jika koperasi sedang kekurangan staf (1 admin darurat); tetap butuh `mfa_code` TOTP terbaru & alasan >= 10 karakter + audit log khusus. |
| `ALLOW_PERSONAL_PAYMENT_PILOT` | `false` | **KEPUTUSAN PEMILIK** & **PERLU VERIFIKASI HUKUM** — tetap `false` sampai QRIS badan hukum koperasi terbit; jika dipakai pilot, dikunci `PILOT_MAX_PER_TX=500000` & `PILOT_MAX_PER_DAY=2000000`. |
| `QRIS_UNIQUE_DIGITS` | `3` | **KEPUTUSAN PEMILIK** — `3` digit (`1..999`, cukup untuk `PENDING_LIMIT_GLOBAL=50`) atau `4` digit (`1..9999`). |
| `PDP_REJECTED_RETENTION_DAYS` | `30` | **PERLU VERIFIKASI HUKUM** — masa simpan data pendaftaran ditolak sebelum dihapus keras oleh worker `pdp_purge`. |
| `BUKTI_RETENTION_DAYS` | `365` | **PERLU VERIFIKASI HUKUM** & **KEPUTUSAN AKUNTAN** — masa retensi berkas bukti transaksi gagal/batal/kedaluwarsa sebelum dihapus worker `bukti_purge` (bukti transaksi `lunas` tetap dipertahankan). |
| `jenis_iuran.klasifikasi_akuntansi` & refund/selisih kode unik | Label teks netral | **KEPUTUSAN AKUNTAN** — tetapkan pemetaan pos akuntansi bersama akuntan koperasi sebelum tutup buku resmi. |
| Item wireframe `BLOCKED` ([ANALISIS_KESENJANGAN.md](wireframe/ANALISIS_KESENJANGAN.md)) | Non-aktif di `frontend-wireframe/` | **KEPUTUSAN PEMILIK** — pastikan ruang lingkup tetap 1 koperasi (non-SIMKOPDES, non-Transfer Bank, non-PDF bukti, non-RAT/CMS) sebelum memproduksi UI final. |

---

## 4. Inisialisasi VPS, Worker Bawaan, Pemulihan Admin, & Backup Off-Server

1. Pastikan konfigurasi valid dan jalankan layanan Compose inti (`postgres`, `api` profil `app`, `worker` profil `worker`, plus opsional `proxy` Caddy):
   ```bash
   go run ./cmd/envcheck --strict --ports --no-listen --env-file .env
   docker compose --env-file .env --profile app --profile worker config --quiet
   docker compose --env-file .env --profile app --profile worker up -d --build
   ```
2. Jika upgrade dari versi sebelum `decision-audit`, jalankan migrasi berkas storage sekali secara idempoten:
   ```bash
   docker compose exec api /app/kopdes --env-file /app/.env storage migrate-files
   ```
3. Buat akun admin awal (wajib setup MFA TOTP saat login pertama):
   ```bash
   docker compose exec api /app/kopdes --env-file /app/.env admin create \
     --email ketua@koperasi.desa.id --nama "Ketua Koperasi" --hp 08120000001 --password "<SANDI_KUAT>"
   ```
4. Verifikasi `/readyz` menampilkan `"database": "CONNECTED"`, `worker_jobs`, dan `"worker_degraded": false`, serta `GET /api/v1/public/config` merespons normal.
5. **Runbook Pemulihan Akses Jika Admin Kehilangan Ponsel MFA atau Lupa Sandi**:
   Masuk via SSH ke VPS pemilik lalu jalankan:
   ```bash
   docker compose exec api /app/kopdes --env-file /app/.env admin reset-mfa --email ketua@koperasi.desa.id
   docker compose exec api /app/kopdes --env-file /app/.env admin reset-password --email ketua@koperasi.desa.id --password "<SANDI_BARU_MIN_12_KARAKTER>"
   ```
   Kedua perintah otomatis mencabut seluruh sesi aktif dan menulis jejak ke `audit_log`.
6. **Cron Backup Harian & Salinan Off-Server (Hanya 1 Tugas Cron di Host)**:
   Tugas rutin (`pesanan_expire`, `iuran_generate`, `pdp_purge`, `bukti_purge`, `rate_limits_cleanup`) kini otomatis berjalan di kontainer `worker`. Di host VPS cukup jadwalkan backup harian terenkripsi `age` + sinkronisasi ke storage terpisah (`rclone copy` / `restic`):
   ```cron
   0 3 * * * cd /opt/kopdes && ENV_FILE=/opt/kopdes/.env bash /opt/kopdes/scripts/backup.sh
   ```
   Lakukan uji pemulihan berkala (dengan kunci private dari perangkat terpisah ke staging DB) menggunakan [restore_test.sh](../scripts/restore_test.sh) atau [restore_drill.sh](../scripts/restore_drill.sh).
7. **WAF/Edge Cloudflare**: Aktifkan Cloudflare proxied HTTPS dan rule rate-limit edge pada jalur `/api/v1/public/*` dan `/api/v1/auth/login` untuk menahan serangan volumetrik sebelum menyentuh VPS.
