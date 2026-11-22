# Panduan Operasional Pengurus — Kopdes Merah Putih

Panduan ini ditulis untuk **pengurus dan staf koperasi** (Admin/Ketua, Bendahara, dan Petugas Toko) menggunakan bahasa sehari-hari yang mudah diikuti.

> **Ringkasnya, siapa memegang tugas apa?**
> - **Admin (Ketua/Sekretaris):** Verifikasi calon anggota & KYC, kelola akun staf/anggota, pengaturan koperasi & rekening QRIS, serta semua tugas operasional bila diperlukan (**wajib MFA 6 digit**).
> - **Bendahara:** Buat/kelola tagihan iuran bulanan, **catat bayar tunai**, **verifikasi QRIS**, **refund manual**, pengaturan rekening QRIS, dan lihat laporan (**wajib MFA 6 digit**).
> - **Petugas Toko:** Kelola produk & promo katalog, **proses pesanan** (`diproses` -> `siap_diambil`/`diantar` -> `selesai`), **daftar antar**, dan lihat laporan (**tidak bisa** mencatat bayar tunai atau memverifikasi QRIS).

---

## Cara Masuk Pertama Kali & Keamanan Dasar

1. Buka alamat web portal koperasi (HTTPS), lalu tekan tombol **Masuk** di kanan atas.
2. Masukkan **Email** atau **Nomor HP** dan **Kata Sandi** Anda (`>= 10` karakter, maksimal `72` byte).
3. Jika akun Anda berperan **Admin** atau **Bendahara**:
   - **Login pertama (`mfa_setup`):** Layar menampilkan **Secret TOTP**. Pindai/masukkan ke aplikasi authenticator di ponsel (Google Authenticator / Aegis / 2FAS), lalu masukkan **6 digit kode OTP** dan tekan **Verifikasi MFA TOTP**. Catat baik-baik **8 kode pemulihan (`recovery_codes`)** di tempat aman terpisah dari ponsel!
   - **Login rutin (`mfa_pending`):** Buka aplikasi authenticator, masukkan 6 digit angka yang tampil saat itu.
4. Sesi otomatis berakhir jika tidak aktif (`30 menit` default) atau setelah masa maksimum (`12 jam` default). Selalu tekan tombol **Keluar** jika memakai komputer bersama.

---

## Tugas 1: Menyetujui / Menolak Pendaftaran & KYC Calon Anggota

**Siapa:** Admin (`Verif Anggota` / layar `03a`–`03b`)

1. Dari menu atas klik **Verif Anggota** (atau dari Dashboard Pengurus klik **03a Verifikasi Anggota**).
2. Gunakan kotak pencarian **Cari nama pemohon...** atau pilih filter status **`Diajukan`** (`Perlu perbaikan`, `Disetujui`, `Ditolak`).
3. Periksa kolom **Dokumen** (idealnya **`3/3`**: KTP, Selfie KTP, dan KK), lalu klik **Tinjau** pada baris calon anggota.
4. Layar detail (`03b`) otomatis mendekripsi dan menampilkan tiga gambar KYC (`ktp`, `selfie_ktp`, `kk`). Cocokkan nama, NIK tersamar (`nik_masked`), dan kejelasan foto wajah/KTP.
5. Pilih salah satu dari 3 keputusan:
   - **Setujui** *(foto jelas & data warga desa cocok)*: Klik **Setujui**. Sistem langsung mencetak **Kode Aktivasi** satu kali pakai di kotak hijau (berlaku 24 jam). Salin kode tersebut dan kirimkan secara aman langsung kepada calon anggota agar mereka bisa membuat kata sandi di halaman **Daftar Anggota -> Aktivasi Kata Sandi Anggota**.
   - **Minta Perbaikan** *(foto buram/terpotong atau 1 dokumen belum cocok)*: Wajib isi alasan jelas di **Catatan / Alasan** (misal: *"Foto selfie KTP buram, mohon unggah ulang yang terang"*), lalu klik **Minta Perbaikan**.
   - **Tolak** *(bukan warga desa / dokumen palsu / tidak memenuhi syarat)*: Wajib isi **Catatan / Alasan**, lalu klik **Tolak**. Berkas pendaftar ditolak akan dibersihkan otomatis setelah masa retensi `PDP_REJECTED_RETENTION_DAYS` (`30` hari default).

