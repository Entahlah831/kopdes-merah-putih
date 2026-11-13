# Referensi Variabel Environment — Kopdes Merah Putih

> **Dokumen ini di-generate otomatis** dari tag struct `AppConfig` di `internal/config/config.go`.
> Jangan mengedit tabel secara manual; ubah tag struct `AppConfig` lalu jalankan generator.

## 1. Pemisahan Konfigurasi: Environment vs Database (`konfigurasi_aturan`)

| Jenis Pengaturan | Lokasi Penyimpanan | Contoh Parameter |
|---|---|---|
| Infrastruktur, Port, DSN, Kunci Kriptografi, Token, Batas Global API | Variabel Environment (`.env`) | `API_PORT`, `DB_*`, `JWT_SECRET`, `AES_MASTER_KEY`, `METRICS_TOKEN`, `BCRYPT_COST` |
| Kebijakan Bisnis per Koperasi / Tenant | Tabel Database `konfigurasi_aturan` (RLS per `koperasi_id`) | Ambang persetujuan ganda khusus tenant, `auto_approve_mutasi_enabled`, batas pilot koperasi, jadwal iuran |

Nilai seperti `PILOT_DEFAULT_MAX_PER_TX` atau `DUAL_APPROVAL_DEFAULT_THRESHOLD` di `.env` hanya berfungsi sebagai **nilai default sistem** ketika tenant belum menimpanya di tabel `konfigurasi_aturan`.

## 2. Pemisahan Port Internal vs Host

| Peran Port | Variabel | Fungsi |
|---|---|---|
| `internal` | `API_PORT` | Port internal HTTP API di dalam kontainer/proses |
| `host` | `API_HOST_PORT` | Port host publik yang dipetakan ke API_PORT |
| `internal` | `METRICS_PORT` | Port internal endpoint Prometheus /metrics (bila berbeda dari API_PORT) |
| `host` | `METRICS_HOST_PORT` | Port host untuk eksposur /metrics internal |
| `host` | `PROMETHEUS_HOST_PORT` | Port host layanan Prometheus opsional |
| `host` | `DB_PORT` | Port koneksi DB yang dituju klien/API di host atau jaringan |
| `internal` | `DB_CONTAINER_PORT` | Port internal PostgreSQL di dalam kontainer |
| `host` | `DB_HOST_PORT` | Port host PostgreSQL yang dipetakan ke DB_CONTAINER_PORT |
| `bind` | `DB_BIND_HOST` | Alamat bind host untuk port PostgreSQL (default loopback 127.0.0.1) |

## 3. Katalog Lengkap Variabel Environment

