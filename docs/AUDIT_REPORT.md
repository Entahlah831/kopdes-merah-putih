# Laporan Audit Keamanan & Remediasi Lengkap (Audit Report)
**Sistem Backend Koperasi Desa Merah Putih (PostgreSQL 16 & Go)**

---

## 1. Ringkasan Eksekutif
Audit independen terhadap basis kode Koperasi Desa Merah Putih mengidentifikasi kerentanan dan ketidaklengkapan arsitektur dari tingkat **KRITIS**, **TINGGI**, **SEDANG**, hingga **KELENGKAPAN FUNGSIONAL (P1)**. Seluruh temuan tersebut telah diremediasi secara tuntas menggunakan metodologi **Test-Driven Development (TDD)** yang ketat: setiap cacat dibuktikan terlebih dahulu dengan pengujian gagal (*failing reproduction test*), diperbaiki hingga lolos (*pass test*), dan dipatenkan dengan migrasi database bernomor urut tanpa mengubah riwayat migrasi awal.

Sistem mengimplementasikan pengamanan multi-tenancy tingkat database (PostgreSQL 16 RLS), enkripsi data pribadi (AES-256-GCM + AAD), serta integritas transaksi finansial (ACID, Maker-Checker, append-only hash chaining). Catatan: Aspek kepatuhan hukum terhadap regulasi eksternal memerlukan audit dan verifikasi hukum formal tersendiri.

---

## 2. Matriks Remediasi Cacat & Status Verifikasi

| Kode | Kategori | Deskripsi Temuan | Status | Bukti Verifikasi / Solusi |
| :--- | :--- | :--- | :--- | :--- |
| **K1** | KRITIS | Multi-Tenant Bypass via Pre-Auth Platform Admin | **RESOLVED** | Migrasi `000002`: Hapus bypass pra-otentikasi, wajib validasi JWT murni, anti-BOLA OWASP API1:2023. |
| **K2** | KRITIS | Race Condition & Arbitrary Payment Claim | **RESOLVED** | Migrasi `000003` & `000004`: Atomic verification function (`fn_setujui_pembayaran_qris`), `FOR UPDATE`, idempotensi status. |
| **K3** | KRITIS | Pembayaran Tidak Terikat Tagihan / Pesanan | **RESOLVED** | Migrasi `000003`: `pembayaran_alokasi` tabel atomik, validasi kepemilikan tagihan/pesanan sebelum QRIS diterbitkan. |
| **T1** | TINGGI | Pembayaran Tanpa Batasan Nominal / Arbitrer | **RESOLVED** | Handler `HandleBuatQRIS` menolak pembayaran tanpa ikatan tagihan atau pesanan valid. |
| **T2** | TINGGI | Kelemahan Rate Limiting & IP Spoofing | **RESOLVED** | `internal/api/middleware/ratelimit.go`: Evaluasi `X-Forwarded-For` hanya dari `TRUSTED_PROXIES`, bounded LRU cache, lockout proteksi brute force login. |
| **T3** | TINGGI | Secret Kriptografi Lemah & Kebocoran Log | **RESOLVED** | Validasi minimal 32 byte secret, AES-256-GCM ber-AAD (`MasterKeyProvider`), per-tenant blind index HKDF, DSN password redactor. |
| **T4** | TINGGI | Celah RLS pada Master & Lookup Users | **RESOLVED** | Migrasi `000005`: Aktifkan RLS pada `users`, `audit_log`, `job_log`, `konfigurasi_aturan`; lookup login via `SECURITY DEFINER` terbatas. |
| **T5** | TINGGI | Sesi JWT Monolitik & Hierarki Peran Rentan | **RESOLVED** | Access token 15 menit, rotating refresh token di DB, deteksi pembajakan token (*token reuse*), hierarki peran deterministik, constant-time dummy bcrypt. |
| **S1** | SEDANG | IDOR Pembayaran dalam Satu Koperasi | **RESOLVED** | `HandleGetPembayaranStatus`: Anggota biasa dibatasi hanya bisa melihat pembayaran miliknya; pengurus/admin memiliki akses penuh. |
| **S2** | SEDANG | Parser EMVCo QRIS Rapuh & Rekening Pengurus | **RESOLVED** | Validasi CRC16 masukan, Tag 00="01", tolak tag duplikat, fuzz testing, migrasi `000006` tolak rekening pengurus di produksi jika tanpa izin pilot. |
| **S3** | SEDANG | Konfigurasi Deployment & Header CORS | **RESOLVED** | Hapus `X-Koperasi-ID` dari CORS headers, tolak `Allow-Credentials` pada wildcard origin, batasan ukuran body 10MB, Slowloris header timeout, DB connection timeouts. |
| **P1-1**| FUNGSI | Endpoint Pendaftaran, KYC, & Kas Mutasi | **RESOLVED** | Handler pendaftaran dengan enkripsi PII + blind index, unggah KYC validasi magic-bytes, maker-checker verifikasi kelulusan anggota, endpoint `GET /api/v1/kas/mutasi`. |
| **P1-2**| FUNGSI | Kapasitas Slot Kode Unik & Alert 70% | **RESOLVED** | Migrasi `000008`: Dukungan 3 digit (999 slot) atau 4 digit (9.999 slot), peringatan log di atas 70%, respons ramah HTTP 409 Conflict. |
| **P1-3**| FUNGSI | Append-Only Hash Chain & Anti TRUNCATE | **RESOLVED** | Migrasi `000007`: Kolom `prev_hash`, `row_hash`, trigger `BEFORE TRUNCATE` pada `kas_mutasi`, `audit_log`, `pembayaran_verifikasi_log`, fungsi verifikasi rantai. |
| **P1-4**| FUNGSI | Operasional, Idempotency & Prometheus | **RESOLVED** | Header `Idempotency-Key` pada pembayaran, endpoint `/metrics` Prometheus, interface `Notifier` + Outbox Worker, skrip backup/restore PITR. |
| **P1-5**| FUNGSI | Rekonsiliasi Skema Database | **RESOLVED** | Dokumen `docs/SCOPE_RECONCILIATION.md` mendokumentasikan 65 tabel dasar + 8 partisi bulanan tanpa penghapusan sembarangan. |

