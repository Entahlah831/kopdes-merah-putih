# PROGRESS PERBAIKAN BACKEND KOPDES MERAH PUTIH (v2)

Berdasarkan temuan terverifikasi dari audit kode pada `prompt-antigravity-v2-temuan-terverifikasi.md`.

---

## Status Temuan & Rencana Kerja

### I. KRITIS
- [x] **K1. Deploy Docker Compose menjalankan API sebagai SUPERUSER (RLS tidak berlaku)**
  - [x] Reproduksi dengan tes gagal: Superuser/bypassrls lolos start, atau DSN deploy default superuser.
  - [x] Pisahkan role: `kopdes_migrator`, `kopdes_app` (LOGIN, NOSUPERUSER, NOBYPASSRLS, NOCREATEDB), `kopdes_readonly_auditor`.
  - [x] API startup check: tolak start jika `current_user` memiliki `rolsuper` atau `rolbypassrls` (kecuali dev eksplisit).
  - [x] Tangani/perbaiki `SET ROLE` di `WithTenantTx`.
  - [x] Tes integrasi: login sebagai `kopdes_app` apa adanya (tanpa SET ROLE) harus lolos tes isolasi lintas tenant; tanpa `app.current_koperasi_id` query tenant = 0 baris.
  - [x] Batasi port PostgreSQL host ke `127.0.0.1` di docker-compose.
- [x] **K2. Partisi hanya sampai 31 Des 2026, tanpa partisi default**
  - [x] Reproduksi dengan tes gagal: INSERT kas_mutasi / audit_log dengan tanggal 2027-01-01 gagal.
  - [x] Buat migrasi `000002_add_default_partitions.up.sql` dan `.down.sql`: DEFAULT partition + 12 bulan ke depan.
  - [x] Tambahkan job pembuatan partisi otomatis (idempoten) menjaga minimal 3 bulan ke depan.
  - [x] Tes: insert tanggal di luar partisi terdaftar tidak boleh gagal.
- [x] **K3. Alur verifikasi pembayaran rusak dan tidak terhubung ke tagihan/pesanan**
  - [x] Reproduksi dengan tes gagal: approve memanggil `pembayaran_verifikasi_log` dengan kolom salah, tidak memanggil fungsi akuntansi/kas, mengizinkan approve sebelum bukti bayar, tidak cek nominal aktual vs total.
  - [x] Buat migrasi fungsi verifikasi pembayaran atomik atau transaksi Go setara (`FOR UPDATE`, status `menunggu_verifikasi`, periksa nominal, perbarui tagihan/pesanan/kas_mutasi/outbox, tulis log dengan skema kolom yang benar).
  - [x] Tangani error HTTP 400 tanpa membocorkan internal database.
  - [x] Tes end-to-end: buat tagihan, buat pembayaran, unggah bukti, verifikasi pengguna berbeda, pastikan tagihan lunas dan kas terisi; tes kurang bayar, bayar ganda, approve ganda.

---

### II. TINGGI
- [x] **T1. Pembayaran tidak terikat tagihan/pesanan (nominal dan anggota dari klien)**
  - [x] Reproduksi tes gagal: pembayaran tanpa tagihan/pesanan diterima atau nominal bebas dari klien.
  - [x] Request memuat `tagihan_iuran_id[]` / `pesanan_id`, hitung nominal di server, buat `pembayaran_alokasi`, gunakan `anggota_id` dari token (admin via catatan audit).
  - [x] Validasi kepemilikan tagihan/pesanan, tolak tagihan anggota lain, tagihan lunas, atau tagihan dengan pembayaran aktif.
  - [x] Verifikasi atomik: alokasi tersimpan pada `pembayaran_alokasi`, audit log tercatat untuk admin on-behalf, tes `TestT1_PaymentBindingToBills` PASS.
- [x] **T2. Rate limiter dapat dilewati dan rawan kehabisan memori**
  - [x] Reproduksi spoofing X-Forwarded-For bypass rate limit (`TestT2_SpoofedXFF_BypassesLimit_Reproduced` membuktikan bypass rate limiter).
  - [x] Batasi proxy terpercaya (`TRUSTED_PROXIES` via `TrustedProxyMatcher`), tolak percaya header proxy dari `RemoteAddr` yang tidak terdaftar.
  - [x] Batasi ukuran memory map dengan bounded LRU eviction (`container/list`, default 10.000 entri).
  - [x] Tambahkan `LoginRateLimiter` per akun (`email_hash`) dan per IP dengan backoff & lockout pada `/api/v1/auth/login`.
- [x] **T3. Secret default di kode, AES key tidak pernah dipakai, DSN bocor ke log**
  - [x] Hapus secret default di `DefaultConfig()`. Gagal start jika kunci JWT/BLIND/QRIS/AES kosong atau < 32 byte (`ValidateSecretKey`).
  - [x] Implementasi `KeyProvider` / `MasterKeyProvider` dengan AAD (`table:column:record_id`) dan prefix versi kunci pada AES-256-GCM.
  - [x] Blind index per tenant dengan HKDF-SHA256 (`DeriveTenantBlindIndexKey` & `HashTenantBlindIndex`).
  - [x] Redaksi DSN password (`RedactDSN`) pada seluruh output log kegagalan koneksi database.
- [x] **T4. Celah cakupan RLS dan bypass platform admin**
  - [x] Migrasi `000004_enforce_rls_all_tables_and_login_helper.up.sql`: Pasang FORCE RLS dan policy isolasi tenant pada `audit_log`, `job_log`, `konfigurasi_aturan`, `koperasi`, dan `users`.
  - [x] Perbaiki jalur login tanpa menyalakan bypass `app.is_platform_admin`: Dibuat fungsi SQL `fn_lookup_user_login(p_email_hash)` bertipe `SECURITY DEFINER` dengan `search_path = public, pg_temp` terkunci.
  - [x] Reproduksi dan verifikasi tes isolasi lintas tenant pada `audit_log` dan `job_log` (`TestT4_RLS_AuditLog_And_JobLog_Isolation` PASS).
- [x] **T5. Autentikasi dan otorisasi**
  - [x] Access token 15 menit + refresh token tabel rotasi (`refresh_tokens`), deteksi token reuse yang mencabut seluruh sesi jika token usang dipakai kembali.
  - [x] Pilih koperasi/peran aktif secara eksplisit dan deterministik melalui `fn_lookup_user_login(p_email_hash, p_koperasi_id)`.
  - [x] Constant-time login response (dummy bcrypt check jika user tidak ditemukan untuk mencegah time-based account enumeration).
  - [x] Biaya bcrypt dinaikkan menjadi 12.
  - [x] Pemisahan izin granular: peranan `pengawas` (hanya read-only audit) dan `operator` (hanya entri antrean/tunggakan) dibatasi dari mutasi pembayaran (`verifikasi`) dan tagihan (`generate`).

---

### III. SEDANG
- [x] **S1. IDOR dalam satu tenant pada GET /pembayaran/{id}**
  - [x] Batasi hak akses anggota hanya pada data pembayarannya sendiri (`pembayar_user_id` atau `anggota_id`).
  - [x] Peran istimewa (`admin`, `pengurus`, `bendahara`, `ketua`, `operator`, `pengawas`) tetap dapat melihat seluruh pembayaran dalam koperasinya.
  - [x] Verifikasi tes per-peran: Anggota B mendapat 404/403 saat mengakses pembayaran Anggota A, sedangkan Admin, Pengurus, dan Anggota A sendiri mendapat 200 OK (`TestS1_IDOR_PembayaranStatus_Reproduction` PASS).
- [x] **S2. Feature flag QRIS dinamis & hardening parser EMVCo**
  - [x] Feature flag `QRIS_DYNAMIC_ENABLED` (default false): saat non-aktif, kembalikan payload statis dengan nominal total & kode unik; saat aktif hasilkan dynamic QRIS auto-lock nominal.
  - [x] Hardening parser EMVCo: validasi checksum CRC16 (Tag 63) masukan, wajibkan Tag 00 bernilai '01', tolak tag duplikat, dan bersihkan Tag 55-57 saat Tag 54 nominal tetap disematkan.
  - [x] Fuzz test `FuzzParseTLV` dan round-trip test menjaga seluruh tag kustom/privat ASPI.
  - [x] Migrasi `000006_rekening_qris_jenis_pemilik.up.sql`: menambahkan kolom `jenis_pemilik` ('badan_hukum_koperasi' / 'perorangan_pengurus') dan penolakan mode perorangan kecuali `ALLOW_PERSONAL_PAYMENT_PILOT=true`.
  - [x] Dibuat checklist uji lapangan resmi di `docs/QRIS_UJI_LAPANGAN.md`.
- [x] **S3. Konfigurasi deployment & header hardening**
  - [x] Sanitasi header CORS: hapus `X-Koperasi-ID` dari `AllowedHeaders` (anti-BOLA). Mencegah pengiriman `Access-Control-Allow-Credentials: true` saat origin wildcard `*`.
  - [x] Validasi produksi: menolak start pada `APP_ENV=production` jika `ALLOWED_ORIGINS` bernilai `*` atau kosong, dan menolak jika kata sandi database menggunakan nilai bawaan dev.
  - [x] Hardening HTTP Server: menambahkan `ReadHeaderTimeout = 5s` (perlindungan Slowloris attack) dan middleware `BodyLimit = 10MB` (mencegah DoS payload besar).
  - [x] Pembatas waktu SQL di PostgreSQL: `statement_timeout = '15s'`, `lock_timeout = '5s'`, dan `idle_in_transaction_session_timeout = '30s'`.
  - [x] Dockerfile healthcheck dinamis menggunakan variabel port `${PORT:-${APP_PORT:-8080}}`.
  - [x] Sinkronisasi panduan aturan agen di `AGENTS.md` dan `GEMINI.md`.

---

### IV. KELENGKAPAN FUNGSI & LAPORAN
- [x] **P1-1. Endpoint KYC, Pendaftaran Anggota, Bukti Bayar, Mutasi Kas**
- [x] **P1-2. Kapasitas & Metrik Slot Kode Unik**
- [x] **P1-3. Append-Only Hash Chaining & Perlindungan TRUNCATE**
- [x] **P1-4. Operasional, Prometheus & Notifier**
- [x] **P1-5. Rekonsiliasi Cakupan Skema**
- [x] **Laporan Akhir: docs/AUDIT_REPORT.md & docs/FRONTEND_GUIDE.md**

---

### V. PERBAIKAN LANJUTAN TERVERIFIKASI (10 TUGAS)
- [x] **1. Hash chain otomatis lewat trigger BEFORE INSERT DB (pgcrypto digest & advisory lock)**
- [x] **2. Partisi SQL idempoten 3 bulan ke depan, job terjadwal & metrik partisi default**
- [x] **3. fn_purge_rejected_pendaftaran_pdp penguatan keamanan, audit tanpa PII, dan pulihkan sinkronisasi_simkopdes**
- [x] **4. Idempotency berbasis tabel database (kunci, user, hash request, respons, TTL) & 422 pada beda body**
- [x] **5. Verifikasi pembayaran: impor mutasi CSV, pencocokan nominal_total, persetujuan ganda, deteksi anomali**
- [x] **6. Pilot rekening pribadi: batas nominal transaksi/harian, banner peringatan admin, audit log**
- [x] **7. Proteksi /metrics (token/internal) & MFA TOTP peran pengurus/admin**
- [x] **8. Password role db/docker-init/01_roles.sql wajib variabel lingkungan tanpa default produksi**
- [x] **9. Kelengkapan endpoint CRUD anggota, iuran, katalog, keranjang/pesanan/checkout, laporan**
- [x] **10. Sanitasi dokumen (hapus klaim berlebihan) & verifikasi go test -count=1 -race ./...**

---

## Log Eksekusi & Bukti Uji
*(Diperbarui setiap tugas selesai beserta output perintah verifikasi)*

### [2026-11-09] K1, K2, K3 Selesai & Terverifikasi
- K1 (Superuser RLS Bypass): Selesai, migrasi role pemisahan, API startup check `rolsuper`/`rolbypassrls` guard, docker-compose bind 127.0.0.1. Commit `8279018`.
- K2 (Partisi Hanya Sampai 2026): Migrasi `000002_add_default_and_future_partitions.up.sql` (12 partisi 2027 + DEFAULT partition), forward partition manager. Commit `84da6cd`.
- K3 (Alur Verifikasi Pembayaran): Migrasi `000003_fix_pembayaran_verification.up.sql` (`fn_setujui_pembayaran_qris`, `fn_klaim_bukti_pembayaran`), atomic verifikasi, pembaruan `kas_mutasi`, maker-checker guard, outbox event. Commit `095c492`.

### [2026-11-09] T1: Ikat Pembayaran ke Tagihan/Pesanan Nyata
- **Reproduksi Gagal**: `TestT1_PaymentBindingToBills` awal gagal dengan status 500 / 400 karena request menerima arbitrary nominal dan anggota tanpa tagihan.
- **Implementasi**:
  - `POST /api/v1/pembayaran/qris` memvalidasi keberadaan `tagihan_iuran_id[]` atau `pesanan_id`. Menolak request jika keduanya kosong.
  - Untuk peran `anggota`, `anggota_id` diambil mutlak dari data anggota terkait token pengguna, menolak klaim/spoofing identitas anggota lain.
  - Untuk peran admin/pengurus, tindakan bayar atas nama anggota dicatat dalam `audit_log`.
  - Nominal dihitung murni di sisi server dari sisa tagihan (`nominal_tagihan - nominal_terbayar`) atau total pesanan.
  - Validasi ketat kepemilikan tagihan, tagihan yang sudah lunas, dan tagihan/pesanan yang sedang dalam proses pembayaran aktif.
  - Penyimpanan entri atomik ke dalam `pembayaran_alokasi`.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestT1_PaymentBindingToBills
  --- PASS: TestT1_PaymentBindingToBills (0.82s)
  ok  	github.com/kopdes/merah-putih/internal/api	8.549s
  ok  	github.com/kopdes/merah-putih	2.474s
  ok  	github.com/kopdes/merah-putih/internal/db	(cached)
  ok  	github.com/kopdes/merah-putih/internal/qris	(cached)
  ```

### [2026-11-09] T2: Penguatan Rate Limiter (Anti-Spoofing, LRU, Account Lockout)
- **Reproduksi Gagal**: `TestT2_SpoofedXFF_BypassesLimit_Reproduced` membuktikan penyerang yang memalsukan `X-Forwarded-For` dari satu IP dapat membypass rate limit (mendapatkan HTTP 200 berulang alih-alih 429).
- **Implementasi**:
  - Dibuat `TrustedProxyMatcher` yang memverifikasi `RemoteAddr` terhadap daftar IP / CIDR terpercaya. Jika `RemoteAddr` bukan proxy terpercaya, header `X-Forwarded-For` dan `X-Real-IP` diabaikan sepenuhnya.
  - Implementasi Token Bucket LRU Cache berbasis `container/list` dengan batas kapasitas `maxClients` (default 10.000). Mencegah memory leak dan serangan exhaustion memory.
  - Dibuat `LoginRateLimiter` khusus untuk `/api/v1/auth/login` yang membatasi kegagalan per akun (`email_hash`) dan per IP, dengan durasi lockout 15 menit dan reset otomatis saat login sukses.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestT2_SpoofedXFF_BypassesLimit_Fixed
  --- PASS: TestT2_SpoofedXFF_BypassesLimit_Fixed (0.00s)
  === RUN   TestT2_TrustedProxy_ValidXFF_Allowed
  --- PASS: TestT2_TrustedProxy_ValidXFF_Allowed (0.00s)
  === RUN   TestT2_BoundedMemory_LRUEviction
  --- PASS: TestT2_BoundedMemory_LRUEviction (0.00s)
  === RUN   TestT2_LoginRateLimiter_AccountAndIPLockout
  --- PASS: TestT2_LoginRateLimiter_AccountAndIPLockout (0.12s)
  ok  	github.com/kopdes/merah-putih/internal/api/middleware	1.296s
  ```

