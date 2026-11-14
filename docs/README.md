# Indeks Dokumentasi — Kopdes Merah Putih API

**Versi Dokumen:** `1.1.0`  
**Tanggal Pembaruan:** `2026-11-14`  
**Ruang Lingkup:** Backend PostgreSQL 16 + Go (Single-Tenant Scope Lokal / Multi-Tenant RLS-Enforced per `koperasi_id`)

---

## Peta Baca Berdasarkan Peran ("Siapa Membaca Apa")

| Peran Pembaca | Urutan Baca yang Disarankan | Tujuan Utama |
|---|---|---|
| **Developer Frontend (Web / Mobile)** | 1. [FRONTEND_GUIDE.md](FRONTEND_GUIDE.md)<br>2. [openapi.yaml](openapi.yaml) & [roles_matrix.json](roles_matrix.json)<br>3. [kopdesClient.ts](kopdesClient.ts) & [kopdes.postman_collection.json](kopdes.postman_collection.json)<br>4. [Frontend Tester Polos](../frontend-tester/README.md) | Integrasi base URL berbasis env, alur MFA wajib, token single-flight refresh, `Idempotency-Key`, QRIS + kode unik, upload KYC, TypeScript SDK, dan uji interaktif langsung di browser |
| **Developer Backend & DevOps** | 1. [BACKEND_GUIDE.md](BACKEND_GUIDE.md)<br>2. [ENV_REFERENCE.md](ENV_REFERENCE.md)<br>3. [HASH_CHAIN_ADVISORY_LOCKS.md](HASH_CHAIN_ADVISORY_LOCKS.md)<br>4. [prometheus.example.yml](prometheus.example.yml) | Arsitektur RLS `WithTenantTx`, RBAC, hash chain & advisory lock, validasi `envcheck`, cadangan logis `pg_dump`, restore drill, deploy port fleksibel |
| **QA / Tester & Auditor Keamanan** | 1. [AUDIT_REPORT.md](AUDIT_REPORT.md)<br>2. [DOC_DISCREPANCIES.md](DOC_DISCREPANCIES.md)<br>3. [PROGRESS.md](PROGRESS.md)<br>4. [Frontend Tester Polos](../frontend-tester/README.md)<br>5. [QRIS_UJI_LAPANGAN.md](QRIS_UJI_LAPANGAN.md) & [SCOPE_RECONCILIATION.md](SCOPE_RECONCILIATION.md) | Bukti uji Set A vs Set B, hasil `envcheck` (83 variabel), pembuktian skenario & matriks peran (`439` cek), CI/CD GitHub Actions, uji beban 2.000 anggota + slot QRIS, dan celah tersisa |
| **Pemilik Proyek / Pengurus Koperasi / Akuntan / Legal** | 1. [TUGAS_MANUAL_PEMILIK.md](TUGAS_MANUAL_PEMILIK.md)<br>2. [BACKEND_GUIDE.md §11](BACKEND_GUIDE.md#11-panduan-kontribusi-utang-teknis--keputusan-manusia)<br>3. [AUDIT_REPORT.md](AUDIT_REPORT.md) | Pengisian rahasia & kredensial produksi (SMS/WA/S3/QRIS), konfirmasi akuntansi iuran (`KEPUTUSAN AKUNTAN`), dan tinjauan hukum retensi PDP (`PERLU VERIFIKASI HUKUM`) |

---

## Daftar Lengkap Berkas Dokumentasi

### Panduan Developer, Spesifikasi API & Frontend Tester
- [BACKEND_GUIDE.md](BACKEND_GUIDE.md) — Panduan developer backend (arsitektur, RLS, auth/MFA, QRIS, kriptografi UU 27/2022 PDP, hash chain, konfigurasi, runbook VPS, rotasi rahasia, cadangan `pg_dump` + status PITR, utang teknis).
- [FRONTEND_GUIDE.md](FRONTEND_GUIDE.md) — Panduan developer frontend (base URL env, akun uji sintetis, single-flight refresh, tabel kode error, `Idempotency-Key`, matriks 7 peran × 54 operasi, diagram status, contoh React+TS & cURL seluruh 48 path / 54 operasi, serta Frontend Tester Polos).
- [backend_integration_guide.md](backend_integration_guide.md) — Catatan integrasi awal dan profil aplikasi klien.
- [openapi.yaml](openapi.yaml) — Spesifikasi OpenAPI `3.0.3` lengkap seluruh 48 path (`54` operasi HTTP) + server berparameter variabel lingkungan (serta ekspor JSON otomatis di `frontend-tester/openapi.json`).
- [roles_matrix.json](roles_matrix.json) — Matriks kontrol akses mesin-terbaca untuk `54` operasi HTTP × `7` peran + `unauthenticated` + `7` probe `cross_tenant`.
- [kopdes.postman_collection.json](kopdes.postman_collection.json) — Koleksi Postman v2.1 hasil generasi otomatis dari `openapi.yaml` via `scripts/generate_api_artifacts.py`.
- [kopdesClient.ts](kopdesClient.ts) — Definisi tipe DTO TypeScript dan peta endpoint klien yang digenerasi dari `openapi.yaml`.
- [ENV_REFERENCE.md](ENV_REFERENCE.md) — Referensi otomatis seluruh **83 variabel** `.env` dan pemisahan dengan tabel database `konfigurasi_aturan`.
- [Frontend Tester Polos (`frontend-tester/README.md`)](../frontend-tester/README.md) — Petunjuk seed akun uji (`cmd/seed_test_accounts`), menjalankan `cmd/uitester` atau profil Docker `tester`, OpenAPI Explorer, alur terpandu `3.1`–`3.9`, Scenario Runner (`26` skenario), dan Roles Matrix Runner.

### Arsitektur, Skema & Operasional
- [erd.md](erd.md) — Diagram relasi entitas PostgreSQL 16 (tabel partisi, log append-only, RLS, indeks).
- [HASH_CHAIN_ADVISORY_LOCKS.md](HASH_CHAIN_ADVISORY_LOCKS.md) — Dokumen urutan kunci advisory lock deterministik untuk pencegahan deadlock dan keutuhan rantai hash.
- [prometheus.example.yml](prometheus.example.yml) — Contoh konfigurasi scrape Prometheus menuju endpoint `/metrics` berproteksi `METRICS_TOKEN`.

### Laporan Verifikasi, Ruang Lingkup & Tindak Lanjut Manusia
- [AUDIT_REPORT.md](AUDIT_REPORT.md) — Laporan audit konfigurasi (`.env.test`, `.env.test.alt`, `--compare`, `--ports`), keamanan statis, CI Set A & Set B, uji beban, dan restore drill.
- [DOC_DISCREPANCIES.md](DOC_DISCREPANCIES.md) — Log ketidaksesuaian dokumentasi vs perilaku kode nyata yang ditemukan saat audit dan pembuktian live v5 (`DISC-01` s.d. `DISC-11`).
- [PROGRESS.md](PROGRESS.md) — Log kronologis seluruh tugas pengembangan, perbaikan, dan bukti verifikasi mentah.
- [LAPORAN_REMEDIASI_DAN_PERUBAHAN_KODE.md](LAPORAN_REMEDIASI_DAN_PERUBAHAN_KODE.md) — Ringkasan riwayat remediasi kode inti.
- [SCOPE_RECONCILIATION.md](SCOPE_RECONCILIATION.md) — Rekonsiliasi ruang lingkup lokal cabang koperasi tunggal dan adapter stub SIMKOPDES.
- [QRIS_UJI_LAPANGAN.md](QRIS_UJI_LAPANGAN.md) — Prosedur uji lapangan QRIS statis/dinamis dan persetujuan ganda.
- [TUGAS_MANUAL_PEMILIK.md](TUGAS_MANUAL_PEMILIK.md) — Daftar lengkap tugas manual pemilik proyek (isi rahasia `.env`, daftar akun penyedia eksternal, `KEPUTUSAN AKUNTAN`, dan `PERLU VERIFIKASI HUKUM`).

### Artefak Terpaket (HTML / PDF / DOCX)
- [DOKUMENTASI_LENGKAP_API_KOPDES.html](DOKUMENTASI_LENGKAP_API_KOPDES.html)
- [DOKUMENTASI_LENGKAP_API_KOPDES.pdf](DOKUMENTASI_LENGKAP_API_KOPDES.pdf)
- [DOKUMENTASI_LENGKAP_API_KOPDES.docx](DOKUMENTASI_LENGKAP_API_KOPDES.docx)

*(Artefak HTML, PDF, dan DOCX di-generate ulang secara deterministik melalui `python scripts/generate_docs.py` setiap kali dokumentasi diperbarui.)*

---

## Verifikasi Otomatis Dokumentasi

Keakuratan dokumentasi dan perilaku server API diuji langsung oleh suite pengujian otomatis di [docs_verification_test.go](../internal/api/docs_verification_test.go), [docs_v5_parity_test.go](../internal/api/docs_v5_parity_test.go), dan [scenarios_and_matrix_e2e_test.go](../internal/api/scenarios_and_matrix_e2e_test.go):
1. Paritas 100% rute `internal/api/server.go` terhadap `docs/openapi.yaml`, `frontend-tester/openapi.json`, `docs/roles_matrix.json`, dan `docs/kopdes.postman_collection.json` (`TestDoc_OpenAPI_RouteParity`, `TestDoc_OpenAPI_JSON_Parity`, `TestDoc_RolesMatrix_Parity`).
2. Paritas 100% penanda `<!-- claim:ID -->` di `FRONTEND_GUIDE.md` & `BACKEND_GUIDE.md` terhadap skenario terautomasi `frontend-tester/scenarios.json` (`TestDoc_ClaimsAndScenariosParity`), validasi seluruh tautan relatif Markdown dan target `#anchor`, larangan klaim mutlak yang belum terverifikasi, serta larangan hardcode URL/port default pada kode contoh (`TestDoc_MarkdownLinks_Anchors_And_NoForbiddenClaims`).
3. Eksekusi langsung alur terdokumentasi dan matriks kontrol akses terhadap server API nyata (`TestDoc_EndToEnd_DocumentedWorkflows` dan `TestV5_LiveScenariosAndRolesMatrix_E2E`: `439` pemeriksaan matriks RBAC + `26` skenario/`102` langkah + pemindaian zero-PII).