| NAMA | TIPE | DEFAULT AMAN | WAJIB DI | RAHASIA? | DIBACA OLEH | PENJELASAN | CONTOH NILAI SINTETIS |
|---|---|---|---|---|---|---|---|
| `APP_ENV` | `string` | `development` | `all` | Tidak | `api,db-init` | Lingkungan runtime (development|test|staging|production) | `test` |
| `APP_TIMEZONE` | `string` | `Asia/Jakarta` | `all` | Tidak | `api,Dockerfile` | Zona waktu operasional (perhitungan batas harian & log) | `Asia/Jakarta` |
| `API_PORT` | `int` | `8080` | `all` | Tidak | `api,Dockerfile,compose` | Port internal HTTP API di dalam kontainer/proses | `8080` |
| `API_HOST_PORT` | `int` | `8080` | `compose` | Tidak | `compose` | Port host publik yang dipetakan ke API_PORT | `8080` |
| `METRICS_PORT` | `int` | `9100` | `prod,compose` | Tidak | `api,compose` | Port internal endpoint Prometheus /metrics (bila berbeda dari API_PORT) | `9100` |
| `METRICS_HOST_PORT` | `int` | `9100` | `compose` | Tidak | `compose,prometheus` | Port host untuk eksposur /metrics internal | `9100` |
| `PROMETHEUS_HOST_PORT` | `int` | `9090` | `monitoring` | Tidak | `prometheus` | Port host layanan Prometheus opsional | `9090` |
| `ALLOWED_ORIGINS` | `[]string` | _(kosong)_ | `prod,staging` | Tidak | `api` | Daftar origin CORS yang diizinkan (dipisah koma; dilarang * di production) | `http://127.0.0.1:3000` |
| `TRUSTED_PROXIES` | `[]string` | `127.0.0.1/32` | `prod` | Tidak | `api` | Daftar CIDR reverse proxy tepercaya untuk X-Forwarded-For | `127.0.0.1/32` |
| `DB_HOST` | `string` | `127.0.0.1` | `all` | Tidak | `api,scripts` | Hostname server PostgreSQL 16 | `127.0.0.1` |
| `DB_PORT` | `int` | `5433` | `all` | Tidak | `api,scripts` | Port koneksi DB yang dituju klien/API di host atau jaringan | `5433` |
| `DB_CONTAINER_PORT` | `int` | `5432` | `compose` | Tidak | `compose` | Port internal PostgreSQL di dalam kontainer | `5432` |
| `DB_HOST_PORT` | `int` | `5433` | `compose` | Tidak | `compose` | Port host PostgreSQL yang dipetakan ke DB_CONTAINER_PORT | `5433` |
| `DB_BIND_HOST` | `string` | `127.0.0.1` | `compose` | Tidak | `compose` | Alamat bind host untuk port PostgreSQL (default loopback 127.0.0.1) | `127.0.0.1` |
| `DB_NAME` | `string` | `kopdes_db` | `all` | Tidak | `api,compose,scripts` | Nama database target PostgreSQL (akhiran _test untuk DB uji) | `kopdes_test` |
| `DB_USER` | `string` | `kopdes_app` | `all` | Tidak | `api,db-init,scripts` | Role PostgreSQL non-superuser aplikasi | `kopdes_app` |
| `DB_PASSWORD` | `string` | _(kosong)_ | `all` | **Ya** | `api,db-init` | Kata sandi role DB_USER (wajib diset, kosong di .env.example) | `kopdes_test_setA_pass_123!` |
| `DB_SSLMODE` | `string` | `disable` | `all` | Tidak | `api,scripts` | Mode SSL PostgreSQL (require/verify-full di production) | `disable` |
| `DATABASE_URL` | `string` | _(kosong)_ | `optional` | **Ya** | `api` | Opsional DSN lengkap; jika diset wajib konsisten dengan komponen DB_* | _(opsional)_ |
| `DB_APP_ROLE` | `string` | `kopdes_app` | `all` | Tidak | `api,db-init` | Nama role non-superuser untuk SET ROLE dan guard startup | `kopdes_app` |
| `POSTGRES_USER` | `string` | `postgres` | `compose` | Tidak | `compose` | User bootstrap kontainer PostgreSQL (init only) | `postgres` |
| `POSTGRES_PASSWORD` | `string` | _(kosong)_ | `compose` | **Ya** | `compose` | Sandi bootstrap superuser PostgreSQL kontainer (init only) | `pg_test_bootstrap_123!` |
| `KOPDES_APP_PASSWORD` | `string` | _(kosong)_ | `db-init` | **Ya** | `db-init` | Sandi role kopdes_app saat inisialisasi db/docker-init/01_roles.sql | `kopdes_test_setA_pass_123!` |
| `ALLOW_SUPERUSER_DB` | `bool` | `false` | `optional` | Tidak | `api` | Hanya untuk diagnosa lokal; wajib false di production | `false` |
| `DB_POOL_MAX_CONNS` | `int` | `25` | `optional` | Tidak | `api` | Batas maksimum koneksi pool pgx | `25` |
| `DB_POOL_MIN_CONNS` | `int` | `2` | `optional` | Tidak | `api` | Batas minimum koneksi pool pgx | `2` |
| `DB_STATEMENT_TIMEOUT` | `duration` | `15s` | `optional` | Tidak | `api` | Timeout eksekusi query PostgreSQL per statement | `15s` |
| `DB_LOCK_TIMEOUT` | `duration` | `5s` | `optional` | Tidak | `api` | Timeout menunggu kunci PostgreSQL | `5s` |
| `DB_IDLE_IN_TX_TIMEOUT` | `duration` | `30s` | `optional` | Tidak | `api` | Timeout transaksi idle PostgreSQL | `30s` |
| `JWT_SECRET` | `string` | _(kosong)_ | `all` | **Ya** | `api` | Kunci HMAC penandatangan JWT aktif (min 32 karakter) | `test_jwt_secret_setA_0123456789abcdef0123456789abcdef` |
| `JWT_PREVIOUS_SECRET` | `string` | _(kosong)_ | `optional` | **Ya** | `api` | Kunci JWT sebelumnya saat rotasi (hanya untuk verifikasi) | _(opsional)_ |
| `JWT_ALGORITHM` | `string` | `HS256` | `all` | Tidak | `api` | Algoritma HMAC JWT yang diizinkan (HS256|HS384|HS512) | `HS256` |
| `JWT_ISSUER` | `string` | `kopdes-merah-putih` | `all` | Tidak | `api` | Nilai klaim iss pada JWT | `kopdes-merah-putih` |
| `JWT_ACCESS_TTL` | `duration` | `15m` | `all` | Tidak | `api` | Masa berlaku access token JWT | `15m` |
| `JWT_REFRESH_TTL` | `duration` | `168h` | `all` | Tidak | `api` | Masa berlaku refresh token berotasi | `168h` |
| `AES_MASTER_KEY` | `string` | _(kosong)_ | `all` | **Ya** | `api` | Kunci utama AES-256-GCM enkripsi PII (min 32 byte/hex64) | `fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210` |
| `AES_KEY_VERSION` | `string` | `v1` | `all` | Tidak | `api` | Prefix versi kunci aktif untuk ciphertext AES-GCM | `v1` |
| `AES_KEY_ID` | `string` | `k1` | `all` | Tidak | `api` | ID kunci aktif untuk rotasi kunci AES-GCM | `k1` |
| `AES_PREVIOUS_KEYS` | `string` | _(kosong)_ | `optional` | **Ya** | `api` | Kunci AES lama format 'v1:k1=hex;v1:k2=hex' untuk dekripsi data historis saat rotasi | _(opsional)_ |
| `BLIND_INDEX_SECRET` | `string` | _(kosong)_ | `all` | **Ya** | `api` | Rahasia root HKDF-SHA256 untuk blind index NIK/email | `abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789` |
| `QRIS_SECRET` | `string` | _(kosong)_ | `all` | **Ya** | `api` | Kunci HMAC penandatanganan payload QRIS internal | `11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff` |
| `METRICS_TOKEN` | `string` | _(kosong)_ | `all` | **Ya** | `api,prometheus` | Bearer token wajib (>= 32 byte) untuk endpoint /metrics | `metrics_token_setA_0123456789abcdef0123456789abcdef` |
| `METRICS_PREVIOUS_TOKEN` | `string` | _(kosong)_ | `optional` | **Ya** | `api` | Token /metrics sebelumnya selama transisi rotasi | _(opsional)_ |
| `BCRYPT_COST` | `int` | `12` | `all` | Tidak | `api` | Biaya komputasi bcrypt hashing kata sandi (min 10 di prod) | `12` |
| `MFA_ISSUER` | `string` | `Kopdes Merah Putih` | `all` | Tidak | `api` | Nama issuer yang tampil pada aplikasi autentikator TOTP | `Kopdes Merah Putih` |
| `MFA_MAX_ATTEMPTS` | `int` | `3` | `all` | Tidak | `api` | Batas percobaan salah per mfa_session_token | `3` |
| `MFA_SESSION_TTL` | `duration` | `5m` | `all` | Tidak | `api` | Masa berlaku sesi tantangan MFA setelah password valid | `5m` |
| `MFA_SETUP_TTL` | `duration` | `10m` | `all` | Tidak | `api` | Masa berlaku token pendaftaran MFA akun istimewa | `10m` |
| `LOGIN_MAX_ATTEMPTS` | `int` | `5` | `all` | Tidak | `api` | Batas kegagalan login sebelum IP/akun dikunci sementara | `5` |
| `LOGIN_LOCKOUT_DURATION` | `duration` | `15m` | `all` | Tidak | `api` | Durasi penguncian login setelah batas terlampaui | `15m` |
| `RATE_LIMIT_RPS` | `float64` | `30` | `optional` | Tidak | `api` | Token bucket rate limit request per detik per IP | `30` |
| `RATE_LIMIT_BURST` | `int` | `60` | `optional` | Tidak | `api` | Burst maksimum rate limiter HTTP per IP | `60` |
| `RATE_LIMIT_MAX_CLIENTS` | `int` | `10000` | `optional` | Tidak | `api` | Batas entri memori pelacak rate limiter | `10000` |
| `MAX_BODY_BYTES` | `int64` | `10485760` | `optional` | Tidak | `api` | Batas ukuran request body HTTP (default 10MB) | `10485760` |
| `HTTP_READ_HEADER_TIMEOUT` | `duration` | `5s` | `optional` | Tidak | `api` | Timeout membaca header HTTP (anti-Slowloris) | `5s` |
| `HTTP_READ_TIMEOUT` | `duration` | `10s` | `optional` | Tidak | `api` | Timeout membaca seluruh request HTTP | `10s` |
| `HTTP_WRITE_TIMEOUT` | `duration` | `15s` | `optional` | Tidak | `api` | Timeout menulis respons HTTP | `15s` |
| `HTTP_IDLE_TIMEOUT` | `duration` | `60s` | `optional` | Tidak | `api` | Timeout koneksi HTTP keep-alive idle | `60s` |
| `PARTITION_MONTHS_AHEAD` | `int` | `3` | `optional` | Tidak | `api` | Jumlah bulan partisi ke depan yang dijaga selalu tersedia | `3` |
| `PARTITION_JOB_INTERVAL` | `duration` | `24h` | `optional` | Tidak | `api` | Interval eksekusi job pemeliharaan partisi bulanan | `24h` |
| `PDP_REJECTED_RETENTION_DAYS` | `int` | `30` | `optional` | Tidak | `api` | Retensi hari berkas pendaftaran ditolak sebelum purge (PERLU VERIFIKASI HUKUM) | `30` |
| `PDP_PURGE_INTERVAL` | `duration` | `24h` | `optional` | Tidak | `api` | Interval job pembersihan pendaftaran ditolak | `24h` |
| `QRIS_DYNAMIC_ENABLED` | `bool` | `false` | `optional` | Tidak | `api` | Feature flag QRIS dinamis (false = QRIS statis + kode unik) | `false` |
| `ALLOW_PERSONAL_PAYMENT_PILOT` | `bool` | `false` | `optional` | Tidak | `api` | Mengizinkan rekening pilot perorangan terbatas dengan batas harian | `false` |
| `PILOT_DEFAULT_MAX_PER_TX` | `int64` | `500000` | `optional` | Tidak | `api` | Batas nominal default per transaksi rekening pilot (Rupiah) | `500000` |
| `PILOT_DEFAULT_MAX_PER_DAY` | `int64` | `2000000` | `optional` | Tidak | `api` | Batas nominal akumulasi harian rekening pilot Asia/Jakarta (Rupiah) | `2000000` |
| `DUAL_APPROVAL_DEFAULT_THRESHOLD` | `int64` | `500000` | `optional` | Tidak | `api` | Ambang nominal default kewajiban persetujuan ganda (Rupiah) | `500000` |
| `STORAGE_KYC_DIR` | `string` | `./storage/kyc` | `all` | Tidak | `api,compose` | Direktori penyimpanan berkas KYC terenkripsi/lokal | `./storage/kyc` |
| `MAX_KYC_UPLOAD_BYTES` | `int64` | `5242880` | `optional` | Tidak | `api` | Batas ukuran maksimal satu berkas KYC (byte) | `5242880` |
| `BACKUP_DIR` | `string` | `./backups` | `scripts` | Tidak | `scripts` | Direktori penyimpanan hasil backup pg_dump/basebackup | `./backups` |
| `BACKUP_ENCRYPTION_KEY` | `string` | _(kosong)_ | `prod` | **Ya** | `scripts` | Passphrase OpenSSL AES-256-CBC untuk enkripsi arsip backup | `test_backup_key_setA_9876543210!` |
| `BACKUP_RETENTION_DAYS` | `int` | `30` | `scripts` | Tidak | `scripts` | Jumlah hari retensi file backup lokal | `30` |
| `NOTIFIER_PROVIDER` | `string` | `noop` | `optional` | Tidak | `api` | Adapter pengirim notifikasi outbox (noop|webhook) | `noop` |
| `WHATSAPP_API_KEY` | `string` | _(kosong)_ | `optional` | **Ya** | `api` | Kunci API gateway WhatsApp opsional untuk worker outbox | _(opsional)_ |
| `SMS_API_KEY` | `string` | _(kosong)_ | `optional` | **Ya** | `api` | Kunci API gateway SMS opsional untuk worker outbox | _(opsional)_ |
| `WEBHOOK_SIGNING_SECRET` | `string` | _(kosong)_ | `optional` | **Ya** | `api` | Rahasia HMAC-SHA256 penandatangan request outbox webhook | _(opsional)_ |
| `ENV_FILE` | `string` | `.env` | `optional` | Tidak | `api,scripts` | Path berkas .env fallback untuk binary server dan skrip operasional | `.env` |
| `TEST_ENV_FILE` | `string` | `.env.test` | `optional` | Tidak | `tests` | Berkas konfigurasi sintetis yang dipilih suite pengujian (Set A .env.test / Set B .env.test.alt) | `.env.test` |
| `TEST_DATABASE_URL` | `string` | _(kosong)_ | `test` | **Ya** | `tests` | DSN khusus suite tes Go (wajib berakhiran *_test) | `postgres://kopdes_app:kopdes_test_setA_pass_123!@127.0.0.1:5433/kopdes_test?sslmode=disable` |
