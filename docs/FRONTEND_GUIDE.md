# Panduan Integrasi Frontend — Kopdes Merah Putih (Ramping)

Target Kontrak: [openapi.yaml](openapi.yaml) (`v2.2.0-lean`, `37` endpoint) | Matriks Peran: [roles_matrix.json](roles_matrix.json) | Audit Keputusan: [DECISION_AUDIT.md](DECISION_AUDIT.md) | Direktori Frontend: **[../frontend/README.md](../frontend/README.md)** | Daftar Tugas Tim Frontend: **[../frontend/TODO_FRONTEND.md](../frontend/TODO_FRONTEND.md)** | Referensi Wireframe: [../frontend/wireframe/](../frontend/wireframe/) ([LAPORAN_E2E.md](wireframe/LAPORAN_E2E.md)) & Tester Polos: [../frontend/tester/](../frontend/tester/).

> **Aturan Keras**: Jangan menanamkan nomor port backend, URL API produksi, ataupun kunci rahasia di kode frontend. Selalu baca konfigurasi runtime dari `/config.json` (`api_public_url: ""` untuk mode same-origin HTTPS di balik Caddy, atau URL env lokal) lalu sinkronkan batas aturan dari `GET /api/v1/public/config`.

---

## 1. Memulai & Konfigurasi Dinamis

1. Panggil **`GET /api/v1/public/config`** saat aplikasi boot (tanpa autentikasi):
   - Mengembalikan `api_version`, `server_time`, `server_unix_ms`, `upload_limits`, `kyc_required_document_types`, `payment_expiry_seconds`, `stock_hold_minutes`, `polling_interval_ms`, `features` (`payments_enabled`, `registration_enabled`, `login_enabled`, `read_only_mode`, `qris_dynamic_enabled`, `personal_payment_pilot_enabled`), `auth` (`cookie_primary: true`, `bearer_enabled`, `csrf_header: "X-Requested-With"`), `captcha`, `mfa_mandatory_roles`, `session_idle_ttl_seconds`, `pagination`, dan objek `koperasi` (`ongkir_tetap`, `qris_unique_digits`, `active_qris`).
2. **Sesi Cookie `HttpOnly` Utama + `CSRFGuard`**:
   - Selalu sertakan `credentials: "include"` pada setiap `fetch()`.
   - Pada semua permintaan mutasi (`POST`, `PUT`, `PATCH`, `DELETE`), wajib kirim header kustom `X-Requested-With: <nama-aplikasi>` agar lolos `CSRFGuard`.
   - Saat aplikasi web dimuat ulang (reload), panggil `GET /api/v1/auth/me` (dengan `credentials: "include"`). Jika `200`, sesi cookie warga desa langsung pulih tanpa login ulang.
   - Bila backend dikonfigurasi `ALLOW_BEARER_AUTH=false` (bawaan produksi/Set B), respons `POST /api/v1/auth/login` **tidak** menyertakan `session_token` di JSON — jangan berasumsi `session_token` selalu ada. Bila `ALLOW_BEARER_AUTH=true` dipakai klien non-browser, simpan token hanya di memori JS (dilarang di `localStorage`/`sessionStorage`).
3. **Header Penting Lain**:
   - `Idempotency-Key: <uuid>` wajib untuk `POST /api/v1/toko/checkout` dan sangat disarankan pada `POST /api/v1/pembayaran`.

---

## 2. Alur Autentikasi, MFA TOTP 2-Langkah, & Sesi Opaque

Backend memakai **sesi opaque** berbasis database (tanpa JWT & tanpa `refresh_token`). Sesi memperpanjang masa idle otomatis (throttled paling sering 1x per 60 detik di server) hingga `SESSION_ABS_TTL`, dan dapat dicabut seketika oleh server saat logout, suspend anggota, atau reset admin.

1. **Login (`POST /api/v1/auth/login`)**:
   - Body: `{ "identifier": "<email_atau_no_hp>", "password": "..." }`.
   - Jika tidak wajib MFA: HTTP `200`, `scope: "full"` + cookie `__Host-kopdes_session` (HTTPS) / `kopdes_session` (HTTP lokal).
   - Jika wajib MFA (`admin`, `bendahara`) dan belum setup: `scope: "mfa_setup"`, `mfa_setup_required: true`. Sesi hanya boleh mengakses `/auth/mfa/setup`, `/auth/me`, dan `/auth/logout`.
   - Jika MFA aktif: `scope: "mfa_pending"`, `mfa_required: true`.
2. **Enrollment MFA 2-Langkah (`POST /api/v1/auth/mfa/setup`)**:
   - **Langkah A**: Kirim `{}` -> `secret`, `otpauth_uri`, `setup_token`.
   - **Langkah B**: Kirim `{ "setup_token": "...", "code": "<6-digit-TOTP>" }` -> `mfa_enabled: true`, `scope: "full"`, dan `8` `recovery_codes` sekali tampil.
3. **Verifikasi MFA Harian (`POST /api/v1/auth/mfa/verify`)**:
   - Kirim `{ "code": "<6-digit-TOTP>" }` atau `{ "recovery_code": "<kode>" }`. Kode TOTP 30-detik tidak dapat dipakai ulang (*anti-replay* `401`).
4. **Aktivasi Akun Baru / Ganti Sandi (`POST /api/v1/auth/password`)**:
   - Aktivasi: `{ "code": "<activation_code>", "new_password": "..." }`; ganti sandi saat login: `{ "current_password": "...", "new_password": "..." }`.
