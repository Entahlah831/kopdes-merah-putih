# Kriteria Go-Live (`UAT`, `Pilot Terbatas`, & `Produksi Penuh`) — Kopdes Merah Putih

Dokumen ini menetapkan gerbang kelulusan terukur untuk membawa backend **Kopdes Merah Putih** (Modular Monolith `backend/`, 1 koperasi tunggal, 21 tabel PostgreSQL 16, **37 endpoint HTTP**, **44 variabel env <= 45**) beserta UI [frontend/wireframe/](../frontend/wireframe/) ke tahap UAT, Pilot Terbatas, hingga Produksi Penuh. Panduan instalasi produksi VPS tersedia di [PANDUAN_PASANG_VPS_LINUX.md](PANDUAN_PASANG_VPS_LINUX.md) dan tata kelola kunci 85/15 di [KEAMANAN_KUNCI_PEMILIK.md](KEAMANAN_KUNCI_PEMILIK.md).

> **Kebijakan Klaim:** Tidak ada tahap yang boleh ditandai lulus tanpa lampiran log perintah asli, kode exit `0`, atau tangkapan layar verifikasi yang bisa diaudit ulang. Istilah hukum dan akuntansi wajib ditandai **PERLU VERIFIKASI HUKUM** atau **KEPUTUSAN AKUNTAN** sampai disahkan pihak berwenang.

---

## 1. Gerbang Tahap 1 — Lulus UAT (`User Acceptance Testing` di VPS Staging)