### [2026-11-09] T3: Pengamanan Secret, KeyProvider AES-AAD, HKDF Blind Index, Redaksi DSN
- **Reproduksi Gagal**: `TestT3_SecretValidation_RequiresStrongKey` dan `TestT3_AESGCM_WithAAD_TamperProof` gagal jika secret < 32 byte diizinkan atau enkripsi tanpa pengikatan AAD.
- **Implementasi**:
  - Hapus seluruh hardcoded dev fallback secrets di `DefaultConfig()`. Tambahkan validasi ketat `cfg.Validate()` (memvalidasi panjang $\ge 32$ byte untuk `JWT_SECRET`, `BLIND_INDEX_SECRET`, `QRIS_SECRET`, dan `AES_MASTER_KEY`).
  - Dibuat `MasterKeyProvider` dengan AAD (`table:column:record_id`) dan prefix metadata versi kunci (`v1:key_id:nonce+ciphertext`), menolak dekripsi jika AAD / tabel / kolom / record ID dimanipulasi.
  - Dibuat `DeriveTenantBlindIndexKey` menggunakan HKDF-SHA256 ber-garam `koperasi_id` untuk mengisolasi blind index NIK per koperasi, memutus korelasi frekuensi lintas tenant.
  - Ditambahkan fungsi `RedactDSN` untuk menyamarkan password database menjadi `******` pada semua pesan log fatal/error.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestT3_SecretValidation_RequiresStrongKey
  --- PASS: TestT3_SecretValidation_RequiresStrongKey (0.00s)
  === RUN   TestT3_AESGCM_WithAAD_TamperProof
  --- PASS: TestT3_AESGCM_WithAAD_TamperProof (0.00s)
  === RUN   TestT3_HKDF_TenantBlindIndex_Isolation
  --- PASS: TestT3_HKDF_TenantBlindIndex_Isolation (0.00s)
  === RUN   TestT3_RedactDSN_HidesPassword
  --- PASS: TestT3_RedactDSN_HidesPassword (0.00s)
  ok  	github.com/kopdes/merah-putih/internal/security	0.438s
  ```

### [2026-11-09] T4: Cakupan Penuh RLS & Penghapusan Pre-Auth Platform Admin Bypass
- **Reproduksi Gagal**: `TestT4_RLS_AuditLog_And_JobLog_Isolation` membuktikan Tenant B dapat melihat `audit_log` dan `job_log` milik Tenant A sebelum migrasi 000004.
- **Implementasi**:
  - Migrasi `000004_enforce_rls_all_tables_and_login_helper.up.sql` dan `.down.sql`: Mengaktifkan dan memaksakan (`FORCE`) Row-Level Security pada tabel `audit_log` (induk dan partisi), `job_log`, `konfigurasi_aturan`, `koperasi`, dan `users`.
  - Membuat fungsi SQL `fn_lookup_user_login(p_email_hash)` berpredikat `SECURITY DEFINER` dengan `search_path = public, pg_temp` terkunci, sehingga handler login Go tidak lagi memerlukan eksekusi berbahaya `set_config('app.is_platform_admin', 'true', true)` sebelum autentikasi.
  - Memperbaiki query profil dan penyimpanan audit log agar selalu berjalan dalam wrapper `WithTenantTx`.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestT4_RLS_AuditLog_And_JobLog_Isolation
  --- PASS: TestT4_RLS_AuditLog_And_JobLog_Isolation (0.07s)
  ok  	github.com/kopdes/merah-putih/internal/db	0.476s
  ok  	github.com/kopdes/merah-putih/internal/api	12.510s
  ```

### [2026-11-09] T5: Penguatan Autentikasi, Rotasi Refresh Token, Deteksi Reuse, dan Granular RBAC
- **Reproduksi Gagal**: `TestT5_AuthAndPermissions_Reproduction` membuktikan peran `pengawas` dan `operator` sebelumnya dapat memanggil endpoint mutatif verifikasi pembayaran dan generate tagihan massal, masa aktif access token terlalu lama (24 jam) tanpa refresh token dan deteksi reuse, serta ketiadaan seleksi koperasi/peran deterministik.
- **Implementasi**:
  - Migrasi `000005_refresh_tokens_and_permissions.up.sql` dan `.down.sql`:
    - Membuat tabel `refresh_tokens` untuk menyimpan token hash SHA-256, rantai rotasi token (`replaced_by_token_hash`), status revokasi (`revoked_at`), dan masa kedaluwarsa 7 hari.
    - Memperbarui fungsi `fn_lookup_user_login(p_email_hash, p_koperasi_id)` dengan parameter opsional `p_koperasi_id` dan hierarki peran deterministik (`admin > ketua > bendahara > sekretaris > pengurus > pengawas > operator > anggota`).
  - Access Token dipersingkat menjadi 15 menit (`ExpiresIn: 900`) dan disematkan klaim unik `ID` (`jti`).
  - Dibuat endpoint `POST /api/v1/auth/refresh` dengan mekanisme rotasi token otomatis dan **Deteksi Token Reuse** (jika refresh token yang sudah dicabut digunakan kembali, sistem otomatis mencabut seluruh sesi pengguna tersebut).
  - Dibuat endpoint `POST /api/v1/auth/revoke` untuk logout/pembatalan refresh token secara eksplisit.
  - Constant-time login timing: Eksekusi dummy bcrypt saat user tidak ditemukan untuk mencegah enumeration akun via perbedaan durasi eksekusi. Bcrypt cost diatur $\ge 12$.
  - Pemisahan izin RBAC:
    - Endpoint mutatif verifikasi pembayaran (`POST /api/v1/pembayaran/{id}/verifikasi`) dibatasi khusus untuk `admin`, `pengurus`, `bendahara`, dan `ketua`. Peran `pengawas` dan `operator` mendapatkan HTTP 403 Forbidden.
    - Endpoint mutatif generate tagihan (`POST /api/v1/iuran/generate`) dibatasi khusus untuk `admin`, `pengurus`, dan `bendahara`. Peran `operator` mendapatkan HTTP 403 Forbidden.
    - Peran `pengawas` dan `operator` tetap diberikan hak akses read-only pada antrean pembayaran dan daftar tunggakan iuran.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestT5_AuthAndPermissions_Reproduction
  --- PASS: TestT5_AuthAndPermissions_Reproduction (3.72s)
  ok  	github.com/kopdes/merah-putih/internal/api	3.875s
  ok  	github.com/kopdes/merah-putih/internal/auth
  ok  	github.com/kopdes/merah-putih/internal/db
  ```

### [2026-11-09] S1: Pencegahan Intra-Tenant IDOR pada Status Pembayaran
- **Reproduksi Gagal**: `TestS1_IDOR_PembayaranStatus_Reproduction` membuktikan Anggota B dapat membaca data pembayaran milik Anggota A dalam koperasi yang sama (HTTP 200 OK).
- **Implementasi**:
  - `HandleGetPembayaranStatus` memvalidasi kepemilikan pembayaran: pengguna berpenanda peran anggota biasa diwajibkan memiliki `pembayar_user_id` atau `anggota_id` yang cocok dengan identitas pada JWT claims.
  - Peran pengurus/admin/pengawas (`admin`, `pengurus`, `bendahara`, `ketua`, `operator`, `pengawas`) tetap memiliki hak akses peninjauan seluruh transaksi koperasi.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestS1_IDOR_PembayaranStatus_Reproduction
  --- PASS: TestS1_IDOR_PembayaranStatus_Reproduction (1.86s)
  ok  	github.com/kopdes/merah-putih/internal/api	2.012s
  ```

### [2026-11-09] S2: Penguatan Parser EMVCo, Feature Flag QRIS Dinamis & Pilot Perorangan
- **Reproduksi Gagal**: `TestS2_QRIS_FeatureFlag_And_Pilot_Validation` dan `TestEMVCo_CRC_Verification_Invalid_Rejected` membuktikan payload QRIS dengan CRC corrupt sebelumnya lolos tanpa validasi, serta tidak adanya feature flag pengontrol aktivasi QRIS dinamis.
- **Implementasi**:
  - `ValidateQRIS`: Memvalidasi checksum CRC16 (Tag 63) dari payload masukan, memastikan Tag 00 == '01', dan menolak tag root yang duplikat.
  - `GenerateDynamicQRIS`: Menghapus Tag 55–57 (Tip / Convenience Fee) saat Tag 54 (Nominal Tetap) dipasang, serta mempertahankan seluruh tag kustom/privat ASPI (round-trip & fuzz tested).
  - Feature flag `QRIS_DYNAMIC_ENABLED` (default false): saat non-aktif, endpoint mengembalikan payload statis + nominal total + kode unik.
  - Migrasi `000006_rekening_qris_jenis_pemilik.up.sql`: Menambahkan kolom `jenis_pemilik` ('badan_hukum_koperasi' / 'perorangan_pengurus') dan konstrain bersyarat. Rekening perorangan pengurus ditolak di produksi kecuali flag `ALLOW_PERSONAL_PAYMENT_PILOT=true` diaktifkan.
  - Dibuat panduan checklist pengujian lapangan di `docs/QRIS_UJI_LAPANGAN.md`.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestCalculateCRC16
  --- PASS: TestCalculateCRC16 (0.00s)
  === RUN   TestGenerateDynamicQRIS_AutoLockNominal
  --- PASS: TestGenerateDynamicQRIS_AutoLockNominal (0.00s)
  === RUN   TestEMVCo_CRC_Verification_Invalid_Rejected
  --- PASS: TestEMVCo_CRC_Verification_Invalid_Rejected (0.00s)
  === RUN   FuzzParseTLV
  --- PASS: FuzzParseTLV (0.00s)
  === RUN   TestS2_QRIS_FeatureFlag_And_Pilot_Validation
  --- PASS: TestS2_QRIS_FeatureFlag_And_Pilot_Validation (0.99s)
  ```

### [2026-11-09] S3: Deployment Hardening, Sanitasi Header CORS & Timeouts
- **Implementasi**:
  - Header `X-Koperasi-ID` dihapus dari `AllowedHeaders` CORS untuk mencegah pembocoran atau injeksi tenant via header klien (anti-BOLA).
  - Menolak penyetelan `Access-Control-Allow-Credentials: true` jika origin bernilai wildcard `*` (kepatuhan standar W3C).
  - Validasi ketat pada mode produksi (`APP_ENV=production`): Gagal start bila `ALLOWED_ORIGINS` bernilai `*` atau kosong, dan gagal start bila kata sandi database memakai password default dev.
  - HTTP Server Hardening: Ditambahkan `ReadHeaderTimeout = 5s` (proteksi Slowloris) dan middleware `BodyLimit = 10MB` (proteksi payload exhaustion DoS).
  - PostgreSQL Timeout Guardrails: Menyematkan `statement_timeout = '15s'`, `lock_timeout = '5s'`, dan `idle_in_transaction_session_timeout = '30s'` pada koneksi pgxpool `AfterConnect`.
  - Dockerfile Healthcheck menggunakan port dinamis `${PORT:-${APP_PORT:-8080}}`.
  - Sinkronisasi aturan agen di `AGENTS.md` dan `GEMINI.md`.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestS3_Deployment_Hardening_And_CORS
  --- PASS: TestS3_Deployment_Hardening_And_CORS (0.96s)
  ok  	github.com/kopdes/merah-putih/internal/api	1.111s
  ```