5. **Logout (`POST /api/v1/auth/logout`)**:
   - Kirim dengan `credentials: "include"` dan `X-Requested-With`. Server mencabut sesi di DB dan menghapus cookie (`Max-Age=0`).

---

## 3. Matriks 4 Peran Inti (Sinkron dengan `routes.go`)

| Peran | Hak Utama | Pembatasan Keras Server (Wajib Dicerminkan di UI) |
| :--- | :--- | :--- |
| `anggota` | Katalog (`harga_anggota` & `stok_tersedia`), checkout, iuran sendiri, lihat & buat pembayaran `qris` sendiri, unggah bukti, batal pesanan yang belum dibayar. | Ditolak `403` untuk `metode: "tunai"`, ubah status pesanan selain batal milik sendiri, lihat anggota lain, `/users`, `/pendaftaran`, `/laporan`. |
| `petugas_toko` | Kelola kategori/produk/promo (`foto_url` relatif `/...` atau `https://` maks 512 karakter, anjuran WebP/JPEG terkompresi <= 500 KB), ubah status pemenuhan pesanan (`PATCH /api/v1/pesanan/{id}/status`), cetak/unduh `daftar-antar`, lihat anggota tersamar & laporan toko. | Ditolak `403` untuk mencatat pembayaran `tunai`, verifikasi pembayaran, refund, maupun membuka KYC/`unmask`. |
| `bendahara` | Kelola jenis/tagihan/keringanan iuran, buat pembayaran `tunai`/`qris`, verifikasi pembayaran & ajukan/putuskan refund (dual-control), ubah pengaturan koperasi, akses laporan penuh. | Ditolak `403` pada `PATCH /api/v1/pesanan/{id}/status` (khusus toko/admin), dilarang self-approve (`DUAL_CONTROL_VIOLATION`), dilarang pakai mode `breakglass`, dilarang `unmask=true`. |
| `admin` | Kendali user (`/users`), verifikasi & dekripsi KYC (`/pendaftaran`), baca **audit log (`GET /api/v1/audit-log`)**, status anggota, impor CSV, `unmask=true`, plus hak toko & bendahara. | Tetap tunduk pada *Dual Control*, kecuali bila `DUAL_CONTROL_BREAKGLASS_ENABLED=true` dan admin menyertakan `breakglass: true`, `mfa_code` TOTP segar, & `alasan` >= 10 karakter. |

---

## 4. Alur Fitur Utama Pasca-Audit Keputusan

1. **Pendaftaran & Minimisasi PII (`claim:04-pendaftaran-kyc`)**:
   - `tanggal_lahir` kini **opsional** di `POST /api/v1/public/pendaftaran` dan impor CSV; tidak perlu memaksa input tanggal lahir di formulir publik kecuali diminta khusus.
   - `POST /api/v1/public/pendaftaran/{id}/dokumen` wajib membawa header `X-Upload-Token`.
2. **E-Katalog & Reservasi Stok (`claim:07-katalog-produk`, `claim:08-checkout-pesanan-daftar-antar`)**:
   - Gunakan `stok_tersedia` (`stok - stok_tertahan`) dan `harga_aktif_tersedia` untuk menonaktifkan tombol beli saat stok tertahan habis atau harga tier belum disetel.
   - Saat checkout sukses, server menahan stok (`stok_tertahan += qty`) selama `STOCK_HOLD_MINUTES` (dari `/public/config.stock_hold_minutes`). Stok fisik baru dikurangi ketika pembayaran disetujui (`setuju`) atau pesanan tunai diproses. Jika masa tahan habis dan stok fisik keburu habis dibeli orang lain, admin yang terlanjur menekan `setuju` tidak lagi mendapat error menggantung melainkan HTTP `200` dengan `status: "perlu_tindak_lanjut"`.
3. **Pembayaran QRIS, Dual-Control, & Refund Manual (`claim:09-pembayaran-qris-dual-control`)**:
   - Tampilkan `nominal_total = nominal_pokok + kode_unik` secara menonjol.
   - Unggah bukti memakai `POST /api/v1/pembayaran` multipart (`file`, `pembayaran_id`); server menolak bukti duplikat SHA-256 (`409`) dan mengenkripsinya di `STORAGE_BUKTI_DIR`.
   - **Pengembalian Dana (`POST /api/v1/pembayaran/{id}/refund`)**: Mendukung JSON & `multipart/form-data` (bukti transfer opsional terenkripsi di `STORAGE_BUKTI_DIR`). Langkah: staf 1 mengirim `{ "aksi": "ajukan", "metode": "tunai|transfer", "nominal": ..., "alasan": "..." }`, staf 2 berbeda mengirim `{ "aksi": "setujui", "catatan_pelaksana": "..." }` (+ opsional `file`). Bukti refund diunduh lewat `GET /api/v1/pembayaran?refund_bukti_id=<id>`.

---

## 5. Verifikasi Live

Semua kontrak di atas diverifikasi otomatis oleh `go -C backend test -count=1 -race -p 1 ./...` (termasuk `TestDecisionAudit_D1..D10` dan `TestRBAC_MatrixMatchesLiveRouterAndEnforcesBoundaries` yang memeriksa **37 rute**), Playwright Suite `01`–`06` pada `frontend/wireframe/`, serta dapat dicoba manual di browser via `go -C backend run ./cmd/kopdes uiserve --ui-dir frontend/tester`.
