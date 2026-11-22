# Backend Go — Kopdes Merah Putih (Modular Monolith)

Direktori `backend/` memuat seluruh kode sumber layanan API, penjadwal worker, CLI utilitas, migrasi PostgreSQL 16, query terketik `sqlc v1.31.1`, dan Dockerfile multi-stage berbasis non-root (`10001:10001`) untuk **1 koperasi tunggal di 1 lokasi**.

---

## Ringkasan Komponen

| Jalur | Peran |
| :--- | :--- |
| [cmd/kopdes/](cmd/kopdes/) | **Biner tunggal utama**: `serve` (API HTTP), `worker` (6 job berkala + `/healthz` `WORKER_PORT`), `uiserve` (SPA + `/config.json`), `healthcheck`, `admin {create,reset-mfa,reset-password}`, `iuran generate`, `pesanan expire`, `pdp/bukti purge`, `storage migrate-files`. |
| [cmd/envcheck/](cmd/envcheck/) | Validator ketat **44 variabel `.env`**, deteksi konflik port, pemeriksaan guard produksi, dan generator [../docs/ENV_REFERENCE.md](../docs/ENV_REFERENCE.md). |
| [cmd/seed_test_accounts/](cmd/seed_test_accounts/) | Seeder deterministik 5 akun uji + katalog/iuran contoh (hanya berjalan jika `DB_NAME` berakhiran `_test`). |
| [cmd/server/](cmd/server/) & [cmd/uitester/](cmd/uitester/) | Pembungkus kompatibilitas API & penyaji UI uji terisolasi. |
| [internal/api/](internal/api/) | Router **37 endpoint** ([routes.go](internal/api/routes.go)), `CORS`, `CSRFGuard`, `HybridRateLimiter`, `IPRateLimiter` (`RATE_LIMIT_RPS`), batas ukuran body, mode baca-saja, serta tes kontrak `TestRBAC_*`, `TestStructural_*`, `TestSecurityAbuse_*`, dan `TestChaos_*`. |
| [internal/auth/](internal/auth/), [pengaturan/](internal/pengaturan/), [anggota/](internal/anggota/), [iuran/](internal/iuran/), [katalog/](internal/katalog/), [pesanan/](internal/pesanan/), [pembayaran/](internal/pembayaran/), [laporan/](internal/laporan/) | **8 modul domain** dengan isolasi impor ketat (`internal/<modul>/api` saja) dan kepemilikan tabel eksplisit yang dipaksa oleh [internal/arch_test.go](internal/arch_test.go). |
| [internal/security/](internal/security/) & [internal/filestore/](internal/filestore/) | Enkripsi AEAD AES-256-GCM + AAD, derivasi HKDF-SHA256 4 konteks, amplop DEK berkas `.enc` (`kyc` vs `bukti`), blind index, TOTP RFC 6238, dan netralisasi formula CSV. |
| [internal/config/](internal/config/) | Loader + validator **44 variabel env** dan tes audit repositori ([repo_audit_test.go](internal/config/repo_audit_test.go)). |
| [internal/db/](internal/db/) | Kode Go hasil generasi **`sqlc v1.31.1`** ([sqlc.yaml](sqlc.yaml)) — **dilarang disunting manual**. |
| [db/](db/) | Inisialisasi role PostgreSQL non-superuser (`kopdes_app`, [01_roles.sql](db/docker-init/01_roles.sql)) dan migrasi skema **21 tabel** ([migrations/](db/migrations/)). |
| [tools/](tools/) | Utilitas uji beban (`loadtest`) dan uji kekacauan atomik (`chaosdrill`). |
| [Dockerfile](Dockerfile) | Multi-stage `golang:1.27.4-alpine3.23` -> `alpine:3.23`, `USER 10001:10001`, dan `HEALTHCHECK CMD ["/app/kopdes", "healthcheck"]`. |

---

## Perintah Pengujian & Audit Harian

Jalankan dari akar repositori (memakai flag `-C backend`) atau langsung dari dalam `backend/`:

```bash
# 1. Validasi & regenerasi query SQL (wajib bebas drift)
cd backend && sqlc version && sqlc generate && git diff --exit-code internal/db

# 2. Validasi 44 variabel lingkungan (dari akar repositori)
go -C backend run ./cmd/envcheck --strict --ports --no-listen --env-file "${PWD}/.env.test"
go -C backend run ./cmd/envcheck --strict --ports --no-listen --env-file "${PWD}/.env.test.alt"
go -C backend run ./cmd/envcheck --compare "${PWD}/.env.test" "${PWD}/.env.test.alt"

# 3. Build, vet, dan tes (mode cepat lokal tanpa DB vs penuh dengan PG16)
go -C backend build ./... && go -C backend vet ./...
go -C backend test -short -count=1 -race ./...
# Bila PostgreSQL 16 uji aktif sesuai .env.test:
go -C backend test -count=1 -race -p 1 ./...

# 4. SAST & pemindai rahasia
(cd backend && staticcheck ./... && gosec ./... && govulncheck ./...)
gitleaks git --config .gitleaks.toml --verbose --redact
```

---

## Dokumentasi Lanjutan
- **[../docs/BACKEND_GUIDE.md](../docs/BACKEND_GUIDE.md)** — Detail arsitektur modular, kontrak antarmodul, & pengamanan role DB.
- **[../docs/ARCHITECTURE.md](../docs/ARCHITECTURE.md)** — Diagram alur, kepemilikan 21 tabel, & penjadwal worker.
- **[../docs/openapi.yaml](../docs/openapi.yaml)** & **[../docs/roles_matrix.json](../docs/roles_matrix.json)** — Kontrak OpenAPI **37 operasi HTTP** (`GET /api/v1/audit-log` termasuk #37).
- **[../docs/KEAMANAN_KUNCI_PEMILIK.md](../docs/KEAMANAN_KUNCI_PEMILIK.md)** — Model brankas & rotasi kunci 85% / 15% inti pemilik.
- **[../docs/PANDUAN_PASANG_VPS_LINUX.md](../docs/PANDUAN_PASANG_VPS_LINUX.md)** — Cara pasang produksi di VPS Linux.