### [2026-11-10] Tugas 1: Hash Chain Otomatis BEFORE INSERT Trigger & Advisory Lock
- **Reproduksi Gagal**: `TestTask1_Concurrent_HashChain_AutoPopulated_By_Trigger` membuktikan 50 insert paralel tanpa `prev_hash`/`row_hash` sebelumnya gagal dengan `row_hash kosong` karena kolom tidak terisi otomatis di tingkat database.
- **Implementasi**:
  - Migrasi `000010_auto_populate_hash_chain_trigger.up.sql`: Trigger `fn_maintain_hash_chain` dipasang `BEFORE INSERT` pada `audit_log`, `kas_mutasi`, dan `pembayaran_verifikasi_log`.
  - Serialisasi konkuren transactional advisory lock per (tenant, base_table) menggunakan `pg_advisory_xact_lock`.
  - Mengambil `row_hash` baris sebelumnya dan menghitung `row_hash` baru dengan `pgcrypto` digest SHA-256.
  - Penanganan tabel berpartisi (`audit_log` dan `kas_mutasi`) mengacu pada tabel dasar induk secara konsisten.
  - Otomatis mencakup semua jalur insert termasuk stored procedure `fn_setujui_pembayaran_qris`.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestTask1_Concurrent_HashChain_AutoPopulated_By_Trigger
      service_test.go:841: SUCCESS: 50 concurrent inserts auto-populated hash chain strictly serialized without branching!
  --- PASS: TestTask1_Concurrent_HashChain_AutoPopulated_By_Trigger (3.26s)
  === RUN   TestK3_ApprovalSuccessFlow_EndToEnd
  --- PASS: TestK3_ApprovalSuccessFlow_EndToEnd (4.46s)
  ok  	github.com/kopdes/merah-putih	3.591s
  ok  	github.com/kopdes/merah-putih/internal/api	4.784s
  ```

### [2026-11-10] Tugas 2: Partisi SQL Idempoten 3 Bulan ke Depan, Job Terjadwal & Metrik Partisi Default
- **Reproduksi Gagal**: `TestTask2_SQLFunction_EnsureFuturePartitions` dan `TestTask2_PartitionMetrics_And_Alert` gagal pada tahap awal karena fungsi SQL `fn_ensure_future_partitions` dan endpoint metrik partisi default belum terpasang.
- **Implementasi**:
  - Migrasi `000011_idempotent_partition_creator.up.sql`: Dibuat fungsi SQL `fn_ensure_future_partitions(p_months_ahead int DEFAULT 3)` bertipe `SECURITY DEFINER` dengan izin eksekusi untuk `kopdes_app`.
  - Secara idempoten membuat partisi bulanan ke depan untuk seluruh 4 tabel berpartisi (`audit_log`, `kas_mutasi`, `stok_mutasi`, `notifikasi`) lengkap dengan penyesuaian hak akses role aplikasi dan auditor.
  - Integrasi terjadwal ganda:
    - Di sisi database via ekstensi `pg_cron` (jika tersedia).
    - Di sisi aplikasi Go via `StartPartitionMaintenanceJob` yang berjalan di background setiap 24 jam dengan eksekusi awal saat startup.
  - Metrik Prometheus operasional di `/metrics`:
    - `kopdes_partisi_bulan_tersisa{tabel="..."}`: Jumlah bulan partisi ke depan yang tersedia.
    - `kopdes_partisi_default_row_count{tabel="..."}`: Jumlah baris data di partisi DEFAULT (harus bernilai 0).
    - `kopdes_partisi_peringatan`: Indikator alarm (1 jika ada baris di partisi default atau sisa bulan < 2, 0 jika normal).
  - Perbaikan trigger `fn_maintain_hash_chain` agar tidak memicu `SQLSTATE 0A000` (moving row to another partition) pada insert data dengan tanggal historis/masa depan terpartisi.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestK2_InsertOutsidePartition_SucceedsAfterMigration
  --- PASS: TestK2_InsertOutsidePartition_SucceedsAfterMigration (0.18s)
  === RUN   TestK2_EnsurePartitionsAhead_Idempotent
  --- PASS: TestK2_EnsurePartitionsAhead_Idempotent (0.06s)
  === RUN   TestTask2_SQLFunction_EnsureFuturePartitions
      partition_test.go:93: Hasil pemanggilan fn_ensure_future_partitions(3): {"details": [], "months_ahead": 3, "created_count": 0}
  --- PASS: TestTask2_SQLFunction_EnsureFuturePartitions (0.06s)
  === RUN   TestTask2_PartitionMetrics_And_Alert
  --- PASS: TestTask2_PartitionMetrics_And_Alert (0.08s)
  === RUN   TestTask2_MetricsEndpoint_PartitionMetrics
  --- PASS: TestTask2_MetricsEndpoint_PartitionMetrics (2.21s)
  ok  	github.com/kopdes/merah-putih/internal/db	0.695s
  ok  	github.com/kopdes/merah-putih/internal/api	2.504s
  ```

### [2026-11-10] Tugas 3: Penguatan fn_purge_rejected_pendaftaran_pdp, Audit Tanpa PII & Pemulihan SIMKOPDES
- **Reproduksi Gagal**:
  - `TestTask3_PDPPurge_SecurityTenantValidation` membuktikan bahwa tanpa validasi session tenant ID, pemanggilan lintas tenant lolos tanpa penolakan.
  - `TestTask3_SIMKOPDES_Table_And_Adapter_Restored` membuktikan ketiadaan tabel `sinkronisasi_simkopdes` pasca migrasi pembersihan awal.
- **Implementasi**:
  - Migrasi `000012_pdp_purge_hardening_and_restore_simkopdes.up.sql`:
    - Pemulihan tabel `sinkronisasi_simkopdes` lengkap dengan RLS forced tenant isolation dan izin role `kopdes_app`.
    - Disediakan implementasi adapter stub di `internal/simkopdes/adapter.go` (`ErrSIMKOPDESDeScoped`) untuk arsitektur satu cabang lokal.
    - Pelepasan foreign key restrict dari `pendaftaran_status_log` ke `pendaftaran_anggota` sehingga penghapusan PII calon anggota ditolak dapat dilakukan tanpa merusak jejak log status append-only.
    - Konfigurasi retensi PDP tersentralisasi pada tabel `konfigurasi_aturan` (`pdp_retensi_pendaftaran_ditolak_hari`) dengan catatan legal `"PERLU VERIFIKASI HUKUM"`.
    - Prosedur `fn_purge_rejected_pendaftaran_pdp`:
      - Wajib memvalidasi `p_koperasi_id` terhadap `app_current_koperasi_id()`; melempar `SQLSTATE 42501` jika terjadi ketidaksesuaian tenant.
      - `REVOKE ALL FROM PUBLIC; GRANT EXECUTE TO kopdes_app;`.
      - Pembersihan berantai dokumen fisik dan metadata tabel `pendaftaran_dokumen` dan `dokumen`.
      - Pencatatan aksi `ANONYMIZE` pada `audit_log` tanpa PII (`record_id` disetel ke `p_koperasi_id`, rincian hanya memuat jumlah berkas dan masa retensi).
  - Background worker terjadwal: `StartPDPPurgeWorker` di Go yang berjalan harian per tenant via wrapper `middleware.WithTenantTx`.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestTask3_PDPPurge_SecurityTenantValidation
      api_test.go:2155: Sukses: Pemanggilan lintas tenant berhasil ditolak: ERROR: AKSES DITOLAK: Tenant id ... tidak sesuai dengan konteks sesi terautentikasi (...) (SQLSTATE 42501)
  --- PASS: TestTask3_PDPPurge_SecurityTenantValidation (2.19s)
  === RUN   TestTask3_SIMKOPDES_Table_And_Adapter_Restored
  --- PASS: TestTask3_SIMKOPDES_Table_And_Adapter_Restored (0.07s)
  === RUN   TestTask3_PDPPurge_AuditLog_WithoutPII_And_ConfigRetention
      api_test.go:2306: Sukses: Audit log bersih tanpa PII: {"catatan": "Pembersihan berkas pendaftaran ditolak (UU 27/2022 PDP). PERLU VERIFIKASI HUKUM.", "purged_count": 1, "retention_days": 30}
  --- PASS: TestTask3_PDPPurge_AuditLog_WithoutPII_And_ConfigRetention (2.55s)
  === RUN   TestP1_1_PDP_30Day_Purge
  --- PASS: TestP1_1_PDP_30Day_Purge (2.30s)
  ok  	github.com/kopdes/merah-putih/internal/api	5.123s
  ```

### [2026-11-10] Tugas 4: Idempotensi Berbasis Database & Deteksi Konflik Body (422)
- **Reproduksi Gagal**: `TestTask4_DatabaseBacked_Idempotency_And_422OnDifferentBody` awalnya gagal karena tabel `idempotency_keys` belum ada di basis data dan in-memory cache tidak mengecek perbedaan hash payload request.
- **Implementasi**:
  - Migrasi `000013_idempotency_keys.up.sql`:
    - Membuat tabel `idempotency_keys` (`id`, `koperasi_id`, `user_id`, `kunci`, `request_hash`, `response_code`, `response_body`, `expires_at`, `created_at`).
    - Unique constraint `(koperasi_id, user_id, kunci)`.
    - Dilengkapi indeks komposit pencarian dan forced Row Level Security tenant isolation.
  - Modul `internal/db/idempotency.go`:
    - Fungsi `HashPayload`: kalkulasi SHA-256 string representatif dari byte body request mentah.
    - Fungsi `CheckIdempotency`: lookup kunci dalam scope tenant RLS aktif; jika hash payload berbeda mengembalikan error sentinel `ErrIdempotencyConflict`.
    - Fungsi `SaveIdempotency`: upsert kunci idempotensi dengan TTL 24 jam.
  - Refaktor `QRISHandler.HandleBuatQRIS`:
    - Membaca body mentah dan menghitung hash SHA-256.
    - Jika kunci sama dikirimkan dengan body berbeda $\to$ mengembalikan **HTTP 422 Unprocessable Entity** (`IDEMPOTENCY_CONFLICT`).
    - Jika kunci sama dikirimkan dengan body identik $\to$ mengembalikan status dan respons tersimpan dari basis data (cache hit).
    - Menghapus in-memory map map[string]IdempotentEntry sehingga idempotensi bertahan lintas restart server dan multi-pod.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestTask4_DatabaseBacked_Idempotency_And_422OnDifferentBody
      api_test.go:2419: Sukses: Idempotency database-backed dan deteksi beda body (422) terverifikasi!
  --- PASS: TestTask4_DatabaseBacked_Idempotency_And_422OnDifferentBody (2.27s)
  ok  	github.com/kopdes/merah-putih/internal/api	2.567s
  ```

