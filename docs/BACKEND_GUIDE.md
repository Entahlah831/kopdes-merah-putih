# Panduan Developer Backend — Kopdes Merah Putih (Ramping)

Target Runtime: `Go 1.27+` | `PostgreSQL 16` | Kontrak API: `v2.1.0-lean` (`36` endpoint, `21` tabel, `46` variabel env).  
Hasil Audit Keputusan Desain D1–D10 tersedia di [DECISION_AUDIT.md](DECISION_AUDIT.md) dan [adr/](adr/).

> **Catatan Kepatuhan & Akuntansi**: Backend ini menerapkan kontrol keamanan teknis sesuai baseline OWASP dan prinsip minimalisasi data (`tanggal_lahir` opsional). Penetapan masa retensi dokumen calon anggota yang ditolak (`PDP_REJECTED_RETENTION_DAYS=30`) dan bukti non-lunas (`BUKTI_RETENTION_DAYS=365`) ditandai **PERLU VERIFIKASI HUKUM**, dan kolom `klasifikasi_akuntansi` / selisih kode unik / koreksi refund ditandai **KEPUTUSAN AKUNTAN**.

---

## 1. Prinsip & Anggaran Arsitektur Ramping

Backend dirancang **khusus untuk 1 koperasi di 1 lokasi** (tanpa multi-tenant RLS, tanpa SIMKOPDES, tanpa simpanan/pinjaman/SHU, tanpa double-entry ledger, tanpa keranjang server, dan tanpa impor CSV mutasi bank):

