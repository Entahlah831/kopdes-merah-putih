# Laporan Baseline Fase 0 (Sebelum Restrukturisasi Modular Monolith)

> **Catatan Kejujuran Lingkungan Pengujian**: Pengujian Fase 0 dijalankan pada host **Windows** (CPU `AMD Ryzen 5 5500U with Radeon Graphics`, RAM `15.34 GB`, PostgreSQL `14.5` lokal pada `127.0.0.1:${DB_PORT}` profil uji Set A & Set B), dengan proses server API dibatasi `GOMAXPROCS=2` untuk mendekati target 2 vCPU VPS. Perkakas Linux/Docker (`docker`, `k6`, `OWASP ZAP`, `schemathesis`, `semgrep`, `trivy`, `hadolint`, `toxiproxy-server`, `age`) **tidak terpasang di host Windows ini**; skrip reproduksinya disediakan di [tests/perf/k6_mixed.js](../tests/perf/k6_mixed.js), [tests/security/zap-api-scan.sh](../tests/security/zap-api-scan.sh), [tests/security/semgrep-kopdes.yml](../tests/security/semgrep-kopdes.yml), dan [tests/security/run_container_scans.sh](../tests/security/run_container_scans.sh), sedangkan beban HTTP, injeksi latensi/putus TCP DB, uji logika bisnis, *abrupt kill*, serta backup/restore terenkripsi dijalankan langsung secara native melalui Go dan biner PostgreSQL 14.

---

## 1. Baseline Performa (`GOMAXPROCS=2`, PostgreSQL Pool `MaxConns=25`)

Target SLO bawaan: **Baca p95 < 300 ms**, **Tulis p95 < 800 ms**, **Rasio error tak terduga < 0,5% (`0.005`)**. Sumber data mentah: [reports/baseline/perf_baseline.json](../reports/baseline/perf_baseline.json) dan [reports/baseline/perf_sys_metrics.json](../reports/baseline/perf_sys_metrics.json).

| Skenario | Puncak VU | Durasi | Total Req | RPS | Baca p50 / p95 / p99 (ms) | Tulis p50 / p95 / p99 (ms) | Error Rate | 429 | PG Max Conns / Lock Wait | Status SLO |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| `smoke` | 5 | 10.01s | 7.206 | 719,66 | 2,00 / 4,01 / 10,01 | 9,00 / 18,17 / 30,27 | 0,0000 | 0 | 6 / 3 | **PASS** |
| `load` | 100 | 25.21s | 16.780 | 665,67 | 77,46 / 176,28 / 235,85 | 159,52 / 349,71 / 560,96 | 0,0000 | 85 | 26 / 24 | **PASS** |
| `stress` | 500 | 34.27s | 24.233 | 707,09 | 130,00 / 719,66 / 1049,62 | 251,01 / 1085,67 / 1209,47 | 0,0000 | 531 | 27 / 24 | **FAIL** (p95 baca & tulis > SLO) |
| `spike` | 250 | 20.57s | 13.577 | 660,20 | 79,99 / 404,61 / 564,34 | 106,62 / 655,43 / 787,51 | 0,0000 | 193 | 26 / 24 | Read p95 > 300ms; Write & Err **PASS** |
| `soak` *(90s time-box)* | 50 | 90.52s | 55.599 | 614,20 | 34,65 / 65,49 / 95,56 | 75,01 / 269,35 / 459,50 | 0,0000 | 62 | 27 / 24 | **PASS** |

- **Sumber Daya API Selama Seluruh Rangkaian Beban**: Puncak memori RSS `125,29 MB`, total waktu CPU `171,83 s`, ukuran log API total `232 bytes` (`perf_sys_metrics.json`).
- **Kapasitas Terukur & Titik Patah**:
  - Beban normal target (`100` VU aktif bersamaan pada `665,67 RPS`) terpenuhi dengan margin aman (`176,28 ms < 300 ms` baca, `349,71 ms < 800 ms` tulis, `0` error 5xx).
  - **Titik patah latensi SLO** berada di kisaran **`~250–300` VU konkuren** dengan konfigurasi saat ini (`GOMAXPROCS=2` dan `MaxConns=25`), di mana antrean pool koneksi (`26–27` koneksi total DB) menyebabkan lonjakan p95 ke `404,61 ms` pada spike 250 VU dan `719,66 ms` / `1085,67 ms` pada stress hingga 500 VU — tanpa *crash* maupun error 5xx (`error_rate = 0`).
  - *Pengungkapan*: Uji `soak` dijalankan *time-boxed* `90s` secara lokal; standar penuh 2 jam dapat dijalankan lewat `-soak-duration=2h` pada `tools/loadtest` atau `tests/perf/k6_mixed.js`.

---

