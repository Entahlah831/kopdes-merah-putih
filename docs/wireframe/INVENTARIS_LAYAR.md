# Inventaris Layar Wireframe Figma Kopdes Merah Putih

- **Sumber Wireframe:** File Figma `5RsPLIopItTEIp21N1VU2x` (`Wireframe-Kopdes`), Halaman `0:1` (mode **READ-ONLY**, inspeksi node & render visual `docs` + PNG lokal)
- **Total Frame Teridentifikasi:** **19 frame** (7 frame draf awal + 12 frame layar bernomor terstruktur)

---

## Ringkasan Kelompok Layar

| Kategori | Rentang Node ID | Jumlah | Keterangan |
| :--- | :--- | :---: | :--- |
| Draf Konsep Awal | `14:6`, `71:123`, `25:147`, `27:20`, `14:335`, `25:273`, `25:409` | 7 | Eksplorasi awal publik, daftar member, admin chart, struktur, FAQ, kontak |
| Layar Publik Bernomor (`01a`–`01e`) | `84:55` – `84:59` | 5 | Halaman beranda publik modular 10 seksi |
| Portal Pengurus / Verifikator / Bendahara (`02`, `03a`, `03b`, `04b`) | `77:55`, `77:225`, `77:370`, `78:179` | 4 | Dashboard pengurus, antrean & detail KYC, verifikasi & alokasi pembayaran |
| Portal Anggota / Toko (`04a`, `05`, `06`) | `78:55`, `78:310`, `78:473` | 3 | Tagihan iuran & upload bukti, katalog publik/anggota, detail pesanan & pengiriman |

> **Layar Implisit yang Direferensikan Navigasi Namun Belum Digambar Frame Tersendiri:**
> - **Masuk & MFA TOTP** (ditautkan tombol `Masuk` pada header `01a` & `05`)
> - **Keranjang / Modal Checkout** (ditautkan tombol `Keranjang (0)` pada header `04a`, `06` dan tombol `Pesan` pada `05`)

---

## A. Kelompok Draf Konsep Awal (7 Frame)

### 1. `Informasi umum` (`14:6`)
- **Peran Pengguna:** Publik / Pengunjung
- **Komponen:** Header navigasi (`Logo`, `Daftar`, `Masuk`, `Informasi umum`, `Tentang Kami`, `Produk usaha`, `Hubungi Kami`, `Struktur Manajemen`), placeholder `foto`.
- **Bidang Formulir:** Tidak ada.
- **Aksi/Tombol:** Tautan navigasi header, `Daftar`, `Masuk`.
- **Navigasi:** Menuju `Daftar Member`, halaman masuk, serta halaman profil/informasi publik.
- **Keadaan (States):**
  - *Digambar:* State statis/placeholder.
  - *TIDAK Digambar:* Memuat (loading), kosong (empty), error, sukses.

### 2. `faq` (`71:123`)
- **Peran Pengguna:** Publik / Anggota
- **Komponen:** Header (`Logo`, `Daftar`, `Masuk`), blok `Pertanyaan umum`, kategori `Cara Belanja`, `Pembayaran`, `Pengembalian dana`, `Aplikasi`, `Kategori`.
- **Bidang Formulir:** Tidak ada.
- **Aksi/Tombol:** `Daftar`, `Masuk`, tab/kartu kategori FAQ.
- **Navigasi:** Header publik.
- **Keadaan (States):**
  - *Digambar:* Tata letak daftar topik statis.
  - *TIDAK Digambar:* Memuat, kosong, error, isi jawaban dinamis, pencarian FAQ.

### 3. `Daftar Member` (`25:147`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Publik / Calon Anggota
- **Komponen:** Form `Aktivasi Akun Koprasi` dengan dua seksi (`Informasi Pribadi` & `Informasi Koperasi`), dua kotak centang persetujuan domisili & UU No. 27 Tahun 2022 (UU PDP), tombol kirim.
- **Bidang Formulir:** `Email`, tombol `Kirim OTP`, `Nomor Induk Kependudukan`, `Nama Lengkap`, `Jenis Kelamin`, `Nomor HP`, `Buat Kata Sandi`, `Ulangi Kata Sandi`, `Provinsi`, `Kabupaten/Kota`, `Kecamatan`, `Desa/Kelurahan`, `Koperasi`.
- **Aksi/Tombol:** `Kirim OTP`, `Kirim`, 2 checkbox pernyataan domisili & persetujuan UU PDP.
- **Navigasi:** Berdiri sendiri sebagai layar registrasi calon anggota.
- **Keadaan (States):**
  - *Digambar:* State form kosong default.
  - *TIDAK Digambar:* Validasi error per field, loading kirim, error duplikat NIK/HP/rate-limit, sukses submit & upload berkas KYC.

