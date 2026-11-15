# Panduan Developer Backend — Kopdes Merah Putih (Ramping)

Target Runtime: `Go 1.26+` | `PostgreSQL 16` | Kontrak API: `v2.0.0-lean` (`35` endpoint, `19` tabel, `40` variabel env).

> **Catatan Kepatuhan & Akuntansi**: Backend ini menerapkan kontrol keamanan teknis sesuai baseline OWASP dan prinsip minimalisasi data. Penetapan masa retensi dokumen calon anggota yang ditolak (`PDP_REJECTED_RETENTION_DAYS`, default `30` hari) ditandai **PERLU VERIFIKASI HUKUM**, dan kolom `klasifikasi_akuntansi` pada jenis iuran ditandai **KEPUTUSAN AKUNTAN**.

---

## 1. Prinsip & Anggaran Arsitektur Ramping

Backend dirancang **khusus untuk 1 koperasi di 1 lokasi** (tanpa multi-tenant RLS, tanpa SIMKOPDES, tanpa simpanan/pinjaman/SHU, tanpa double-entry ledger, tanpa keranjang server, dan tanpa impor CSV mutasi bank):

1. **Basis Data**: `19` tabel inti di [000001_init_kopdes_schema.up.sql](../db/migrations/000001_init_kopdes_schema.up.sql), seluruh query diketik melalui **`sqlc`** ke [internal/db/](../internal/db/). Dilarang menulis SQL mentah di handler/service atau menyunting berkas hasil generasi `internal/db/*.go` secara manual.
2. **Keamanan Role Database**: Koneksi aplikasi wajib memakai role non-superuser `kopdes_app` (`NOSUPERUSER NOBYPASSRLS`) yang dicek saat startup oleh server.
3. **Batas Ukuran Kode**: Setiap berkas handler `<= 400` baris, setiap fungsi `<= 60` baris (diverifikasi otomatis oleh `TestStructural_FileAndFunctionSizes_NoRawSQL`).
4. **Dependensi Minimal**: Hanya `github.com/google/uuid`, `github.com/jackc/pgx/v5`, dan `golang.org/x/crypto` plus Go standard library `net/http`.

---

## 2. Peta Paket & Domain

| Paket | Tanggung Jawab |
| :--- | :--- |
| `cmd/server` | Entrypoint server API 35 endpoint, validasi role non-superuser DB, dan startup config check. |
| `cmd/kopdes` | CLI pemeliharaan operasional: `iuran generate --periode YYYY-MM`, `pesanan expire`, dan `pdp purge`. |
| `cmd/envcheck` | Validasi ketat 40 variabel env (`--strict --ports --no-listen --compare`), menolak wildcard CORS / default password pada profil `production`. |
| `cmd/seed_test_accounts` | Seeder deterministik 5 akun uji sintetis (`admin`, `bendahara`, `petugas_toko`, 2x `anggota`) + katalog/iuran contoh untuk DB `*_test` non-produksi. |
| `cmd/uitester` | Server UI tester lokal non-produksi (menyajikan `frontend-tester/`, `/config.json`, `/api/probe-cors`, dan `/api/totp-helper`). |
| `internal/config` | Loader dan validator ketat `AppConfig` tepat 40 variabel tanpa default diam-diam yang berbahaya di produksi. |
| `internal/security` | Enkripsi AES-256-GCM + AAD per entitas/kolom, blind index HKDF-SHA256, opaque token hash SHA-256, TOTP RFC 6238, recovery code hash, netralisasi formula CSV. |
| `internal/api` | Perakitan server HTTP (`server.go`, `routes.go`), middleware keamanan, pembatas laju (in-memory + DB), CORS, body limit, dan mode baca-saja (`READ_ONLY_MODE`). |
| `internal/auth` | Login, sesi opaque (idle & absolute TTL), MFA TOTP 2-langkah, kata sandi/aktivasi, pengaturan koperasi (`PUT /api/v1/pengaturan`), manajemen user admin. |
| `internal/pendaftaran` | Pendaftaran publik + proteksi `X-Upload-Token`, penyimpanan berkas KYC terenkripsi `.enc` setelah validasi magic bytes & `image.DecodeConfig`, verifikasi admin. |
| `internal/anggota` | Daftar & detail anggota (NIK/HP tersamar secara default; `?unmask=true` khusus `admin` + audit log), suspend (mencabut seluruh sesi aktif), impor CSV (`?dry_run=true`). |
| `internal/iuran` | Jenis iuran, pembuatan tagihan bulanan idempoten, daftar tagihan & tunggakan, keringanan nominal. |
| `internal/katalog` | Kategori, promo, dan produk dengan penentuan tier harga otomatis (`harga_anggota` vs `harga_umum`). |
| `internal/pesanan` | Checkout tanpa keranjang server (idempoten lewat `Idempotency-Key`), reservasi stok atomik, siklus status pesanan, `daftar-antar` + ekspor CSV aman. |
| `internal/pembayaran` | Instruksi pembayaran QRIS statis (+ probe kode unik 3/4 digit) atau tunai, unggah bukti gambar dengan deteksi duplikat SHA-256, verifikasi dual control (`dibuat_oleh != claims.UserID`). |
| `internal/laporan` | Laporan operasional (`anggota`, `tunggakan`, `penjualan`, `pembayaran`) format JSON maupun CSV anti formula injection. |