## 2. Baseline Keamanan & Temuan yang Diperbaiki

### 2.1 Hasil Pemindai Statis & Rahasia (Native)
| Perkakas | Hasil Baseline | Berkas Artefak |
| :--- | :--- | :--- |
| `staticcheck ./...` | Exit 0 (0 temuan) | [reports/baseline/staticcheck.txt](../reports/baseline/staticcheck.txt) |
| `gosec ./...` | 0 isu keamanan di seluruh 71 berkas / 18.288 baris (0 pengecualian global) | [reports/baseline/gosec.txt](../reports/baseline/gosec.txt) |
| `govulncheck ./...` | `0 vulnerabilities` pada kode dan paket terimpor (1 CVE modul tidak terpanggil) | [reports/baseline/govulncheck.txt](../reports/baseline/govulncheck.txt) |
| `gitleaks` (111 commit) | `no leaks found` (~3,67 MB dipindai dalam 7,27 detik) | [reports/baseline/gitleaks.txt](../reports/baseline/gitleaks.txt) |

### 2.2 Suite Penyalahgunaan Logika Bisnis (`internal/api/security_abuse_test.go`) — Temuan Nyata Fail-First & Perbaikan
Sebelum perbaikan, pengujian otomatis *fail-first* (`reports/baseline/fail_first_security_chaos.txt`) menangkap **3 cacat nyata**:

1. **HIGH — Mass Assignment Diterima Diam-Diam di 16 Endpoint JSON Mutatif**:
   - *Temuan*: `response.DecodeJSON` (`internal/api/response/http_helpers.go`) belum menyetel `dec.DisallowUnknownFields()`. Akibatnya injeksi field palsu (`"peran":"admin"`, `"status":"disetujui"`, `"total_bayar":1`, `"mfa_verified":true`) diterima HTTP 200/201/404 alih-alih ditolak di lapisan decoding (bahkan `POST /api/v1/auth/password` sempat memproses pembaruan kata sandi).
   - *Perbaikan*: Menambahkan `dec.DisallowUnknownFields()` dan penolakan *trailing JSON tokens* di `DecodeJSON`, serta menjaga `HandleMFASetup` tetap menerima body kosong (`ContentLength == 0`) namun tetap ketat untuk payload berfield asing.
2. **MEDIUM — Tabrakan `Idempotency-Key` Lintas Pengguna Memicu HTTP 500**:
   - *Temuan*: Kolom `pesanan.idempotency_key` dan `pembayaran.idempotency_key` bersifat `UNIQUE` global; ketika User B mengirim kunci yang sama persis dengan User A, layanan melewati *replay* milik User A lalu gagal di constraint `UNIQUE` PostgreSQL (`500`).
   - *Perbaikan*: Melakukan *scoping* kunci idempotensi per `UserID` (`<user_id>:<idempotency_key>`) di [internal/pesanan/service.go](../internal/pesanan/service.go) dan [internal/pembayaran/service.go](../internal/pembayaran/service.go), sehingga replay oleh User A tetap mengembalikan `200 OK`, sementara User B terisolasi membuat transaksi miliknya sendiri (`201 Created`) tanpa kebocoran data ataupun `500`.
3. **LOW — Guard Pointer `store == nil` Saat Direktori KYC Gagal Diinisialisasi**:
   - *Temuan*: `HandleUploadDokumen` membaca `h.svc.store.maxBytes` langsung tanpa memeriksa `store != nil`.
   - *Perbaikan*: Menambahkan pengaman di [internal/pendaftaran/handler.go](../internal/pendaftaran/handler.go) dan `persistKYCDocument` ([internal/pendaftaran/workflow.go](../internal/pendaftaran/workflow.go)) agar mengembalikan `503 STORAGE_UNAVAILABLE` secara aman tanpa *panic*.

Seluruh skenario penyalahgunaan berikut kini terverifikasi **PASS** dengan `-race` ([reports/baseline/pass_security_chaos.txt](../reports/baseline/pass_security_chaos.txt)), diikuti seluruh **22 paket Go** pada `go test -count=1 -race -p 1 ./...` ([reports/baseline/go_test_race.txt](../reports/baseline/go_test_race.txt)):
- Pengurasan slot kode unik per anggota (`429 TOO_MANY_PENDING_QRIS`) & batas global (`503 PAYMENT_QUEUE_FULL`).
- Race 8 pembeli bersamaan memperebutkan 1 stok (`1` menang `201`, `7` gagal `409 OUT_OF_STOCK`) + promo kuota habis/sekali per anggota.
- Spam pendaftaran 500 IP palsu acak (0 baris berlebih di `rate_limits` berkat Hybrid L1 LRU) + 1 IP berulang (`429`) + *credential stuffing* CGNAT (akun korban terkunci `429`, warga lain di IP sama tetap login `200`).
- Penolakan mass assignment `400 INVALID_JSON` pada seluruh 16 endpoint tulis JSON.
- Matriks BOLA/BFLA lintas anggota, sesi kedaluwarsa/dicabut/`mfa_setup`, token Set A ditolak `401` di Set B, CSRF cookie tanpa `X-Requested-With` (`403`).
- Unggahan jahat (path traversal `../../escape_ktp.png` tidak keluar direktori, polyglot script `400`, PNG dimensi `9001x9001` `400`, melebihi batas ukuran `400`/`413`), penetralan CSV formula injection (`'=CMD|...`), proteksi XSS JSON (`Content-Type: application/json`, `X-Content-Type-Options: nosniff`, escaping `<script>`), serta penolakan replay bukti transfer identik (`409 DUPLICATE_BUKTI`).

