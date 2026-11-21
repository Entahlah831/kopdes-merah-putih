# Kopdes Merah Putih — Backend API Ramping (Koperasi Tunggal)

Backend API operasional berbasis **Go (`net/http` + `sqlc` + `pgx/v5`)** dan **PostgreSQL 16** yang dirancang khusus untuk **1 koperasi desa di 1 lokasi** (keanggotaan, pendaftaran + KYC terenkripsi, iuran bulanan QRIS statis/tunai, pengembalian dana manual, e-katalog + reservasi stok + checkout tanpa keranjang server, daftar pengantaran, pengaturan koperasi, worker terjadwal bawaan, serta laporan CSV aman).

> **Kebijakan Konfigurasi & Keamanan (`AGENTS.md`)**:
> Dilarang menyimpan nilai `.env` asli, kunci enkripsi, kata sandi produksi, ataupun nomor port secara statis di kode sumber, berkas HTML/JS, maupun dokumentasi. Seluruh nilai konfigurasi dikontrol melalui **SATU sumber `.env` tepat 51 variabel** tervalidasi oleh `cmd/envcheck` (templat: [.env.example](.env.example); profil uji sintetis: [.env.test](.env.test) dan [.env.test.alt](.env.test.alt)). Hasil audit & arsitektur terdokumentasi di [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/MODULAR_REPORT.md](docs/MODULAR_REPORT.md), dan [docs/adr/](docs/adr/).

---

## Ringkasan Arsitektur (`modular-and-wireframe`)

| Komponen | Spesifikasi Aktual |
| :--- | :--- |
| **Runtime & Bahasa** | Go `1.27.4`, `PostgreSQL 16`, `sqlc`, `age` (backup off-server), Vite + TypeScript strict (`frontend-wireframe/`) |
| **Pola Proses** | **Modular Monolith** (`8` modul domain `internal/<modul>/api` + `TestArchitecture_*`) dalam **satu biner `kopdes`** (`serve`, `worker`, `uiserve`, `admin`) |
| **Dependensi Langsung (`go.mod`)** | `github.com/google/uuid`, `github.com/jackc/pgx/v5`, `golang.org/x/crypto` |
| **Skema Basis Data** | **21 tabel** + 1 sequence dalam 2 migrasi ([000001](db/migrations/000001_init_kopdes_schema.up.sql), [000002](db/migrations/000002_decision_audit_enhancements.up.sql)) |
| **Permukaan API** | **36 endpoint HTTP** terdaftar di [routes.go](internal/api/routes.go) & [openapi.yaml](docs/openapi.yaml) |
| **Variabel Lingkungan** | Tepat **51 variabel** didefinisikan di [config.go](internal/config/config.go) (`WORKER_PORT`, `WORKER_TICK_INTERVAL`, `SLO_*`, dll.) |
| **Role Aplikasi DB** | Non-superuser `kopdes_app` (`NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS`) |
| **Transport Sesi & CSRF** | Cookie `HttpOnly` utama (`__Host-kopdes_session` / `kopdes_session`) + `CSRFGuard` (`Origin`/`Referer` + `X-Requested-With`) + Bearer opsional (`ALLOW_BEARER_AUTH`) |
| **Kriptografi & Penyimpanan** | Derivasi **HKDF-SHA256** 4 domain (`db-columns`, `kyc-files`, `bukti-files`, `backup`) via `internal/filestore`; amplop DEK seragam di `STORAGE_KYC_DIR` & `STORAGE_BUKTI_DIR` |
| **Stok & Refund** | Reservasi `stok_tertahan` selama `STOCK_HOLD_MINUTES` saat checkout, pemotongan `stok` nyata saat verifikasi `setuju` (atau fallback `perlu_tindak_lanjut`), & refund manual dual-control (`/pembayaran/{id}/refund`) |
| **Worker & Backup** | Proses `kopdes worker` (`pg_try_advisory_lock` + `/healthz` pada `WORKER_PORT`) + telemetri `/readyz` + backup `.tar.age` (`BACKUP_AGE_RECIPIENT`, private key wajib off-server) |

