# Kopdes Merah Putih — Backend API Ramping (Koperasi Tunggal)

Backend API operasional berbasis **Go (`net/http` + `sqlc` + `pgx/v5`)** dan **PostgreSQL 16** yang dirancang khusus untuk **1 koperasi desa di 1 lokasi** (keanggotaan, pendaftaran + KYC terenkripsi, iuran bulanan QRIS statis/tunai, pengembalian dana manual, e-katalog + reservasi stok + checkout tanpa keranjang server, daftar pengantaran, pengaturan koperasi, worker terjadwal bawaan, serta laporan CSV aman).

> **Kebijakan Konfigurasi & Keamanan (`AGENTS.md`)**:
> Dilarang menyimpan nilai `.env` asli, kunci enkripsi, kata sandi produksi, ataupun nomor port secara statis di kode sumber, berkas HTML/JS, maupun dokumentasi. Seluruh nilai konfigurasi dikontrol melalui **46 variabel env** tervalidasi oleh `cmd/envcheck` (templat: [.env.example](.env.example); profil uji sintetis: [.env.test](.env.test) dan [.env.test.alt](.env.test.alt)). Hasil audit keputusan arsitektur terdokumentasi di [docs/DECISION_AUDIT.md](docs/DECISION_AUDIT.md) dan [docs/adr/](docs/adr/).

---

## Ringkasan Arsitektur Pasca-Audit (`decision-audit`)

| Komponen | Spesifikasi Aktual |
| :--- | :--- |
| **Runtime & Bahasa** | Go `1.27.4`, `PostgreSQL 16`, `sqlc`, `age` (backup off-server) |
| **Dependensi Langsung (`go.mod`)** | `github.com/google/uuid`, `github.com/jackc/pgx/v5`, `golang.org/x/crypto` |
| **Skema Basis Data** | **21 tabel** + 1 sequence dalam 2 migrasi ([000001](db/migrations/000001_init_kopdes_schema.up.sql), [000002](db/migrations/000002_decision_audit_enhancements.up.sql)) |
| **Permukaan API** | **36 endpoint HTTP** terdaftar di [routes.go](internal/api/routes.go) & [openapi.yaml](docs/openapi.yaml) |
| **Variabel Lingkungan** | Tepat **46 variabel** didefinisikan di [config.go](internal/config/config.go) |
| **Role Aplikasi DB** | Non-superuser `kopdes_app` (`NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS`) |
| **Transport Sesi & CSRF** | Cookie `HttpOnly` utama (`__Host-kopdes_session` / `kopdes_session`) + `CSRFGuard` (`Origin`/`Referer` + `X-Requested-With`) + Bearer opsional (`ALLOW_BEARER_AUTH`) |
| **Kriptografi & Penyimpanan** | Derivasi **HKDF-SHA256** 4 domain (`db-columns`, `kyc-files`, `bukti-files`, `backup`); amplop DEK seragam di `STORAGE_KYC_DIR` & volume terpisah `STORAGE_BUKTI_DIR` |
| **Stok & Refund** | Reservasi `stok_tertahan` selama `STOCK_HOLD_MINUTES` saat checkout, pemotongan `stok` nyata saat verifikasi `setuju` (atau fallback `perlu_tindak_lanjut`), & refund manual dual-control (`/pembayaran/{id}/refund`) |
| **Worker & Backup** | Layanan `worker` Compose (`pg_try_advisory_lock`) terekspos di `/readyz` + backup `.tar.age` (`BACKUP_AGE_RECIPIENT`, private key wajib off-server) |

---

## Peta Struktur Repositori