---

## 3. Model Keamanan & Kriptografi Penting

- **Autentikasi Sesi Opaque**: Login menerbitkan token acak CSPRNG 32-byte (`base64url`); database hanya menyimpan `SHA-256(token)`. Middleware memperbarui aktivitas idle (`SESSION_IDLE_TTL`) dalam batas `SESSION_ABS_TTL`.
- **MFA TOTP (RFC 6238) + Anti-Replay**: Wajib bagi peran dalam `MFA_REQUIRED_ROLES` (default: `admin,bendahara`). Menyimpan `mfa_last_step` sehingga kode TOTP yang sama tidak dapat diputar ulang pada jendela waktu yang sama.
- **PII & Berkas KYC**: NIK, No. HP, Tanggal Lahir, Alamat dienkripsi AES-256-GCM dengan AAD; berkas KYC disimpan dengan DEK terbungkus di direktori non-publik (`STORAGE_KYC_DIR`) dalam format `.enc`.
- **Anti-Fraud Pembayaran**:
  - `ALLOW_PERSONAL_PAYMENT_PILOT` default `false`. Bila diaktifkan secara sadar oleh pemilik, transaksi ke rekening `perorangan` tetap dibatasi oleh `PILOT_MAX_PER_TX` dan `PILOT_MAX_PER_DAY`.
  - Unggahan bukti identik ditolak `409 Conflict` berdasar hash SHA-256 berkas.
  - Verifikator pembayaran (`POST /api/v1/pembayaran/{id}/verifikasi`) **dilarang keras** sama dengan `dibuat_oleh` (*Dual Control*).

---

## 4. Pekerjaan Terjadwal CLI (`cmd/kopdes`)

Jalankan secara periodik lewat cron/systemd timer atau task scheduler sesuai konfigurasi `.env`:

```powershell
go run ./cmd/kopdes --env-file <path_env> iuran generate --periode 2026-12
go run ./cmd/kopdes --env-file <path_env> pesanan expire
go run ./cmd/kopdes --env-file <path_env> pdp purge
```

> Catatan `pdp purge`: Menghapus baris database pendaftaran berstatus `ditolak` beserta berkas KYC `.enc` di disk setelah melewati `PDP_REJECTED_RETENTION_DAYS` hari (*hard delete*, bukan *crypto-shredding*). Angka 30 hari berstatus **PERLU VERIFIKASI HUKUM**.

---

## 5. Verifikasi Harian & Keamanan

```powershell
sqlc generate
git diff --exit-code
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
