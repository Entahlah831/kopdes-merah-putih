# 15. Cara Menguji Integrasi Tanpa Bertanya ke Backend

Pengembang frontend dan QA dapat membuktikan sendiri seluruh alur, payload, status error, peran, hingga perilaku SDK tanpa perlu membuka kode Go atau menebak respons server.

---

## 1. Empat Alat Uji Mandiri yang Sudah Tersedia

1. **Frontend Tester "Lab Belajar"** ([frontend-tester/](../../frontend-tester/index.html) dijalankan via `go run ./cmd/uitester` pada `.env.test`)
2. **Halaman Referensi Minimal Berbasis Pola SDK** ([frontend-tester/reference/](../../frontend-tester/reference/index.html))
3. **Suite Unit & Live SDK Test** ([frontend-reference/test/sdk.test.mjs](../../frontend-reference/test/sdk.test.mjs) lewat `npm --prefix frontend-reference test`)
4. **Koleksi Postman & OpenAPI Validator** ([kopdes.postman_collection.json](../kopdes.postman_collection.json) & [openapi.yaml](../openapi.yaml))

## 2. Mode Pengujian Standar vs Opt-In Lockout

- **Mode Standar (Disarankan Sehari-hari)**:
  - Menjalankan **26 dari 27 skenario** klaim aktif (`CLAIM-AUTH-BRUTEFORCE-429` dilewati/`SKIP` agar IP lokal Anda tidak terkunci 15 menit saat bekerja).
  - Menjalankan **Live Demo 10 Modul End-to-End**.
  - Menjalankan **Matriks Peran 55 Operasi x 7 Peran + Cross-Tenant**.
- **Mode Lockout 429 (Opt-In Terpisah)**: Centang kotak opsi uji brute-force hanya bila Anda sengaja menguji skenario `CLAIM-AUTH-BRUTEFORCE-429`, lalu reset server/limiter bila perlu.

## 3. Cara Menjalankan Stack Uji Lokal Secara Mandiri

1. Pastikan `.env.test` (Set A) telah terverifikasi:
   ```bash
   go run ./cmd/envcheck -env .env.test -strict
   ```
2. Jalankan migrasi & seed akun uji sintetis (`anggota_a`, `ketua_a` ber-MFA, `bendahara_a`, `admin_b`, dll.):
   ```bash
   go run ./cmd/seed_test_accounts
   ```
3. Jalankan server API dan Frontend Tester di dua terminal terpisah (menggunakan env `.env.test`):
   ```bash
   go run ./cmd/server
   go run ./cmd/uitester
   ```
4. Buka URL tester (sesuai `UI_BIND` & `UI_PORT` di `.env.test`).

## 4. Fitur Utama Frontend Tester "Lab Belajar"

- **Diagnostik Lingkungan**: Klik sekali untuk memverifikasi `/healthz`, `/readyz`, `/api/v1/public/config`, CORS `/api/probe-cors`, serta selisih jam perangkat vs server.
- **Config Playground**: Ubah JSON konfigurasi secara interaktif, validasi langsung terhadap [frontend.config.schema.json](frontend.config.schema.json), dan lihat bagaimana banner Read-Only atau tombol bayar berubah.
- **Simulator Kondisi**: Latih layar aplikasi menghadapi *offline*, *timeout 15s*, `401` paralel (*single-flight*), `429 Retry-After`, dan `503 READ_ONLY_MODE`.
- **Panduan Terpandu ("Berikutnya")**: Ikuti 7 langkah terpandu dari mengambil config publik hingga laporan keuangan dengan penanda progres otomatis.
- **Lab Belajar Per Bab (`A`–`F`)**: Pilih bab mana saja dari `00` sampai `19` untuk membaca tujuan, menjalankan request live ke API nyata, melihat penjelasan tiap field respons, dan menyalin snippet kode TypeScript SDK / `fetch` / cURL.
- **Unduh Laporan Bukti (`JSON` & `Markdown`)**: Menghasilkan laporan bukti lengkap berisi status 27 skenario, Diagnostik, Matriks 55 Operasi, serta daftar hal `UNVERIFIED` yang memang tidak bisa diotomasi di API.

## 5. Checklist QA Mandiri

- [ ] Seluruh test unit SDK (`npx --prefix frontend-reference tsc --noEmit` & `npm --prefix frontend-reference test`) hijau.
- [ ] `26 PASS / 1 SKIP / 0 FAIL` pada Skenario Standar + `10/10 PASS` pada Live Demo.
- [ ] Tidak ada token tersimpan di `localStorage`/`sessionStorage`/`document.cookie` sesudah test selesai.
