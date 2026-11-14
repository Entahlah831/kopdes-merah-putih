# Laporan Audit Mandiri Keamanan & Remediasi (Audit Report)
**Sistem Backend Koperasi Desa Merah Putih (PostgreSQL 16 & Go)**

> [!NOTE]
> **Status Tinjauan**: Audit mandiri oleh agen pengembang; belum ada peninjauan pihak ketiga (*third-party security audit / legal review*). Seluruh status **TERVERIFIKASI DI LINGKUNGAN UJI** di bawah ini didasarkan pada kode sumber, migrasi SQL (`000001`–`000018`), serta pengujian otomatis di profil terisolasi Set A (`kopdes_test`) dan Set B (`kopdes_alt_test`), dengan daftar lengkap keterbatasan dan hal tertunda pada Bagian 8.

---

## 1. Ringkasan Eksekutif

Audit mandiri terhadap basis kode Koperasi Desa Merah Putih mengidentifikasi sejumlah kelemahan arsitektur, integritas data, dan konfigurasi dari tingkat **KRITIS**, **TINGGI**, **SEDANG**, hingga **KELENGKAPAN FUNGSIONAL (P1)** serta pengerasan lanjutan pasca-audit (Tugas 1–10 dan v5). Seluruh temuan yang ditangani diremediasi menggunakan pendekatan **Test-Driven Development (TDD)** — membuktikan kegagalan lewat pengujian sebelum perbaikan — dan dicatat melalui migrasi bernomor urut (`000001` sampai `000018`) tanpa memodifikasi riwayat migrasi awal setelah terbit.

Sistem mengandalkan pengamanan multi-tenancy pada tingkat database (PostgreSQL 16 `FORCE ROW LEVEL SECURITY` dengan role non-superuser `kopdes_app`), enkripsi data pribadi (AES-256-GCM + AAD per kolom dan record ID, blind index HKDF-SHA256 per tenant), serta pengaman transaksi finansial (*Maker-Checker*, *row locking*, idempotency berbasis database, dan rantai hash *append-only*). Catatan: kelayakan hukum, perpajakan/akuntansi, serta sertifikasi keamanan eksternal **PERLU VERIFIKASI HUKUM** dan penetapan resmi pihak ketiga/pemilik sistem.

---

## 2. Matriks Remediasi Cacat & Penyelarasan Migrasi (`000001`–`000018`)

Tabel di bawah diselaraskan dengan riwayat commit Git dan berkas migrasi aktual di `db/migrations/`.

### A. Temuan Keamanan Awal (`K1`–`K3`, `T1`–`T5`, `S1`–`S3`, `P1-1`–`P1-5`)