```text
kopdes-merah-putih/
├── cmd/
│   ├── server/              # Server HTTP API utama (kopdes-api)
│   ├── kopdes/              # CLI & worker: admin {create,reset-mfa,reset-password}, iuran generate, pesanan expire, pdp/bukti purge, storage migrate-files, worker
│   ├── envcheck/            # Validator ketat 46 variabel .env & konflik port
│   ├── seed_test_accounts/  # Seeder akun uji sintetis (khusus database *_test)
│   └── uitester/            # Server Frontend Tester polos non-produksi
├── internal/
│   ├── api/                 # Server HTTP, routes.go, CORS (Vary: Origin), CSRFGuard, HybridRateLimiter, E2E & RBAC matrix tests
│   ├── auth/                # Sesi opaque (throttle 60s), MFA TOTP 2-langkah, pengaturan, manajemen user
│   ├── pendaftaran/         # Pendaftaran publik (tanggal_lahir opsional), berkas KYC amplop DEK di STORAGE_KYC_DIR
│   ├── anggota/             # Profil anggota, masking/unmasking NIK & HP, suspend + cabut sesi instan, impor CSV
│   ├── iuran/               # Jenis iuran, generate tagihan bulanan idempoten, keringanan nominal
│   ├── katalog/             # Kategori, produk (validasi foto_url, stok_tersedia), promo berkuota
│   ├── pesanan/             # Checkout idempoten + reservasi stok_tertahan, status pesanan (admin/petugas_toko), daftar antar CSV/label
│   ├── pembayaran/          # QRIS statis (+ kode unik), tunai (admin/bendahara), bukti amplop di STORAGE_BUKTI_DIR, dual-control + breakglass admin, refund
│   ├── laporan/             # Laporan anggota, tunggakan, penjualan, pembayaran (+ total_dikembalikan) JSON & CSV
│   ├── worker/              # Penjadwal 5 tugas berkala dengan pg_try_advisory_lock per koneksi & tabel worker_runs
│   ├── config/              # Loader & validator ketat AppConfig (46 variabel)
│   ├── db/                  # Kode Go hasil generasi sqlc & pemeriksaan role DB non-superuser
│   ├── security/            # AES-256-GCM + AAD, kdf.go HKDF-SHA256, envelope encryption, TOTP, CSV neutralization
│   └── testseed/            # Helper seed sintetis terisolasi untuk pengujian otomatis
├── db/
│   ├── docker-init/         # 01_roles.sql (kopdes_app) & 02_migrations.sh (seluruh migrasi berurut)
│   └── migrations/          # 000001_init_kopdes_schema.* & 000002_decision_audit_enhancements.*
├── docs/                    # Dokumentasi backend/frontend, OpenAPI, 36-route RBAC matrix, ENV_REFERENCE, DECISION_AUDIT, & 10 ADR
├── frontend-tester/         # Aplikasi uji HTML/JS polos (tanpa framework, build step, maupun CDN; credentials: include + X-Requested-With)
├── scripts/                 # promote_env.{sh,ps1}, port_check.{sh,ps1}, backup.sh (age), restore_drill.sh, restore_test.sh
├── Dockerfile               # Multi-stage build minimal & container non-root (kopdesuser)
└── docker-compose.yml       # Orkestrasi PostgreSQL 16 + API + Worker (volume pg_data, kyc_storage, bukti_storage)
```

---

## Panduan Cepat Menjalankan

### 1. Mode Uji / Pengembangan Lokal
Pastikan PostgreSQL 16 berjalan pada `${DB_PORT}` sesuai `.env.test`:

```bash
# 1. Validasi konfigurasi 46 variabel & cek konflik port
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test

# 2. Inisialisasi role kopdes_app & seluruh migrasi (21 tabel) ke database uji
psql -h "${DB_HOST}" -p "${DB_PORT}" -U postgres -d "${DB_NAME}" -v ON_ERROR_STOP=1 -f db/docker-init/01_roles.sql
for f in db/migrations/*_*.up.sql; do
  psql -h "${DB_HOST}" -p "${DB_PORT}" -U postgres -d "${DB_NAME}" -v ON_ERROR_STOP=1 -f "$f"
done

# 3. Isi akun uji sintetis (khusus database *_test)
go run ./cmd/seed_test_accounts --env-file .env.test

# 4. Jalankan API Server, Worker (--tick), dan UI Tester
go run ./cmd/server --env-file .env.test
go run ./cmd/kopdes --env-file .env.test worker --tick 60s
go run ./cmd/uitester --env-file .env.test
```

