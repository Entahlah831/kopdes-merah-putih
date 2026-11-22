# Panduan Sistem Lengkap — Kopdes Merah Putih

Dokumen ini adalah manual referensi operasional, integrasi frontend, alur peran, dan arsitektur basis data untuk **Backend Ramping Kopdes Merah Putih** (**1 koperasi di 1 lokasi**, Go `net/http` + `sqlc` + PostgreSQL 16).

---

## Daftar Isi
1. [Bagian 1: Panduan Set Up & Deployment (Lokal dan VPS)](#bagian-1-panduan-set-up--deployment-lokal-dan-vps)
2. [Bagian 2: Kegunaan Setiap Fitur & Cara Memasang / Mengonfigurasinya](#bagian-2-kegunaan-setiap-fitur--cara-memasang--mengonfigurasinya)
3. [Bagian 3: Cara Mengintegrasikan Backend ke Aplikasi Frontend](#bagian-3-cara-mengintegrasikan-backend-ke-aplikasi-frontend)
4. [Bagian 4: Alur Lengkap Setiap Role (`publik`, `anggota`, `petugas_toko`, `bendahara`, `admin`)](#bagian-4-alur-lengkap-setiap-role)
5. [Bagian 5: Arsitektur & Alur Database Lengkap (Seluruh 21 Tabel)](#bagian-5-arsitektur--alur-database-lengkap-seluruh-21-tabel)
6. [Bagian 6: Kepatuhan Hukum, Akuntansi, & Batasan Operasional](#bagian-6-kepatuhan-hukum-akuntansi--batasan-operasional)

---

## Bagian 1: Panduan Set Up & Deployment (Lokal dan VPS)

### 1.1 Komponen Biner yang Tersedia
Proyek dikompilasi menjadi 4 biner utama (ditambah 1 alat seeder pengujian):
- **`kopdes-api`** ([cmd/server/main.go](../cmd/server/main.go)): Server HTTP API 36 endpoint (`net/http`), memverifikasi role DB non-superuser (`kopdes_app`) saat startup dan menolak start jika profil `production` memakai `ALLOWED_ORIGINS=*` atau password default dev.
- **`kopdes`** ([cmd/kopdes/main.go](../cmd/kopdes/main.go)): CLI pemeliharaan & worker (`admin {create,reset-mfa,reset-password}`, `iuran generate`, `pesanan expire`, `pdp purge`, `bukti purge`, `storage migrate-files`, `worker [--tick 60s] [--once]`).
- **`envcheck`** ([cmd/envcheck/main.go](../cmd/envcheck/main.go)): Memvalidasi tepat 46 variabel env (`--strict --ports --no-listen --compare`).
- **`uitester`** ([cmd/uitester/main.go](../cmd/uitester/main.go)): Server frontend tester polos HTML/JS lokal (khusus non-produksi).
- **`seed_test_accounts`** ([cmd/seed_test_accounts/main.go](../cmd/seed_test_accounts/main.go)): Seeder deterministik khusus database berakhiran `_test`.

### 1.2 Menyiapkan Berkas `.env` (46 Variabel)
Seluruh konfigurasi infrastruktur berada di `.env` (referensi lengkap: [ENV_REFERENCE.md](ENV_REFERENCE.md)).

1. Buat `.env` dari templat kosong [.env.example](../.env.example) sambil membangkitkan kunci rahasia acak CSPRNG (`32 byte`):
   - **Linux/macOS**:
     ```bash
     GENERATE_SECRETS=true ./scripts/promote_env.sh .env.example .env
     ```
   - **Windows PowerShell**:
     ```powershell
     .\scripts\promote_env.ps1 -Template .env.example -Target .env -GenerateEmptySecrets
     ```
2. Atur variabel non-rahasia sesuai lingkungan Anda:
   - `APP_ENV=production` (atau `development` saat pengembangan)
   - `APP_TIMEZONE=Asia/Jakarta`
   - `API_PORT`, `API_HOST_PORT`, `DB_PORT`, `DB_HOST_PORT` (tidak boleh bentrok; dicek oleh `envcheck --ports`)
   - `ALLOWED_ORIGINS=https://kopdes-desaanda.id` (**wajib** origin frontend spesifik di produksi; dilarang `*`)
   - `TRUSTED_PROXIES=127.0.0.1/32` (isi CIDR *reverse proxy* agar pembacaan IP klien & `X-Forwarded-Proto: https` valid dan anti-spoofing)
   - `COOKIE_SECURE=auto`, `ALLOW_BEARER_AUTH=false` (cookie `HttpOnly` utama `__Host-kopdes_session` + `CSRFGuard`)
   - `DB_HOST=postgres` (jika memakai `docker-compose.yml`) atau `127.0.0.1`, `DB_USER=kopdes_app`
   - `STORAGE_KYC_DIR=/app/storage/kyc` dan `STORAGE_BUKTI_DIR=/app/storage/bukti` (volume & retensi terpisah)
   - `BACKUP_AGE_RECIPIENT=age1...` (*public key* `age` saja; **private key wajib disimpan di luar VPS!**)
3. Verifikasi `.env` sebelum menyalakan aplikasi:
   ```bash
   go run ./cmd/envcheck --strict --ports --no-listen --env-file .env
   ```

### 1.3 Cara Set Up di VPS (Docker Compose + Reverse Proxy HTTPS + Cron)

#### Langkah 1 — Persiapan Server VPS
- Gunakan VPS Linux (Ubuntu 24.04 disarankan, minimal 1 vCPU / 1 GB RAM) dengan Docker Engine + Docker Compose v2 terpasang.
- Batasi firewall VPS (`ufw`) hanya ke SSH dan web HTTP/HTTPS. **Jangan** mengekspos `${DB_HOST_PORT}` PostgreSQL ke internet publik (di [docker-compose.yml](../docker-compose.yml#L24), port DB terikat khusus ke `127.0.0.1:${DB_HOST_PORT}`).
- Disarankan mengubah mapping port API di [docker-compose.yml](../docker-compose.yml#L91) menjadi `"127.0.0.1:${API_HOST_PORT}:${API_PORT}"` bila reverse proxy berjalan di VPS yang sama, sehingga seluruh akses luar wajib melewati TLS di reverse proxy.

#### Langkah 2 — Menjalankan Kontainer DB, API, & Worker Bawaan
```bash
docker compose --env-file .env config --quiet
docker compose --env-file .env up -d --build
```
Pada boot pertama dengan volume DB baru, kontainer `postgres` otomatis menjalankan:
1. [01_roles.sql](../db/docker-init/01_roles.sql) — membuat role aplikasi `kopdes_app` tanpa hak superuser (`NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS`).
2. [02_migrations.sh](../db/docker-init/02_migrations.sh) — menerapkan seluruh migrasi (`000001` & `000002`, total `21` tabel + `GRANT` izin terbatas ke `kopdes_app`).

Layanan **`worker`** di `docker-compose.yml` otomatis berjalan berkelanjutan (`kopdes worker --tick 60s`) dengan `pg_try_advisory_lock` per tugas untuk menjalankan `pesanan_expire` (melepas stok tertahan habis waktu), `iuran_generate`, `pdp_purge`, `bukti_purge`, dan `rate_limits_cleanup` sekaligus mencatat telemetry ke `worker_runs`.

#### Langkah 3 — Bootstrap Admin Pertama (atau Pemulihan Akses)
```bash
docker compose exec api /app/kopdes --env-file /app/.env admin create \
  --email ketua@kopdes-desaanda.id --nama "Ketua Koperasi" --hp 081234567890 --password "<SANDI_KUAT>"
# Bila pemilik/admin kehilangan ponsel TOTP atau lupa kata sandi:
docker compose exec api /app/kopdes --env-file /app/.env admin reset-mfa --email ketua@kopdes-desaanda.id
```

#### Langkah 4 — Pasang Reverse Proxy HTTPS (Caddy atau Nginx)
Dengan `COOKIE_SECURE=auto`, backend menyetel cookie `__Host-kopdes_session` (`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`) apabila koneksi TLS langsung **atau** `X-Forwarded-Proto: https` berasal dari IP di `TRUSTED_PROXIES`:
```caddyfile
api.kopdes-desaanda.id {
    reverse_proxy 127.0.0.1:{$API_HOST_PORT}
}
```
Uji kesehatan server setelah proxy aktif:
- `GET https://api.kopdes-desaanda.id/healthz` -> HTTP `200`
- `GET https://api.kopdes-desaanda.id/readyz` -> HTTP `200` (`database: CONNECTED`, `worker_jobs`, `worker_degraded`)
- `GET https://api.kopdes-desaanda.id/api/v1/public/config` -> HTTP `200`

#### Langkah 5 — Cron Backup Harian `age` Terautentikasi
Karena tugas periodik DB dijalankan oleh layanan `worker` di Compose, host VPS cukup menjadwalkan 1 tugas backup harian terenkripsi `age` + sinkronisasi salinan ke luar server (`rclone copy`):
```cron
# Backup harian DB + arsip STORAGE_KYC_DIR + STORAGE_BUKTI_DIR + manifest.sha256 (.tar.age)
0 3 * * * cd /opt/kopdes && ENV_FILE=/opt/kopdes/.env bash /opt/kopdes/scripts/backup.sh
```
Lakukan latihan pemulihan secara berkala memakai [restore_drill.sh](../scripts/restore_drill.sh) (memerlukan *private key* `age` off-server).

---

## Bagian 2: Kegunaan Setiap Fitur & Cara Memasang / Mengonfigurasinya

| # | Fitur & Paket | Kegunaan Operasional | Cara Memasang & Mengatur |
|---|---|---|---|
| 1 | **Konfigurasi Publik Dinamis** (`internal/api/handlers`) | Menyediakan parameter runtime aman tanpa autentikasi (jam server, batas upload, fitur aktif, CAPTCHA site key, profil koperasi, ongkir, & QRIS aktif) agar frontend tidak *hardcode*. | Otomatis aktif di `GET /api/v1/public/config`. Isi profil koperasi via `PUT /api/v1/pengaturan`; nyalakan/matikan fitur `.env` lewat `PAYMENTS_ENABLED`, `REGISTRATION_ENABLED`, `CAPTCHA_ENABLED`, dan `ALLOW_PERSONAL_PAYMENT_PILOT` (`read_only_mode` dikontrol opsi runtime `api.Config.ReadOnlyMode`). |
| 2 | **Autentikasi Sesi Opaque & MFA TOTP** (`internal/auth`) | Login email/no HP + `bcrypt`, sesi *opaque* tercatat di DB (`idle` & `abs TTL`), proteksi brute force (`failed_login_count` + `locked_until`), dan MFA TOTP RFC 6238 anti-replay + 8 *recovery code*. | Atur `SESSION_IDLE_TTL`, `SESSION_ABS_TTL`, `MFA_REQUIRED_ROLES`, `LOGIN_MAX_ATTEMPTS`, `LOGIN_LOCKOUT_DURATION` di `.env`. |
| 3 | **Pendaftaran Anggota & KYC Terenkripsi** (`internal/pendaftaran`) | Warga mendaftar mandiri + unggah foto KTP/KK/Selfie; file divalidasi ukuran, *magic bytes*, dan dimensi gambar asli, lalu dienkripsi **envelope AES-256-GCM** (`DEK` acak dibungkus `KEK`). | Pastikan `REGISTRATION_ENABLED=true`, volume persisten menunjuk `STORAGE_KYC_DIR`, berkas disimpan di `<STORAGE_KYC_DIR>/kyc/*.enc`. Aktifkan Turnstile bila perlu (`CAPTCHA_ENABLED=true`, `CAPTCHA_SITE_KEY`, `CAPTCHA_SECRET_KEY`). |
| 4 | **Manajemen Anggota & Impor CSV** (`internal/anggota`) | Melihat anggota (NIK/HP tersamar default; `?unmask=true` khusus `admin` + log audit), menonaktifkan anggota (otomatis mencabut semua sesi login aktifnya seketika), & impor massal CSV (`?dry_run=true`). | Siap pakai setelah migrasi. Admin menjalankan endpoint `/api/v1/anggota/...`. |
| 5 | **Iuran Bulanan & Keringanan** (`internal/iuran`) | Membuat jenis iuran (mis. Iuran Pokok / Wajib), menerbitkan tagihan bulanan massal secara idempoten (tidak duplikat untuk anggota+periode sama), dan memberi diskon keringanan dengan alasan tercatat. | Buat jenis iuran via `POST /api/v1/iuran/jenis` (`admin`/`bendahara`), lalu jalankan generate bulanan lewat CLI `kopdes iuran generate` atau API. |
| 6 | **E-Katalog Produk & Promo** (`internal/katalog`) | Mengelola kategori, produk (harga tier umum vs anggota aktif otomatis sesuai status sesi), stok non-negatif, dan voucher promo dengan kuota batas pakai & batas `sekali_per_anggota`. | `petugas_toko` atau `admin` menambahkan kategori, produk, & promo via `POST /api/v1/katalog/produk`. Produk tanpa harga aktif untuk tier terkait otomatis ditolak `409` saat checkout. |
| 7 | **Checkout Tanpa Keranjang Server & Pengantaran** (`internal/pesanan`) | Keranjang dikelola di browser klien; saat checkout (`Idempotency-Key` wajib) server menghitung subtotal/diskon/ongkir secara otoritatif, mengenkripsi HP & alamat penerima, serta memvalidasi `stok >= qty` tanpa memotong stok dulu. Menyediakan daftar antar & label cetak/CSV aman. | Atur `ongkir_tetap` koperasi via `PUT /api/v1/pengaturan` dan durasi batas bayar via `PAYMENT_EXPIRY_DURATION` di `.env`. Jadwalkan cron `kopdes pesanan expire`. |
| 8 | **Pembayaran QRIS Statis, Tunai, Refund & Dual Control** (`internal/pembayaran`) | QRIS statis dengan kode unik integer (`1..999` / `1..9999`), pencatatan tunai (`admin`/`bendahara`), bukti terenkripsi amplop di `STORAGE_BUKTI_DIR` (`bukti_<id>.enc` / `refund_<id>.enc`), verifikasi **Dual Control** (+ `breakglass` admin opsional), **potong stok tertahan atomik saat `setuju`** (atau fallback `perlu_tindak_lanjut`), & refund manual. | Tambah rekening QRIS via `POST /api/v1/pembayaran` (`action: "create_rekening"`), aktifkan via `PUT /api/v1/pengaturan`. Atur `STOCK_HOLD_MINUTES=60`, `DUAL_CONTROL_BREAKGLASS_ENABLED=false`, & `ALLOW_PERSONAL_PAYMENT_PILOT=false` di `.env`. |
| 9 | **Laporan Operasional & Audit Log Imutabel** (`internal/laporan` & `audit_log`) | Menghasilkan laporan `anggota`, `tunggakan`, `penjualan`, dan `pembayaran` dalam JSON maupun CSV dengan netralisasi karakter pembuka formula (`=`, `+`, `-`, `@`). | Dapat langsung diunduh melalui `GET /api/v1/laporan` oleh `admin`, `bendahara`, dan `petugas_toko`. |

---

## Bagian 3: Cara Mengintegrasikan Backend ke Aplikasi Frontend

### 3.1 Inisialisasi Saat Aplikasi Frontend Dimuat
Selalu arahkan `API_BASE_URL` dari environment frontend Anda (mis. `NEXT_PUBLIC_API_URL` / `VITE_API_URL`), lalu panggil:
```javascript
const res = await fetch(`${API_BASE_URL}/api/v1/public/config`, { credentials: "include" });
const { data: pubCfg } = await res.json();
```
Gunakan `pubCfg` untuk:
- Mengecek `pubCfg.features.read_only_mode`, `payments_enabled`, `registration_enabled` sebelum menampilkan form mutasi.
- Membatasi ukuran upload berkas sesuai `pubCfg.upload_limits.max_kyc_upload_bytes` dan tipe `pubCfg.upload_limits.allowed_mime_types`.
- Menampilkan informasi koperasi (`pubCfg.koperasi.nama_koperasi`, `pubCfg.koperasi.ongkir_tetap`, `pubCfg.koperasi.active_qris.qris_image_url`).
- Memeriksa selisih jam lokal pengguna terhadap `pubCfg.server_unix_ms`.

### 3.2 Penyimpanan Sesi & Pengiriman Kredensial + `CSRFGuard`
Backend memprioritaskan cookie `HttpOnly` sebagai transport web utama dan menonaktifkan Bearer di produksi secara bawaan (`ALLOW_BEARER_AUTH=false`):
1. **Cookie `HttpOnly` utama (`__Host-kopdes_session` HTTPS / `kopdes_session` HTTP lokal)**:
   Sertakan selalu `credentials: "include"` pada `fetch()`. Saat halaman dimuat ulang, panggil `GET /api/v1/auth/me` untuk memulihkan status login aktif tanpa meminta warga login berulang kali. Untuk mencegah CSRF, setiap mutasi (`POST`, `PUT`, `PATCH`, `DELETE`) dengan cookie aktif divalidasi `Origin`/`Referer`-nya oleh `CSRFGuard` dan **wajib** menyertakan header kustom `X-Requested-With: <id-app>`.
2. **Header `Authorization: Bearer <session_token>`** (hanya jika `ALLOW_BEARER_AUTH=true` untuk klien non-browser):
   Bila aktif dan dipakai klien non-browser, simpan `session_token` hanya di memori JS (dilarang di `localStorage`/`sessionStorage`).

> **CORS Produksi**: Di `.env` backend, `ALLOWED_ORIGINS` harus tepat menyamai origin frontend (termasuk skema `https://` dan subdomain tanpa slash penutup).

### 3.3 Header Khusus & Format Respons Standar
- **Header wajib/penting**:
  - `Idempotency-Key: <uuid-v4>` pada `POST /api/v1/toko/checkout` (wajib) dan `POST /api/v1/pembayaran` (sangat disarankan) agar klik ganda atau *retry* jaringan mengembalikan hasil transaksi yang sama (HTTP `200`) tanpa menggandakan pesanan/tagihan.
  - `X-Upload-Token: <upload_token>` saat mengunggah berkas KYC (`POST /api/v1/public/pendaftaran/{id}/dokumen`).
  - `X-Captcha-Token: <turnstile_response_token>` bila `pubCfg.captcha.enabled === true`.
- **Format JSON Respons Sukses**:
  ```json
  { "success": true, "data": { ... }, "meta": { "limit": 20, "offset": 0 } }
  ```
- **Format JSON Respons Error**:
  ```json
  { "success": false, "error": { "code": "KODE_ERROR", "message": "Pesan ramah pengguna", "details": ... } }
  ```
- **Error & Status Autentikasi Penting yang Harus Ditangani UI**:
  - Respons `200 OK` dari `POST /api/v1/auth/login` berisi `data.scope` (`"full"`, `"mfa_setup"`, atau `"mfa_pending"`) serta boolean `data.mfa_setup_required` / `data.mfa_required`: alihkan ke halaman setup/verifikasi MFA sebelum masuk ke menu utama (akses ke endpoint penuh dengan sesi terbatas akan ditolak `403 FORBIDDEN`).
  - `401 UNAUTHORIZED`: Sesi habis / kadaluwarsa / akun disuspend -> kosongkan state sesi JS, arahkan ke `/login`.
  - `401 INVALID_MFA_CODE` / `MFA_SESSION_REVOKED`: Kode TOTP salah atau percobaan MFA habis sehingga sesi dicabut dan wajib login ulang.
  - `403 DUAL_CONTROL_VIOLATION`: Tampilkan info bahwa verifikasi pembayaran harus dilakukan oleh petugas/admin lain.
  - `409 OUT_OF_STOCK`: Dapat muncul saat `Checkout` **maupun** saat verifikasi pembayaran disetujui (`setuju`) atau pesanan tunai diproses jika stok keburu habis oleh transaksi lain.
  - `409 DUPLICATE_BUKTI`: Gambar struk transfer yang sama sudah pernah dipakai pada transaksi lain.
  - `503 READ_ONLY_MODE` / `PAYMENTS_DISABLED`: Nonaktifkan tombol kirim dengan pemberitahuan pemeliharaan sistem.

---

## Bagian 4: Alur Lengkap Setiap Role

```text
+------------+     +---------------------+     +----------------------+
|   PUBLIK   | --> | Daftar + Upload KYC | --> | Menunggu Admin (KYC) |
+------------+     +---------------------+     +----------------------+
                                                          | disetujui + activation_code
                                                          v
+------------+     +---------------------+     +----------------------+
|  ANGGOTA   | <-- |  Aktivasi Password  | <-- | Akun + Data Anggota  |
+------------+     +---------------------+     +----------------------+
      |
      +---> Bayar Iuran / Checkout Katalog (QRIS) -> Unggah Bukti (.enc)
      |                                                    |
      |                                                    v
+------------+     +---------------------+     +----------------------+
| PETUGAS    | <-- |  Pesanan: dibayar   | <-- | BENDAHARA / ADMIN    |
| TOKO       |     |  (+ potong stok)    |     | Klik "setuju" (lunas)|
+------------+     +---------------------+     +----------------------+
      |
      +---> diproses -> diantar / siap_diambil -> selesai (+ cetak daftar-antar)
```

### 4.1 Role `publik` (Tanpa Login)
1. Membaca konfigurasi publik (`GET /api/v1/public/config`) dan menjelajahi katalog produk umum (`GET /api/v1/katalog/produk` memakai `harga_umum`).
2. Mengajukan pendaftaran anggota (`POST /api/v1/public/pendaftaran`) -> menyimpan sementara `id` dan `upload_token` dari respons di memori.
3. Mengunggah dokumen KYC `multipart/form-data` (`tipe_dokumen`: `ktp` wajib; `kk` & `selfie_ktp` opsional) ke `POST /api/v1/public/pendaftaran/{id}/dokumen` memakai header `X-Upload-Token`.
4. Setelah pendaftaran disetujui Admin dan menerima `activation_code`, mengaktifkan sandi akun lewat `POST /api/v1/auth/password` lalu login di `POST /api/v1/auth/login`.

### 4.2 Role `anggota` (Anggota Aktif Terautentikasi)
1. Melihat profil sesi (`GET /api/v1/auth/me`) dan detail profil anggota miliknya sendiri (`GET /api/v1/anggota/{id}` — tersamar). Akses ke data anggota lain ditolak `403`.
2. Melihat tagihan iuran miliknya (`GET /api/v1/iuran/tagihan`) dan memantau daftar jenis iuran (`?scope=jenis`).
3. Mengakses katalog (`GET /api/v1/katalog/produk`) dengan otomatis mendapatkan `harga_anggota` & `stok_tersedia` (`stok - stok_tertahan`), mengelola keranjang di browser, lalu mengirim checkout ke `POST /api/v1/toko/checkout` + `Idempotency-Key`. Server mengunci dan menahan stok (`stok_tertahan += qty`) selama `STOCK_HOLD_MINUTES` **tanpa memotong stok fisik dulu**.
4. Membayar tagihan iuran atau pesanan lewat `POST /api/v1/pembayaran` khusus `metode: "qris"` (ditolak `403` jika memilih `tunai`; hanya `admin`/`bendahara` yang boleh mencatat `tunai`). Server menerbitkan `nominal_total = nominal_pokok + kode_unik` dan data QRIS aktif.
5. Mengunggah gambar bukti transfer (`JPEG`/`PNG`) ke `POST /api/v1/pembayaran` (`multipart/form-data`: `pembayaran_id`, `file`, `catatan_pembayar`). Bukti dienkripsi amplop DEK ke `STORAGE_BUKTI_DIR` (`bukti_<id>.enc`) dan status berubah ke `menunggu_verifikasi`.
6. Dapat melihat pesanannya (`GET /api/v1/pesanan`), mengunduh bukti bayarnya (`GET /api/v1/pembayaran?bukti_id=<id>`), atau membatalkan pesanannya sendiri sebelum dibayar (`PATCH /api/v1/pesanan/{id}/status` ke `batal` ketika status `baru` atau `menunggu_pembayaran`).

### 4.3 Role `petugas_toko`
*(MFA tidak wajib kecuali disetel di `MFA_REQUIRED_ROLES`)*
1. Mengelola kategori, produk (harga umum, harga anggota, stok), dan promo melalui `POST /api/v1/katalog/produk` (`create_produk`, `update_produk`, `create_kategori`, `create_promo`).
2. Memantau seluruh pesanan (`GET /api/v1/pesanan`) dan menggerakkan status pemenuhan (`PATCH /api/v1/pesanan/{id}/status`):
   - Pesanan tunai di toko (`baru` -> `dibayar` atau `diproses`): **stok produk otomatis terpotong pada titik ini**.
   - Pesanan QRIS yang sudah disetujui bendahara/admin (status sudah `dibayar`, stok sudah terpotong): `dibayar` -> `diproses` -> `diantar` (untuk pesan-antar) atau `siap_diambil` -> `selesai`.
3. Mencetak / mengunduh rute kurir beserta nama, nomor HP, dan alamat penerima terdekripsi melalui `GET /api/v1/pesanan/daftar-antar` (`JSON` berisi `label_antar`, atau `&format=csv`).
4. Melihat daftar anggota tersamar (`GET /api/v1/anggota`, tanpa hak `?unmask=true`) dan meninjau/mengekspor laporan operasional (`GET /api/v1/laporan`).

### 4.4 Role `bendahara`
*(Wajib MFA TOTP secara default)*
1. Login pertama kali dengan `scope: "mfa_setup"`: menjalankan 2 langkah `POST /api/v1/auth/mfa/setup` (meminta `secret` + `setup_token`, lalu konfirmasi kode TOTP 6 digit untuk memperoleh `8` kode pemulihan sekali pakai). Login selanjutnya memverifikasi `POST /api/v1/auth/mfa/verify`.
2. Mengelola tarif jenis iuran dan menerbitkan tagihan bulanan massal (`POST /api/v1/iuran/jenis`), serta mencatat keringanan/pembebasan nominal tagihan (`PATCH /api/v1/iuran/tagihan/{id}/keringanan`).
3. Menambah rekening QRIS koperasi (`POST /api/v1/pembayaran` `action: "create_rekening"`) serta memperbarui pengaturan operasional koperasi & rekening QRIS aktif (`PUT /api/v1/pengaturan`).
4. Mencatat pembayaran tunai warga di kasir (`POST /api/v1/pembayaran` `metode: "tunai"` -> otomatis mendapat nomor kwitansi awal berurut `KWT-YYYY-XXXXXX` dan status `menunggu_verifikasi`).
5. Memverifikasi pembayaran QRIS/tunai yang diajukan pihak lain (`POST /api/v1/pembayaran/{id}/verifikasi` dengan `keputusan: "setuju" | "tolak"`, `nominal_dibayar`, `alasan`), termasuk membuka gambar bukti terenkripsi (`GET /api/v1/pembayaran?bukti_id=<id>`).
   - **Dual Control**: Jika bendahara ini adalah pembuat baris `pembayaran` tersebut, server menolak `403 DUAL_CONTROL_VIOLATION` agar diverifikasi oleh admin/bendahara lain.
   - Jika `nominal_dibayar != nominal_total`, pembayaran ditandai `perlu_tindak_lanjut`.
   - Jika disetujui (`setuju`) & nominal cocok -> pembayaran `lunas`, kwitansi terbit, tagihan iuran -> `lunas`, dan untuk pesanan: **stok produk langsung dipotong atomik** lalu pesanan -> `dibayar`.
6. Mengakses laporan lengkap & ekspor CSV (`GET /api/v1/laporan?jenis=anggota|tunggakan|penjualan|pembayaran`).

### 4.5 Role `admin`
*(Wajib MFA TOTP secara default)*
Memiliki seluruh kapabilitas `bendahara` dan `petugas_toko` (tetap tunduk pada **Dual Control** bila membuat pembayaran sendiri), ditambah kewenangan eksklusif:
1. Mengelola seluruh akun user & staf (`GET /api/v1/users`, `POST /api/v1/users`, `PATCH /api/v1/users/{id}`) — mengganti peran atau menonaktifkan user langsung mencabut seluruh sesi aktif user tersebut.
2. Meninjau antrean pendaftar (`GET /api/v1/pendaftaran`), mendekripsi dan melihat foto KYC (`GET /api/v1/pendaftaran/{id}/dokumen/{tipe}`), serta memverifikasi pendaftaran (`POST /api/v1/pendaftaran/{id}/verifikasi` dengan `disetujui` | `perlu_perbaikan` | `ditolak`). Persetujuan otomatis membuat record `users`, `anggota`, nomor anggota unik, dan `activation_code`.
3. Mengubah status keanggotaan (`PATCH /api/v1/anggota/{id}/status` menjadi `aktif` | `nonaktif` | `keluar`; status nonaktif otomatis mencabut sesi login), membuka data tersamar (`GET /api/v1/anggota/{id}?unmask=true`, tercatat di `audit_log`), dan impor CSV massal anggota (`POST /api/v1/anggota/impor`).

---

## Bagian 5: Arsitektur & Alur Database Lengkap (Seluruh 21 Tabel)

Seluruh skema berada pada [000001_init_kopdes_schema.up.sql](../db/migrations/000001_init_kopdes_schema.up.sql) dan [000002_decision_audit_enhancements.up.sql](../db/migrations/000002_decision_audit_enhancements.up.sql), memakai PK `UUID v7`, nilai uang `BIGINT` Rupiah bulat, dan dieksekusi dengan role non-superuser `kopdes_app`. Ringkasan lengkap hasil audit 10 keputusan desain tersedia di [DECISION_AUDIT.md](DECISION_AUDIT.md).

*(Selain 19 tabel inti berikut, migrasi `000002` menambahkan tabel **#20 `pengembalian_dana`** untuk alur refund manual dual-control D2 dan tabel **#21 `worker_runs`** untuk telemetry tugas terjadwal D8, kolom `produk.stok_tertahan`, `pesanan.stok_status`, `pesanan.stok_hold_expires_at`, `pembayaran.bukti_key_id`, `pembayaran.bukti_dek_wrapped`, serta `tanggal_lahir_enc` menjadi opsional/nullable.)*

### 5.1 Rincian 21 Tabel Berdasarkan Kategori

#### A. Konfigurasi, Identitas, Sesi, & Pembatasan Laju (5 Tabel)
1. **`pengaturan`**: Baris tunggal terkunci (`id = 1 CHECK (id = 1)`) menyimpan `nama_koperasi`, `alamat_koperasi`, `telepon_koperasi`, `qris_unique_digits` (`3` atau `4`), `ongkir_tetap` (`>= 0`), `kebijakan_teks`, `catatan_akuntansi`, dan FK `active_rekening_qris_id -> rekening_qris(id)`.
2. **`users`**: Seluruh akun login (`admin`, `bendahara`, `petugas_toko`, `anggota`).
   - *Kolom Terenkripsi (`BYTEA` AES-256-GCM+AAD)*: `email_enc`, `hp_enc`, `mfa_secret_enc`.
   - *Blind Index (`HMAC-SHA256`)*: `email_hash` (UNIQUE), `hp_hash` (UNIQUE) — dengan constraint minimal salah satu terisi.
   - *Autentikasi & Keamanan*: `nama_lengkap`, `password_hash` (`bcrypt`), `peran`, `status_aktif`, `mfa_enabled`, `mfa_last_step`, `mfa_recovery_hashes` (`TEXT[]`), `failed_login_count`, `locked_until`, `last_failed_at`.
3. **`sesi`**: Sesi *opaque* server-side (`user_id -> users(id) ON DELETE CASCADE`, `token_hash` SHA-256 UNIQUE, `mfa_verified`, `scope` `full|mfa_pending|mfa_setup`, `mfa_attempts`, `ip_address`, `user_agent`, `last_active_at`, `idle_expires_at`, `abs_expires_at`, `revoked_at`). Partial index `idx_sesi_user_aktif` untuk pencabutan sesi instan.
4. **`kode_aktivasi`**: Token aktivasi awal & reset password sekali pakai (`user_id -> users(id)`, `kode_hash` UNIQUE, `jenis` `aktivasi_awal|reset_password`, `expires_at`, `used_at`, `created_by`).
5. **`rate_limits`**: State pembatasan laju persisten di DB (`key` PK, `kategori`, `hit_count`, `window_reset_at`, `blocked_until`).

#### B. Anggota, Pendaftaran Publik, & Dokumen KYC (3 Tabel)
6. **`anggota`**: Profil anggota resmi koperasi.
   - *Terenkripsi (`BYTEA`)*: `nik_enc`, `hp_enc`, `tanggal_lahir_enc`, `alamat_enc`.
   - *Blind Index & Masking*: `nik_hash` (UNIQUE), `nik_key_version`, `nik_masked`, `hp_masked`.
   - *Data Operasional*: `user_id -> users(id)` (UNIQUE), `nomor_anggota` (UNIQUE), `nama_lengkap`, `status` (`aktif|nonaktif|keluar`), `alasan_status`, `tanggal_bergabung`.
7. **`pendaftaran`**: Antrean calon anggota publik.
   - *Terenkripsi (`BYTEA`)*: `email_enc`, `nik_enc`, `hp_enc`, `tanggal_lahir_enc`, `alamat_enc`.
   - *Blind Index & Masking*: `email_hash`, `nik_hash`, `nik_key_version`, `nik_masked`, `hp_hash`, `hp_masked`.
   - *Status & Token*: `pekerjaan`, `status` (`diajukan|perlu_perbaikan|disetujui|ditolak`), `catatan_verifikasi`, `verified_by`, `verified_at`, `rejected_at`, `anggota_id -> anggota(id)`, `upload_token_hash`, `ip_address`. Diindeks di `(status, created_at DESC)`.
8. **`dokumen_kyc`**: Metadata enkripsi berkas KYC pada disk `<STORAGE_KYC_DIR>/kyc/`.
   - *Kolom*: `pendaftaran_id -> pendaftaran(id) ON DELETE CASCADE`, `tipe_dokumen` (`ktp|kk|selfie_ktp`), `storage_key` (nama acak `<uuid>.enc` UNIQUE), `key_id`, `dek_wrapped` (`BYTEA`), `mime_type`, `ukuran_bytes`, `lebar_px`, `tinggi_px`, `sha256_plain`. Unique per `(pendaftaran_id, tipe_dokumen)`.

#### C. Iuran Bulanan (2 Tabel)
9. **`jenis_iuran`**: Definisi tarif iuran (`kode` UNIQUE, `nama`, `nominal > 0`, `hari_jatuh_tempo` 1..28, `klasifikasi_akuntansi` berlabel `'KEPUTUSAN AKUNTAN'`, `aktif`).
10. **`tagihan_iuran`**: Tagihan bulanan per anggota (`anggota_id -> anggota(id)`, `jenis_iuran_id -> jenis_iuran(id)`, `periode_tahun`, `periode_bulan`, `nominal`, `nominal_keringanan <= nominal`, `tanggal_jatuh_tempo`, `status` `belum_bayar|menunggu_verifikasi|lunas|dibebaskan|batal`, `alasan_keringanan`, `disetujui_oleh`, `paid_at`). Constraint `UNIQUE (anggota_id, jenis_iuran_id, periode_tahun, periode_bulan)` mencegah tagihan ganda saat generate ulang.

#### D. E-Katalog, Checkout, & Pengantaran (5 Tabel)
11. **`kategori`**: Kategori produk (`slug` UNIQUE, `nama`, `aktif`).
12. **`produk`**: Katalog barang (`kategori_id -> kategori(id)`, `sku` UNIQUE, `nama`, `deskripsi`, `satuan`, `harga_umum`, `harga_anggota`, `stok CHECK (stok >= 0)`, `foto_url`, `status` `aktif|nonaktif`).
13. **`promo`**: Voucher diskon (`kode` UNIQUE, `nama`, `tipe_diskon` `persen|nominal`, `nilai_diskon`, `min_belanja`, `kuota_total`, `kuota_terpakai <= kuota_total`, `sekali_per_anggota`, `mulai_pada`, `berakhir_pada`, `aktif`).
14. **`pesanan`**: Transaksi checkout tanpa keranjang server.
    - *Terenkripsi (`BYTEA`)*: `hp_penerima_enc`, `alamat_pengantaran_enc`.
    - *Plaintext*: `nomor_pesanan` UNIQUE, `user_id -> users(id)`, `anggota_id -> anggota(id)`, `promo_id -> promo(id)`, `idempotency_key` UNIQUE, `metode_pengiriman` (`diantar|ambil_sendiri`), `metode_pembayaran` (`qris|tunai`), `status` (`baru|menunggu_pembayaran|menunggu_verifikasi|dibayar|diproses|diantar|siap_diambil|selesai|batal`), `subtotal`, `diskon`, `ongkir`, `total_bayar`, `nama_penerima`, `hp_penerima_masked`, `catatan`, `tanggal_antar`, `expires_at`. Diindeks pada `idx_pesanan_daftar_antar`.
15. **`pesanan_item`**: Snapshot rincian item saat checkout (`pesanan_id -> pesanan(id) ON DELETE CASCADE`, `produk_id -> produk(id) ON DELETE RESTRICT`, `nama_produk`, `kuantitas`, `harga_satuan`, `subtotal`).

#### E. Pembayaran & Audit (4 Tabel + 1 Sequence)
16. **`rekening_qris`**: Daftar rekening penerima QRIS (`nama_merchant`, `nmid`, `nama_bank`, `nomor_rekening`, `nama_pemilik`, `jenis_pemilik` `badan_hukum|perorangan`, `qris_image_url`, `qris_payload`, `aktif`).
17. **`pembayaran`** (didukung sequence `seq_nomor_kwitansi`): Catatan instruksi pembayaran, kode unik, dan hasil verifikasi (`nomor_pembayaran` UNIQUE, `nomor_kwitansi` UNIQUE, `idempotency_key` UNIQUE, `pembayar_user_id -> users(id)`, `anggota_id -> anggota(id)`, `rekening_qris_id -> rekening_qris(id)`, `jenis_transaksi` `iuran|pesanan`, `metode` `qris|tunai`, `nominal_pokok`, `kode_unik`, `nominal_total`, `nominal_dibayar`, `selisih_kode_unik`, `catatan_akuntansi`, `status` `menunggu_pembayaran|menunggu_verifikasi|lunas|perlu_tindak_lanjut|ditolak|kedaluwarsa|batal`, `bukti_url`, `bukti_hash` UNIQUE anti-duplikat, `catatan_pembayar`, `alasan_verifikasi`, `dibuat_oleh`, `diverifikasi_oleh`, `verified_at`, `expires_at`). Dilindungi partial unique index **`idx_pembayaran_qris_unik_aktif`** pada `(rekening_qris_id, nominal_total) WHERE metode = 'qris' AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi')`.
18. **`pembayaran_alokasi`**: Relasi pemetaan 1 pembayaran ke 1..N `tagihan_iuran` **atau** 1 `pesanan`, dijaga constraint `chk_alokasi_target` sehingga tidak pernah kosong ataupun menunjuk iuran dan pesanan sekaligus dalam 1 baris.
19. **`audit_log`**: Catatan aksi kritis tanpa PII mentah (`actor_user_id`, `actor_peran`, `aksi`, `entitas`, `entitas_id`, `ringkasan`, `ip_address`, `created_at`). Dilindungi trigger **`trg_audit_log_immutable`** (`BEFORE UPDATE OR DELETE`) + pencabutan hak `UPDATE, DELETE, TRUNCATE` untuk `PUBLIC` maupun `kopdes_app`.
20. **`pengembalian_dana`** *(migrasi `000002`)*: Pengajuan & penyelesaian refund manual dual-control (`pembayaran_id`, `nominal`, `metode` `tunai|transfer`, `alasan`, `status` `diajukan|selesai|ditolak`, `bukti_url`, `bukti_hash`, `bukti_key_id`, `bukti_dek_wrapped`, `diajukan_oleh`, `dilaksanakan_oleh`, `breakglass`).
21. **`worker_runs`** *(migrasi `000002`)*: Telemetri eksekusi 5 tugas periodik worker bawaan (`job_name`, `last_started_at`, `last_finished_at`, `last_status`, `last_summary`, `last_error`, `run_count`) yang dibaca `/readyz`.

### 5.2 Mesin Status (State Machine) Utama
- **`pendaftaran`**: `diajukan` -> `perlu_perbaikan` | `disetujui` | `ditolak` (`perlu_perbaikan` dapat kembali ke `diajukan`).
- **`tagihan_iuran`**: `belum_bayar` -> `menunggu_verifikasi` -> `lunas`; atau `belum_bayar` -> `dibebaskan` | `batal`.
- **`pembayaran`**: `menunggu_pembayaran` -> `menunggu_verifikasi` -> `lunas` | `perlu_tindak_lanjut` | `ditolak`; atau `menunggu_pembayaran` -> `kedaluwarsa` | `batal`.
- **`pesanan`**:
  - Jalur normal QRIS: `menunggu_pembayaran` -> (`menunggu_verifikasi`) -> `dibayar` *(stok produk dipotong atomik saat verifikasi `setuju`)* -> `diproses` -> (`diantar` | `siap_diambil`) -> `selesai`.
  - Jalur langsung tunai di toko: `baru` -> `dibayar` / `diproses` *(stok produk dipotong saat transisi pertama ke status setelah-bayar)* -> `diantar` / `siap_diambil` -> `selesai`.
  - Pembatalan/kedaluwarsa: jika dibatalkan sebelum `dibayar`/`diproses`, hanya kuota promo yang dikembalikan; jika dibatalkan setelah `dibayar`/`diproses`, stok produk dan kuota promo keduanya dikembalikan.

### 5.3 Skema Enkripsi & Kriptografi Kolom Database & Disk

| Data Sensitif | Lokasi | Mekanisme & Konteks (`AAD` / Algoritma) |
| :--- | :--- | :--- |
| `users.email_enc`, `users.hp_enc`, `users.mfa_secret_enc` | PostgreSQL `BYTEA` | AES-256-GCM, AAD: `users:email:<uid>`, `users:hp:<uid>`, `users:mfa_secret:<uid>` |
| `anggota.*_enc` & `pendaftaran.*_enc` | PostgreSQL `BYTEA` | AES-256-GCM, AAD: `<tabel>:<kolom>:<record_id>` + dukungan `AES_PREVIOUS_KEYS` |
| `pesanan.hp_penerima_enc`, `pesanan.alamat_pengantaran_enc` | PostgreSQL `BYTEA` | AES-256-GCM, AAD: `pesanan:hp_penerima:<oid>`, `pesanan:alamat_pengantaran:<oid>` |
| Blind index (`email_hash`, `hp_hash`, `nik_hash`) | PostgreSQL `TEXT` | Kunci turunan `HKDF-SHA256` dari `BLIND_INDEX_SECRET` + `nik_key_version` |
| Berkas foto KYC (`dokumen_kyc`) | `<STORAGE_KYC_DIR>/*.enc` | *Envelope encryption* (`DEK` 256-bit acak dibungkus subkunci HKDF `kopdes:v1:kyc-files`), AAD: `dokumen_kyc:<tipe>:<pendaftaran_id>` |
| Berkas bukti bayar & refund (`pembayaran`, `pengembalian_dana`) | `<STORAGE_BUKTI_DIR>/*.enc` | *Envelope encryption* seragam (`DEK` acak dibungkus subkunci HKDF `kopdes:v1:bukti-files`), AAD: `pembayaran:bukti:<id>` / `pengembalian_dana:bukti:<id>` |
| Token sesi (`sesi.token_hash`) & kode aktivasi | PostgreSQL `TEXT` | Hash satu arah `SHA-256` dengan pembandingan *constant-time* |
| Kata sandi user (`users.password_hash`) | PostgreSQL `TEXT` | `bcrypt` (cost minimal `12` pada bootstrap/produksi; `>=10` default) |

---

## Bagian 6: Kepatuhan Hukum, Akuntansi, & Batasan Operasional

1. **Lingkup Koperasi Tunggal**: Sistem dilarang dipakai untuk multi-koperasi/multi-tenant, simpan-pinjam, SHU, pembukuan *double-entry*, maupun impor CSV mutasi bank.
2. **Retensi Dokumen Ditolak (`PERLU VERIFIKASI HUKUM`)**: CLI `kopdes pdp purge` menjalankan penghapusan keras (*hard delete*, bukan *crypto-shredding*) terhadap baris pendaftaran berstatus `ditolak` beserta berkas `.enc`-nya setelah melewati `PDP_REJECTED_RETENTION_DAYS` hari (default `30`). Angka dan tata cara penghapusan wajib dikonfirmasikan ke penasihat hukum koperasi.
3. **Klasifikasi Akuntansi (`KEPUTUSAN AKUNTAN`)**: Kolom `jenis_iuran.klasifikasi_akuntansi`, `pembayaran.catatan_akuntansi` (selisih kode unik QRIS), serta koreksi pembukuan `pengembalian_dana` wajib ditetapkan bersama akuntan koperasi sebelum tutup buku resmi.
4. **Backup & Pemulihan Bencana**: Skrip [backup.sh](../scripts/backup.sh) mengarsipkan `pg_dump` logis + `STORAGE_KYC_DIR` + `STORAGE_BUKTI_DIR` + `manifest.sha256` lalu mengenkripsi memakai kunci publik terautentikasi **`age`** (`BACKUP_AGE_RECIPIENT`; private key wajib off-server). Arsip WAL/PITR belum dibuat kecuali diminta pemilik.
5. **Rekening QRIS Pilot**: Variabel `ALLOW_PERSONAL_PAYMENT_PILOT` default `false` (**KEPUTUSAN PEMILIK** & **PERLU VERIFIKASI HUKUM**). Jika diaktifkan untuk uji coba rekening pribadi pengurus, batas maksimal transaksi & harian tetap dikunci server melalui `PILOT_MAX_PER_TX` dan `PILOT_MAX_PER_DAY`.