### 4. `Dashboard-admin` (`27:20`)
- **Peran Pengguna:** Pengurus / Admin
- **Komponen:** Sketsa ringkas `Pembeli`, `Penjualan`, `Grafik penjualan bulanan` *(placeholder teks `twefdscx` menandakan sketsa awal yang digantikan oleh frame `02 Dashboard Pengurus`)*.
- **Bidang Formulir:** Tidak ada.
- **Aksi/Tombol:** Tidak ada.
- **Navigasi:** Tidak ada.
- **Keadaan (States):** Hanya sketsa kasar; semua state dinamis tidak digambar.

### 5. `Catalog` (`14:335`)
- **Peran Pengguna:** Publik / Anggota
- **Komponen:** Grid `Produk Rekomdasi` dengan contoh item (`Beras 5 Kg`, `Gas LPG 3 Kg` Rp 16.000, `Gula 600 g` Rp 11.000, `Minyak Goreng 2L` Rp 38.000 / Rp 58.000, `Mie Instan isi 40` Rp 85.000). *(Digantikan oleh frame bernomor `05 Katalog Produk`).*
- **Bidang Formulir:** Tidak ada.
- **Aksi/Tombol:** `BELI`.
- **Navigasi:** Tidak ada navigasi lengkap.
- **Keadaan (States):** Hanya contoh kartu statis; state memuat/kosong/error/stok habis tidak digambar.

### 6. `Struktur manajemen` (`25:273`)
- **Peran Pengguna:** Publik
- **Komponen:** Header navigasi, bagian `Struktur Manajemen` (`Dewan Pengawas`, `Dewan Pengurus`).
- **Bidang Formulir:** Tidak ada.
- **Aksi/Tombol:** Tautan header (`Daftar`, `Masuk`, menu informasi).
- **Navigasi:** Terintegrasi melalui blok Transparansi `01d`.
- **Keadaan (States):** Hanya tata letak statis; semua state dinamis tidak digambar.

### 7. `Hubungi Kami` (`25:409`)
- **Peran Pengguna:** Publik
- **Komponen:** Header navigasi, banner `Kami siap Membantu`, `Frequently Asked Questions`, kartu `NO Handphone`, `Alamat`, `Customer care`, `sosmade`.
- **Bidang Formulir:** Tidak ada form kontak kirim pesan.
- **Aksi/Tombol:** Tautan informasi & header.
- **Navigasi:** Ditautkan dari CTA `01e` dan header `Kontak`.
- **Keadaan (States):** Hanya state statis; state memuat/error tidak digambar.

---

## B. Kelompok Layar Bernomor Inti (12 Frame)

### 8. `01a Homepage – Header, Hero & Ringkasan` (`84:55`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Publik / Pengunjung / Anggota
- **Komponen:** Topbar (`Logo / Nama Koperasi`, `Beranda`, `Tentang Kami`, `Keanggotaan`, `Unit Usaha`, `Katalog`, `Berita`, `Kontak`, `Masuk`, `Daftar Anggota`), Seksi `02 — HERO` (headline, subcopy, placeholder `Foto kegiatan / produk asli`), Seksi `03 — RINGKASAN KOPERASI` (4 kartu metrik: `Anggota terdaftar`, `Unit usaha aktif`, `Wilayah layanan`, `Tahun berdiri`), box `Catatan: Isi hanya dengan angka terverifikasi. Jangan memakai angka dummy seolah data nyata.`
- **Bidang Formulir:** Tidak ada.
- **Aksi/Tombol:** `Gabung Menjadi Anggota`, `Lihat Unit Usaha`, `Masuk`, `Daftar Anggota`.
- **Navigasi:** Header menuju seksi beranda, katalog (`05`), daftar (`25:147`), masuk.
- **Keadaan (States):** Placeholder `[ angka ]` + catatan larangan angka dummy. Memuat/error backend tidak digambar.

### 9. `01b Homepage – Tentang & Unit Usaha` (`84:56`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Publik
- **Komponen:** Seksi `04 — TENTANG KOPERASI` (`Visi`, `Misi`, `Prinsip koperasi`), Seksi `05 — UNIT USAHA UNGGULAN` (`Gerai Sembako`, `Gudang & Distribusi`, `Unit Usaha [Nama]` dengan `Jam & lokasi: [isi]`).
- **Bidang Formulir:** Tidak ada.
- **Aksi/Tombol:** `Selengkapnya ->`, tombol `Detail` di tiap kartu unit usaha.
- **Navigasi:** Lanjutan beranda `01a`.
- **Keadaan (States):** Konten placeholder statis; loading/error/kosong tidak digambar.

