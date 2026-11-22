# Referensi Variabel Environment — Kopdes Merah Putih

> **Dokumen ini di-generate otomatis** dari struct `AppConfig` di `internal/config/config.go`.
> Total variabel: **44** (target ramping <= 45 variabel).

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
| `host` | `UI_PORT` | Port HTTP Frontend Tester/Wireframe lokal (dilarang aktif di production) |
| `host` | `WORKER_PORT` | Port loopback healthcheck worker (0 = nonaktif) |
| `host` | `DB_PORT` | Port koneksi PostgreSQL yang dituju API/skrip |
| `host` | `DB_HOST_PORT` | Port host PostgreSQL yang dipetakan Compose ke kontainer |

## 3. Variabel yang Dihapus untuk Ramping (51 -> 44 Variabel <= 45)

| Variabel Dihapus | Nilai Bawaan Aman / Pengganti | Alasan Penghapusan |
|---|---|---|
| `SLO_READ_P95_MS` | Default `300` ms (`tools/loadtest`, `tests/perf/k6_mixed.js`) | Tidak dibaca biner API/worker produksi; cukup diatur perkakas uji |
| `SLO_WRITE_P95_MS` | Default `800` ms (`tools/loadtest`, `tests/perf/k6_mixed.js`) | Tidak dibaca biner API/worker produksi; cukup diatur perkakas uji |
| `SLO_MAX_ERROR_RATE` | Default `0.005` (`tools/loadtest`, `tests/perf/k6_mixed.js`) | Tidak dibaca biner API/worker produksi; cukup diatur perkakas uji |
| `WORKER_TICK_INTERVAL` | Default aman `60s` (`kopdes worker --tick`) | Sudah tersedia flag CLI `--tick`; tidak perlu menuhankan `.env` |
| `LOGIN_MAX_ATTEMPTS` | Default OWASP `5` di `api.DefaultConfig()` | Batas brute-force standar cukup dikunci aman pada kode |
| `LOGIN_LOCKOUT_DURATION` | Default OWASP `15m` di `api.DefaultConfig()` | Durasi lockout standar cukup dikunci aman pada kode |
| `PENDING_LIMIT_PER_MEMBER` | Default aman `2` di `api.DefaultConfig()` | Kebijakan internal aman koperasi tunggal tanpa perlu knob opsional |
| `PENDING_LIMIT_GLOBAL` | Default aman `50` di `api.DefaultConfig()` | Kapasitas aman ruang kode unik (3/4 digit) di `api.DefaultConfig()` |

## 4. Katalog Lengkap 44 Variabel Environment & Alasan Tertulis

