# ADR-010: Audit Menyeluruh RBAC, Parameter Operasional, Kode Unik QRIS, Minimisasi PII & CI Database (D10)

- **Status**: `DIUBAH` (perbaikan drift RBAC, validasi `foto_url`, `tanggal_lahir` opsional, dan CI DB penuh) + `DIPERTAHANKAN` (kode unik 3 digit default, jenis iuran, retensi dengan tag hukum/akuntan/pemilik)

## (a) Keputusan Saat Ini dan Lokasi Pembuatan
Meninjau kembali 7 keputusan operasional & privasi di branch `lean-revamp`:
1. **RBAC**: `internal/pesanan/lifecycle.go` tidak melarang `bendahara` mengubah status pesanan; `internal/pembayaran/service.go` hanya menolak `anggota` untuk `metode=tunai` (tidak melarang `petugas_toko`); `internal/pendaftaran/workflow.go` memiliki pengecekan mati `isStaff`. Matriks `docs/roles_matrix.json` hanya menguji 16 dari 36 endpoint tanpa cek router otomatis.
2. **Jenis Iuran vs Akuntansi**: Tabel `jenis_iuran.klasifikasi_akuntansi` memakai label teks netral (`KEPUTUSAN AKUNTAN`) tanpa tabel chart-of-accounts double-entry.
3. **Kode Unik QRIS (3 vs 4 digit)**: Default `QRIS_UNIQUE_DIGITS=3` (`1..999`), dengan opsi `4` (`1..9999`), didampingi `PENDING_LIMIT_GLOBAL=50` & `PENDING_LIMIT_PER_MEMBER=2` serta indeks unik parsial aktif `idx_pembayaran_qris_unik_aktif`.
4. **Retensi Pendaftaran Ditolak & Bukti**: `PDP_REJECTED_RETENTION_DAYS=30` dan `BUKTI_RETENTION_DAYS=365` (baru ditambahkan pada D4).
5. **Pilot Rekening Perorangan**: `ALLOW_PERSONAL_PAYMENT_PILOT=false` default, dengan `PILOT_MAX_PER_TX=500000`, `PILOT_MAX_PER_DAY=2000000`.
6. **`produk.foto_url` & Berkas Media Produk**: Kolom teks bebas tanpa validasi skema URL (`javascript:` bisa masuk), tanpa upload file biner lokal terpisah.
7. **Minimisasi PII (`tanggal_lahir`, `alamat`) & Tes `internal/db`**: `pendaftaran` dan CSV `anggota/impor` mewajibkan `tanggal_lahir` padahal tidak ada logika umur/ultah di domain koperasi; dan `internal/db/role_guard_test.go` tidak mendukung `-short` sedangkan CI hanya menerapkan migrasi `000001`.

## (b) Opsi Alternatif & Evaluasi Bobot (30% Keamanan / 20% Kesederhanaan / 20% Skala Desa / 15% Pemulihan / 15% Biaya)
1. **RBAC**: (1) Biarkan cek terpisah di tiap handler -> rawan drift; (2) **Sinkronkan RBAC + Uji Otomatis Live Router & Matriks (`docs/roles_matrix.json` 36 rute)** -> menang skor (`4.90` vs `3.10`).
2. **Kode Unik QRIS (`3` vs `4` digit)**:
   - *Analisis Matematika Kapasitas*: Pada `QRIS_UNIQUE_DIGITS=3` (`999` kode unik), batas `PENDING_LIMIT_GLOBAL=50` hanya memakai paling banyak `50/999 = 5.01%` slot pada satu waktu untuk 100–1.000 pengunjung dengan `PAYMENT_EXPIRY_DURATION=15m` — peluang benturan acak rendah dan nominal tambahan maksimal Rp999 ramah warga desa vs tambahan hingga Rp9.999 pada 4 digit. Jika koperasi berkembang dan menaikkan pending limit global, `.env` cukup diubah menjadi `QRIS_UNIQUE_DIGITS=4` (`9.999` slot) tanpa migrasi kode -> **DIPERTAHANKAN default `3` (`KEPUTUSAN PEMILIK`)**.
3. **Jenis Iuran vs Akuntansi**: **DIPERTAHANKAN** (`jenis_iuran.klasifikasi_akuntansi` sebagai tag kategori sederhana + **KEPUTUSAN AKUNTAN** sebelum tutup buku resmi; double-entry dilarang oleh lingkup koperasi tunggal).
4. **Retensi PDP & Bukti (`30` hari ditolak, `365` hari bukti non-lunas)**: **DIPERTAHANKAN** nilai bawaan konservatif di balik `.env` dengan tanda wajib **PERLU VERIFIKASI HUKUM** (aturan UU PDP/ketentuan koperasi) dan **KEPUTUSAN AKUNTAN**.
5. **Pilot Rekening Perorangan**: **DIPERTAHANKAN** `ALLOW_PERSONAL_PAYMENT_PILOT=false` (aman secara hukum & anti-fraud default); bila terpaksa diaktifkan sebelum QRIS badan hukum terbit -> **KEPUTUSAN PEMILIK** & **PERLU VERIFIKASI HUKUM**.
6. **`produk.foto_url` & Media**: (1) Teks bebas tanpa cek (rawan XSS URI `javascript:`); (2) **Validasi URL ketat (`""`, path relatif `/...`, atau `https://` <= 512 karakter) + anjuran gambar terkompresi WebP/JPEG di CDN/asset terpisah (`<= 500 KB`)**; (3) Simpan blob gambar katalog di VPS. Opsi 2 menjaga server ringan, aman dari Stored XSS, dan hemat bandwidth warga desa.
7. **Minimisasi Data Pribadi (`tanggal_lahir` vs `alamat`)**:
   - `alamat` terpakai langsung oleh fitur pengantaran (`pesanan.alamat_pengantaran_enc` & `daftar-antar`), sedangkan **`tanggal_lahir` TIDAK terpakai oleh satu pun logika bisnis koperasi** (tidak ada aturan batas usia, program ulang tahun, maupun laporan usia).
   - **DIUBAH**: Buat `tanggal_lahir` **opsional (nullable)** pada pendaftaran publik, konversi ke anggota, dan impor CSV (pertahankan `alamat` karena dipakai pengantaran dan enkripsi AAD-nya tetap utuh).