---

## Tugas 2: Mencatat Pembayaran Tunai di Gerai / Kasir

**Siapa:** **Admin** atau **Bendahara** *(Petugas Toko dilarang mencatat pembayaran tunai agar pengawasan kas tetap terjaga)*.

1. Pastikan pesanan atau tagihan iuran anggota sudah dibuat:
   - Untuk belanja di gerai dari layar **Katalog** (`05`): masukkan barang (`Pesan`), pilih **Metode Bayar -> `Tunai (kasir)`**, isi penerima, lalu klik **Buat Pesanan**.
   - Untuk pembayaran iuran bulanan atau pesanan yang sudah ada: catat lewat endpoint pencatatan pembayaran (`POST /api/v1/pembayaran` melalui portal kasir/admin) dengan `metode: "tunai"`, `jenis_transaksi: "iuran"` atau `"pesanan"`, serta ID tagihan (`tagihan_ids`) atau pesanan (`pesanan_id`).
2. Sistem otomatis menerbitkan **Nomor Kwitansi (`KWT-YYYY-NNNNNN`)** dengan status awal **`menunggu_verifikasi`**.
3. **Penting (Kendali Ganda):** Uang belum otomatis menjadi `lunas` sebelum diperiksa oleh **staf kedua** (contoh: Bendahara mencatat terima tunai -> Admin menekan **Verifikasi & Alokasikan** pada layar `04b`, atau sebaliknya).
4. Jika koperasi terpaksa hanya dijaga 1 staf saat darurat dan pemilik mengaktifkan `DUAL_CONTROL_BREAKGLASS_ENABLED=true`, Admin ber-MFA dapat mencentang **Break-glass Darurat Admin**, mengisi alasan minimal `10` karakter serta kode **MFA TOTP terbaru**, lalu memverifikasi.

---

## Tugas 3: Memverifikasi Pembayaran QRIS Statis

**Siapa:** **Bendahara** atau **Admin** (`Verif Bayar` / layar `04b`)

> **Aturan emas:** Cek aplikasi/notifikasi rekening QRIS koperasi yang asli, jangan hanya percaya gambar bukti transfer!

1. Buka menu **Verif Bayar** (`04b`).
2. Klik **Periksa** pada transaksi yang berstatus **`menunggu_verifikasi`**.
3. Cocokkan tiga hal ini:
   - **Gambar Bukti Pembayaran** yang tampil di layar (hasil dekripsi dari storage terenkripsi terpisah).
   - **Total Harus Dibayar** (`Pokok + Kode Unik`, contoh: `Rp 70.342`, dengan 3 atau 4 digit kode unik).
   - **Mutasi masuk asli di aplikasi merchant/bank koperasi.**
4. Isi kolom **Nominal Dibayar Aktual** sesuai jumlah rupiah yang benar-benar masuk:
   - **Jika uang masuk sesuai `Nominal Total`:** Klik **Verifikasi & Alokasikan**. Status pembayaran berubah menjadi **`lunas`**, stok produk yang ditahan langsung dipotong permanen, dan status pesanan naik menjadi **`dibayar`** (atau tagihan iuran menjadi **`lunas`**).
   - **Jika uang masuk kurang/berbeda nominalnya:** Sistem menandai status **`perlu_tindak_lanjut`** (selisih dicatat agar diselesaikan manual sebelum dilunaskan ulang).
   - **Jika bukti palsu, tidak terbaca, atau dana tidak pernah masuk:** Wajib tulis keterangan jelas pada **Alasan / Catatan**, lalu klik **Tolak Bukti** (status menjadi **`ditolak`**).

---

## Tugas 4: Memproses Pesanan hingga Selesai

**Siapa:** **Petugas Toko** atau **Admin** (`Pesanan Saya` / layar `06`) *(Bendahara tidak diperbolehkan mengubah status pemenuhan barang)*.