| Kode | Kategori | Deskripsi Temuan | Status | Migrasi & Commit Sebenarnya | Solusi & Bukti Verifikasi Teramati |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **K1** | KRITIS | API terhubung sebagai superuser DB di Docker Compose, dan celah bypass pra-otentikasi platform admin | **TERVERIFIKASI DI UJI** | Commit `8279018` (`db/docker-init/01_roles.sql`, `internal/db/role_guard.go`, `docker-compose.yml`); difinalisasi bersama Migrasi `000004` (`3282f84`) | Aplikasi wajib memakai role `kopdes_app` (`NOSUPERUSER NOBYPASSRLS`) dan gagal start jika mendeteksi superuser (`db.AssertNonSuperuser`). Menghapus bypass pra-otentikasi; `koperasi_id` hanya berasal dari JWT terverifikasi. |
| **K2** | KRITIS | Partisi tabel hanya sampai 31 Des 2026 tanpa partisi lanjutan / `DEFAULT`, serta risiko hilangnya insert setelah 2026 | **TERVERIFIKASI DI UJI** | Migrasi `000002` (`000002_add_default_and_future_partitions.up.sql`, commit `84da6cd`); dilengkapi Migrasi `000011` (`a9d354a`) | Menambah partisi bulanan 2027 dan partisi `DEFAULT` pada `audit_log`, `kas_mutasi`, `notifikasi`, dan `stok_mutasi`, lalu menambahkan fungsi pembuat partisi idempoten 3 bulan ke depan + metrik pemantauan. |
| **K3** | KRITIS | *Race condition* verifikasi pembayaran QRIS, klaim ganda bukti transfer, & integrasi mutasi kas/outbox | **TERVERIFIKASI DI UJI** | Migrasi `000003` (`000003_fix_pembayaran_verification.up.sql`, commit `095c492`) | Fungsi SQL atomik `fn_setujui_pembayaran_qris` dengan `FOR UPDATE`, penjagaan idempotensi status, mutasi kas, serta penulisan antrean `notifikasi` outbox dalam satu transaksi. |
| **T1** | TINGGI | Pembayaran dapat diajukan tanpa ikatan tagihan iuran atau pesanan valid | **TERVERIFIKASI DI UJI** | Tabel `pembayaran_alokasi` (Migrasi `000001`, dipadukan di `000003`), handler commit `c0b7448` | `HandleBuatQRIS` mewajibkan tagihan iuran / pesanan milik anggota bersangkutan yang sah di database sebelum mengeluarkan kode unik. |
| **T2** | TINGGI | IP Spoofing via `X-Forwarded-For`, kebocoran memori rate limiter, dan brute-force login | **TERVERIFIKASI DI UJI** | Commit `a73970a`; diperketat `/16` CIDR guard pada Tugas 0 v5 (`internal/config/config.go`) | Hanya membaca `X-Forwarded-For` dari `TRUSTED_PROXIES` (di produksi CIDR lebih lebar dari `/16` ditolak saat startup); bounded LRU limiter & lockout brute-force login per IP dan akun. |
| **T3** | TINGGI | Secret kriptografi berisiko lemah, ketiadaan AAD AES, blind index lintas-tenant, & kebocoran DSN di log | **TERVERIFIKASI DI UJI** | Commit `2cdafbb` (`internal/security/`) | Panjang secret minimal 32 byte, AES-256-GCM dengan AAD `table:column:record_id`, blind index HKDF-SHA256 per tenant, serta `RedactDSN`. |
| **T4** | TINGGI | Celah RLS pada `users`, `audit_log`, `job_log`, `konfigurasi_aturan`, & lookup pra-login | **TERVERIFIKASI DI UJI** | Migrasi `000004` (`000004_enforce_rls_all_tables_and_login_helper.up.sql`, commit `3282f84`) | Mengaktifkan `FORCE ROW LEVEL SECURITY` di seluruh tabel terproteksi dan mengunci lookup pra-login melalui fungsi `SECURITY DEFINER` `fn_lookup_user_login`. |
| **T5** | TINGGI | Sesi JWT monolitik jangka panjang, pembagian izin `pengawas`/`operator`, & enumerasi akun | **TERVERIFIKASI DI UJI** | Migrasi `000005` (`000005_refresh_tokens_and_permissions.up.sql`, commit `0180c5b`) | Access token ber-`jti` 15 menit + tabel `refresh_tokens` berotasi dengan deteksi *token reuse*, read-only role `pengawas`/`operator`, serta constant-time dummy bcrypt. |
| **S1** | SEDANG | IDOR akses status pembayaran antaranggota dalam satu koperasi | **TERVERIFIKASI DI UJI** | Commit `b0d3ac0` (`internal/api/handlers/qris_handler.go`) | Anggota biasa hanya boleh melihat `pembayaran` dengan `pembayar_user_id` atau `anggota_id` miliknya; permintaan terhadap milik anggota lain ditolak (`403`/`404`). |
| **S2** | SEDANG | Parser EMVCo QRIS rapuh terhadap tag duplikat/checksum dan rekening settlement perorangan | **TERVERIFIKASI DI UJI** | Migrasi `000006` (`000006_rekening_qris_jenis_pemilik.up.sql`, commit `adda568`) | Validasi CRC16 (Tag 63), Tag `00='01'`, tolak tag duplikat, hapus Tag 55–57 saat Tag 54 ada, fuzz test, serta tolak rekening perorangan tanpa flag pilot eksplisit. |
| **S3** | SEDANG | Hardening HTTP Slowloris, batasan body, sanitasi CORS `X-Koperasi-ID`, & timeout query SQL | **TERVERIFIKASI DI UJI** | Commit `b829484` (`internal/api/server.go`, `cors.go`, `body_limit.go`) | `ReadHeaderTimeout = 5s`, `BodyLimit = 10MB`, hapus `X-Koperasi-ID` dari CORS, tolak wildcard `*` di produksi, `statement_timeout`, dan `lock_timeout`. *(Header `Access-Control-Expose-Headers` dan `X-Request-ID` disempurnakan di Tugas 0 v5.)* |
| **P1-1** | FUNGSI | Alur pendaftaran KYC anggota, unggah dokumen aman, dan mutasi kas | **TERVERIFIKASI DI UJI** | Commit `158de31` (`pendaftaran_handler.go`, `kas_handler.go`) | Pendaftaran berenkripsi PII + blind index, validasi magic-bytes berkas KYC, persetujuan Maker-Checker, dan `GET /api/v1/kas/mutasi`. |
| **P1-2** | FUNGSI | Kapasitas kode unik 3/4 digit, peringatan kapasitas 70%, & penolakan aman saat penuh | **TERVERIFIKASI DI UJI** | Migrasi `000008` (`000008_kode_unik_configurable_digits.up.sql`, commit `158de31`) | Dukungan 3 digit (`999` slot) / 4 digit (`9.999` slot), peringatan log pada `>= 70%`, dan respons ramah `409 Conflict` saat slot habis (diuji dengan 1.500 request konkuren pada Tugas 0 v5). |
| **P1-3** | FUNGSI | Rantai hash *append-only* (`audit_log`, `kas_mutasi`, `pembayaran_verifikasi_log`) & anti-`TRUNCATE` | **TERVERIFIKASI DI UJI** | Migrasi `000007` (`158de31`) & Migrasi `000010` (`d10d73b`) | `000007` menambah kolom `prev_hash`/`row_hash` dan trigger anti-`TRUNCATE`; `000010` memindahkan pengisian hash chain ke trigger `BEFORE INSERT` dengan advisory lock agar jalur SQL maupun API terisi konsisten. |
| **P1-4** | FUNGSI | Idempotency DB, proteksi `/metrics`, MFA TOTP, outbox worker, & backup/restore logis | **TERVERIFIKASI TERBATAS** | Migrasi `000013`–`000017` (`54a15ba`..`3612a99`, `ab8c6c4`) | Idempotency tabel DB + HTTP `422` (`000013`), mutasi bank & dual approval (`000014`, `000017`), `/metrics` wajib token >=32B & MFA TOTP wajib (`000016`), backup logis `pg_dump` + `restore_drill.sh`. **Keterbatasan: PITR / arsip WAL belum diimplementasikan (lihat Bagian 3 & 8).** |
| **P1-5** | FUNGSI | Rekonsiliasi ruang lingkup skema database & tabel SIMKOPDES | **TERVERIFIKASI DI UJI** | Migrasi `000009` (`1d62927`) lalu dipulihkan Migrasi `000012` (`cd70b5a`) | `000009` sempat mendrop `sinkronisasi_simkopdes`; `000012` memulihkannya beserta adapter stub `internal/simkopdes/adapter.go`. Skema aktual berisi **69 tabel induk/dasar** (`65` reguler + `4` parent terpartisi) dan **60 partisi** (**129 objek tabel fisik**). Lihat [SCOPE_RECONCILIATION.md](SCOPE_RECONCILIATION.md). |

