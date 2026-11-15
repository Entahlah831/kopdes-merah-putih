# Laporan Akhir Lean Revamp — Kopdes Merah Putih (`lean-revamp`)

Dokumen ini merangkum hasil pemangkasan arsitektur, pengujian terotomatisasi, verifikasi live browser pada `frontend-tester/`, pemindaian keamanan, serta catatan tindak lanjut pemilik/hukum/akuntan untuk backend **Kopdes Merah Putih** pada branch `lean-revamp`.

---

## 1. Ringkasan Eksekutif & Perbandingan Metrik (Sebelum vs Sesudah)

Backend disederhanakan dari skema multi-tenant kompleks menjadi backend **1 koperasi tunggal di 1 lokasi** (anggota, pendaftaran + KYC terenkripsi, iuran bulanan QRIS statis/tunai, e-katalog + checkout tanpa keranjang server, pengantaran daftar antar, pengaturan, laporan) dengan kepatuhan batas struktural berikut:

| Metrik Arsitektur | Sebelum Revamp (Git `a5b0c52~1` / `0030de1^`) | Sesudah Lean Revamp (`HEAD`) | Batas Target | Status |
| :--- | :---: | :---: | :---: | :---: |
| Tabel PostgreSQL (migrasi aktif) | 69 tabel induk + 60 partisi (129 objek tabel fisik; 18 berkas migrasi) | **19 tabel** dalam 1 berkas migrasi | `<= 20` | **TERPENUHI** |
| Endpoint HTTP (`docs/openapi.yaml` & `routes.go`) | 55 operasi HTTP (`openapi.yaml`) | **35 endpoint HTTP** | `<= 35` | **TERPENUHI** |
| Variabel konfigurasi `.env` | 95 variabel (`.env.example`) | **40 variabel** | `<= 40` | **TERPENUHI** |
| Paket domain `internal/` | Multi-modul termasuk `outbox`, `qris`, `simkopdes` | **8 paket domain** + `api`, `config`, `db`, `security`, `testseed` | Fokus 1 koperasi | **TERPENUHI** |
| Dependensi langsung `go.mod` | Multi-modul + framework/tooling | **3 modul** (`uuid`, `pgx/v5`, `x/crypto`) pada `go 1.27.4` | Minimalis | **TERPENUHI** |
| Ukuran berkas handler | > 400 baris di beberapa handler | Maks **178 baris** (`internal/auth/handler.go`) | `<= 400` baris | **TERPENUHI** |
| Ukuran fungsi non-`internal/db/` | > 60 baris di beberapa fungsi | **100% `<= 60` baris** (`TestStructural_FileAndFunctionSizes_NoRawSQL`) | `<= 60` baris | **TERPENUHI** |
| Query SQL | Campuran raw SQL + helper | **100% via `sqlc`** (nol SQL mentah di luar `internal/db/`) | `sqlc` murni | **TERPENUHI** |

### Daftar 19 Tabel Inti ([db/migrations/000001_init_kopdes_schema.up.sql](../db/migrations/000001_init_kopdes_schema.up.sql))
`pengaturan`, `users`, `sesi`, `kode_aktivasi`, `rate_limits`, `anggota`, `pendaftaran`, `dokumen_kyc`, `jenis_iuran`, `tagihan_iuran`, `kategori`, `produk`, `promo`, `pesanan`, `pesanan_item`, `rekening_qris`, `pembayaran`, `pembayaran_alokasi`, `audit_log`.

### Struktur Paket Go Aktif
- **Domain (`internal/`)**: `anggota`, `auth`, `iuran`, `katalog`, `laporan`, `pembayaran`, `pendaftaran`, `pesanan`.
- **Infrastruktur (`internal/`)**: `api` (`handlers`, `middleware`, `response`), `config`, `db` (`sqlc`), `security`, `testseed`.
- **Binary (`cmd/`)**: `server`, `kopdes`, `envcheck`, `seed_test_accounts`, `uitester`.

---

## 2. Fitur Bloat yang Dihapus vs Kontrol Keamanan yang Dipertahankan