### 10. `01c Homepage – Produk & Cara Bergabung` (`84:57`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Publik
- **Komponen:** Seksi `06 — PRODUK / KATALOG PILIHAN` (4 kartu produk menampilkan dual-harga `Umum: Rp ..` dan `Anggota: Rp ..` + `Catatan: Bedakan harga umum dan harga anggota hanya jika memang diterapkan`), Seksi `07 — CARA BERGABUNG` (5 langkah: `1 Isi formulir`, `2 Unggah dokumen`, `3 Verifikasi`, `4 Keputusan`, `5 Aktivasi anggota`).
- **Bidang Formulir:** Tidak ada.
- **Aksi/Tombol:** `Lihat semua katalog ->`, `Daftar Sekarang`.
- **Navigasi:** Ke `05 Katalog Produk` dan `Daftar Member`.
- **Keadaan (States):** State statis; memuat/kosong/error tidak digambar.

### 11. `01d Homepage – Berita & Transparansi` (`84:58`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Publik
- **Komponen:** Seksi `08 — BERITA DAN AGENDA` (3 kartu berita + panel `Agenda`: RAT, Kegiatan koperasi, Perubahan layanan), Seksi `09 — TRANSPARANSI DAN KEPERCAYAAN` (4 kartu: `Legalitas`, `Struktur organisasi`, `Kebijakan privasi`, `Ringkasan laporan`) + catatan privasi ketat: *"Jangan tampilkan NIK, alamat pribadi, kontak pribadi, bukti pembayaran, atau data keluarga di website publik. Hanya laporan yang disetujui untuk publikasi."*
- **Bidang Formulir:** Tidak ada.
- **Aksi/Tombol:** Tautan berita & transparansi.
- **Navigasi:** Menuju rincian informasi publik.
- **Keadaan (States):** Statis; loading/kosong/error tidak digambar.

### 12. `01e Homepage – CTA & Footer` (`84:59`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Publik
- **Komponen:** Seksi `10 — CTA DAN FOOTER` (`Siap bergabung bersama kami?`), footer 4 kolom (`Alamat & kontak`, `Tautan penting`, `Legal`, `Media sosial`).
- **Bidang Formulir:** Tidak ada.
- **Aksi/Tombol:** `Gabung Menjadi Anggota`, `Hubungi Kami`.
- **Navigasi:** Menuju pendaftaran & halaman kontak.
- **Keadaan (States):** Statis; state dinamis tidak digambar.

### 13. `02 Dashboard Pengurus` (`77:55`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Pengurus / Admin
- **Komponen:** Sidebar (`UTAMA: Ringkasan`, `KELEMBAGAAN: Data Koperasi, Wilayah & Legalitas, Unit Usaha, Anggota, Pengurus & Pengawas, Rapat Anggota (RAT)`, `AKSES: Pengguna, Role & Permission`, `PELAPORAN: Laporan, Audit Trail`), topbar (`Cari...`, avatar), 4 KPI card (`Anggota aktif`, `Menunggu verifikasi`, `Tagihan menunggak`, `Pesanan diproses`), grafik `Arus kas bulanan`, daftar `Perlu tindakan` (`Pendaftaran menunggu verifikasi`, `Pembayaran menunggu verifikasi`, `Stok di bawah minimum`, `Sinkronisasi SIMKOPDES gagal`), tabel `Aktivitas terbaru (audit trail)` (`Waktu`, `Pengguna`, `Aksi`, `Objek`, `Status`).
- **Bidang Formulir:** Pencarian global `Cari...`, dropdown filter `Periode: Bulan ini`.
- **Aksi/Tombol:** `Ekspor`, tautan sidebar ke modul-modul, item `Perlu tindakan`.
- **Navigasi:** Breadcrumb `Dashboard / Ringkasan`; menu ke verifikasi, pengguna, laporan, audit.
- **Keadaan (States):** Placeholder KPI `[ angka ]` & badge `Berhasil`/`Ditolak`; memuat/kosong/error API tidak digambar.