| NAMA | TIPE | DEFAULT AMAN | WAJIB DI | RAHASIA? | DIBACA OLEH | PENJELASAN | ALASAN DIPERTAHANKAN | CONTOH NILAI SINTETIS |
|---|---|---|---|---|---|---|---|---|
| `APP_ENV` | `string` | `development` | `all` | Tidak | `api,db-init` | Lingkungan runtime (development|test|staging|production) | Membedakan guard ketat produksi (SSL, CORS non-wildcard, age) vs test/dev | `test` |
| `APP_TIMEZONE` | `string` | `Asia/Jakarta` | `all` | Tidak | `api,Dockerfile` | Zona waktu operasional (perhitungan batas harian & log) | Menyelaraskan batas harian pilot, laporan, dan timestamp kontainer dengan zona koperasi | `Asia/Jakarta` |
| `API_PORT` | `int` | `8080` | `all` | Tidak | `api,Dockerfile,compose` | Port internal HTTP API di dalam proses/kontainer | Mengikat listener API tanpa literal port agar portabel antar lingkungan | `8080` |
| `API_HOST_PORT` | `int` | `8080` | `compose` | Tidak | `compose,uitester` | Port host publik yang dipetakan ke API_PORT | Memetakan port host Docker Compose & target API uitester secara independen dari port kontainer | `8080` |
| `UI_PORT` | `int` | `8081` | `all` | Tidak | `uitester,compose` | Port HTTP Frontend Tester/Wireframe lokal (dilarang aktif di production) | Memisahkan port server UI uji agar Set A/Set B/Set C tidak bertabrakan | `8081` |
| `WORKER_PORT` | `int` | `8082` | `optional` | Tidak | `worker,compose,envcheck` | Port loopback healthcheck worker (0 = nonaktif) | Menyediakan /healthz proses kopdes worker terpantau Compose tanpa bentrok port | `8082` |
| `ALLOWED_ORIGINS` | `[]string` | _(kosong)_ | `prod,staging` | Tidak | `api` | Daftar origin CORS yang diizinkan (dipisah koma; dilarang * di production) | Membatasi sumber browser sah untuk cookie HttpOnly + CSRFGuard di tiap VPS | `http://127.0.0.1:8081` |
| `TRUSTED_PROXIES` | `[]string` | `127.0.0.1/32` | `prod` | Tidak | `api` | Daftar CIDR reverse proxy tepercaya untuk X-Forwarded-For | Mencegah spoofing IP limiter & memercayai X-Forwarded-Proto hanya dari Caddy/Cloudflare | `127.0.0.1/32` |
| `RATE_LIMIT_RPS` | `float64` | `30` | `optional` | Tidak | `api` | Batas request per detik global IPRateLimiter per IP (default 30 rps; produksi 1..120 rps, kuota hybridLimit per jam tetap ketat) | Mengatur batas laju global IPRateLimiter dari satu .env tanpa melemahkan hybridLimit pendaftaran/KYC | `30` |
| `DB_HOST` | `string` | `127.0.0.1` | `all` | Tidak | `api,scripts` | Hostname server PostgreSQL | Menghubungkan biner lokal (127.0.0.1) maupun kontainer Compose (postgres) | `127.0.0.1` |
| `DB_PORT` | `int` | `5433` | `all` | Tidak | `api,scripts` | Port koneksi PostgreSQL yang dituju API/skrip | Portabilitas port DB non-standar antar stack uji & produksi tanpa ubah kode | `5433` |
| `DB_HOST_PORT` | `int` | `5433` | `compose` | Tidak | `compose` | Port host PostgreSQL yang dipetakan Compose ke kontainer | Mengisolasi port binding PostgreSQL pada 127.0.0.1 host Docker | `5433` |
| `DB_NAME` | `string` | `kopdes_db` | `all` | Tidak | `api,compose,scripts` | Nama database PostgreSQL (akhiran _test untuk DB uji) | Memaksa guard seeder *_test serta memisahkan DB Set A/Set B/staging/produksi | `kopdes_test` |
| `DB_USER` | `string` | `kopdes_app` | `all` | Tidak | `api,db-init,scripts` | Role PostgreSQL non-superuser aplikasi | Menegakkan prinsip least-privilege role non-superuser pada runtime API/worker | `kopdes_app` |
| `DB_PASSWORD` | `string` | _(kosong)_ | `all` | **Ya** | `api,db-init` | Kata sandi role DB_USER (kosong di .env.example) | Kredensial rahasia DB per lingkungan yang dilarang masuk repositori | `kopdes_test_setA_pass_123!` |
| `DB_SSLMODE` | `string` | `disable` | `all` | Tidak | `api,scripts` | Mode SSL PostgreSQL (require/verify-full di production) | Wajib require/verify-full pada profil produksi sekaligus mengizinkan disable pada CI/test lokal | `disable` |
| `POSTGRES_PASSWORD` | `string` | _(kosong)_ | `compose` | **Ya** | `compose` | Sandi bootstrap superuser/migrator Postgres kontainer (init only) | Kredensial inisialisasi postgres:16-alpine yang terpisah dari role runtime kopdes_app | `pg_test_bootstrap_123!` |
| `AES_MASTER_KEY` | `string` | _(kosong)_ | `all` | **Ya** | `api` | Kunci AES-256-GCM enkripsi PII/KYC/bukti (opsional prefix ver:id=hex) | Kunci root HKDF-SHA256 enkripsi kolom PII, KYC, dan bukti pembayaran | `v1:k1=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef` |
| `AES_PREVIOUS_KEYS` | `string` | _(kosong)_ | `optional` | **Ya** | `api` | Kunci AES historis format 'v1:k1=hex;v1:k2=hex' saat rotasi | Memungkinkan dekripsi data lama selama rotasi kunci tanpa downtime | _(opsional)_ |
| `BLIND_INDEX_SECRET` | `string` | _(kosong)_ | `all` | **Ya** | `api` | Rahasia root HKDF-SHA256 untuk blind index NIK/email/token | Kunci terpisah dari AES untuk pencocokan NIK/email/token anti-lookup-table | `abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789` |
| `SESSION_IDLE_TTL` | `duration` | `30m` | `all` | Tidak | `api,frontend` | Masa berlaku idle sesi opaque sebelum kedaluwarsa | Batas idle kebijakan keamanan koperasi yang disinkronkan pula ke frontend via /public/config | `30m` |
| `SESSION_ABS_TTL` | `duration` | `12h` | `all` | Tidak | `api,frontend` | Masa berlaku absolut maksimum satu sesi login | Menetapkan masa hidup sesi maksimal agar staf/anggota login ulang secara berkala | `12h` |
| `COOKIE_SECURE` | `string` | `auto` | `all` | Tidak | `api,frontend` | Kebijakan atribut Secure cookie sesi (auto|true|false; produksi wajib auto/true) | Mengaktifkan __Host-kopdes_session pada HTTPS sambil mendukung dev HTTP lokal | `auto` |
| `ALLOW_BEARER_AUTH` | `bool` | `false` | `optional` | Tidak | `api,frontend` | Izinkan header Authorization Bearer untuk klien non-browser (default false di produksi) | Menutup permukaan Bearer token di browser produksi sambil mengizinkan CLI/loadtest terkonfigurasi | `false` |
| `MFA_REQUIRED_ROLES` | `[]string` | `admin,bendahara` | `all` | Tidak | `api,frontend` | Daftar peran yang wajib MFA TOTP | Memungkinkan perluasan kewajiban TOTP (mis. menambah petugas_toko) tanpa rebuild biner | `admin,bendahara` |
| `CAPTCHA_ENABLED` | `bool` | `false` | `optional` | Tidak | `api,frontend` | Aktifkan verifikasi CAPTCHA Turnstile pada endpoint publik | Saklar proteksi bot Turnstile antara lokal/CI vs produksi publik | `false` |
| `CAPTCHA_SITE_KEY` | `string` | _(kosong)_ | `optional` | Tidak | `api,frontend` | Public site key Turnstile untuk frontend | Kunci publik Cloudflare Turnstile spesifik domain koperasi | _(opsional)_ |
| `CAPTCHA_SECRET_KEY` | `string` | _(kosong)_ | `optional` | **Ya** | `api` | Secret key verifikasi server-side CAPTCHA Turnstile | Rahasia verifikasi Turnstile di sisi server yang wajib di luar kode | _(opsional)_ |
| `PAYMENTS_ENABLED` | `bool` | `true` | `optional` | Tidak | `api,frontend` | Status fitur pembuatan & verifikasi pembayaran | Kill-switch cepat transaksi pembayaran jika ada insiden operasional/rekening | `true` |
| `REGISTRATION_ENABLED` | `bool` | `true` | `optional` | Tidak | `api,frontend` | Status fitur pendaftaran anggota baru & unggah KYC | Kill-switch pendaftaran publik saat koperasi menutup rekrutmen sementara | `true` |
| `ALLOW_PERSONAL_PAYMENT_PILOT` | `bool` | `false` | `optional` | Tidak | `api,frontend` | Izinkan rekening pilot perorangan terbatas batas nominal/harian | Pembuka darurat rekening pilot sebelum QRIS badan hukum terbit (KEPUTUSAN PEMILIK) | `false` |
| `PILOT_MAX_PER_TX` | `int64` | `500000` | `optional` | Tidak | `api` | Batas nominal per transaksi rekening pilot (Rupiah) | Plafon per transaksi untuk mengendalikan risiko rekening pilot perorangan | `500000` |
| `PILOT_MAX_PER_DAY` | `int64` | `2000000` | `optional` | Tidak | `api` | Batas nominal harian rekening pilot Asia/Jakarta (Rupiah) | Plafon akumulasi harian rekening pilot Asia/Jakarta | `2000000` |
| `QRIS_UNIQUE_DIGITS` | `int` | `3` | `optional` | Tidak | `api,frontend` | Jumlah digit kode unik integer QRIS statis (3 atau 4) | Menyesuaikan kapasitas antrean kode unik QRIS statis (3 vs 4 digit) milik koperasi | `3` |
| `PAYMENT_EXPIRY_DURATION` | `duration` | `15m` | `optional` | Tidak | `api,frontend` | Durasi kedaluwarsa tagihan pembayaran QRIS menunggu bayar | Menentukan TTL pembayaran & countdown UI sebelum slot kode unik dilepas worker | `15m` |
| `STOCK_HOLD_MINUTES` | `int` | `60` | `optional` | Tidak | `api,cli` | Durasi tahan (reservasi) stok pesanan dalam menit mencakup batas bayar + SLA verifikasi (KEPUTUSAN PEMILIK) | Durasi reservasi stok sebelum dilepas worker (KEPUTUSAN PEMILIK sesuai jam layanan) | `60` |
| `DUAL_CONTROL_BREAKGLASS_ENABLED` | `bool` | `false` | `optional` | Tidak | `api` | Izinkan mode darurat break-glass admin ber-MFA untuk kendali ganda (KEPUTUSAN PEMILIK) | Kontrol darurat 1 admin bila bendahara berhalangan (KEPUTUSAN PEMILIK) | `false` |
| `PDP_REJECTED_RETENTION_DAYS` | `int` | `30` | `optional` | Tidak | `api,cli` | Retensi hari berkas pendaftaran ditolak sebelum purge (PERLU VERIFIKASI HUKUM) | Batas simpan dokumen pendaftar ditolak untuk worker pdp_purge (PERLU VERIFIKASI HUKUM) | `30` |
| `STORAGE_KYC_DIR` | `string` | `./storage/kyc` | `all` | Tidak | `api,cli,compose` | Direktori penyimpanan berkas KYC terenkripsi AES-256-GCM | Memisahkan path/volume blob KYC terenkripsi antara Set A/Set B/kontainer | `./storage/kyc` |
| `STORAGE_BUKTI_DIR` | `string` | `./storage/bukti` | `all` | Tidak | `api,cli,compose` | Direktori/volume penyimpanan berkas bukti bayar & refund terenkripsi terpisah dari KYC | Memisahkan path/volume bukti bayar dari KYC sesuai isolasi ruang simpan | `./storage/bukti` |
| `BUKTI_RETENTION_DAYS` | `int` | `365` | `optional` | Tidak | `api,cli` | Retensi hari berkas bukti bayar non-lunas sebelum purge (PERLU VERIFIKASI HUKUM) | Masa retensi bukti non-lunas sebelum dibersihkan worker bukti_purge (PERLU VERIFIKASI HUKUM) | `365` |
| `BACKUP_DIR` | `string` | `./backups` | `scripts` | Tidak | `scripts` | Direktori penyimpanan arsip backup PostgreSQL & berkas terenkripsi | Menentukan direktori target backup.sh / restore_drill.sh agar terisolasi per lingkungan | `./backups` |
| `BACKUP_AGE_RECIPIENT` | `string` | _(kosong)_ | `prod` | Tidak | `scripts` | Public key penerima age (age1...) untuk enkripsi backup terautentikasi; private key WAJIB disimpan di luar server | Menghindari penempatan private key di VPS dengan enkripsi asimetris age | `age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p` |
| `BACKUP_RETENTION_DAYS` | `int` | `30` | `scripts` | Tidak | `scripts` | Jumlah hari retensi arsip backup lokal | Mengatur jumlah hari arsip .tar.age sebelum rotasi otomatis oleh backup.sh | `30` |
