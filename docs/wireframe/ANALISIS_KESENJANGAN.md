# Analisis Kesenjangan Wireframe Figma vs Backend Kopdes Merah Putih

Dokumen ini memetakan seluruh layar & aksi pada [INVENTARIS_LAYAR.md](INVENTARIS_LAYAR.md) terhadap spesifikasi API nyata di [openapi.yaml](../openapi.yaml) **sebelum** implementasi `frontend-wireframe/` dimulai. Sesuai **Aturan Workspace Kopdes** dan **Prompt v9 §4.2**, fitur di luar lingkup koperasi tunggal **tidak** ditambahkan ke backend melainkan ditandai eksplisit sebagai **`BLOCKED: butuh keputusan pemilik`**.

---

## 1. Rekonsiliasi Keputusan Autentikasi & Keamanan (D3 — `ADR-003`)

- **Wireframe Implisit:** Tombol `Masuk` tanpa rincian penyimpanan token; beberapa form registrasi menyebut sandi & OTP email/WA di awal.
- **Perilaku Backend Aktual:**
  - Sesuai **D3 (`DECISION_AUDIT.md`)**: di `APP_ENV=production` dan Set B (`ALLOW_BEARER_AUTH=false`), API **hanya menerima cookie sesi HttpOnly** (`__Host-kopdes_session` / `kopdes_session`), dengan proteksi CSRF `X-Requested-With: XMLHttpRequest`. Bearer header ditolak (`401`).
  - Frontend referensi `frontend-wireframe/` **wajib** memakai `fetch(..., { credentials: "include", headers: { "X-Requested-With": "XMLHttpRequest" } })` dan **dilarang** menyimpan token atau PII di `localStorage` / `sessionStorage`.
  - Admin/Bendahara (serta role di `MFA_REQUIRED_ROLES`) mengikuti login 2-tahap `/api/v1/auth/login` -> `/api/v1/auth/mfa/setup` atau `/api/v1/auth/mfa/verify`.

---

## 2. Pemetaan Layar & Aksi ke `openapi.yaml`

### A. Layar Publik & Pendaftaran (`01a`–`01e`, `14:6`, `71:123`, `25:147`, `25:273`, `25:409`)