### B. Ringkasan Migrasi Lanjutan (`000009`–`000018`)

- **Migrasi `000009` (`1d62927`)**: Menambah kolom/konfigurasi retensi PDP (`fn_purge_rejected_pendaftaran_pdp`) serta sempat mendrop `sinkronisasi_simkopdes` (dikoreksi pada `000012`).
- **Migrasi `000010` (`d10d73b`)**: Trigger `BEFORE INSERT` `pgcrypto` pengisi `prev_hash` / `row_hash` dengan advisory lock (diperbaiki lagi pengurutan kuncinya melalui `internal/db/advisory_lock.go` di commit `a25d8f7`).
- **Migrasi `000011` (`a9d354a`)**: Fungsi SQL idempoten pembuat partisi 3 bulan ke depan (`partition_manager.go`).
- **Migrasi `000012` (`cd70b5a`)**: Mengunci `fn_purge_rejected_pendaftaran_pdp` (validasi `app.current_koperasi_id`, `REVOKE PUBLIC`, `GRANT kopdes_app`, audit log tanpa PII) + memulihkan tabel `sinkronisasi_simkopdes`.
- **Migrasi `000013` (`54a15ba`)**: Tabel `idempotency_keys` dan respons `422 IDEMPOTENCY_CONFLICT` untuk kunci sama dengan isi body berbeda.
- **Migrasi `000014` (`b1b5361`)**: Impor mutasi bank CSV, pencocokan nominal otomatis, dual approval di atas ambang batas, deteksi anomali, dan laporan rekonsiliasi harian.
- **Migrasi `000015` (`5d64241`)**: Batas nominal per transaksi & harian zona waktu `Asia/Jakarta` (`1fa4f1c`) + peringatan pilot rekening perorangan.
- **Migrasi `000016` (`1c5114b`)**: MFA TOTP wajib bagi `admin`/`bendahara`/`ketua`, *enrollment-only setup token*, anti-replay window, recovery codes ber-hash, dan proteksi token `/metrics`.
- **Migrasi `000017` (`3612a99`)**: Pemisahan tugas pengimpor vs pembuat vs penyetuju mutasi bank (`segregation of duties`), hash baris mutasi unik 1-ke-1, serta auto-approve nonaktif secara default.
- **Migrasi `000018` (`52fc1e7`)**: Pemberian izin `DELETE` terkontrol untuk operasi bisnis sah (mis. penghapusan item keranjang/katalog) pada role `kopdes_app`.