---

## Peta Struktur Repositori

```text
kopdes-merah-putih/
├── cmd/
│   ├── kopdes/              # Biner tunggal utama: serve, worker, uiserve, admin {create,reset-mfa,reset-password}, iuran, pesanan, pdp/bukti purge, storage migrate-files
│   ├── server/              # Alias biner HTTP API (dipakai chaosdrill / kompatibilitas)
│   ├── envcheck/            # Validator ketat 51 variabel .env, konflik port, & generator ENV_REFERENCE.md
│   ├── seed_test_accounts/  # Seeder akun uji sintetis (khusus database *_test)
│   └── uitester/            # Wrapper UI tester / referensi UI via /config.json dinamis
├── internal/
│   ├── api/                 # Server HTTP, routes.go, CORS (Vary: Origin), CSRFGuard, HybridRateLimiter, E2E, RBAC & security/chaos tests
│   ├── auth/                # Modul 1: api/, service, handler (sesi opaque, MFA TOTP 2-langkah, manajemen user)
│   ├── pengaturan/          # Modul 2: api/, service, handler (konfigurasi koperasi & audit_log)
│   ├── anggota/             # Modul 3: api/, service, handler (pendaftaran publik, KYC terenkripsi, profil & impor CSV)
│   ├── iuran/               # Modul 4: api/, service, handler (jenis iuran, generate bulanan idempoten, keringanan)
│   ├── katalog/             # Modul 5: api/, service, handler (kategori, produk, promo berkuota, reservasi stok)
│   ├── pesanan/             # Modul 6: api/, service, handler (checkout idempoten, pemenuhan, daftar antar CSV)
│   ├── pembayaran/          # Modul 7: api/, service, handler (QRIS statis, tunai, bukti terenkripsi, dual-control + refund)
│   ├── laporan/             # Modul 8: read aggregator murni (0 tabel milik sendiri, membaca antar-modul via */api)
│   ├── filestore/           # Penyimpanan berkas terenkripsi amplop DEK AES-256-GCM terpisah (kyc vs bukti)
│   ├── uiserve/             # Penyaji aset SPA statis + endpoint runtime /config.json tanpa hardcoded port
│   ├── worker/              # Penjadwal tugas berkala dengan pg_try_advisory_lock, worker_runs, & health server WORKER_PORT
│   ├── config/              # Loader & validator ketat AppConfig (51 variabel) + audit keselarasan repo
│   ├── db/                  # Kode Go hasil generasi sqlc (per modul) & pemeriksaan role DB non-superuser
│   ├── security/            # AES-256-GCM + AAD, kdf.go HKDF-SHA256, envelope encryption, TOTP, CSV neutralization
│   └── testseed/            # Helper seed sintetis terisolasi untuk pengujian otomatis
├── frontend-wireframe/      # Implementasi UI referensi Wireframe Figma (Vite + TypeScript ketat, 0 deps runtime, Playwright E2E)
├── frontend-tester/         # Aplikasi uji HTML/JS polos (tanpa framework/build step; credentials: include + X-Requested-With)
├── tools/                   # loadtest & chaosdrill Go native
├── tests/                   # Skrip k6, OWASP ZAP, Semgrep, & pemindaian container Linux/CI
├── db/                      # docker-init/01_roles.sql & migrations/000001..000002 (21 tabel)
├── docs/                    # Dokumentasi lengkap, ARCHITECTURE, BASELINE_REPORT, MODULAR_REPORT, adr/, wireframe/, openapi.yaml, ENV_REFERENCE
├── reports/                 # Bukti pengukuran terukur (baseline/, spike/, wireframe/, final/)
├── scripts/                 # promote_env.{sh,ps1}, port_check.{sh,ps1}, backup.sh (age), restore_drill.sh, restore_test.sh
├── Dockerfile               # Multi-stage build minimal & container non-root (kopdesuser)
└── docker-compose.yml       # Orkestrasi profil app, worker, tester, & proxy (Caddy); 0 literal port
```

