# ADR-003: Transport Sesi Cookie `HttpOnly` Utama + Perlindungan CSRF & Konfigurasi Proxy TLS (D3)

- **Status**: `DIUBAH` (cookie `HttpOnly` dipromosikan sebagai transport utama dengan `COOKIE_SECURE`, `ALLOW_BEARER_AUTH`, dan `CSRFGuard`)

## (a) Keputusan Saat Ini dan Lokasi Pembuatan
Pada branch `lean-revamp`, login menerbitkan `session_token` di body JSON sekaligus menyetel cookie `kopdes_session` dengan `Secure: r.TLS != nil` (`internal/auth/handler.go:26`). Di balik *reverse proxy* yang mengakhiri TLS (Caddy/Cloudflare -> HTTP internal), `r.TLS` bernilai `nil` sehingga cookie HTTPS produksi tidak berflag `Secure`. Selain itu belum ada middleware CSRF untuk melindungi permintaan mutasi yang membawa cookie, CORS belum mengirim header `Vary: Origin`, dan frontend SPA yang hanya mengandalkan token di memori JS kehilangan sesi setiap kali warga desa me-reload browser.

## (b) Opsi Alternatif (Minimal 2)
1. **Opsi 1 — Cookie + Bearer Tanpa CSRF & `Secure: r.TLS != nil`**: Rentan CSRF di browser dan atribut `Secure` hilang di balik proxy.
2. **Opsi 2 — Cookie `HttpOnly` Utama + `COOKIE_SECURE` (`auto|true|false`) + `CSRFGuard` + `ALLOW_BEARER_AUTH` Opsional**: Cookie disetel `HttpOnly`, `SameSite=Lax`, `Path=/`, tanpa `Domain`, memakai prefiks `__Host-kopdes_session` otomatis saat HTTPS (`r.TLS != nil` atau `X-Forwarded-Proto: https` HANYA dari `TRUSTED_PROXIES`). Semua mutasi (`POST|PUT|PATCH|DELETE`) yang membawa cookie divalidasi `Origin`/`Referer` terhadap `ALLOWED_ORIGINS`/host sendiri + wajib header kustom `X-Requested-With`. `ALLOW_BEARER_AUTH` default `false` pada `.env.example` / `.env.test.alt` (body login tidak membocorkan `session_token`, otorisasi Bearer ditolak `401`), dan hanya diaktifkan di `.env.test`/pengujian non-browser bila perlu.
3. **Opsi 3 — Bearer di Memori Saja Tanpa Cookie**: Aman dari CSRF tetapi sesi warga desa langsung hilang setiap reload halaman / perpindahan tab di browser seluler lambat.

## (c) Kriteria dan Bobot Keputusan
| Kriteria | Bobot | Opsi 1 (Lama) | Opsi 2 (Cookie + CSRF) | Opsi 3 (Bearer Memori) |
| :--- | :---: | :---: | :---: | :---: |
| Keamanan uang & data pribadi | 30% | 2 | **5** | 4 |
| Kesederhanaan operasi (1 dev) | 20% | 4 | **4** | 4 |
| Kecocokan skala & UX warga desa | 20% | 4 | **5** | 2 |
| Keandalan & kemudahan pemulihan | 15% | 3 | **5** | 3 |
| Biaya & kemampuan dibalik | 15% | 4 | **5** | 4 |
| **Skor Tertimbang** | **100%** | **3.25** | **4.80** | **3.45** |

## (d) Bukti Terukur di Repositori
- `TestDecisionAudit_D3_CookieOnlyAndCSRFAndProxySecure` (`internal/api/decision_audit_auth_csrf_test.go`, **PASS**, `-race`, `6.72s`):
  - Behind trusted proxy (`127.0.0.1/32` + `X-Forwarded-Proto: https`, `COOKIE_SECURE=auto`, `ALLOW_BEARER_AUTH=false`): set cookie `__Host-kopdes_session` (`Secure=true`, `HttpOnly=true`, `Path=/`, `SameSite=Lax`, `Domain=""`), respons JSON tidak memuat `session_token`, dan header Bearer ditolak `401`.
  - Spoof `X-Forwarded-Proto: https` dari IP proxy tak tepercaya diabaikan (`Secure=false`).
  - Simulasi browser TLS + reload halaman via `GET /api/v1/auth/me` sukses `200`; `POST /api/v1/auth/logout` lintas-origin (`Origin: https://evil.example`) ditolak `403 CSRF_ORIGIN_REJECTED`; tanpa `X-Requested-With` ditolak `403 CSRF_HEADER_REQUIRED`; logout sah mengembalikan `200`, membersihkan kedua variasi cookie (`Max-Age=0`), dan request berikutnya `401`. Header `Vary: Origin` terverifikasi ada.

## (e) Rekomendasi
**DIUBAH -> Pilih Opsi 2 (Cookie `HttpOnly` Utama + `__Host-` + `CSRFGuard` + `ALLOW_BEARER_AUTH=false` di Produksi).**

## (f) Implementasi yang Dikerjakan
- Variabel env `COOKIE_SECURE` (`auto|true|false`) & `ALLOW_BEARER_AUTH` (`bool`, default `false` pada `.env.example`/Set B).
- `internal/api/middleware/csrf.go` (`CSRFGuard`, `RequestScheme`), `internal/api/middleware/cors.go` (`Vary: Origin`), `internal/api/middleware/auth.go` (`ExtractSessionToken`, `SessionAuthOptions`).
- `internal/auth/handler.go` & `service.go` (cookie `__Host-kopdes_session`/`kopdes_session`, penghapusan dua cookie pada logout, `session_token,omitempty`), `public_config_handler.go` (bagian `auth`), dan `frontend-tester/app.js` (`credentials: "include"`, `X-Requested-With`, pemulihan sesi `/auth/me` saat reload).

## (g) Risiko Sisa
- Frontend pada domain berbeda dari API harus berada dalam domain HTTPS dengan `ALLOWED_ORIGINS` persis sama dan pengaturan `SameSite=Lax` (untuk lintas *eTLD+1* penuh dengan cookie tidak akan dikirim browser pada subrequest lintas situs; disarankan menempatkan API dan web di satu situs/subdomain eTLD+1 yang sama di bawah TLS).
