# Kopdes Merah Putih — Backend API Ramping (Koperasi Tunggal)

Backend API operasional berbasis **Go (`net/http` + `sqlc` + `pgx/v5`)** dan **PostgreSQL 16** yang dirancang khusus untuk **1 koperasi desa di 1 lokasi** (keanggotaan, pendaftaran + KYC terenkripsi, iuran bulanan QRIS statis/tunai, pengembalian dana manual, e-katalog + reservasi stok + checkout tanpa keranjang server, daftar pengantaran, pengaturan koperasi, worker terjadwal bawaan, serta laporan CSV aman).

> **Kebijakan Konfigurasi & Keamanan (`AGENTS.md`)**:
> Dilarang menyimpan nilai `.env` asli, kunci enkripsi, kata sandi produksi, ataupun nomor port secara statis di kode sumber, berkas HTML/JS, maupun dokumentasi. Seluruh nilai konfigurasi dikontrol melalui **SATU sumber `.env` tepat 44 variabel** (`<= 45`, dengan alasan tertulis di [docs/ENV_REFERENCE.md](docs/ENV_REFERENCE.md)) tervalidasi oleh `backend/cmd/envcheck` (templat: [.env.example](.env.example); profil uji sintetis: [.env.test](.env.test) dan [.env.test.alt](.env.test.alt)).

---

## Ringkasan Arsitektur (`modular-and-wireframe`)