---

## Panduan Cepat Menjalankan

### 1. Mode Uji / Pengembangan Lokal
Pastikan PostgreSQL 16 berjalan pada `${DB_PORT}` sesuai `.env.test`:

```bash
# 1. Validasi konfigurasi 51 variabel & cek konflik port
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test

# 2. Inisialisasi role kopdes_app & seluruh migrasi (21 tabel) ke database uji
psql -h "${DB_HOST}" -p "${DB_PORT}" -U postgres -d "${DB_NAME}" -v ON_ERROR_STOP=1 -f db/docker-init/01_roles.sql
for f in db/migrations/*_*.up.sql; do
  psql -h "${DB_HOST}" -p "${DB_PORT}" -U postgres -d "${DB_NAME}" -v ON_ERROR_STOP=1 -f "$f"
done

# 3. Isi akun uji sintetis (khusus database *_test)
go run ./cmd/seed_test_accounts --env-file .env.test

# 4. Jalankan API Server, Worker, dan UI (satu biner kopdes)
go run ./cmd/kopdes --env-file .env.test serve
go run ./cmd/kopdes --env-file .env.test worker
go run ./cmd/kopdes --env-file .env.test uiserve --ui-dir frontend-wireframe/dist
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

- **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** — Peta Modular Monolith 8 modul (`internal/<modul>/api`), kepemilikan 21 tabel, diagram alur Mermaid, dan aturan batas (`TestArchitecture_*`).
- **[docs/MODULAR_REPORT.md](docs/MODULAR_REPORT.md)** & **[docs/BASELINE_REPORT.md](docs/BASELINE_REPORT.md)** — Perbandingan Baseline (Fase 0) vs Final (Fase 4), hasil chaos/SAST, Set C port acak, & eksperimen `spike/filesvc`.
- **[docs/adr/](docs/adr/)** — ADR-0001 (Modular Monolith), ADR-0002 (Eksperimen `spike/filesvc`), serta 10 ADR Audit Keputusan D1–D10 (`ADR-001`..`ADR-010`).
- **[docs/wireframe/](docs/wireframe/)** — Inventaris 19 layar Figma, analisis kesenjangan (`BLOCKED: butuh keputusan pemilik`), dan laporan Playwright `frontend-wireframe/`.
- **[docs/DECISION_AUDIT.md](docs/DECISION_AUDIT.md)** — Laporan Audit Keputusan D1–D10 lengkap beserta bukti pengujian terukur.
- **[docs/PANDUAN_SISTEM_LENGKAP.md](docs/PANDUAN_SISTEM_LENGKAP.md)** — Buku panduan end-to-end: set up lokal & VPS, integrasi frontend, alur lengkap peran, serta skema 21 tabel PostgreSQL.
- **[docs/README.md](docs/README.md)** — Indeks seluruh dokumen proyek.
- **[docs/BACKEND_GUIDE.md](docs/BACKEND_GUIDE.md)** — Arsitektur 8 modul domain, `sqlc`, HKDF, worker advisory lock, CLI pemulihan, dan verifikasi harian.
- **[docs/FRONTEND_GUIDE.md](docs/FRONTEND_GUIDE.md)** — Kontrak integrasi klien frontend, cookie `HttpOnly` + CSRF, reservasi stok, & refund manual.
- **[docs/openapi.yaml](docs/openapi.yaml)** & **[docs/roles_matrix.json](docs/roles_matrix.json)** — Spesifikasi OpenAPI 3.0.3 (`36` endpoint) & katalog `36` rute / matriks RBAC.
- **[docs/ENV_REFERENCE.md](docs/ENV_REFERENCE.md)** — Katalog resmi **51 variabel** environment.
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