### 2.3 Celah Scanner Eksternal
`OWASP ZAP`, `schemathesis`, `semgrep`, `trivy`, dan `hadolint` belum dijalankan langsung di host Windows ini karena tidak tersedia secara lokal (skrip/ruleset tersedia di `tests/security/` untuk lingkungan Linux/Docker/CI).

---

## 3. Baseline Keandalan & Chaos (`internal/api/reliability_chaos_test.go` + `tools/chaosdrill`)

Sumber data mentah: [reports/baseline/chaos_baseline.json](../reports/baseline/chaos_baseline.json) & [reports/baseline/pass_security_chaos.txt](../reports/baseline/pass_security_chaos.txt).

| Skenario Chaos | Perilaku yang Teramati | Hasil |
| :--- | :--- | :---: |
| **Putus Koneksi DB + Pemulihan** (`chaosTCPProxy`) | Saat DB terputus `/readyz` mengembalikan `503 DATABASE_UNAVAILABLE`, read endpoint gagal terkontrol 5xx tanpa panik; saat koneksi kembali dengan injeksi latensi TCP `60ms`, `/readyz` pulih `200` otomatis. | **PASS** |
| **Saturasi Connection Pool** (`MaxConns=2`) | Permintaan masuk di tengah 2 query `pg_sleep` gagal/timeout bersih di `400ms` tanpa hang, lalu `/readyz` kembali `200` setelah koneksi dilepas. | **PASS** |
| **Storage Tidak Dapat Ditulis / Penuh** | Gagal menulis bukti terenkripsi mengembalikan `5xx` terstruktur; transaksi DB di-rollback penuh (`status` tetap `menunggu_pembayaran`, `bukti_url = ""`), 0 berkas yatim. | **PASS** |
| **Restart API Saat Sesi Aktif** | Mematikan `httptest` server #1 dan menyalakan server #2 ke DB sama mempertahankan sesi login (`GET /api/v1/auth/me` tetap `200 OK`). | **PASS** |
| **Worker Mati Tengah Tagihan** | Menjalankan `GenerateMonthlyBills` ulang pada periode sama menghasilkan `Dibuat=0` dan `Dilewati` seluruhnya idempoten. | **PASS** |
| **Abrupt Kill (`Process.Kill()`) Saat Checkout Ramai** | `70` request dikirim (`53` order sempat komit sebelum *hard kill*): `kill_negative_stock_rows=0`, `kill_orphan_item_orders=0`, `kill_mismatched_paid_rows=0`. | **PASS** |
| **Drill Backup Terenkripsi & Restore** (`pg_dump` + AES-256-GCM HKDF `PurposeBackup`) | Ukuran dump `10.024.399 B` -> terenkripsi `10.024.433 B`; modifikasi 1 byte ciphertext ditolak; restore ke DB terisolasi memulihkan **21 tabel** dan **26 anggota** dalam **RTO = 5.180 ms (5,18 detik)** (`RPO=0` sebelum snapshot, 24 jam antar-cron tanpa PITR). *Catatan: `age` CLI diuji pada CI Ubuntu, lokal memakai AES-256-GCM terautentikasi.* | **PASS** |

---

## 4. Angka Patokan Pembanding Fase 4

- **Load Normal (100 VU)**: Read p95 `<= 176,28 ms` (SLO `< 300 ms`), Write p95 `<= 349,71 ms` (SLO `< 800 ms`), Error Rate `0,0000`.
- **SAST & Keamanan**: `0` temuan `gosec`, `0` `staticcheck`, `0` `govulncheck` di kode, `0` kebocoran `gitleaks`, seluruh uji `TestSecurityAbuse_*` hijau.
- **Chaos**: `0` inkonsistensi stok/uang pasca abrupt kill, pemulihan DB otomatis `< 2 s`, `RTO` restore baseline `~5,18 s`.
