# ADR-009: Pertahankan Sesi Opaque + Throttle Write `last_active_at` (<= 1x/Menit) & CLI Pemulihan Admin (D9)

- **Status**: `DIUBAH` (mekanisme sesi opaque **DIPERTAHANKAN**; ditambah optimasi throttle penulisan 60 detik & CLI pemulihan akses admin `reset-mfa` / `reset-password`)

## (a) Keputusan Saat Ini dan Lokasi Pembuatan
Pada branch `lean-revamp`, sistem memakai token sesi *opaque* (`sesi.token_hash` SHA-256). Hal ini sangat kuat karena pencabutan sesi (`logout`, `suspend`, ubah peran/password) langsung berlaku seketika. Namun:
1. `internal/api/middleware/auth.go:73` memanggil `UPDATE sesi SET last_active_at = NOW(), idle_expires_at = ...` pada **setiap permintaan HTTP** (`Get`, katalog, polling pembayaran), dan `GetActiveSessionByDigest` tidak membaca `last_active_at`.
2. Belum ada perintah CLI bagi pemilik VPS untuk mereset MFA atau kata sandi admin apabila pemilik/ketua kehilangan ponsel TOTP-nya (*lockout* koperasi).

## (b) Opsi Alternatif (Minimal 2)
1. **Opsi 1 — Sesi Opaque Tetap, Tulis DB Setiap Request & Tanpa CLI Recovery**: Pencabutan seketika bagus, tetapi menimbulkan IOPS `UPDATE sesi` yang tidak perlu pada setiap polling/GET dan berisiko *lockout* permanen jika satu-satunya admin kehilangan ponsel.
2. **Opsi 2 — Pertahankan Sesi Opaque + Throttle Update 60 Detik (Go + Guard SQL) + CLI `kopdes admin reset-mfa` & `reset-password`**: Pencabutan tetap diperiksa lewat `GetActiveSessionByDigest` + `status_aktif` + `anggota.status` pada setiap request (seketika!), sementara `TouchSessionActivity` hanya ditulis maksimal sekali per `60` detik (diperiksa di Go dan dikunci pula lewat klausa SQL `AND (last_active_at IS NULL OR last_active_at <= NOW() - INTERVAL '60 seconds')`). CLI pemulihan memungkinkan pemilik yang punya akses SSH VPS mereset MFA/password sekaligus mencabut semua sesi aktif dan menulis `audit_log`.
3. **Opsi 3 — Ganti Menjadi JWT Stateless**: Mengurangi kueri sesi, tetapi menghilangkan kapabilitas pencabutan seketika saat staf/anggota disuspend atau TOTP/sesi diretas — berisiko tinggi untuk data KYC dan uang.

## (c) Kriteria dan Bobot Keputusan
| Kriteria | Bobot | Opsi 1 (Opaque Tanpa Throttle) | Opsi 2 (Opaque + Throttle + CLI) | Opsi 3 (JWT Stateless) |
| :--- | :---: | :---: | :---: | :---: |
| Keamanan uang & data pribadi | 30% | 5 | **5** | 2 |
| Kesederhanaan operasi (1 dev) | 20% | 3 | **5** | 4 |
| Kecocokan skala & UX warga desa | 20% | 4 | **5** | 4 |
| Keandalan & kemudahan pemulihan | 15% | 2 | **5** | 3 |
| Biaya & kemampuan dibalik | 15% | 5 | **5** | 4 |
| **Skor Tertimbang** | **100%** | **3.95** | **5.00** | **3.20** |

## (d) Bukti Terukur di Repositori
- `TestDecisionAudit_D9_SessionTouchThrottleAndInstantRevocation` (`internal/api/decision_audit_auth_csrf_test.go`, **PASS**, `-race`, `2.59s`):
  - **Sebelum perbaikan (fail-first)**: 20 request cepat menggeser `sesi.last_active_at` (`20/20` tulis `UPDATE`).
  - **Sesudah perbaikan**: `20 rapid GET /auth/me finished in 29.8834ms with 0 DB session writes` (`last_active_at` identik selama `<60` detik); setelah `last_active_at` berumur `>90` detik, request berikutnya memperbarui `last_active_at` sekali; dan setelah `revoked_at` disetel di jendela `<60` detik, request berikutnya seketika ditolak **`401`**.
- `TestAdminRecoveryCLI_ResetMFAAndResetPassword` (`cmd/kopdes/main_test.go`, **PASS**, `-race`, `10.80s`): CLI `admin reset-mfa` & `admin reset-password` mereset `mfa_enabled=false`, memperbarui hash `bcrypt` (cost 12), mencabut semua sesi aktif, dan mencatat >= 2 entri `audit_log` (`admin.cli_reset_mfa`, `admin.cli_reset_password`).

## (e) Rekomendasi
**DIUBAH (Transport Sesi Opaque DIPERTAHANKAN, Disempurnakan Throttle 60 Detik + CLI Pemulihan Akses Admin).**

## (f) Implementasi yang Dikerjakan
- `internal/auth/queries/auth.sql` (`GetActiveSessionByDigest`, `TouchSessionActivity`, `ResetUserMFAByID`) + `sqlc generate`.
- `internal/api/middleware/auth.go` (`maybeTouchSessionActivity` interval 60s).
- `cmd/kopdes/admin_ops.go` & `main.go` (`admin reset-mfa --email ...`, `admin reset-password --email ... (--password | --password-stdin)`).

## (g) Risiko Sisa
- Selisih maksimal perpanjangan `idle_expires_at` dapat tertinggal <= 60 detik bila pengguna hanya melakukan 1 ketukan pada detik ke-10 setelah menit terakhir; dapat diabaikan dibanding `SESSION_IDLE_TTL=30m` dan jauh lebih hemat beban IOPS DB.
- Pemilik VPS harus mengamankan akses SSH/server karena pemegang akses shell dapat menjalankan `kopdes admin reset-mfa` (**KEPUTUSAN PEMILIK**).
