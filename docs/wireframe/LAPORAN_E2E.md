# Laporan E2E Wireframe Kopdes Merah Putih (`frontend-wireframe/`)

- **Target Uji:** Stack Set B (`.env.test.alt`: `APP_ENV=test`, `DB_NAME=kopdes_alt_test`, `ALLOW_BEARER_AUTH=false`, `MFA_REQUIRED_ROLES=admin,bendahara,petugas_toko`, port UI & API dibaca dinamis melalui `/config.json` dari `kopdes uiserve`)
- **Runner:** Playwright Chromium (`159.0.8154.6`) headless, `--reporter=list`
- **Waktu Eksekusi Terakhir:** 2026-11-22 01:42 WIB — **4 / 4 suite PASS (`18.7s`)**

---

## 1. Verifikasi Arsitektur Frontend & Keamanan D3

1. **Zero Hardcoded Ports & Zero Runtime Dependencies:**
   - `frontend-wireframe/package.json` memiliki `"dependencies": {}`.
   - Seluruh pemanggilan API membaca `api_public_url` dari `GET /config.json` (`cache: "no-store"`) dan konfigurasi koperasi dari `GET /api/v1/public/config`.
2. **Cookie-Primary Autentikasi (`ALLOW_BEARER_AUTH=false`):**
   - Seluruh permintaan memakai `credentials: "include"` + header `X-Requested-With: XMLHttpRequest`.
   - Diverifikasi otomatis di Playwright: `localStorage.length + sessionStorage.length === 0`.
3. **Mitigasi XSS & Upload Aman:**
   - Seluruh data teks dirender via `document.createTextNode` / `textContent` (`dom.ts`), **0 penggunaan `innerHTML`** pada data pengguna.
   - Berkas KYC (`ktp`, `selfie_ktp`, `kk`) dan bukti bayar diproses & dikompresi melalui `<canvas>` (`image.ts`), menolak PDF/non-gambar di sisi klien sebelum menyentuh backend.
4. **Polling & Lingkup Fitur:**
   - Polling status pembayaran (`startTerminalPoll`) berhenti otomatis pada status terminal (`lunas`, `ditolak`, `batal`, `kadaluarsa`, `selesai`, `perlu_tindak_lanjut`).
   - Fitur out-of-scope wireframe (`SIMKOPDES`, `RAT`, `Kas & Mutasi`, `Transfer Bank`, `Surat pernyataan`, `Multi-Gudang/Varian`) ditampilkan non-aktif berlabel **`BLOCKED: butuh keputusan pemilik`**.

---

## 2. Hasil Suite Playwright E2E

Lihat pemetaan lengkap 19 frame pada [MATRIKS_E2E_19_FRAME.md](MATRIKS_E2E_19_FRAME.md).

| Suite | Cakupan Skenario | Status |
| :--- | :--- | :---: |
| `01` | Beranda `01a`–`01e` (`beranda-hero`, `beranda-tentang` `01b`, `06 Produk Pilihan`, `07 Cara Bergabung`, `beranda-transparansi` `01d`, `beranda-footer` `01e`/`25:409`/`71:123`), label `BLOCKED: butuh keputusan pemilik`, viewport responsif mobile `375x812` (`scrollWidth <= innerWidth`), katalog `05`, filter tersedia/harga & empty state + `Reset filter` | **PASS** *(Set B + Set C)* |
| `02` | Daftar Member (`25:147`) dengan nama berisi payload XSS (`<img src=x onerror=...>`), upload 3 dokumen KYC via `<canvas>`, login Admin (cookie HttpOnly + MFA TOTP), `02 Dashboard Pengurus`, cek 0 storage token, pratinjau dokumen terdekripsi + verifikasi XSS **tidak dieksekusi**, approval `disetujui`, aktivasi sandi anggota baru, login anggota | **PASS** *(Set B)* |
| `03` | Setup rekening QRIS/produk/iuran, checkout `anggota1`, `06 Detail Pesanan`, buat pembayaran QRIS + upload bukti kanvas, `04a Tagihan Iuran` checklist & upload bukti, `04b` verifikasi Bendahara (dual control MFA) untuk pesanan & iuran, Admin proses `diproses -> siap_diambil -> selesai` | **PASS** *(Set B)* |
| `04` | **7 Skenario Negatif**: (1) Kata sandi salah ditolak UI, (2) BFLA anggota ke `#/dashboard` & `GET /api/v1/users` (`403`), (3) Upload PDF ditolak di kanvas sebelum request HTTP, (4) Idempotency key checkout replay (`order.id` identik), (5) Batalkan pesanan (`btn-batal-pesanan` hilang di status terminal `batal`), (6) Cookie write tanpa header `X-Requested-With` ditolak (`403`), (7) Logout mencabut sesi (`/auth/me` berikutnya `401`) | **PASS** *(Set B)* |
| `05` | Cakupan penuh 19 frame: produk `stok=0` (`Stok habis` disabled) & guard `Masuk untuk Checkout`; navigasi sidebar `#sb-ver-ang`, filter antrean `03a`, preview `<img>` KYC terdekripsi, keputusan `perlu_perbaikan` & `ditolak`; checkout `metode_pengiriman=diantar`, Bendahara `Tolak Bukti` (`04b`), upload ulang & `Setuju`; login `petugas_toko` (RBAC blokir `403` `/users` & menu pembayaran) memproses `diproses -> diantar -> selesai` | **Ditambahkan pada tindak lanjut v9 (diverifikasi di CI `frontend-wireframe-e2e`)** |

---

## 3. Bukti Tangkapan Layar (`reports/wireframe/`)

| Berkas | Layar / Momen yang Direkam |
| :--- | :--- |
| [01_beranda.png](../../reports/wireframe/01_beranda.png) | Beranda `01a`–`01e` desktop, harga produk nyata & badge `BLOCKED` |
| [02_katalog.png](../../reports/wireframe/02_katalog.png) | `05 Katalog Produk` + panel pilihan lokal (tanpa keranjang server) |
| [03_daftar_kyc.png](../../reports/wireframe/03_daftar_kyc.png) | `Daftar Member` + 3 berkas KYC terenkripsi lewat `<canvas>` |
| [04_dashboard_pengurus.png](../../reports/wireframe/04_dashboard_pengurus.png) | `02 Dashboard Pengurus` dengan metrik live & menu SIMKOPDES/Kas berlabel `BLOCKED` |
| [05_verifikasi_anggota.png](../../reports/wireframe/05_verifikasi_anggota.png) | `03a/03b Verifikasi Anggota` + 3 gambar KYC terdekripsi & payload XSS tampil literal aman |
| [06_pesanan_upload_bukti.png](../../reports/wireframe/06_pesanan_upload_bukti.png) | `06 Detail Pesanan` anggota dengan kode unik QRIS & upload bukti berhasil |
| [07_verifikasi_pembayaran.png](../../reports/wireframe/07_verifikasi_pembayaran.png) | `04b Verifikasi Pembayaran Bendahara` dengan pratinjau bukti terdekripsi |
| [08_tagihan_iuran.png](../../reports/wireframe/08_tagihan_iuran.png) | `04a Tagihan Iuran Anggota` dengan multi-pilih tagihan & Transfer Bank berlabel `BLOCKED` |
| [08b_pesanan_selesai.png](../../reports/wireframe/08b_pesanan_selesai.png) | `06 Detail Pesanan` status terminal `selesai` |
| [09_mobile_beranda.png](../../reports/wireframe/09_mobile_beranda.png) | Beranda responsif mobile viewport `375x812` bebas *horizontal overflow* |