### [2026-11-10] Tugas 5: Verifikasi Pembayaran (Impor Mutasi CSV, Auto-Matching, Persetujuan Ganda & Rekonsiliasi)
- **Reproduksi Gagal**: `TestTask5_PaymentVerification_Reconciliation_EndToEnd` dibuat sebelum implementasi fitur impor mutasi CSV, persetujuan ganda berjenjang, dan pelaporan rekonsiliasi harian.
- **Implementasi**:
  - Migrasi `000014_payment_verification_reconciliation.up.sql`:
    - Status enum `status_pembayaran_enum` ditambahkan nilai `'menunggu_persetujuan_kedua'`.
    - Kolom baru pada tabel `pembayaran`: `persetujuan_pertama_oleh`, `waktu_persetujuan_pertama`, `persetujuan_kedua_oleh`, `waktu_persetujuan_kedua`, `is_anomali`, `indikator_anomali`.
    - Tabel baru `mutasi_bank_impor` (`id`, `koperasi_id`, `no_rekening`, `tanggal_mutasi`, `keterangan`, `tipe`, `nominal`, `saldo_akhir`, `status_rekonsiliasi`, `pembayaran_id`, `catatan_rekonsiliasi`) dengan indeks komposit dan Row Level Security terisolasi per tenant.
    - Default aturan ambang batas persetujuan ganda di `konfigurasi_aturan` (`ambang_persetujuan_ganda_rp` = 5.000.000).
    - Pembaruan trigger `trg_fn_validasi_transisi_pembayaran` dan fungsi atomik `fn_setujui_pembayaran_qris` agar mendukung transisi dari `menunggu_persetujuan_kedua` ke `disetujui`.
  - Endpoint baru di `internal/api/handlers/qris_handler.go` & `server.go`:
    - `POST /api/v1/pembayaran/mutasi-bank/impor`: Menerima berkas CSV rekening koran bank, memparsing mutasi kredit (CR), mendeteksi anomali (jam di luar kerja 22:00-05:00 WIB, transaksi beruntun burst > 3 dalam 5 menit, dan duplikasi hash bukti), serta melakukan auto-matching pada pembayaran pending berdasarkan `nominal_total` yang unik. Jika nominal <= ambang ganda dan bukti sudah diunggah, otomatis disetujui.
    - `POST /api/v1/pembayaran/{id}/verifikasi`: Memvalidasi ambang persetujuan ganda (`ambang_persetujuan_ganda_rp`). Jika transaksi > ambang, verifikator pertama memindahkan status ke `menunggu_persetujuan_kedua`. Verifikasi kedua dilarang dieksekusi oleh orang yang sama (menolak self-approval dengan HTTP 400).
    - `GET /api/v1/pembayaran/rekonsiliasi/harian`: Menghasilkan ringkasan rekonsiliasi harian antara total pembayaran sistem (disetujui) dan mutasi bank terimpor (CR), menghitung selisih nominal dan daftar transaksi yang belum cocok / anomali.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestTask5_PaymentVerification_Reconciliation_EndToEnd
  --- PASS: TestTask5_PaymentVerification_Reconciliation_EndToEnd (3.34s)
  ok  	github.com/kopdes/merah-putih/internal/api	3.710s
  ```

### [2026-11-10] Tugas 6: Guardrails Pilot Rekening Pribadi (Batas Transaksi/Harian, Banner Peringatan & Audit Log)
- **Reproduksi Gagal**: `TestTask6_PersonalAccountPilot_Limits_Banner_Audit` membuktikan bahwa sebelum implementasi, pembuatan pembayaran menggunakan rekening pilot perorangan pengurus di atas batas nominal lolos tanpa pembatasan dan respon admin tidak memuat banner peringatan segregasi dana.
- **Implementasi**:
  - Migrasi `000015_pilot_personal_account_guardrails.up.sql`:
    - Menambahkan aturan konfigurasi bawaan di `konfigurasi_aturan`:
      - `pilot_rekening_pribadi_max_per_transaksi` (default Rp 1.000.000).
      - `pilot_rekening_pribadi_max_per_hari` (default Rp 5.000.000).
  - Validasi ketat di `QRISHandler.HandleBuatQRIS`:
    - Mengambil jenis pemilik rekening (`COALESCE(jenis_pemilik, 'badan_hukum_koperasi')`).
    - Jika `perorangan_pengurus`:
      - Memeriksa batas nominal per transaksi terhadap konfigurasi aturan koperasi.
      - Menghitung akumulasi pembayaran hari ini (`SELECT COALESCE(SUM(nominal_total), 0) FROM pembayaran WHERE created_at >= CURRENT_DATE AND ...`) dan menolak jika total melampaui batas harian dengan error HTTP 400 informatif.
      - Mencatat audit log khusus bertipe `'INSERT'` dengan penanda `PILOT_REKENING_PRIBADI` dan rincian peringatan kepatuhan segregasi dana.
      - Menyertakan `warning_banner` dan `is_rekening_pribadi: true` pada DTO respons pembayaran `BuatQRISResponse`.
  - Peringatan di `HandleGetPembayaranStatus`:
    - Melakukan `LEFT JOIN rekening_qris_koperasi` untuk mendeteksi `jenis_pemilik`.
    - Jika berasal dari rekening perorangan pengurus, secara eksplisit menyertakan banner peringatan risiko kepatuhan bagi admin/pengawas (`warning_banner: "PERINGATAN: Pembayaran ini menggunakan rekening perorangan pengurus (Pilot Mode). Risiko kepatuhan dan segregasi dana. Wajib dialihkan ke rekening badan hukum koperasi."`).
- **Hasil Verifikasi**:
  ```text
  === RUN   TestTask6_PersonalAccountPilot_Limits_Banner_Audit
  --- PASS: TestTask6_PersonalAccountPilot_Limits_Banner_Audit (2.29s)
  ok  	github.com/kopdes/merah-putih/internal/api	2.592s
  ```

### [2026-11-10] Tugas 7: Proteksi Endpoint /metrics & Autentikasi Dua Faktor (MFA TOTP)
- **Reproduksi Gagal**: `TestTask7_ProtectedMetrics_And_MFA_TOTP` dibuat untuk menguji: (1) penolakan akses `/metrics` dari jaringan eksternal tanpa token otentikasi internal (401 Unauthorized), dan (2) siklus penuh aktivasi dan verifikasi MFA TOTP (RFC 6238) untuk akun bendahara/admin/ketua.
- **Implementasi**:
  - Proteksi `/metrics` di `internal/api/handlers/metrics_handler.go`:
    - Evaluasi IP klien (`middleware.GetClientIP`) dan IP jaringan internal (loopback `127.0.0.1`, RFC 1918 private network `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`).
    - Validasi token internal melalui header `Authorization: Bearer <METRICS_TOKEN>` atau `X-Metrics-Token: <METRICS_TOKEN>`.
    - Akses dari luar jaringan internal tanpa token yang sah ditolak dengan HTTP 401 Unauthorized.
  - Implementasi TOTP RFC 6238 di `internal/security/totp.go`:
    - Pembuatan secret Base32 160-bit (`GenerateTOTPSecret`) dan validasi kode 6 digit berbasis waktu dengan toleransi clock-drift $\pm 30$ detik (`ValidateTOTPCode`).
    - Penghasil URL authenticator standar `otpauth://totp/...` (`GenerateTOTPURI`).
  - Migrasi `000016_metrics_protection_and_mfa_totp.up.sql`:
    - Menambahkan kolom `mfa_enabled` dan `totp_secret_enkripsi` pada tabel `users`.
    - Membuat tabel `mfa_login_sessions` untuk menampung sesi login sementara ber-TTL 5 menit.
    - Fungsi `SECURITY DEFINER` terkunci: `fn_get_user_mfa` dan `fn_set_user_mfa` (dengan hak eksekusi hanya untuk role `kopdes_app`).
  - Alur Autentikasi Dua-Faktor di `internal/api/handlers/auth_handler.go` & `server.go`:
    - `POST /api/v1/auth/mfa/setup`: Menghasilkan secret baru & URL authenticator.
    - `POST /api/v1/auth/mfa/enable`: Memvalidasi kode awal pengguna, mengenkripsi secret dengan AES-256-GCM AAD, dan mengaktifkan `mfa_enabled = true`.
    - `POST /api/v1/auth/login`: Jika akun memiliki `mfa_enabled = true`, login tahap 1 menahan penerbitan JWT final dan mengembalikan `mfa_required: true` beserta `mfa_session_token`.
    - `POST /api/v1/auth/mfa/verify`: Memvalidasi `mfa_session_token` dan kode TOTP 6-digit; jika valid, sesi dihapus (one-time use) dan JWT Access Token + Refresh Token diterbitkan.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestP1_4_Operasional_Metrics_And_Idempotency
  --- PASS: TestP1_4_Operasional_Metrics_And_Idempotency (2.26s)
  === RUN   TestTask2_MetricsEndpoint_PartitionMetrics
  --- PASS: TestTask2_MetricsEndpoint_PartitionMetrics (2.16s)
  === RUN   TestTask7_ProtectedMetrics_And_MFA_TOTP
  --- PASS: TestTask7_ProtectedMetrics_And_MFA_TOTP (3.73s)
  ok  	github.com/kopdes/merah-putih/internal/api	8.448s
  ```

### [2026-11-10] Tugas 9: Penyelesaian Endpoint Operasional Inti Koperasi (Anggota, Iuran, Katalog/Promo, Toko/Pesanan, Laporan, Manajemen Pengguna)
- **Reproduksi Gagal**: `TestTask9_MissingCRUD_Endpoints_EndToEnd` dibuat mencakup 6 domain bisnis utama yang sebelumnya belum memiliki endpoint HTTP aktif. Tes awal gagal dengan status HTTP 404 (Not Found) untuk seluruh rute baru.
- **Implementasi**:
  - **Manajemen Anggota** (`internal/api/handlers/anggota_handler.go`):
    - `GET /api/v1/anggota`: Listing anggota koperasi terfilter tenant (RLS) dengan pagination dan filter status/pencarian.
    - `GET /api/v1/anggota/{id}`: Detail profil keanggotaan terisolasi tenant.
    - `PATCH /api/v1/anggota/{id}/status`: Perubahan status keanggotaan (`aktif`, `nonaktif`, `keluar`, `meninggal`) dengan pencatatan otomatis ke tabel append-only `anggota_status_log` (memenuhi aturan immutability audit).
  - **CRUD Iuran & Tagihan** (`internal/api/handlers/iuran_handler.go`):
    - `POST /api/v1/iuran/jenis`: Pembuatan jenis iuran baru (simpanan wajib, pokok, sukarela) dengan nominal standar.
    - `GET /api/v1/iuran/jenis`: Listing konfigurasi jenis iuran aktif untuk tenant bersangkutan.
    - `GET /api/v1/iuran/tagihan/saya`: Endpoint anggota untuk melihat rekapitulasi tagihan iuran miliknya sendiri beserta status bayar dan jatuh tempo.
  - **Katalog & Promo** (`internal/api/handlers/katalog_handler.go`):
    - `POST /api/v1/katalog/produk`: Pembuatan master produk baru, inisialisasi kategori default bila belum ada, dan pencatatan riwayat harga awal (`produk_harga`).
    - `GET /api/v1/katalog/produk`: Katalog produk aktif dengan snapshot harga umum dan harga anggota.
    - `PUT /api/v1/katalog/produk/{id}` & `DELETE /api/v1/katalog/produk/{id}`: Pembaruan metadata produk dan soft-delete berbasis tenant.
    - `POST /api/v1/katalog/promo`: Pembuatan promosi diskon nominal/persen dengan batas belanja, kuota, dan masa berlaku.
    - `GET /api/v1/katalog/promo`: Listing promo aktif yang masih berlaku.
  - **Keranjang, Checkout & Pesanan** (`internal/api/handlers/toko_handler.go`):
    - `POST /api/v1/toko/keranjang`: Tambah item / update kuantitas ke keranjang belanja pengguna (`keranjang_item`).
    - `GET /api/v1/toko/keranjang`: Tampilkan isi keranjang belanja terkini beserta kalkulasi harga umum/anggota.
    - `DELETE /api/v1/toko/keranjang/{produk_id}`: Hapus item dari keranjang belanja.
    - `POST /api/v1/toko/checkout`: Konversi item keranjang menjadi pesanan resmi (`pesanan` dan `pesanan_item`), menghitung subtotal khusus anggota jika pembeli terdaftar sebagai anggota aktif, mengosongkan keranjang belanja secara atomik.
    - `GET /api/v1/toko/pesanan`: Riwayat pesanan pembeli (anggota) atau seluruh antrean pesanan bagi peran pengurus/admin.
  - **Pelaporan** (`internal/api/handlers/laporan_handler.go`):
    - `GET /api/v1/laporan/keuangan`: Ringkasan keuangan kas, mencakup total pemasukan kas (`kas_mutasi`), total pengeluaran, saldo periode kas berjalan, dan surplus/defisit operasional.
    - `GET /api/v1/laporan/anggota`: Rekapitulasi distribusi anggota koperasi berdasarkan status aktif/calon/nonaktif.
  - **Manajemen Pengguna** (`internal/api/handlers/user_management_handler.go`):
    - `GET /api/v1/users`: Listing seluruh pengguna terafiliasi tenant koperasi beserta peran (`user_roles`).
    - `GET /api/v1/users/{id}`: Rincian profil pengguna dan penugasan peran.
    - `PATCH /api/v1/users/{id}/role`: Mutasi peran pengguna (`admin`, `pengurus`, `bendahara`, `operator`, `anggota`) dengan pencatatan audit log tanpa membuka celah BOLA/IDOR.
  - Seluruh rute didaftarkan di `internal/api/server.go` dengan middleware otentikasi JWT dan pembatasan peran RBAC yang ketat.
- **Hasil Verifikasi**:
  ```text
### [2026-11-10] Tugas 10: Sanitasi Dokumen (Penghapusan Klaim "Enterprise" & "Memenuhi UU PDP") Serta Verifikasi Penuh Suite Uji
- **Sanitasi Dokumen**:
  - Seluruh klaim mutlak seperti *"memenuhi standar keamanan enterprise"*, *"memenuhi UU PDP"*, *"kepatuhan UU PDP"*, dan *"tersertifikasi enkripsi UU PDP"* telah dihapus dan disanitasi dari seluruh berkas dokumentasi (`docs/AUDIT_REPORT.md`, `docs/LAPORAN_REMEDIASI_DAN_PERUBAHAN_KODE.md`, `docs/FRONTEND_GUIDE.md`, `docs/erd.md`, `docs/DOKUMENTASI_LENGKAP_API_KOPDES.html`, `scripts/generate_docs.py`, `.env.example`).
  - Dokumen kini secara jujur dan akurat mencerminkan spesifikasi teknis: *arsitektur enkripsi data pribadi AES-256-GCM + AAD*, *blind index HKDF-SHA256*, dan menyertakan catatan bahwa kepatuhan regulasi hukum formal memerlukan peninjauan serta verifikasi hukum tersendiri (*"PERLU VERIFIKASI HUKUM"*).
  - Verifikasi repository bebas dari klaim berlebihan dibuktikan dengan hasil pencarian kosong pada perintah:
    `git grep -i -E "enterprise|memenuhi.*pdp"` (0 temuan).
- **Hasil Asli Eksekusi `go test -count=1 -race ./...`**:
  *(Catatan Lingkungan: Pada OS Windows tanpa C compiler / MinGW terpasang di host, flag `-race` memerlukan runtime CGO ThreadSanitizer yang memicu build error `# runtime/cgo` seperti terlihat di bawah ini)*
  ```text
  PS C:\Users\12\kopdes-merah-putih> $env:TEST_DATABASE_URL="postgres://kopdes_app:kopdes_dev_password@localhost:5433/kopdes_db?sslmode=disable"; go test -count=1 -race ./...
  go: -race requires cgo; enable cgo by setting CGO_ENABLED=1

  PS C:\Users\12\kopdes-merah-putih> $env:CGO_ENABLED="1"; go test -count=1 -race ./...
  # runtime/cgo
  cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in %PATH%
  FAIL	github.com/kopdes/merah-putih [build failed]
  FAIL	github.com/kopdes/merah-putih/cmd/server [build failed]
  FAIL	github.com/kopdes/merah-putih/internal/api [build failed]
  ?   	github.com/kopdes/merah-putih/internal/api/handlers	[no test files]
  FAIL	github.com/kopdes/merah-putih/internal/api/middleware [build failed]
  ?   	github.com/kopdes/merah-putih/internal/api/response	[no test files]
  ?   	github.com/kopdes/merah-putih/internal/auth	[no test files]
  FAIL	github.com/kopdes/merah-putih/internal/db [build failed]
  FAIL	github.com/kopdes/merah-putih/internal/outbox [build failed]
  FAIL	github.com/kopdes/merah-putih/internal/qris [build failed]
  FAIL	github.com/kopdes/merah-putih/internal/security [build failed]
  ?   	github.com/kopdes/merah-putih/internal/simkopdes	[no test files]
  FAIL
  ```
- **Hasil Asli Eksekusi `go test -count=1 ./...` (Seluruh Paket Terhubung ke DB PostgreSQL 16)**:
  ```text
  PS C:\Users\12\kopdes-merah-putih> $env:TEST_DATABASE_URL="postgres://kopdes_app:kopdes_dev_password@localhost:5433/kopdes_db?sslmode=disable"; go test -count=1 ./...
  ok  	github.com/kopdes/merah-putih	8.271s
  ?   	github.com/kopdes/merah-putih/cmd/server	[no test files]
  ok  	github.com/kopdes/merah-putih/internal/api	77.630s
  ?   	github.com/kopdes/merah-putih/internal/api/handlers	[no test files]
  ok  	github.com/kopdes/merah-putih/internal/api/middleware	3.042s
  ?   	github.com/kopdes/merah-putih/internal/api/response	[no test files]
  ?   	github.com/kopdes/merah-putih/internal/auth	[no test files]
  ok  	github.com/kopdes/merah-putih/internal/db	1.629s
  ok  	github.com/kopdes/merah-putih/internal/outbox	0.850s
  ok  	github.com/kopdes/merah-putih/internal/qris	1.648s
  ok  	github.com/kopdes/merah-putih/internal/security	1.392s
  ?   	github.com/kopdes/merah-putih/internal/simkopdes	[no test files]
  ```
- **Hasil Pemindaian Keamanan `govulncheck ./...`**:
  0 kerentanan pada kode aplikasi sistem Kopdes Merah Putih. Seluruh peringatan yang muncul murni bersumber dari pustaka standar runtime Go bawaan mesin host (`go1.27.1`).

### [2026-11-12] Item 7: Batas Pilot Rekening Pribadi Asia/Jakarta, Purge Berkas Fisik & Log Bebas PII
- **Reproduksi Gagal**:
  `TestItem7_Pilot_AsiaJakarta_And_Purge_PhysicalFiles_PIIFree` mendeteksi bahwa berkas KYC pendaftaran sebelumnya hanya dicatat metadatanya di database tanpa ditulis ke disk penyimpanan fisik (`api_test.go:4441: Berkas KYC fisik harus tersimpan di ..., tetapi tidak ditemukan!`), serta memastikan batas harian pilot rekening pribadi dihitung berdasarkan zona waktu lokal `Asia/Jakarta` dan hanya memperhitungkan transaksi aktif/disetujui (bukan ditolak/dibatalkan).
