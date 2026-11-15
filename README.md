# Kopdes Merah Putih — Backend API Ramping (Koperasi Tunggal)

Backend API operasional berbasis **Go (`net/http` + `sqlc` + `pgx/v5`)** dan **PostgreSQL 16** yang dirancang khusus untuk **1 koperasi desa di 1 lokasi** (keanggotaan, pendaftaran + KYC terenkripsi, iuran bulanan QRIS statis/tunai, e-katalog + checkout tanpa keranjang server, daftar pengantaran, pengaturan koperasi, serta laporan CSV aman).

> **Kebijakan Konfigurasi & Keamanan (`AGENTS.md`)**:
> Dilarang menyimpan nilai `.env` asli, kunci enkripsi, kata sandi produksi, ataupun nomor port secara statis di kode sumber, berkas HTML/JS, maupun dokumentasi. Seluruh nilai konfigurasi dikontrol melalui 40 variabel env tervalidasi oleh `cmd/envcheck` (templat: [.env.example](.env.example); profil uji sintetis: [.env.test](.env.test) dan [.env.test.alt](.env.test.alt)).

---

## Ringkasan Arsitektur Ramping

| Komponen | Spesifikasi Aktual |
| :--- | :--- |
| **Runtime & Bahasa** | Go `1.27.4`, `PostgreSQL 16`, `sqlc` |
| **Dependensi Langsung (`go.mod`)** | `github.com/google/uuid`, `github.com/jackc/pgx/v5`, `golang.org/x/crypto` |
| **Skema Basis Data** | **19 tabel** + 1 sequence dalam 1 migrasi awal ([000001_init_kopdes_schema.up.sql](db/migrations/000001_init_kopdes_schema.up.sql)) |
| **Permukaan API** | **35 endpoint HTTP** terdaftar di [routes.go](internal/api/routes.go) & [openapi.yaml](docs/openapi.yaml) |
| **Variabel Lingkungan** | Tepat **40 variabel** didefinisikan di [config.go](internal/config/config.go) |
| **Role Aplikasi DB** | Non-superuser `kopdes_app` (`NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS`) |
| **Penyimpanan Berkas Terenkripsi** | Terpisah otomatis di `<STORAGE_KYC_DIR>/kyc/*.enc` (KYC envelope AES-256-GCM) dan `<STORAGE_KYC_DIR>/bukti/bukti_<id>.enc` (bukti bayar AES-256-GCM + AAD) |
| **Pemotongan Stok Pesanan** | Validasi stok saat `Checkout`, pemotongan stok saat verifikasi pembayaran disetujui (`setuju`) oleh `admin`/`bendahara` atau saat pesanan tunai diproses (`dibayar`/`diproses`) |

---

## Peta Struktur Repositori

```text
kopdes-merah-putih/
├── cmd/
│   ├── server/              # Server HTTP API utama (kopdes-api)
│   ├── kopdes/              # CLI operasional: admin create, iuran generate, pesanan expire, pdp purge
│   ├── envcheck/            # Validator ketat 40 variabel .env & konflik port
│   ├── seed_test_accounts/  # Seeder akun uji sintetis (khusus database *_test)
│   └── uitester/            # Server Frontend Tester polos non-produksi
├── internal/
│   ├── api/                 # Server HTTP, routes.go, middleware CORS/rate-limit/security-headers/session, E2E tests
│   ├── auth/                # Login, sesi opaque server-side, MFA TOTP 2-langkah, pengaturan, users
│   ├── pendaftaran/         # Pendaftaran publik, validasi & enkripsi berkas KYC .enc, verifikasi admin
│   ├── anggota/             # Profil anggota, masking/unmasking NIK & HP, suspend + pencabutan sesi, impor CSV
│   ├── iuran/               # Jenis iuran, generate tagihan bulanan idempoten, keringanan nominal
│   ├── katalog/             # Kategori, produk (harga_umum vs harga_anggota), promo berkuota
│   ├── pesanan/             # Checkout idempoten tanpa keranjang server, status pesanan, daftar antar CSV/label
│   ├── pembayaran/          # QRIS statis (+ kode unik integer 3/4 digit), tunai, bukti .enc, dual control
│   ├── laporan/             # Laporan anggota, tunggakan, penjualan, pembayaran (JSON & CSV aman)
│   ├── config/              # Loader & validator ketat AppConfig (40 variabel)
│   ├── db/                  # Kode Go hasil generasi sqlc & pemeriksaan role DB non-superuser
│   ├── security/            # AES-256-GCM + AAD, envelope encryption, HKDF blind index, TOTP, CSV neutralization
│   └── testseed/            # Helper seed sintetis terisolasi untuk pengujian otomatis
├── db/
│   ├── docker-init/         # 01_roles.sql (kopdes_app) & 02_migrations.sh
│   └── migrations/          # 000001_init_kopdes_schema.up.sql & .down.sql
├── docs/                    # Dokumentasi lengkap backend, frontend, OpenAPI, RBAC, env, dan laporan audit
├── frontend-tester/         # Aplikasi uji HTML/JS polos (tanpa framework, build step, maupun CDN)
├── scripts/                 # promote_env.{sh,ps1}, port_check.{sh,ps1}, backup.sh, restore_drill.sh
├── Dockerfile               # Multi-stage build minimal & container non-root (kopdesuser)
└── docker-compose.yml       # Orkestrasi PostgreSQL 16 + API dengan volume pg_data & kyc_storage
```

