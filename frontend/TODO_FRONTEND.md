# Daftar Tugas Pengembangan Frontend (`TODO_FRONTEND.md`)

Backend **Kopdes Merah Putih** sudah terpisah rapi di [../backend/](../backend/) (`37` endpoint terautentikasi, RBAC teruji, & `44` variabel env). Versi SPA di [wireframe/](wireframe/) saat ini adalah **referensi fungsional sederhana** yang menutupi seluruh alur inti end-to-end beserta layar operasional/admin dasar.

Dokumen ini menjadi daftar tugas resmi bagi **tim frontend** untuk menyempurnakan tampilan, pengalaman pengguna (*UX*), dan keandalan antarmuka **tanpa melanggar aturan kontrak backend maupun ruang lingkup 1 koperasi tunggal**.

> **Aturan Teknis Keras Sebelum Menulis Kode UI:**
> 1. **Anti-XSS (Semgrep ERROR):** Dilarang menggunakan `.innerHTML` di [wireframe/src/](wireframe/src/) maupun [tester/](tester/). Selalu gunakan DOM builder aman (`el()`, `.textContent`, komponen React/Vue aman tanpa `dangerouslySetInnerHTML`/`v-html`).
> 2. **Tanpa Port / URL / Rahasia Hardcoded:** Baca selalu `/config.json` saat boot (`api_public_url: ""` untuk same-origin di balik `Caddy`, atau URL dari env) dan `GET /api/v1/public/config`.
> 3. **Cookie `HttpOnly` Utama + CSRFGuard:** Selalu kirim `credentials: "include"` dan header `X-Requested-With` pada setiap mutasi (`POST/PUT/PATCH/DELETE`). Jangan simpan `session_token` di `localStorage`/`sessionStorage`.

---

## P0 — Desain Sistem, Tata Letak 19 Frame Figma, & Responsivitas Mobile

- [ ] **Selaraskan Visual Design System dengan 19 Frame Figma (`5RsPLIopItTEIp21N1VU2x`):** Ganti gaya wireframe polos di `style.css` / framework UI target menjadi palet warna resmi Koperasi Desa Merah Putih, tipografi terstruktur, komponen kartu produk, stepper visual status pesanan/iuran, dan ilustrasi/logo resmi di header (`LOGO`).
- [ ] **Navigasi Mobile (`375px`) & Drawer Responsif:** Pada saat ini nav topbar masih datar sederhana; ubah menjadi bottom navigation / hamburger drawer rapi tanpa memotong tombol logout atau filter katalog.
- [ ] **Komponen Feedback Global (Toast, Modal Konfirmasi, Loading Skeleton):** Gantikan teks status `"Memuat..."` / pesan alert inline pada aksi destruktif (Tolak KYC, Tolak Bukti Pembayaran, Batal Pesanan, Nonaktifkan Akun, Submit Refund) dengan modal konfirmasi eksplisit serta indikator loading/disabled agar pengguna tidak menekan dua kali.
- [ ] **Tampilan Kode QRIS Interaktif:** Pada portal tagihan (`04a`) dan checkout pesanan QRIS (`06`), render visual QR (`active_qris.qris_image_url` atau konversi `qris_payload` di sisi klien tanpa layanan pihak ketiga yang membocorkan data transaksi) serta tombol **"Salin Nominal Tepat (termasuk kode unik)"** agar warga desa tidak salah transfer digit unik.

---

## P1 — Penyempurnaan Layar Operasional & Admin Sederhana (Sudah Ada Kerangkanya di `admin.ts` & `operasional.ts`)

Layar berikut sudah tersedia fungsinya secara sederhana dan terverifikasi backend; tim frontend perlu memoles UX, validasi form klien, dan kelengkapan sub-fiturnya:

- [ ] **02 Dashboard — Tabel Audit Log (`GET /api/v1/audit-log`):**
  - Tambahkan kontrol paginasi (`limit`, `offset`, tombol Berikutnya/Sebelumnya), filter cepat `aksi` & `entitas` (dropdown pilihan umum), serta tombol **"Salin / Ekspor Tampilan Audit"**.
- [ ] **04b Verifikasi Pembayaran — Alur Pengembalian Dana Dual-Control (`POST /api/v1/pembayaran/{id}/refund`):**
  - Saat ini formulir refund menggunakan JSON (`ajukan`, `setujui`, `tolak`). Tambahkan opsi unggah **foto bukti transfer refund** (`multipart/form-data`, terkompresi via kanvas JPG/PNG sebelum dikirim) dan pratinjau gambar terdekripsi (`GET /api/v1/pembayaran?refund_bukti_id=...`) menggunakan `fetchBlob()` + `URL.revokeObjectURL`.
  - Tampilkan status pengajuan refund berjalan secara otomatis pada detail pembayaran (tidak sekadar instruksi teks).
- [ ] **07 Operasional — Laporan (`GET /api/v1/laporan`):**
  - Tambahkan pratinjau ringkasan angka total (Total Omzet, Total Tunggakan, Total Pembayaran) di atas tabel, preset rentang tanggal ("Hari Ini", "7 Hari Terakhir", "Bulan Berjalan"), dan tampilan cetak rapi (*print stylesheet*).
