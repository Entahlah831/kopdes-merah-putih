# Dokumentasi Integrasi Frontend — Kopdes Merah Putih (Ramping)

Seluruh panduan integrasi frontend untuk versi modular (`v2.1.0-lean`: 1 koperasi tunggal, 21 tabel, 36 endpoint HTTP, **51 variabel env**, sesi cookie `HttpOnly` utama + `CSRFGuard`) merujuk ke satu sumber kebenaran berikut:

## Berkas Rujukan Utama
1. [Implementasi UI Referensi Wireframe Figma (`frontend-wireframe/`)](../../frontend-wireframe/) — Vite + TypeScript ketat (`0` dependencies runtime), autentikasi cookie HttpOnly, kanvas `<canvas>` KYC/bukti, & [LAPORAN_E2E.md](../wireframe/LAPORAN_E2E.md) (`4/4` Playwright E2E Set B & Set C).
2. [Inventaris 19 Layar Figma & Analisis Kesenjangan](../wireframe/ANALISIS_KESENJANGAN.md) — pemetaan tiap layar Figma ke OpenAPI & daftar fitur `BLOCKED: butuh keputusan pemilik` (`WF-DISC-01`).
3. [Panduan Integrasi Frontend Lengkap](../FRONTEND_GUIDE.md) — cookie `HttpOnly` utama + `credentials: "include"` + header wajib `X-Requested-With`, MFA TOTP 2-langkah, `GET /api/v1/public/config`, reservasi `stok_tertahan`, refund manual `POST /api/v1/pembayaran/{id}/refund`, `tanggal_lahir` opsional, serta matriks 4 peran terkini.
4. [Audit Keputusan Desain & ADR](../DECISION_AUDIT.md) — dasar teknis perubahan arsitektur D1–D10, [ADR-0001](../adr/0001-modular-monolith.md), & [ADR-0002](../adr/0002-filesvc-spike.md).
5. [Spesifikasi OpenAPI 3.0.3 (36 Endpoint)](../openapi.yaml)
6. [Matriks RBAC 4 Peran & Katalog 36 Rute](../roles_matrix.json)
7. [Frontend Tester Polos & 4 Halaman Referensi HTML Murni](../../frontend-tester/README.md)
8. [Katalog Variabel Lingkungan (51 Variabel)](../ENV_REFERENCE.md)

## Pemetaan Klaim ke Skenario Uji Live (`frontend-tester/scenarios.json`)
- `claim:01-public-config` -> `SCEN-01`
- `claim:02-auth-login-session` -> `SCEN-02` + `TestDecisionAudit_D3` / `D9`
- `claim:03-rbac-matrix` -> `SCEN-03` & `RBAC-01..19` + `TestRBAC_MatrixMatchesLiveRouterAndEnforcesBoundaries`
- `claim:04-pendaftaran-kyc` -> `SCEN-04`
- `claim:05-anggota-status` -> `SCEN-05`
- `claim:06-iuran-keringanan` -> `SCEN-06`
- `claim:07-katalog-produk` -> `SCEN-07`
- `claim:08-checkout-pesanan-daftar-antar` -> `SCEN-08` + `TestDecisionAudit_D1`
- `claim:09-pembayaran-qris-dual-control` -> `SCEN-09` + `TestDecisionAudit_D2` / `D7`
- `claim:10-laporan-csv` -> `SCEN-10`
- `claim:02-auth-lockout-optin` -> `SCEN-11` (opt-in)
