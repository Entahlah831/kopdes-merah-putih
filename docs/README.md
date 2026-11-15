# Dokumentasi Backend Ramping — Kopdes Merah Putih

Repositori ini adalah backend API **lingkup satu koperasi di satu lokasi** berbasis **Go (`net/http` + `sqlc` + `pgx/v5`)** dan **PostgreSQL 16**.

> Seluruh port, URL, token, dan kunci kriptografi berasal dari berkas konfigurasi lingkungan (`.env.example`, `.env.test`, `.env.test.alt`) yang divalidasi oleh `cmd/envcheck`. Dilarang menanamkan nilai `.env` produksi atau nomor port secara statis di kode maupun dokumentasi.

## Peta Dokumen Utama

| Dokumen | Target Pembaca | Isi |
| :--- | :--- | :--- |
| [BACKEND_GUIDE.md](BACKEND_GUIDE.md) | Developer Backend & Ops | Arsitektur 8 domain (`internal/<domain>/`), 19 tabel PostgreSQL (`000001_init_kopdes_schema.up.sql`), `sqlc`, worker CLI (`cmd/kopdes`), enkripsi PII/KYC, backup/restore. |
| [FRONTEND_GUIDE.md](FRONTEND_GUIDE.md) | Developer Frontend & QA | Kontrak 35 endpoint, sesi opaque (`Authorization: Bearer` / cookie `kopdes_session`), alur 2-langkah MFA TOTP, `GET /api/v1/public/config`, QRIS kode unik, idempotensi, dan daftar klaim tester. |
| [frontend/README.md](frontend/README.md) | Frontend Engineer | Pintu masuk integrasi frontend & tautan ke Frontend Tester polos. |
| [openapi.yaml](openapi.yaml) | Backend, Frontend, QA | Spesifikasi OpenAPI 3.0.3 untuk seluruh 35 endpoint HTTP aktual. |
| [roles_matrix.json](roles_matrix.json) | Security & QA | Matriks otorisasi aktual untuk 4 peran inti (`admin`, `bendahara`, `petugas_toko`, `anggota`) dan publik. |
| [ENV_REFERENCE.md](ENV_REFERENCE.md) | Ops & Dev | Daftar tepat 40 variabel lingkungan yang diakui `cmd/envcheck`. |
| [DOC_DISCREPANCIES.md](DOC_DISCREPANCIES.md) | Semua Tim | Catatan ketidaksesuaian yang ditemukan oleh doc-test maupun verifikasi live browser beserta perbaikannya. |
| [TUGAS_MANUAL_PEMILIK.md](TUGAS_MANUAL_PEMILIK.md) | Pemilik Koperasi | Daftar periksa keputusan manual hukum, akuntansi, penyedia DNS/TLS/SMTP, dan verifikasi lapangan. |
| [SKILLS.md](SKILLS.md) | AI Coding Agent | Daftar 3 skill lokal aktif (`.agents/skills/`). |
| [PROGRESS.md](PROGRESS.md) | Audit Trail Teknis | Riwayat pengerjaan per fase lengkap beserta hasil perintah verifikasi. |
| [REVAMP_PLAN.md](REVAMP_PLAN.md) | Arsitektur | Rencana & target batas pemangkasan (`<= 20` tabel, `<= 35` endpoint, `<= 40` env, `<= 400` baris/handler, `<= 60` baris/fungsi). |
| [AUDIT_REPORT.md](AUDIT_REPORT.md) | Arsip Pra-Revamp | Catatan audit historis sebelum pemangkasan arsitektur ramping satu koperasi. |

## Menjalankan & Memverifikasi Secara Cepat

```powershell
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test
sqlc generate
go build ./...
go vet ./...
go test -count=1 -race -p 1 ./...
```
