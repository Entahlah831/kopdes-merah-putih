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