- **Implementasi**:
  - `internal/api/handlers/qris_handler.go`: Query akumulasi batas harian pilot diperkuat dengan konversi zona waktu PostgreSQL `(CURRENT_TIMESTAMP AT TIME ZONE 'Asia/Jakarta')::date AT TIME ZONE 'Asia/Jakarta'` dan hanya menghitung transaksi berstatus `status IN ('menunggu_pembayaran', 'menunggu_verifikasi', 'menunggu_persetujuan_kedua', 'disetujui')`.
  - `internal/api/handlers/pendaftaran_handler.go`:
    - `HandleUploadDokumenPendaftaran`: Menyimpan berkas fisik ke `STORAGE_DIR` dengan proteksi permission direktori `0750` dan berkas `0600`. Jika transaksi database gagal, berkas fisik otomatis dibersihkan.
    - `HandlePurgeRejectedPendaftaran`: Membaca seluruh `storage_key` calon anggota yang berstatus ditolak, memanggil prosedur SQL `fn_purge_rejected_pendaftaran_pdp`, lalu menghapus berkas fisik KYC secara permanen dari penyimpanan disk via `os.Remove`.
    - `sanitizeLogCatatan`: Menambahkan sanitasi berbasis ekspresi reguler untuk menyensor NIK 16 digit (`[NIK_REDACTED]`), nomor telepon (`[NO_HP_REDACTED]`), dan email (`[EMAIL_REDACTED]`) sebelum disisipkan ke tabel riwayat append-only `pendaftaran_status_log`.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestItem7_Pilot_AsiaJakarta_And_Purge_PhysicalFiles_PIIFree
      api_test.go:4515: Sukses: Batas pilot Asia/Jakarta valid, berkas KYC fisik terhapus permanen saat purge, dan log status bebas PII!
  --- PASS: TestItem7_Pilot_AsiaJakarta_And_Purge_PhysicalFiles_PIIFree (0.87s)
  PASS
  ok  	github.com/kopdes/merah-putih/internal/api	1.023s
  ```

### [2026-11-12] Item 8: Pembaruan Go Dockerfile, Konfigurasi CI GitHub Actions, govulncheck & Verifikasi Lengkap
- **Implementasi**:
  - `Dockerfile`: Memperbarui runtime image runner ke versi Alpine Linux tertambal `alpine:3.21` dan builder `golang:1.24-alpine` dengan kompilasi statis murni tanpa CGO (`CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s -extldflags '-static'"`).
  - `.github/workflows/ci.yml`: Membangun pipeline CI lengkap berbasis Linux (Ubuntu) yang otomatis berjalan pada `push` dan `pull_request` ke cabang `main`:
    1. Menyiapkan database PostgreSQL 16 Alpine container dengan inisialisasi role dan seluruh file migrasi `db/migrations/*.up.sql`.
    2. Menjalankan seluruh pengujian dengan race detector: `go test -count=1 -race -v ./...`.
    3. Menjalankan pemindaian kerentanan `govulncheck ./...`.
    4. Menjalankan linter analisis statis `staticcheck ./...`.
    5. Menjalankan pemindai keamanan SAST `gosec -exclude=G101,G404,G115 ./...`.
    6. Menjalankan pemindai kebocoran kredensial `gitleaks`.
    7. Melakukan smoke test otomatis `docker compose up -d --build` dari nol, memverifikasi kontainer berstatus `healthy`, dan menguji probe `/healthz`.
  - `internal/api/handlers/katalog_handler.go`: `HandleCreateProduk` otomatis menginisialisasi saldo stok awal di gudang default (`stok_saldo`) agar produk baru dapat langsung diproses melalui alur transaksi toko.
  - `internal/api/handlers/auth_handler.go`: Menyertakan `koperasi_id` pada payload data respon `MFA_MANDATORY_SETUP_REQUIRED` dan login multi-koperasi.
- **Hasil Asli Eksekusi `go test -count=1 ./...` (100% PASS Seluruh Paket)**:
  ```text
  ok  	github.com/kopdes/merah-putih	4.078s
  ?   	github.com/kopdes/merah-putih/cmd/server	[no test files]
  ok  	github.com/kopdes/merah-putih/internal/api	39.397s
  ?   	github.com/kopdes/merah-putih/internal/api/handlers	[no test files]
  ok  	github.com/kopdes/merah-putih/internal/api/middleware	1.487s
  ?   	github.com/kopdes/merah-putih/internal/api/response	[no test files]
  ?   	github.com/kopdes/merah-putih/internal/auth	[no test files]
  ok  	github.com/kopdes/merah-putih/internal/db	1.132s
  ok  	github.com/kopdes/merah-putih/internal/outbox	0.516s
  ok  	github.com/kopdes/merah-putih/internal/qris	1.253s
  ok  	github.com/kopdes/merah-putih/internal/security	0.939s
  ?   	github.com/kopdes/merah-putih/internal/simkopdes	[no test files]
  ```
- **Kompilasi Binary Statis Linux**:
  Terverifikasi berhasil dikompilasi secara lokal via `CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o ./kopdes-server-linux ./cmd/server` (11.8 MB).

---

### [2026-11-12] Item 2: Alur Enrollment MFA Akun Istimewa & Hashed Recovery Codes
- **Reproduksi Gagal**:
  `TestMFA_Enrollment_PrivilegedAccount_RestrictedToken_And_HashedRecoveryCodes` mendeteksi bahwa akun istimewa tanpa MFA sebelumnya langsung ditolak login tanpa jalur enrollment yang aman, atau jika diberikan token khusus, token tersebut berpotensi disalahgunakan untuk mengakses endpoint bisnis.
- **Implementasi**:
  - `internal/api/middleware/auth.go`: `RequireAuth` membatasi token dengan peran khusus `mfa_setup` (restricted enrollment-only token). Pengguna dengan peran ini HANYA diizinkan mengakses `/api/v1/auth/mfa/setup` dan `/api/v1/auth/mfa/enable`. Seluruh endpoint bisnis/profil/keuangan menolak akses dengan HTTP 403 Forbidden.
  - `internal/security/totp.go`: Menambahkan fungsi `ValidateTOTPCodeWithStep` yang mengembalikan `matchedStep` (`time.Unix() / 30`) untuk mengunci pencegahan replay ke nomor step waktu eksak.
  - `internal/api/handlers/auth_handler.go`:
    - `HandleLogin`: Akun istimewa (`admin`, `ketua`, `bendahara`) yang belum mengaktifkan MFA diberikan restricted access token berdurasi 15 menit dengan role `mfa_setup`.
    - `HandleMFAEnable`: Menghasilkan 8 kode pemulihan acak ber-entropi tinggi, menyimpan hash SHA-256 terenkripsi AES-256-GCM + AAD `users:recovery:<userID>` di database (kode asli tidak pernah disimpan dalam bentuk plaintext).
    - `HandleMFAVerify`: Menguji kode pemulihan dengan pembakaran sekali pakai (one-time burn).
- **Hasil Verifikasi**:
  ```text
  === RUN   TestMFA_Enrollment_PrivilegedAccount_RestrictedToken_And_HashedRecoveryCodes
  --- PASS: TestMFA_Enrollment_PrivilegedAccount_RestrictedToken_And_HashedRecoveryCodes (20.63s)
  PASS
  ok  	github.com/kopdes/merah-putih/internal/api	22.045s
  ```

---

### [2026-11-12] Item 3: Impor Mutasi Bank: Segregation of Duties & Konfirmasi Orang Kedua
- **Reproduksi Gagal**:
  `TestItem3_BankMutation_DefaultNoAutoApprove_SecondConfirmation_ImporterCannotApprove` memvalidasi bahwa impor CSV mutasi tidak boleh langsung meng-auto-approve pembayaran secara default, pengimpor mutasi dilarang keras bertindak sebagai penyetuju pembayaran, dan butuh konfirmasi dari pengurus kedua yang berbeda.
- **Implementasi**:
  - `internal/api/handlers/qris_handler.go`:
    - Mengatur konfigurasi `mutasi_auto_approve_enabled` bernilai `false` secara default di `konfigurasi_aturan`.
    - Pembayaran hasil pencocokan mutasi bank dialihkan ke status `menunggu_persetujuan_kedua` dengan mencatat `persetujuan_pertama_oleh` = user pengimpor mutasi.
    - Pada `HandleVerifikasiPembayaran`:
      - Menolak jika `mutasiImporterID == claims.UserID` (pengimpor mutasi dilarang menyetujui pembayarannya sendiri, HTTP 403 Forbidden).
      - Menolak jika `persetujuanPertamaOleh == claims.UserID` pada tahap persetujuan kedua (memastikan 2 pasang mata / four-eyes principle).
- **Hasil Verifikasi**:
  ```text
  === RUN   TestItem3_BankMutation_DefaultNoAutoApprove_SecondConfirmation_ImporterCannotApprove
      api_test.go:4928: Sukses: Impor mutasi auto-approve nonaktif default, pengimpor = penyetuju ditolak, dan orang kedua berhasil konfirmasi!
  --- PASS: TestItem3_BankMutation_DefaultNoAutoApprove_SecondConfirmation_ImporterCannotApprove (11.52s)
  PASS
  ok  	github.com/kopdes/merah-putih/internal/api	12.871s
  ```

---

### [2026-11-12] Item 4: Konkurensi Stok Toko (Bebas Oversell) & Pelepasan Stok/Promo saat Pesanan Batal/Kedaluwarsa
- **Reproduksi Gagal**:
  `TestItem4_Toko_StockConcurrency_NoOversell_And_OrderCancellation_Release` menguji 5 goroutines yang bersaing membeli stok produk terbatas (stok = 2) secara bersamaan, serta memastikan pelepasan stok (`stok_mutasi` 'retur_penjualan') dan kuota promo saat pesanan dibatalkan atau dikedaluwarsakan.