---

## 3. KEPUTUSAN RESMI MANAJEMEN & KLIEN (Final Decisions Adopted)

Seluruh 5 parameter kebijakan telah ditetapkan secara resmi oleh Ketua Koperasi dan Klien:

### 1. Kebijakan Rekening & QRIS Perorangan Pengurus (DISETUJUI)
- **Keputusan**: Mengizinkan penggunaan rekening dan QRIS statis atas nama pengurus koperasi. Admin koperasi mengunggah gambar/payload QRIS statis pengurus ke sistem melalui endpoint `/api/v1/koperasi/rekening-qris`.
- **Konfigurasi Aktif**: `ALLOW_PERSONAL_PAYMENT_PILOT=true` pada `.env` dan konfigurasi backend.

### 2. Status Fitur QRIS Dinamis (DINONAKTIFKAN — QRIS STATIS + KODE UNIK AKTIF)
- **Keputusan**: Fitur QRIS dinamis auto-lock dinonaktifkan sementara. Transaksi operasional koperasi sepenuhnya mengandalkan alokasi QRIS Statis dengan pencocokan 3-digit kode unik kriptografis (`nominal_total = nominal_dasar + kode_unik`).
- **Konfigurasi Aktif**: `QRIS_DYNAMIC_ENABLED=false` pada `.env`.

### 3. Persetujuan Ganda (Dual Control / Maker-Checker Transaksi Finansial) (DISETUJUI)
- **Keputusan**: Menerapkan prinsip *Dual Control* di mana pemohon dilarang menyetujui transaksi sendiri (*Maker-Checker*). Transaksi mutasi keuangan dan pendaftaran anggota wajib diverifikasi oleh pihak berwenang (Admin/Pengurus).

### 4. Kebijakan Retensi Data Pribadi UU 27/2022 PDP (DITETAPKAN 30 HARI)
- **Keputusan**: Berkas foto KTP/KYC dan data pendaftaran yang berstatus `'ditolak'` disimpan paling lama **30 hari**. Setelah 30 hari, data pribadi wajib dihapus permanen (*crypto-shredding*) untuk melindungi hak privasi warga.
- **Implementasi Database**: Fungsi `fn_purge_rejected_pendaftaran_pdp(koperasi_id, 30)` dan endpoint `POST /api/v1/pendaftaran/purge-ditolak`.

### 5. Pelaporan SIMKOPDES Kemenkop UKM (DIDE-SCOPE — FOKUS LOKAL SATU CABANG)
- **Keputusan**: Koperasi desa beroperasi secara mandiri dan lokal pada satu kantor cabang yang telah disepakati (ACC) dengan klien. Integrasi jaringan dan sinkronisasi nasional SIMKOPDES dinonaktifkan sepenuhnya.
- **Implementasi Database**: Migrasi `000009_local_scope_and_pdp_retention.up.sql` mendrop tabel `sinkronisasi_simkopdes`, merampingkan skema database menjadi 64 tabel dasar terproteksi RLS lokal.

---

