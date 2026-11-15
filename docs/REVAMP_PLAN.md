# Rencana Revamp Backend Ramping (`lean-revamp`)

Tag pengaman: `pre-revamp` | Branch kerja: `lean-revamp`  
Status DB produksi: **Belum ada data produksi nyata yang perlu dipertahankan** (migrasi historis `000001..000018` diarsipkan pada tag `pre-revamp` dan digantikan oleh skema awal tunggal `000001` ramping).

---

## 1. Metrik Awal (Baseline Sebelum Revamp)

| Metrik | Nilai Awal (`pre-revamp`) | Target Akhir (`lean-revamp`) |
| :--- | :--- | :--- |
| Berkas terlacak Git | `224` berkas | Berkurang signifikan (hapus artifak hasil generate & paket bloat) |
| Total baris kode Go (termasuk tes) | `23.657` baris (`18` direktori paket) | Ramping, maks `<= 400` baris/handler, `<= 60` baris/fungsi |
| Tabel database dasar | `65` tabel (plus partisi bulanan/default di `18` file `.up.sql`) | `<= 20` tabel (`19` tabel tunggal non-RLS) |
| Endpoint HTTP (`server.go`) | `56` rute (`55` di API + `/metrics`) | `<= 35` endpoint (tanpa `/metrics` dan tanpa keranjang server) |
| Variabel di `.env.example` | `95` variabel | `<= 40` variabel (satu sumber `.env`) |
| Dependensi langsung (`go.mod`) | `4` (`jwt/v5`, `google/uuid`, `pgx/v5`, `golang.org/x/crypto`) | `3` (`google/uuid`, `pgx/v5`, `golang.org/x/crypto`) — hapus `jwt/v5` |

### Rincian Baris Go Per Paket Sebelum Revamp

| Paket | Berkas | Total Baris | Keputusan |
| :--- | :---: | :---: | :--- |
| `.` (`service.go`, `service_test.go`) | 2 | 1.073 | **CUT** — Dipindah ke paket domain terstruktur per fitur. |
| `./cmd/envcheck` | 2 | 310 | **CHANGE** — Disederhanakan ke <= 40 variabel `.env` tunggal. |
| `./cmd/loadtest` | 2 | 308 | **CUT** — Dihapus dan diganti panduan/skrip k6 ringan bila diperlukan. |
| `./cmd/seed_test_accounts` | 2 | 99 | **CHANGE** — Diselaraskan ke skema 19 tabel & CLI `cmd/kopdes`. |
| `./cmd/server` | 2 | 163 | **CHANGE** — Hapus inisialisasi worker partisi/outbox/metrics/JWT. |
| `./cmd/uitester` | 2 | 403 | **CHANGE** — Diselaraskan ke konfigurasi publik & port tunggal `.env`. |
| `./internal/api` | 10 | 8.381 | **CHANGE** — Router dirampingkan ke <= 35 endpoint & tes terfokus. |
| `./internal/api/handlers` | 14 | 5.205 | **CHANGE / SPLIT** — Dipecah ke paket domain `internal/<domain>/` (<= 400 baris/file). |
| `./internal/api/middleware` | 8 | 682 | **CHANGE** — Sesi opaque DB, rate limiter DB, hapus `tenant.go`. |
| `./internal/api/response` | 2 | 132 | **KEEP** — Helper respons JSON konsisten tanpa detail internal. |
| `./internal/auth` | 2 | 206 | **CHANGE** — Hapus JWT/refresh; ganti domain auth sesi opaque + TOTP + aktivasi. |
| `./internal/config` | 4 | 979 | **CHANGE** — Pangkas field bloat hingga <= 40 env var dari `.env`. |
| `./internal/db` | 14 | 3.410 | **CHANGE** — Hapus partisi, advisory lock, RLS test, dan hasil edit tangan; murni `sqlc`. |
| `./internal/outbox` | 4 | 525 | **CUT** — Dihapus; diganti tautan `wa.me` siap pakai tanpa gateway. |
| `./internal/qris` | 2 | 426 | **CUT** — Mesin EMVCo QRIS dinamis dihapus sesuai lingkup QRIS statis. |
| `./internal/security` | 5 | 592 | **KEEP / CHANGE** — Pertahankan AES-256-GCM, HMAC blind index NIK, TOTP; tambah envelope file KYC. |
| `./internal/simkopdes` | 2 | 50 | **CUT** — Tidak ada integrasi/sinkronisasi SIMKOPDES di lingkup klien. |
| `./internal/testseed` | 2 | 713 | **CHANGE** — Seeder pengujian disesuaikan ke skema single-tenant baru. |

