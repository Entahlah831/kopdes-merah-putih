# ADR-0001: Arsitektur Modular Monolith dengan Batas Modul Teruji

- **Status:** Diterima (Fase 1, setelah penyusunan Baseline Fase 0)
- **Tanggal:** 2026-11-22
- **Keputusan Terkait:** Prompt v9 — Fase 0 s.d. Fase 4, Eksperimen Terukur `spike/filesvc` (ADR-0002)

---

## 1. Konteks

Backend **Kopdes Merah Putih** (Go + PostgreSQL 16) melayani **satu koperasi di satu lokasi** dengan target lalu lintas sekitar **100 sampai 1.000 pengunjung aktif**, dikembangkan dan dioperasikan oleh **satu developer** pada **satu VPS** menggunakan satu berkas `.env` sebagai sumber kebenaran konfigurasi.

Pemilik sempat menginginkan *"setiap fitur esensial berbasis microservice"*. Sementara itu, audit arsitektur awal (`pre-modular`) menemukan bahwa beberapa paket domain sebelumnya tercampur melalui SQL lintas tabel (`iuran` melakukan JOIN langsung ke `anggota`, `pesanan` mengubah stok di tabel `produk`, `pembayaran` mengubah `tagihan_iuran` & `pesanan` langsung serta membaca `pengaturan` langsung, `laporan` membaca 5 tabel lintas domain langsung) sehingga batas tanggung jawab modul belum terisolasi.

## 2. Keputusan

Kami menetapkan arsitektur **Modular Monolith** dengan **dua proses runtime dari satu basis kode (`kopdes serve` dan `kopdes worker`, ditambah subperintah CLI operasional/`admin` serta `uiserve`)**, dan **memaksa batas modul secara otomatis melalui unit test arsitektur** (`internal/arch_test.go`):

1. **8 Modul Domain + Runtime Pendukung**:
   - `auth`: identitas, login, MFA TOTP, sesi (`sesi`), `kode_aktivasi`, `rate_limits`, `users`, dan manajemen akun staf.
   - `pengaturan`: konfigurasi koperasi (`pengaturan`) dan catatan audit (`audit_log`).
   - `anggota` *(menyatukan `pendaftaran` & KYC)*: registrasi calon anggota (`pendaftaran`), dokumen KYC (`dokumen_kyc`), persetujuan/penolakan, profil anggota (`anggota`).
   - `iuran`: jenis & `tagihan_iuran`, tarif bulanan, penandaan lunas iuran via antarmuka kontrak.
   - `katalog`: `kategori`, `produk`, dan `promo`.
   - `pesanan`: `pesanan`, `pesanan_item`, daftar antaran/label, pelepasan/penahanan stok via kontrak `katalog/api`.
   - `pembayaran`: `rekening_qris`, `pembayaran`, `pembayaran_alokasi`, `pengembalian_dana`, unggah bukti bayar, alur verifikasi/penolakan/refund admin.
   - `laporan`: agregator murni (**0 tabel** milik sendiri) yang membaca proyeksi read-only melalui antarmuka `*/api`.
   - Komponen runtime bersama non-domain: `internal/filestore` (abstraksi penyimpanan terenkripsi envelope AES-256-GCM) dan `internal/worker` (penjadwal tugas dengan `pg_try_advisory_lock` + tabel `worker_runs`).
2. **Kontrak Antarmuka Sempit (`internal/<modul>/api`)**:
   - Modul A **dilarang** mengimpor paket implementasi `internal/<modulB>`; komunikasi antarmodul hanya boleh melalui `internal/<modulB>/api`.
   - Setiap modul **hanya boleh** menulis/membaca tabel miliknya sendiri (daftar kepemilikan 21 tabel dikunci di `internal/arch_test.go`).
   - Transaksi ACID lintas modul memakai pola **Unit of Work** berbasis `pgx.Tx` yang diteruskan melalui metode kontrak (contoh: `pembayaran` memanggil `iuran/api.Service.TandaiLunas` dan `pesanan/api.Service.TandaiLunas` pada transaksi `tx` yang sama).
3. **Eksperimen Microservice Terukur Tunggal (Fase 3)**:
   - Pemisahan fisik sungguhan **tidak** dilakukan secara serentak. Hanya satu kandidat berisiko rendah dan bernilai isolasi paling tinggi (**`filesvc`** di belakang antarmuka `filestore.FileStore`) yang diuji coba pada branch `spike/filesvc`, lalu hanya diadopsi bila lolos seluruh kriteria angka di ADR-0002.

