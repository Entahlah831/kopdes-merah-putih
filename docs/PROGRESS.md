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
  - [x] Handler pendaftaran calon anggota baru dengan enkripsi PII (AES-256-GCM + AAD) dan blind index HKDF per tenant.
  - [x] Cek duplikasi NIK aktif (HTTP 409 Conflict).
  - [x] Unggah dokumen KYC multipart form dengan deteksi magic-bytes (hanya JPEG/PNG/PDF), batas 5MB, SHA-256 checksum.
  - [x] Anti-IDOR pada `GET /api/v1/pendaftaran/{id}` (404 jika bukan miliknya/bukan admin) dengan masking NIK (`3201********0001`).
  - [x] Maker-checker verifikasi: pemohon dilarang menyetujui formulirnya sendiri (403), admin menyetujui menghasilkan `no_anggota` atomik aman konkurensi.
  - [x] Endpoint `GET /api/v1/kas/mutasi` untuk membaca mutasi kas ringan ber-RLS.
- [x] **P1-2. Kapasitas & Metrik Slot Kode Unik**
  - [x] Migrasi `000008_kode_unik_configurable_digits`: mendukung 3 digit (1..999) atau 4 digit (1..9999).
  - [x] Metrik pemakaian slot (`GetSlotUsage`) dan peringatan otomatis jika pemakaian melebihi 70%.
  - [x] Penanganan respons error ramah `409 Conflict` saat kapasitas slot penuh.
- [x] **P1-3. Append-Only Hash Chaining & Perlindungan TRUNCATE**
  - [x] Migrasi `000007_append_only_hash_chain`: kolom `prev_hash`, `row_hash`, `ip_address`, `user_agent`, `request_id`.
  - [x] Trigger `BEFORE TRUNCATE` pada tabel append-only `kas_mutasi`, `audit_log`, `pembayaran_verifikasi_log` (`fn_prevent_truncate`).
  - [x] Generator hash chain SHA-256 `ComputeHashChain` dan fungsi pemeriksa integritas `VerifyHashChain`.
- [x] **P1-4. Operasional, Prometheus & Notifier**
  - [x] Header `Idempotency-Key` pada `POST /api/v1/pembayaran/qris`.
  - [x] Endpoint metrik Prometheus `GET /metrics` (`kopdes_uptime_seconds`, `kopdes_pembayaran_antrean_verifikasi`, `kopdes_pembayaran_slot_kode_unik_aktif`, `kopdes_job_gagal_total`, `kopdes_partisi_tersisa`).
  - [x] Interface `Notifier` dan `OutboxWorker` dengan pola `FOR UPDATE SKIP LOCKED`.
  - [x] Skrip backup snapshot dan uji restore PITR di `scripts/backup.sh` dan `scripts/restore_test.sh`.
- [x] **P1-5. Rekonsiliasi Cakupan Skema**
  - [x] Dokumen rekonsiliasi `docs/SCOPE_RECONCILIATION.md` mendokumentasikan 65 tabel dasar + 8 partisi bulanan.
- [x] **Laporan Akhir: docs/AUDIT_REPORT.md & docs/FRONTEND_GUIDE.md**
  - [x] Panduan frontend UI/UX mobile-first di `docs/FRONTEND_GUIDE.md`.
  - [x] Laporan audit komprehensif beserta keputusan manusia di `docs/AUDIT_REPORT.md`.

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