---

## 3. KEPUTUSAN YANG DIUSULKAN / DIPAKAI SEMENTARA

> [!IMPORTANT]
> Bagian ini memuat parameter kebijakan operasional yang **diusulkan / dipakai sementara** dalam kode dan konfigurasi pengujian. Agen pengembang **tidak berwenang menyatakan persetujuan resmi** atas nama Ketua Koperasi, Pengurus, ataupun Penasihat Hukum. Pemilik sistem wajib mengisi kolom **Bukti persetujuan** dan memverifikasi aspek hukum/akuntansi sebelum go-live produksi.

| No | Topik Keputusan | Nilai / Mekanisme Sementara di Kode | Konfigurasi & Default Produksi | Status Verifikasi Eksternal | Bukti persetujuan (diisi pemilik: notulen/email/chat) |
| :--- | :--- | :--- | :--- | :--- | :--- |
| 1 | Penggunaan Rekening & QRIS Perorangan Pengurus | Diperbolehkan hanya bila flag pilot aktif; dibatasi limit per transaksi, limit harian `Asia/Jakarta`, log audit khusus, dan banner peringatan di respons admin | Default **`false`** pada `.env.example`, `.env.test`, `.env.test.alt`, serta `AppConfig`; mode `true` hanya diuji pada kasus uji API Go terisolasi (`internal/api/`). | Perlu keputusan pengurus sebelum mengaktifkan `true` di produksi | |
| 2 | Mode QRIS Statis + Kode Unik vs QRIS Dinamis | QRIS statis + alokasi kode unik kriptografis 3 digit (atau 4 digit) aktif; auto-lock QRIS dinamis dinonaktifkan sementara | `QRIS_DYNAMIC_ENABLED=false` (default) | Menunggu kesiapan kontrak QRIS dinamis PJP resmi | |
| 3 | *Dual Control* (*Maker-Checker*) Transaksi & Mutasi CSV | Pemohon/pembuat/pengimpor dilarang menyetujui transaksi sendiri; dual approval di atas ambang nominal; auto-approve impor CSV nonaktif secara default | Ditegakkan di DB (`fn_setujui_pembayaran_qris` + Migrasi `000014`/`000017`) | Disarankan tetap aktif saat go-live | |
| 4 | Masa Retensi Penghapusan Data Pendaftar Ditolak | Fungsi `fn_purge_rejected_pendaftaran_pdp` dan `internal/db/pdp_worker.go` melakukan **penghapusan baris/kolom database serta penghapusan berkas foto KYC fisik di disk** (**BUKAN** *crypto-shredding*). | `PDP_REJECTED_RETENTION_DAYS=30` (dapat diubah via konfigurasi) | **PERLU VERIFIKASI HUKUM** (konsultan hukum/DPO harus menilai kecukupan masa retensi terhadap UU PDP & arsip koperasi) | |
| 5 | Pelaporan SIMKOPDES & Operasi Lokal Cabang | Skema `sinkronisasi_simkopdes` dipulihkan oleh Migrasi `000012` dengan adapter stub `internal/simkopdes/adapter.go`; operasi default tetap lokal satu cabang | Adapter stub terisolasi; siap disambungkan ke spesifikasi API eksternal Kemenkop UKM bila dibutuhkan | **MENUNGGU KEPUTUSAN PEMILIK & SPEK EKSTERNAL** | |
| 6 | Klasifikasi Akuntansi Jenis Iuran | Label kode netral tanpa menyematkan kepastian hukum akuntansi koperasi secara sepihak; setiap respon `GET/POST /api/v1/iuran/jenis` menyertakan penanda | `"klasifikasi_akuntansi": "KEPUTUSAN AKUNTAN"` (`iuran_handler.go`) | **KEPUTUSAN AKUNTAN** (pemetaan ekuitas vs liabilitas wajib ditetapkan akuntan koperasi) | |

### Bukti Ketiadaan Berkas `.env` Produksi pada Riwayat Git

