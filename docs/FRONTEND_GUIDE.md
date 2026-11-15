# Panduan Integrasi Frontend — Kopdes Merah Putih (Ramping)

Target Kontrak: [openapi.yaml](openapi.yaml) (`v2.0.0-lean`, `35` endpoint) | Matriks Peran: [roles_matrix.json](roles_matrix.json) | Bukti Live: [../frontend-tester/README.md](../frontend-tester/README.md).

> **Aturan Keras**: Jangan menanamkan nomor port backend, URL API produksi, ataupun kunci rahasia di kode frontend. Selalu baca konfigurasi runtime dari environment frontend lalu sinkronkan batas aturan dari `GET /api/v1/public/config`.

---

## 1. Memulai & Konfigurasi Dinamis

1. Panggil **`GET /api/v1/public/config`** saat aplikasi boot (tanpa autentikasi):
   - Mengembalikan `api_version`, `server_time`, `server_unix_ms` (deteksi selisih jam klien), `upload_limits`, `kyc_required_document_types`, `payment_expiry_seconds`, `polling_interval_ms`, `features` (`payments_enabled`, `registration_enabled`, `login_enabled`, `read_only_mode`), `captcha`, `mfa_mandatory_roles`, `access_token_ttl_seconds`, `session_idle_ttl_seconds`, `pagination`, dan objek `koperasi` (termasuk `ongkir_tetap`, `qris_unique_digits`, dan `active_qris`).
2. Gunakan header standar bila perlu:
   - `Authorization: Bearer <session_token>` (disimpan **hanya di memori JS** — jangan gunakan `localStorage` atau `sessionStorage` untuk bearer token agar terhindar dari eksfiltrasi XSS; atau andalkan cookie HttpOnly `kopdes_session` bila memakai BFF se-origin).
   - `Idempotency-Key: <uuid>` wajib untuk `POST /api/v1/toko/checkout` dan sangat disarankan pada `POST /api/v1/pembayaran`.

---

## 2. Alur Autentikasi, MFA TOTP 2-Langkah, & Sesi Opaque

Backend memakai **opaque session token** (tanpa JWT & tanpa `refresh_token`). Sesi memperpanjang masa idle otomatis selama pengguna aktif hingga batas absolut `SESSION_ABS_TTL`.

1. **Login (`POST /api/v1/auth/login`)**:
   - Body: `{ "identifier": "<email_atau_no_hp>", "password": "..." }`.
   - Jika peran anggota (`mfa_enabled: false` & bukan `admin`/`bendahara`): mengembalikan HTTP `200` dengan `scope: "full"` dan `session_token`.
   - Jika peran wajib MFA (`admin`, `bendahara`) dan **belum setup MFA**: mengembalikan `scope: "mfa_setup"`, `mfa_setup_required: true`. Sesi ini **hanya** bisa mengakses `/auth/mfa/setup`, `/auth/me`, dan `/auth/logout` (`403` untuk endpoint lain).
   - Jika peran memiliki **MFA aktif**: mengembalikan `scope: "mfa_pending"`, `mfa_required: true`.
2. **Enrollment MFA 2-Langkah (`POST /api/v1/auth/mfa/setup`)**:
   - **Langkah A (Minta QR/Secret)**: Kirim `{}` dengan `Authorization: Bearer <session_token>`. Respons `200` berisi `secret`, `otpauth_uri`, dan `setup_token` (terenkripsi AAD terikat pada `session_id`).
   - **Langkah B (Konfirmasi Aktivasi)**: Kirim `{ "setup_token": "...", "code": "<6-digit-TOTP>" }` ke **endpoint yang sama** (`POST /api/v1/auth/mfa/setup`). Respons `200` berisi `mfa_enabled: true`, `scope: "full"`, dan 8 `recovery_codes` sekali tampil.
3. **Verifikasi MFA Harian (`POST /api/v1/auth/mfa/verify`)**:
   - Untuk login berikutnya (`scope: "mfa_pending"`), kirim `{ "code": "<6-digit-TOTP>" }` atau `{ "recovery_code": "<kode>" }`.
   - **Penting**: Setiap kode TOTP 30-detik hanya berlaku satu kali (*anti-replay*; penggunaan ulang mengembalikan `401`).
4. **Aktivasi Akun Baru / Ganti Sandi (`POST /api/v1/auth/password`)**:
   - Aktivasi tanpa login: kirim `{ "code": "<activation_code>", "new_password": "..." }`.
   - Ubah sandi saat login: kirim `{ "current_password": "...", "new_password": "..." }`.
5. **Logout (`POST /api/v1/auth/logout`)**:
   - Segera hapus token dari memori JS setelah memanggil logout. Apabila API mengembalikan `401` (misal anggota disuspend oleh admin), bersihkan state sesi dan arahkan ke halaman login.

---

## 3. Matriks 4 Peran Inti

