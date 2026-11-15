# 16. Checklist Pra-Produksi UX, Keamanan & Kepatuhan Klien

Gunakan daftar periksa ini sebelum aplikasi frontend dirilis kepada pengurus maupun warga koperasi desa.

---

## 1. Pengalaman Pengguna Desa (*Mobile-First* & Sinyal Lambat)

- [ ] Ukuran target tekan tombol utama minimal **44x44 px** (mudah ditekan jari di layar ponsel).
- [ ] Seluruh tombol mutasi (`Bayar`, `Checkout`, `Ajukan Pendaftaran`, `Verifikasi`) langsung nonaktif & menampilkan spinner saat diklik agar tidak terjadi klik ganda.
- [ ] Pesan kesalahan memakai Bahasa Indonesia ramah warga sesuai [pesan-error-id.json](pesan-error-id.json) beserta nomor `X-Request-ID` bila terjadi `500`.
- [ ] Nominal QRIS menampilkan **Nominal Total tepat (termasuk 3–4 digit kode unik)** dalam huruf besar disertai tombol **"Salin Nominal"** 1 kali ketuk.
- [ ] Polling status pembayaran berhenti otomatis saat halaman dipindah/tutup (`AbortController`) dan jeda saat layar HP dimatikan/pindah tab (`visibilitychange`).
- [ ] Seluruh foto KYC dioptimalkan otomatis di browser (`<= max_kyc_upload_bytes`, format JPEG 0.8 lebar maks 1600 px) dengan bar progres persen.

## 2. Aksesibilitas & Ketahanan Data

- [ ] Setiap input formulir memiliki `<label>` / `aria-label` yang jelas; pesan error memakai `role="alert"` dan notifikasi sukses memakai `role="status"`.
- [ ] Kontras teks memenuhi rasio keterbacaan di luar ruangan (*outdoor sunlight*) dengan teks hitam/gelap tegas.
- [ ] Seluruh nominal uang memakai `formatRupiah()` (`int64` / string desimal, **tanpa aritmetika float pecahan**).
- [ ] Seluruh tanggal operasional dan kueri harian memakai zona waktu `Asia/Jakarta` (`timezone` dari `/api/v1/public/config`), dan hitung mundur memakai selisih `server_unix_ms`.

## 3. Keamanan & Privasi Klien

- [ ] **Zero hardcode**: Tidak ada literal port tes (`127.0.0.1:...` / `localhost:...`) atau secret di repositori/bundle frontend; seluruh URL & batas berasal dari env build-time + `GET /api/v1/public/config`.
- [ ] **Zero `localStorage` / `sessionStorage` untuk token & PII**: `access_token`, `refresh_token`, `setup_token`, `mfa_session_token`, NIK mentah, maupun foto KTP tidak pernah ditulis ke Web Storage maupun URL query. <!-- claim:CLAIM-CLIENT-TOKEN-MEMORY UNVERIFIED: Kebijakan penyimpanan memori di sisi aplikasi frontend akhir diperiksa melalui audit kode statis dan Frontend Tester, bukan assertion payload API -->
- [ ] Jika memakai BFF berbasis cookie `HttpOnly`, seluruh mutasi dilengkapi proteksi CSRF (`SameSite=Strict/Lax` + anti-CSRF token).
- [ ] `BroadcastChannel` logout hanya mengirim sinyal `"logout"` tanpa menyebarkan isi JWT.
- [ ] Banner peringatan `warning_banner` tidak dapat ditutup ketika transaksi memakai rekening pilot (`is_rekening_pribadi === true`).

## 4. Catatan Hukum & Akuntansi Sebelum Go-Live

- [ ] Dokumen kebijakan privasi, persetujuan pemrosesan NIK/KTP (UU No. 27 Tahun 2022 PDP), dan jangka waktu retensi penghapusan berkas tertolak (**30 hari**) telah ditinjau (**PERLU VERIFIKASI HUKUM**).
- [ ] Penamaan akun akuntansi untuk jenis iuran di koperasi setempat telah disahkan oleh akuntan/pengurus berwenang (**KEPUTUSAN AKUNTAN**).
