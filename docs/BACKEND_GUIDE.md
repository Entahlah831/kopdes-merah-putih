# Panduan Developer Backend — Kopdes Merah Putih (Ramping)

Target Runtime: `Go 1.27+` | `PostgreSQL 16` | Kontrak API: `v2.1.0-lean` (`36` endpoint, `21` tabel, `43` variabel env).  
Arsitektur Modular Monolith terdokumentasi di [ARCHITECTURE.md](ARCHITECTURE.md), [adr/0001-modular-monolith.md](adr/0001-modular-monolith.md), dan [MODULAR_REPORT.md](MODULAR_REPORT.md).

> **Catatan Kepatuhan & Akuntansi**: Backend ini menerapkan kontrol keamanan teknis sesuai baseline OWASP dan prinsip minimalisasi data (`tanggal_lahir` opsional). Penetapan masa retensi dokumen calon anggota yang ditolak (`PDP_REJECTED_RETENTION_DAYS=30`) dan bukti non-lunas (`BUKTI_RETENTION_DAYS=365`) ditandai **PERLU VERIFIKASI HUKUM**, dan kolom `klasifikasi_akuntansi` / selisih kode unik / koreksi refund ditandai **KEPUTUSAN AKUNTAN**.

---

## 1. Prinsip & Anggaran Arsitektur Ramping Modular

Backend dirancang **khusus untuk 1 koperasi di 1 lokasi** (tanpa multi-tenant RLS, tanpa SIMKOPDES, tanpa simpanan/pinjaman/SHU, tanpa double-entry ledger, tanpa keranjang server, dan tanpa impor CSV mutasi bank):

1. **Basis Data**: `21` tabel lintas migrasi [000001_init_kopdes_schema.up.sql](../db/migrations/000001_init_kopdes_schema.up.sql) dan [000002_decision_audit_enhancements.up.sql](../db/migrations/000002_decision_audit_enhancements.up.sql). Seluruh query diketik via **`sqlc`** ke [internal/db/](../internal/db/) dengan kepemilikan tabel per modul yang dijaga `TestArchitecture_*`. Dilarang menulis SQL mentah atau mengedit `internal/db/*.go` manual.
2. **Keamanan Role Database**: Koneksi aplikasi wajib memakai role non-superuser `kopdes_app` (`NOSUPERUSER NOBYPASSRLS`) yang dicek saat startup.
3. **Batas Ukuran Kode & Batas Modul**: Setiap berkas handler `<= 400` baris, setiap fungsi non-test `<= 60` baris, maks `45` env var (**43 aktif**), `22` tabel, `36` endpoint (`TestStructural_*` & `TestArchitecture_*`). Modul hanya boleh mengimpor `internal/<modul>/api`, bukan paket internal modul lain.
4. **Dependensi Minimal**: Hanya `github.com/google/uuid`, `github.com/jackc/pgx/v5`, dan `golang.org/x/crypto` plus Go standard library `net/http`.

---

## 2. Peta Paket & 8 Modul Domain