---

## 2. Daftar Inventaris KEEP / CUT / CHANGE / ADD

### 2.1 Paket Go & CLI

| Item | Status | Alasan (Satu Kalimat) |
| :--- | :--- | :--- |
| `internal/auth` (lama JWT) | **CUT -> ADD** | Hapus logika JWT/refresh token dan bangun ulang domain `internal/auth` berbasis sesi opaque di PostgreSQL. |
| `internal/simkopdes` | **CUT** | Klien tidak memakai sinkronisasi SIMKOPDES. |
| `internal/outbox` | **CUT** | Tidak menggunakan gateway WhatsApp/SMS berbayar maupun worker outbox latar. |
| `internal/qris` | **CUT** | Pembayaran QRIS cukup statis + kode unik integer, tanpa parser/generator EMVCo dinamis. |
| `cmd/loadtest` | **CUT** | Beban target hanya 100–1.000 pengunjung sehingga binary loadtest Go internal tidak diperlukan. |
| `service.go` / `service_test.go` | **CUT** | Logika layanan lama root package digantikan oleh service per domain yang menggunakan `sqlc`. |
| `cmd/kopdes` | **ADD** | CLI operasional untuk `admin create`, `iuran generate`, `pdp purge`, dan `pesanan expire`. |
| `internal/pendaftaran` | **ADD** | Paket domain pendaftaran publik, verifikasi Turnstile, `FileStore` KYC terenkripsi, dan persetujuan. |
| `internal/anggota` | **CHANGE** | Paket domain anggota dengan masking PII, ubah status, dan impor/ekspor CSV aman. |
| `internal/iuran` | **CHANGE** | Paket domain iuran bulanan sederhana, keringanan beralasan, tunggakan, dan tautan `wa.me`. |
| `internal/katalog` | **CHANGE** | Paket domain kategori, produk (harga umum/anggota), dan promo kuota sederhana. |
| `internal/pesanan` | **CHANGE** | Paket domain checkout langsung tanpa keranjang server, kunci stok, dan daftar antar + label alamat. |
| `internal/pembayaran` | **CHANGE** | Paket domain pembayaran QRIS statis + kode unik, bukti unik, verifikasi admin/bendahara, dan tunai/kwitansi. |
| `internal/laporan` | **CHANGE** | Paket domain laporan status anggota, tunggakan, penjualan, pembayaran per metode, dan ekspor CSV aman. |
| `internal/security`, `internal/config`, `internal/api/middleware`, `internal/api/response` | **CHANGE** | Dipertahankan prinsip keamanannya dan disesuaikan untuk sesi opaque, rate limit DB, serta <= 40 env var. |
| `cmd/server`, `cmd/envcheck`, `cmd/uitester` | **CHANGE** | Disesuaikan dengan port tunggal `.env` dan skema single-tenant ramping. |

### 2.2 Tabel Database (`65` Lama -> `19` Baru)