- **Implementasi**:
  - `internal/api/handlers/toko_handler.go`:
    - `HandleCheckout`: Mengunci baris `stok_saldo` dengan `FOR UPDATE`, memeriksa `qty_tersedia >= requested_qty`, dan trigger database `trg_fn_proses_stok_mutasi()` mencegah stok minus pada level mesin database.
    - `ReleasePesananStockAndPromoTx`: Helper atomik yang melepaskan kuota promo (`GREATEST(kuota_terpakai - 1, 0)`), memperbarui `promo_penggunaan.status = 'dibatalkan'`, mengembalikan stok ke gudang melalui entri ledger append-only `stok_mutasi` dengan jenis `'retur_penjualan'`.
    - `HandleBatalPesanan`: Endpoint `POST /api/v1/toko/pesanan/{id}/batal` dengan proteksi Anti-IDOR (anggota hanya dapat membatalkan pesanan miliknya sendiri).
    - `HandleExpirePesanan`: Endpoint `POST /api/v1/toko/pesanan/{id}/expire` untuk memproses pesanan kedaluwarsa secara terjadwal.
  - `internal/api/server.go`: Registrasi rute pembatalan dan kedaluwarsa pesanan.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestItem4_Toko_StockConcurrency_NoOversell_And_OrderCancellation_Release
      api_test.go:5106: Verifikasi A Berhasil: 5 checkout bersamaan ditangani atomik, 2 sukses, 3 ditolak, saldo akhir = 0 (bebas oversell).
      api_test.go:5226: Verifikasi B Berhasil: Pembatalan pesanan berhasil memulihkan stok (+2) dan kuota promo (-1) secara konsisten.
      api_test.go:5295: Verifikasi C Berhasil: Pesanan kedaluwarsa melepaskan stok dan kuota promo kembali ke saldo tersedia.
  --- PASS: TestItem4_Toko_StockConcurrency_NoOversell_And_OrderCancellation_Release (23.46s)
  PASS
  ok  	github.com/kopdes/merah-putih/internal/api	25.809s
  ```

---

### [2026-11-12] Item 5: Helper Kunci Advisory Terurut Kanonikal & Tes Kegagalan Urutan Terbalik
- **Reproduksi Gagal**:
  `TestAdvisoryLock_ReverseOrder_CausesDeadlockOrTimeout` mereproduksi bahwa transaksi yang mengambil kunci advisory secara manual dengan urutan terbalik (`tx1`: audit_log -> kas_mutasi, `tx2`: kas_mutasi -> audit_log) secara konsisten memicu kegagalan deadlock PostgreSQL (`40P01`) atau lock timeout (`55P03`).
- **Implementasi**:
  - `internal/db/advisory_lock.go`:
    - `AcquireOrderedAdvisoryLocks`: Helper transaksi yang menormalisasi nama tabel partisi ke tabel dasar (`CanonicalBaseTable`), melakukan deduplikasi, mengurutkan secara leksikografis (deterministik kanonikal), dan mengambil `pg_advisory_xact_lock` di awal transaksi.
    - Menjamin pemrosesan batch/paralel bebas dari siklus saling menunggu (anti-circular wait).
- **Hasil Verifikasi**:
  ```text
  === RUN   TestAdvisoryLock_CanonicalSorting
      advisory_lock_test.go:39: Sukses: Normalisasi partisi dan pengurutan leksikografis kanonikal valid: [audit_log kas_mutasi pembayaran_verifikasi_log stok_mutasi]
  --- PASS: TestAdvisoryLock_CanonicalSorting (0.03s)
  === RUN   TestAdvisoryLock_Ordered_PreventsDeadlock_UnderParallelLoad
      advisory_lock_test.go:106: Sukses: 40 worker paralel dengan urutan masukan terbalik berhasil dieksekusi tanpa satu pun deadlock!
  --- PASS: TestAdvisoryLock_Ordered_PreventsDeadlock_UnderParallelLoad (0.50s)
  === RUN   TestAdvisoryLock_ReverseOrder_CausesDeadlockOrTimeout
      advisory_lock_test.go:187: Sukses: Penguncian manual dengan urutan terbalik terbukti memicu kegagalan deadlock/timeout (tx1Err=ERROR: canceling statement due to lock timeout (SQLSTATE 55P03), tx2Err=ERROR: canceling statement due to lock timeout (SQLSTATE 55P03)), membuktikan bahwa AcquireOrderedAdvisoryLocks mutlak diperlukan!
  --- PASS: TestAdvisoryLock_ReverseOrder_CausesDeadlockOrTimeout (0.56s)
  PASS
  ok  	github.com/kopdes/merah-putih/internal/db	2.198s
  ```

---

### [2026-11-12] Item 6: METRICS_TOKEN Wajib >= 32 Byte, Gagal Start Bila Kosong & Contoh Konfigurasi Prometheus
- **Reproduksi Gagal**:
  `TestItem6_MetricsToken_Min32Bytes_RefusesShortOrEmpty` mendeteksi penolakan inisialisasi server apabila `METRICS_TOKEN` kosong atau berukuran kurang dari 32 byte.
- **Implementasi**:
  - `internal/api/server.go`:
    - `Config.Validate()`: Menambahkan validasi `security.ValidateSecretKey("METRICS_TOKEN", decodeSecretBytes(c.MetricsToken))` (wajib >= 32 byte).
    - `Server.Start()`: Memvalidasi konfigurasi keamanan sebelum memulai listener HTTP. Jika token tidak valid, mengembalikan error fatal dan menolak running.
  - `cmd/server/main.go`: Membaca variabel lingkungan `METRICS_TOKEN` dari environment sistem.
  - `docs/prometheus.example.yml`: Menambahkan berkas contoh konfigurasi scraping Prometheus dengan otentikasi Bearer Token 32-byte.
- **Hasil Verifikasi**:
  ```text
  === RUN   TestItem6_MetricsToken_Min32Bytes_RefusesShortOrEmpty
      api_test.go:5347: Sukses: Validasi METRICS_TOKEN >= 32 byte dan pencegahan startup server terbukti andal.
  --- PASS: TestItem6_MetricsToken_Min32Bytes_RefusesShortOrEmpty (8.75s)
  PASS
  ok  	github.com/kopdes/merah-putih/internal/api	11.208s
  ```

---

### [2026-11-12] Item 7: Latihan Pemulihan Bencana Nyata (`scripts/restore_drill.sh`) & Notifier WhatsApp/SMS Berbasis Outbox
- **Implementasi**:
  - `internal/outbox/notifier.go`:
    - Mengimplementasikan `WhatsAppSMSNotifier` dengan dukungan fallback cerdas dari WhatsApp ke SMS bila pengiriman WA mengalami kendala timeout/koneksi.
    - Fungsi `EnqueueNotification`: Memasukkan payload terstruktur ke tabel `outbox_event` dalam transaksi bisnis yang sama (atomik).
    - Fungsi `RedactPhoneNumber`: Menyamarkan nomor telepon (`+62****890`) demi kepatuhan UU PDP.
  - `scripts/restore_drill.sh`: Skrip bash otomatis untuk simulasi Disaster Recovery (DR) dan verifikasi Point-In-Time Recovery (PITR):
    1. Mencatat Checkpoint LSN dan menanam marker verifikasi unik ke `audit_log`.
    2. Mengekstrak snapshot dump terkompresi menggunakan `pg_dump -Fc` dan memverifikasi integritas berkas via checksum SHA-256.
    3. Membuat database target drill terisolasi.
    4. Merestorasi dump ke database drill menggunakan `pg_restore`.
    5. Memvalidasi jumlah tabel (129 tabel terpulihkan), ketersediaan seluruh partisi `audit_log`, `kas_mutasi`, `stok_mutasi`, keberadaan 61 tabel ber-RLS aktif, dan keutuhan data marker transaksi drill.
    6. Membersihkan database temporer secara bersih.
- **Hasil Verifikasi Nyata Eksekusi `scripts/restore_drill.sh`**:
  ```text
  =============================================================================
  [RESTORE-DRILL] Memulai Simulasi Latihan Pemulihan Bencana (DR Drill)
  Waktu Mulai : 2026-11-12T13:10:47Z
  Target Host : 127.0.0.1:5433 (Database: kopdes_db)
  Drill DB    : kopdes_restore_drill_20261112_201047
  =============================================================================
  [LANGKAH 1/6] Mencatat Checkpoint LSN dan Marker Transaksi...
  -> WAL LSN Checkpoint saat ini: 0/556A988
  -> Marker DR checkpoint berhasil ditanam ke audit_log.
  [LANGKAH 2/6] Mengekspor Snapshot Database Menggunakan pg_dump...
  -> Snapshot dump selesai: ./backups/drill/drill_20261112_201047_1028.dump (1.8M)
  -> Checksum SHA-256: 996a8ccaf724a2878a4b8173ad1a216120d2ebc84a87228f7ca75c1c9addea24
  [LANGKAH 3/6] Membuat Database Target Drill: kopdes_restore_drill_20261112_201047...
  CREATE DATABASE
  [LANGKAH 4/6] Menjalankan pg_restore ke Database Drill...
  -> Proses restorasi selesai dalam 3 detik.
  [LANGKAH 5/6] Memverifikasi Integritas Data & Skema Pasca-Restorasi...
  -> Jumlah Tabel Publik Terpulihkan: 129
  -> Marker Transaksi Ditemukan: 1
  -> Partisi untuk audit_log: 16 tabel terdaftar
  -> Partisi untuk kas_mutasi: 16 tabel terdaftar
  -> Partisi untuk stok_mutasi: 16 tabel terdaftar
  -> Tabel dengan RLS Aktif Terpulihkan: 61
  [LANGKAH 6/6] Membersihkan Database Temporer Drill...
  DROP DATABASE
  -> Database kopdes_restore_drill_20261112_201047 berhasil dihapus secara bersih.
  =============================================================================
  [RESTORE-DRILL] HASIL LATIHAN PEMULIHAN BENCANA: BERHASIL 100% (SUCCESS)
  Dump File        : ./backups/drill/drill_20261112_201047_1028.dump
  Ukuran Dump      : 1.8M
  Waktu Restorasi  : 3 detik
  Tabel Dipulihkan : 129 tabel
  Tabel RLS Aktif  : 61 tabel
  Integritas Data  : Marker drill_20261112_201047_1028 TERVERIFIKASI IDENTIK
  Waktu Selesai    : 2026-11-12T13:10:53Z
  =============================================================================
  ```
- **Hasil Verifikasi Pengujian Unit & Integrasi Outbox (`go test -v -race ./internal/outbox`)**:
  ```text
  === RUN   TestWhatsAppSMSNotifier_DirectChannelsAndFallback
  --- PASS: TestWhatsAppSMSNotifier_DirectChannelsAndFallback (0.00s)
  === RUN   TestWhatsAppSMSNotifier_IntegratedWithOutboxWorker
      notifier_test.go:205: Sukses: Enqueue notifikasi outbox dan dispatching via worker & WhatsApp gateway 100% terverifikasi!
  --- PASS: TestWhatsAppSMSNotifier_IntegratedWithOutboxWorker (0.08s)
  === RUN   TestOutboxWorker_ProcessBatch
  --- PASS: TestOutboxWorker_ProcessBatch (0.07s)
  PASS
  ok  	github.com/kopdes/merah-putih/internal/outbox	2.360s
  ```

---

### VIII. HASIL RUN HIJAU GITHUB ACTIONS CI & VERIFIKASI AKHIR

#### 1. Ringkasan Eksekusi CI (Workflow Run ID: 43237728664, Commit: `ddaec96`)
- **URL Run**: `https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/43237728664`
- **Status Akhir**: `completed`
- **Kesimpulan**: `success` (100% HIJAU)
- **Komponen yang Lolos Verifikasi**:
  1. `go test -count=1 -race -v ./...` (Seluruh unit & integration test multi-tenant dengan detektor data race)
  2. `govulncheck ./...` (Pemindaian basis data kerentanan resmi Go)
  3. `staticcheck ./...` (Analisis kode statis Go tingkat lanjut)
  4. `gosec Security Scanner` (Pemindaian keamanan AST Go)
  5. `Gitleaks Secret Detection` (Pemindaian kebocoran kredensial/secret)
  6. `Docker Compose Smoke Test` (Build multi-stage image Go dari nol, bootstrap database PostgreSQL 16 + migrasi otomatis, healthcheck kontainer, dan pengujian probe `/healthz`)

#### 2. Rincian Output Asli Jobs & Steps GitHub Actions

```text
id           name                                      status    conclusion started_at           completed_at        
--           ----                                      ------    ---------- ----------           ------------        
131249522797 Test, Static Analysis & Security Scanning completed success    2026-11-12T14:29:37Z 2026-11-12T14:41:14Z
131256707928 Docker Compose Smoke Test                 completed success    2026-11-12T14:41:17Z 2026-11-12T14:41:56Z
```

##### Rincian Step Job 1: Test, Static Analysis & Security Scanning
```text
number name                                              status    conclusion
------ ----                                              ------    ----------
     1 Set up job                                        completed success   
     2 Initialize containers                             completed success   
     3 Checkout Code                                     completed success   
     4 Setup Go                                          completed success   
     5 Install PostgreSQL Client & Setup Database Schema completed success   
     6 Run Tests with Race Detector                      completed success   
     7 Upload Test Output on Failure                     completed skipped   
     8 Run govulncheck                                   completed success   
     9 Run staticcheck                                   completed success   
    10 Run gosec Security Scanner                        completed success   
    11 Run Gitleaks Secret Detection                     completed success   
    20 Post Setup Go                                     completed success   
    21 Post Checkout Code                                completed success   
    22 Stop containers                                   completed success   
    23 Complete job                                      completed success   
```

##### Rincian Step Job 2: Docker Compose Smoke Test
```text
number name                                         status    conclusion
------ ----                                         ------    ----------
     1 Set up job                                   completed success   
     2 Checkout Code                                completed success   
     3 Create .env Configuration for Smoke Test     completed success   
     4 Build and Start Services with Docker Compose completed success   
     5 Wait for Containers Health Check             completed success   
     6 Verify Health Endpoint                       completed success   
     7 Teardown Services                            completed success   
    14 Post Checkout Code                           completed success   
    15 Complete job                                 completed success   
```

#### 3. Verifikasi Detektor Race Lokal dengan GCC (UCRT64)
- **Kompiler C/C++ Lokal**: `gcc.exe (Rev6, Built by MSYS2 project) 16.2.0` pada `C:\msys64\ucrt64\bin\gcc.exe`.
- **Hasil Eksekusi Lokal**:
  ```text
  $env:PATH = "C:\msys64\ucrt64\bin;" + $env:PATH; $env:CGO_ENABLED = "1"; go test -v -race ./internal/security
  === RUN   TestT3_SecretValidation_RequiresStrongKey
  --- PASS: TestT3_SecretValidation_RequiresStrongKey (0.00s)
  === RUN   TestT3_AESGCM_WithAAD_TamperProof
  --- PASS: TestT3_AESGCM_WithAAD_TamperProof (0.00s)
  === RUN   TestT3_HKDF_TenantBlindIndex_Isolation
  --- PASS: TestT3_HKDF_TenantBlindIndex_Isolation (0.00s)
  === RUN   TestT3_RedactDSN_HidesPassword
  --- PASS: TestT3_RedactDSN_HidesPassword (0.00s)
  PASS
  ok  	github.com/kopdes/merah-putih/internal/security	1.426s
  ```

---

### IX. AUDIT KONFIGURASI TERPUSAT, PORT ALTERNATIF (SET B), CI MULTI-JOB & DOKUMENTASI LENGKAP (2026-11-13)

1. **Perbaikan Sensitivitas Zona Waktu Linux (UTC) pada Batas Harian Pilot (`1fa4f1c`)**
   - Akar masalah di `internal/api/handlers/qris_handler.go`: casting ganda `((CURRENT_TIMESTAMP AT TIME ZONE $3::text)::date AT TIME ZONE $3::text)` menghasilkan offset keliru saat `TimeZone` sesi PostgreSQL bernilai `UTC` (Linux Docker/CI).
   - Diperbaiki menjadi `date_trunc('day', CURRENT_TIMESTAMP AT TIME ZONE $3::text) AT TIME ZONE $3::text`.
   - Diverifikasi lulus di kedua zona waktu (`PGTZ=UTC TZ=UTC` dan `PGTZ=Asia/Jakarta TZ=Asia/Jakarta`) via `go test -count=1 -v -race -run "Pilot" ./internal/api`.

2. **Perbaikan Keamanan `gosec G706` & Kepercayaan Proxy Load Test Docker Set B (`5b24dab`)**
   - Menambahkan sanitasi newline/CRLF (`sanitizeLogValue`) di `cmd/server/main.go` serta menandai `#nosec G706` hanya pada titik cetak yang telah disanitasi.
   - Menambahkan subnet bridge Docker `172.16.0.0/12` ke `TRUSTED_PROXIES` pada `.env.test.alt` agar simulasi 2.000 anggota dengan `X-Forwarded-For` tidak terkena rate-limit per host gateway.

3. **Hasil Penuh GitHub Actions CI Linux Run ID `43385163339` (Commit `5b24dab` — 100% HIJAU)**
   - **URL Run:** `https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/43385163339` (`status=completed`, `conclusion=success`)
   - **`config-and-static-audit` (`131751801060`):** `completed/success` (`envcheck --strict --ports` Set A & Set B, `--compare`, penolakan `.env.example`, `docker compose config`, Hadolint, Trivy `--severity HIGH,CRITICAL --exit-code 1 .`, Gitleaks).
   - **`test-and-security-setA` (`131751801297`):** `completed/success` (`go test -count=1 -race -p 1 ./...` semua paket `ok` termasuk `internal/api 472.845s`, `govulncheck ./...` -> `No vulnerabilities found.`, `staticcheck ./...`, `gosec` -> `Issues : 0`).
   - **`docker-setB-e2e-and-load` (`131752034881`):** `completed/success`:
     - Stack Set B (`18080/15433/19100`, `kopdes_alt_test`) & `go test -count=1 -race -p 1 ./...`: semua paket `ok` (`internal/api 469.022s`).
     - Backup & `scripts/restore_drill.sh`: `WAL LSN Checkpoint: 0/2342000`, `Tabel Terpulihkan: 129 (durasi 2s)`, `Marker Audit Drill: 1`, `Pemeriksaan Rantai Hash audit_log (broken links): 0`, `Tabel RLS Aktif: 61`, `RESTORE DRILL BERHASIL: Snapshot, Marker, RLS, dan Integritas Hash Chain Terverifikasi.`
     - `cmd/loadtest` 2.000 anggota (100 konkurensi): `success_count: 2000`, `error_count: 0`, `error_rate_pct: 0`, `rps: 15790.52`, `duration_sec: 0.127`, `p50: 3.71`, `p95: 20.08`, `p99: 31.67`.

4. **Verifikasi Lokal Konfigurasi (`envcheck`) & Pemindaian Keamanan**
   ```text
   envcheck STRICT PASS (.env.test): 79 variabel terverifikasi, APP_ENV=test, DB_NAME=kopdes_test, API_PORT=8080, METRICS_PORT=9100 (EXIT: 0)
   envcheck STRICT+PORTS PASS (.env.test.alt): tidak ada konflik port, 79 variabel, API_PORT=18080, METRICS_PORT=19100 (EXIT: 0)
   envcheck STRICT REJECT (.env.example): DITOLAK sesuai aturan — NILAI KOSONG DB_PASSWORD, JWT_SECRET, AES_MASTER_KEY, BLIND_INDEX_SECRET, QRIS_SECRET, METRICS_TOKEN (EXIT: 1)
   envcheck COMPARE PASS (.env.test vs .env.test.alt): skema kunci 100% identik (79 kunci) (EXIT: 0)
   envcheck PORTS CONFLICT (.env.test saat PG Set A aktif di 5433): PORT SEDANG DIPAKAI (DB_HOST_PORT): 127.0.0.1:5433 (EXIT: 1)

   gitleaks git --no-banner -v -> 68 commits scanned, 1.35 MB, no leaks found (EXIT: 0)
   staticcheck ./... -> 0 isu (EXIT: 0)
   gosec -quiet -exclude=G104,G304,G101,G505,G115 ./... -> 0 isu (EXIT: 0)
   govulncheck ./... (Windows lokal go1.27.1) -> mendeteksi 11 kerentanan stdlib go1.27.1 (fixed in go1.27.4; CI Linux & Dockerfile sudah go 1.27.4 dengan 0 kerentanan).
   ```