---

## Panduan Cepat Menjalankan

### 1. Mode Uji / Pengembangan Lokal
Pastikan PostgreSQL 16 sudah berjalan pada `DB_PORT` sesuai `.env.test`, lalu jalankan inisialisasi schema, seeder, API server, dan Frontend Tester:

```bash
# 1. Validasi konfigurasi 40 variabel & cek konflik port
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test

# 2. Inisialisasi role kopdes_app & skema 19 tabel ke database uji
psql -h "${DB_HOST}" -p "${DB_PORT}" -U postgres -d "${DB_NAME}" -v ON_ERROR_STOP=1 -f db/docker-init/01_roles.sql
psql -h "${DB_HOST}" -p "${DB_PORT}" -U postgres -d "${DB_NAME}" -v ON_ERROR_STOP=1 -f db/migrations/000001_init_kopdes_schema.up.sql

# 3. Isi akun uji sintetis (khusus database *_test)
go run ./cmd/seed_test_accounts --env-file .env.test

# 4. Jalankan API Server dan UI Tester pada dua terminal terpisah
go run ./cmd/server --env-file .env.test
go run ./cmd/uitester --env-file .env.test
```

### 2. Mode Produksi di VPS (Docker Compose)

```bash
# 1. Salin .env dari .env.example & generate rahasia AES/BlindIndex/DB acak
GENERATE_SECRETS=true ./scripts/promote_env.sh .env.example .env
# Edit .env: atur APP_ENV=production, ALLOWED_ORIGINS=<URL_FRONTEND_HTTPS>, TRUSTED_PROXIES, DB_HOST=postgres

# 2. Validasi ketat konfigurasi produksi (memastikan tidak ada placeholder/wildcard)
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env

# 3. Bangun dan nyalakan PostgreSQL 16 + API container
docker compose --env-file .env up -d --build

# 4. Buat akun Admin pertama (wajib setup MFA TOTP saat login pertama)
docker compose exec api /app/kopdes --env-file /app/.env admin create \
  --email admin@desaanda.id --nama "Ketua Koperasi" --hp 08120000001 --password "<SANDI_MIN_10_KARAKTER>"
```

Pasang *reverse proxy* HTTPS (Caddy/Nginx) di depan `${API_HOST_PORT}` dan jadwalkan cron pemeliharaan (`pesanan expire`, `iuran generate`, `pdp purge`, dan `scripts/backup.sh`). Penjelasan terperinci tersedia di [docs/PANDUAN_SISTEM_LENGKAP.md](docs/PANDUAN_SISTEM_LENGKAP.md).

---

## Peta Dokumentasi Lengkap

- **[docs/PANDUAN_SISTEM_LENGKAP.md](docs/PANDUAN_SISTEM_LENGKAP.md)** — Buku panduan utama end-to-end: set up lokal & VPS, cara pasang & guna tiap fitur, integrasi frontend, alur lengkap 5 peran (`publik`, `anggota`, `petugas_toko`, `bendahara`, `admin`), serta skema & alur seluruh 19 tabel PostgreSQL.
- **[docs/README.md](docs/README.md)** — Indeks seluruh dokumen proyek.
- **[docs/BACKEND_GUIDE.md](docs/BACKEND_GUIDE.md)** — Arsitektur domain, `sqlc`, enkripsi, CLI worker, dan verifikasi harian.
- **[docs/FRONTEND_GUIDE.md](docs/FRONTEND_GUIDE.md)** — Kontrak integrasi klien frontend & autentikasi sesi opaque.
- **[docs/openapi.yaml](docs/openapi.yaml)** & **[docs/roles_matrix.json](docs/roles_matrix.json)** — Spesifikasi OpenAPI 3.0.3 (35 endpoint) & matriks RBAC.
- **[docs/ENV_REFERENCE.md](docs/ENV_REFERENCE.md)** — Katalog resmi 40 variabel environment.
- **[docs/TUGAS_MANUAL_PEMILIK.md](docs/TUGAS_MANUAL_PEMILIK.md)** — Daftar periksa kewajiban pemilik koperasi, hukum, & akuntan.
- **[docs/REVAMP_REPORT.md](docs/REVAMP_REPORT.md)** — Laporan verifikasi akhir, metrik pemangkasan, & bukti eksekusi.

---

## Perintah Verifikasi Harian & Keamanan

```bash
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test.alt
go run ./cmd/envcheck --compare .env.test .env.test.alt
sqlc generate && git diff --exit-code internal/db
go build ./... && go vet ./...
go test -count=1 -race -p 1 ./...
staticcheck ./... && gosec -terse ./... && govulncheck ./...
gitleaks git --config .gitleaks.toml --verbose --redact
```

> **Catatan Kepatuhan**: Pengaturan retensi hapus data pendaftaran ditolak (`PDP_REJECTED_RETENTION_DAYS=30`) berstatus **PERLU VERIFIKASI HUKUM**, dan kolom klasifikasi akuntansi iuran serta perlakuan selisih kode unik QRIS berstatus **KEPUTUSAN AKUNTAN**.