### Fitur Bloat yang Dihapus Permanen
- Multi-tenant (`koperasi_id`, `FORCE ROW LEVEL SECURITY` multi-koperasi, `fn_lookup_user_login`).
- Adapter & tabel `sinkronisasi_simkopdes`.
- Simpanan/pinjaman, SHU, dan buku besar *double-entry*.
- Keranjang belanja server-side (*cart*).
- Impor CSV mutasi bank & rekonsiliasi otomatis.
- Parser/generator QRIS dinamis EMVCo (`internal/qris`).
- Tabel `refresh_tokens` terpisah dan antrean `notifikasi` outbox DB (`internal/outbox`).
- Workspace TypeScript `frontend-reference/` serta 25 dokumen multi-tenant usang di `docs/frontend/`.

### Kontrol Keamanan yang Dipertahankan
- **Role DB Non-Superuser**: Aplikasi memakai role `kopdes_app` ([db/docker-init/01_roles.sql](../db/docker-init/01_roles.sql)) yang diverifikasi saat startup lewat `internal/db/role_guard.go`.
- **Autentikasi & Sesi Opaque**: Hash kata sandi memakai `bcrypt` melalui `golang.org/x/crypto/bcrypt` (`security.HashPasswordWithCost`, `CheckPassword`, `DummyBcryptCheck`). Token sesi disimpan sebagai hash SHA-256 (`security.HashOpaqueToken`) di tabel `sesi` dengan cookie `kopdes_session` (`HttpOnly`, `SameSite=Lax`, `Secure` aktif saat `r.TLS != nil`).
- **TOTP MFA Wajib 2-Langkah**: Diaktifkan untuk peran pada `MFA_REQUIRED_ROLES` (`admin`, `bendahara` pada Set A; `admin`, `bendahara`, `petugas_toko` pada Set B) dengan kode pemulihan satu kali pakai.
- **Enkripsi PII & Berkas KYC `.enc`**: AES-256-GCM dengan AAD serta *envelope encryption* (`security.EncryptEnvelopeFile`), rotasi kunci melalui `AES_PREVIOUS_KEYS`, blind index HMAC-SHA256 (`security.HashBlindIndex`), validasi *magic bytes*, `X-Content-Type-Options: nosniff`, dan pembatasan ukuran request melalui `http.MaxBytesReader`.
- **Proteksi Jaringan & Brute-Force**: Anti-spoof `X-Forwarded-For` (`TRUSTED_PROXIES`), DB rate limiting (`rate_limits`), dan penguncian akun gagal login (`LOGIN_MAX_ATTEMPTS`, `LOGIN_LOCKOUT_DURATION`).
- **Dual Control & Anti-Duplikasi Pembayaran**: Indeks unik QRIS aktif (`idx_pembayaran_qris_unik_aktif`), hash bukti unik (`bukti_hash`), `idempotency_key` unik, dan penolakan *self-approval* oleh pembuat pembayaran (`DUAL_CONTROL_VIOLATION` di [internal/pembayaran/verifikasi.go](../internal/pembayaran/verifikasi.go)).
- **Audit Log Immutable**: Tabel `audit_log` dilindungi trigger `trg_audit_log_immutable` (`BEFORE UPDATE OR DELETE` memanggil `trg_fn_reject_audit_mutation()`).

---

## 3. Bukti Verifikasi Live Browser (`frontend-tester/`)

Verifikasi antarmuka uji polos (`frontend-tester/`, tanpa framework, tanpa build step, tanpa CDN) dijalankan langsung terhadap server API Set A (`API_PORT=8080`) dan UI Tester (`UI_PORT=8081`):

- **Diagnostics**: `/healthz -> HTTP 200`, `/readyz -> HTTP 200`, `/api/v1/public/config -> HTTP 200 api_version=2.0.0-lean`, `CORS probe origin=http://127.0.0.1:8081 -> HTTP 204 allow_origin=http://127.0.0.1:8081`.
- **Live Demo (`DEMO-01`..`DEMO-05`)**: `Live Demo selesai: 5/5 langkah PASS.` (`detail: ""`).
- **Scenario Runner (`SCEN-01`..`SCEN-10`, plus `SCEN-11` opt-in)**: `Scenario Runner selesai: 10/10 skenario dijalankan PASS.` (`SCEN-11` opt-in dilewati bersih, `detail: ""`).
- **Role Matrix (`RBAC-01`..`RBAC-16` × 5 peran)**: `Role Matrix selesai: 80/80 pemeriksaan cocok.` (`detail: ""`).

