# Tugas Manual Pemilik Proyek

Dokumen ini berisi langkah-langkah yang wajib dilakukan langsung oleh pemilik sistem pada file `.env` asli dan lingkungan produksi (agen dilarang membaca, mengubah, atau mengomit `.env` asli).

## 1. Sinkronisasi `.env` Asli dari `.env.example` (40 Variabel)

Jalankan skrip sinkronisasi agar seluruh 40 kunci variabel standar tetap sinkron tanpa menimpa rahasia yang sudah Anda isi:

- **Windows (PowerShell):**
  ```powershell
  .\scripts\promote_env.ps1 -Template .env.example -Target .env
  ```
- **Linux / macOS:**
  ```bash
  bash scripts/promote_env.sh .env.example .env
  ```

Setelah sinkronisasi, cek apakah masih ada variabel wajib yang kosong atau konflik port dengan:
```bash
go run ./cmd/envcheck --strict --ports --env-file .env
go run ./cmd/envcheck --compare .env.example .env
```
*(Seluruh nilai rahasia disamarkan menjadi `***REDACTED***` oleh `envcheck`.)*

## 2. Pengisian Kunci & Kata Sandi Produksi

Pastikan nilai berikut di `.env` produksi dibuat unik menggunakan CSPRNG (`openssl rand -hex 32`) dan **bukan** nilai dari `.env.test` / `.env.test.alt`:

- `AES_MASTER_KEY` (opsional format `ver:id=material`, >= 32 byte) dan `AES_PREVIOUS_KEYS` (saat rotasi kunci)
- `BLIND_INDEX_SECRET` (>= 32 byte / 64 karakter hex)
- `DB_PASSWORD` dan `POSTGRES_PASSWORD` (kata sandi kuat unik)
- `BACKUP_ENCRYPTION_KEY` (passphrase panjang untuk enkripsi backup harian `scripts/backup.sh`)
- `DB_USER=kopdes_app` (role non-superuser aplikasi)
- `DB_SSLMODE=require` atau `verify-full` saat memakai koneksi jaringan non-loopback
- `ALLOWED_ORIGINS` berisi domain frontend resmi koperasi (dilarang wildcard `*` saat `APP_ENV=production`)
- `ALLOW_PERSONAL_PAYMENT_PILOT=false` sampai pemilik resmi mengizinkan penggunaan QRIS pilot pribadi

## 3. Verifikasi & Inisialisasi Awal di Host Target Pemilik

1. Pastikan konfigurasi valid dan jalankan stack Docker:
   ```bash
   go run ./cmd/envcheck --strict --ports --env-file .env
   docker compose --env-file .env config --quiet
   docker compose --env-file .env up -d --build
   ```
2. Buat akun admin awal dan aktifkan MFA saat login pertama:
   ```bash
   go run ./cmd/kopdes --env-file .env admin create --email admin@koperasi.desa.id --nama "Ketua Koperasi" --hp 08120000001 --password "<SANDI_KUAT>"
   ```
3. Verifikasi `curl -fsS http://127.0.0.1:${API_HOST_PORT}/readyz` mengembalikan HTTP `200` dan `GET /api/v1/public/config` merespons normal.
4. Jadwalkan cron operasional (opsional di host):
   - Bulanan: `kopdes --env-file .env iuran generate --periode YYYY-MM`
   - Harian: `kopdes --env-file .env pesanan expire`, `kopdes --env-file .env pdp purge`, dan `ENV_FILE=.env bash scripts/backup.sh`
5. **PERLU VERIFIKASI HUKUM**: Konsultasikan masa retensi pendaftar ditolak (`PDP_REJECTED_RETENTION_DAYS`, default 30 hari) dengan penasihat hukum / pengurus koperasi.
6. **KEPUTUSAN AKUNTAN**: Tetapkan perlakuan akuntansi untuk jenis iuran (`jenis_iuran.klasifikasi_akuntansi`) dan selisih kode unik QRIS (`pembayaran.catatan_akuntansi`) bersama akuntan koperasi sebelum tutup buku.
7. Catatan backup: saat ini backup memakai `pg_dump` logis + enkripsi `openssl enc` (`scripts/backup.sh`); arsip WAL/PITR penuh belum diimplementasikan.
