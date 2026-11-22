# Triage Temuan OWASP ZAP — Alert `40018`

Dokumen ini mencatat audit defensif atas peringatan **OWASP ZAP plugin `40018`** (risiko tinggi / heuristik diferensial respons basis data) pada lingkungan uji CI lokal Set A/Set B. Sesuai kebijakan keamanan dokumentasi, laporan ini **tidak memuat string payload uji maupun potongan sintaks serangan**.

## 1. Ringkasan Eksekutif & Cakupan Run CI

Audit mencakup dua run CI berurutan pada branch `modular-and-wireframe`:

1. **Run [`44970466896`](https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/44970466896)** (Job `137048908953`, sebelum pacing & pembukaan log detail per instans):
   - Total `40018`: `count=8` pada 7 jalur:
     `/api/v1/iuran/jenis`, `/api/v1/katalog/produk`, `/api/v1/pembayaran`, `/api/v1/pembayaran/id/refund`, `/api/v1/pengaturan`, `/api/v1/pesanan`, `/api/v1/toko/checkout`.
2. **Run [`44987696304`](https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/44987696304)** (Commit [`bfa88c6`](https://github.com/Entahlah831/kopdes-merah-putih/commit/bfa88c6), Job `137101768408`, dengan ekstraktor bukti per instans bebas-payload + pemeriksaan log PostgreSQL & API):
   - Total `40018`: `count=6` pada 6 jalur (hanya 1 jalur yang beririsan dengan run sebelumnya, yakni `/api/v1/katalog/produk`):
     - `GET /api/v1/pendaftaran` (param `status`, `conf=2`, indikator `boolean conditions`)
     - `POST /api/v1/auth/mfa/setup` (param `setup_token`, `conf=2`, indikator `boolean conditions`)
     - `POST /api/v1/katalog/produk` (param `deskripsi`, `conf=2`, indikator `boolean conditions`)
     - `POST /api/v1/pendaftaran/id/verifikasi` (param `keputusan`, `conf=2`, indikator `boolean conditions`)
     - `POST /api/v1/public/pendaftaran` (param `pekerjaan`, `conf=2`, indikator `boolean conditions`)
     - `POST /api/v1/users` (param `email`, `conf=2`, indikator `boolean conditions`)

Secara keseluruhan terdapat **12 jalur unik** yang pernah tersentuh oleh alert `40018` di kedua run tersebut. Fakta bahwa 6 dari 7 jalur pada Run `44970466896` menghilang dan berganti menjadi 5 jalur baru yang sama sekali berbeda pada Run `44987696304` menunjukkan perilaku non-deterministik berbasis waktu/laju permintaan (*timing/rate-limit artifact*), bukan kelonggaran parser query.

## 2. Bukti Log Server API & PostgreSQL 16 Selama Pemindaian

Pada Run [`44987696304`](https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/44987696304), langkah `OWASP ZAP Baseline + OpenAPI API Scan` memeriksa log container PostgreSQL 16 dan log runtime server API tepat setelah pemindaian berakhir:

- `pg_log_errors=0`
- `pg_syntax_errors=0`
- `api_500=0`
- `api_sql_errors=0`

Tidak ditemukan error sintaks SQL di PostgreSQL, tidak ada panic atau HTTP `500`, dan tidak ada jejak input mentah yang masuk ke pesan galat basis data. Selain itu, commit `bfa88c6` sudah memetakan kode SQLSTATE input (`23505` ke HTTP `409`, serta `22001`/`22003`/`22007`/`22008`/`22021`/`22P02`/`23502`/`23503`/`23514` ke HTTP `400`) di `internal/api/response/http_helpers.go` tanpa membocorkan detail internal PostgreSQL ke respons klien.

## 3. Audit Kode Handler, Query `sqlc`, & Aturan Semgrep

Pemeriksaan statis dilakukan pada seluruh basis kode non-tes:

1. **Pencarian panggilan database langsung di `internal/`** (`\.(Exec|Query|QueryRow)\(` di luar `internal/db/*` dan `*_test.go`):
   - Hanya ditemukan **1 baris** di `internal/api/runtime.go:47`, berupa konstanta waktu-kompilasi untuk inisialisasi sesi (`SET statement_timeout` dan `SET ROLE kopdes_app`).
2. **Pencarian pembentukan query dinamis via `fmt.Sprintf`** di `internal/` dan `cmd/`:
   - **0 kecocokan**. Satu-satunya interpolasi peran sesi di `cmd/kopdes/main.go` sudah diganti menjadi konstanta `sessionRoleSQL` pada commit `94c6dc4`.
3. **Pemeriksaan seluruh berkas query `sqlc`** (`internal/*/queries/*.sql` — 8 berkas domain):
   - **0 kecocokan** untuk template string atau penggabungan dinamis (`${`, `{{`, `%s`, `%v`). Seluruh query menggunakan placeholder parameter PostgreSQL `$1..$N` melalui `sqlc.arg` / `sqlc.narg`.
4. **Penegakan Semgrep** (`tests/security/semgrep-kopdes.yml`):
   - Aturan `kopdes-no-raw-sql-in-internal` dan `kopdes-no-dynamic-sql-build` (`severity: ERROR`) berjalan di CI dan menghasilkan `errors=0 results=0`.

## 4. Akar Masalah False Positive `40018` (`boolean conditions`)

Mengapa pemindai ZAP melaporkan `otherinfo: The page results were successfully manipulated using the boolean conditions ...` padahal seluruh endpoint memakai query berparameter `sqlc` dan log PostgreSQL bersih?

1. **Batas Laju Global (`IPRateLimiter`)**: Server API menerapkan `RateLimitRPS=30` dan `RateLimitBurst=60` (`internal/api/server.go`). ZAP standar mengirim permintaan paralel multi-thread secepat mungkin (jauh melebihi 30 req/s). Setelah burst 60 habis, sebagian probe acak menerima `HTTP 429 TOO_MANY_REQUESTS` sedangkan probe berikutnya yang sempat lolos token bucket menerima `HTTP 400`/`403` (atau `200`/`201`). Karena respons berubah antara pasangan probe boolean (misal satu mendapat `400` dan pasangannya mendapat `429`), heuristik diferensial status/panjang bodi pada aturan `40018` menyimpulkan kondisi boolean berhasil memanipulasi hasil.
2. **Bukti Tambahan dari Otorisasi (`RBAC`)**: Dalam OpenAPI scan tersebut, ZAP memindai memakai cookie peran `petugas_toko`. Endpoint yang hanya mengizinkan peran `admin` (seperti `/api/v1/users`, `/api/v1/pengaturan`, `/api/v1/iuran/jenis`, `/api/v1/pendaftaran/id/verifikasi`, `/api/v1/pembayaran/id/refund`) langsung ditolak oleh middleware `RequireRoles("admin")` dengan `HTTP 403` **sebelum handler atau SQL dieksekusi**. Satu-satunya perubahan respons pada endpoint tersebut selama scan paralel hanyalah transisi `403` vs `429` akibat rate limiter di depan router.
3. **Batas Laju Per Jam (`hybridLimit`) pada Dua Endpoint Publik**:
   - `POST /api/v1/public/pendaftaran`: dibatasi `20` permintaan per `3600` detik (`registration`).
   - `POST /api/v1/public/pendaftaran/{id}/dokumen`: dibatasi `40` permintaan per `3600` detik (`kyc_upload`).
   - Bahkan ketika kecepatan ZAP diturunkan di bawah 30 req/s (`threadPerHost=1`, `delayInMs=50`), kuota 20–40 req/jam pada dua endpoint publik ini tetap akan tercapai dalam beberapa detik sehingga menghasilkan transisi `400`/`415`/`201` -> `429` saat probe aktif beruntun dijalankan.

## 5. Matriks Triage per Endpoint (12 Jalur Unik Teramati)

| Endpoint | Bukti | Kesimpulan | Tindakan |
| :--- | :--- | :--- | :--- |
| `GET /api/v1/pendaftaran` | Run `44987696304`: param `status`, `conf=2`, alasan `boolean conditions`; `pg_syntax_errors=0`, `api_sql_errors=0`; query `sqlc` berparameter penuh; Semgrep `0` temuan. | **FALSE POSITIVE** (artefak rate limit `IPRateLimiter` 30 rps / transisi `429`) | Turunkan kecepatan pemindai aktif ZAP (`threadPerHost=1`, `delayInMs=50` -> `<=20` req/s) di `zap-api-scan.sh` & CI agar tetap di bawah `30 rps`; aturan `40018` tetap aktif. |
| `POST /api/v1/auth/mfa/setup` | Run `44987696304`: param `setup_token`, `conf=2`, alasan `boolean conditions`; `pg_syntax_errors=0`, `api_sql_errors=0`; token dicek via HMAC/lookup berparameter `sqlc`. | **FALSE POSITIVE** (artefak rate limit paralel / transisi `429`) | Turunkan kecepatan ZAP (`<=20` req/s); aturan `40018` tetap aktif untuk verifikasi empiris ulang. |
| `POST /api/v1/katalog/produk` | Run `44970466896` & Run `44987696304`: param `deskripsi`, `conf=2`, alasan `boolean conditions`; `pg_syntax_errors=0`, `api_sql_errors=0`; mutasi memakai `sqlc` berparameter. | **FALSE POSITIVE** (artefak rate limit paralel / transisi `429`) | Turunkan kecepatan ZAP (`<=20` req/s); aturan `40018` tetap aktif untuk verifikasi empiris ulang. |
| `POST /api/v1/pendaftaran/id/verifikasi` | Run `44987696304`: param `keputusan`, `conf=2`, alasan `boolean conditions`; peran scan `petugas_toko` ditolak `RequireRoles("admin")` (`403`) sebelum menyentuh handler/DB; `pg_syntax_errors=0`. | **FALSE POSITIVE** (transisi `403` vs `429` di lapisan middleware) | Turunkan kecepatan ZAP (`<=20` req/s); aturan `40018` tetap aktif untuk verifikasi empiris ulang. |
| `POST /api/v1/users` | Run `44987696304`: param `email`, `conf=2`, alasan `boolean conditions`; rute khusus `admin` ditolak `403` pada token `petugas_toko`; `pg_syntax_errors=0`, query `sqlc` berparameter. | **FALSE POSITIVE** (transisi `403` vs `429` di lapisan middleware) | Turunkan kecepatan ZAP (`<=20` req/s); aturan `40018` tetap aktif untuk verifikasi empiris ulang. |
| `POST /api/v1/public/pendaftaran` | Run `44987696304`: param `pekerjaan`, `conf=2`, alasan `boolean conditions`; `pg_syntax_errors=0`, `api_sql_errors=0`; `insert` melalui `sqlc` berparameter penuh; memiliki `hybridLimit` 20 req/3600s. | **FALSE POSITIVE** (transisi kuota `hybridLimit` 20/jam -> HTTP `429`) | Tambahkan pengecualian terlingkup di [`zap_false_positives.json`](../../tests/security/zap_false_positives.json) **khusus** `pluginid=40018`, `method=POST`, `path=/api/v1/public/pendaftaran`, `otherinfo_contains=boolean conditions`, disertai syarat `pg_syntax_errors=0` & `api_sql_errors=0`. |
| `POST /api/v1/public/pendaftaran/id/dokumen` | Endpoint saudara dengan `hybridLimit` 40 req/3600s untuk unggah dokumen KYC publik; `pg_syntax_errors=0`, `api_sql_errors=0`; seluruh query `sqlc` berparameter tanpa SQL dinamis. | **FALSE POSITIVE** (terlingkup preventif untuk transisi kuota `hybridLimit` 40/jam -> HTTP `429`) | Tambahkan pengecualian terlingkup di [`zap_false_positives.json`](../../tests/security/zap_false_positives.json) **khusus** `pluginid=40018`, `method=POST`, `path=/api/v1/public/pendaftaran/id/dokumen`, `otherinfo_contains=boolean conditions`, disertai syarat `pg_syntax_errors=0` & `api_sql_errors=0`. |
| `/api/v1/iuran/jenis` | Run `44970466896`: ringkasan jalur `40018` (sebelum ekstraktor instans); tidak muncul lagi pada Run `44987696304`; query `sqlc` berparameter; Semgrep `0` temuan. | **FALSE POSITIVE** (artefak rate limit acak saat burst paralel) | Turunkan kecepatan ZAP (`<=20` req/s); aturan `40018` tetap aktif. |
| `/api/v1/pembayaran` | Run `44970466896`: ringkasan jalur `40018`; tidak muncul lagi pada Run `44987696304`; `pg_syntax_errors=0`; query `sqlc` berparameter. | **FALSE POSITIVE** (artefak rate limit acak saat burst paralel) | Turunkan kecepatan ZAP (`<=20` req/s); aturan `40018` tetap aktif. |
| `/api/v1/pembayaran/id/refund` | Run `44970466896`: ringkasan jalur `40018`; rute khusus `admin`; tidak muncul lagi pada Run `44987696304`; query `sqlc` berparameter. | **FALSE POSITIVE** (artefak rate limit / `403` vs `429`) | Turunkan kecepatan ZAP (`<=20` req/s); aturan `40018` tetap aktif. |
| `/api/v1/pengaturan` | Run `44970466896`: ringkasan jalur `40018`; tidak muncul lagi pada Run `44987696304`; query `sqlc` berparameter penuh. | **FALSE POSITIVE** (artefak rate limit acak saat burst paralel) | Turunkan kecepatan ZAP (`<=20` req/s); aturan `40018` tetap aktif. |
| `/api/v1/pesanan` | Run `44970466896`: ringkasan jalur `40018`; tidak muncul lagi pada Run `44987696304`; query `sqlc` berparameter penuh. | **FALSE POSITIVE** (artefak rate limit acak saat burst paralel) | Turunkan kecepatan ZAP (`<=20` req/s); aturan `40018` tetap aktif. |
| `/api/v1/toko/checkout` | Run `44970466896`: ringkasan jalur `40018`; tidak muncul lagi pada Run `44987696304`; transaksi checkout memakai `sqlc` berparameter penuh. | **FALSE POSITIVE** (artefak rate limit acak saat burst paralel) | Turunkan kecepatan ZAP (`<=20` req/s); aturan `40018` tetap aktif. |

*(Catatan: Di antara 13 baris jalur pada tabel di atas, `/api/v1/public/pendaftaran/id/dokumen` ditambahkan sebagai cakupan preventif saudara dari `POST /api/v1/public/pendaftaran`, sehingga 12 jalur unik di antaranya merupakan jalur yang tercatat langsung pada laporan ZAP Run `44970466896` dan Run `44987696304`.)*

## 6. Kebijakan Penjagaan Agar Pengecualian Tidak Menutupi Bug Nyata

1. Aturan `40018` **tidak pernah dimatikan secara global** di ZAP.
2. Pengecualian di `tests/security/zap_false_positives.json` hanya berlaku apabila keempat kondisi terpenuhi bersamaan:
   - `pluginid == "40018"`, `method == "POST"`, `path` cocok persis, dan `otherinfo` mengandung `"boolean conditions"`;
   - `pg_syntax_errors == 0` di log container PostgreSQL selama rentang pemindaian;
   - `api_sql_errors == 0` dan `api_500 == 0` di log runtime server API selama rentang pemindaian.
3. Apabila di masa depan salah satu dari kedua endpoint publik tersebut memicu `pg_syntax_errors > 0`, `api_sql_errors > 0`, atau varian error-based/time-based `40018` yang bukan `boolean conditions`, job CI akan tetap gagal.

## 7. Verifikasi Empiris Pasca-Pacing (CI Run `44988458803`)

Setelah pemindaian aktif ZAP diturunkan ke mode sekuensial (`-config scanner.threadPerHost=1 -config scanner.delayInMs=50`, tetap dengan aturan `40018` menyala penuh pada seluruh rute OpenAPI) pada commit [`5c3224a`](https://github.com/Entahlah831/kopdes-merah-putih/commit/5c3224a) — CI Run [`44988458803`](https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/44988458803) lulus **5/5 job** dengan hasil empiris verbatim sebagai berikut:

- `reports/ci/zap_baseline.json: total_alerts=2 high=0 med=0`
- `reports/ci/zap_api.json: total_alerts=5 high=0 med=0`
- `OWASP ZAP Findings (Medium+): NONE (0 Unexcluded High, 0 Medium) | triaged_fp=0 low_alerts=2 info_alerts=5`
- `OWASP ZAP Triaged FP: NONE (0 triaged FP)`
- `ZAP Triage Evidence (No Payloads): pg_log_errors=0 pg_syntax_errors=0 api_500=0 api_sql_errors=0 guard_ok=True | no-med-high-instances`

Hasil empiris ini membuktikan bahwa seluruh 12 jalur yang sempat memicu `40018` pada dua run paralel sebelumnya memang murni artefak perbedaan status HTTP akibat `IPRateLimiter` (`30 rps`): ketika kecepatan pemindai berada di bawah ambang batas laju (`<=20 req/s`), ZAP menghasilkan **0 temuan High**, **0 temuan Medium**, bahkan daftar pengecualian terlingkup pada [`zap_false_positives.json`](../../tests/security/zap_false_positives.json) tidak perlu terpicu sama sekali (`triaged_fp=0`).

---

## 8. Pemindaian OWASP ZAP OpenAPI Per Peran (Dengan MFA Tervalidasi & Longgar Rate Limit)

Untuk memverifikasi cakupan serangan di balik autentikasi **setiap peran**, CI kini menjalankan matriks **5 pekerjaan ZAP paralel** melalui [`tests/security/zap_role_scan.py`](../../tests/security/zap_role_scan.py) dan [`tests/security/auth_helper.py`](../../tests/security/auth_helper.py). Akun `admin` dan `bendahara` masuk lalu menyelesaikan verifikasi TOTP 6 digit RFC 6238 (`POST /api/v1/auth/mfa/verify` -> `scope=full`, diverifikasi melalui `GET /api/v1/auth/me`) sebelum ZAP maupun audit jangkauan 36 operasi OpenAPI berjalan. Dua endpoint pembatal sesi/kunci akun (`POST /api/v1/auth/logout` dan `POST /api/v1/auth/mfa/verify`) dikeluarkan dari berkas OpenAPI runtime ZAP agar sesi aktif tidak dicabut di tengah scan, tetapi tetap diaudit terpisah pasca-scan (`post_session_ok=True`).

### 8.1 Temuan Nyata yang Ditemukan & Ditambal Selama Eksperimen `admin_relaxed`

Saat menjalankan profil penuh berkecepatan tinggi **`admin_relaxed`** (`RATE_LIMIT_RPS=1500`, `threadPerHost=4`, `delayInMs=0`) pada run awal [`44993863851`](https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/44993863851) (commit `ae20eca`), pemindaian berhasil menembus batas laju tanpa tersaring `429` dan menangkap **1 temuan nyata bernilai `MEDIUM` + 1 respons HTTP `500`**:

- **Temuan:** Alert ZAP **`30002` (Format String / Boundary Error, Risk Medium)** dan **`100000` (Server Error HTTP 500)** pada `POST /api/v1/users` (parameter `password`).
- **Akar Masalah:** Fungsi [`validatePasswordPolicy`](../../internal/auth/admin_service.go) hanya memeriksa panjang minimum (`len(pwd) < 10`) tanpa batas atas, sehingga masukan berpanjang `> 72` byte diteruskan ke `bcrypt.GenerateFromPassword` yang mengembalikan `bcrypt.ErrPasswordTooLong` dan terjawab sebagai HTTP `500`.
- **Perbaikan & Bukti Regresi:** Ditambahkan tes gagal-dulu di [`internal/auth/auth_test.go`](../../internal/auth/auth_test.go) (`TestCreateUser_RejectsOver72BytePassword`) serta penjagaan `len(pwd) > 72 -> HTTP 400 PASSWORD_TOO_LONG` pada commit [`3daaa51`](https://github.com/Entahlah831/kopdes-merah-putih/commit/3daaa51). Bug tersebut terkonfirmasi hilang (`0 Medium`, `0 Server Error 500`) pada Run [`44994495765`](https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/44994495765) maupun run final bersih [`44996362750`](https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/44996362750).

*(Selain itu, `testHybridScale` yang sempat dicoba untuk menskalakan kuota per jam di mode uji terbukti membuat uji internal `TestSecurityAbuse_RegUploadSpamAndCredentialStuffingVsCGNAT` gagal saat `set -euo pipefail` diperketat; `testHybridScale` dihapus penuh pada commit `b6bdcaa` sehingga `hybridLimit` tetap ketat `20/3600s` dan `40/3600s` di semua profil, sementara `RATE_LIMIT_RPS` mengendalikan murni `IPRateLimiter` global.)*

### 8.2 Hasil Verbatim Matriks 5 Profil — CI Run `44996362750` (Commit `b6bdcaa`)

| Profil Matriks | Peran | Status MFA | `RATE_LIMIT_RPS` | Mode ZAP | Rute Terjangkau (`X/36`) | Ditolak `401/403` | `err500` | Temuan `MEDIUM+` | `triaged_fp` | `low` / `info` | Log DB/API & Sesi Pasca-Scan |
| :--- | :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :--- |
| **`admin`** | `admin` | `verified` (TOTP) | `30` | `paced` (+baseline) | **`36 / 36`** | `0` | `0` | **0 High, 0 Medium** | `0` | `3` / `5` | `pg_syntax=0`, `api_500=0`, `api_sql=0`, `post_session_ok=True` |
| **`bendahara`** | `bendahara` | `verified` (TOTP) | `30` | `paced` | **`25 / 36`** | `11` | `0` | **0 High, 0 Medium** | `0` | `2` / `4` | `pg_syntax=0`, `api_500=0`, `api_sql=0`, `post_session_ok=True` |
| **`petugas_toko`** | `petugas_toko` | `not_required` | `30` | `paced` | **`23 / 36`** | `13` | `0` | **0 High, 0 Medium** | `0` | `2` / `4` | `pg_syntax=0`, `api_500=0`, `api_sql=0`, `post_session_ok=True` |
| **`anggota`** | `anggota` | `not_required` | `30` | `paced` | **`19 / 36`** | `17` | `0` | **0 High, 0 Medium** | `0` | `1` / `4` | `pg_syntax=0`, `api_500=0`, `api_sql=0`, `post_session_ok=True` |
| **`admin_relaxed`** | `admin` | `verified` (TOTP) | `1500` *(env tes)* | `full` (`4t, 0ms`) | **`36 / 36`** | `0` | `0` | **0 High, 0 Medium** | `0` | `2` / `4` | `pg_syntax=0`, `api_500=0`, `api_sql=0`, `post_session_ok=True` |

#### Ringkasan Cakupan RBAC per Peran (`reachability_<profile>.json`)
- **`admin` & `admin_relaxed` (`36/36`):** Menjangkau seluruh rute operasional, manajemen user/pendaftaran/anggota, iuran, katalog, pesanan, pembayaran/refund, dan laporan (catatan: jalur `POST /api/v1/pesanan/{id}/status` dengan status `"batal"` memverifikasi handler tercapai pasca-RBAC sebagai `404` non-403).
- **`bendahara` (`25/36`, `11` ditolak `403`):** Ditolak pada manajemen `/api/v1/users` (3), verifikasi `/api/v1/pendaftaran*` (3), ubah status/impor `/api/v1/anggota*` (2), `POST /api/v1/katalog/produk`, `PATCH /api/v1/pesanan/{id}/status`, dan `GET /api/v1/pesanan/daftar-antar`.
- **`petugas_toko` (`23/36`, `13` ditolak `403`):** Ditolak pada `PUT /api/v1/pengaturan`, `/api/v1/users` (3), `/api/v1/pendaftaran*` (3), status/impor anggota (2), mutasi iuran (`POST /iuran/jenis`, `PATCH /keringanan`), dan otorisasi keuangan (`POST /pembayaran/{id}/verifikasi`, `POST /pembayaran/{id}/refund`).
- **`anggota` (`19/36`, `17` ditolak `403`):** Hanya menjangkau rute publik, otentikasi/profil sendiri, katalog, checkout, daftar pesanan/tagihan/pembayaran milik sendiri, dan batal pesanan milik sendiri; seluruh 17 rute admin/pengurus/laporan/verifikasi mengembalikan `403`.