### 14. `03a Verifikasi Anggota – Antrean` (`77:225`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Verifikator Keanggotaan / Admin / Petugas
- **Komponen:** Sidebar (`Antrean Pendaftaran`, `Riwayat Keputusan`), filter bar (`Cari nama / no. pendaftaran`, `Status: Semua`, `Rentang tanggal`), tabel antrean (`No. Pendaftaran`, `Nama pemohon`, `NIK` dimasking `••••••1234`, `Tgl ajuan`, `Dokumen` mis. `4/4`, `Status`: `Diajukan`, `Perlu perbaikan`, `Dalam peninjauan`, `Disetujui`, `Ditolak`, `Aksi`), pagination `< Sebelumnya` / `Berikutnya >`, box catatan.
- **Bidang Formulir:** Input pencarian, filter status, rentang tanggal.
- **Aksi/Tombol:** `Tinjau` (baris aktif), `Lihat` (disetujui/ditolak), pagination.
- **Navigasi:** Menuju `03b Detail Pengajuan`.
- **Keadaan (States):**
  - *Digambar:* Contoh semua 5 status dalam tabel. State **kosong (empty state)** *dijelaskan lewat catatan* (`"Belum ada pendaftaran baru"` + tautan ke Riwayat Keputusan), tetapi tidak digambar terpisah.
  - *TIDAK Digambar:* Memuat (skeleton), error jaringan/API.

### 15. `03b Verifikasi Anggota – Detail Pengajuan` (`77:370`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Verifikator Keanggotaan / Admin
- **Komponen:** Breadcrumb `Keanggotaan / Antrean / PND-...`, kartu `Data pemohon` (`Nama lengkap`, `NIK` masked + aksi `Tampilkan – tercatat di audit`, `Tanggal lahir`, `Alamat`, `Kontak`, `Tanggal pengajuan`), kartu `Dokumen` (`KTP`, `Foto diri`, `Kartu Keluarga`, `Surat pernyataan` beserta badge status dokumen & tombol `Lihat`), kartu `Keputusan` & panel `Riwayat status`.
- **Bidang Formulir:** Textarea `Catatan / alasan` (wajib untuk `Perbaikan` dan `Tolak`).
- **Aksi/Tombol:** `Tampilkan` (unmask NIK ber-audit), `Lihat` per dokumen KYC, `Setujui`, `Minta Perbaikan`, `Tolak`. Catatan: *"Tiap keputusan meminta konfirmasi dan tercatat di riwayat status."*
- **Navigasi:** Sidebar ke `03a`; breadcrumb kembali ke antrean.
- **Keadaan (States):**
  - *Digambar:* Detail terisi pada status `Dalam peninjauan`.
  - *TIDAK Digambar:* Modal dialog konfirmasi, tampilan lightbox gambar dokumen, loading saat dekripsi/submit, error validasi alasan wajib, sukses keputusan.

### 16. `04a Tagihan & Pembayaran – Portal Anggota` (`78:55`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Anggota
- **Komponen:** Topbar portal anggota (`Beranda`, `Katalog`, `Pesanan Saya`, `Tagihan`, `Profil`, `Keranjang (0)`), 3 kartu ringkasan (`Total belum dibayar`, `Jatuh tempo terdekat`, `Menunggak`), tabel `Daftar tagihan iuran` (checkbox multi-pilih, `Tagihan`, `Jatuh tempo`, `Jumlah`, `Status`: `Menunggak`, `Belum dibayar`, `Menunggu verifikasi`, `Bukti ditolak`, `Lunas`), panel kanan `Bayar tagihan terpilih` (`Dipilih: 2 tagihan`, `Total Rp ..`, tab `QRIS`/`Transfer`, placeholder gambar `QRIS koperasi`, zona unggah bukti `JPG / PNG / PDF`, tombol kirim, catatan aturan verifikasi).
- **Bidang Formulir:** Checkbox tagihan, pemilih metode `QRIS`/`Transfer`, input file bukti pembayaran.
- **Aksi/Tombol:** Pilih tagihan, `QRIS` / `Transfer`, pilih berkas, `Kirim bukti pembayaran`.
- **Navigasi:** Topbar portal anggota.
- **Keadaan (States):**
  - *Digambar:* 5 varian status pada tabel + panel bayar terpilih + catatan agar status menjelaskan tindak lanjut (mis. *"Bukti ditolak – unggah ulang"*).
  - *TIDAK Digambar:* Nominal kode unik terpisah, timer kedaluwarsa `PAYMENT_EXPIRY_DURATION`, loading upload, error ukuran/tipe file, empty state tanpa tagihan, state sukses menunggu verifikasi.

