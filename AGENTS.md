# Workspace Rules: Kopdes Merah Putih (PostgreSQL 16 & Go)

## 1. Aturan Bisnis & Integritas Data Inti
1. **Multi-Tenant & RLS**: Setiap transaksi pgx wajib menjalankan `SELECT set_config('app.current_koperasi_id', $1, true)` sebelum query bisnis via wrapper `middleware.WithTenantTx`. Nilai `koperasi_id` wajib diekstrak murni dari token JWT terverifikasi, DILARANG mengambil dari parameter URL, body JSON, atau header klien (anti-BOLA OWASP API1:2023). Seluruh tabel diproteksi dengan `FORCE ROW LEVEL SECURITY`.
2. **Koneksi Database Non-Superuser**: Aplikasi API wajib terhubung menggunakan role non-superuser `kopdes_app` (NOSUPERUSER, NOBYPASSRLS). Aplikasi menolak start jika koneksi terdeteksi sebagai superuser.
3. **Tipe Uang & PK**: Wajib gunakan `NUMERIC(18,2)` di SQL (dilarang `float`) dan `uuid_generate_v7()` untuk Primary Key. Di Go, gunakan `int64` (sen/Rupiah utuh) atau `string` desimal.
4. **Tabel Append-Only**: Dilarang membuat query `UPDATE` atau `DELETE` pada `audit_log`, `pendaftaran_status_log`, `anggota_status_log`, `kas_mutasi`, `stok_mutasi`, dan `pembayaran_verifikasi_log`. Koreksi wajib melalui entri reversal.
5. **Autentikasi & Granular RBAC**:
   - Access token berlaku 15 menit dengan klaim unik `ID` (`jti`) dan didampingi refresh token berotasi (tabel `refresh_tokens`). Deteksi token reuse wajib mencabut seluruh sesi aktif.
   - Lookup pra-autentikasi wajib melalui fungsi `SECURITY DEFINER` terkunci `fn_lookup_user_login(p_email_hash, p_koperasi_id)`. Dilarang menyalakan bypass platform admin sebelum login.
   - Pemisahan hak akses: `pengawas` dan `operator` hanya memiliki izin read-only audit dan antrean; mutasi verifikasi pembayaran dan generate tagihan dibatasi untuk `admin`, `pengurus`, `bendahara`, dan `ketua`.
   - Constant-time dummy bcrypt saat user tidak ditemukan untuk mencegah enumeration akun.
6. **Anti-IDOR Pembayaran**:
   - Anggota biasa hanya berhak mengakses pembayaran miliknya sendiri (`pembayar_user_id` atau `anggota_id`). Akses ke data pembayaran anggota lain ditolak (404/403).
7. **QRIS & Rekening Merchant**:
   - Feature flag `QRIS_DYNAMIC_ENABLED` (default false). Saat non-aktif, sistem mengembalikan QRIS statis + kode unik.
   - Parser EMVCo wajib memvalidasi checksum CRC16 (Tag 63), memastikan Tag 00 == '01', menolak duplikasi tag, dan membersihkan Tag 55–57 saat Tag 54 nominal tetap dipasang.
   - Rekening penampung settlement wajib atas nama badan hukum koperasi (`jenis_pemilik = 'badan_hukum_koperasi'`). Rekening perorangan pengurus ditolak kecuali mode pilot `ALLOW_PERSONAL_PAYMENT_PILOT=true`.
8. **Data Pribadi (UU 27/2022 PDP)**:
   - NIK, nomor HP, dan alamat disimpan terenkripsi AES-256-GCM dengan AAD (`table:column:record_id`) dan prefix versi kunci.
   - Blind index NIK diturunkan per tenant menggunakan HKDF-SHA256 (`DeriveTenantBlindIndexKey`).
   - Dilarang mencetak NIK, sandi, atau DSN lengkap ke dalam log sistem (`RedactDSN`).

## 2. Guardrails Arsitektur Hibrida & Batasan Agen AI
1. **Kunci Folder Fondasi Keamanan (Security Core Locked)**: Agen DILARANG MENGUBAH file di dalam `db/migrations/`, `internal/security/`, dan `internal/auth/` tanpa instruksi eksplisit dari pemilik proyek. Agen wajib menggunakan wrapper `middleware.WithTenantTx` yang sudah disediakan.
2. **Mode Eksekusi Pustaka Pihak Ketiga**: Jangan memasang dependensi Go eksternal baru (`go get`) tanpa persetujuan eksplisit pengguna.
3. **Pertahanan Berlapis di PostgreSQL 16 (Defense-in-Depth)**: Mengandalkan `FORCE ROW LEVEL SECURITY`, trigger append-only, konstrain CHECK, serta parameter sesi (`statement_timeout = '15s'`, `lock_timeout = '5s'`, `idle_in_transaction_session_timeout = '30s'`).
4. **Hardening HTTP**: Server menerapkan `ReadHeaderTimeout = 5s` (anti-Slowloris), `BodyLimit = 10MB`, sanitasi header CORS (tanpa `X-Koperasi-ID`, dan menolak `*` pada mode produksi).
5. **Verifikasi & Pemindaian Otomatis**: Setiap pembuatan atau modifikasi endpoint API baru wajib diverifikasi dengan `go test ./...` dan pemindaian celah keamanan `govulncheck ./...`.
