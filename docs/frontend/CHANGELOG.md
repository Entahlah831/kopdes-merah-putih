# Catatan Perubahan Kontrak & Dokumentasi Frontend (`docs/frontend/`)

Semua perubahan kontrak JSON, header, status fitur, atau panduan integrasi frontend dicatat di sini.

## [1.1.0] - 2026-11-15

### Ditambahkan
- Endpoint publik tanpa autentikasi `GET /api/v1/public/config` (`Cache-Control: public, max-age=60`) berisi metadata non-rahasia: `api_version`, `server_time`, `server_unix_ms`, `timezone`, `upload_limits`, `kyc_required_document_types`, `kyc_supported_document_types`, `payment_expiry_seconds`, `polling_interval_ms`, `features`, `captcha`, `mfa_mandatory_roles`, `access_token_ttl_seconds`, dan `pagination`.
- Pengaman fitur runtime (`PAYMENTS_ENABLED`, `REGISTRATION_ENABLED`, `LOGIN_ENABLED`) dan mode pemeliharaan (`READ_ONLY_MODE`) dengan kode error resmi `FEATURE_DISABLED` (`503`) dan `READ_ONLY_MODE` (`503`).
- Paket dokumentasi terstruktur `docs/frontend/` (21 bab bertemplat seragam, kamus error `pesan-error-id.json`, skema `frontend.config.schema.json`, dan `config.example.json`).
- SDK TypeScript referensi terisolasi zero-runtime-dependency di [frontend-reference/](../../frontend-reference/) lengkap dengan contoh React, Vue 3, dan Vanilla JS.
- Perluasan Frontend Tester polos di [frontend-tester/](../../frontend-tester/) menjadi **Lab Belajar** interaktif beserta halaman referensi minimal di `frontend-tester/reference/`.

### Diperbaiki
- Seluruh 15 `error.code` dari `internal/api/` terdokumentasi 1-to-1 termasuk `MFA_SESSION_LOCKED` (HTTP `429`), `MFA_ENROLLMENT_REQUIRED` (HTTP `403`), dan `PAYLOAD_TOO_LARGE` (HTTP `413`).
- Penyelarasan status awal `POST /api/v1/toko/checkout` (`"status": "draft"`) serta parameter wajib `GET /api/v1/katalog/harga?produk_id=<UUID>&anggota_id=<UUID>`.

## [1.0.0] - 2026-11-13

### Dirilis
- Dokumentasi tunggal awal `docs/FRONTEND_GUIDE.md`, `docs/openapi.yaml`, `docs/roles_matrix.json`, serta Frontend Tester v5 (`26` skenario klaim dan matriks peran `54` operasi).