Seluruh pengujian dan CI memakai `.env.example`, `.env.test`, serta `.env.test.alt`. Berkas asli pemilik (`.env`, `.env.local`, `.env.production`, `.env.live`, `secrets/`) tidak pernah disentuh ataupun masuk ke riwayat Git, terbukti dari hasil perintah langsung berikut:

```text
$ git log --name-status -- ".env*"
commit 5b24dab685da4b10492091d44965b54df38102fc
M	.env.test.alt
commit de75732a9527770484091d44965b54df38102fc (ringkas: .env.example, .env.test, .env.test.alt)
commit 568dbe0e7fe1aca424fa7181926df153a740d9d4
M	.env.example
commit 0c5254564e8bd6841ae530542c3de6a83b0b00eb
M	.env.example
M	.env.test
M	.env.test.alt
commit d198098bc683288d19eb4e2b136ab94b6bea9bd3
M	.env.example
A	.env.test
A	.env.test.alt
commit 52fc1e73b7a5021dd55ee02e74d663bcb7748371 -> M .env.example
commit 7b1dd050f3702f765215ceffa03e64248fc8e862 -> M .env.example
commit 1d62927ccc209304818a175979082b28ecf44fab -> M .env.example
commit 24ffe9a24c5114bb41282852b7da6cfc3ffa0159 -> M .env.example
commit b2ced33c064cca6a164b0bf284a8d6af34239874 -> A .env.example
```

*(Ringkasan jalur seluruh 10 commit `.env*`: hanya `.env.example`, `.env.test`, dan `.env.test.alt` yang ada dalam riwayat Git.)*

---

## 4. Perbaikan Audit v5 & Pembuktian ketidaksesuaian (DISC-01 s.d. DISC-04)

Pada penyelarasan audit v5 (`Tugas 0`), perbaikan kode berikut telah diuji dengan pola gagal-lalu-lolos:

1. **Penolakan CIDR `TRUSTED_PROXIES` Lebar (`> /16`) di Produksi (`0.7`)**:
   `AppConfig.ValidateProduction()` di `internal/config/config.go` kini mem-parsing setiap entri `TRUSTED_PROXIES` dan menolak prefix `< 16` (mis. `172.17.0.0/12`, `10.0.0.0/8`, `0.0.0.0/0`). Verifikasi lokal:
   ```text
   $ go test -v ./internal/config -run TestValidateProduction_RejectsWideTrustedProxiesCIDR
   === RUN   TestValidateProduction_RejectsWideTrustedProxiesCIDR
   --- PASS: TestValidateProduction_RejectsWideTrustedProxiesCIDR (0.00s)
   PASS
   ok  	github.com/kopdes/merah-putih/internal/config	1.160s
   ```
2. **Penghapusan Pengecualian Global `gosec` & Penambahan `trivy image` (`0.5`)**:
   Pengecualian global `-exclude=G104,G304,G101,G505,G115` telah dihapus dari `.github/workflows/ci.yml`. Anotasi `#nosec G505` (kebutuhan SHA-1 pada RFC 6238 TOTP) dan pemeriksaan batas non-negatif + `#nosec G115` ditambahkan secara spesifik pada `internal/security/totp.go`. Pemindaian penuh tanpa `-exclude` di lokal menghasilkan:
   ```text
   $ gosec.exe ./...
   Summary:
     Gosec  : dev
     Files  : 44
     Lines  : 13045
     Nosec  : 16
     Issues : 0
   ```
   Selain itu, job CI kini membangun image Docker dan menjalankan `trivy image --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1` (menunggu hasil eksekusi Linux CI saat push).
3. **Uji Kelelahan Kapasitas Slot Kode Unik QRIS (1.500 Request Konkuren pada 1 Nominal / 999 Slot) (`0.6`)**:
   Fungsi `BuatPembayaranQRISStatis` di `service.go` diperkuat agar menghindari antrean terkunci berantai saat bentrok kode unik (`freeCodes` diacak dengan `crypto/rand` setelah bentrok pertama dan di-prune setiap 4 bentrok), dan diuji melalui `TestQRISSlotExhaustion_1500ConcurrentSameNominal` (`internal/api/qris_slot_exhaustion_test.go`) serta submode `cmd/loadtest --mode qris-slot-exhaustion`:
   ```text
   === RUN   TestQRISSlotExhaustion_1500ConcurrentSameNominal
       qris_slot_exhaustion_test.go:145: QRIS Slot Exhaustion (1500 concurrent requests, 100 workers, 5.07s): 201_Created=999, 409_Conflict=501, 5xx=0, other=0
   --- PASS: TestQRISSlotExhaustion_1500ConcurrentSameNominal (7.40s)
   ```
   Hasil membuktikan tepat `999` transaksi memperoleh kode unik (`201 Created`) tanpa duplikasi aktif pada `(rekening_qris_id, nominal_total)` dan seluruh `501` sisanya mendapat respons ramah `409 Conflict` (`0` HTTP `5xx`, `0` status tak terduga).
