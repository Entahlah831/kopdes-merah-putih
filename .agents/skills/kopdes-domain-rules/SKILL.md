---
name: kopdes-domain-rules
description: Gunakan saat merancang skema database, validasi bisnis, perhitungan uang/diskon/iuran, mesin status, peran pengguna, atau dokumentasi domain Kopdes Merah Putih agar tetap berada dalam lingkup koperasi tunggal.
---

# Aturan Domain Kopdes Merah Putih (`kopdes-domain-rules`)

## 1. Lingkup Klien & Batas Wajib
- Sistem melayani **satu koperasi di satu lokasi** (~100–1.000 pengunjung), mencakup:
  1. Manajemen anggota, pendaftaran publik, dan KYC terenkripsi.
  2. Penagihan iuran bulanan sederhana (QRIS statis dan tunai + kwitansi).
  3. E-katalog produk (harga umum vs harga anggota aktif, promo sederhana), checkout langsung tanpa keranjang server, dan daftar pengantaran + label alamat untuk armada milik klien.
  4. Pengaturan koperasi satu baris (`pengaturan`) dan laporan operasional + ekspor CSV aman.
- **Daftar Fitur yang DILARANG Dibuat**:
  - Multi-koperasi / multi-tenant (`koperasi_id`, RLS tenant, `WithTenantTx`).
  - Sinkronisasi SIMKOPDES, simpanan sukarela/berjangka, pinjaman/angsuran, perhitungan SHU.
  - Akuntansi *double-entry* (neraca/jurnal berpasangan), buku kas `kas_mutasi`, impor CSV mutasi bank, generator QRIS dinamis EMVCo.
  - Partisi tabel, hash-chain SHA-256 + advisory lock, outbox/gateway WhatsApp berbayar, server Prometheus `/metrics`, keranjang belanja di server, endpoint HTTP purge.

## 2. Peran Pengguna (4 Peran Resmi)
1. `admin`: pengelolaan sistem penuh, pengaturan koperasi, manajemen user/staf, verifikasi pendaftaran & KYC, iuran, katalog, pesanan, pembayaran, laporan.
2. `bendahara`: pencatatan pembayaran tunai & kwitansi, verifikasi pembayaran QRIS, pembuatan jenis iuran & keringanan, laporan keuangan/tunggakan.
3. `petugas_toko`: pengelolaan katalog/stok/promo, pemrosesan pesanan, status antar/siap diambil, cetak daftar antar dan label alamat.
4. `anggota`: melihat profil/tagihan sendiri, checkout katalog dengan harga anggota (jika status `aktif`), membuat pembayaran & unggah bukti milik sendiri, membatalkan pesanan milik sendiri yang belum dibayar.
- Otorisasi di server wajib identik dengan `docs/roles_matrix.json`.

## 3. Mesin Status Inti
- **`pendaftaran`**: `diajukan` -> `perlu_perbaikan` | `disetujui` | `ditolak` (`perlu_perbaikan` -> `diajukan` -> `disetujui`/`ditolak`).
- **`tagihan_iuran`**: `belum_bayar` -> `menunggu_verifikasi` -> `lunas`; atau `belum_bayar` -> `dibebaskan` | `batal`.
- **`pesanan`**: `baru` -> `menunggu_pembayaran` -> `menunggu_verifikasi` -> `dibayar` -> `diproses` -> (`diantar` | `siap_diambil`) -> `selesai`; atau `baru`/`menunggu_pembayaran` -> `batal` (wajib mengembalikan stok & kuota promo).
- **`pembayaran`**: `menunggu_pembayaran` -> `menunggu_verifikasi` -> (`lunas` | `perlu_tindak_lanjut` | `ditolak`); atau `menunggu_pembayaran` -> (`kedaluwarsa` | `batal`). Kelebihan/kekurangan bayar dicatat `perlu_tindak_lanjut`, **jangan** dilunasi otomatis.

## 4. Aturan Uang, QRIS Statis, & Kode Unik
- Gunakan **bilangan bulat Rupiah** (`BIGINT` di PostgreSQL, `int64` di Go). **Dilarang menggunakan `float32`/`float64`**.
- **Kode unik QRIS**: bilangan bulat Rupiah `1..(10^N - 1)` (`QRIS_UNIQUE_DIGITS` = 3 atau 4) **ditambahkan** ke nominal pokok (BUKAN sen/desimal), dijamin unik di antara pembayaran pending lewat partial unique index `(rekening_qris_id, nominal_total)`.
- Rekening QRIS `perorangan` ditolak di produksi kecuali `ALLOW_PERSONAL_PAYMENT_PILOT=true` (dengan batas per transaksi/hari & penanda peringatan).
- Selisih kode unik dan klasifikasi iuran ditandai `"KEPUTUSAN AKUNTAN"`.
- Produk tanpa harga aktif (`NULL` / `<= 0`) tidak boleh tampil sebagai Rp0 dan wajib ditolak `409 Conflict` saat checkout.

## 5. Aturan Data Pribadi
- Kolom sensitif (`nik`, `hp`, `tanggal_lahir`, `alamat`, email/HP user) disimpan terenkripsi **AES-256-GCM** dengan AAD.
- **Blind index hanya untuk NIK** (`nik_hash`, dilengkapi versi kunci agar rotasi memungkinkan) serta `email_hash`/`hp_hash` untuk lookup login; jangan membuat blind index tanggal lahir.
- Nomor NIK & HP disamarkan (*masked*) pada respons standar; akses penuh membutuhkan peran berwenang dan tercatat di `audit_log`.
- Berkas KYC di disk wajib terenkripsi **AES-256-GCM** sebelum ditulis.
- Retensi penghapusan pendaftaran ditolak (`PDP_REJECTED_RETENTION_DAYS`) dan klaim kepatuhan regulasi wajib ditandai **`PERLU VERIFIKASI HUKUM`**.
