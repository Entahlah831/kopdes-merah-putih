# Panduan Integrasi Frontend Mandiri — Kopdes Merah Putih

Versi Kontrak API: `v1.1.0` | Spesifikasi OpenAPI: [openapi.yaml](../openapi.yaml) | Katalog Error: [pesan-error-id.json](pesan-error-id.json)

> **Tujuan Paket Dokumentasi Ini**: Membantu pengembang frontend membangun aplikasi lengkap Kopdes Merah Putih **secara mandiri** hanya dari panduan per bab ini, SDK TypeScript referensi ([frontend-reference/](../../frontend-reference/)), dan alat uji interaktif ([frontend-tester/](../../frontend-tester/)) tanpa menebak-nebak perilaku backend.
> **Prinsip Pembuktian**: Contoh JSON respons yang ditandai `<!-- doc-test-response: ... -->` serta klaim berlabel `<!-- claim:CLAIM-... -->` dijalankan dan dicocokkan langsung terhadap server uji Go + PostgreSQL 16.

---

## Peta Baca Berdasarkan Peran Tim

| Peran Anda | Urutan Bab yang Wajib Dibaca Pertama | Fokus Utama |
|---|---|---|
| **Tech Lead / Arsitek Frontend** | [00](00-mulai-cepat.md) -> [01](01-konfigurasi-frontend.md) -> [02](02-autentikasi-mfa-sesi.md) -> [03](03-peran-dan-izin.md) -> [19](19-deploy-dan-lingkungan.md) | Arsitektur tanpa hardcode URL/port, penyimpanan token di memori / BFF, single-flight refresh, RBAC & isolasi multi-tenant. |
| **Frontend Developer (React/Vue/JS)** | [00](00-mulai-cepat.md) -> [01](01-konfigurasi-frontend.md) -> [17](17-contoh-react-vue-vanila.md) -> [04](04-pendaftaran-kyc.md)..[11](11-laporan-dan-pengawas.md) -> [13](13-kesalahan-retry-idempotensi.md) | Pemakaian SDK `createClient`, `AuthManager`, komponen per fitur, upload file, polling QRIS, idempotensi, dan format Rupiah/`Asia/Jakarta`. |
| **UI/UX Designer** | [03](03-peran-dan-izin.md) -> [04](04-pendaftaran-kyc.md) -> [08](08-toko-checkout-pesanan.md) -> [09](09-pembayaran-qris-transfer.md) -> [16](16-checklist-praproduksi-ux.md) -> [18](18-glosarium-ui-ux.md) | Bahasa ramah warga desa, status badge, alur kode unik QRIS + warning rekening pilot, ukuran sentuh mobile 44x44px, pesan error Bahasa Indonesia. |
| **QA Engineer / Tester** | [00](00-mulai-cepat.md) -> [15](15-testing-tanpa-bertanya.md) -> [03](03-peran-dan-izin.md) -> [99](99-glosarium-koperasi.md) | Menjalankan **Lab Belajar**, **Panduan Terpandu**, **Config Playground**, `27` skenario klaim, matriks `55` operasi x `7` peran, dan zero-PII audit. |

---

## Daftar Isi Lengkap (21 Bab + Aset Mesin-Terbaca)

### Fondasi & Konfigurasi
- [00. Mulai Cepat 5 Menit](00-mulai-cepat.md) — Menjalankan stack lokal/tester, memanggil `GET /api/v1/public/config`, dan login perdana.
- [01. Konfigurasi Frontend Item demi Item](01-konfigurasi-frontend.md) — Penjelasan detail seluruh variabel env frontend, seluruh field `GET /api/v1/public/config`, batas keamanan, dampak jika salah, dan skema validasi.
- [02. Autentikasi, MFA Wajib & Manajemen Sesi](02-autentikasi-mfa-sesi.md) — 3 cabang login (`access_token`, `mfa_session_token`, `403 setup_token`), single-flight refresh, reuse detection, revoke, dan sinkronisasi antar-tab.
- [03. Peran, Izin & Matriks Otorisasi](03-peran-dan-izin.md) — 7 peran koperasi, pemisahan Maker-Checker, anti-eskalasi, dan tabel sinkronisasi otomatis 55 operasi.