4. **Koreksi Ketidaksesuaian Dokumentasi vs Implementasi (`DISC-01`–`DISC-04`)**:
   - `internal/api/middleware/auth.go`: mengembalikan kode `MFA_ENROLLMENT_REQUIRED` ketika token `mfa_setup` dipakai mengakses rute terlindungi.
   - `internal/api/server.go`: membatasi `POST /api/v1/toko/pesanan/{id}/expire` hanya bagi `admin`, `pengurus`, `bendahara`, `ketua`, `operator` (bukan anggota biasa).
   - `internal/api/middleware/security_headers.go` & `cors.go`: propagasi/pembuatan `X-Request-ID`, penambahan `Access-Control-Expose-Headers` (`X-Request-ID`, `Retry-After`), dan status `403` untuk origin tidak diizinkan pada *preflight* `OPTIONS`.
   - `internal/api/handlers/iuran_handler.go`: respons `POST /api/v1/iuran/jenis` kini konsisten menyertakan `"klasifikasi_akuntansi": "KEPUTUSAN AKUNTAN"` seperti `GET /api/v1/iuran/jenis`.
   - Keempat test `TestV5_Discrepancy01..04` lulus dalam pengujian lokal (`ok github.com/kopdes/merah-putih/internal/api 14.754s`).

### Pembuktian Lanjutan v5: Frontend Tester, Matriks 54 Operasi, & `DISC-05` s.d. `DISC-11`

Setelah perbaikan awal `DISC-01`–`DISC-04`, pengujian live terhadap server API nyata dan paritas artefak (`Tugas 1`–`7` v5) menemukan **7 ketidaksesuaian tambahan (`DISC-05` s.d. `DISC-11`, seluruhnya dicatat di [DOC_DISCREPANCIES.md](DOC_DISCREPANCIES.md))**, termasuk **3 ketidaksesuaian fungsional/pengujian nyata**:
- **`DISC-09` (`iuran_handler.go`)**: `GET /api/v1/iuran/tagihan/saya` mengembalikan HTTP `500` untuk user manajemen non-anggota karena `pgx.ErrNoRows` di tabel `anggota`; diperbaiki menjadi `200 OK []`.
- **`DISC-10` (`service.go`)**: `POST /api/v1/iuran/generate` gagal dengan PostgreSQL `SQLSTATE 42P08` (*inconsistent types deduced for parameter `$3`*); diperbaiki via cast eksplisit `$3::text`.
- **`DISC-11` (`internal/testseed/seed.go` & `frontend-tester/app.js`)**: Penyeragaman resource ID lintas-tenant uji (`produk_b_id`, `pembayaran_b_id`, `rekening_qris_b_id`) sehingga seluruh `7` probe `cross_tenant` berjalan deterministik.
- **Pembuktian Live (`TestV5_LiveScenariosAndRolesMatrix_E2E` & `TestDoc_*`)**: Memverifikasi **`439` pemeriksaan matriks peran** (`54` operasi × `7` peran + `unauthenticated` + `7` `cross_tenant`), **`26` skenario uji `claim:ID` (`102` langkah)**, kebijakan CORS + `Access-Control-Expose-Headers`, penolakan body `> 10MB` (`413`), dan pemindaian **zero-PII di `540` respons API**.

---

## 5. Status Cadangan Data (*Backup*), Latihan Pemulihan (*Restore Drill*), & PITR (`0.4`)

