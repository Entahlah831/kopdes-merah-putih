# Dokumentasi Backend Ramping — Kopdes Merah Putih

Repositori ini adalah backend API **lingkup satu koperasi di satu lokasi** berbasis **Go (`net/http` + `sqlc` + `pgx/v5`)** dan **PostgreSQL 16**.

> Seluruh port, URL, token, dan kunci kriptografi berasal dari berkas konfigurasi lingkungan (`.env.example`, `.env.test`, `.env.test.alt`) yang divalidasi oleh `cmd/envcheck`. Dilarang menanamkan nilai `.env` produksi atau nomor port secara statis di kode maupun dokumentasi.

## Peta Dokumen Utama

| Dokumen | Target Pembaca | Isi |
| :--- | :--- | :--- |
| [../README.md](../README.md) | Semua Tim | Pintu masuk repositori, peta direktori, quickstart lokal & VPS (`worker` bawaan + `age` backup), dan daftar perintah verifikasi. |
| [DECISION_AUDIT.md](DECISION_AUDIT.md) | Pemilik, Arsitek, Tech Lead | Laporan Audit Keputusan Desain D1–D10 (`DIPERTAHANKAN`/`DIUBAH`), bukti pengukuran nyata, dan daftar `KEPUTUSAN PEMILIK`. |
| [adr/](adr/) | Arsitek & Developer | 10 Architecture Decision Records (`ADR-001` s.d. `ADR-010`) lengkap dengan opsi alternatif, matriks skor berbobot, & risiko sisa. |
| [PANDUAN_SISTEM_LENGKAP.md](PANDUAN_SISTEM_LENGKAP.md) | Pemilik, Ops, Backend, Frontend, QA | Panduan end-to-end terbaru: setup lokal & VPS (Docker + `worker` + Caddy HTTPS + `age`), fitur, integrasi frontend, 5 peran, skema 21 tabel DB & HKDF `.enc`. |
| [BACKEND_GUIDE.md](BACKEND_GUIDE.md) | Developer Backend & Ops | Arsitektur 8 domain + `internal/worker`, 21 tabel PostgreSQL (`000001` & `000002`), `sqlc`, HKDF 4 subkunci, `HybridRateLimiter`, CLI pemulihan. |
| [FRONTEND_GUIDE.md](FRONTEND_GUIDE.md) | Developer Frontend & QA | Kontrak 36 endpoint, cookie `HttpOnly` utama (`__Host-kopdes_session`) + `CSRFGuard` (`X-Requested-With`), MFA 2-langkah, reservasi stok, & refund manual. |
| [frontend/README.md](frontend/README.md) | Frontend Engineer | Pintu masuk integrasi frontend & pemetaan klaim ke skenario tester. |
| [openapi.yaml](openapi.yaml) | Backend, Frontend, QA | Spesifikasi OpenAPI 3.0.3 (`v2.1.0-lean`) untuk seluruh 36 endpoint HTTP aktual. |
| [roles_matrix.json](roles_matrix.json) | Security & QA | Katalog 36 rute + matriks otorisasi aktual 4 peran inti & publik (diuji otomatis terhadap router nyata). |
| [ENV_REFERENCE.md](ENV_REFERENCE.md) | Ops & Dev | Daftar tepat 46 variabel lingkungan yang diakui `cmd/envcheck`. |
| [DOC_DISCREPANCIES.md](DOC_DISCREPANCIES.md) | Semua Tim | Catatan ketidaksesuaian yang ditemukan oleh doc-test maupun verifikasi live browser beserta perbaikannya. |
| [TUGAS_MANUAL_PEMILIK.md](TUGAS_MANUAL_PEMILIK.md) | Pemilik Koperasi | Kewajiban pemilik (46 var `.env`, **private key `age` off-server**, pemulihan CLI admin, hukum, & akuntansi). |
| [SKILLS.md](SKILLS.md) | AI Coding Agent | Daftar 3 skill lokal aktif (`.agents/skills/`). |
| [PROGRESS.md](PROGRESS.md) | Audit Trail Teknis | Riwayat pengerjaan per fase & hasil perintah verifikasi asli. |
| [REVAMP_REPORT.md](REVAMP_REPORT.md) | Audit & Manajemen | Laporan hasil pemangkasan arsitektur pra-audit keputusan. |
| [REVAMP_PLAN.md](REVAMP_PLAN.md) | Arsitektur | Rencana & anggaran awal pemangkasan (`lean-revamp`). |
| [AUDIT_REPORT.md](AUDIT_REPORT.md) | Arsip Pra-Revamp | Catatan audit historis sebelum pemangkasan arsitektur ramping. |

## Menjalankan & Memverifikasi Secara Cepat

```powershell
go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test
sqlc generate
go build ./...
go vet ./...
go test -count=1 -race -p 1 ./...
```