## 4. Kesimpulan & Status Kesiapan Terkini (2026-11-13)
Basis kode, skema PostgreSQL 16 (termasuk pemulihan adapter stub `internal/simkopdes` sebagai batas integrasi siap-ekstensi), pengujian otomatis Go `-race`, konfigurasi terpusat (`envcheck`), serta paket dokumentasi teknis berada dalam kondisi terverifikasi pada lingkungan pengujian Set A dan Set B (Windows + Linux GitHub Actions CI). Keseluruhan klaim kepatuhan mutlak tetap tunduk pada audit hukum (`PERLU VERIFIKASI HUKUM`), penetapan pembukuan akuntansi (`KEPUTUSAN AKUNTAN`), dan pengisian kredensial produksi oleh pemilik sistem ([TUGAS_MANUAL_PEMILIK.md](TUGAS_MANUAL_PEMILIK.md)).

---

## 5. Hasil Audit Konfigurasi Terpusat (`envcheck`) & Uji Konflik Port

Seluruh konfigurasi dievaluasi tanpa memodifikasi `.env` asli pemilik, melainkan menggunakan dua berkas uji terisolasi (`.env.test` Set A dan `.env.test.alt` Set B dengan 79 variabel):

```text
=== .env.test (Set A --strict) ===
envcheck STRICT PASS: 79 variabel terverifikasi, APP_ENV=test, DB_NAME=kopdes_test, API_PORT=8080, METRICS_PORT=9100 (EXIT: 0)

=== .env.test.alt (Set B --strict --ports) ===
envcheck PORTS PASS: tidak ada konflik port pada konfigurasi.
envcheck STRICT PASS: 79 variabel terverifikasi, APP_ENV=test, DB_NAME=kopdes_alt_test, API_PORT=18080, METRICS_PORT=19100 (EXIT: 0)

=== .env.example (Penolakan Nilai Kosong Wajib) ===
NILAI KOSONG: DB_PASSWORD wajib diisi
NILAI KOSONG: JWT_SECRET wajib diisi
NILAI KOSONG: AES_MASTER_KEY wajib diisi
NILAI KOSONG: BLIND_INDEX_SECRET wajib diisi
NILAI KOSONG: QRIS_SECRET wajib diisi
NILAI KOSONG: METRICS_TOKEN wajib diisi
DSN DB TIDAK VALID: DB_PASSWORD wajib diisi
VALIDASI KEAMANAN GAGAL: secret JWT_SECRET tidak valid: panjang minimal 32 byte (didapat 0 byte)
(EXIT: 1 — Ditolak sesuai ekspektasi)

=== Perbandingan Skema (.env.test vs .env.test.alt) ===
envcheck COMPARE PASS: skema kunci 100% identik (79 kunci). (Seluruh rahasia disamarkan ***REDACTED***)

=== Uji Deteksi Konflik Port Live (--ports saat Set A PostgreSQL aktif pada :5433) ===
PORT SEDANG DIPAKAI (DB_HOST_PORT): 127.0.0.1:5433 (listen tcp 127.0.0.1:5433: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted.) (EXIT: 1 — Bentrok terdeteksi akurat)
```

---

## 6. Pemindaian Keamanan Statis, Container, Secret & Toolchain

| Pemindai | Lingkungan Eksekusi | Hasil Teramati | Catatan |
|---|---|---|---|
| **Gitleaks** | Lokal Windows + Linux CI | `0` kebocoran (`68` commit dipindai, `1.35 MB`) | EXIT `0` |
| **staticcheck `./...`** | Lokal Windows + Linux CI | `0` temuan | EXIT `0` |
| **gosec `./...`** | Lokal Windows + Linux CI | `Issues : 0` | Pengecualian eksplisit `G104,G304,G101,G505,G115` (`SHA-1` TOTP RFC 6238 di folder terkunci `internal/security/`) |
| **Hadolint & Trivy Config** | Linux CI (`ubuntu-latest`) | Lolos (`--severity HIGH,CRITICAL --exit-code 1 .`) | Run ID `43385163339` |
| **`govulncheck ./...` (Linux CI)** | Linux CI (`go 1.27.4`) | `No vulnerabilities found.` | Lolos 100% di target build CI/Docker |
| **`govulncheck ./...` (Host Windows Lokal)** | Lokal Windows (`go1.27.1`) | 11 temuan stdlib (a.l. `GO-2026-6859`, fixed in `go1.27.4`) | Hanya memengaruhi toolchain dev Windows lokal `1.27.1`; image CI/Docker memakai `1.27.4` |

---

## 7. Perbandingan Hasil Pengujian Set A vs Set B, Load Test & Restore Drill

