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
- [ ] **S1. IDOR dalam satu tenant pada GET /pembayaran/{id}**
- [ ] **S2. Feature flag QRIS dinamis & hardening parser EMVCo**
- [ ] **S3. Konfigurasi deployment & header hardening**

---

### IV. KELENGKAPAN FUNGSI & LAPORAN
- [ ] **P1-1. Endpoint KYC, Pendaftaran Anggota, Bukti Bayar, Mutasi Bank**
- [ ] **P1-2. Kapasitas & Metrik Slot Kode Unik**
- [ ] **P1-3. Append-Only Hash Chaining**
- [ ] **P1-4. Operasional, Prometheus & Notifier**
- [ ] **P1-5. Rekonsiliasi Cakupan Skema**
- [ ] **Laporan Akhir: docs/AUDIT_REPORT.md**

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