- **Status PITR**: **PITR belum diimplementasikan** (*Point-In-Time Recovery* berbasis arsip WAL berkelanjutan / PgBackRest / WAL-G tidak tersedia pada skrip atau Compose saat ini).
- **Yang Tersedia & Teruji Saat Ini**: Skrip cadangan logis `scripts/backup.sh` (`pg_dump` dengan dukungan enkripsi opsional OpenSSL AES-256-CBC via `BACKUP_ENCRYPTION_KEY`; di profil uji `.env.test` & `.env.test.alt` kunci ini terisi, sedangkan di `.env.example` dibiarkan kosong sehingga pengisiannya wajib bagi pemilik) serta latihan pemulihan `scripts/restore_drill.sh`.
- **Pembaruan Latihan Restore (Tugas 0.4)**: `scripts/restore_drill.sh` telah diperbarui agar mencetak status enkripsi dump secara eksplisit **dan** memverifikasi `VerifyHashChain` pada **ketiga** tabel append-only: `audit_log`, `kas_mutasi`, serta `pembayaran_verifikasi_log` (sebelumnya hanya `audit_log`). Hasil terperinci terakhir sebelumnya tercatat pada CI Run `43385163339` (`WAL LSN Checkpoint: 0/2342000`, `129` objek tabel pulih dalam `2s`, `audit_log broken links: 0`, `61` RLS aktif). Pada **CI Run `43565262692`** (commit `689beae`, pasca-pembaruan skrip 3-tabel), step 5 job `docker-setB-e2e-and-load` (`Jalankan Full Suite -race & Restore Drill & Load Test 2000 Anggota pada Set B`) selesai dengan status `completed/success`. *(Catatan: unduhan log teks mentah langkah CI di GitHub memerlukan sesi autentikasi akun/`gh` sehingga isi stdout langkah `43565262692` tidak diekstrak secara anonim.)*

---

## 6. Audit Konfigurasi (`envcheck`), Pemindaian Keamanan, & Toolchain

### Hasil `envcheck` (Terverifikasi Lokal pada Tugas 8 v5)
- `.env.test` (`--strict`): `envcheck STRICT PASS: 83 variabel terverifikasi, APP_ENV=test, DB_NAME=kopdes_test, API_PORT=8080, METRICS_PORT=9100` (`EXIT: 0`).
- `.env.test.alt` (`--strict --ports`): `envcheck PORTS PASS: tidak ada konflik port pada konfigurasi.` + `envcheck STRICT PASS: 83 variabel terverifikasi, APP_ENV=test, DB_NAME=kopdes_alt_test, API_PORT=18080, METRICS_PORT=19100` (`EXIT: 0`).
- `--compare .env.test .env.test.alt`: `envcheck COMPARE PASS: skema kunci 100% identik (83 kunci).` (`EXIT: 0`).
- `.env.example` (`--strict`): ditolak `EXIT: 1` karena rahasia wajib kosong (`DB_PASSWORD`, `JWT_SECRET`, `AES_MASTER_KEY`, `BLIND_INDEX_SECRET`, `QRIS_SECRET`, `METRICS_TOKEN`).
- Uji bentrok port live (`.env.test --strict --ports` saat PostgreSQL Set A aktif di `5433`): mendeteksi `PORT SEDANG DIPAKAI (DB_HOST_PORT): 127.0.0.1:5433` (`EXIT: 1`).

### Pemindaian Keamanan, Uji `-race`, & Toolchain

| Pemindai / Pengujian | Lingkungan Eksekusi | Hasil Teramati | Catatan |
|---|---|---|---|
| **Gitleaks** (`git --config .gitleaks.toml`) | Lokal Windows Tugas 8 v5 | `74 commits scanned`, `no leaks found` (`EXIT: 0`) | Diuji juga pada Linux CI |
| **`staticcheck ./...`** | Lokal Windows Tugas 8 v5 | `0` isu (`EXIT: 0`) | Setelah perbaikan `S1021` di `scenarios_and_matrix_e2e_test.go` |
| **`gosec ./...` (Tanpa `-exclude` global)** | Lokal Windows Tugas 8 v5 | `Files: 47, Lines: 14144, Nosec: 20, Issues: 0` (`EXIT: 0`) | Pengecualian global CI telah dihapus |
| **`go test -count=1 -race -p 1 ./...`** | Lokal Windows Set A Tugas 8 v5 | Seluruh paket lulus (`EXIT: 0`; `internal/api` `167.549s`) | Setelah `fastTestHashPassword` (cost 10 cache) di `api_test.go` |
| **`govulncheck ./...`** | Lokal `go1.27.1` vs CI `go1.27.4` | Lokal `go1.27.1`: 12 temuan stdlib (`http2`/`net/http` fixed di `1.27.2+`); CI `43565262692` (`go1.27.4`): step pemindaian Keamanan Go `completed/success` (run `43385163339`: `No vulnerabilities found.`) | Image Docker & Linux CI memakai `go1.27.4` |
| **Hadolint + Trivy Config + `trivy image`** | Linux CI (`43565262692`) | Job `config-and-static-audit` (step Hadolint, Trivy Config, Gitleaks) dan step 4 `docker-setB-e2e-and-load` (berisi `trivy image` `--severity HIGH,CRITICAL`) seluruhnya `completed/success` | Log teks mentah GitHub Actions tidak tersedia tanpa login (`HTTP 403` anonim) |