1. **Basis Data**: `21` tabel lintas migrasi [000001_init_kopdes_schema.up.sql](../db/migrations/000001_init_kopdes_schema.up.sql) dan [000002_decision_audit_enhancements.up.sql](../db/migrations/000002_decision_audit_enhancements.up.sql) (+ tabel #20 `pengembalian_dana`, tabel #21 `worker_runs`, kolom `stok_tertahan`, `stok_status`, `stok_hold_expires_at`, `bukti_key_id`, `bukti_dek_wrapped`, dan `tanggal_lahir_enc` nullable). Seluruh query diketik via **`sqlc`** ke [internal/db/](../internal/db/). Dilarang menulis SQL mentah atau mengedit `internal/db/*.go` manual.
2. **Keamanan Role Database**: Koneksi aplikasi wajib memakai role non-superuser `kopdes_app` (`NOSUPERUSER NOBYPASSRLS`) yang dicek saat startup.
3. **Batas Ukuran Kode**: Setiap berkas handler `<= 400` baris, setiap fungsi non-test `<= 60` baris, maks `46` env var, `22` tabel, `36` endpoint (`TestStructural_FileAndFunctionSizes_NoRawSQL`, `TestStructural_LeanBudgetsAndLimits`).
4. **Dependensi Minimal**: Hanya `github.com/google/uuid`, `github.com/jackc/pgx/v5`, dan `golang.org/x/crypto` plus Go standard library `net/http`.

---

## 2. Peta Paket & Domain

| Paket | Tanggung Jawab |
| :--- | :--- |
| `cmd/server` | Server HTTP API 36 endpoint, verifikasi `kopdes_app` saat startup, dan config guard. |
| `cmd/kopdes` | CLI pemeliharaan & pemulihan: `admin create`, `admin reset-mfa`, `admin reset-password`, `iuran generate`, `pesanan expire`, `pdp purge`, `bukti purge`, `storage migrate-files`, `worker [--tick 60s] [--once]`. |
| `cmd/envcheck` | Validasi ketat 46 variabel env (`--strict --ports --no-listen --compare`), menolak wildcard CORS / default password pada profil `production`. |
| `cmd/seed_test_accounts` | Seeder deterministik 5 akun uji sintetis (`admin`, `bendahara`, `petugas_toko`, 2x `anggota`) + katalog/iuran contoh untuk DB `*_test` non-produksi. |
| `cmd/uitester` | Server UI tester lokal non-produksi (`frontend-tester/`, `/config.json`, `/api/probe-cors`, `/api/totp-helper`). |
| `internal/config` | Loader dan validator ketat `AppConfig` tepat 46 variabel (`COOKIE_SECURE`, `ALLOW_BEARER_AUTH`, `STOCK_HOLD_MINUTES`, `DUAL_CONTROL_BREAKGLASS_ENABLED`, `STORAGE_BUKTI_DIR`, `BUKTI_RETENTION_DAYS`, `BACKUP_AGE_RECIPIENT`). |
| `internal/security` | AES-256-GCM + AAD, derivasi 4 subkunci HKDF-SHA256 (`kdf.go`: `db-columns`, `kyc-files`, `bukti-files`, `backup`), amplop DEK seragam, blind index HKDF-SHA256, TOTP RFC 6238, recovery codes, netralisasi CSV. |
| `internal/api` | Server HTTP (`server.go`, `routes.go`), `CSRFGuard` (Origin/Referer + `X-Requested-With`), `CORS` (`Vary: Origin`), `HybridRateLimiter` (LRU L1 + persistensi ambang `rate_limits` L2, tanpa ticker goroutine liar), body limit, mode baca-saja, & `TestRBAC_MatrixMatchesLiveRouterAndEnforcesBoundaries`. |
| `internal/auth` | Cookie `HttpOnly` utama (`__Host-kopdes_session` HTTPS / `kopdes_session` dev), Bearer opsional (`ALLOW_BEARER_AUTH`), throttle `TouchSessionActivity` <= 1x/60s dengan pencabutan seketika, MFA TOTP 2-langkah, `PUT /api/v1/pengaturan`, users. |
| `internal/pendaftaran` | Pendaftaran publik (`tanggal_lahir` opsional), `X-Upload-Token` wajib, berkas KYC `.enc` di `STORAGE_KYC_DIR` (subkunci `kyc-files` + amplop DEK), verifikasi admin. |
| `internal/anggota` | Profil anggota (`unmask=true` khusus `admin`), suspend + cabut sesi seketika, impor CSV (`tanggal_lahir` opsional). |
| `internal/iuran` | Jenis iuran (`KEPUTUSAN AKUNTAN`), generate tagihan bulanan idempoten, keringanan nominal. |
| `internal/katalog` | Kategori, promo berkuota, produk dengan validasi `foto_url` (`https://` / `/...`, maks 512 karakter) & kalkulasi `stok_tersedia = stok - stok_tertahan`. |
| `internal/pesanan` | Checkout idempoten tanpa keranjang server; reservasi atomik `stok_tertahan` berlaku `STOCK_HOLD_MINUTES`; status pemenuhan (`admin`/`petugas_toko` saja — `bendahara` ditolak `403`); `daftar-antar` CSV aman. |
| `internal/pembayaran` | QRIS statis (+ kode unik 3/4 digit), tunai (`admin`/`bendahara` saja — `petugas_toko`/`anggota` ditolak `403`), bukti terenkripsi amplop di `STORAGE_BUKTI_DIR` (subkunci `bukti-files`), dual-control + `breakglass` admin opsional (`DUAL_CONTROL_BREAKGLASS_ENABLED`), fallback savepoint `perlu_tindak_lanjut` bila stok habis pasca-hold, & refund manual `POST /api/v1/pembayaran/{id}/refund`. |
| `internal/laporan` | Laporan `anggota`, `tunggakan`, `penjualan`, `pembayaran` (JSON/CSV) termasuk `total_dikembalikan` dari tabel `pengembalian_dana`. |
| `internal/worker` | Penjadwalan bawaan (`kopdes worker`) ber-advisory-lock per koneksi tunggal (`pg_try_advisory_lock`/`pg_advisory_unlock`) untuk 5 job berkala + telemetri `worker_runs` ke `/readyz`. |

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
