# Panduan & Checklist Uji Lapangan QRIS (Statis & Dinamis)
**Koperasi Desa Merah Putih**
Dokumen Kepatuhan PADG Bank Indonesia No. 21/18/PADG/2019 & Standar ASPI QRIS MPM.

> [!WARNING]
> **PERINGATAN KEBIJAKAN FITUR PRODUKSI**:
> Fitur QRIS Dinamis (`QRIS_DYNAMIC_ENABLED=true`) **DILARANG DIAKTIFKAN** pada lingkungan produksi sebelum seluruh checklist pada dokumen ini diuji dan ditandatangani secara manual oleh Penanggung Jawab Koperasi bersama Bank Mitra (PJP).

---

## 1. Matriks Aplikasi Perbankan & Dompet Digital untuk Pengujian

Pengujian wajib dilakukan minimal pada aplikasi-aplikasi berikut menggunakan perangkat fisik (Android & iOS):

| No | Penyelenggara Jasa Pembayaran (PJP) | Aplikasi Pengujian | Jenis Akun | Status Pengujian |
|:--:|:-----------------------------------|:-------------------|:----------:|:----------------:|
| 1  | PT Bank Mandiri (Persero) Tbk       | Livin' by Mandiri  | Rekening Nasabah | [ ] Belum / [ ] Lulus |
| 2  | PT Bank Rakyat Indonesia (Persero) | BRImo              | Rekening Nasabah | [ ] Belum / [ ] Lulus |
| 3  | PT Bank Central Asia Tbk           | myBCA / BCA mobile | Rekening Nasabah | [ ] Belum / [ ] Lulus |
| 4  | PT Bank Negara Indonesia (Persero) | Wondr by BNI       | Rekening Nasabah | [ ] Belum / [ ] Lulus |
| 5  | PT GoTo Gojek Tokopedia Tbk        | GoPay              | Akun Terverifikasi (Plus) | [ ] Belum / [ ] Lulus |
| 6  | PT Espay Debit Indonesia Koe       | DANA               | Akun Premium     | [ ] Belum / [ ] Lulus |
| 7  | PT Visionet Internasional          | OVO                | Akun OVO Premier | [ ] Belum / [ ] Lulus |
| 8  | PT AirPay International Indonesia  | ShopeePay          | Akun Terverifikasi | [ ] Belum / [ ] Lulus |

---

## 2. Checklist Pengujian Fungsional

### A. Pengujian QRIS Dinamis (Auto-Lock Nominal) — `QRIS_DYNAMIC_ENABLED=true`
*Tujuan: Memastikan nasabah/anggota tidak dapat mengubah nominal transaksi secara manual.*

- [ ] **A1. Pengenalan Kode QR**: Kamera aplikasi bank/e-wallet dapat memindai payload secara instan tanpa error checksum CRC16 (Tag 63).
- [ ] **A2. Tampilan Merchant & NMID**: Nama merchant koperasi (Tag 59), kota (Tag 60), dan NMID (Tag 26 sub-tag 02) tampil sesuai data legalitas koperasi.
- [ ] **A3. Kunci Nominal Otomatis (Tag 54)**:
  - Nominal total (pokok + kode unik) langsung terisi otomatis pada layar konfirmasi pembayaran nasabah.
  - Bidang nominal terkunci rapat (read-only/disabled); nasabah **tidak dapat menyunting nominal**.
- [ ] **A4. Nomor Referensi/Tagihan (Tag 62 Sub-tag 01)**: Nomor pembayaran (contoh: `INV/202611/0001`) tampil di layar konfirmasi atau bukti mutasi bank nasabah.
- [ ] **A5. Ketiadaan Konflik Biaya/Tip (Tag 55–57)**: Tidak muncul dialog meminta tip tambahan atau biaya kemudahan yang merusak nominal transfer.

### B. Pengujian QRIS Statis (Manual Input + Kode Unik) — `QRIS_DYNAMIC_ENABLED=false` (Mode Default)
*Tujuan: Menjamin keandalan alur transaksi manual 3-digit kode unik jika QRIS Dinamis belum berizin resmi.*

- [ ] **B1. Tampilan Nominal Kosong**: Saat dipindai, aplikasi bank menampilkan kolom nominal yang dapat diisi nasabah.
- [ ] **B2. Validasi Instruksi Frontend**: Aplikasi frontend menampilkan nominal dasar + 3 digit kode unik secara jelas disertai tombol "Salin Nominal Tepat".
- [ ] **B3. Toleransi Waktu Pembayaran**: Alokasi kode unik tetap terkunci dan tidak bentrok dengan anggota lain selama masa aktif pembayaran (1 jam).

### C. Pengujian Rekening Merchant (Badan Hukum vs Pilot Perorangan)
*Tujuan: Memastikan kepatuhan UU Perkoperasian & PADG Bank Indonesia.*

- [ ] **C1. Verifikasi Badan Hukum Koperasi**: Rekening penampung settlement PJP terbukti atas nama resmi Koperasi Konsumen/Simpan Pinjam Desa Merah Putih (bukan nama pribadi).
- [ ] **C2. Pembatasan Pilot Perorangan**: Jika `ALLOW_PERSONAL_PAYMENT_PILOT=false`, sistem menolak pendaftaran QRIS atas nama pengurus perorangan. Jika `true` (uji coba percontohan), batas nominal harian wajib dipatuhi dan tercatat di audit log.

---

## 3. Lembar Berita Acara Hasil Uji Lapangan

| Tanggal Pengujian | Nama Penguji & Jabatan | Aplikasi & Versi OS | Catatan Hasil & RRN Uji Coba | Rekomendasi (Lanjut/Perbaiki) |
|:-----------------|:----------------------|:-------------------|:-----------------------------|:------------------------------|
|                  |                       |                    |                              |                               |
|                  |                       |                    |                              |                               |
|                  |                       |                    |                              |                               |

### Tanda Tangan Persetujuan Produksi

| Ketua Koperasi | Pengawas Koperasi | Petugas Teknis / Auditor |
|:--------------:|:-----------------:|:------------------------:|
|                |                   |                          |
| (....................) | (....................) | (....................) |
| Tanggal:       | Tanggal:          | Tanggal:                 |