| Layar / Fitur Wireframe | Endpoint `docs/openapi.yaml` | Klasifikasi | Penjelasan & Penanganan di UI Referensi |
| :--- | :--- | :--- | :--- |
| `01a`, `01c` Config publik & QRIS/toko | `GET /api/v1/public/config` | **DIDUKUNG** | Nama koperasi/toko, status fitur (`payments_enabled`, `registration_enabled`, `captcha_enabled`), ongkir, info QRIS statis. |
| `01c`, `14:335`, `05` Produk unggulan & katalog publik | `GET /api/v1/katalog/produk` | **DIDUKUNG** | Daftar produk aktif, stok, harga umum vs anggota (`harga_anggota` hanya berlaku setelah login anggota). |
| `01a` Ringkasan 4 angka koperasi (anggota terdaftar, unit aktif, wilayah, tahun berdiri) | *(tidak ada endpoint publik aggregate count)* | **BLOCKED: butuh keputusan pemilik** | Catatan wireframe sendiri melarang angka dummy; tampilkan banner/status `"Data metrik publik — BLOCKED: butuh keputusan pemilik"` dan ringkasan `GET /api/v1/public/config`. |
| `01b` Unit usaha multi-cabang (`Gerai Sembako`, `Gudang & Distribusi`) | — | **DI LUAR LINGKUP — BLOCKED: butuh keputusan pemilik** | Backend melayani **1 koperasi di 1 lokasi** (tidak ada tabel unit usaha/cabang ganda). |
| `01d` Berita, Agenda, **Rapat Anggota Tahunan (RAT)**, Pengurus & Pengawas | — | **DI LUAR LINGKUP — BLOCKED: butuh keputusan pemilik** | Belum ada CMS berita/agenda/RAT maupun manajemen kepengurusan RAT pada schema DB ramping (**KEPUTUSAN PEMILIK**). |
| `01e` CTA & Footer, `25:409` Hubungi Kami, `71:123` FAQ | `GET /api/v1/public/config` (kontak dasar bila dikonfigurasi) | **TIDAK JELAS / STATIS** | Tidak ada tabel CMS FAQ/sosmed di backend; tampilkan blok informasi statis jujur tanpa angka dummy. |
| `25:147` Daftar Member: `nama_lengkap`, `nik`, `no_hp`, `email`, `alamat`, `tanggal_lahir`, `pekerjaan` | `POST /api/v1/public/pendaftaran` | **DIDUKUNG** | Mengembalikan `pendaftaran_id` + `upload_token` untuk unggah dokumen KYC (**PERLU VERIFIKASI HUKUM** persetujuan UU PDP). |
| `01c` Langkah 2 Unggah Dokumen KYC (`KTP`, `Foto diri / Selfie KTP`, `Kartu Keluarga`) | `POST /api/v1/public/pendaftaran/{id}/dokumen` | **DIDUKUNG** | Mendukung `ktp`, `selfie_ktp`, `kk` (JPG/PNG; divalidasi + kompres `<canvas>`). |
| `25:147` Field `Buat Kata Sandi`, `Ulangi Kata Sandi`, `Kirim OTP`, `Jenis Kelamin`, `Provinsi/Kab/Kec/Desa`, selector `Koperasi` | *(didesain berbeda di backend)* | **DI LUAR LINGKUP / BLOCKED: butuh keputusan pemilik** | Pendaftaran tidak memilih koperasi (koperasi tunggal), belum mengirim OTP SMS/WA eksternal, tidak menyimpan `jenis_kelamin`/wilayah terpisah, dan sandi diset setelah disetujui via `kode_aktivasi`. Tandai field terkait `"BLOCKED: butuh keputusan pemilik"` pada tampilan penjelasan. |
| **Masuk & MFA** (implisit dari tombol `Masuk`) | `POST /api/v1/auth/login`, `POST /api/v1/auth/mfa/setup`, `POST /api/v1/auth/mfa/verify`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me` | **DIDUKUNG** | Dilengkapi layar Masuk + MFA penuh berbasis cookie HttpOnly. |

### B. Portal Pengurus & Verifikator (`02`, `03a`, `03b`, `04b`)

| Layar / Fitur Wireframe | Endpoint `docs/openapi.yaml` | Klasifikasi | Penjelasan & Penanganan di UI Referensi |
| :--- | :--- | :--- | :--- |
| `02` Ringkasan KPI (`anggota aktif`, `menunggu verifikasi`, `tagihan menunggak`, `pesanan diproses`) & Laporan | `GET /api/v1/laporan`, `GET /api/v1/pendaftaran`, `GET /api/v1/pembayaran`, `GET /api/v1/pesanan`, `GET /api/v1/iuran/tagihan` | **DIDUKUNG** | Kartu metrik nyata dihitung langsung dari endpoint laporan & daftar antrean aktif (tanpa angka dummy). |
| `02` Sidebar Kelembagaan (`Data Koperasi`, `Wilayah & Legalitas`, `Unit Usaha`, `Pengurus & Pengawas`, `RAT`), `Sinkronisasi SIMKOPDES gagal`, `Arus kas pemasukan vs pengeluaran`, `Ekspor`, `Role & Permission` | — | **DI LUAR LINGKUP — BLOCKED: butuh keputusan pemilik** | Sesuai Aturan Workspace #1: dilarang membuat fitur SIMKOPDES, double-entry akuntansi kas keluar, atau multi-unit usaha. Tampilkan badge `"BLOCKED: butuh keputusan pemilik"`. Item pengeluaran/arus kas tambahan juga menunggu **KEPUTUSAN AKUNTAN**. |
| `02` Pengelolaan Pengguna & Pengaturan Koperasi | `GET/POST /api/v1/users`, `PATCH /api/v1/users/{id}`, `PUT /api/v1/pengaturan` | **DIDUKUNG** | Disediakan tautan fungsional ke Pengaturan & Manajemen Pengguna staf/anggota. |
| `02` Tabel Audit Trail | *(tidak ada endpoint `GET /api/v1/audit-log` di `openapi.yaml`)* | **BUTUH PERUBAHAN BACKEND KECIL** *(ditunda)* | Saat ini `audit_log` ditulis internal & tidak diekspos di `openapi.yaml` (menjaga batas `<= 36` route); panel audit di UI menampilkan catatan status/aktivitas ringkas dari riwayat transaksi + keterangan pembatasan akses **KEPUTUSAN PEMILIK**. |
| `03a` Antrean Pendaftaran (`Cari`, `Status`, `Rentang tanggal`, `dokumen_tersedia`, `nik` masked, empty state) | `GET /api/v1/pendaftaran` | **DIDUKUNG** | Mendukung filter `status`, menampilkan `nik_masked` + daftar `dokumen_tersedia` (`[n]/3`). Status `Dalam peninjauan` dan `Minta Perbaikan` (untuk dokumen) dicatat pada detail di bawah. |
| `03b` Detail Pemohon & Pratinjau Dokumen KYC (`ktp`, `selfie_ktp`, `kk`) | `GET /api/v1/pendaftaran`, `GET /api/v1/pendaftaran/{id}/dokumen/{tipe}`, `POST /api/v1/pendaftaran/{id}/verifikasi` | **DIDUKUNG** | Mendekripsi & menampilkan gambar `ktp`, `selfie_ktp`, `kk` secara aman via blob URL terotentikasi + keputusan `disetujui` / `perlu_perbaikan` / `ditolak` (+ catatan alasan). |
| `03b` Dokumen ke-4 `Surat pernyataan`, status terpisah `Dalam peninjauan`, tombol `Tampilkan` unmask NIK di antrean pendaftaran | — | **DI LUAR LINGKUP / TIDAK DIDUKUNG — BLOCKED: butuh keputusan pemilik** | Backend KYC memvalidasi tepat 3 tipe (`ktp`, `selfie_ktp`, `kk`); unmask NIK PII tersedia di `GET /api/v1/anggota/{id}?unmask=true` setelah disetujui menjadi anggota. |
| `04b` Verifikasi Pembayaran Bendahara/Admin (`Bukti pembayaran` gambar terenkripsi, `Detail`, `Alokasi`, `Nominal` / `Alasan penolakan`, `Setujui`/`Tolak`, Break-glass MFA admin) | `GET /api/v1/pembayaran`, `GET /api/v1/pembayaran?bukti_id={id}`, `GET /api/v1/pembayaran?rekening=true`, `POST /api/v1/pembayaran/{id}/verifikasi`, `POST /api/v1/pembayaran/{id}/refund` | **DIDUKUNG** | Verifikasi QRIS/tunai memakai `keputusan: "setuju" \| "tolak"`, `nominal_dibayar`, `alasan`, dan opsi `breakglass` + `mfa_code` bila dikonfigurasi (**KEPUTUSAN PEMILIK** `DUAL_CONTROL_BREAKGLASS_ENABLED`). Catatan: backend **tidak** membaca PDF bukti (JPG/PNG saja untuk mitigasi XSS/polyglot PDF). |
| `04b` Menu Kas (`Kas & Mutasi`, `Periode Kas`, `Rekonsiliasi`, `Laporan Keuangan`), impor mutasi bank | — | **DI LUAR LINGKUP — BLOCKED: butuh keputusan pemilik** | Sesuai Aturan #1: tidak ada double-entry, kas keluar, maupun impor CSV mutasi bank (**KEPUTUSAN AKUNTAN**). Ditandai `"BLOCKED: butuh keputusan pemilik"`. |

### C. Portal Anggota, Iuran, Katalog & Pesanan (`04a`, `05`, `06`)

| Layar / Fitur Wireframe | Endpoint `docs/openapi.yaml` | Klasifikasi | Penjelasan & Penanganan di UI Referensi |
| :--- | :--- | :--- | :--- |
| `04a` Tagihan & Pembayaran Anggota (ringkasan, checklist multi-tagihan, QRIS koperasi, buat pembayaran, unggah bukti JPG/PNG) | `GET /api/v1/iuran/tagihan`, `POST /api/v1/pembayaran` (JSON buat bayar & `multipart/form-data` unggah `pembayaran_id`+`file`), `GET /api/v1/pembayaran` | **DIDUKUNG** | Multi-tagihan (`tagihan_ids`) didukung hingga 24 tagihan dalam satu pembayaran QRIS dengan **kode unik integer**. Polling status pembayaran berhenti otomatis saat terminal (`lunas`, `ditolak`, `batal`, `kadaluarsa`). |
| `04a` Tab `Transfer` bank & upload format **PDF** | *(tidak didukung)* | **DI LUAR LINGKUP — BLOCKED: butuh keputusan pemilik** | Metode tersedia adalah **QRIS statis** (anggota) dan **Tunai** (dicatat petugas/admin/bendahara). Upload dibatasi ketat pada gambar **JPG/PNG** tervalidasi magic-byte + `<canvas>` re-encode. |
| `05` Katalog Produk (cari `q`, kategori/tersedia, harga Umum vs Anggota, stok tersedia, empty state + Reset filter) | `GET /api/v1/katalog/produk`, `POST /api/v1/katalog/produk` | **DIDUKUNG** | Filter `q` + filter kategori/ketersediaan klien, badge `Tersedia` / `Stok terbatas` / `Stok habis`. Catatan checkout tamu: `POST /api/v1/toko/checkout` memerlukan sesi terautentikasi (anggota atau akun tamu terdaftar), sehingga tombol `Pesan` meminta login terlebih dahulu jika anonim (**KEPUTUSAN PEMILIK**). |
| `05` Slider rentang harga & sort `Terpopuler` | *(filter klien dari hasil `GET /api/v1/katalog/produk`)* | **DIDUKUNG SEBAGIAN (Client-side)** | Filter harga & reset filter dikerjakan aman di browser atas data `katalog/produk`; urutan populer belum punya metrik penjualan publik sehingga memakai urutan default/harga. |
| **Checkout** (`POST /api/v1/toko/checkout`) | `POST /api/v1/toko/checkout` | **DIDUKUNG** | **Tanpa keranjang server** (Aturan #1): item dipilih pada client state sementara lalu dikirim sekaligus saat checkout beserta `Idempotency-Key`. |
| `06` Detail Pesanan & Daftar Pesanan (`Pesanan Saya`, item dibekukan, metode `ambil_sendiri` / `diantar`, bayar sekarang, batalkan, daftar antar admin) | `GET /api/v1/pesanan`, `PATCH /api/v1/pesanan/{id}/status`, `GET /api/v1/pesanan/daftar-antar`, `POST /api/v1/pembayaran` | **DIDUKUNG** | Menyiapkan semua status nyata (`baru`, `menunggu_pembayaran`, `dibayar`, `diproses`, `siap_diambil`, `diantar`, `selesai`, `batal`) serta alur pengajuan/eksekusi `refund` bendahara/admin (`POST /api/v1/pembayaran/{id}/refund`). Kolom `Varian` dan `Gudang / gerai` (multi-lokasi) ditandai `"BLOCKED: butuh keputusan pemilik"` karena SKU tunggal & 1 lokasi. |

---

## 3. Keputusan Desain Sebelum Implementasi UI Referensi

1. **Tidak menambah fitur atau endpoint baru** pada `docs/openapi.yaml` maupun skema SQL selama Fase 2.
2. **Keamanan & Portabilitas Ketat:**
   - API base URL berasal 100% dari `/config.json` (disajikan dinamis oleh `kopdes uiserve`), **tanpa literal port**.
   - Autentikasi memakai cookie HttpOnly (`credentials: "include"` + header `X-Requested-With: XMLHttpRequest`).
   - Seluruh render string pengguna (nama, alamat, catatan, alasan penolakan, nama produk) wajib melalui `textContent` / DOM nodes aman (zero `innerHTML` data untrusted) agar aman dari Stored XSS.
   - Seluruh unggahan gambar KYC & bukti bayar diproses & divalidasi di `<canvas>` (hanya `image/jpeg` dan `image/png`) sebelum dikirim sebagai `multipart/form-data`.
   - Fitur out-of-scope (`SIMKOPDES`, `RAT`, `Kas & Mutasi`, `Transfer Bank`, `Upload PDF`, `Multi-Koperasi`, `Multi-Gudang`) tetap ditampilkan jujur di UI navigasi/badge dengan label eksplisit **`BLOCKED: butuh keputusan pemilik`** dalam keadaan non-aktif.
