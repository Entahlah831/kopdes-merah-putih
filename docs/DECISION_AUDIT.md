# Audit Keputusan Desain (Prompt v9A) — Kopdes Merah Putih (`decision-audit`)

Dokumen ini merangkum hasil audit menyeluruh terhadap seluruh keputusan arsitektur, keamanan, data pribadi, dan operasional pada backend ramping Kopdes Merah Putih (**1 koperasi di 1 lokasi**, target **100–1.000 pengunjung**, warga desa dengan jaringan seluler, staf non-teknis, **1 developer**, **1 VPS**).

## Metode & Bobot Audit
Setiap keputusan (D1–D10) diaudit secara independen terhadap minimal 2 opsi alternatif pada 5 kriteria berbobot tetap:
1. **Keamanan uang dan data pribadi**: `30%`
2. **Kesederhanaan operasi untuk 1 developer**: `20%`
3. **Kecocokan dengan skala (100–1.000 pengunjung) & UX warga desa**: `20%`
4. **Keandalan & kemudahan pemulihan**: `15%`
5. **Biaya & kemampuan dibalik (*reversibility*)**: `15%`

Setiap perubahan diverifikasi secara *fail-first* dan diukur menggunakan tes nyata (konkurensi 50 goroutine, benchmark flood 500 IP palsu, pengukuran penulisan sesi 20 request, uji concurrent 2 worker, uji live RBAC 36 rute) terhadap PostgreSQL nyata.

---

## Ringkasan Akhir Keputusan (D1–D10)

