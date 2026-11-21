# Dokumentasi Backend Ramping — Kopdes Merah Putih

Repositori ini adalah backend API **lingkup satu koperasi di satu lokasi** berbasis **Go (`net/http` + `sqlc` + `pgx/v5`)** dan **PostgreSQL 16**.

> Seluruh port, URL, token, dan kunci kriptografi berasal dari SATU sumber konfigurasi lingkungan (`.env.example`, `.env.test`, `.env.test.alt`, tepat **51 variabel**) yang divalidasi oleh `cmd/envcheck`. Dilarang menanamkan nilai `.env` produksi atau nomor port secara statis di kode maupun dokumentasi.

## Peta Dokumen Utama

| Dokumen | Target Pembaca | Isi |
| :--- | :--- | :--- |
| [../README.md](../README.md) | Semua Tim | Pintu masuk repositori, peta direktori, quickstart lokal & VPS (`kopdes serve` + `worker` + `age` backup), dan daftar perintah verifikasi. |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Arsitek & Developer | Peta **8 modul domain** (`internal/<modul>/api`), kepemilikan 21 tabel, tes batas arsitektur (`TestArchitecture_*`), alur transaksi lintas modul, & cara mengekstrak modul. |
| [MODULAR_REPORT.md](MODULAR_REPORT.md) | Pemilik, QA, Tech Lead | Laporan akhir Prompt v9: perbandingan Baseline vs Final, chaos, SAST, uji Set C port acak, & hasil Figma wireframe E2E. |
| [BASELINE_REPORT.md](BASELINE_REPORT.md) | Arsitek & QA | Laporan tolok ukur performa, keamanan, dan chaos Fase 0 sebelum modularisasi. |
| [adr/0001-modular-monolith.md](adr/0001-modular-monolith.md) & [adr/0002-filesvc-spike.md](adr/0002-filesvc-spike.md) | Arsitek & Tech Lead | ADR keputusan Modular Monolith (`Diterima`) dan hasil eksperimen terukur `spike/filesvc` (`Ditolak Sementara`). |
| [wireframe/](wireframe/) | Frontend, Desain, Pemilik | Inventaris 19 frame Figma ([INVENTARIS_LAYAR.md](wireframe/INVENTARIS_LAYAR.md)), pemetaan kesenjangan ([ANALISIS_KESENJANGAN.md](wireframe/ANALISIS_KESENJANGAN.md)), & bukti Playwright ([LAPORAN_E2E.md](wireframe/LAPORAN_E2E.md)). |
| [DECISION_AUDIT.md](DECISION_AUDIT.md) | Pemilik, Arsitek, Tech Lead | Laporan Audit Keputusan Desain D1–D10 (`DIPERTAHANKAN`/`DIUBAH`), bukti pengukuran nyata, dan daftar `KEPUTUSAN PEMILIK`. |
| [adr/](adr/) | Arsitek & Developer | Kumpulan ADR D1–D10 (`ADR-001`..`ADR-010`) + ADR Modular Monolith (`0001`) & Spike Filesvc (`0002`). |
| [PANDUAN_SISTEM_LENGKAP.md](PANDUAN_SISTEM_LENGKAP.md) | Pemilik, Ops, Backend, Frontend, QA | Panduan end-to-end terbaru: setup lokal & VPS (Docker + `worker` + Caddy HTTPS + `age`), fitur, integrasi frontend, 5 peran, skema 21 tabel DB & HKDF `.enc`. |
| [BACKEND_GUIDE.md](BACKEND_GUIDE.md) | Developer Backend & Ops | Arsitektur 8 modul domain (`internal/<modul>/api`) + `internal/worker`, 21 tabel PostgreSQL (`000001` & `000002`), `sqlc`, HKDF 4 subkunci, `HybridRateLimiter`, CLI pemulihan. |
| [FRONTEND_GUIDE.md](FRONTEND_GUIDE.md) | Developer Frontend & QA | Kontrak 36 endpoint, cookie `HttpOnly` utama (`__Host-kopdes_session`) + `CSRFGuard` (`X-Requested-With`), MFA 2-langkah, reservasi stok, & refund manual. |
| [frontend/README.md](frontend/README.md) | Frontend Engineer | Pintu masuk integrasi frontend (`frontend-wireframe/` & `frontend-tester/`) & pemetaan klaim ke skenario tester. |
| [openapi.yaml](openapi.yaml) | Backend, Frontend, QA | Spesifikasi OpenAPI 3.0.3 (`v2.1.0-lean`) untuk seluruh 36 endpoint HTTP aktual. |
| [roles_matrix.json](roles_matrix.json) | Security & QA | Katalog 36 rute + matriks otorisasi aktual 4 peran inti & publik (diuji otomatis terhadap router nyata). |
| [ENV_REFERENCE.md](ENV_REFERENCE.md) | Ops & Dev | Daftar tepat **51 variabel** lingkungan yang diakui `cmd/envcheck`. |
| [DOC_DISCREPANCIES.md](DOC_DISCREPANCIES.md) | Semua Tim | Catatan ketidaksesuaian yang ditemukan oleh doc-test maupun verifikasi live browser & wireframe beserta perbaikannya. |
| [TUGAS_MANUAL_PEMILIK.md](TUGAS_MANUAL_PEMILIK.md) | Pemilik Koperasi | Kewajiban pemilik (51 var `.env`, **private key `age` off-server**, pemulihan CLI admin, keputusan `BLOCKED` wireframe, hukum, & akuntansi). |
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