## 3. Alasan Teknis

1. **Konsistensi Transaksional Database Tunggal**: Alur inti (checkout pesanan & pemotongan stok, pembuatan & persetujuan pembayaran beserta pelunasan tagihan iuran/pesanan, pencegahan race condition kode unik pembayaran dan slot) bergantung pada transaksi ACID PostgreSQL tunggal. Memecah ke layanan terpisah memaksa pola saga/kompensasi asinkron yang berisiko memunculkan selisih uang, pembayaran ganda, stok menggantung, atau status setengah selesai.
2. **Kesederhanaan Operasional ("Ramping" & Satu Sumber `.env`)**: Setiap microservice independen menambah port baru, token otentikasi antar-layanan, konfigurasi jaringan, pipeline build/deploy, rotasi log, healthcheck, serta puluhan variabel `.env` tambahan — bertentangan langsung dengan keterbatasan satu developer di satu VPS.
3. **Skala Lalu Lintas Aktual**: Beban target `<= 1.000` pengunjung masih jauh di bawah titik di mana pemecahan layanan memberi manfaat throughput horizontal. Data baseline Fase 0 (`docs/BASELINE_REPORT.md`) menunjukkan bottleneck utama berasal dari pool/DB latency dan ukuran payload, bukan kurangnya partisi proses antar domain; pemecahan jaringan prematur hanya menambah latensi hop HTTP dan titik gagal baru.
4. **Manfaat Modularitas Tetap Tercapai**: Kebutuhan pemilik yang sebenarnya — batas fitur tegas, kemudahan tim frontend mengintegrasikan per modul, isolasi perubahan agar satu modul tidak merusak tabel modul lain, dan kesiapan diekstrak menjadi microservice di masa depan — terwujud penuh lewat paket `internal/<modul>/api` + `TestArchitecture_*`.

## 4. Alternatif yang Ditolak

- **Big-Bang Microservices (8 layanan terpisah dari awal)**: Ditolak karena meledakkan kompleksitas deployment, mengorbankan atomisitas transaksi pembayaran-pesanan-stok, dan tidak sebanding dengan kapasitas tim 1 developer di 1 VPS.
- **Monolith Tanpa Batas Modul (`pre-modular`)**: Ditolak karena `iuran`, `pesanan`, `pembayaran`, dan `laporan` sebelumnya saling mengakses tabel milik modul lain lewat SQL mentah/sqlc silang sehingga sulit diuji terpisah maupun diekstrak nanti.

## 5. Pemicu Pemecahan di Masa Depan

Ekstraksi satu modul menjadi layanan terpisah hanya dibenarkan apabila salah satu pemicu berikut **terbukti nyata**:

1. **Kapasitas VPS Tunggal Terlampaui**: Beban berkelanjutan tetap jenuh setelah optimasi kueri PostgreSQL, indeks, connection pool, serta penyetelan worker.
2. **Skala Tim Lebih dari Satu Tim Independen**: Ada beberapa tim dengan jadwal rilis terpisah yang saling terhambat bila berbagi siklus build/deploy tunggal.
3. **Kebutuhan Isolasi Keamanan Terbukti**: Misalnya memisahkan pemegang kunci dekripsi dokumen KYC dan bukti transfer dari proses HTTP publik agar potensi kerentanan RCE/path-read pada proses API tidak langsung mengakses kunci dekripsi berkas (kandidat inilah yang diukur pada Fase 3 `spike/filesvc`).

## 6. Konsekuensi & Catatan Kepatuhan

- **Positif**: Satu biner `kopdes`, satu `docker-compose.yml`, satu `.env` sebagai sumber kebenaran, transaksi lintas domain tetap atomic dalam satu `pgx.Tx`, dan tes `go test ./internal -run TestArchitecture` gagal otomatis jika ada pelanggaran impor antarmodul atau SQL lintas kepemilikan tabel.
- **Konsekuensi**: Pengembang baru wajib menghormati pemisahan `internal/<modul>/api` dan tidak menulis kueri sqlc yang menyentuh tabel modul lain.
- **Catatan Hukum (*PERLU VERIFIKASI HUKUM*)**: Keputusan arsitektur ini mengatur isolasi teknis dan kontrol akses modul, sedangkan kecukupan hukum terkait retensi dokumen KYC, penghapusan data ditolak, dan persetujuan pengumpulan data pribadi anggota merujuk regulasi UU No. 27 Tahun 2022 (UU PDP) dan anggaran dasar koperasi (**PERLU VERIFIKASI HUKUM**).
