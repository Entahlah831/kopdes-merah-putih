# Dokumentasi Integrasi Frontend — Kopdes Merah Putih (Ramping)

Seluruh panduan integrasi frontend untuk versi ramping (1 koperasi tunggal, 35 endpoint, sesi opaque tanpa refresh token) telah dikonsolidasikan agar satu sumber kebenaran dan tidak memiliki bab usang multi-tenant.

## Berkas Rujukan Utama
1. [Panduan Integrasi Frontend Lengkap](../FRONTEND_GUIDE.md) — alur sesi opaque, MFA TOTP 2-langkah (`/auth/mfa/setup` & `/auth/mfa/verify`), `GET /api/v1/public/config`, checkout tanpa keranjang server, QRIS kode unik, bukti SHA-256, dan dual control.
2. [Spesifikasi OpenAPI 3.0.3 (35 Endpoint)](../openapi.yaml)
3. [Matriks RBAC 4 Peran](../roles_matrix.json)
4. [Frontend Tester Polos & 4 Halaman Referensi HTML Murni](../../frontend-tester/README.md)
5. [Katalog Variabel Lingkungan (40 Variabel)](../ENV_REFERENCE.md)

## Pemetaan Klaim ke Skenario Uji Live (`frontend-tester/scenarios.json`)
- `claim:01-public-config` -> `SCEN-01`
- `claim:02-auth-login-session` -> `SCEN-02`
- `claim:03-rbac-matrix` -> `SCEN-03` & `RBAC-01..16`
- `claim:04-pendaftaran-kyc` -> `SCEN-04`
- `claim:05-anggota-status` -> `SCEN-05`
- `claim:06-iuran-keringanan` -> `SCEN-06`
- `claim:07-katalog-produk` -> `SCEN-07`
- `claim:08-checkout-pesanan-daftar-antar` -> `SCEN-08`
- `claim:09-pembayaran-qris-dual-control` -> `SCEN-09`
- `claim:10-laporan-csv` -> `SCEN-10`
- `claim:02-auth-lockout-optin` -> `SCEN-11` (opt-in)