1. Buka menu **Pesanan Saya** (`06`).
2. Klik **Detail** pada pesanan yang berstatus **`dibayar`**. *(Pesanan yang belum lunas jangan diserahkan ke pembeli kecuali tunai sudah diverifikasi).*
3. Siapkan barang, lalu ubah status bertahap memakai tombol status di bagian bawah detail:
   - Klik **`Set diproses`** saat barang mulai dikemas/disiapkan.
   - Jika pembeli mengambil sendiri di gerai (`ambil_sendiri`): klik **`Set siap_diambil`** dan beritahu pembeli.
   - Jika memakai kurir desa (`diantar`): klik **`Set diantar`** saat kurir berangkat.
   - Setelah barang benar-benar diterima pembeli: klik **`Set selesai`**.
4. **Pembatalan:** Jika pesanan belum dibayar (`baru` / `menunggu_pembayaran`) batal, klik **Batalkan Pesanan** agar stok tertahan & kuota promo langsung kembali tersedia untuk anggota lain. Jika pembayaran sudah terlanjur `lunas` lalu batal/stok kadaluarsa, lanjutkan ke **Tugas 6 (Refund Manual)**.

---

## Tugas 5: Menggunakan Daftar Antar & Cetak Label Pengiriman

**Siapa:** **Petugas Toko** atau **Admin**

1. Buka fitur daftar antar (`GET /api/v1/pesanan/daftar-antar`, dapat difilter per tanggal memakai `?tanggal=YYYY-MM-DD`).
2. Sistem hanya menampilkan pesanan pengiriman **`diantar`** yang perlu diantar/dipantau, lengkap dengan **Nomor Pesanan, Status, Nama Penerima, No. HP, Alamat Pengantaran, Catatan, dan `LabelAntar` ringkas**.
3. Untuk dicetak di spreadsheet atau kertas label kurir, ekspor CSV melalui `?format=csv` (semua sel CSV otomatis diamankan dari injeksi rumus Excel/Spreadsheet).

---

## Tugas 6: Mengembalikan Dana (Refund Manual)

**Siapa:** **Bendahara** dan **Admin** *(wajib 2 orang berbeda: pengaju != pengeksekusi, kecuali Admin memakai `breakglass` darurat)*.

Gunakan langkah ini bila pembayaran sudah terlanjur `lunas` atau `perlu_tindak_lanjut` (misal salah transfer ganda, atau barang habis setelah masa tahan stok `STOCK_HOLD_MINUTES` berakhir):

1. **Langkah 1 — Pengajuan (Staf Pertama):**
   Ajukan permintaan refund melalui API/portal admin (`POST /api/v1/pembayaran/{id}/refund`) dengan payload:
   - `aksi`: `"ajukan"`
   - `nominal`: jumlah rupiah yang dikembalikan (tidak boleh melebihi nominal bayar)
   - `metode`: `"tunai"` atau `"transfer"`
   - `alasan`: penjelasan jelas penyebab refund
   Status pengajuan menjadi **`diajukan`**.
2. **Langkah 2 — Penyerahan Dana Nyata:**
   Serahkan uang tunai langsung dengan tanda terima warga, atau transfer manual melalui rekening koperasi (simpan foto struk/bukti serah-terima dalam format **JPG atau PNG**).
3. **Langkah 3 — Persetujuan/Penolakan (Staf Kedua yang Berbeda):**
   Panggil `POST /api/v1/pembayaran/{id}/refund` (multipart jika melampirkan foto `file` bukti refund):
   - Setujui dengan `aksi: "setujui"`, `metode`, dan `catatan_pelaksana` (atau tolak dengan `aksi: "tolak"` + `alasan`).
   - Saat disetujui, status pembayaran berubah menjadi **`dikembalikan`**, stok pesanan yang sempat dipotong otomatis dipulihkan bila belum dibatalkan, foto bukti refund disimpan terenkripsi di `STORAGE_BUKTI_DIR`, dan angka masuk ke kolom `total_dikembalikan` laporan pembayaran.

---

## Tugas 7: Membaca & Mengunduh Laporan Operasional

**Siapa:** **Admin**, **Bendahara**, atau **Petugas Toko**