Pengujian akhir pada **GitHub Actions Run `43385163339`** (`https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/43385163339`, commit `5b24dab`) menyelesaikan 3 job paralel hingga 100% hijau (`completed/success`), termasuk validasi sensitivitas timezone `UTC` (Linux CI) vs `Asia/Jakarta` (Windows lokal) untuk batas harian pilot rekening pribadi:

| Aspek Uji | Set A (`kopdes_test`, CI `test-and-security-setA`) | Set B (`kopdes_alt_test`, CI `docker-setB-e2e-and-load`) |
|---|---|---|
| **Job ID & Status** | `131751801297` (`completed/success`) | `131752034881` (`completed/success`) |
| **Port API / DB / Metrics** | `8080 / 5433 / 9100` | `18080 / 15433 / 19100` |
| **Timeout & TTL** | `JWT 15m (HS256)`, `stmt 15s`, `rate 30 rps` | `JWT 3m (HS512)`, `stmt 12s`, `rate 45 rps`, key rotation aktif |
| **`go test -count=1 -race -p 1 ./...`** | Seluruh paket `ok` (`internal/api 472.845s`) | Seluruh paket `ok` (`internal/api 469.022s`) |
| **Uji Beban 2.000 Anggota (`cmd/loadtest`)** | *(Dijalankan di Set B Docker Compose)* | `success_count: 2000`, `error_count: 0`, `error_rate_pct: 0`, `rps: 15790.52`, `p50: 3.71ms`, `p95: 20.08ms`, `p99: 31.67ms` |
| **Backup + `scripts/restore_drill.sh`** | *(Dijalankan di Set B Docker Compose)* | `WAL LSN 0/2342000`, `129` tabel pulih (`2s`), Marker Drill `1`, `0 broken links` (`VerifyHashChain` `audit_log`), `61` tabel RLS aktif |

### Verifikasi Dokumentasi & Spesifikasi API Otomatis (`TestDoc_*`)
Eksekusi lokal pasca-pembaruan dokumentasi (`go test -count=1 -v -race -run TestDoc ./internal/api`):
- `TestDoc_OpenAPI_RouteParity` (`PASS 0.03s`): paritas 100% seluruh rute `internal/api/server.go` terhadap `docs/openapi.yaml`, `docs/kopdes.postman_collection.json`, dan `docs/kopdesClient.ts` tanpa hardcode alamat/port tetap.
- `TestDoc_MarkdownLinks_Anchors_And_NoForbiddenClaims` (`PASS 0.23s`): validasi seluruh tautan relatif & `#heading-anchor` di `docs/*.md`, nihil klaim mutlak yang dilarang, dan mencantumkan penanda `PERLU VERIFIKASI HUKUM` & `KEPUTUSAN AKUNTAN`.
- `TestDoc_EndToEnd_DocumentedWorkflows` (`PASS 41.29s`): mengeksekusi alur langsung `/healthz`, `/readyz`, `/metrics`, MFA wajib (`MFA_MANDATORY_SETUP_REQUIRED` -> `data.setup_token` -> `/enable` -> `recovery_codes` -> `/mfa/verify`), pendaftaran + upload PNG KYC + masking NIK `****`, `GET /iuran/jenis` (`KEPUTUSAN AKUNTAN`), QRIS `Idempotency-Key` (`201`, replay, `422 IDEMPOTENCY_CONFLICT`), klaim RRN, dan verifikasi maker-checker.

---

## 8. Hal Tertunda & Keputusan Manusia yang Masih Diperlukan

- **`TES DITUNDA` (Menunggu Kredensial & Infrastruktur Eksternal Pemilik):** Integrasi langsung ke API penyedia SMS/WhatsApp berbayar (`internal/outbox` kini menggunakan stub/outbox worker lokal teruji), penyimpanan objek eksternal S3/MinIO jika memindahkan lampiran KYC dari disk lokal, verifikasi domain TLS HTTPS di reverse proxy VPS produksi, serta aktivasi QRIS badan hukum resmi dari PJP (saat `ALLOW_PERSONAL_PAYMENT_PILOT` dimatikan).
- **`PERLU VERIFIKASI HUKUM`:** Konsultan hukum/notaris perlu meninjau masa retensi penghapusan data pendaftar ditolak (`PDP_REJECTED_RETENTION_DAYS`, default `30` hari).
- **`KEPUTUSAN AKUNTAN`:** Pengurus & akuntan koperasi perlu menetapkan pemetaan jurnal akuntansi (ekuitas vs kewajiban/simpanan) untuk setiap jenis iuran sebelum tutup buku.