| Komponen | Spesifikasi Aktual |
| :--- | :--- |
| **Runtime & Bahasa** | Go `1.27.4` (`backend/`), `PostgreSQL 16`, `sqlc v1.31.1`, `age` (backup + brankas kunci 85/15), Vite + TypeScript strict (`frontend/wireframe/`), `Caddy` (`Caddyfile`) |
| **Pola Proses** | **Modular Monolith** (`8` modul domain `backend/internal/<modul>/api` + `TestArchitecture_*`) dalam **satu biner `kopdes`** (`serve`, `worker`, `uiserve`, `healthcheck`, `admin`) |
| **Dependensi Langsung (`backend/go.mod`)** | `github.com/google/uuid`, `github.com/jackc/pgx/v5`, `golang.org/x/crypto` |
| **Skema Basis Data** | **21 tabel** + 1 sequence dalam 2 migrasi ([000001](backend/db/migrations/000001_init_kopdes_schema.up.sql), [000002](backend/db/migrations/000002_decision_audit_enhancements.up.sql)) |
| **Permukaan API** | **37 endpoint HTTP** terdaftar di [routes.go](backend/internal/api/routes.go) & [openapi.yaml](docs/openapi.yaml) (termasuk #37 `GET /api/v1/audit-log`) |
| **Variabel Lingkungan** | Tepat **44 variabel** (`<= 45`) didefinisikan di [config.go](backend/internal/config/config.go) |
| **Role Aplikasi DB** | Non-superuser `kopdes_app` (`NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS`) |
| **Transport Sesi & CSRF** | Cookie `HttpOnly` utama (`__Host-kopdes_session` / `kopdes_session`) + `CSRFGuard` (`Origin`/`Referer` + `X-Requested-With`) + Bearer opsional (`ALLOW_BEARER_AUTH`) |
| **Kriptografi & Kunci 85/15** | Derivasi **HKDF-SHA256** 4 domain (`db-columns`, `kyc-files`, `bukti-files`, `backup`) via `internal/filestore`; skrip brankas & rotasi kunci 85% (`scripts/{backup,restore,rotate}_keys.sh`) dengan 15% inti oleh pemilik ([docs/KEAMANAN_KUNCI_PEMILIK.md](docs/KEAMANAN_KUNCI_PEMILIK.md)) |
| **Stok & Refund** | Reservasi `stok_tertahan` selama `STOCK_HOLD_MINUTES` saat checkout, pemotongan `stok` nyata saat verifikasi `setuju` (atau fallback `perlu_tindak_lanjut`), & refund manual dual-control (`/pembayaran/{id}/refund`) |
| **Worker & Backup** | Proses `kopdes worker` (`pg_try_advisory_lock` + `/healthz` pada `WORKER_PORT`) + telemetri `/readyz` + backup `.tar.age` (`BACKUP_AGE_RECIPIENT`, private key wajib off-server) |

---

## Peta Struktur Repositori (Backend & Frontend Terpisah)

```text
kopdes-merah-putih/
├── backend/                     # Layanan Backend Go Modular Monolith -> backend/README.md
│   ├── cmd/                     # kopdes (serve, worker, uiserve, healthcheck, admin, ...), server, envcheck, seed_test_accounts, uitester
│   ├── internal/                # api/ (37 rute), 8 modul domain (<modul>/api), filestore, uiserve, worker, config, db (sqlc), security
│   ├── db/                      # docker-init/01_roles.sql & migrations/000001..000002 (21 tabel)
│   ├── tools/                   # loadtest & chaosdrill Go native
│   ├── go.mod / go.sum / sqlc.yaml
│   └── Dockerfile               # Multi-stage build minimal non-root (UID/GID 10001:10001) + HEALTHCHECK JSON
├── frontend/                    # Antarmuka Klien SPA & Alat Uji -> frontend/README.md & frontend/TODO_FRONTEND.md
│   ├── wireframe/               # Vite + TypeScript ketat (11 rute, 0 innerHTML, Playwright E2E Suite 01–06)
│   └── tester/                  # Aplikasi uji HTML/JS polos (tanpa build step)
├── tests/                       # Skrip k6, OWASP ZAP 5-peran + MFA, Semgrep, & Schemathesis
├── docs/                        # Panduan lengkap: PANDUAN_PASANG_VPS_LINUX, KEAMANAN_KUNCI_PEMILIK, BACKEND_GUIDE, FRONTEND_GUIDE, dll.
├── reports/                     # Bukti pengukuran terukur (baseline/, spike/, wireframe/, final/)
├── scripts/                     # promote_env, port_check, backup/restore_drill (age), & backup_keys/restore_keys/rotate_keys (85/15)
├── Caddyfile                    # Reverse proxy HTTPS produksi + penyaji frontend/wireframe/dist same-origin (0 literal port)
└── docker-compose.yml           # Orkestrasi profil app, worker, tester, & proxy (Caddy); 0 literal port
```

---

## Panduan Cepat Menjalankan

### 1. Mode Uji / Pengembangan Lokal
Pastikan PostgreSQL 16 berjalan pada `${DB_PORT}` sesuai `.env.test`:

```bash
# 1. Validasi konfigurasi 44 variabel & cek konflik port
go -C backend run ./cmd/envcheck --strict --ports --no-listen --env-file "${PWD}/.env.test"

# 2. Inisialisasi role kopdes_app & seluruh migrasi (21 tabel) ke database uji
psql -h "${DB_HOST}" -p "${DB_PORT}" -U postgres -d "${DB_NAME}" -v ON_ERROR_STOP=1 -f backend/db/docker-init/01_roles.sql
for f in backend/db/migrations/*_*.up.sql; do
  psql -h "${DB_HOST}" -p "${DB_PORT}" -U postgres -d "${DB_NAME}" -v ON_ERROR_STOP=1 -f "$f"
done

# 3. Isi akun uji sintetis (khusus database *_test) & bangun UI
go -C backend run ./cmd/seed_test_accounts --env-file "${PWD}/.env.test"
npm --prefix frontend/wireframe ci && npm --prefix frontend/wireframe run build

# 4. Jalankan API Server, Worker, dan UI (satu biner kopdes)
go -C backend run ./cmd/kopdes --env-file "${PWD}/.env.test" serve
go -C backend run ./cmd/kopdes --env-file "${PWD}/.env.test" worker
go -C backend run ./cmd/kopdes --env-file "${PWD}/.env.test" uiserve --ui-dir frontend/wireframe/dist
```

### 2. Mode Produksi di VPS Linux (`Caddy` HTTPS)

Panduan lengkap instalasi dari nol hingga HTTPS, firewall, cron, & rotasi kunci ada di **[docs/PANDUAN_PASANG_VPS_LINUX.md](docs/PANDUAN_PASANG_VPS_LINUX.md)**:

```bash
# 1. Salin .env dari .env.example & generate rahasia (lalu isi 15% kunci inti pemilik)
GENERATE_SECRETS=true ./scripts/promote_env.sh .env.example .env

# 2. Validasi ketat konfigurasi produksi & bangun UI statis
go -C backend run ./cmd/envcheck --strict --ports --no-listen --env-file "${PWD}/.env"
npm --prefix frontend/wireframe ci && npm --prefix frontend/wireframe run build

# 3. Nyalakan PostgreSQL 16 + API + Worker + Reverse Proxy Caddy HTTPS
KOPDES_SITE_ADDRESS="https://koperasi.desaanda.id" \
docker compose --env-file .env --profile app --profile worker --profile proxy up -d --build

# 4. Buat akun Admin pertama (wajib setup MFA TOTP saat login pertama)
docker compose exec api /app/kopdes --env-file /app/.env admin create \
  --email admin@desaanda.id --nama "Ketua Koperasi" --hp 08120000001 --password "<SANDI_KUAT>"
```

---

## Peta Dokumentasi Lengkap

- **[backend/README.md](backend/README.md)** & **[docs/BACKEND_GUIDE.md](docs/BACKEND_GUIDE.md)** — Dokumentasi backend Go Modular Monolith, `sqlc`, HKDF, worker, CLI, dan audit keamanan.
- **[frontend/README.md](frontend/README.md)**, **[frontend/TODO_FRONTEND.md](frontend/TODO_FRONTEND.md)**, & **[docs/FRONTEND_GUIDE.md](docs/FRONTEND_GUIDE.md)** — Peta UI 11 rute, kontrak integrasi frontend, & **daftar tugas tim frontend**.
- **[docs/PANDUAN_PASANG_VPS_LINUX.md](docs/PANDUAN_PASANG_VPS_LINUX.md)** & **[docs/STAGING_CHECKLIST.md](docs/STAGING_CHECKLIST.md)** — Panduan langkah demi langkah pemasangan produksi & staging di VPS Linux.
- **[docs/KEAMANAN_KUNCI_PEMILIK.md](docs/KEAMANAN_KUNCI_PEMILIK.md)** & **[docs/TUGAS_MANUAL_PEMILIK.md](docs/TUGAS_MANUAL_PEMILIK.md)** — Model 85% skrip vs 15% inti pemilik untuk brankas & rotasi kunci enkripsi (`age`) + keputusan bisnis `KP-01`..`KP-10`.
- **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)**, **[docs/MODULAR_REPORT.md](docs/MODULAR_REPORT.md)**, & **[docs/adr/](docs/adr/)** — Arsitektur 8 modul, kepemilikan 21 tabel, dan ADR keputusan D1–D10.
- **[docs/wireframe/](docs/wireframe/)** — Inventaris 19 frame Figma, analisis kesenjangan (`BLOCKED: butuh keputusan pemilik`), & [matriks 19 frame vs E2E](docs/wireframe/MATRIKS_E2E_19_FRAME.md).
- **[docs/GO_LIVE_CRITERIA.md](docs/GO_LIVE_CRITERIA.md)**, **[docs/PENTEST_SCOPE.md](docs/PENTEST_SCOPE.md)**, **[docs/PANDUAN_PENGURUS.md](docs/PANDUAN_PENGURUS.md)** — Kriteria UAT/Pilot/Produksi, pentest, & panduan harian pengurus.
- **[docs/openapi.yaml](docs/openapi.yaml)**, **[docs/roles_matrix.json](docs/roles_matrix.json)**, & **[docs/ENV_REFERENCE.md](docs/ENV_REFERENCE.md)** — Spesifikasi **37 endpoint** OpenAPI & katalog **44 variabel env**.

---

## Perintah Verifikasi Harian & Keamanan

```bash
go -C backend run ./cmd/envcheck --strict --ports --no-listen --env-file "${PWD}/.env.test"
go -C backend run ./cmd/envcheck --strict --ports --no-listen --env-file "${PWD}/.env.test.alt"
go -C backend run ./cmd/envcheck --compare "${PWD}/.env.test" "${PWD}/.env.test.alt"
(cd backend && sqlc generate && git diff --exit-code internal/db)
go -C backend build ./... && go -C backend vet ./...
go -C backend test -count=1 -race -p 1 ./...
(cd backend && staticcheck ./... && gosec ./... && govulncheck ./...)
gitleaks git --config .gitleaks.toml --verbose --redact
(cd frontend/wireframe && npx tsc --noEmit && npm run build)
```