8. **Tes `internal/db` & Migrasi CI**: **DIUBAH** — tambah `testing.Short()` skip pada `internal/db/role_guard_test.go` agar `-short` konsisten bagi pengembang tanpa Postgres lokal, sambil mewajibkan CI menerapkan semua `db/migrations/*_*.up.sql` & menjalankan suite penuh tanpa `-short`.

## (c) Ringkasan Skor Keputusan Utama D10
| Sub-Keputusan | Status | Skor Lama vs Terpilih |
| :--- | :--- | :--- |
| RBAC Sync & Live Router Test | **DIUBAH** | `3.10` -> **`4.90`** |
| `foto_url` Validasi & Panduan WebP | **DIUBAH** | `2.80` -> **`4.75`** |
| `tanggal_lahir` Opsional (Minimisasi PII) | **DIUBAH** | `3.45` -> **`4.80`** |
| `internal/db` CI Full & `-short` | **DIUBAH** | `3.20` -> **`4.90`** |
| Kode Unik `3` Digit + Limit Global | **DIPERTAHANKAN** | **`4.70`** (cukup 5% saturasi) |
| `jenis_iuran` & `catatan_akuntansi` | **DIPERTAHANKAN** | **`4.65`** (**KEPUTUSAN AKUNTAN**) |
| Retensi 30d/365d & Pilot False | **DIPERTAHANKAN** | **`4.60`** (**PERLU VERIFIKASI HUKUM** & **KEPUTUSAN PEMILIK**) |

## (d) Bukti Terukur di Repositori
- `TestRBAC_MatrixMatchesLiveRouterAndEnforcesBoundaries` (`internal/api/rbac_matrix_test.go`, **PASS**, `-race`, `6.00s`):
  - Memverifikasi paritas 100% dari seluruh **36 rute** di `internal/api/routes.go` dengan `docs/roles_matrix.json` dan `frontend-tester/roles_matrix.json`.
  - Menguji **19 x 5 = 95 panggilan peran langsung** + membuktikan secara eksplisit bahwa `bendahara` ditolak `403` pada `PATCH /api/v1/pesanan/{id}/status` dan `petugas_toko` ditolak `403` pada `POST /api/v1/pembayaran` `metode=tunai`.
  - Menguji registrasi publik -> upload KTP -> persetujuan admin -> `GET /api/v1/anggota/{id}?unmask=true` **tanpa mengirim `tanggal_lahir`** berakhir sukses dengan `tanggal_lahir == ""`.
- `TestValidateFotoURL` (`internal/katalog/katalog_test.go`, **PASS**): menolak `javascript:alert(1)`, `data:image/png;base64,...`, `http://...`, `//evil...`, `>512` karakter; menerima `""`, `/assets/produk/beras.jpg`, `https://...`.
- `TestValidateRegisterInput_OptionalTanggalLahir` (`internal/pendaftaran/pendaftaran_test.go`, **PASS**) & `TestParseCSVRows_OptionalTanggalLahir` (`internal/anggota/anggota_test.go`, **PASS**).
- `TestSuperuserRejectedOnStartup` (`internal/db/role_guard_test.go`, **PASS** pada DB live `0.30s` dan dilewati bersih pada `-short`).

## (e) Rekomendasi
Lihat status per sub-keputusan di atas (**RBAC, `foto_url`, `tanggal_lahir` opsional, CI DB = `DIUBAH`; Kode unik 3 digit, klasifikasi iuran, retensi default, pilot false = `DIPERTAHANKAN`**).

## (f) Implementasi yang Dikerjakan
- Migrasi `000002`: `pendaftaran.tanggal_lahir_enc DROP NOT NULL`, `anggota.tanggal_lahir_enc DROP NOT NULL`.
- `internal/pesanan/lifecycle.go`, `internal/pembayaran/service.go`, `internal/pendaftaran/{service,workflow}.go`, `internal/anggota/impor.go`, `internal/katalog/service.go`, `internal/db/role_guard_test.go`.
- `docs/roles_matrix.json`, `frontend-tester/roles_matrix.json`, dan `.github/workflows/ci.yml`.

## (g) Risiko Sisa & Keputusan Terbuka
- Bila pemilik ingin memindah kode unik ke 4 digit atau menaikkan `PENDING_LIMIT_GLOBAL` > 100, cukup ubah env (`QRIS_UNIQUE_DIGITS=4`).
- Penetapan akhir masa retensi (`PERLU VERIFIKASI HUKUM`), klasifikasi iuran & selisih kode unik (`KEPUTUSAN AKUNTAN`), dan izin pilot rekening pribadi (`KEPUTUSAN PEMILIK`) tetap menunggu keputusan pemilik bersama konsultan resmi sebelum operasional publik penuh.
