# Tugas Manual Pemilik Proyek

Dokumen ini berisi langkah-langkah yang harus dilakukan langsung oleh pemilik sistem pada file `.env` asli dan lingkungan produksi (karena agen dilarang membaca/mengedit `.env` asli maupun menyimpan rahasia nyata di repositori).

## 1. Sinkronisasi `.env` Asli dari `.env.example`

Jika sebelumnya Anda sudah memiliki `.env` dari versi awal, jalankan skrip sinkronisasi agar seluruh kunci variabel baru dari `internal/config/config.go` ikut ditambahkan tanpa menimpa rahasia yang sudah Anda isi:

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
*(Kedua perintah di atas menyamarkan seluruh nilai rahasia dengan `***REDACTED***`.)*

## 2. Pengisian Kunci & Kata Sandi Produksi

Pastikan seluruh nilai berikut di `.env` produksi dibuat unik menggunakan CSPRNG (`openssl rand -hex 32`) dan **bukan** nilai dari `.env.test` / `.env.test.alt`:

- `JWT_SECRET`, `AES_MASTER_KEY`, `BLIND_INDEX_SECRET`, `QRIS_SECRET` (>= 32 byte / 64 karakter hex)
- `METRICS_TOKEN` (>= 32 byte, jangan gunakan token pendek)
- `DB_PASSWORD`, `KOPDES_APP_PASSWORD`, `POSTGRES_PASSWORD` (kata sandi kuat unik)
- `BACKUP_ENCRYPTION_KEY` (passphrase panjang untuk enkripsi backup harian `scripts/backup.sh`)
- `DB_SSLMODE=require` atau `verify-full` saat memakai koneksi jaringan non-loopback
- `ALLOWED_ORIGINS` berisi domain frontend resmi koperasi (dilarang wildcard `*` saat `APP_ENV=production`)
- `ALLOW_SUPERUSER_DB=false`

## 3. Verifikasi Manual Akhir di Host Target Pemilik

Karena host pengujian saat pengembangan tidak memiliki daemon Docker lokal maupun sertifikat TLS domain koperasi:
1. Jalankan di server target:
   ```bash
   go run ./cmd/envcheck --strict --ports --env-file .env
   docker compose --env-file .env config --quiet
   docker compose --env-file .env up -d --build
   ```
2. Verifikasi `curl -fsS http://127.0.0.1:${API_HOST_PORT}/readyz` mengembalikan HTTP `200`.
3. Pastikan `/metrics` tidak terekspos ke publik oleh reverse proxy (Caddy/Nginx) dan hanya di-scrape Prometheus menggunakan `METRICS_TOKEN`.
4. Konsultasikan dengan konsultan hukum/notaris mengenai kesesuaian masa retensi `PDP_REJECTED_RETENTION_DAYS` (default 30 hari — **PERLU VERIFIKASI HUKUM**).
5. Tetapkan klasifikasi pembukuan akuntansi untuk setiap jenis iuran (`pokok`, `wajib`, `sukarela`) bersama akuntan koperasi sebelum periode tutup buku (**KEPUTUSAN AKUNTAN**).
6. Lengkapi kredensial penyedia eksternal nyata saat beralih dari stub/outbox lokal: gateway SMS/WhatsApp (`NOTIFIER_*`), penyimpanan objek S3/MinIO untuk berkas KYC (jika pindah dari `STORAGE_KYC_DIR` lokal), serta payload QRIS badan hukum koperasi dari PJP resmi ketika menonaktifkan `ALLOW_PERSONAL_PAYMENT_PILOT=false`.
7. Bila membangun biner dari mesin pengembang Windows secara langsung (non-Docker), perbarui toolchain Go lokal ke rilis patch terbaru (di mesin dev terdeteksi `go1.27.1`, sementara `govulncheck` pada container Linux CI sudah lolos `No vulnerabilities found`).