### 2. Mode Produksi di VPS (Docker Compose)

```bash
# 1. Salin .env dari .env.example & generate rahasia CSPRNG (di mesin pemilik)
GENERATE_SECRETS=true ./scripts/promote_env.sh .env.example .env
# Bangkitkan kunci age di luar VPS: age-keygen -o offserver-key.txt
# Edit .env: setel APP_ENV=production, ALLOWED_ORIGINS=<URL_FRONTEND_HTTPS>, TRUSTED_PROXIES, BACKUP_AGE_RECIPIENT=<age1...>

# 2. Validasi ketat konfigurasi produksi
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env

# 3. Nyalakan PostgreSQL 16 + API + Worker terjadwal
docker compose --env-file .env up -d --build

# 4. Buat akun Admin pertama (wajib setup MFA TOTP saat login pertama)
docker compose exec api /app/kopdes --env-file /app/.env admin create \
  --email admin@desaanda.id --nama "Ketua Koperasi" --hp 08120000001 --password "<SANDI_KUAT>"
```

Pasang *reverse proxy* HTTPS (Caddy/Nginx) di depan `${API_HOST_PORT}` dan jadwalkan cron backup harian (`ENV_FILE=/opt/kopdes/.env bash /opt/kopdes/scripts/backup.sh`) serta sinkronkan hasilnya ke luar VPS.

---

## Peta Dokumentasi Lengkap

- **[docs/DECISION_AUDIT.md](docs/DECISION_AUDIT.md)** & **[docs/adr/](docs/adr/)** — Laporan Audit Keputusan D1–D10 lengkap beserta 10 ADR berbobot dan bukti pengujian terukur.
- **[docs/PANDUAN_SISTEM_LENGKAP.md](docs/PANDUAN_SISTEM_LENGKAP.md)** — Buku panduan utama end-to-end: set up lokal & VPS, cara pasang & guna tiap fitur, integrasi frontend, alur lengkap 5 peran, serta skema 21 tabel PostgreSQL.
- **[docs/README.md](docs/README.md)** — Indeks seluruh dokumen proyek.
- **[docs/BACKEND_GUIDE.md](docs/BACKEND_GUIDE.md)** — Arsitektur domain, `sqlc`, HKDF, worker advisory lock, CLI pemulihan, dan verifikasi harian.
- **[docs/FRONTEND_GUIDE.md](docs/FRONTEND_GUIDE.md)** — Kontrak integrasi klien frontend, cookie `HttpOnly` + CSRF, reservasi stok, & refund manual.
- **[docs/openapi.yaml](docs/openapi.yaml)** & **[docs/roles_matrix.json](docs/roles_matrix.json)** — Spesifikasi OpenAPI 3.0.3 (`36` endpoint) & katalog `36` rute / matriks RBAC.
- **[docs/ENV_REFERENCE.md](docs/ENV_REFERENCE.md)** — Katalog resmi `46` variabel environment.
- **[docs/TUGAS_MANUAL_PEMILIK.md](docs/TUGAS_MANUAL_PEMILIK.md)** — Kewajiban pemilik (kunci `age` off-server, CLI pemulihan admin, & keputusan bisnis/hukum/akuntan).

---

## Perintah Verifikasi Harian & Keamanan

```bash
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test.alt
go run ./cmd/envcheck --compare .env.test .env.test.alt
sqlc generate && git diff --exit-code internal/db
go build ./... && go vet ./...
go test -count=1 -race -p 1 ./...
staticcheck ./... && gosec ./... && govulncheck ./...
gitleaks git --config .gitleaks.toml --verbose --redact
```
