# Frontend Tester Polos — Kopdes Merah Putih

Antarmuka web statis ultra-sederhana (HTML polos + `<style>` minimal + vanilla JavaScript ES2020) untuk menguji seluruh endpoint API Kopdes Merah Putih secara langsung di browser serta membuktikan kesesuaian dokumentasi (`docs/FRONTEND_GUIDE.md`, `docs/BACKEND_GUIDE.md`, `docs/openapi.yaml`, dan `docs/roles_matrix.json`) terhadap perilaku server nyata.

## Prinsip Desain & Keamanan

- **Tanpa framework, tanpa build step, tanpa CDN/skrip pihak ketiga**: seluruh berkas statis berada di `frontend-tester/` (`index.html`, `app.js`, serta artefak JSON hasil generator).
- **Single source of truth dari environment**: server `cmd/uitester` mengekspos `/config.json` dari variabel lingkungan (`UI_BIND`, `UI_PORT`, `API_PUBLIC_URL`, beserta data koperasi & akun uji sintetis dari DB). Tidak ada nilai port, URL, kredensial produksi, atau rahasia hardcode di dalam kode frontend.
- **Tanpa penyimpanan persisten browser**: token JWT access, refresh token, `mfa_session_token`, dan state sesi pengujian **hanya disimpan di memori tab browser** (tidak memakai `localStorage`, `sessionStorage`, ataupun cookie).
- **Helper lokal tester**: `cmd/uitester` menyediakan `POST /api/totp-helper` untuk menghasilkan kode TOTP 6 digit dari secret akun uji sintetis (termasuk parameter opsional `offset_seconds`, misalnya `30` detik agar terhindar dari benturan replay window TOTP saat pengujian berturut-turut) dan `POST /api/probe-cors` untuk menguji header CORS dari browser.

---

## Prasyarat

1. Database PostgreSQL 16 uji sudah aktif, termigrasi penuh, dan dapat diakses menggunakan konfigurasi profil uji (mis. `.env.test` atau `.env.test.alt`, **jangan** menggunakan `.env` produksi).
2. Server API Kopdes (`cmd/api`) sudah berjalan dan dapat dijangkau melalui `API_PUBLIC_URL`.
3. Variabel environment berikut tersedia pada berkas profil uji:
   - `UI_BIND` dan `UI_PORT` — alamat bind dan port mendengarkan `cmd/uitester`
   - `API_PUBLIC_URL` — base URL server API yang diakses tester
   - `TEST_SEED_PASSWORD` — kata sandi seragam untuk akun uji sintetis `@kopdes-test.local`

---

## Menyiapkan Akun Uji Sintetis (`seed_test_accounts`)

Jalankan seeder idempoten sebelum menjalankan skenario atau matriks peran:

```bash
go run ./cmd/seed_test_accounts --env-file .env.test
```

Atau gunakan skrip pembantu yang otomatis menolak berkas konfigurasi produksi (`.env`, `.env.local`, `.env.production`, `.env.live`):

- Windows PowerShell: `.\scripts\seed_test_accounts.ps1 -EnvFile .env.test`
- Linux/macOS Shell: `./scripts/seed_test_accounts.sh .env.test`

Seeder ini menyiapkan tenant uji (Tenant A dan Tenant B), akun uji sintetis untuk seluruh 7 peran (`admin`, `pengurus`, `bendahara`, `ketua`, `pengawas`, `operator`, `anggota`), akun khusus pengujian wajib MFA (`admin_nomfa_a`), serta resource ID referensi lintas tenant (seperti `produk_b_id`, `pembayaran_b_id`, `rekening_qris_b_id`, `anggota_b_id`) untuk verifikasi isolasi anti-IDOR/anti-BOLA.

> **Catatan reset status MFA:** Menjalankan skenario `CLAIM-AUTH-MFA-MANDATORY-01` (atau tombol aliran `3.1b`) akan menyelesaikan proses enrollment MFA untuk `admin_nomfa_a`. Jika Anda ingin mengulang skenario tersebut dari awal (status belum ber-MFA), jalankan ulang `go run ./cmd/seed_test_accounts --env-file .env.test`.

---

## Menjalankan Frontend Tester

### Opsi 1: Langsung dengan Go (Lokal)

```bash
go run ./cmd/uitester --env-file .env.test --ui-dir frontend-tester
```

Lalu buka browser ke `http://<UI_BIND>:<UI_PORT>` sesuai nilai `UI_BIND` dan `UI_PORT` pada `.env.test`.