- [ ] **07 Operasional — Daftar Antar & Label Kurir (`GET /api/v1/pesanan/daftar-antar`):**
  - Sediakan tata letak **Kartu/Stiker Label Antar Siap Cetak** (`window.print()`) selain tabel dan tombol unduh CSV agar kurir toko desa mudah menempel alamat di paket.
- [ ] **07 Operasional — Katalog Produk, Kategori, & Promo (`GET/POST /api/v1/katalog/produk`):**
  - Form sederhana baru mencakup buat produk & buat kategori. Tambahkan **Edit Produk / Ubah Stok & Status** (`action: "update_produk"`), unggah/validasi gambar preview, serta tab **Manajemen Kode Promo** (`?tab=promo` & `action: "create_promo"`).
- [ ] **07 Operasional — Iuran Bulanan & Keringanan (`GET/POST /api/v1/iuran/*`, `PATCH /api/v1/iuran/tagihan/{id}/keringanan`):**
  - Tambahkan daftar tagihan seluruh anggota (`scope=tunggakan` / filter status) dengan tombol **"Berikan Keringanan"** per baris serta kolom pilihan `klasifikasi_akuntansi` (*KEPUTUSAN AKUNTAN*).
- [ ] **07/08 Operasional — Daftar Anggota, Impor CSV, & Ubah Status (`GET/POST/PATCH /api/v1/anggota/*`):**
  - Tambahkan halaman daftar anggota lengkap (mode masked default + tombol khusus Admin **`unmask=true`** hanya saat diperlukan), tombol Ubah Status (`aktif` / `nonaktif`), serta formulir unggah/tempel teks **Impor Massal CSV** dengan pratinjau **`?dry_run=true`** sebelum eksekusi final.
- [ ] **08 Pengaturan & Manajemen Pengguna (`/api/v1/users`, `/api/v1/pengaturan`, `POST /api/v1/pembayaran` `create_rekening`):**
  - Tambahkan formulir **"Tambah Rekening QRIS Merchant"** (`action: "create_rekening"`) langsung di tab Pengaturan agar Admin tidak perlu memakai API manual untuk mendaftarkan QRIS awal.
  - Pada tabel pengguna, tambahkan pemilih perubahan **Peran** inline, tombol salin tautan WhatsApp (`wa_link`) untuk **Kode Aktivasi / Reset Sandi 1x Pakai**, serta dialog panduan pemulihan MFA pengguna.
- [ ] **Portal Profil Saya — Ganti Sandi & Aktivasi Akun (`POST /api/v1/auth/password`):**
  - Sediakan menu khusus ganti kata sandi mandiri saat pengguna sudah login (`current_password` + `new_password`) dan halaman aktivasi kode dari tautan `?code=...`.

---

## P2 — Penguatan UX Keamanan, Aksesibilitas, & Cakupan E2E

- [ ] **Widget CAPTCHA Turnstile Dinamis:** Periksa `publicCfg.captcha?.enabled` saat boot; jika `true`, muat widget Turnstile memakai `captcha.site_key` di form Daftar (`/api/v1/public/pendaftaran`) & Login.
- [ ] **Aksesibilitas (WCAG 2.1 AA) & Navigasi Keyboard:** Pastikan seluruh modal, tabel, badge status, dan input memiliki `aria-label`, `label[for]`, serta rasio kontras warna memenuhi standar.
- [ ] **Pemulihan Jaringan & Banner Mode Pemeliharaan:** Tangani `features.read_only_mode=true` (tampilkan banner jelas bahwa transaksi tulis sedang ditutup sementara) serta HTTP `429 TOO_MANY_REQUESTS` dengan informasi ramah pengguna desa.
- [ ] **Perluasan Tes Playwright (`wireframe/e2e/`):** Tambahkan E2E untuk alur **2-orang Refund Manual**, **Impor CSV dry-run**, **Buat Kategori/Promo**, dan **Reset Password + Reset MFA UI** setelah antarmuka P1 di atas lengkap.

---

## Batasan Keras — Fitur Dilarang / `BLOCKED: butuh keputusan pemilik` (`KP-08`)

Jangan membangun fitur di bawah ini pada frontend sampai pemilik koperasi menerbitkan ADR dan endpoint backend baru ([ANALISIS_KESENJANGAN.md](../docs/wireframe/ANALISIS_KESENJANGAN.md)):
1. **Multi-koperasi / multi-cabang / multi-gudang** (backend dikunci untuk 1 koperasi di 1 lokasi).
2. **SIMKOPDES sync, manajemen Rapat Anggota Tahunan (RAT), simpanan/pinjaman/SHU, double-entry kas/akuntansi, atau impor CSV mutasi bank.**
3. **Transfer Bank antar-rekening otomatis, QRIS dinamis EMVCo, upload dokumen PDF KYC/bukti, ataupun dokumen ke-4 Surat Pernyataan.**
4. **CMS publik berita/agenda/FAQ dinamis atau penayangan data NIK/HP publik tanpa masker.**