---

## 4. Bukti Output Asli Perintah Verifikasi (Fase 9)

Seluruh perintah verifikasi di bawah ini dijalankan pada kondisi kode terbaru (`627792d`) terhadap database PostgreSQL 16 terisolasi **Set A** (`kopdes_test`, port `5433`) dan **Set B** (`kopdes_alt_test`, port `15433`):

### 4.1 `sqlc generate`, Build, Vet, & Structural Lint
```text
$ sqlc generate && git diff --exit-code
sqlc+diff exit=0

$ go version
go version go1.27.4 windows/amd64

$ go build ./... && go vet ./...
# Command exited with code 0

$ go test -count=1 -v ./internal/api -run TestStructural
=== RUN   TestStructural_LeanBudgetsAndLimits
--- PASS: TestStructural_LeanBudgetsAndLimits (0.00s)
=== RUN   TestStructural_FileAndFunctionSizes_NoRawSQL
--- PASS: TestStructural_FileAndFunctionSizes_NoRawSQL (0.05s)
PASS
ok  	github.com/kopdes/merah-putih/internal/api	0.178s
```

### 4.2 Validasi Lingkungan (`envcheck` Set A, Set B, & Parity Check)
```text
$ go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test
envcheck PORTS PASS: tidak ada konflik port pada konfigurasi.
envcheck STRICT PASS: 40 variabel terverifikasi, APP_ENV=test, DB_NAME=kopdes_test, API_PORT=8080

$ go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test.alt
envcheck PORTS PASS: tidak ada konflik port pada konfigurasi.
envcheck STRICT PASS: 40 variabel terverifikasi, APP_ENV=test, DB_NAME=kopdes_alt_test, API_PORT=18080

$ go run ./cmd/envcheck --compare .env.test .env.test.alt
# Seluruh nilai sensitif otomatis disamarkan menjadi ***REDACTED***
envcheck COMPARE PASS: skema kunci 100% identik (40 kunci).
```

### 4.3 Pengujian Penuh Race Detector (`21` Paket Go)
```text
$ go test -count=1 -race -p 1 ./...
ok  	github.com/kopdes/merah-putih/cmd/envcheck	1.407s
ok  	github.com/kopdes/merah-putih/cmd/kopdes	1.809s
ok  	github.com/kopdes/merah-putih/cmd/seed_test_accounts	1.567s
ok  	github.com/kopdes/merah-putih/cmd/server	1.574s
ok  	github.com/kopdes/merah-putih/cmd/uitester	2.124s
ok  	github.com/kopdes/merah-putih/internal/anggota	1.805s
ok  	github.com/kopdes/merah-putih/internal/api	26.987s
ok  	github.com/kopdes/merah-putih/internal/api/handlers	1.661s
ok  	github.com/kopdes/merah-putih/internal/api/middleware	1.867s
ok  	github.com/kopdes/merah-putih/internal/api/response	1.677s
ok  	github.com/kopdes/merah-putih/internal/auth	1.603s
ok  	github.com/kopdes/merah-putih/internal/config	1.534s
ok  	github.com/kopdes/merah-putih/internal/db	2.250s
ok  	github.com/kopdes/merah-putih/internal/iuran	1.804s
ok  	github.com/kopdes/merah-putih/internal/katalog	1.572s
ok  	github.com/kopdes/merah-putih/internal/laporan	1.830s
ok  	github.com/kopdes/merah-putih/internal/pembayaran	1.843s
ok  	github.com/kopdes/merah-putih/internal/pendaftaran	1.988s
ok  	github.com/kopdes/merah-putih/internal/pesanan	1.543s
ok  	github.com/kopdes/merah-putih/internal/security	1.385s
ok  	github.com/kopdes/merah-putih/internal/testseed	1.559s
```

