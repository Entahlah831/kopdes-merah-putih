# Frontend Tester Polos — Kopdes Merah Putih (Ramping)

Alat bantu uji interaktif berbasis HTML dan JavaScript murni tanpa framework, tanpa build step, dan tanpa CDN untuk membuktikan kontrak **35 endpoint HTTP** Backend Ramping Kopdes Merah Putih (1 koperasi tunggal, 19 tabel PostgreSQL 16).

## Fitur Utama
1. **Konfigurasi Dinamis (`/config.json` & `GET /api/v1/public/config`)**: Tidak ada URL, port, atau rahasia yang ditulis keras di HTML/JS.
2. **Sesi Opaque + Helper TOTP**: Login anggota maupun staf (`admin` / `bendahara` wajib MFA TOTP), penyimpanan token di memori JavaScript saja (tanpa `localStorage` / `sessionStorage`), dan `/api/totp-helper` hanya aktif pada lingkungan non-produksi.
3. **Live Demo End-to-End**: Menjalankan alur nyata pendaftaran -> KYC terenkripsi `.enc` -> verifikasi admin -> aktivasi -> checkout toko -> QRIS kode unik -> verifikasi dual control bendahara -> daftar antar kurir -> laporan CSV.
4. **Scenario Runner & Role Matrix**: Memverifikasi `scenarios.json` dan `roles_matrix.json` langsung terhadap database uji (`Set A`).
5. **Explorer 35 Endpoint & Halaman Referensi**: Formulir uji manual serta empat halaman referensi integrasi di [`reference/`](reference/index.html).

## Cara Menjalankan
Jalankan API backend dan server UI Tester memakai `.env.test` (jangan gunakan `.env` produksi):

```powershell
go run ./cmd/seed_test_accounts --env-file .env.test
go run ./cmd/server --env-file .env.test
go run ./cmd/uitester --env-file .env.test
```

Buka URL sesuai nilai `UI_PORT` dari `.env.test` (mis. lihat output log `cmd/uitester`), lalu masukkan kata sandi sintetis `TEST_SEED_PASSWORD` di kolom UI Tester.