1. **Ringkasan Cepat di Browser:** Buka menu **Pengurus** (`02 — Dashboard Pengurus`) untuk melihat 4 kartu langsung: *Anggota Aktif*, *Pendaftaran Menunggu*, *Pembayaran Menunggu Verifikasi*, dan *Rekap Tunggakan*.
2. **Laporan Rinci & Unduhan CSV (`GET /api/v1/laporan`):**
   Tersedia 4 jenis laporan (`?jenis=...`, opsional `?dari=YYYY-MM-DD&sampai=YYYY-MM-DD`, serta `?format=csv`):
   - `anggota`: rekap jumlah anggota menurut status (`aktif`, `nonaktif`, `keluar`, dll.).
   - `tunggakan`: daftar anggota yang masih memiliki tagihan **`belum_bayar`** beserta periode & tanggal jatuh tempo.
   - `penjualan`: omzet penjualan harian (jumlah pesanan, subtotal, diskon, ongkir, total omzet).
   - `pembayaran`: rekap transaksi per jenis (`iuran`/`pesanan`), metode (`qris`/`tunai`), status, total pokok, **selisih kode unik**, total dibayar, dan **total dikembalikan**.

---

## Tugas 8: Reset Password Staf / Anggota

1. **Ganti Sandi Sendiri (masih ingat sandi lama):**
   Kirim permintaan `POST /api/v1/auth/password` saat sudah login dengan `current_password` dan `new_password` (`10`–`72` karakter/byte).
2. **Anggota atau Staf Lupa Password (dibantu Admin lewat Aplikasi):**
   - Admin memanggil pembaruan user (`PATCH /api/v1/users/{id}`) dengan **`issue_reset_code: true`**.
   - Sistem langsung membatalkan kode lama dan menerbitkan **`reset_code`** baru (berlaku **24 jam**) beserta tautan **`wa_link`**.
   - Kirim `reset_code` kepada pemilik akun. Pemilik akun membuka halaman **Daftar Anggota -> Sudah Disetujui? Aktivasi Kata Sandi Anggota**, lalu mengisi **Kode Aktivasi** dengan `reset_code` tersebut dan membuat kata sandi baru (semua sesi lama otomatis dicabut).
3. **Reset Password Darurat via Server VPS (jika tidak ada Admin yang bisa masuk):**
   Pemilik sistem / administrator server login ke VPS via SSH dan menjalankan:
   ```bash
   docker compose exec api /app/kopdes --env-file /app/.env admin reset-password --email email@koperasi.id --password-stdin
   ```
   *(Ketik sandi baru minimal **12 karakter** lalu tekan `Ctrl+D`; semua sesi lama otomatis dicabut dan tercatat di `audit_log` dengan aktor `owner_cli`.)*

---

## Tugas 9: Jika Staf Kehilangan Ponsel MFA (`TOTP`)

1. **Coba Pakai Kode Pemulihan (`recovery_codes`) Dulu:**
   Saat layar meminta 6 digit TOTP, masukkan salah satu dari **8 kode pemulihan** (`a1b2-c3d4`) yang pernah dicatat saat aktivasi MFA pertama kali (kirim pada field `recovery_code` di `POST /api/v1/auth/mfa/verify`). Kode yang sudah dipakai otomatis hangus.
2. **Jika Bendahara / Petugas Lain Kehilangan Ponsel + Kode Pemulihan (dibantu Admin):**
   - Admin membuka pengelolaan user (`PATCH /api/v1/users/{id}`) lalu mengirim **`reset_mfa: true`**.
   - Semua sesi lama pengguna langsung dicabut.
   - Pengguna tersebut login ulang dengan email & kata sandi -> sistem otomatis memintanya mendaftarkan aplikasi authenticator baru (`mfa_setup`) dan memberikan 8 kode pemulihan baru.
3. **Jika Satu-satunya Admin Kehilangan Ponsel + Kode Pemulihan:**
   Pemilik koperasi / operator server masuk ke VPS via SSH lalu menjalankan:
   ```bash
   docker compose exec api /app/kopdes --env-file /app/.env admin reset-mfa --email admin@koperasi.id
   ```
   Setelah perintah sukses (*"semua sesi aktif dicabut"*), Admin tersebut cukup login ulang memakai email & kata sandi untuk langsung diarahkan ke layar pendaftaran MFA baru (`mfa_setup`). **Ingat:** catat kembali 8 kode pemulihan barunya!