---

## 7. Ringkasan Uji Beban & Perbandingan Lingkungan Uji

- **GitHub Actions Run `43565262692`** (`https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/43565262692`, commit `689beae`, keseluruhan `completed/success`):
  - `config-and-static-audit` (`132357603433`): `completed/success` (`40s`, seluruh 5 langkah validasi envcheck, compose, Hadolint, Trivy config, Gitleaks).
  - `test-and-security-setA` (`132357603168`): `completed/success` (`4m 12s`, `go test -count=1 -race -p 1 -timeout 20m ./...`, `govulncheck`, `staticcheck`, `gosec` tanpa `-exclude` global).
  - `docker-setB-e2e-and-load` (`132357786607`): `completed/success` (`4m 47s`, Docker Compose Set B port non-standar + `trivy image` + `/readyz` + `/metrics`, `-race`, `backup.sh`, `restore_drill.sh` 3-tabel, dan `cmd/loadtest` 2.000 anggota).
- **Angka Terukur Uji Beban & Lokal**:
  - **Load Test 2.000 Anggota (`cmd/loadtest`)** tercatat pada CI Run `43385163339`: `success_count: 2000`, `error_count: 0`, `error_rate_pct: 0`, `rps: 15790.52`, `p50: 3.71ms`, `p95: 20.08ms`, `p99: 31.67ms` (pengujian kelelahan 1.500 slot QRIS konkuren lulus lokal pada Tugas 0 v5: `999` `201 Created`, `501` `409 Conflict`, `0` error dalam `5.07s`).

---

## 8. Keterbatasan yang Diketahui & Tindak Lanjut Wajib

1. **Belum Ada Audit & Pengujian Penetrasi Pihak Ketiga**: Seluruh hasil pada dokumen ini adalah hasil verifikasi mandiri tim/agen pengembang pada lingkungan uji, bukan sertifikasi keamanan atau peninjauan eksternal independen.
2. **PITR Belum Diimplementasikan**: Hanya tersedia cadangan logis `pg_dump` terjadwal + latihan restore; arsip WAL berkelanjutan (untuk RPO < 24 jam dan pemulihan ke titik waktu tertentu) perlu dibangun pada fase infrastruktur produksi berikutnya.
3. **Integrasi Eksternal Masih Stub/Lokal**: Pengiriman WhatsApp/SMS (`internal/outbox`) dan sinkronisasi SIMKOPDES (`internal/simkopdes`) bersifat *adapter stub* tanpa kredensial penyedia resmi; penyimpanan lampiran KYC memakai disk lokal dengan batasan akses, belum object storage eksternal S3/MinIO.
4. **Keterbatasan Akses Log Mentah CI Anonim**: Run Linux CI `43565262692` (`689beae`) terverifikasi `completed/success` pada ketiga job beserta seluruh step (`trivy image`, `govulncheck` `go1.27.4`, `gosec`, `-race` Set A/B, `restore_drill.sh` 3-tabel), namun unduhan berkas log stdout mentah GitHub Actions membutuhkan autentikasi (`gh`/sesi akun GitHub; REST API anonim mengembalikan `403 Forbidden`). Pemindaian lokal penuh (`envcheck` 83 var, `go test -count=1 -race -p 1 ./...`, `gosec`, `staticcheck`, `gitleaks`, dan `TestV5_LiveScenariosAndRolesMatrix_E2E` 439+102 cek) telah diekstrak secara lengkap di `docs/PROGRESS.md`.
5. **Kewajiban Hukum & Akuntansi Eksternal**:
   - **`PERLU VERIFIKASI HUKUM`**: Evaluasi UU 27/2022 PDP atas kebijakan retensi `PDP_REJECTED_RETENTION_DAYS` (default `30` hari) dan pemrosesan data biometrik/KTP.
   - **`KEPUTUSAN AKUNTAN`**: Pemetaan pos akuntansi iuran koperasi sebelum tutup buku.
   - **Tugas Operasional Pemilik**: Pengisian kredensial produksi, sertifikat TLS/reverse proxy domain HTTPS, dan verifikasi rekening resmi badan hukum koperasi ([TUGAS_MANUAL_PEMILIK.md](TUGAS_MANUAL_PEMILIK.md)).
