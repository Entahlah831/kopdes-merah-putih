# Referensi Variabel Environment — Kopdes Merah Putih

> **Dokumen ini di-generate otomatis** dari struct `AppConfig` di `internal/config/config.go`.
> Total variabel: **51** (maksimal 51 variabel).

## 1. Pemisahan Konfigurasi: Environment vs Database (`pengaturan`)

| Jenis Pengaturan | Lokasi Penyimpanan | Contoh Parameter |
|---|---|---|
| Infrastruktur, Port, DB, Kunci AES/HMAC, Batas Sesi & Keamanan | Variabel Environment (`.env`) | `API_PORT`, `DB_*`, `AES_MASTER_KEY`, `BLIND_INDEX_SECRET`, `SESSION_*`, `MFA_REQUIRED_ROLES` |
| Profil & Kebijakan Operasional Koperasi Tunggal | Tabel DB `pengaturan` (1 baris, `id = 1`) | `nama_koperasi`, `alamat_koperasi`, `ongkir_tetap`, `qris_unique_digits`, `active_rekening_qris_id` |

## 2. Pemisahan Port Internal vs Host

| Peran Port | Variabel | Fungsi |
|---|---|---|
| `internal` | `API_PORT` | Port internal HTTP API di dalam proses/kontainer |
| `host` | `API_HOST_PORT` | Port host publik yang dipetakan ke API_PORT |
| `host` | `UI_PORT` | Port HTTP Frontend Tester lokal/uji (dilarang aktif di production) |
| `host` | `WORKER_PORT` | Port loopback healthcheck worker (0 = nonaktif) |
| `host` | `DB_PORT` | Port koneksi PostgreSQL yang dituju API/skrip |
| `host` | `DB_HOST_PORT` | Port host PostgreSQL yang dipetakan Compose ke kontainer |

## 3. Katalog Lengkap Variabel Environment