| Tabel Lama / Baru | Status | Alasan (Satu Kalimat) |
| :--- | :--- | :--- |
| `wilayah`, `koperasi`, `koperasi_legalitas`, `koperasi_anggaran_dasar`, `unit_usaha`, `koperasi_nomor_urut` | **CUT** | Aplikasi hanya melayani satu koperasi tunggal tanpa hierarki multi-tenant maupun RLS. |
| `roles`, `permissions`, `role_permissions`, `user_roles` | **CUT** | Empat peran tetap (`admin`, `bendahara`, `petugas_toko`, `anggota`) cukup sebagai kolom `users.peran` dengan constraint `CHECK`. |
| `refresh_tokens`, `mfa_login_sessions` | **CUT** | Digantikan satu tabel `sesi` opaque server-side dengan kolom `scope` dan TTL dari env. |
| `dokumen`, `pendaftaran_status_log`, `anggota_status_log`, `anggota_alamat`, `anggota_keluarga`, `anggota_kontak`, `consent_data`, `pengurus_pengawas`, `rapat_anggota` | **CUT** | Diserap atau dipangkas ke kolom inti tabel `anggota`, `pendaftaran`, `dokumen_kyc`, dan `audit_log`. |
| `kebijakan_keringanan_iuran`, `keringanan_iuran_riwayat`, `job_log` | **CUT** | Keringanan dicatat langsung di `tagihan_iuran` dan audit dicatat di `audit_log`. |
| `merek`, `supplier`, `produk_varian`, `produk_media`, `produk_harga`, `gudang`, `stok_saldo`, `stok_mutasi`, `promo_produk`, `promo_kategori`, `promo_penggunaan`, `keranjang`, `keranjang_item`, `pengiriman_pesanan`, `retur_refund`, `pembelian_supplier`, `penerimaan_barang` | **CUT** | Keranjang pindah ke frontend, multi-gudang/varian/supplier/retur dihapus, dan stok dikelola langsung via `produk.stok` + baris `pesanan`. |
| `penyedia_pembayaran`, `metode_pembayaran`, `pembayaran_bukti`, `pembayaran_verifikasi_log`, `mutasi_bank_impor`, `periode_kas`, `kas_mutasi`, `konfigurasi_aturan`, `sinkronisasi_simkopdes`, `notifikasi_template`, `notifikasi`, `outbox_event`, `idempotency_keys` | **CUT** | Impor mutasi CSV, buku kas, hash chain, SIMKOPDES, outbox notifikasi, dan tabel idempotensi terpisah dihapus sesuai lingkup ramping. |
| `pengaturan` | **ADD** | Menyimpan konfigurasi satu baris koperasi (nama, alamat, jumlah digit kode unik, ongkir tetap). |
| `users`, `sesi`, `kode_aktivasi`, `rate_limits` | **CHANGE / ADD** | Mengelola user terenkripsi, sesi opaque, aktivasi sekali pakai, dan rate limiter di database. |
| `anggota`, `pendaftaran`, `dokumen_kyc` | **CHANGE** | Menyimpan data terenkripsi PII/KYC dan blind index NIK berversi kunci tanpa RLS multi-tenant. |
| `jenis_iuran`, `tagihan_iuran` | **CHANGE** | Tagihan iuran bulanan per anggota dengan dukungan keringanan dan penanda `KEPUTUSAN AKUNTAN`. |
| `kategori`, `produk`, `promo`, `pesanan`, `pesanan_item` | **CHANGE** | E-katalog harga umum/anggota, promo sederhana, serta checkout langsung dan jadwal pengantaran. |
| `rekening_qris`, `pembayaran`, `pembayaran_alokasi` | **CHANGE** | Pembayaran QRIS statis + kode unik integer, tunai berkwitansi, dan penandaan under/overpayment. |
| `audit_log` | **CHANGE** | Tabel audit append-only tunggal yang diproteksi trigger penolak `UPDATE`/`DELETE` tanpa partisi maupun hash chain. |

### 2.3 Endpoint HTTP (`56` Lama -> `35` Baru)