### Opsi 2: Menggunakan Docker Compose (`docker-compose.test.yml`, Profil `tester`)

```bash
docker compose -f docker-compose.test.yml --profile tester --env-file .env.test up -d
```

Service `uitester` berjalan pada profil `tester` (`entrypoint: ["/app/uitester", "--ui-dir", "/app/frontend-tester"]`) dan mengekspos endpoint berikut selain berkas statis HTML/JS:
- `/config.json` — konfigurasi runtime env & ID hasil seed uji
- `/api/totp-helper` — pembangkit TOTP 6 digit lokal untuk akun uji sintetis
- `/api/probe-cors` — probe CORS sisi server tester untuk perbandingan header
- `/openapi.json` — spesifikasi OpenAPI dalam format JSON
- `/roles_matrix.json` — matriks kontrol akses berbasis peran & lintas tenant
- `/scenarios.json` — daftar skenario uji berbasis `claim:ID`

---

## Fitur & Panel di Halaman Tester (`index.html`)

- **0. Status Lingkungan, Konfigurasi & Guard Keamanan**: Menampilkan hasil `/config.json`, konfirmasi pengaman jika target non-lokal (`SAYA PAHAM INI BUKAN PRODUKSI`), tombol refresh/logout/hapus sesi memori, dan catatan variabel ID terkumpul.
- **1. Pengalih Akun Cepat (Seeded Role Accounts)**: Tabel akun uji sintetis (Koperasi A & B) untuk login cepat atau berganti peran.
- **2. Konsol OpenAPI Generik (`54` operasi / `48` path)**: Memuat `/openapi.json`, mengisi contoh parameter/body, memilih mode `Idempotency-Key`, menyalin request sebagai `cURL`, dan mengirim request langsung dengan token aktif.
- **3. Panel Alur Terpandu (`3.1` s.d. `3.9`)**:
  - `3.1` Autentikasi, MFA Wajib Enrollment, Token Reuse & Lockout (termasuk `GET /metrics`)
  - `3.2` Pendaftaran Anggota + Upload KYC PNG + Maker-Checker Verifikasi
  - `3.3` Pembayaran QRIS Statis, Kode Unik, Idempotency-Key (`201`/`Replay`/`422`) & Maker-Checker
  - `3.4` Impor Mutasi Bank CSV, Segregasi Peran & Rekonsiliasi Harian
  - `3.5` Manajemen Rekening QRIS Merchant (Tolak Perorangan Tanpa Pilot vs Badan Hukum)
  - `3.6` Toko Koperasi, Keranjang, Promo, Checkout & Verifikasi Pembayaran
  - `3.7` CRUD Jenis Iuran & Generate Tagihan Bulanan (Penanda `KEPUTUSAN AKUNTAN`)
  - `3.8` Laporan Keuangan, Rekonsiliasi, Manajemen Anggota, Users & Anti-Escalation Role
  - `3.9` Isolasi Lintas-Tenant (Kop A vs Kop B) & Anti-IDOR Anggota
- **4. Eksekutor Pembuktian Dokumentasi (`scenarios.json`)**: Menjalankan `26` skenario uji (`102` langkah) yang dipetakan 1-ke-1 ke penanda `<!-- claim:ID -->` di `docs/FRONTEND_GUIDE.md` dan `docs/BACKEND_GUIDE.md`, lalu mengekspor laporan bukti dalam format JSON atau Markdown dari memori sesi browser. Skenario `CLAIM-AUTH-BRUTEFORCE-429` dijalankan di akhir agar pembatasan login per IP tidak mengganggu skenario lain.
- **5. Pengujian Matriks Peran (`roles_matrix.json`)**: Menguji `54` operasi terhadap `7` peran + kondisi `unauthenticated` + `7` probe `cross_tenant` (`439` pemeriksaan) serta membandingkannya terhadap ekspektasi matriks.
- **6. Panel Perilaku Browser Nyata (`6.1` s.d. `6.4`)**: Menguji CORS origin sah vs origin asing, `AbortController`/retry idempotency, penolakan body `> 10MB` (`413`) & malformed JSON (`400`), serta pemindaian PII/rahasia di seluruh respons yang tersimpan di sesi.

---

## Rujukan Terkait

- [Panduan Developer Frontend](../docs/FRONTEND_GUIDE.md)
- [Panduan Developer Backend](../docs/BACKEND_GUIDE.md)
- [Daftar Diskrepansi Dokumentasi vs Implementasi](../docs/DOC_DISCREPANCIES.md)
- [Matriks Peran (`roles_matrix.json`)](../docs/roles_matrix.json)