| Dimensi | Syarat Kelulusan Wajib | Bukti Verifikasi |
| :--- | :--- | :--- |
| **Fungsional 19 Frame** | Seluruh skenario in-scope pada [MATRIKS_E2E_19_FRAME.md](wireframe/MATRIKS_E2E_19_FRAME.md) berjalan end-to-end (`Daftar -> KYC -> Approval Admin -> Aktivasi -> Login Anggota -> Checkout/Iuran QRIS -> Upload Bukti -> Verifikasi Dual Control Bendahara -> Pemenuhan Petugas Toko -> Selesai`, termasuk perbaikan/tolak KYC, tolak bukti, refund manual, pembatalan, audit log #37, operasional, & pengaturan). | Suite Playwright `01`–`06` PASS + berita acara UAT pengurus. |
| **Konfigurasi Tunggal** | `go -C backend run ./cmd/envcheck --strict --ports --no-listen --env-file "${PWD}/.env.staging"` lolos dengan **44 variabel**, zero konflik port, zero `APP_ENV=test` / seeder di staging/produksi. | Output asli `envcheck STRICT PASS` & `PORTS PASS` + [ENV_REFERENCE.md](ENV_REFERENCE.md). |
| **Pelatihan & Panduan Pengurus** | Admin/Ketua, Bendahara, dan Petugas Toko menyelesaikan minimal 1 siklus latihan langsung memakai [PANDUAN_PENGURUS.md](PANDUAN_PENGURUS.md) dan menandatangani [LEMBAR_PELATIHAN_PENGURUS.md](LEMBAR_PELATIHAN_PENGURUS.md) (termasuk simulasi lupa password & kehilangan MFA). | Lembar pelatihan tertandatangani pengurus inti. |
| **Arsitektur & Skema** | `sqlc generate && git diff --exit-code internal/db` bersih; `TestArchitecture_*` (4/4) & `TestStructural_*` lulus penuh; koneksi aplikasi memakai role non-superuser `kopdes_app`. | Output asli CI PostgreSQL 16. |
| **Kontrol Item Out-of-Scope** | Seluruh item `BLOCKED: butuh keputusan pemilik` (`SIMKOPDES`, `RAT`, `Kas & Mutasi`, `Transfer Bank`, `Upload PDF`, multi-cabang) non-aktif & tidak bisa ditembak lewat API tersembunyi. | Inspeksi UI + `TestRBAC_MatrixMatchesLiveRouterAndEnforcesBoundaries`. |

---

## 2. Gerbang Tahap 2 — Pilot Terbatas (Operasional Terkontrol 1–2 Minggu)

Digunakan apabila pengurus mulai menerima transaksi nyata skala kecil (atau memakai `ALLOW_PERSONAL_PAYMENT_PILOT=true` sambil menunggu QRIS badan hukum terbit — **KEPUTUSAN PEMILIK** & **PERLU VERIFIKASI HUKUM**):

1. **Kelulusan Pentest & DAST Per Peran**: Seluruh pipeline CI Linux (`hadolint`, `trivy`, `semgrep`, `OWASP ZAP` 5 profil per peran termasuk MFA & relaxed RL di [ZAP_TRIAGE.md](security/ZAP_TRIAGE.md), `schemathesis`, `gosec`, `staticcheck`, `govulncheck`, `gitleaks`) bebas temuan `HIGH`/`CRITICAL`/`MEDIUM` terbuka yang belum dimitigasi, dan laporan pentest eksternal ([PENTEST_SCOPE.md](PENTEST_SCOPE.md)) tidak menyisakan temuan `Critical`/`High` terbuka.
2. **Enkripsi & Keamanan Sesi**:
   - `COOKIE_SECURE=auto` / `true` di balik TLS HTTPS (`__Host-kopdes_session`), `ALLOW_BEARER_AUTH=false`, `ALLOWED_ORIGINS` berisi origin HTTPS resmi (tanpa `*`), `TRUSTED_PROXIES` ber-CIDR `/16`–`/32`, `RATE_LIMIT_RPS` berada di rentang produksi (`1..120`, default `30`).
   - Direktori `STORAGE_KYC_DIR` dan `STORAGE_BUKTI_DIR` terpisah, seluruh berkas terenkripsi `.enc` (0 plaintext PNG/JPG magic bytes di disk).
3. **Pembatas Risiko Pilot**:
   - Jika QRIS badan hukum belum aktif: `ALLOW_PERSONAL_PAYMENT_PILOT=true` wajib dikunci ketat (`PILOT_MAX_PER_TX <= 500000`, `PILOT_MAX_PER_DAY <= 2000000`) dan disepakati tertulis oleh pengurus/pemilik (**KEPUTUSAN PEMILIK** & **PERLU VERIFIKASI HUKUM**).
4. **Latihan Bencana `age` Nyata**:
   - `scripts/backup.sh` menghasilkan bundel `.tar.age` memakai public key `age1...`, **tanpa menaruh private key di VPS**, dan `scripts/restore_drill.sh` lulus pemulihan ke DB uji terisolasi (termasuk uji tolak modifikasi 1 byte ciphertext) sebelum pilot dibuka.
5. **SLO Operasional**:
   - Baca p95 `< 300 ms`, Tulis p95 `< 800 ms`, Error 5xx `< 0,5%` pada beban target 200 VU (`100–300` pengguna aktif, laporan [PERFORMANCE_K6.md](PERFORMANCE_K6.md)), serta `/readyz` menunjukkan `database: CONNECTED` dan `worker_degraded: false`.

---

## 3. Gerbang Tahap 3 — Produksi Penuh

| Persyaratan Wajib | Penanggung Jawab | Keterangan |
| :--- | :--- | :--- |
| Rekening QRIS berbadan hukum koperasi aktif & `ALLOW_PERSONAL_PAYMENT_PILOT=false` | Pemilik / Bendahara | Menghapus ketergantungan rekening pribadi (**KEPUTUSAN PEMILIK**). |
| Masa retensi data (`PDP_REJECTED_RETENTION_DAYS`, `BUKTI_RETENTION_DAYS`) disahkan secara hukum & disinkronkan | Penasihat Hukum / Pengurus | **PERLU VERIFIKASI HUKUM** (UU No. 27/2022 PDP & ketentuan pajak/pembukuan). |
| Pemetaan `jenis_iuran.klasifikasi_akuntansi` & SOP selisih kode unik/refund disahkan | Akuntan Koperasi | **KEPUTUSAN AKUNTAN** sebelum tutup buku resmi. |
| Cron backup harian terenkripsi `age` + sinkronisasi off-server (`rclone`/`restic`) berjalan >= 7 hari beruntun | Admin / DevOps | Dipantau log sukses + uji `restore_drill.sh` berkala menggunakan private key dari perangkat offline pemilik. |
| Kebijakan `STOCK_HOLD_MINUTES` dan `DUAL_CONTROL_BREAKGLASS_ENABLED=false` (normal) disahkan tertulis | Pemilik / Pengurus | **KEPUTUSAN PEMILIK** (`breakglass` hanya untuk darurat staf dengan TOTP + audit log). |

---

## 4. Daftar Keputusan Pemilik yang Masih Terbuka (`KP-01` s.d. `KP-10`)

Seluruh butir berikut memiliki nilai bawaan aman di kode/konfigurasi agar pengembangan dan uji otomatis tetap berjalan tanpa blokir, **namun wajib diputuskan secara tertulis oleh Pemilik / Penasihat Hukum / Akuntan** sebelum melewati target gerbang masing-masing (lihat pula ringkasan operasional di [TUGAS_MANUAL_PEMILIK.md](TUGAS_MANUAL_PEMILIK.md)):

| ID | Topik & Parameter | Nilai Bawaan Aman Saat Ini | Kategori | Target Penutupan | Dampak Jika Belum Diputus |
| :---: | :--- | :--- | :--- | :---: | :--- |
| **KP-01** | Status QRIS badan hukum koperasi vs mode pilot (`ALLOW_PERSONAL_PAYMENT_PILOT`, `PILOT_MAX_PER_TX`, `PILOT_MAX_PER_DAY`) | `false` (`500000` / `2000000` Rupiah bila pilot aktif) | **KEPUTUSAN PEMILIK** & **PERLU VERIFIKASI HUKUM** | **Pilot Terbatas** (wajib `false` di **Produksi Penuh**) | Pembayaran QRIS tidak dapat diaktifkan sebelum ada rekening QRIS aktif, atau berisiko hukum bila memakai rekening pribadi tanpa batas pilot. |
| **KP-02** | Kebijakan reservasi stok & TTL bayar QRIS (`STOCK_HOLD_MINUTES`, `PAYMENT_EXPIRY_DURATION`) | `60` menit hold, `15m` expiry QRIS | **KEPUTUSAN PEMILIK** | **UAT / Pilot Terbatas** | Durasi penahanan stok dapat terlalu pendek untuk jam tutup toko/verifikasi malam atau terlalu lama menahan stok dari anggota lain. |
| **KP-03** | Mode darurat kendali ganda (`DUAL_CONTROL_BREAKGLASS_ENABLED`) | `false` (wajib 2 orang berbeda untuk verifikasi/refund) | **KEPUTUSAN PEMILIK** | **UAT / Pilot Terbatas** | Jika hanya 1 pengurus bertugas saat jam operasional tanpa kebijakan break-glass, verifikasi tunai/QRIS tertahan menunggu staf kedua. |
| **KP-04** | Jumlah digit kode unik QRIS statis (`QRIS_UNIQUE_DIGITS`) | `3` digit (`1..999`, kapasitas pending global `50`) | **KEPUTUSAN PEMILIK** | **Pilot Terbatas** | Jika lonjakan tanggal gajian/jatuh tempo melebihi ratusan tagihan menunggu bayar serentak, perlu keputusan naik ke `4` digit. |
| **KP-05** | Masa simpan data pendaftar ditolak (`PDP_REJECTED_RETENTION_DAYS`) | `30` hari sebelum `pdp purge` | **PERLU VERIFIKASI HUKUM** | **Produksi Penuh** | Berpotensi tidak selaras dengan kewajiban audit internal atau hak penghapusan data pribadi UU PDP bila belum ditinjau penasihat hukum. |
| **KP-06** | Masa retensi berkas bukti bayar non-lunas (`BUKTI_RETENTION_DAYS`) | `365` hari sebelum `bukti purge` (bukti `lunas` tetap disimpan) | **PERLU VERIFIKASI HUKUM** & **KEPUTUSAN AKUNTAN** | **Produksi Penuh** | Dokumen sengketa/transaksi gagal terhapus terlalu cepat atau menumpuk melebihi masa simpan pembukuan koperasi. |
| **KP-07** | Pemetaan pos `jenis_iuran.klasifikasi_akuntansi` & SOP selisih kode unik/refund | Label teks netral di tabel `jenis_iuran` & kolom laporan pembayaran | **KEPUTUSAN AKUNTAN** | **Produksi Penuh** | Laporan neraca/SHU tahunan oleh akuntan eksternal butuh pemetaan akun pembukuan dan perlakuan selisih kode unik (`1..999`). |
| **KP-08** | Ruang lingkup item `BLOCKED` Figma wireframe (`SIMKOPDES`, `RAT`, `Kas & Mutasi`, `Transfer Bank`, `Upload PDF`, multi-cabang) | Dinonaktifkan sesuai lingkup 1 koperasi tunggal ([ANALISIS_KESENJANGAN.md](wireframe/ANALISIS_KESENJANGAN.md)) | **KEPUTUSAN PEMILIK** | **Produksi Penuh** | Jika pemilik menginginkan salah satu fitur luar lingkup di atas pada rilis lanjutan, perlu ADR dan fase kerja terpisah pasca-MVP. |
| **KP-09** | Proteksi bot publik & batas laju produksi (`CAPTCHA_ENABLED`, `RATE_LIMIT_RPS`, daftar `MFA_REQUIRED_ROLES`, WAF edge) | `CAPTCHA_ENABLED=false`, `RATE_LIMIT_RPS=30`, `MFA_REQUIRED_ROLES=admin,bendahara` | **KEPUTUSAN PEMILIK** | **Pilot Terbatas / Produksi Penuh** | Endpoint pendaftaran/login publik tanpa Cloudflare Turnstile/WAF bergantung penuh pada rate limiter VPS (`30 rps`). |
| **KP-10** | Tata kelola kunci backup offline (`BACKUP_AGE_RECIPIENT`, lokasi simpan minimal 2 salinan private key `age`, jadwal `restore_drill.sh`) | `BACKUP_RETENTION_DAYS=30`; private key wajib off-server | **KEPUTUSAN PEMILIK** | **Pilot Terbatas** (sebelum uang nyata) | Jika private key `age` hilang bersama pengelola IT tunggal, seluruh arsip `.tar.age` tidak dapat didekripsi selamanya. |

---

## 5. Kondisi Penghentian / Rollback Segera

Hentikan lalu lintas mutasi atau rollback segera (`PAYMENTS_ENABLED=false` / `REGISTRATION_ENABLED=false` atau hentikan kontainer `api`/`worker`) apabila terjadi salah satu kondisi berikut:
1. Terdeteksi inkonsistensi stok atau nominal lunas (`stok_tertahan < 0`, `stok < 0`, atau selisih antara `pembayaran.nominal_total` dan alokasi).
2. Kegagalan dekripsi berkas KYC/bukti atau kebocoran plaintext PII/file `.enc`.
3. `worker_degraded=true` terus-menerus pada `/readyz` sehingga kedaluwarsa reservasi stok/pembayaran terhenti.
4. Kebocoran kredensial `.env` atau temuan `Critical`/`High` aktif pasca-rilis -> rotasi kunci (`AES_MASTER_KEY` + `AES_PREVIOUS_KEYS`, `BLIND_INDEX_SECRET`, password DB, serta cabut sesi aktif).