| Keputusan | Status | Alasan | Bukti | KEPUTUSAN PEMILIK |
| :--- | :--- | :--- | :--- | :--- |
| **D1. Waktu pemotongan stok** | **DIUBAH** *(Reservasi `stok_tertahan` + `perlu_tindak_lanjut`)* | Checkout tanpa menahan stok memungkinkan jual-berlebih & verifikator gagal `409` meski warga sudah membayar. Reservasi menahan kuota tanpa memotong stok fisik hingga bayar disetujui. | [ADR-001](adr/ADR-001-reservasi-stok.md); `TestDecisionAudit_D1_StockReservationConcurrency50Buyers` (50 pembeli -> tepat 5 sukses, 45 `409`), `TestDecisionAudit_D1_ApprovalAfterExpiredHold_GracefulTindakLanjut` (`200 perlu_tindak_lanjut`). | **KEPUTUSAN PEMILIK**: Setel lama tahanan stok `STOCK_HOLD_MINUTES` (bawaan aman `60` menit). |
| **D2. Pengembalian dana (refund)** | **DIUBAH** *(Ditambahkan manual dual-control + bukti terenkripsi)* | Pembatalan/penolakan setelah bayar atau stok habis setelah masa tahan butuh jejak audit & koreksi laporan/stok. | [ADR-002](adr/ADR-002-pengembalian-dana-manual.md); `TestDecisionAudit_D2_ManualRefundFlowAndReports` (`POST /api/v1/pembayaran/{id}/refund`, self-approve `403`, stok pulih `3->5`, laporan `total_dikembalikan`). | **KEPUTUSAN PEMILIK**: Tetapkan SOP pencairan transfer/tunai manual di luar sistem & **KEPUTUSAN AKUNTAN** terkait jurnal koreksi refund. |
| **D3. Transport sesi** | **DIUBAH** *(Cookie `HttpOnly` utama + `__Host-` + CSRF + `COOKIE_SECURE`/`ALLOW_BEARER_AUTH`)* | `r.TLS != nil` salah di belakang proxy TLS; Bearer di memori JS hilang saat reload; cookie butuh CSRF guard (`Origin` + `X-Requested-With`). | [ADR-003](adr/ADR-003-transport-sesi-cookie-csrf.md); `TestDecisionAudit_D3_CookieOnlyAndCSRFAndProxySecure` (proxy HTTPS `__Host-`, spoof proxy diabaikan, reload `200`, CSRF cross-origin & tanpa header `403`, Bearer `401` di Set B). | **KEPUTUSAN PEMILIK**: Biarkan `ALLOW_BEARER_AUTH=false` di produksi kecuali memiliki klien API mobile/mesin non-browser khusus. |
| **D4. Pemisahan kunci kriptografi & storage** | **DIUBAH** *(HKDF-SHA256 4 subkunci + amplop seragam + `STORAGE_KYC_DIR`/`STORAGE_BUKTI_DIR`)* | Satu kunci mentah berisiko blast-radius; skema KYC vs bukti sebelumnya tidak seragam; dua class berkas perlu path/volume & retensi terpisah. | [ADR-004](adr/ADR-004-pemisahan-kunci-hkdf-dan-storage.md); `TestPurposeKeyDerivationAndCrossPurposeIsolation`, `TestProofEnvelopeAndSeparateStorageIsolation`, `TestRelocateLegacySubdirs_Idempotent` (`kopdes storage migrate-files`). | **KEPUTUSAN PEMILIK**: Tetapkan retensi bukti non-lunas `BUKTI_RETENTION_DAYS` (bawaan `365`) & jadwal rotasi `AES_MASTER_KEY`. |
| **D5. Backup** | **DIUBAH** *(Enkripsi kunci publik terautentikasi `age` + arsip DB/KYC/Bukti + kunci off-server)* | `openssl aes-256-cbc` tanpa AEAD dan menyimpan kunci di `.env` yang sama dengan DB di VPS. | [ADR-005](adr/ADR-005-backup-age-terautentikasi.md); `scripts/backup.sh`, `scripts/restore_drill.sh` (uji tolak tamper 1 byte `age -d` + `>= 21` tabel), `scripts/restore_test.sh`, & `.github/workflows/ci.yml`. | **KEPUTUSAN PEMILIK**: Simpan **private key `age` mutlak di luar server** (hilang = data tidak bisa dibuka) + salin arsip ke remote off-server (`rclone`/`restic`). |
| **D6. Pembatas laju database** | **DIUBAH** *(Hybrid LRU memori L1 + persistensi DB L2 saat tembus batas; hapus ticker goroutine)* | Menulis DB `rate_limits` di setiap hit publik memicu amplifikasi write saat flood IP palsu. | [ADR-006](adr/ADR-006-pembatas-laju-hybrid.md); `TestDecisionAudit_D6_Hybrid500FakeIPsZeroDBAmplification`: `500` IP palsu = **`500 row DB (196.48ms)` vs `0 row DB (1.99ms)`** + persistensi saat restart + CGNAT aman. | **KEPUTUSAN PEMILIK**: Aktifkan aturan WAF/Rate Limit Cloudflare di edge DNS untuk mitigasi DDoS volumetrik sebelum menyentuh VPS. |
| **D7. Kendali ganda vs staf sedikit** | **DIUBAH** *(Tetap ketat default + opsi break-glass khusus `admin` teraudit)* | Staf desa terbatas (1 admin, 1 bendahara) bisa buntu jika salah satu sakit/cuti. | [ADR-007](adr/ADR-007-kendali-ganda-dan-breakglass.md); `TestDecisionAudit_D7_DualControlNormalAndBreakglass` (default `403`; `bendahara` selalu `403`; admin dengan alasan `>=10` + TOTP anti-replay `200` & `audit_log`). | **KEPUTUSAN PEMILIK**: Biarkan `DUAL_CONTROL_BREAKGLASS_ENABLED=false` (bawaan aman), aktifkan sementara hanya jika staf darurat tunggal & audit rutin log break-glass. |
| **D8. Penjadwalan tugas berkala** | **DIUBAH** *(Layanan `worker` Compose + `pg_try_advisory_lock` + `/readyz`)* | Cron host berada di luar Compose/`.env`, tidak terpantau `/readyz`, dan gagal diam-diam. | [ADR-008](adr/ADR-008-worker-terjadwal-advisory-lock.md); `TestDecisionAudit_D8_TwoConcurrentWorkersNoDuplicateBills` (0 tagihan ganda), `TestDecisionAudit_D8_CrashRestartCatchUpAndReadyz`. | **KEPUTUSAN PEMILIK**: Tetap jadwalkan backup host off-server harian di luar container Compose agar kredensial & penyimpanan terpisah. |
| **D9. Sesi opaque & pemulihan admin** | **DIUBAH** *(Opaque session **DIPERTAHANKAN** + throttle write 60s + CLI `admin reset-mfa/password`)* | Opaque wajib demi pencabutan instan; `UPDATE sesi` tiap GET boros IOPS; kehilangan HP admin tidak boleh mengunci koperasi. | [ADR-009](adr/ADR-009-sesi-opaque-throttle-dan-cli-pemulihan.md); `TestDecisionAudit_D9_SessionTouchThrottleAndInstantRevocation` (**20 request `29.88ms`, 0 tulis DB** + cabut instan `401`), `TestAdminRecoveryCLI_ResetMFAAndResetPassword`. | **KEPUTUSAN PEMILIK**: Batasi akses SSH VPS ketat; pakai `kopdes admin reset-mfa` / `reset-password` hanya bila pemilik memverifikasi kehilangan akses admin. |
| **D10. RBAC, Minimisasi PII, Kode Unik, & CI** | **DIUBAH** *(RBAC sinkron, `foto_url` tervalidasi, `tanggal_lahir` opsional, CI DB penuh)*; sebagian **DIPERTAHANKAN** *(kode unik 3 digit, iuran, retensi)* | Menutup celah `bendahara` pada status pesanan, `petugas_toko` pada tunai, XSS `foto_url`, mengumpulkan `tanggal_lahir` tak terpakai, dan gap skema migrasi CI. | [ADR-010](adr/ADR-010-rbac-minimisasi-data-dan-parameter-operasional.md); `TestRBAC_MatrixMatchesLiveRouterAndEnforcesBoundaries` (**36 rute sinkron**, `19x5` live matrix + tes tanpa `tanggal_lahir`), `TestValidateFotoURL`. | **KEPUTUSAN PEMILIK**: `QRIS_UNIQUE_DIGITS=3` (kapasitas 5% pada limit global 50), `ALLOW_PERSONAL_PAYMENT_PILOT=false`; **PERLU VERIFIKASI HUKUM**: retensi 30d/365d & pilot rekening; **KEPUTUSAN AKUNTAN**: klasifikasi iuran & selisih kode unik. |

