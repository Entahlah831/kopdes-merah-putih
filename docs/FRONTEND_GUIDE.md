# Panduan Integrasi Frontend: Koperasi Desa Merah Putih
**Arsitektur Integrasi API, Panduan UI/UX Mobile-First, dan Rekomendasi Keamanan Data**

---

## 1. Rekomendasi Teknologi Frontend
Untuk aplikasi warga desa dan pengurus koperasi di lapangan:
- **Web App / PWA**: **Next.js 14+ (App Router)** atau **Vite + React (TypeScript)** + **Tailwind CSS**. Ringan dimuat pada koneksi 3G/4G pedesaan.
- **Mobile App Native/Hybrid**: **Flutter** atau **React Native** (satu basis kode untuk Android & iOS).

---

## 2. Alur Autentikasi & Keamanan Sesi

### A. Alur Token (Short-lived Access Token + Rotating Refresh Token)
1. **Login (`POST /api/v1/auth/login`)**:
   - Kirim `email` dan `password`.
   - Menerima `access_token` (berlaku 15 menit) dan `refresh_token` (berlaku 7 hari).
   - Simpan `access_token` di memori (in-memory state/store) dan `refresh_token` di `SecureStore` (mobile) atau `HttpOnly Cookie` (web).
2. **Refresh Otomatis (`POST /api/v1/auth/refresh`)**:
   - Pasang interceptor Axios / fetch middleware.
   - Saat request API mengembalikan `401 Unauthorized`, lakukan refresh token secara otomatis di latar belakang.
   - **PENTING (Anti Session-Hijacking)**: Sistem backend menerapkan *Refresh Token Rotation*. Setiap pemanggilan refresh akan menerbitkan refresh token baru dan membatalkan token lama. Jika terjadi percobaan penggunaan ulang (*token reuse*), seluruh sesi user tersebut akan dibekukan secara otomatis.

---

## 3. Desain Layar Pembayaran QRIS (Mobile-First UI/UX)

Layar pembayaran adalah titik krusial konversi dan integritas rekonsiliasi. Penerapan desain wajib mematuhi panduan berikut:

### A. Tampilan QRIS & Nominal
1. **Gambar QR Code Besar & Jelas**:
   - Render string `qris_payload` dari API menggunakan library QR (`qrcode.react` atau `qr_flutter`) dengan ukuran minimal 260x260 px.
   - Berikan tombol **"Simpan Gambar QR"** agar warga bisa menyimpan ke galeri foto.
2. **Sorot Kode Unik Kriptografis (Highlight 3-4 Digit Terakhir)**:
   - Tampilkan `nominal_total` dengan ukuran font besar (misal 28pt bold).
   - Beri warna kontras (merah bata atau jingga gelap) pada 3 digit terakhir (`kode_unik`).
   - Contoh UI: `Rp 50.`**`342`** *(50.000 nominal pokok + 342 kode unik)*.
3. **Tombol "Salin Nominal Tepat" (Copy Button)**:
   - Sediakan tombol salin satu ketukan di sebelah nominal total.
   - Berikan konfirmasi toast: *"Nominal Rp 50.342 disalin ke clipboard"*.
4. **Hitung Mundur Masa Berlaku (Countdown Timer)**:
   - Gunakan `sisa_waktu_detik` dari API untuk menampilkan hitung mundur (misal: `59:45 tersisa`).
   - Jika waktu habis, ubah status layar menjadi kedaluwarsa dan sediakan tombol *"Buat Kode Bayar Baru"*.
5. **Banner Peringatan Wajib**:
   - Cantumkan teks panduan: *"Wajib transfer PERSIS sesuai 3 digit terakhir agar pembayaran Anda diverifikasi otomatis oleh sistem."*

### B. Idempotency Key pada Pembuatan Pembayaran
Saat memanggil `POST /api/v1/pembayaran/qris`, sertakan header:
```http
Idempotency-Key: <uuidv4-client-generated>
```
Hal ini menjamin apabila user menekan tombol bayar berkali-kali akibat sinyal lambat di pedesaan, backend hanya memproses transaksi satu kali dan mengembalikan alokasi yang sama.

---

## 4. Unggah Bukti Bayar & Dokumen KYC (Enkripsi Data Pribadi)

### A. Kompresi Gambar di Sisi Klien (Client-Side Compression)
Kamera smartphone modern menghasilkan foto berukuran 5MB - 12MB, sedangkan backend membatasi maksimal 5MB.
- **Wajib Kompresi Sebelum Unggah**: Gunakan library seperti `browser-image-compression` (Web) atau `flutter_image_compress` (Mobile).
- Konversikan gambar menjadi JPEG dengan kualitas 80% dan resolusi maksimal 1920x1080 (ukuran hasil berkisar 300KB - 800KB).
- Kirim via `multipart/form-data` dengan parameter `dokumen` (atau `file`) dan `jenis_dokumen` (`ktp`, `kk`, `bukti_bayar`).

### B. Perlindungan Privasi Data Pribadi (PII Masking)
- **NIK KTP**: API hanya mengembalikan NIK dalam format tersamar (masking): `3201********1234`.
- Frontend **DILARANG** mencoba membuka sensor masking atau menyimpan plaintext NIK di local storage yang tidak terenkripsi.
- Tampilkan indikator *"Data Pribadi Terenkripsi"* pada formulir pendaftaran untuk transparansi keamanan data warga desa.

---

## 5. Ringkasan Endpoint Utama untuk Frontend

| Metode | Endpoint | Deskripsi | Peran Akses |
| :--- | :--- | :--- | :--- |
| `POST` | `/api/v1/auth/login` | Masuk akun warga / pengurus | Publik |
| `POST` | `/api/v1/auth/refresh` | Perpanjang masa aktif token | Publik |
| `GET` | `/api/v1/auth/me` | Profil pengguna & peran saat ini | Terautentikasi |
| `POST` | `/api/v1/pembayaran/qris` | Minta kode QRIS & kode unik | Anggota / Pengurus |
| `GET` | `/api/v1/pembayaran/{id}` | Cek status lunas / verifikasi | Anggota (Miliknya) / Admin |
| `POST` | `/api/v1/pembayaran/{id}/bukti` | Unggah bukti transfer | Anggota (Miliknya) |
| `POST` | `/api/v1/pendaftaran` | Ajukan keanggotaan baru | Calon Anggota |
| `GET` | `/api/v1/pendaftaran/{id}` | Cek status pendaftaran (Masked NIK) | Calon Anggota (Miliknya) / Admin |
| `POST` | `/api/v1/pendaftaran/{id}/dokumen` | Unggah foto KTP KYC | Calon Anggota |
| `POST` | `/api/v1/pendaftaran/{id}/verifikasi` | Persetujuan anggota (Maker-Checker) | Pengurus / Admin |
| `GET` | `/api/v1/kas/mutasi` | Buku mutasi kas masuk/keluar | Pengurus / Pengawas |
