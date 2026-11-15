# 19. Deploy, CORS, Reverse Proxy, BFF & Mode Pemeliharaan

Panduan ini ditujukan bagi Tech Lead & DevOps untuk menyiapkan lingkungan pengembangan, pengujian terpisah (*Set A / Set B*), staging, hingga produksi tanpa membuka celah keamanan.

---

## 1. Perbedaan Lingkungan & Larangan Hardcode

| Lingkungan | Sumber Konfigurasi | Catatan Kunci |
|---|---|---|
| **Local Dev / Tester (Set A)** | `.env.test` + `cmd/uitester` | Port dari `API_HOST_PORT` & `UI_PORT`; CORS mengizinkan origin lokal persis. |
| **Stack Uji Alternatif (Set B)** | `.env.test.alt` | Port berbeda sepenuhnya untuk uji paralel CI Linux tanpa benturan Set A. |
| **Staging / Produksi** | Env CI/CD + Reverse Proxy HTTPS | `APP_ENV=production`: server **menolak startup** jika `ALLOWED_ORIGINS` mengandung `*`. |

## 2. Konfigurasi CORS (`ALLOWED_ORIGINS`) & Header yang Diizinkan

Backend hanya mengizinkan header permintaan:
- `Authorization`, `Content-Type`, `X-Requested-With`, `X-Request-ID`, `Idempotency-Key`.
Header yang sengaja **tidak diizinkan**: `X-Koperasi-ID` (karena ID koperasi selalu diekstrak dari JWT terverifikasi!).

Header yang diekspos (`Access-Control-Expose-Headers`) untuk dibaca JS browser:
- `X-Request-ID`, `Retry-After`.

## 3. Reverse Proxy, HTTPS & `TRUSTED_PROXIES`

Jika frontend dan API berdiri di belakang Nginx / Caddy / Cloudflare:
1. Teruskan header `X-Forwarded-For`, `X-Forwarded-Proto`, dan `Host`.
2. Pastikan operator backend mengisi CIDR proxy pada `TRUSTED_PROXIES` sehingga rate limiter IP membaca alamat asli HP warga—bukan mengunci seluruh satu desa karena mengira semua request berasal dari IP internal proxy!
3. Jangan biarkan reverse proxy memodifikasi `Cache-Control` `/api/v1/auth/*` atau menyimpannya di CDN (`GET /api/v1/public/config` sudah aman memakai `public, max-age=60`).

## 4. Opsi Backend-for-Frontend (`BFF`) dengan Cookie `HttpOnly` + Anti-CSRF

Pada aplikasi web produksi berisiko tinggi (seperti portal admin/bendahara):
- Opsi standar SPA SDK menyimpan access & refresh token **hanya di memori JS** (hilang saat tab ditutup/refresh; aman dari akses `localStorage`).
- Bila ingin sesi bertahan antar-refresh halaman dengan keamanan tinggi, pasang lapisan BFF ringan di domain yang sama (*same-origin*) untuk memegang `refresh_token` di **cookie `HttpOnly; Secure; SameSite=Strict`** (atau `Lax`).
- **Wajib Anti-CSRF saat memakai Cookie**: Setiap mutasi (`POST`, `PUT`, `PATCH`, `DELETE`) lewat BFF **wajib** memvalidasi header anti-CSRF (misal `X-CSRF-Token` atau *Double Submit Cookie*) plus pengecekan header `Origin`.

## 5. Content Security Policy (`CSP`) yang Disarankan

```http
Content-Security-Policy:
  default-src 'self';
  script-src 'self';
  connect-src 'self' https://api.kopdes-contoh.example;
  img-src 'self' blob: data:;
  style-src 'self' 'unsafe-inline';
  frame-ancestors 'none';
  object-src 'none';
```

*(Catatan: bila `captcha.enabled === true` dari `GET /api/v1/public/config`, tambahkan origin widget CAPTCHA terkait pada `script-src` & `frame-src`.)*

## 6. Kill-Switch Fitur & Mode Pemeliharaan Zero-Downtime

Operator dapat menutup sementara fitur bermasalah tanpa redeploy kode frontend lewat env backend:
- `READ_ONLY_MODE=true` -> seluruh API mutasi membalas `503 READ_ONLY_MODE`; frontend menampilkan banner baca-saja dari `GET /api/v1/public/config`.
- `PAYMENTS_ENABLED=false` / `REGISTRATION_ENABLED=false` / `LOGIN_ENABLED=false` -> membalas `503 FEATURE_DISABLED` pada grup rute terkait.

## 7. Checklist Deploy Frontend

- [ ] Seluruh bundle bebas dari port/URL tes hardcoded maupun token tes.
- [ ] `ALLOWED_ORIGINS` backend cocok persis hingga skema (`https://`) dan subdomainnya.
- [ ] Banner `read_only_mode` dan `features.*` teruji dalam **Config Playground**.