### 4.4 Pemindaian Keamanan & Kualitas (`staticcheck`, `gosec`, `govulncheck`, `gitleaks`)
```text
$ staticcheck ./...
staticcheck exit=0

$ gosec -terse ./...
Results:

Summary:
  Gosec  : dev
  Files  : 60
  Lines  : 14398
  Nosec  : 37
  Issues : 0

gosec exit=0

$ govulncheck ./...
=== Symbol Results ===

No vulnerabilities found.

Your code is affected by 0 vulnerabilities.
This scan also found 0 vulnerabilities in packages you import and 1
vulnerability in modules you require, but your code doesn't appear to call these
vulnerabilities.
Use '-show verbose' for more details.
govulncheck exit=0

$ gitleaks git --config .gitleaks.toml --verbose --redact
6:49PM INF 98 commits scanned.
6:49PM INF scanned ~3244982 bytes (3.24 MB) in 5.8s
6:49PM INF no leaks found
gitleaks exit=0
```

### 4.5 Smoke Test CLI (`cmd/kopdes`) & Seeder Set A
```text
$ go run ./cmd/kopdes --env-file .env.test iuran generate --periode 2099-01
Tagihan 2099-01: dibuat=5, dilewati=0

$ go run ./cmd/kopdes --env-file .env.test pesanan expire
Kedaluwarsa selesai: pesanan=0, pembayaran=1

$ go run ./cmd/kopdes --env-file .env.test pdp purge
PDP purge selesai: 0 pendaftaran ditolak kedaluwarsa dihapus

$ go run ./cmd/seed_test_accounts --env-file .env.test
Seeded 5 akun uji sintetis ke database kopdes_test (postgres://kopdes_app:%2A%2A%2A%2A%2A%2A@127.0.0.1:5433/kopdes_test?sslmode=disable) selesai.
```

### 4.6 Penyempurnaan Penyimpanan Bukti Terenkripsi, Potong Stok saat Verifikasi, & Dokumentasi Akhir (`22e49ce`)
- **Pemisahan & Enkripsi Bukti Pembayaran (`22e49ce`)**: Berkas bukti pembayaran kini dipisahkan ke subdirektori `<STORAGE_KYC_DIR>/bukti/bukti_<id>.enc` dan dienkripsi menggunakan AES-256-GCM dengan AAD `pembayaran:bukti:<id>`, terpisah dari berkas KYC di `<STORAGE_KYC_DIR>/kyc/` (dengan fallback baca berkas plaintext/lokasi lama untuk kompatibilitas migrasi).
- **Pemotongan Stok saat Pembayaran Disetujui (`22e49ce`)**: `Checkout` tetap memvalidasi ketersediaan stok namun menunda pemotongan stok atomik hingga pembayaran QRIS diverifikasi `setuju` oleh Admin/Bendahara atau pesanan tunai masuk ke status `dibayar`/`diproses`; pembatalan hanya memulihkan stok apabila stok sudah terpotong.
- **Dokumentasi Akhir Lengkap**: Ditambahkan [README.md](../README.md) di root repositori serta [PANDUAN_SISTEM_LENGKAP.md](PANDUAN_SISTEM_LENGKAP.md) yang merangkum setup lokal & VPS (Docker + Caddy HTTPS), pemasangan fitur, integrasi frontend, alur lengkap 5 peran, dan skema 19 tabel PostgreSQL.

---

## 5. Catatan Regulasi, Akuntansi, & Tindak Lanjut Pemilik

1. **`PERLU VERIFIKASI HUKUM`**: Masa retensi data pendaftaran ditolak (`PDP_REJECTED_RETENTION_DAYS=30`) menggunakan penghapusan baris database dan berkas terenkripsi (bukan *crypto-shredding*); kecukupan jangka waktu 30 hari serta kepatuhan penuh terhadap UU PDP No. 27 Tahun 2022 wajib diverifikasi penasihat hukum atau pengurus koperasi.
2. **`KEPUTUSAN AKUNTAN`**: Nilai default kolom `jenis_iuran.klasifikasi_akuntansi` dan `pembayaran.catatan_akuntansi` (`'KEPUTUSAN AKUNTAN'`) wajib ditetapkan klasifikasinya oleh akuntan/pengurus koperasi (misalnya perlakuan iuran wajib/sukarela serta selisih kode unik QRIS).
3. **Tugas Manual Pemilik**: Seluruh langkah pengisian `.env` produksi asli, rotasi kunci, pembuatan admin awal, serta jadwal cron dan backup terenkripsi (`scripts/backup.sh`, dengan catatan arsip WAL/PITR belum diimplementasikan) dirangkum di [TUGAS_MANUAL_PEMILIK.md](TUGAS_MANUAL_PEMILIK.md).