### Alur Bisnis Koperasi Desa (End-to-End)
- [04. Pendaftaran Anggota & Unggah KYC](04-pendaftaran-kyc.md) — Formulir pendaftaran, enkripsi NIK & masking `nik_masked`, kompresi & unggah berkas KTP/KK, verifikasi pengurus, dan purge retensi (**PERLU VERIFIKASI HUKUM**).
- [05. Profil, Manajemen Anggota & Pengguna](05-profil-anggota-pengguna.md) — `GET /auth/me`, daftar & detail anggota, perubahan status keanggotaan, dan manajemen peran pengguna anti-eskalasi.
- [06. Iuran & Simpanan Anggota](06-iuran-simpanan.md) — Jenis iuran netral (**KEPUTUSAN AKUNTAN**), generate tagihan bulanan, tagihan saya, dan daftar menunggak.
- [07. Katalog Produk, Promo & Keranjang Belanja](07-katalog-promo-keranjang.md) — Katalog publik/anggota, kalkulasi harga anggota (`produk_id` + `anggota_id`), kode promo, dan kelola keranjang.
- [08. Toko, Checkout & Siklus Pesanan](08-toko-checkout-pesanan.md) — Checkout atomik (`status: "draft"`), pencegahan klik ganda, pembatalan, dan kedaluwarsa pesanan.
- [09. Pembayaran QRIS, Kode Unik & Anti-IDOR](09-pembayaran-qris-transfer.md) — QRIS statis + kode unik 3–4 digit, `Idempotency-Key`, hitung mundur `sisa_waktu_detik`, klaim RRN, polling hemat kuota, persetujuan ganda, dan Pilot Rekening Pribadi.
- [10. Rekonsiliasi, Rekening QRIS, Mutasi CSV & Kas](10-rekonsiliasi-mutasi-kas.md) — Pendaftaran rekening merchant badan hukum, impor mutasi bank CSV, konfirmasi orang kedua, rekonsiliasi harian `Asia/Jakarta`, dan `kas_mutasi` append-only.
- [11. Laporan Keuangan, Rekap Anggota & Pengawasan](11-laporan-dan-pengawas.md) — Laporan keuangan & anggota serta batasan akses read-only untuk peran `pengawas` dan `operator`.

### Pola Teknis, Kualitas & Produksi
- [12. Format Data, Mata Uang Rupiah & Zona Waktu](12-format-data-uang-waktu.md) — Integer `int64`/`NUMERIC(18,2)`, pemformatan `Intl.NumberFormat("id-ID")`, tanggal `Asia/Jakarta`, dan deteksi selisih jam perangkat.
- [13. Penanganan Kesalahan, Retry & Idempotensi](13-kesalahan-retry-idempotensi.md) — Envelope JSON, 15 kode error resmi, `Retry-After` pada `429`/`503`, `AbortController`, dan strategi UUID `Idempotency-Key`.
- [14. Unggahan Berkas & Perlindungan Data Klien](14-unggahan-berkas-keamanan-klien.md) — Validasi MIME & ukuran, kompresi gambar sisi klien, pelacakan progres, serta kepatuhan privasi UU 27/2022 PDP (**PERLU VERIFIKASI HUKUM**).
- [15. Cara Menguji Tanpa Bertanya ke Backend](15-testing-tanpa-bertanya.md) — Penggunaan **Lab Belajar** di [frontend-tester/](../../frontend-tester/), Config Playground, Simulator Kondisi, `scenarios.json`, dan tes unit SDK.
- [16. Checklist Pra-Produksi UX & Keamanan](16-checklist-praproduksi-ux.md) — Daftar periksa mobile-first desa, aksesibilitas ARIA, penanganan sinyal lambat, dan verifikasi bebas kebocoran PII/token.
- [17. Contoh Integrasi Lengkap: React, Vue 3 & Vanilla JS](17-contoh-react-vue-vanila.md) — Pola siap pakai memanggil [frontend-reference/](../../frontend-reference/) pada tiga stack populer.
- [18. Glosarium Istilah UI/UX & Terjemahan Status](18-glosarium-ui-ux.md) — Kamus pemetaan istilah teknis status database ke teks antarmuka berbahasa Indonesia.
- [19. Deploy, CORS, Reverse Proxy, BFF & Mode Pemeliharaan](19-deploy-dan-lingkungan.md) — Konfigurasi lingkungan terpisah, `ALLOWED_ORIGINS`, `TRUSTED_PROXIES`, opsi BFF HttpOnly + CSRF, CSP, serta switch `READ_ONLY_MODE`.
- [99. Glosarium Koperasi Desa & Catatan Akuntansi/Hukum](99-glosarium-koperasi.md) — Referensi istilah perkoperasian, penanda **KEPUTUSAN AKUNTAN**, dan item **PERLU VERIFIKASI HUKUM**.

### Aset Mesin-Terbaca & Alat Langsung
- Skema Konfigurasi JSON: [frontend.config.schema.json](frontend.config.schema.json) & [config.example.json](config.example.json)
- Kamus Pesan Error Indonesia: [pesan-error-id.json](pesan-error-id.json)
- Riwayat Perubahan Frontend: [CHANGELOG.md](CHANGELOG.md)
- SDK Referensi TypeScript: [frontend-reference/](../../frontend-reference/) (contoh [React](../../frontend-reference/src/examples/react-example.tsx), [Vue 3](../../frontend-reference/src/examples/vue-example.ts), [Vanilla JS](../../frontend-reference/src/examples/vanilla-example.js))
- Frontend Tester & Lab Belajar: [frontend-tester/index.html](../../frontend-tester/index.html) & halaman referensi minimal [frontend-tester/reference/](../../frontend-tester/reference/)
- Kontrak OpenAPI 3 & Panduan Ringkas: [openapi.yaml](../openapi.yaml), [FRONTEND_GUIDE.md](../FRONTEND_GUIDE.md), [BACKEND_GUIDE.md](../BACKEND_GUIDE.md), [DOC_DISCREPANCIES.md](../DOC_DISCREPANCIES.md)