---

## Ringkasan Keputusan Terbuka Sebelum Go-Live

### 1. KEPUTUSAN PEMILIK (Nilai Bawaan Aman Sudah Terpasang)
- `STOCK_HOLD_MINUTES=60`: durasi tahanan stok saat checkout menunggu bayar & verifikasi.
- `DUAL_CONTROL_BREAKGLASS_ENABLED=false`: izin darurat admin memverifikasi transaksinya sendiri dengan MFA segar.
- `ALLOW_BEARER_AUTH=false`: nonaktifkan Bearer di produksi kecuali ada klien non-browser.
- `ALLOW_PERSONAL_PAYMENT_PILOT=false`: melarang penggunaan QRIS rekening perorangan pengurus.
- `QRIS_UNIQUE_DIGITS=3` (`PENDING_LIMIT_GLOBAL=50`): jumlah digit acak nominal QRIS.
- Pengaturan Cloudflare WAF/Rate Limit edge dan penyimpanan **private key `age`** di luar server secara offline/terpisah.

### 2. PERLU VERIFIKASI HUKUM
- Masa retensi hapus keras pendaftaran ditolak (`PDP_REJECTED_RETENTION_DAYS=30`) dan retensi berkas bukti non-lunas (`BUKTI_RETENTION_DAYS=365`) sesuai peraturan UU PDP & perkoperasian Indonesia.
- Aspek legal & pajak apabila pemilik mengaktifkan `ALLOW_PERSONAL_PAYMENT_PILOT=true` untuk rekening pribadi pengurus.

### 3. KEPUTUSAN AKUNTAN
- Penetapan kode/nama `jenis_iuran.klasifikasi_akuntansi` (simpanan wajib/pokok/iuran operasional dalam batasan lingkup koperasi tunggal).
- Pencatatan selisih kode unik (`pembayaran.selisih_kode_unik` & `catatan_akuntansi`) serta pencatatan koreksi `pengembalian_dana` pada tutup buku koperasi.
