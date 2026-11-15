# 18. Glosarium Istilah UI/UX & Pemetaan Status Ramah Warga

Daftar terjemahan istilah teknis API menjadi label antarmuka dan warna/ikon yang konsisten serta mudah dipahami anggota koperasi desa.

---

## 1. Status Pendaftaran (`pendaftaran_anggota.status`)

| Kode API | Label UI Ramah Warga | Tindakan yang Tampil di Layar |
|---|---|---|
| `diajukan` | **Menunggu Pemeriksaan Pengurus** | Tampilkan informasi perkiraan waktu verifikasi & `nik_masked`. |
| `perbaikan` | **Perlu Perbaikan Data / Foto** | Tampilkan `catatan` pengurus + form unggah ulang foto KYC. |
| `disetujui` | **Diterima — Anggota Aktif** | Tampilkan nomor anggota & ajakan melihat katalog/iuran. |
| `ditolak` | **Belum Dapat Disetujui** | Tampilkan alasan penolakan jelas *(data fisik dihapus setelah retensi — **PERLU VERIFIKASI HUKUM**)*. |

## 2. Status Anggota (`anggota.status`)

| Kode API | Label UI | Hak Akses Harga Toko |
|---|---|---|
| `aktif` | **Anggota Aktif** | Berhak `harga_anggota` & promo khusus anggota. |
| `nonaktif` | **Nonaktif Sementara** | Tidak dapat mengajukan iuran baru hingga diaktifkan kembali. |
| `keluar` | **Sudah Keluar** | Hanya rekam historis audit. |

## 3. Status Tagihan Iuran (`tagihan_iuran.status`)

| Kode API | Label UI | Keterangan |
|---|---|---|
| `belum_lunas` | **Belum Dibayar** | Bisa dicentang untuk dibuatkan QRIS pembayaran. |
| `dibayar_sebagian` | **Dibayar Sebagian** | Tampilkan selisih `nominal_tagihan - nominal_terbayar`. |
| `lunas` | **Lunas** | Tidak dapat dipilih lagi; tampilkan tanggal lunas. |

## 4. Status Pesanan Toko (`pesanan.status`)

| Kode API | Label UI | Keterangan |
|---|---|---|
| `draft` | **Menunggu Lanjut Bayar** | Status awal segera sesudah `POST /toko/checkout`. Arahkan ke buat QRIS. |
| `menunggu_pembayaran` | **Menunggu Pembayaran** | Tampilkan QRIS & hitung mundur + tombol Batalkan Pesanan. |
| `diproses` | **Sedang Disiapkan** | Pembayaran sudah sah; petugas menyiapkan barang di gudang koperasi. |
| `selesai` | **Selesai / Sudah Diambil** | Pesanan tuntas diterima warga. |
| `batal` | **Dibatalkan Pembeli** | Stok & kuota promo telah dikembalikan otomatis. |
| `kedaluwarsa` | **Waktu Bayar Habis** | Stok & kuota promo dilepas kembali otomatis. |

## 5. Status Pembayaran (`pembayaran.status`)

| Kode API | Label UI Warga | Label UI Backoffice Pengurus |
|---|---|---|
| `menunggu_pembayaran` | **Silakan Transfer Tepat Nominal** | Menunggu transfer dari warga |
| `menunggu_verifikasi` | **Bukti Dikirim — Menunggu Dicek** | Perlu diperiksa 1 verifikator (`Maker-Checker`) |
| `menunggu_persetujuan_kedua` | **Sedang Diproses Pengurus** | **Wajib Persetujuan Orang Ke-2 Berbeda** |
| `disetujui` | **Pembayaran Lunas / Sah** | Tercatat sah di `kas_mutasi` |
| `ditolak` | **Ditolak (Cek Alasan)** | Ditolak — tidak masuk kas |
| `kedaluwarsa` | **Kedaluwarsa** | Melewati `berlaku_sampai` |

## 6. Kamus Singkat Istilah Antarmuka

- **Kode Unik**: 3–4 angka acak tambahan pada nominal transfer (misal +`356`) untuk mencocokkan mutasi bank tanpa menunggu pembayaran QRIS dinamis.
- **Maker-Checker**: Prinsip bahwa petugas pembuat/pengimpor tidak boleh mengesahkan sendiri transaksinya demi mencegah kecurangan.
- **Single-Flight Refresh**: Teknik memanggil endpoint pembaru token **sekali saja** meski 5 komponen gagal `401` pada milidetik yang bersamaan.
- **Idempotency-Key**: Nomor unik (`UUIDv4`) per niat transaksi bayar agar ketukan ganda saat sinyal lambat tidak membuat dua tagihan.