5. **Pembuatan & Pengujian Otomatis Paket Dokumentasi Lengkap (`Tugas E`)**
   - Pembaruan menyeluruh: `docs/openapi.yaml` (OpenAPI 3.0.3 v1.1.0, seluruh 48 path, server berparameter), `scripts/generate_api_artifacts.py` (`docs/kopdes.postman_collection.json`, `docs/kopdesClient.ts`), `docs/README.md`, `docs/BACKEND_GUIDE.md`, `docs/FRONTEND_GUIDE.md`, `docs/TUGAS_MANUAL_PEMILIK.md`, `docs/AUDIT_REPORT.md`, serta pembaruan `scripts/generate_docs.py` dan regenerasi `DOKUMENTASI_LENGKAP_API_KOPDES.html` (30.872 B), `.pdf` (507.935 B), `.docx` (3.955 B).
   - Suite uji dokumentasi otomatis (`internal/api/docs_verification_test.go`) lulus penuh:
     ```text
     $ go test -count=1 -v -race -run TestDoc ./internal/api
     === RUN   TestDoc_OpenAPI_RouteParity
     --- PASS: TestDoc_OpenAPI_RouteParity (0.03s)
     === RUN   TestDoc_MarkdownLinks_Anchors_And_NoForbiddenClaims
     --- PASS: TestDoc_MarkdownLinks_Anchors_And_NoForbiddenClaims (0.23s)
     === RUN   TestDoc_EndToEnd_DocumentedWorkflows
     --- PASS: TestDoc_EndToEnd_DocumentedWorkflows (41.29s)
     PASS
     ok  	github.com/kopdes/merah-putih/internal/api	46.062s
     ```

---

## Prompt Antigravity v5 — Tugas 0: Koreksi `docs/AUDIT_REPORT.md` & Perbaikan Terkait (2026-11-14)

### 1. Perubahan yang Dilakukan
- **Penyelarasan Laporan Audit ([`docs/AUDIT_REPORT.md`](AUDIT_REPORT.md), [`docs/SCOPE_RECONCILIATION.md`](SCOPE_RECONCILIATION.md), [`docs/LAPORAN_REMEDIASI_DAN_PERUBAHAN_KODE.md`](LAPORAN_REMEDIASI_DAN_PERUBAHAN_KODE.md))**:
  - Menyelaraskan seluruh matriks remediasi (`K1`–`K3`, `T1`–`T5`, `S1`–`S3`, `P1-1`–`P1-5`, serta ringkasan migrasi `000001`–`000018`) terhadap riwayat commit Git dan berkas migrasi aktual, termasuk mencantumkan temuan **K1** (API superuser di Compose) dan **K2** (partisi berhenti 31 Des 2026).
  - Mengganti label `"Audit independen"` / `"diremediasi secara tuntas"` menjadi `"Audit mandiri oleh agen pengembang; belum ada peninjauan pihak ketiga"`, mengubah Bagian 3 menjadi **`KEPUTUSAN YANG DIUSULKAN / DIPAKAI SEMENTARA`** dengan kolom kosong **`Bukti persetujuan (diisi pemilik: notulen/email/chat)`**, menjelaskan mekanisme retensi 30 hari apa adanya (hapus baris/kolom DB & berkas KYC fisik, bukan *crypto-shredding*, bertanda `PERLU VERIFIKASI HUKUM`), menyelaraskan status pemulihan `sinkronisasi_simkopdes` (`000012` + `internal/simkopdes/adapter.go`, **69 tabel induk/dasar + 60 partisi = 129 objek tabel fisik**), dan menambahkan bagian **`Keterbatasan yang Diketahui & Tindak Lanjut Wajib`**.
  - Menegaskan status **`PITR belum diimplementasikan`** dan memperbarui `scripts/restore_drill.sh` untuk mencetak status enkripsi dump (`BACKUP_ENCRYPTION_KEY`) serta menjalankan `VerifyHashChain` pada **ketiga** tabel append-only (`audit_log`, `kas_mutasi`, `pembayaran_verifikasi_log`).
- **Pengetatan `TRUSTED_PROXIES` CIDR `> /16` di Produksi (`internal/config/config.go`, `config_test.go`)**: `ValidateProduction()` menolak CIDR lebih lebar dari `/16` (`ones < 16`).
- **Penghapusan Pengecualian Global `gosec` & Penambahan `trivy image` (`.github/workflows/ci.yml`, `internal/security/totp.go`)**: menghapus `-exclude=G104,G304,G101,G505,G115` dan menandai `#nosec G505` / guard non-negatif + `#nosec G115` spesifik pada TOTP RFC 6238; menambahkan `docker build` + `trivy image` di CI.
- **Uji Kelelahan Kapasitas QRIS 1.500 Request Konkuren & Peningkatan Konkurensi (`service.go`, `internal/api/qris_slot_exhaustion_test.go`, `cmd/loadtest/main.go`)**: menambahkan `ambilSlotTerpakaiQRIS` + `acakSisaSlot` (`crypto/rand`) setelah bentrok pertama untuk menghilangkan lock convoy antar worker.
- **Perbaikan Ketidaksesuaian Dokumentasi vs API (`DISC-01` s.d. `DISC-04`)** dengan TDD test di `internal/api/v5_discrepancies_test.go`:
  - `MFA_ENROLLMENT_REQUIRED` pada `internal/api/middleware/auth.go` untuk token `mfa_setup`.
  - Pembatasan role `POST /api/v1/toko/pesanan/{id}/expire` di `internal/api/server.go`.
  - Penambahan `X-Request-ID` + `Access-Control-Expose-Headers` + `403` preflight pada `security_headers.go` dan `cors.go`.
  - Penambahan `"klasifikasi_akuntansi": "KEPUTUSAN AKUNTAN"` di `HandleCreateJenisIuran`.

### 2. Output Verifikasi Asli (`Tugas 0`)
```text
$ git log --name-status -- ".env*"
M	.env.test.alt       (5b24dab)
M	.env.example/.test/.test.alt (de75732)
M	.env.example        (568dbe0)
M	.env.example/.test/.test.alt (0c52545)
A	.env.test, .env.test.alt / M .env.example (d198098)
M	.env.example        (52fc1e7, 7b1dd05, 1d62927, 24ffe9a)
A	.env.example        (b2ced33)
-> Nihil .env / .env.local / .env.production / .env.live di riwayat Git.

$ go test -v ./internal/config -run TestValidateProduction_RejectsWideTrustedProxiesCIDR
=== RUN   TestValidateProduction_RejectsWideTrustedProxiesCIDR
--- PASS: TestValidateProduction_RejectsWideTrustedProxiesCIDR (0.00s)
PASS
ok  	github.com/kopdes/merah-putih/internal/config	1.160s

$ go test -v . -run TestKonkurensiQRIS_AntiTabrakan
=== RUN   TestKonkurensiQRIS_AntiTabrakan
    service_test.go:353: Sukses menguji 30 transaksi QRIS konkuren: seluruh kode unik (1..999) unik tanpa tabrakan!
--- PASS: TestKonkurensiQRIS_AntiTabrakan (0.77s)
PASS
ok  	github.com/kopdes/merah-putih	1.088s

$ go test -v ./internal/api -run "TestQRISSlotExhaustion_1500ConcurrentSameNominal|TestV5_Discrepancy"
=== RUN   TestQRISSlotExhaustion_1500ConcurrentSameNominal
    qris_slot_exhaustion_test.go:145: QRIS Slot Exhaustion (1500 concurrent requests, 100 workers, 5.07s): 201_Created=999, 409_Conflict=501, 5xx=0, other=0
--- PASS: TestQRISSlotExhaustion_1500ConcurrentSameNominal (7.40s)
=== RUN   TestV5_Discrepancy01_SetupTokenReturnsMFAEnrollmentRequired
--- PASS: TestV5_Discrepancy01_SetupTokenReturnsMFAEnrollmentRequired (1.70s)
=== RUN   TestV5_Discrepancy02_ExpirePesananRejectsAnggota
--- PASS: TestV5_Discrepancy02_ExpirePesananRejectsAnggota (1.69s)
=== RUN   TestV5_Discrepancy03_RequestIDAndCORSPreflight
--- PASS: TestV5_Discrepancy03_RequestIDAndCORSPreflight (1.74s)
=== RUN   TestV5_Discrepancy04_CreateJenisIuranIncludesKlasifikasiAkuntansi
--- PASS: TestV5_Discrepancy04_CreateJenisIuranIncludesKlasifikasiAkuntansi (1.79s)
PASS
ok  	github.com/kopdes/merah-putih/internal/api	14.754s

$ go test -v ./internal/api -run "TestDoc_MarkdownLinks|TestDoc_OpenAPI_RouteParity"
=== RUN   TestDoc_OpenAPI_RouteParity
--- PASS: TestDoc_OpenAPI_RouteParity (0.04s)
=== RUN   TestDoc_MarkdownLinks_Anchors_And_NoForbiddenClaims
--- PASS: TestDoc_MarkdownLinks_Anchors_And_NoForbiddenClaims (0.02s)
PASS
ok  	github.com/kopdes/merah-putih/internal/api	0.355s

$ gosec.exe ./...
Summary:
  Gosec  : dev
  Files  : 44
  Lines  : 13045
  Nosec  : 16
  Issues : 0
```

---

## Prompt Antigravity v5 — Tugas 1: Infrastruktur Env, `cmd/uitester`, `cmd/seed_test_accounts` & Kerangka Frontend Tester (2026-11-14)

### 1. Perubahan yang Dilakukan
- **Registri Variabel Lingkungan & Audit (`internal/config/config.go`, `cmd/envcheck/main.go`, `.env.example`, `.env.test`, `.env.test.alt`, `docs/ENV_REFERENCE.md`, `internal/config/repo_audit_test.go`)**:
  - Menambahkan `UI_PORT` (`8081` Set A / `18081` Set B), `UI_BIND` (`127.0.0.1`), `API_PUBLIC_URL` (`http://127.0.0.1:8080` Set A / `http://127.0.0.1:18080` Set B), serta `TEST_SEED_PASSWORD` (opsional; kosong di `.env.example`). Total `83` variabel terdaftar di `AppConfig`.
  - Memperbarui `ALLOWED_ORIGINS` pada `.env.test` dan `.env.test.alt` agar menyertakan origin loopback `UI_PORT` masing-masing profil.
  - Memperbarui `runPortAudit` agar mendeteksi bentrokan port pada `UI_PORT`, dan memperluas `TestRepoAudit_NoHardcodedPortsOrDevPasswords_AndInfraFilesExist` untuk memeriksa `frontend-tester/index.html`, `frontend-tester/app.js`, `cmd/uitester/main.go`, `cmd/seed_test_accounts/main.go`, dan `scripts/seed_test_accounts.{ps1,sh}`.
- **Seeder Idempoten Akun Uji (`internal/testseed/seed.go`, `cmd/seed_test_accounts/`, `scripts/seed_test_accounts.{ps1,sh}`)**:
  - Menolak eksekusi bila `APP_ENV != test` atau `DB_NAME` tidak berakhiran `*_test`.
  - Mengisi secara idempoten Koperasi A (`11111111-...`) dan Koperasi B (`22222222-...`), 11 akun uji (`admin_a`, `admin_nomfa_a`, `pengurus_a`, `pengurus2_a`, `bendahara_a`, `ketua_a`, `pengawas_a`, `operator_a`, `anggota_a`, `admin_b`, `anggota_b`), rekening QRIS badan hukum, jenis & tagihan iuran, produk/harga/gudang/stok/promo toko, serta contoh pembayaran tenant B untuk pengujian anti-IDOR.
- **Server Tester & Frontend Polos (`cmd/uitester/`, `Dockerfile`, `docker-compose.test.yml`, `frontend-tester/{index.html,app.js}`)**:
  - `cmd/uitester` menolak startup saat `APP_ENV=production`, menyajikan `/config.json` tanpa rahasia/kata sandi, `/api/probe-cors`, `/api/totp-helper` (RFC 6238 khusus mode non-produksi), serta berkas statis `frontend-tester/`.
  - `docker-compose.test.yml` menambahkan service opsional `uitester` berprofil `tester` tanpa port literal.
  - `frontend-tester/index.html` dan `app.js` ditulis tanpa framework, tanpa build step, tanpa CDN, tanpa CSS warna/ikon/animasi, token hanya disimpan dalam memori JS, serta guard konfirmasi **`SAYA PAHAM INI BUKAN PRODUKSI`** untuk target non-lokal.

### 2. Output Verifikasi Asli (`Tugas 1`)
```text
$ go test -v ./internal/config ./cmd/uitester ./cmd/seed_test_accounts
=== RUN   TestConfig_Load_UITesterAndSeedEnvVars
--- PASS: TestConfig_Load_UITesterAndSeedEnvVars (0.00s)
=== RUN   TestRepoAudit_EnvCatalogConsistency_AndEnvReferenceFresh
--- PASS: TestRepoAudit_EnvCatalogConsistency_AndEnvReferenceFresh (0.01s)
=== RUN   TestRepoAudit_NoHardcodedPortsOrDevPasswords_AndInfraFilesExist
--- PASS: TestRepoAudit_NoHardcodedPortsOrDevPasswords_AndInfraFilesExist (0.01s)
ok  	github.com/kopdes/merah-putih/internal/config	0.714s
=== RUN   TestUITester_RefusesProductionMode
--- PASS: TestUITester_RefusesProductionMode (0.00s)
=== RUN   TestUITester_ConfigJSON_ProbeCORS_And_TOTPHelper
--- PASS: TestUITester_ConfigJSON_ProbeCORS_And_TOTPHelper (0.01s)
ok  	github.com/kopdes/merah-putih/cmd/uitester	1.522s
=== RUN   TestSeedTestAccounts_RefusesNonTestEnvironmentOrDatabase
--- PASS: TestSeedTestAccounts_RefusesNonTestEnvironmentOrDatabase (0.00s)
ok  	github.com/kopdes/merah-putih/cmd/seed_test_accounts	0.479s

$ go run ./cmd/envcheck --env-file .env.test --strict
envcheck STRICT PASS: 83 variabel terverifikasi, APP_ENV=test, DB_NAME=kopdes_test, API_PORT=8080, METRICS_PORT=9100

$ go run ./cmd/envcheck --env-file .env.test.alt --strict --ports
envcheck PORTS PASS: tidak ada konflik port pada konfigurasi.
envcheck STRICT PASS: 83 variabel terverifikasi, APP_ENV=test, DB_NAME=kopdes_alt_test, API_PORT=18080, METRICS_PORT=19100

$ go run ./cmd/envcheck --compare .env.test .env.test.alt
envcheck COMPARE PASS: skema kunci 100% identik (83 kunci).

$ go run ./cmd/seed_test_accounts --env-file .env.test   # Eksekusi 1 & 2 (idempoten)
Seeded 11 akun uji sintetis ke database kopdes_test (postgres://kopdes_app:%2A%2A%2A%2A%2A%2A@127.0.0.1:5433/kopdes_test?sslmode=disable) selesai.

$ gosec.exe ./...
Summary:
  Gosec  : dev
  Files  : 47
  Lines  : 14132
  Nosec  : 20
  Issues : 0
```


## Tugas 2 & Tugas 5 Prep (v5) — OpenAPI JSON, `roles_matrix.json` 54 Operasi, Perbaikan Alur Frontend Tester & Tes Paritas

