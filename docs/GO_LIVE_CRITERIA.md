# Kriteria Go-Live (`UAT`, `Pilot Terbatas`, & `Produksi Penuh`) — Kopdes Merah Putih

Dokumen ini menetapkan gerbang kelulusan terukur untuk membawa backend **Kopdes Merah Putih** (Modular Monolith, 1 koperasi tunggal, 21 tabel PostgreSQL 16, 36 endpoint HTTP, **43 variabel env**) beserta UI [frontend-wireframe/](../frontend-wireframe/) ke tahap UAT, Pilot Terbatas, hingga Produksi Penuh.

> **Kebijakan Klaim:** Tidak ada tahap yang boleh ditandai lulus tanpa lampiran log perintah asli, kode exit `0`, atau tangkapan layar verifikasi yang bisa diaudit ulang.

---

## 1. Gerbang Tahap 1 — Lulus UAT (`User Acceptance Testing` di VPS Staging)

| Dimensi | Syarat Kelulusan Wajib | Bukti Verifikasi |
| :--- | :--- | :--- |
| **Fungsional 19 Frame** | Seluruh skenario in-scope pada [MATRIKS_E2E_19_FRAME.md](wireframe/MATRIKS_E2E_19_FRAME.md) berjalan end-to-end (`Daftar -> KYC -> Approval Admin -> Aktivasi -> Login Anggota -> Checkout/Iuran QRIS -> Upload Bukti -> Verifikasi Dual Control Bendahara -> Pemenuhan Petugas Toko -> Selesai`, termasuk skenario perbaikan/tolak KYC, tolak bukti, & pembatalan). | Suite Playwright `01`–`05` PASS + berita acara UAT pengurus. |
| **Konfigurasi Tunggal** | `go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.staging` lolos dengan **43 variabel**, zero konflik port, zero `APP_ENV=test` / seeder di staging/produksi. | Output asli `envcheck STRICT PASS` & `PORTS PASS`. |
| **Arsitektur & Skema** | `sqlc generate && git diff --exit-code internal/db` bersih; `TestArchitecture_*` (4/4) & `TestStructural_*` lulus penuh; koneksi aplikasi memakai role non-superuser `kopdes_app`. | Output asli CI PostgreSQL 16. |
| **Kontrol Item Out-of-Scope** | Seluruh item `BLOCKED: butuh keputusan pemilik` (`SIMKOPDES`, `RAT`, `Kas & Mutasi`, `Transfer Bank`, `Upload PDF`, multi-cabang) non-aktif & tidak bisa ditembak lewat API tersembunyi. | Inspeksi UI + `TestRBAC_MatrixMatchesLiveRouterAndEnforcesBoundaries`. |

---

## 2. Gerbang Tahap 2 — Pilot Terbatas (Operasional Terkontrol 1–2 Minggu)

Digunakan apabila pengurus mulai menerima transaksi nyata skala kecil (atau memakai `ALLOW_PERSONAL_PAYMENT_PILOT=true` sambil menunggu QRIS badan hukum terbit — **KEPUTUSAN PEMILIK** & **PERLU VERIFIKASI HUKUM**):

1. **Kelulusan Pentest & DAST**: Seluruh pipeline CI Linux (`hadolint`, `trivy`, `semgrep`, `OWASP ZAP`, `schemathesis`, `gosec`, `staticcheck`, `govulncheck`, `gitleaks`) bebas temuan `HIGH`/`CRITICAL` yang belum dimitigasi, dan laporan pentest eksternal ([PENTEST_SCOPE.md](PENTEST_SCOPE.md)) tidak menyisakan temuan `Critical`/`High` terbuka.
2. **Enkripsi & Keamanan Sesi**:
   - `COOKIE_SECURE=auto` / `true` di balik TLS HTTPS (`__Host-kopdes_session`), `ALLOW_BEARER_AUTH=false`, `ALLOWED_ORIGINS` berisi origin HTTPS resmi (tanpa `*`), `TRUSTED_PROXIES` ber-CIDR `/16`–`/32`.
   - Direktori `STORAGE_KYC_DIR` dan `STORAGE_BUKTI_DIR` terpisah, seluruh berkas terenkripsi `.enc` (0 plaintext PNG/JPG magic bytes di disk).
3. **Pembatas Risiko Pilot**:
   - Jika QRIS badan hukum belum aktif: `ALLOW_PERSONAL_PAYMENT_PILOT=true` wajib dikunci ketat (`PILOT_MAX_PER_TX <= 500000`, `PILOT_MAX_PER_DAY <= 2000000`) dan disepakati tertulis oleh pengurus/pemilik (**KEPUTUSAN PEMILIK** & **PERLU VERIFIKASI HUKUM**).
4. **Latihan Bencana `age` Nyata**:
   - `scripts/backup.sh` menghasilkan bundel `.tar.age` memakai public key `age1...`, **tanpa menaruh private key di VPS**, dan `scripts/restore_drill.sh` lulus pemulihan ke DB uji terisolasi (termasuk uji tolak modifikasi 1 byte ciphertext) sebelum pilot dibuka.
5. **SLO Operasional**:
   - Baca p95 `< 300 ms`, Tulis p95 `< 800 ms`, Error 5xx `< 0,5%` pada beban terukur (`k6` / `tools/loadtest`), serta `/readyz` menunjukkan `database: CONNECTED` dan `worker_degraded: false`.

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

## 4. Kondisi Penghentian / Rollback Segera

Hentikan lalu lintas mutasi atau rollback segera (`PAYMENTS_ENABLED=false` / `REGISTRATION_ENABLED=false` atau hentikan kontainer `api`/`worker`) apabila terjadi salah satu kondisi berikut:
1. Terdeteksi inkonsistensi stok atau nominal lunas (`stok_tertahan < 0`, `stok < 0`, atau selisih antara `pembayaran.nominal_total` dan alokasi).
2. Kegagalan dekripsi berkas KYC/bukti atau kebocoran plaintext PII/file `.enc`.
3. `worker_degraded=true` terus-menerus pada `/readyz` sehingga kedaluwarsa reservasi stok/pembayaran terhenti.
4. Kebocoran kredensial `.env` atau temuan `Critical`/`High` aktif pasca-rilis -> rotasi kunci (`AES_MASTER_KEY` + `AES_PREVIOUS_KEYS`, `BLIND_INDEX_SECRET`, password DB, serta cabut sesi aktif).
