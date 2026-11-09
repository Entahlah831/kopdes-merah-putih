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
- [ ] **T2. Rate limiter dapat dilewati dan rawan kehabisan memori**
  - [ ] Reproduksi spoofing X-Forwarded-For bypass rate limit.
  - [ ] Batasi proxy terpercaya (`TRUSTED_PROXIES`), batasi ukuran map LRU, tambahkan limit login per akun.
- [ ] **T3. Secret default di kode, AES key tidak pernah dipakai, DSN bocor ke log**
  - [ ] Hapus secret default di `DefaultConfig()`. Gagal start jika kunci < 32 byte.
  - [ ] Sambungkan `AES_MASTER_KEY` ke enkripsi data pribadi anggota dengan AAD.
  - [ ] Blind index per tenant dengan HKDF(master, koperasi_id).
  - [ ] Redaksi DSN password dan PII di log.
- [ ] **T4. Celah cakupan RLS dan bypass platform admin**
  - [ ] Pasang RLS pada `audit_log`, `job_log`, `konfigurasi_aturan`.
  - [ ] Perbaiki jalur login tanpa bypass platform admin pra-autentikasi (`SECURITY DEFINER` sempit).
  - [ ] Tes isolasi tenant pada audit/job log.
- [ ] **T5. Autentikasi dan otorisasi**
  - [ ] Access token 15 menit + refresh token tabel rotasi.
  - [ ] Pilih koperasi/peran aktif secara eksplisit dan deterministik.
  - [ ] Constant-time login response (dummy hash bila user tidak ditemukan).
  - [ ] Biaya bcrypt >= 12 / Argon2id.

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