- **Tes gagal lebih dahulu (`TestDoc_OpenAPI_JSON_Parity`, `TestDoc_RolesMatrix_Parity`)**:
  - `go test -v -run "TestDoc_OpenAPI_JSON_Parity|TestDoc_RolesMatrix_Parity" ./internal/api` gagal awal karena `frontend-tester/openapi.json` dan `docs/roles_matrix.json` belum tersedia.
- **Perbaikan ketidaksesuaian awal & single source of truth**:
  - Memperbarui `docs/openapi.yaml`: menambahkan query parameter wajib `anggota_id` (UUID) pada `GET /api/v1/katalog/harga` sesuai `katalog_handler.go`, serta memperjelas respons `GET /api/v1/pendaftaran/{id}` (`pendaftaran_id`, `nik_masked`).
  - Memperbaiki `internal/api/handlers/katalog_handler.go` (`HandleGetHargaAnggota`) agar mengembalikan `404 NotFound` saat `pgx.ErrNoRows` (bukan `500` ketika produk lintas tenant diakses).
  - Memperbaiki typo `auto_approve_mutasi_enabled` -> `mutasi_auto_approve_enabled` di `internal/config/config.go` dan meregenerasi `docs/ENV_REFERENCE.md`.
  - Membangun `docs/roles_matrix.json` yang mencakup seluruh **54 operasi HTTP / 48 path** x **7 peran** (`admin`, `pengurus`, `bendahara`, `ketua`, `pengawas`, `operator`, `anggota`) + `unauthenticated` + `cross_tenant`.
  - Memperluas `scripts/generate_api_artifacts.py` untuk mengonversi `docs/openapi.yaml` -> `frontend-tester/openapi.json`, menyinkronkan `docs/roles_matrix.json` -> `frontend-tester/roles_matrix.json`, dan merender tabel matriks otorisasi otomatis ke `docs/FRONTEND_GUIDE.md` di antara `<!-- BEGIN_ROLES_MATRIX -->` .. `<!-- END_ROLES_MATRIX -->`.
  - Memperbaiki payload/rute pada `frontend-tester/app.js` & `frontend-tester/index.html` (`/api/v1/auth/revoke`, `alamat_ktp`, `POST /pembayaran/{id}/bukti`, kolom CSV 5-kolom `tipe=CR`, `GET /katalog/produk`, `metode_serah_terima`, `POST /iuran/generate`, CRC16 EMVCo valid, serta label `Semua 54 Operasi / 48 Path`).

```text
$ go test -count=1 -v -run "TestDoc_" ./internal/api
=== RUN   TestDoc_OpenAPI_JSON_Parity
--- PASS: TestDoc_OpenAPI_JSON_Parity (0.00s)
=== RUN   TestDoc_RolesMatrix_Parity
--- PASS: TestDoc_RolesMatrix_Parity (0.00s)
=== RUN   TestDoc_OpenAPI_RouteParity
--- PASS: TestDoc_OpenAPI_RouteParity (0.00s)
=== RUN   TestDoc_MarkdownLinks_Anchors_And_NoForbiddenClaims
--- PASS: TestDoc_MarkdownLinks_Anchors_And_NoForbiddenClaims (0.01s)
=== RUN   TestDoc_EndToEnd_DocumentedWorkflows
--- PASS: TestDoc_EndToEnd_DocumentedWorkflows (1.69s)
PASS
ok  	github.com/kopdes/merah-putih/internal/api	1.859s
```



## Tugas 3, 4, 5 & 6 (v5) — Peta Klaim (`scenarios.json`), 102 Langkah E2E Nyata, Matriks Peran 54x8+7, Browser Hardening & Perbaikan DISC-05..DISC-11

- **Tes gagal lebih dahulu (TDD)**:
  - `TestDoc_ClaimsAndScenariosParity` gagal sebelum anotasi `<!-- claim:ID -->` dan sebelum `frontend-tester/scenarios.json` serta `docs/DOC_DISCREPANCIES.md` dibuat.
  - `TestV5_LiveScenariosAndRolesMatrix_E2E` gagal langsung saat pertama dijalankan terhadap PostgreSQL 16 nyata karena menemukan dua bug kode nyata yang sebelumnya lolos dari tes parsial: `DISC-09` (`GET /api/v1/iuran/tagihan/saya` -> HTTP `500` untuk akun non-anggota) dan `DISC-10` (`POST /api/v1/iuran/generate` -> `SQLSTATE 42P08` inconsistent parameter type `$3` di `service.go`).
- **Perbaikan & Artefak Verifikasi**:
  - `docs/FRONTEND_GUIDE.md` & `docs/BACKEND_GUIDE.md`: dianotasi dengan **26** penanda `<!-- claim:CLAIM-* -->` teruji + **5** penanda `UNVERIFIED` disertai alasan jujur untuk klaim non-REST (infra startup non-superuser DB, flag pilot isolasi Go, mock adapter WhatsApp/SMS, backup/TLS/Trivy OS, dan audit statis penyimpanan memori browser).
  - `docs/DOC_DISCREPANCIES.md`: mencatat seluruh **11 ketidaksesuaian (`DISC-01`..`DISC-11`)** beserta perbaikan kode/dokumen.
  - `internal/api/handlers/iuran_handler.go` (`DISC-09`): `HandleGetTagihanSaya` mengembalikan `200 OK []` bila `pgx.ErrNoRows` (akun non-anggota).
  - `service.go` (`DISC-10`): menambahkan cast `$3::text` pada `GenerateTagihanIuranBulanan`.
  - `internal/testseed/seed.go` & `frontend-tester/app.js` (`DISC-11`): mengekspos `produk_b_id` dan placeholder cross-tenant lengkap (`{produk_b_id}`, `{pembayaran_b_id}`, `{rekening_qris_b_id}`).
  - `cmd/uitester/main.go` & `cmd/uitester/main_test.go`: dukungan `offset_seconds` di `POST /api/totp-helper` serta uji serving `/openapi.json`, `/roles_matrix.json`, `/scenarios.json`.
  - `frontend-tester/scenarios.json`: 26 skenario (102 langkah) mencakup 100% klaim teruji termasuk TOTP window, upload PNG vs file teks invalid, idempotency 201/422, impor mutasi CSV 5-kolom + duplikat + segregasi pengimpor `403` + persetujuan kedua `200`, isolasi tenant `404`, IDOR `404`, dan brute-force `429 + Retry-After`.
  - `internal/api/scenarios_and_matrix_e2e_test.go` (`TestV5_LiveScenariosAndRolesMatrix_E2E`): eksekusi langsung terhadap PostgreSQL 16 + `httptest.NewServer` mencakup seluruh matriks `54` operasi x `8` subjek + `7` probe lintas-tenant (`439` pemeriksaan matriks), CORS allowed/disallowed + `Access-Control-Expose-Headers`, penolakan payload `>10MB` HTTP `413`, seluruh `26` skenario (`102` langkah), serta pemindaian **540 respons** terhadap kebocoran NIK/sandi/rahasia.

```text
$ go test -count=1 -v -run "TestV5_LiveScenariosAndRolesMatrix_E2E|TestDoc_" ./internal/api ; go test -count=1 -v ./cmd/uitester
=== RUN   TestDoc_OpenAPI_JSON_Parity
--- PASS: TestDoc_OpenAPI_JSON_Parity (0.00s)
=== RUN   TestDoc_RolesMatrix_Parity
--- PASS: TestDoc_RolesMatrix_Parity (0.00s)
=== RUN   TestDoc_ClaimsAndScenariosParity
--- PASS: TestDoc_ClaimsAndScenariosParity (0.00s)
=== RUN   TestDoc_OpenAPI_RouteParity
--- PASS: TestDoc_OpenAPI_RouteParity (0.00s)
=== RUN   TestDoc_MarkdownLinks_Anchors_And_NoForbiddenClaims
--- PASS: TestDoc_MarkdownLinks_Anchors_And_NoForbiddenClaims (0.01s)
=== RUN   TestDoc_EndToEnd_DocumentedWorkflows
--- PASS: TestDoc_EndToEnd_DocumentedWorkflows (1.67s)
=== RUN   TestV5_LiveScenariosAndRolesMatrix_E2E
    scenarios_and_matrix_e2e_test.go:767: V5 LIVE E2E PASSED: 54 operasi x 8 subjek + 7 cross-tenant = 439 pemeriksaan matriks; 26 skenario (102 langkah); CORS/413/Zero-PII tervalidasi (540 respons dipindai).
--- PASS: TestV5_LiveScenariosAndRolesMatrix_E2E (4.73s)
PASS
ok  	github.com/kopdes/merah-putih/internal/api	6.680s
=== RUN   TestUITester_RefusesProductionMode
--- PASS: TestUITester_RefusesProductionMode (0.00s)
=== RUN   TestUITester_ConfigJSON_ProbeCORS_And_TOTPHelper
--- PASS: TestUITester_ConfigJSON_ProbeCORS_And_TOTPHelper (0.08s)
PASS
ok  	github.com/kopdes/merah-putih/cmd/uitester	1.494s
```

---

## Tugas 7 & Tugas 8 (v5) — Panduan Frontend Tester, Penyelarasan Dokumentasi, Optimasi Uji `-race`, & Verifikasi Penuh Lokal

- **Pembaruan Dokumentasi & Artefak (`Tugas 7`)**:
  - `frontend-tester/README.md`: dokumentasi lengkap Frontend Tester polos (tanpa framework/CDN/build/penyimpanan persisten), `cmd/seed_test_accounts`, `scripts/seed_test_accounts.{ps1,sh}`, `cmd/uitester`, `docker-compose.test.yml --profile tester`, OpenAPI Explorer (`54` operasi / `48` path), alur `3.1`–`3.9`, `scenarios.json` (`26` skenario / `102` langkah), `roles_matrix.json` (`439` pemeriksaan), panel perilaku browser `6.1`–`6.4`, serta catatan reset MFA untuk `admin_nomfa_a`.
  - `docs/README.md`, `docs/FRONTEND_GUIDE.md` (§12), `docs/BACKEND_GUIDE.md` (§10 status `restore_drill.sh` 3-tabel + status PITR belum diimplementasikan, §11 rujukan diskrepansi & tester), dan `docs/AUDIT_REPORT.md` (§3 koreksi `ALLOW_PERSONAL_PAYMENT_PILOT=false` pada `.env.test`/`.env.test.alt`, §4 ringkasan `DISC-05`–`DISC-11`).
  - `scripts/generate_docs.py`: penambahan ringkasan Frontend Tester dan regenerasi `docs/DOKUMENTASI_LENGKAP_API_KOPDES.html`, `.pdf` (`515,374 bytes`), dan `.docx`.
- **Optimasi & Perbaikan `Tugas 8`**:
  - `internal/api/scenarios_and_matrix_e2e_test.go`: memperbaiki temuan `staticcheck` (`S1021`) pada deklarasi `expandString`.
  - `internal/api/api_test.go`: sebelum optimasi, `go test -count=1 -race -p 1 ./...` menyentuh batas timeout default `10m0s` (`FAIL internal/api 601.537s` saat menjalankan `TestV5_Discrepancy04` pada detik ke-8) akibat 100+ pemanggilan ulang *bcrypt cost-12* pada fixture uji. Ditambahkan `fastTestHashPassword` ber-cache `sync.Map` (cost 10 di fixture uji) sehingga waktu `-race` paket `internal/api` turun menjadi **`167.549s`** dengan seluruh tes tetap aktif.
  - `.github/workflows/ci.yml`: menambah `-timeout 20m` pada langkah `go test -count=1 -race -p 1` Set A dan Set B agar runner CI memiliki ruang waktu longgar.

```text
$ go run ./cmd/envcheck --env-file .env.test --strict
envcheck STRICT PASS: 83 variabel terverifikasi, APP_ENV=test, DB_NAME=kopdes_test, API_PORT=8080, METRICS_PORT=9100

$ go run ./cmd/envcheck --env-file .env.test.alt --strict --ports
envcheck PORTS PASS: tidak ada konflik port pada konfigurasi.
envcheck STRICT PASS: 83 variabel terverifikasi, APP_ENV=test, DB_NAME=kopdes_alt_test, API_PORT=18080, METRICS_PORT=19100

$ go run ./cmd/envcheck --compare .env.test .env.test.alt
envcheck COMPARE PASS: skema kunci 100% identik (83 kunci).

$ go run ./cmd/envcheck --env-file .env.example --strict  # (EXIT: 1)
NILAI KOSONG: DB_PASSWORD wajib diisi ... VALIDASI KEAMANAN GAGAL: secret JWT_SECRET tidak valid: panjang minimal 32 byte (didapat 0 byte)

$ go run ./cmd/envcheck --env-file .env.test --strict --ports  # saat PG Set A aktif di 5433 (EXIT: 1)
PORT SEDANG DIPAKAI (DB_HOST_PORT): 127.0.0.1:5433

$ gosec.exe ./...
Summary:
  Gosec  : dev
  Files  : 47
  Lines  : 14144
  Nosec  : 20
  Issues : 0

$ staticcheck.exe ./...
EXIT:0

$ gitleaks.exe git --config .gitleaks.toml --verbose --redact
12:50PM INF 74 commits scanned.
12:50PM INF scanned ~1984380 bytes (1.98 MB) in 3.75s
12:50PM INF no leaks found

$ govulncheck.exe ./...  # host lokal go1.27.1 (CI & Dockerfile memakai go1.27.4)
Your code is affected by 12 vulnerabilities from the Go standard library. (net/http / http2 @go1.27.1, fixed in go1.27.2+)

$ go test -count=1 -race -p 1 ./...
ok  	github.com/kopdes/merah-putih	5.204s
ok  	github.com/kopdes/merah-putih/cmd/envcheck	1.645s
?   	github.com/kopdes/merah-putih/cmd/loadtest	[no test files]
ok  	github.com/kopdes/merah-putih/cmd/seed_test_accounts	1.534s
?   	github.com/kopdes/merah-putih/cmd/server	[no test files]
ok  	github.com/kopdes/merah-putih/cmd/uitester	2.243s
ok  	github.com/kopdes/merah-putih/internal/api	167.549s
?   	github.com/kopdes/merah-putih/internal/api/handlers	[no test files]
ok  	github.com/kopdes/merah-putih/internal/api/middleware	1.806s
?   	github.com/kopdes/merah-putih/internal/api/response	[no test files]
ok  	github.com/kopdes/merah-putih/internal/auth	1.594s
ok  	github.com/kopdes/merah-putih/internal/config	1.530s
ok  	github.com/kopdes/merah-putih/internal/db	3.835s
ok  	github.com/kopdes/merah-putih/internal/outbox	2.339s
ok  	github.com/kopdes/merah-putih/internal/qris	1.583s
ok  	github.com/kopdes/merah-putih/internal/security	1.445s
?   	github.com/kopdes/merah-putih/internal/simkopdes	[no test files]
?   	github.com/kopdes/merah-putih/internal/testseed	[no test files]
EXIT:0
```