| Kelompok Endpoint | Status | Alasan (Satu Kalimat) |
| :--- | :--- | :--- |
| `GET /metrics` | **CUT** | Endpoint Prometheus dihapus bersama paket metrics. |
| `POST /api/v1/auth/refresh`, `POST /api/v1/auth/revoke`, `POST /api/v1/auth/mfa/enable` | **CUT** | Digantikan `POST /api/v1/auth/logout`, verifikasi MFA terpadu, dan sesi opaque server-side. |
| `GET/POST/DELETE /api/v1/toko/keranjang*` | **CUT** | Keranjang disimpan di sisi frontend agar lebih sederhana dan stateless di server. |
| `POST /api/v1/pembayaran/mutasi-bank/impor`, `GET /api/v1/pembayaran/rekonsiliasi/harian` | **CUT** | Impor mutasi bank CSV dan rekonsiliasi otomatis dihapus sesuai lingkup klien. |
| `GET /api/v1/kas/mutasi` | **CUT** | Buku kas mutasi terpisah dihapus karena laporan memakai data pembayaran/pesanan langsung. |
| `POST /api/v1/pendaftaran/purge-ditolak`, `POST /api/v1/iuran/generate`, `POST /api/v1/toko/pesanan/{id}/expire` | **CUT** | Tugas batch dipindah dari HTTP ke CLI `cmd/kopdes` yang dijalankan scheduler OS. |
| `GET /api/v1/katalog/stok`, `GET /api/v1/katalog/harga`, `GET /api/v1/katalog/promo`, `GET /api/v1/iuran/jenis` | **CUT / MERGE** | Data stok, harga aktif, promo, dan jenis iuran disajikan terpadu pada endpoint katalog/iuran utama agar jumlah endpoint `<= 35`. |
| `POST /api/v1/public/pendaftaran`, `POST /api/v1/public/pendaftaran/{id}/dokumen` | **ADD** | Menutup blocker onboarding dengan mengizinkan warga desa mendaftar dan mengunggah KYC tanpa akun lebih dulu. |
| `POST /api/v1/auth/password`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me` | **ADD / CHANGE** | Mendukung aktivasi akun via kode sekali pakai, reset password, ganti password, dan pencabutan sesi instan. |
| `GET/PUT /api/v1/pengaturan` | **ADD** | Mengatur identitas koperasi, ongkir tetap, digit kode unik, dan rekening QRIS statis aktif dalam satu tempat. |
| `POST /api/v1/users`, `PATCH /api/v1/users/{id}`, `GET /api/v1/users` | **ADD / CHANGE** | Memungkinkan admin membuat akun staf/operator dan mengubah peran/status aktif beserta pencabutan sesi otomatis. |
| `GET /api/v1/pendaftaran`, `POST /api/v1/pendaftaran/{id}/verifikasi`, `GET /api/v1/pendaftaran/{id}/dokumen/{tipe}` | **CHANGE / ADD** | Verifikasi pendaftaran oleh admin plus unduhan dokumen KYC terdekripsi on-the-fly dengan header aman. |
| `POST /api/v1/anggota/impor`, `GET /api/v1/anggota`, `GET /api/v1/anggota/{id}`, `PATCH /api/v1/anggota/{id}/status` | **ADD / CHANGE** | Menambah impor CSV anggota awal (`dry-run`) dan mempertahankan pengelolaan anggota dengan NIK/HP tersamar. |
| `POST /api/v1/iuran/jenis`, `GET /api/v1/iuran/tagihan`, `PATCH /api/v1/iuran/tagihan/{id}/keringanan` | **CHANGE** | Daftar tagihan terpadu (saya / semua / tunggakan + `wa.me`), jenis iuran, dan pembebasan beralasan. |
| `GET/POST /api/v1/katalog/produk`, `POST /api/v1/toko/checkout`, `GET /api/v1/pesanan`, `PATCH /api/v1/pesanan/{id}/status`, `GET /api/v1/pesanan/daftar-antar` | **CHANGE / ADD** | Checkout langsung tanpa keranjang, pengelolaan status pesanan/batal, dan manifest daftar antar + label alamat. |
| `GET/POST /api/v1/pembayaran`, `POST /api/v1/pembayaran/{id}/verifikasi`, `GET /api/v1/laporan` | **CHANGE / ADD** | Menyatukan buat pembayaran QRIS/tunai/bukti, verifikasi antrean tanpa dual-approval threshold, dan laporan + ekspor CSV. |

### 2.4 Skrip, Konfigurasi, Dokumen, dan Variabel `.env`

| Item | Status | Alasan (Satu Kalimat) |
| :--- | :--- | :--- |
| `docs/DOKUMENTASI_LENGKAP_API_KOPDES.*`, `docs/kopdes.postman_collection.json`, `docs/kopdesClient.ts`, `docs/prometheus.example.yml`, `scripts/generate_*.py`, `frontend-reference/` | **CUT** | Artifak generate besar dan klien terlacak dihapus agar repositori ramping dan `openapi.yaml` menjadi sumber spesifikasi tunggal. |
| `docs/HASH_CHAIN_ADVISORY_LOCKS.md`, `docs/QRIS_UJI_LAPANGAN.md`, `docs/SCOPE_RECONCILIATION.md`, `docs/LAPORAN_REMEDIASI_DAN_PERUBAHAN_KODE.md`, `docs/backend_integration_guide.md`, `docs/erd.md` | **CUT** | Dokumen arsitektur/fitur yang dipangkas atau historis dihapus agar dokumentasi tetap ringkas dan akurat. |
| `db/migrations/000002_* .. 000018_*` | **CUT** | Dikonsolidasikan menjadi satu migrasi awal bersih `000001_init_kopdes_schema.up.sql` sebelum ada data produksi. |
| `docs/REVAMP_PLAN.md`, `docs/REVAMP_REPORT.md`, `docs/SKILLS.md` | **ADD** | Menyediakan inventaris rencana revamp, daftar skill aktif, dan bukti laporan akhir. |
| `docs/README.md`, `docs/BACKEND_GUIDE.md`, `docs/openapi.yaml`, `docs/roles_matrix.json`, `docs/TUGAS_MANUAL_PEMILIK.md`, `docs/DOC_DISCREPANCIES.md`, `docs/frontend/` | **CHANGE** | Diperbarui agar 100% sesuai kontrak backend ramping yang baru. |
| `frontend-tester/` | **CHANGE** | Dipertahankan polos tanpa framework/CDN serta diperbarui untuk seluruh skenario baru end-to-end. |
| `Dockerfile`, `docker-compose.yml`, `docker-compose.test.yml`, `.github/workflows/ci.yml`, `sqlc.yaml` | **CHANGE** | Disinkronkan ke variabel `.env` <= 40, check `sqlc generate`, dan CI ramping tanpa pengecualian global `gosec`. |
| `scripts/backup.sh`, `scripts/restore_drill.sh`, `scripts/restore_test.sh`, `scripts/promote_env.*`, `scripts/port_check.*` | **CHANGE** | Mengenkripsi `pg_dump` dan salinan KYC tanpa hash-chain, serta sinkron dengan env ramping. |
| Variabel `.env` (JWT, metrics, partition, outbox, SIMKOPDES, dual-approval, dinamik QRIS, webhook/SMS) | **CUT** | Dihapus dari `.env.example` sehingga total variabel <= 40 dan tidak ada konfigurasi mati. |
| Variabel `.env` baru (`SESSION_IDLE_TTL`, `SESSION_ABS_TTL`, `MFA_REQUIRED_ROLES`, `QRIS_UNIQUE_DIGITS`, `CAPTCHA_SECRET_KEY`, `TURNSTILE_VERIFY_URL`, `PENDING_ORDER_MAX_PER_USER`, `PENDING_PAYMENT_MAX_GLOBAL`, `PENDING_PAYMENT_MAX_PER_MEMBER`, `INITIAL_ADMIN_PASSWORD`) | **ADD / CHANGE** | Menggantikan pengaturan JWT/bloat dengan kontrol sesi opaque, Turnstile server-side, dan batas slot pembayaran/pesanan. |

---

## 3. Daftar Tugas Pelaksanaan Berurutan

- [x] Tag `pre-revamp` & branch `lean-revamp`
- [x] **Fase 0**: 3 skill lokal (`lean-go-backend`, `kopdes-domain-rules`, `security-baseline-kopdes`), `AGENTS.md`, `docs/SKILLS.md`, hapus `GEMINI.md`
- [x] **Fase 1**: Buat `docs/REVAMP_PLAN.md`
- [ ] **Fase 2**: Pangkas paket, dokumen, migrasi 000002..000018, dan dependensi `jwt/v5`
- [ ] **Fase 3**: Skema `000001` (<= 20 tabel), query `sqlc`, domain packages (`<= 400` baris/file, `<= 60` baris/fungsi), CLI `cmd/kopdes`, endpoint `<= 35`
- [ ] **Fase 7**: `.env.example` (<= 40 variabel), `envcheck`, skrip backup/restore/promote, `Dockerfile`/`docker-compose`
- [ ] **Fase 8**: Tes unit, integrasi, keamanan, Set A/B, Frontend Tester, dokumentasi Markdown/OpenAPI, dan CI
- [ ] **Fase 9**: Laporan akhir `docs/REVAMP_REPORT.md` beserta output verifikasi asli