### 17. `04b Verifikasi Pembayaran – Bendahara` (`78:179`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Bendahara / Admin
- **Komponen:** Sidebar (`IURAN: Jenis Iuran, Tagihan, Keringanan`, `PEMBAYARAN: Verifikasi Pembayaran, Alokasi`, `KAS: Kas & Mutasi, Periode Kas, Rekonsiliasi, Laporan Keuangan`), kartu `Bukti pembayaran` (pratinjau gambar/PDF), `Detail pembayaran` (`Pembayar`, `Metode`, `Jumlah dibayar`, `Waktu unggah`), tabel `Alokasi ke kewajiban` (`Tagihan / pesanan`, `Sisa`, `Dialokasikan`), panel `Keputusan` & `Jejak verifikasi`.
- **Bidang Formulir:** Input `Referensi transaksi` (no. referensi mutasi/QRIS), textarea `Alasan penolakan` (wajib bila ditolak).
- **Aksi/Tombol:** `Verifikasi & alokasikan`, `Tolak bukti`. Catatan: *"Bukti QRIS belum tentu settlement final. Catat pemeriksa, waktu, referensi; lakukan rekonsiliasi. Aksi finansial meminta konfirmasi."*
- **Navigasi:** Sidebar keuangan.
- **Keadaan (States):**
  - *Digambar:* Status `Menunggu verifikasi`.
  - *TIDAK Digambar:* Mode break-glass kendali ganda admin, daftar antrean pembayaran sebelum memilih `PAY-0045`, loading, modal konfirmasi, error validasi alasan penolakan, sukses terverifikasi/ditolak.

### 18. `05 Katalog Produk` (`78:310`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Publik / Anggota
- **Komponen:** Header publik, filter sidebar kiri (`Kategori`: Sembako, Kebutuhan rumah tangga, Produk lokal, Pertanian, Lainnya; `Ketersediaan`: Tersedia, Stok terbatas; `Rentang harga`: Slider harga), search `Cari produk...`, dropdown `Urutkan: Terpopuler`, grid 8 kartu produk (`Foto produk`, nama, harga `Umum` vs `Anggota`, badge `Tersedia`/`Stok terbatas`/`Stok habis`), pagination `< 1 2 3 >`, box catatan (`Putuskan apakah checkout tamu diizinkan atau hanya anggota login. Harga anggota dibekukan saat pesanan dibuat. Empty state hasil pencarian...`).
- **Bidang Formulir:** Pencarian `Cari produk...`, checkbox kategori & ketersediaan, slider harga, sort.
- **Aksi/Tombol:** `Pesan` (disabled/berlabel `Stok habis` pada produk habis), pagination, `"Reset filter"` *(disebut pada catatan)*.
- **Navigasi:** Header menuju Beranda, Masuk, Daftar Anggota.
- **Keadaan (States):**
  - *Digambar:* Grid normal dengan status `Tersedia`, `Stok terbatas`, `Stok habis`. Empty state pencarian dijelaskan via catatan (`jelaskan penyebab + tombol "Reset filter"`).
  - *TIDAK Digambar:* Memuat, error API, alur drawer/form pilih alamat & metode pengiriman saat klik `Pesan`.

### 19. `06 Detail Pesanan – Portal Anggota` (`78:473`) — *(terinspeksi visual PNG)*
- **Peran Pengguna:** Anggota / Pembeli Terautentikasi
- **Komponen:** Header portal anggota, stepper 6 tahap (`Dibuat` -> `Menunggu pembayaran` -> `Pembayaran diverifikasi` -> `Diproses` -> `Dikirim / diambil` -> `Selesai`), tabel `Item pesanan` (`Produk`, `Varian`, `Qty`, `Harga anggota*`, `Subtotal`), kartu `Pengambilan / pengiriman` (`Metode`, `Gudang / gerai`, `Alamat / lokasi ambil`, `Status`), kartu `Ringkasan pembayaran` (`Subtotal`, `Promo`, `Total`), catatan bawah (`Siapkan state: pesanan kedaluwarsa, stok habis, retur/refund, pembayaran dialokasikan ke lebih dari satu kewajiban`).
- **Bidang Formulir:** Tidak ada form langsung; ajukan retur/refund disebut tersedia setelah `Selesai`.
- **Aksi/Tombol:** `Bayar sekarang`, `Batalkan pesanan`.
- **Navigasi:** Breadcrumb `Pesanan Saya / ORD-0001`.
- **Keadaan (States):**
  - *Digambar:* Status `Menunggu pembayaran` dengan countdown `[hh:mm:ss]`.
  - *TIDAK Digambar:* Detail visual status `Kedaluwarsa`, `Dibatalkan`, `Diproses`, `Diantar/Siap Diambil`, `Selesai`, dan alur input pengajuan retur/refund *(catatan menekankan agar state-state ini disiapkan di implementasi UI)*.