| Paket | Tanggung Jawab |
| :--- | :--- |
| `cmd/kopdes` | **Biner utama tunggal**: `serve` (HTTP API), `worker` (jadwal + `/healthz` `WORKER_PORT`), `uiserve` (UI statis + `/config.json`), `admin {create,reset-mfa,reset-password}`, `iuran generate`, `pesanan expire`, `pdp/bukti purge`, `storage migrate-files`. |
| `cmd/server` | Alias binary HTTP API (kompatibilitas `chaosdrill`). |
| `cmd/envcheck` | Validasi ketat **43 variabel env** (`--strict --ports --no-listen --compare`), menolak bentrok `WORKER_PORT`, wildcard CORS, atau default password di `production`. |
| `cmd/seed_test_accounts` | Seeder deterministik 5 akun uji sintetis (`admin`, `bendahara`, `petugas_toko`, 2x `anggota`) + katalog/iuran contoh untuk DB `*_test` non-produksi. |
| `cmd/uitester` | Wrapper penyaji UI statis (`frontend-wireframe/dist` atau `frontend-tester/`) lewat `internal/uiserve`. |
| `internal/config` | Loader dan validator ketat `AppConfig` tepat **43 variabel** (`WORKER_PORT`, `COOKIE_SECURE`, `ALLOW_BEARER_AUTH`, `STOCK_HOLD_MINUTES`, `DUAL_CONTROL_BREAKGLASS_ENABLED`, `STORAGE_BUKTI_DIR`, `BUKTI_RETENTION_DAYS`, `BACKUP_AGE_RECIPIENT`). |
| `internal/filestore` | Kontrak `FileStore` AES-256-GCM + amplop DEK (`NamespaceKYC` & `NamespaceBukti` terpisah). |
| `internal/uiserve` | Penyaji SPA + `/config.json` runtime, `/api/probe-cors`, `/api/totp-helper` (`*test*` saja). |
| `internal/security` | AES-256-GCM + AAD, derivasi 4 subkunci HKDF-SHA256 (`kdf.go`: `db-columns`, `kyc-files`, `bukti-files`, `backup`), amplop DEK seragam, blind index HKDF-SHA256, TOTP RFC 6238, recovery codes, netralisasi CSV. |
| `internal/api` | Server HTTP (`server.go`, `routes.go`, `runtime.go`), `CSRFGuard`, `CORS`, `HybridRateLimiter`, body limit, mode baca-saja, `TestRBAC_*` & `TestStructural_*`. |
| `internal/auth` | Sesi opaque (throttle 60s) dengan pencabutan instan, MFA TOTP 2-langkah, dan manajemen user (ekspor kontrak via `internal/auth/api`). |
| `internal/pengaturan` | Konfigurasi koperasi & pencatatan `audit_log` lintas modul (`internal/pengaturan/api`). |
| `internal/anggota` | Registrasi publik (`tanggal_lahir` opsional), `X-Upload-Token`, berkas KYC `.enc` di `STORAGE_KYC_DIR`, verifikasi admin, profil anggota (`unmask=true`), suspend + cabut sesi instan, impor CSV (`internal/anggota/api`). |
| `internal/iuran` | Jenis iuran (`KEPUTUSAN AKUNTAN`), generate tagihan bulanan idempoten, keringanan nominal (`internal/iuran/api`). |
| `internal/katalog` | Kategori, promo berkuota, produk dengan validasi `foto_url` & reservasi `stok_tertahan` via `internal/katalog/api`. |
| `internal/pesanan` | Checkout idempoten tanpa keranjang server; reservasi atomik `stok_tertahan` (`STOCK_HOLD_MINUTES`); pemenuhan & `daftar-antar` CSV aman (`internal/pesanan/api`). |
| `internal/pembayaran` | QRIS statis (+ kode unik), tunai, bukti `.enc` di `STORAGE_BUKTI_DIR`, dual-control + `breakglass` admin opsional, savepoint `perlu_tindak_lanjut`, & refund manual (`internal/pembayaran/api`); memanggil `iuran/api` & `pesanan/api` dalam `pgx.Tx` tunggal. |
| `internal/laporan` | Aggregator murni (**0 tabel** milik sendiri) membaca proyeksi laporan `anggota`, `tunggakan`, `penjualan`, `pembayaran` (JSON/CSV) lewat kontrak `*/api`. |
| `internal/worker` | Penjadwal tugas berkala ber-advisory-lock (`pg_try_advisory_lock`) + telemetri `worker_runs` (`/readyz`) + `/healthz` pada `WORKER_PORT`. |

---

## 3. Model Keamanan, Kunci Kriptografi, & Worker

- **Cookie `HttpOnly` Utama + `CSRFGuard`**: Saat `COOKIE_SECURE=auto`, cookie `__Host-kopdes_session` (`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`) diaktifkan jika `r.TLS != nil` atau `X-Forwarded-Proto: https` datang dari `TRUSTED_PROXIES`. Semua mutasi cookie wajib lolos pengecekan `Origin`/`Referer` + header `X-Requested-With`. `ALLOW_BEARER_AUTH=false` menyembunyikan `session_token` dari body login & menolak header Bearer (`401`).
- **Throttle Sesi & CLI Pemulihan**: Penulisan `sesi.last_active_at` dibatasi paling sering 1 kali per 60 detik, sementara pencabutan (`status_aktif=false`, `revoked_at`, suspend anggota) tetap berlaku seketika. Jika admin kehilangan perangkat MFA: `kopdes admin reset-mfa --email ...` dan `kopdes admin reset-password --email ... --password ...`.
- **HKDF & Penyimpanan Terpisah**: Subkunci diturunkan dari `AES_MASTER_KEY` per tujuan (`db-columns`, `kyc-files`, `bukti-files`, `backup`). Berkas KYC di `STORAGE_KYC_DIR` dan bukti bayar/refund di `STORAGE_BUKTI_DIR` menggunakan amplop DEK seragam. Jalankan `kopdes storage migrate-files` secara idempoten saat upgrade dari layout lama.
- **Backup `age` Terautentikasi**: [backup.sh](../scripts/backup.sh) mengarsipkan `database.dump` + `STORAGE_KYC_DIR` + `STORAGE_BUKTI_DIR` + `manifest.sha256` lalu mengenkripsi memakai **kunci publik `age`** (`BACKUP_AGE_RECIPIENT`). Private key dilarang berada di VPS produksi.

---

## 4. Verifikasi Harian & Keamanan

```powershell
sqlc generate
git diff --exit-code internal/db
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test.alt
go run ./cmd/envcheck --compare .env.test .env.test.alt
go build ./...
go vet ./...
go test -count=1 -race -p 1 ./...
staticcheck ./...
gosec ./...
govulncheck ./...
gitleaks git --config .gitleaks.toml --verbose --redact
```
