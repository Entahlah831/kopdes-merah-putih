# PROGRESS PERBAIKAN BACKEND KOPDES MERAH PUTIH (v2)

Berdasarkan temuan terverifikasi dari audit kode pada `prompt-antigravity-v2-temuan-terverifikasi.md`.

---

## Status Temuan & Rencana Kerja

### I. KRITIS
- [ ] **K1. Deploy Docker Compose menjalankan API sebagai SUPERUSER (RLS tidak berlaku)**
  - [ ] Reproduksi dengan tes gagal: Superuser/bypassrls lolos start, atau DSN deploy default superuser.
  - [ ] Pisahkan role: `kopdes_migrator`, `kopdes_app` (LOGIN, NOSUPERUSER, NOBYPASSRLS, NOCREATEDB), `kopdes_readonly_auditor`.
  - [ ] API startup check: tolak start jika `current_user` memiliki `rolsuper` atau `rolbypassrls` (kecuali dev eksplisit).
  - [ ] Tangani/perbaiki `SET ROLE` di `WithTenantTx`.
  - [ ] Tes integrasi: login sebagai `kopdes_app` apa adanya (tanpa SET ROLE) harus lolos tes isolasi lintas tenant; tanpa `app.current_koperasi_id` query tenant = 0 baris.
  - [ ] Batasi port PostgreSQL host ke `127.0.0.1` di docker-compose.
- [ ] **K2. Partisi hanya sampai 31 Des 2026, tanpa partisi default**
  - [ ] Reproduksi dengan tes gagal: INSERT kas_mutasi / audit_log dengan tanggal 2027-01-01 gagal.
  - [ ] Buat migrasi `000002_add_default_partitions.up.sql` dan `.down.sql`: DEFAULT partition + 12 bulan ke depan.
  - [ ] Tambahkan job pembuatan partisi otomatis (idempoten) menjaga minimal 3 bulan ke depan.
  - [ ] Tes: insert tanggal di luar partisi terdaftar tidak boleh gagal.
- [ ] **K3. Alur verifikasi pembayaran rusak dan tidak terhubung ke tagihan/pesanan**
  - [ ] Reproduksi dengan tes gagal: approve memanggil `pembayaran_verifikasi_log` dengan kolom salah, tidak memanggil fungsi akuntansi/kas, mengizinkan approve sebelum bukti bayar, tidak cek nominal aktual vs total.
  - [ ] Buat migrasi fungsi verifikasi pembayaran atomik atau transaksi Go setara (`FOR UPDATE`, status `menunggu_verifikasi`, periksa nominal, perbarui tagihan/pesanan/kas_mutasi/outbox, tulis log dengan skema kolom yang benar).
  - [ ] Tangani error HTTP 400 tanpa membocorkan internal database.
  - [ ] Tes end-to-end: buat tagihan, buat pembayaran, unggah bukti, verifikasi pengguna berbeda, pastikan tagihan lunas dan kas terisi; tes kurang bayar, bayar ganda, approve ganda.

---

### II. TINGGI
- [ ] **T1. Pembayaran tidak terikat tagihan/pesanan (nominal dan anggota dari klien)**
  - [ ] Reproduksi tes gagal.
  - [ ] Request memuat `tagihan_iuran_id[]` / `pesanan_id`, hitung nominal di server, buat `pembayaran_alokasi`, gunakan `anggota_id` dari token.
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
