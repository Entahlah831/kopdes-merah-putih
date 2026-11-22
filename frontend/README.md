# Frontend Kopdes Merah Putih

Direktori `frontend/` berisi dua antarmuka klien resmi yang terhubung ke backend Go di [../backend/](../backend/) tanpa pernah menanamkan literal port, URL produksi, maupun rahasia di kode klien:

1. **[wireframe/](wireframe/)** — Aplikasi web **Vite + TypeScript ketat** (`0` runtime dependencies, DOM builder aman tanpa `.innerHTML`, sesi cookie `HttpOnly` + header `X-Requested-With`, kompresi & sanitasi gambar KYC/bukti via `<canvas>`) + suite **Playwright E2E (`01`–`06`)**.
2. **[tester/](tester/)** — Alat uji HTML/JS murni tanpa build step untuk eksplorasi kontrak endpoint, matriks peran (`roles_matrix.json`), dan spesifikasi [openapi.json](tester/openapi.json).

---

## Peta Rute UI `frontend/wireframe/`

| Rute Hash | Fungsi | Hak Akses | Berkas Sumber |
| :--- | :--- | :--- | :--- |
| `#/beranda` | Halaman utama informasi koperasi, produk pilihan & cara bergabung | Publik | [beranda.ts](wireframe/src/views/beranda.ts) |
| `#/katalog` | Katalog produk, filter kategori/stok/harga, ringkasan lokal & checkout langsung | Publik / Login | [katalog_pesanan.ts](wireframe/src/views/katalog_pesanan.ts) |
| `#/daftar` | Pendaftaran calon anggota, unggah 3 foto KYC terenkripsi `.enc`, aktivasi sandi | Publik | [auth_daftar.ts](wireframe/src/views/auth_daftar.ts) |
| `#/masuk` | Login cookie `HttpOnly` + alur 2-langkah MFA TOTP | Publik | [auth_daftar.ts](wireframe/src/views/auth_daftar.ts) |
| `#/tagihan` | Portal tagihan iuran anggota, QRIS kode unik, unggah bukti, polling status terminal | Login | [tagihan.ts](wireframe/src/views/tagihan.ts) |
| `#/pesanan` | Daftar & detail pesanan, stepper status, bayar QRIS, pemenuhan staf | Login | [katalog_pesanan.ts](wireframe/src/views/katalog_pesanan.ts) |
| `#/dashboard` | Dashboard Pengurus, 4 KPI agregat, sidebar operasional & **Audit Log (`admin`)** | `admin`, `bendahara`, `petugas_toko` | [admin.ts](wireframe/src/views/admin.ts) |
| `#/verifikasi_anggota` | Antrean verifikasi pendaftaran + pratinjau dekripsi foto KYC | `admin`, `bendahara`, `petugas_toko` | [admin.ts](wireframe/src/views/admin.ts) |
| `#/verifikasi_pembayaran` | Verifikasi pembayaran dual-control + pratinjau bukti `.enc` + **Refund 2-Orang** | `admin`, `bendahara` | [admin.ts](wireframe/src/views/admin.ts) |
| `#/operasional` | **Laporan (+ CSV), Daftar Antar (+ CSV), Tambah Produk/Kategori, Jenis/Generate Iuran, Catat Tunai** | `admin`, `bendahara`, `petugas_toko` | [operasional.ts](wireframe/src/views/operasional.ts) |
| `#/pengaturan_user` | **Pengaturan Koperasi (`PUT /api/v1/pengaturan`) & Manajemen Pengguna / Reset Sandi & MFA** | `admin` | [operasional.ts](wireframe/src/views/operasional.ts) |

---

## Perintah Pengembangan, Build & E2E Lokal

### Build SPA Produksi (`frontend/wireframe/`)
```bash
cd frontend/wireframe
npm ci
npx tsc --noEmit
npm run build
```
Keluaran statis berada di `frontend/wireframe/dist/` dan langsung dimount oleh [../Caddyfile](../Caddyfile) / [../docker-compose.yml](../docker-compose.yml) ke `/srv/frontend` (atau disajikan lewat `go -C backend run ./cmd/kopdes --env-file .env.test uiserve --ui-dir frontend/wireframe/dist`).

### Menjalankan Lokal Terhadap Backend Uji (`.env.test`)
1. Pastikan PostgreSQL uji sudah terisi role & migrasi, lalu jalankan API + seeder:
   ```bash
   go -C backend run ./cmd/seed_test_accounts --env-file "${PWD}/.env.test"
   go -C backend run ./cmd/kopdes --env-file "${PWD}/.env.test" serve
   ```
2. Pada terminal terpisah, sajikan UI Wireframe (atau Tester Polos):
   ```bash
   # Wireframe TypeScript SPA (memakai hasil npm run build)
   go -C backend run ./cmd/kopdes --env-file "${PWD}/.env.test" uiserve --ui-dir frontend/wireframe/dist

   # ATAU Tester HTML/JS Polos
   go -C backend run ./cmd/kopdes --env-file "${PWD}/.env.test" uiserve --ui-dir frontend/tester
   ```
3. Buka URL sesuai `UI_PORT` pada `.env.test`. Konfigurasi runtime dimuat otomatis dari `/config.json`.

### Menjalankan Playwright E2E (`Suite 01`–`06`)
Jalankan API & `uiserve` memakai `.env.test.alt` (Set B Cookie-Primary), lalu eksekusi dari `frontend/wireframe/`:
```bash
cd frontend/wireframe
UI_BASE_URL="http://127.0.0.1:${UI_PORT}" E2E_SEED_PASSWORD="<PASSWORD_SEED_TEST>" npx playwright test --reporter=list
```

---

## Daftar Tugas & Panduan Lanjutan
- Daftar lengkap pekerjaan penyempurnaan antarmuka tim frontend ada di **[TODO_FRONTEND.md](TODO_FRONTEND.md)**.
- Kontrak integrasi HTTP, cookie, CSRF, dan alur bisnis ada di **[../docs/FRONTEND_GUIDE.md](../docs/FRONTEND_GUIDE.md)**.
- Inventaris 19 frame Figma dan matriks penutupannya ada di **[../docs/wireframe/MATRIKS_E2E_19_FRAME.md](../docs/wireframe/MATRIKS_E2E_19_FRAME.md)**.
