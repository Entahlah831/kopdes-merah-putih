# Rekonsiliasi Cakupan Skema Database (Scope Reconciliation)
**Sistem Backend Koperasi Desa Merah Putih (PostgreSQL 16 & Go)**

---

## 1. Ringkasan Inventaris Skema (Fokus Cabang Lokal)
Berdasarkan keputusan resmi Ketua Koperasi dan Klien:
- **Lingkup Implementasi**: Dikhususkan untuk **satu kantor cabang Kopdes lokal** yang telah disetujui (ACC) oleh klien. Tidak memerlukan pelaporan atau sinkronisasi nasional ke SIMKOPDES Kemenkop UKM.
- **Tabel Pelaporan Nasional**: Tabel `sinkronisasi_simkopdes` telah dihapus (*de-scoped*) melalui migrasi `000009_local_scope_and_pdp_retention.up.sql`.
- **Total Tabel Dasar (Base Tables)**: **64 tabel**
- **Total Tabel Partisi (Partitions)**: **8 partisi bulanan**
  - `kas_mutasi_y2026m11`, `kas_mutasi_y2026m12`
  - `stok_mutasi_y2026m11`, `stok_mutasi_y2026m12`
  - `audit_log_y2026m11`, `audit_log_y2026m12`
  - `pendaftaran_status_log_y2026m11`, `pendaftaran_status_log_y2026m12`
- **Total Objek Tabel Keseluruhan**: **72 entitas fisik**.

---

## 2. Pengelompokan Tabel: Aktif vs Cadangan Milestone

### Kelompok A: Core Foundation & Active API Scope (Milestone 1 - Saat Ini)
Tabel-tabel ini telah memiliki endpoint API, validasi bisnis, otorisasi peran, dan pengujian otomatis (TDD) menyeluruh:
1. `wilayah` — Master data wilayah administratif desa/kecamatan/kabupaten.
2. `koperasi` — Profil tenant koperasi desa, status legalitas, dan isolasi RLS.
3. `koperasi_nomor_urut` — Generator atomik penomoran dokumen (`no_registrasi`, `no_anggota`, `no_bukti_kas`) aman konkurensi.
4. `roles`, `permissions`, `role_permissions`, `user_roles` — RBAC granular (pemisahan verifikasi, generate, audit).
5. `users`, `refresh_tokens` — Autentikasi JWT 15 menit, rotating refresh token, anti session-hijacking.
6. `pendaftaran_anggota`, `pendaftaran_status_log`, `pendaftaran_dokumen` — Formulir pendaftaran calon anggota, KYC magic-bytes, maker-checker, enkripsi PII (AES-256-GCM + AAD), dan retensi 30 hari via `fn_purge_rejected_pendaftaran_pdp`.
7. `anggota`, `anggota_status_log` — Manajemen data anggota desa aktif/nonaktif, pencarian NIK via tenant blind index HKDF.
8. `penyedia_pembayaran`, `metode_pembayaran`, `rekening_qris_koperasi` — Manajemen rekening penampung pengurus/koperasi dan payload QRIS statis yang diunggah admin.
9. `jenis_iuran`, `tagihan_iuran` — Iuran pokok, wajib, sukarela, dan pembuatan tagihan idempoten per periode.
10. `pembayaran`, `pembayaran_alokasi`, `pembayaran_bukti`, `pembayaran_verifikasi_log` — Transaksi QRIS dengan slot kode unik anti-tabrakan, batas waktu bayar vs SLA verifikasi terpisah, audit hash chain.
11. `periode_kas`, `kas_mutasi` — Pembukuan kas append-only terpartisi, pencatatan surplus kode unik, proteksi trigger `BEFORE TRUNCATE`.
12. `konfigurasi_aturan` — Aturan bisnis berversi per tenant dengan RLS penuh (termasuk ambang nominal persetujuan ganda).
13. `outbox_event` — Transactional outbox pattern untuk dispatch notifikasi asinkron yang andal.
14. `job_log` — Jejak eksekusi penjadwalan sistem secara idempoten.

---

### Kelompok B: E-Katalog & Logistik (Milestone 2 - Siap Digunakan)
Tabel-tabel ini sudah terdefinisi lengkap di skema database dan aman di bawah isolasi Multi-Tenant RLS, siap untuk endpoint lanjutan:
1. `unit_usaha`
2. `kategori_produk`, `merek`, `supplier`
3. `produk`, `produk_varian`, `produk_media`, `produk_harga`
4. `gudang`, `stok_saldo`, `stok_mutasi`
5. `promo`, `promo_produk`, `promo_kategori`, `promo_penggunaan`
6. `keranjang`, `keranjang_item`
7. `pesanan`, `pesanan_item`, `pengiriman_pesanan`, `retur_refund`
8. `pembelian_supplier`, `penerimaan_barang`

---

### Kelompok C: Tata Kelola & Kepatuhan Internal (Milestone 3)
1. `koperasi_legalitas`, `koperasi_anggaran_dasar` — Akta notaris, AHU Kemenkumham, NIB.
2. `anggota_alamat`, `anggota_keluarga`, `anggota_kontak`, `consent_data` — Data ahli waris dan rekam persetujuan PDP UU 27/2022.
3. `pengurus_pengawas`, `rapat_anggota` — Buku daftar pengurus dan risalah Rapat Anggota Tahunan (RAT).
4. `kebijakan_keringanan_iuran`, `keringanan_iuran_riwayat` — Kebijakan dispensasi iuran anggota rentan.
5. `notifikasi` — Riwayat notifikasi in-app kepada anggota.

---

## 3. Kebijakan Retensi & Integritas Skema
1. **Pembersihan Data Otomatis (UU PDP 30 Hari)**: Pendaftaran dengan status `'ditolak'` dibersihkan otomatis setelah 30 hari via fungsi `fn_purge_rejected_pendaftaran_pdp`.
2. **Kesesuaian dengan Aturan Bisnis**: Seluruh tabel di atas secara konsisten menggunakan Primary Key `uuid_generate_v7()`, tipe uang `NUMERIC(18,2)`, dan audit field `created_at`/`created_by`.
