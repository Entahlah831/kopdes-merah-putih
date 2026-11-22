# Lembar Pelatihan Singkat Pengurus (90 Menit — Lingkungan Staging)

Gunakan lembar ini saat melatih Ketua/Admin, Bendahara, dan Petugas Toko sebelum mengaktifkan operasional nyata. **Pastikan latihan dilakukan di VPS Staging dengan data uji sintetis (BUKAN data warga/uang asli).**

---

## 1. Kartu Saku Izin Peran (Tempel di Meja Kasir / Kantor Desa)

| Aktivitas Harian | Admin (Ketua) | Bendahara | Petugas Toko |
| :--- | :---: | :---: | :---: |
| Verifikasi pendaftaran warga & foto KYC | **Ya** | Tidak | Tidak |
| Tambah akun staf & reset MFA/password anggota | **Ya** | Tidak | Tidak |
| Buat jenis iuran, keringanan, & verifikasi QRIS/Tunai | **Ya** | **Ya** | Tidak |
| Catat pembayaran tunai di kasir/sistem | **Ya** | **Ya** | **Tidak** |
| Pengembalian dana (Refund) — *harus beda orang antara pengaju & penyetuju* | **Ya** | **Ya** | Tidak |
| Tambah/ubah produk katalog & promo | **Ya** | Tidak | **Ya** |
| Ubah status pesanan (`diproses`/`siap_diambil`/`diantar`/`selesai`) & daftar antar | **Ya** | Tidak | **Ya** |
| Lihat & unduh laporan operasional | **Ya** | **Ya** | **Ya** |
| Wajib pakai MFA 6-digit saat masuk | **Wajib** | **Wajib** | Opsional (`MFA_REQUIRED_ROLES`) |

---

## 2. Jadwal Praktik Langsung (6 Latihan — 90 Menit)

| Waktu | Latihan Praktik di Staging | Langkah yang Dikerjakan Peserta | Tanda Lulus |
| :---: | :--- | :--- | :--- |
| **00–15 mnt** | **Login & Aktivasi MFA** | Admin & Bendahara login pertama -> pindai secret ke authenticator -> verifikasi 6 digit -> **tulis minimal 2 kode pemulihan di kertas uji**. | Masuk ke `02 — Dashboard Pengurus`. |
| **15–30 mnt** | **Terima Warga Baru (KYC)** | 1 orang daftar di halaman **Daftar Anggota** + unggah 3 gambar JPG/PNG. Admin buka **Verif Anggota** -> **Tinjau** -> uji **Minta Perbaikan**, lalu **Setujui** -> aktifkan sandi memakai **Kode Aktivasi**. | Anggota baru berhasil masuk portal. |
| **30–50 mnt** | **Belanja, QRIS, & Verifikasi Ganda** | Anggota pesan 1 produk -> buat QRIS (amati kode unik) -> unggah foto struk JPG/PNG. Bendahara buka **Verif Bayar** -> **Periksa** bukti -> cocokkan nominal -> **Verifikasi & Alokasikan**. | Status berubah `lunas`, stok berkurang. |
| **50–65 mnt** | **Pemenuhan Toko & Daftar Antar** | Petugas Toko buka **Pesanan Saya** -> ubah status `dibayar` -> `diproses` -> `diantar` -> `selesai`; unduh daftar antar CSV dan cek nama/alamat terbaca bersih. *(Coba juga: Bendahara mencoba mengubah status pesanan -> pastikan sistem menolak `403`).* | Status `selesai` + daftar antar terunduh. |
| **65–80 mnt** | **Salah Transfer / Tolak Bukti / Refund** | Uji 1 pembayaran dengan **Tolak Bukti** (isi alasan). Uji 1 pengajuan **Refund Manual** oleh Admin -> disetujui oleh Bendahara (orang berbeda) dengan unggah foto bukti serah terima. | Laporan pembayaran mencatat `total_dikembalikan`. |
| **80–90 mnt** | **Simulasi Lupa Sandi & HP Hilang** | Admin menerbitkan `reset_code` (`issue_reset_code: true`) untuk Petugas Toko dan `reset_mfa: true` untuk Bendahara -> staf terkait login ulang & daftarkan MFA baru. *(Operator demo CLI `kopdes admin reset-mfa` untuk Admin).* | Staf kembali masuk dengan aman. |

---

## 3. Daftar Periksa Kebiasaan Aman Harian

- [ ] **Selalu periksa mutasi masuk asli di HP/aplikasi merchant koperasi** sebelum klik **Verifikasi & Alokasikan** pada pembayaran QRIS.
- [ ] **Jangan meminjamkan akun atau ponsel MFA ke staf lain** (sistem mencatat siapa pengaju dan siapa pemverifikasi di `audit_log`).
- [ ] **Jangan menerima bukti transfer dalam bentuk PDF atau mengirim file lewat komputer umum tanpa Logout** — sistem hanya menerima gambar JPG/PNG demi mencegah file berbahaya.
- [ ] **Catat dan simpan kode pemulihan MFA di tempat terkunci** (bukan di grup WhatsApp).
- [ ] Jika melihat selisih nominal (`perlu_tindak_lanjut`) atau stok tiba-tiba habis saat bayar diverifikasi, **segera koordinasikan antara Bendahara dan Petugas Toko sebelum menyerahkan barang**.

## 4. Lembar Tanda Tangan Pelatihan Staging

| Nama Peserta | Peran | Semua 6 Latihan Selesai? | Tanggal | Tanda Tangan |
| :--- | :--- | :---: | :---: | :---: |
| | Admin / Ketua | [ ] Ya | | |
| | Bendahara | [ ] Ya | | |
| | Petugas Toko | [ ] Ya | | |
