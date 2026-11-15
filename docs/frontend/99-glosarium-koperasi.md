# 99. Glosarium Koperasi Desa Merah Putih & Catatan Keputusan

Referensi lengkap istilah perkoperasian, akuntansi, pembayaran digital Indonesia, serta penandaan tegas atas item yang memerlukan pengesahan akuntan maupun ahli hukum.

---

## 1. Istilah Koperasi & Operasional Desa

- **Kopdes Merah Putih**: Model koperasi desa multi-layanan (keanggotaan, iuran, toko ritel kebutuhan desa, dan pembayaran digital).
- **Multi-Tenant (`koperasi_id`)**: Satu platform perangkat lunak melayani banyak koperasi desa sekaligus; setiap data koperasi diisolasi ketat lewat PostgreSQL *Row-Level Security* (`FORCE ROW LEVEL SECURITY`).
- **Anggota Aktif vs Non-Anggota**: Anggota resmi memperoleh `harga_anggota` di toko koperasi dan tercatat pada rekapitulasi iuran rutin.
- **Badan Pengawas (`pengawas`)**: Organ koperasi yang bertugas mengaudit pembukuan dan operasional; di sistem API hanya memiliki hak baca (*read-only*).

## 2. Istilah Keuangan, QRIS & Pembukuan

- **QRIS Statis + Kode Unik (`QRIS_DYNAMIC_ENABLED=false`)**: Kode QR merchant tetap yang nominalnya diketik manual oleh pembayar sebesar `nominal_total` (= `nominal_dasar` + `kode_unik` 3–4 digit) agar mutasi bank mudah dicocokkan otomatis.
- **EMVCo CRC16 (Tag 63)**: Standar internasional kode QR pembayaran di Indonesia (BI SNAP / ASPI); backend memvalidasi checksum sebelum mengizinkan pendaftaran rekening QRIS.
- **Rekening Badan Hukum Koperasi (`badan_hukum_koperasi`)**: Rekening giro/tabungan resmi atas nama badan hukum koperasi.
- **Mode Pilot Rekening Pribadi (`ALLOW_PERSONAL_PAYMENT_PILOT`)**: Pengecualian sementara yang dibatasi per hari (`Asia/Jakarta`) & per transaksi dengan peringatan merah wajib sebelum rekening badan hukum koperasi terbit.
- **Append-Only Ledger (`kas_mutasi`, `stok_mutasi`, `audit_log`)**: Tabel pembukuan abadi yang dilindungi trigger database agar tidak bisa diubah/dihapus; perbaikan hanya melalui jurnal pembalik (*reversal*).
- **Rekonsiliasi Harian**: Pencocokan antara transaksi `disetujui` sistem koperasi dengan baris kredit (`CR`) rekening koran bank per tanggal operasional `Asia/Jakarta`.

## 3. Penandaan Keputusan Akuntan (`KEPUTUSAN AKUNTAN`)

- **Klasifikasi Jenis Iuran (`iuran_handler.go`)**: Backend sengaja menggunakan label netral **`KEPUTUSAN AKUNTAN`** untuk klasifikasi pos iuran (misal antara simpanan pokok/wajib sebagai ekuitas anggota vs titipan/kewajiban berdasarkan PSAK Perkoperasian dan AD/ART masing-masing koperasi). Pengembang frontend **dilarang** mengubah label jenis iuran menjadi kata tetap seperti "Tabungan Bank" atau pos akuntansi sepihak tanpa persetujuan akuntan koperasi.

## 4. Penandaan Verifikasi Hukum (`PERLU VERIFIKASI HUKUM`)

- **Perlindungan Data Pribadi & Retensi KYC (UU No. 27 Tahun 2022 PDP)**:
  1. Kebijakan penghapusan fisik baris pendaftaran tertolak + berkas KTP setelah **30 hari** (`/api/v1/pendaftaran/purge-ditolak`) beserta pengecualian retensi arsip keuangan/audit ditandai **PERLU VERIFIKASI HUKUM** oleh konsultan hukum koperasi sebelum diterapkan penuh di produksi.
  2. Teks persetujuan (*informed consent*) saat calon anggota mengunggah KTP/KK dan pemberitahuan penggunaan data di UI juga wajib disetujui kuasa hukum/pengelola koperasi (**PERLU VERIFIKASI HUKUM**).