| NAMA | TIPE | DEFAULT AMAN | WAJIB DI | RAHASIA? | DIBACA OLEH | PENJELASAN | CONTOH NILAI SINTETIS |
|---|---|---|---|---|---|---|---|
| `APP_ENV` | `string` | `development` | `all` | Tidak | `api,db-init` | Lingkungan runtime (development|test|staging|production) | `test` |
| `APP_TIMEZONE` | `string` | `Asia/Jakarta` | `all` | Tidak | `api,Dockerfile` | Zona waktu operasional (perhitungan batas harian & log) | `Asia/Jakarta` |
| `API_PORT` | `int` | `8080` | `all` | Tidak | `api,Dockerfile,compose` | Port internal HTTP API di dalam proses/kontainer | `8080` |
| `API_HOST_PORT` | `int` | `8080` | `compose` | Tidak | `compose,uitester` | Port host publik yang dipetakan ke API_PORT | `8080` |
| `UI_PORT` | `int` | `8081` | `all` | Tidak | `uitester,compose` | Port HTTP Frontend Tester lokal/uji (dilarang aktif di production) | `8081` |
| `WORKER_PORT` | `int` | `8082` | `optional` | Tidak | `worker,compose,envcheck` | Port loopback healthcheck worker (0 = nonaktif) | `8082` |
| `WORKER_TICK_INTERVAL` | `duration` | `60s` | `optional` | Tidak | `worker,compose` | Interval tick penjadwalan worker latar | `60s` |
| `SLO_READ_P95_MS` | `int` | `300` | `optional` | Tidak | `loadtest,monitoring` | Target SLO latensi p95 endpoint baca (milidetik) | `300` |
| `SLO_WRITE_P95_MS` | `int` | `800` | `optional` | Tidak | `loadtest,monitoring` | Target SLO latensi p95 endpoint tulis (milidetik) | `800` |
| `SLO_MAX_ERROR_RATE` | `float64` | `0.005` | `optional` | Tidak | `loadtest,monitoring` | Batas rasio error maksimum SLO beban normal (mis. 0.005 = 0.5%) | `0.005` |
| `ALLOWED_ORIGINS` | `[]string` | _(kosong)_ | `prod,staging` | Tidak | `api` | Daftar origin CORS yang diizinkan (dipisah koma; dilarang * di production) | `http://127.0.0.1:8081` |
| `TRUSTED_PROXIES` | `[]string` | `127.0.0.1/32` | `prod` | Tidak | `api` | Daftar CIDR reverse proxy tepercaya untuk X-Forwarded-For | `127.0.0.1/32` |
| `DB_HOST` | `string` | `127.0.0.1` | `all` | Tidak | `api,scripts` | Hostname server PostgreSQL | `127.0.0.1` |
| `DB_PORT` | `int` | `5433` | `all` | Tidak | `api,scripts` | Port koneksi PostgreSQL yang dituju API/skrip | `5433` |
| `DB_HOST_PORT` | `int` | `5433` | `compose` | Tidak | `compose` | Port host PostgreSQL yang dipetakan Compose ke kontainer | `5433` |
| `DB_NAME` | `string` | `kopdes_db` | `all` | Tidak | `api,compose,scripts` | Nama database PostgreSQL (akhiran _test untuk DB uji) | `kopdes_test` |
| `DB_USER` | `string` | `kopdes_app` | `all` | Tidak | `api,db-init,scripts` | Role PostgreSQL non-superuser aplikasi | `kopdes_app` |
| `DB_PASSWORD` | `string` | _(kosong)_ | `all` | **Ya** | `api,db-init` | Kata sandi role DB_USER (kosong di .env.example) | `kopdes_test_setA_pass_123!` |
| `DB_SSLMODE` | `string` | `disable` | `all` | Tidak | `api,scripts` | Mode SSL PostgreSQL (require/verify-full di production) | `disable` |
| `POSTGRES_PASSWORD` | `string` | _(kosong)_ | `compose` | **Ya** | `compose` | Sandi bootstrap superuser postgres di kontainer (init only) | `pg_test_bootstrap_123!` |
| `AES_MASTER_KEY` | `string` | _(kosong)_ | `all` | **Ya** | `api` | Kunci AES-256-GCM enkripsi PII/KYC/bukti (opsional prefix ver:id=hex) | `v1:k1=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef` |
| `AES_PREVIOUS_KEYS` | `string` | _(kosong)_ | `optional` | **Ya** | `api` | Kunci AES historis format 'v1:k1=hex;v1:k2=hex' saat rotasi | _(opsional)_ |
| `BLIND_INDEX_SECRET` | `string` | _(kosong)_ | `all` | **Ya** | `api` | Rahasia root HKDF-SHA256 untuk blind index NIK/email/token | `abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789` |
| `SESSION_IDLE_TTL` | `duration` | `30m` | `all` | Tidak | `api,frontend` | Masa berlaku idle sesi opaque sebelum kedaluwarsa | `30m` |
| `SESSION_ABS_TTL` | `duration` | `12h` | `all` | Tidak | `api,frontend` | Masa berlaku absolut maksimum satu sesi login | `12h` |
| `COOKIE_SECURE` | `string` | `auto` | `all` | Tidak | `api,frontend` | Kebijakan atribut Secure cookie sesi (auto|true|false; produksi wajib auto/true) | `auto` |
| `ALLOW_BEARER_AUTH` | `bool` | `false` | `optional` | Tidak | `api,frontend` | Izinkan header Authorization Bearer untuk klien non-browser (default false di produksi) | `false` |
| `MFA_REQUIRED_ROLES` | `[]string` | `admin,bendahara` | `all` | Tidak | `api,frontend` | Daftar peran yang wajib MFA TOTP | `admin,bendahara` |
| `LOGIN_MAX_ATTEMPTS` | `int` | `5` | `all` | Tidak | `api` | Batas kegagalan login sebelum kunci sementara | `5` |
| `LOGIN_LOCKOUT_DURATION` | `duration` | `15m` | `all` | Tidak | `api` | Durasi penguncian login setelah batas percobaan terlampaui | `15m` |
| `CAPTCHA_ENABLED` | `bool` | `false` | `optional` | Tidak | `api,frontend` | Aktifkan verifikasi CAPTCHA Turnstile pada endpoint publik | `false` |
| `CAPTCHA_SITE_KEY` | `string` | _(kosong)_ | `optional` | Tidak | `api,frontend` | Public site key Turnstile untuk frontend | _(opsional)_ |
| `CAPTCHA_SECRET_KEY` | `string` | _(kosong)_ | `optional` | **Ya** | `api` | Secret key verifikasi server-side CAPTCHA Turnstile | _(opsional)_ |
| `PAYMENTS_ENABLED` | `bool` | `true` | `optional` | Tidak | `api,frontend` | Status fitur pembuatan & verifikasi pembayaran | `true` |
| `REGISTRATION_ENABLED` | `bool` | `true` | `optional` | Tidak | `api,frontend` | Status fitur pendaftaran anggota baru & unggah KYC | `true` |
| `ALLOW_PERSONAL_PAYMENT_PILOT` | `bool` | `false` | `optional` | Tidak | `api,frontend` | Izinkan rekening pilot perorangan terbatas batas nominal/harian | `false` |
| `PILOT_MAX_PER_TX` | `int64` | `500000` | `optional` | Tidak | `api` | Batas nominal per transaksi rekening pilot (Rupiah) | `500000` |
| `PILOT_MAX_PER_DAY` | `int64` | `2000000` | `optional` | Tidak | `api` | Batas nominal harian rekening pilot Asia/Jakarta (Rupiah) | `2000000` |
| `QRIS_UNIQUE_DIGITS` | `int` | `3` | `optional` | Tidak | `api,frontend` | Jumlah digit kode unik integer QRIS statis (3 atau 4) | `3` |
| `PAYMENT_EXPIRY_DURATION` | `duration` | `15m` | `optional` | Tidak | `api,frontend` | Durasi kedaluwarsa tagihan pembayaran QRIS menunggu bayar | `15m` |
| `STOCK_HOLD_MINUTES` | `int` | `60` | `optional` | Tidak | `api,cli` | Durasi tahan (reservasi) stok pesanan dalam menit mencakup batas bayar + SLA verifikasi (KEPUTUSAN PEMILIK) | `60` |
| `PENDING_LIMIT_PER_MEMBER` | `int` | `2` | `optional` | Tidak | `api` | Batas maksimal pembayaran QRIS berstatus menunggu per anggota | `2` |
| `PENDING_LIMIT_GLOBAL` | `int` | `50` | `optional` | Tidak | `api` | Batas global pembayaran QRIS aktif bersamaan untuk ruang kode unik | `50` |
| `DUAL_CONTROL_BREAKGLASS_ENABLED` | `bool` | `false` | `optional` | Tidak | `api` | Izinkan mode darurat break-glass admin ber-MFA untuk kendali ganda (KEPUTUSAN PEMILIK) | `false` |
| `PDP_REJECTED_RETENTION_DAYS` | `int` | `30` | `optional` | Tidak | `api,cli` | Retensi hari berkas pendaftaran ditolak sebelum purge (PERLU VERIFIKASI HUKUM) | `30` |
| `STORAGE_KYC_DIR` | `string` | `./storage/kyc` | `all` | Tidak | `api,cli,compose` | Direktori penyimpanan berkas KYC terenkripsi AES-256-GCM | `./storage/kyc` |
| `STORAGE_BUKTI_DIR` | `string` | `./storage/bukti` | `all` | Tidak | `api,cli,compose` | Direktori/volume penyimpanan berkas bukti bayar & refund terenkripsi terpisah dari KYC | `./storage/bukti` |
| `BUKTI_RETENTION_DAYS` | `int` | `365` | `optional` | Tidak | `api,cli` | Retensi hari berkas bukti bayar non-lunas sebelum purge (PERLU VERIFIKASI HUKUM) | `365` |
| `BACKUP_DIR` | `string` | `./backups` | `scripts` | Tidak | `scripts` | Direktori penyimpanan arsip backup PostgreSQL & berkas terenkripsi | `./backups` |
| `BACKUP_AGE_RECIPIENT` | `string` | _(kosong)_ | `prod` | Tidak | `scripts` | Public key penerima age (age1...) untuk enkripsi backup terautentikasi; private key WAJIB disimpan di luar server | `age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p` |
| `BACKUP_RETENTION_DAYS` | `int` | `30` | `scripts` | Tidak | `scripts` | Jumlah hari retensi arsip backup lokal | `30` |