| Peran | Hak Utama | Catatan Keamanan UI |
| :--- | :--- | :--- |
| `anggota` | Katalog (`harga_anggota`), checkout, iuran sendiri, lihat & buat pembayaran `qris` sendiri, unggah bukti. | Tidak boleh melihat anggota lain, tidak boleh metode `tunai`, tidak boleh akses `/users`, `/pendaftaran`, `/laporan`. |
| `petugas_toko` | Kelola produk/kategori/promo (`POST /api/v1/katalog/produk`), ubah status pesanan (`PATCH /api/v1/pesanan/{id}/status`), cetak/unduh `daftar-antar`, lihat anggota & laporan toko. | Tidak boleh verifikasi pembayaran atau melihat KYC mentah. |
| `bendahara` | Kelola jenis/tagihan/keringanan iuran, verifikasi pembayaran (`POST /api/v1/pembayaran/{id}/verifikasi` dengan dual control), ubah pengaturan koperasi, akses laporan penuh. | Dilarang menyetujui pembayaran yang ia buat sendiri (`403 DUAL_CONTROL_VIOLATION`); tidak boleh `unmask=true` NIK. |
| `admin` | Seluruh kendali user (`/users`), review & dekripsi KYC (`/pendaftaran`), ubah status anggota (`/anggota/{id}/status`), impor CSV, `unmask=true` anggota, pengaturan. | Juga tunduk pada aturan *Dual Control* bila membuat pembayaran sendiri (`403` saat mencoba memverifikasi sendiri). |

---

## 4. Ringkasan Endpoint & Alur Bisnis Utama

1. **Pendaftaran & KYC (`claim:04-pendaftaran-kyc`)**:
   - `POST /api/v1/public/pendaftaran` -> simpan `id` dan `upload_token` (hanya di memori).
   - `POST /api/v1/public/pendaftaran/{id}/dokumen` (multipart `tipe_dokumen=ktp|selfie_ktp|kk`, `file=image/png|jpeg`) wajib menyertakan header `X-Upload-Token`.
   - Setelah admin menyetujui (`POST /api/v1/pendaftaran/{id}/verifikasi`, `keputusan: "disetujui"`), admin meneruskan `activation_code` kepada calon anggota untuk mengaktifkan kata sandi via `POST /api/v1/auth/password`.
2. **E-Katalog & Checkout (`claim:07-katalog-produk`, `claim:08-checkout-pesanan-daftar-antar`)**:
   - Tidak ada endpoint keranjang server; kelola keranjang sementara di state React/JS, lalu kirim array `items: [{ produk_id, kuantitas }]` ke `POST /api/v1/toko/checkout`.
   - Produk yang `harga_aktif_tersedia: false` (`harga_aktif: null`) wajib dinonaktifkan tombol belinya di UI; jika dipaksa checkout, server menolak (`409` / `400`).
   - Saat checkout, server memeriksa ketersediaan stok namun **belum memotong stok**. Pemotongan stok atomik dilakukan saat pembayaran disetujui admin/bendahara (`setuju`) atau saat pesanan tunai masuk status `dibayar`/`diproses`.
3. **Pembayaran QRIS Statis & Dual Control (`claim:09-pembayaran-qris-dual-control`)**:
   - `POST /api/v1/pembayaran` menghasilkan `pembayaran.nominal_total = nominal_pokok + kode_unik`. Tampilkan `nominal_total` dan QRIS `rekening_qris` secara jelas kepada pembayar agar nominal transfer persis hingga 3–4 digit terakhir.
   - Unggah bukti dilakukan dengan `POST /api/v1/pembayaran` bertipe `multipart/form-data` (`pembayaran_id`, `file`). Gambar duplikat (SHA-256 identik) ditolak `409`. Server mengenkripsi file bukti ke `<STORAGE_KYC_DIR>/bukti/bukti_<id>.enc` (terpisah dari berkas KYC).
   - Verifikasi bendahara/admin memanggil `POST /api/v1/pembayaran/{id}/verifikasi` dengan `{ "keputusan": "setuju" | "tolak", "nominal_dibayar": <int64>, "alasan": "..." }`. Perhatikan `keputusan` bernilai `"setuju"` atau `"tolak"` (berbeda dari verifikasi pendaftaran yang memakai `"disetujui"` / `"ditolak"` / `"perlu_perbaikan"`). Tangani respons `409` (`OUT_OF_STOCK`) saat verifikasi `setuju` atau perubahan status pesanan jika stok habis terjual terlebih dahulu oleh pesanan lain.
4. **Laporan & Ekspor CSV (`claim:10-laporan-csv`)**:
   - Gunakan `GET /api/v1/laporan?jenis=anggota|tunggakan|penjualan|pembayaran` (tambahkan `&format=csv` untuk unduh). Seluruh sel CSV sudah dinetralisasi awalan `=`, `+`, `-`, `@` terhadap *CSV formula injection*.

---

## 5. Cara Membuktikan Dokumentasi Secara Live

Semua klaim integrasi di atas dapat dijalankan langsung (tanpa mock) menggunakan **Frontend Tester Polos** (`frontend-tester/`):

1. Jalankan API & tester dengan profil sintetis `.env.test`.
2. Masukkan `TEST_SEED_PASSWORD` di kotak input UI Tester.
3. Jalankan **Run All Diagnostics**, **Run Full Live Demo**, **Run All Non-Destructive Scenarios (SCEN-01..10)**, dan **Run Role Matrix (80/80)** untuk memverifikasi perilaku nyata 35 endpoint di browser.
