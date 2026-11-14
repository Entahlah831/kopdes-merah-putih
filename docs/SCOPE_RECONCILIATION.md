# Rekonsiliasi Cakupan Skema Database (Scope Reconciliation)
**Sistem Backend Koperasi Desa Merah Putih (PostgreSQL 16 & Go)**

---

## 1. Ringkasan Inventaris Skema (Fokus Cabang Lokal + Adapter Siap-Ekstensi)
Berdasarkan konfigurasi cakupan cabang lokal sementara (lihat keputusan yang diusulkan di `docs/AUDIT_REPORT.md` Bagian 3 — menunggu konfirmasi resmi pemilik sistem):
- **Lingkup Implementasi**: Difokuskan untuk **satu kantor cabang Kopdes lokal**.
- **Riwayat Tabel `sinkronisasi_simkopdes`**:
  - Tabel `sinkronisasi_simkopdes` sempat dihapus (*de-scoped*) melalui migrasi `000009_local_scope_and_pdp_retention.up.sql`.
  - Atas instruksi pemeliharaan arsitektur agar batas integrasi tetap terjaga tanpa kehilangan jejak skema, tabel `sinkronisasi_simkopdes` **dipulihkan** melalui migrasi `000012_pdp_purge_hardening_and_restore_simkopdes.up.sql` lengkap dengan kebijakan RLS tenant isolation dan adapter stub Go (`internal/simkopdes/adapter.go` yang mengembalikan `ErrSIMKOPDESDeScoped`).
- **Riwayat Pertambahan Tabel Baru Selama Remediasi**:
  - Skema awal (`000001`): **65 tabel dasar/induk** (termasuk `sinkronisasi_simkopdes` dan 4 tabel induk terpartisi) + **8 partisi bulanan awal**.
  - Migrasi `000005`: menambah `refresh_tokens` (+1).
  - Migrasi `000009` & `000012`: menghapus lalu memulihkan `sinkronisasi_simkopdes` (net 0).
  - Migrasi `000013`: menambah `idempotency_keys` (+1).
  - Migrasi `000014`: menambah `mutasi_bank_impor` (+1).
  - Migrasi `000016`: menambah `mfa_login_sessions` (+1).
- **Total Tabel Dasar / Induk Terkini**: **69 tabel** (65 tabel reguler + 4 tabel induk terpartisi: `audit_log`, `kas_mutasi`, `notifikasi`, `stok_mutasi`).
- **Total Partisi Dasar Terdaftar**: `>= 60` partisi anak (masing-masing tabel induk memiliki partisi bulanan sejak `y2026m11` sampai `y2027m12` plus partisi `default`, dan fungsi `fn_ensure_future_partitions(3)` terus menambah partisi 3 bulan ke depan secara otomatis).
- **Total Objek Tabel Keseluruhan (`relkind IN ('r','p')`)**: **`129` entitas tabel fisik** di skema `public` (terverifikasi langsung via `psql` di `kopdes_test` maupun pasca-restorasi di `scripts/restore_drill.sh`).

---

## 2. Pengelompokan Tabel: Aktif vs Cadangan Milestone

### Kelompok A: Core Foundation & Active API Scope (Milestone 1 - Aktif)
Tabel-tabel ini memiliki endpoint API, validasi bisnis, otorisasi peran, dan pengujian otomatis (TDD):
1. `wilayah` — Master data wilayah administratif desa/kecamatan/kabupaten.
2. `koperasi` — Profil tenant koperasi desa, status legalitas, dan isolasi RLS.
3. `koperasi_nomor_urut` — Generator atomik penomoran dokumen (`no_registrasi`, `no_anggota`, `no_bukti_kas`) aman konkurensi.
4. `roles`, `permissions`, `role_permissions`, `user_roles` — RBAC granular (pemisahan verifikasi, generate, audit).
5. `users`, `refresh_tokens`, `mfa_login_sessions` — Autentikasi JWT 15 menit, rotating refresh token berdeteksi reuse, dan MFA TOTP RFC 6238.
6. `pendaftaran_anggota`, `pendaftaran_status_log`, `pendaftaran_dokumen`, `dokumen` — Formulir pendaftaran calon anggota, KYC magic-bytes, maker-checker, enkripsi PII (AES-256-GCM + AAD), dan pembersihan tertolak terjadwal via `fn_purge_rejected_pendaftaran_pdp`.
7. `anggota`, `anggota_status_log` — Manajemen data anggota desa aktif/nonaktif, pencarian NIK via tenant blind index HKDF.
8. `penyedia_pembayaran`, `metode_pembayaran`, `rekening_qris_koperasi` — Manajemen rekening penampung koperasi/pilot pengurus dan payload QRIS statis.
9. `jenis_iuran`, `tagihan_iuran` — Konfigurasi jenis iuran berlabel netral (`KEPUTUSAN AKUNTAN`) dan pembuatan tagihan idempoten per periode.
10. `pembayaran`, `pembayaran_alokasi`, `pembayaran_bukti`, `pembayaran_verifikasi_log`, `mutasi_bank_impor`, `idempotency_keys` — Transaksi QRIS dengan slot kode unik anti-tabrakan, idempotensi berbasis DB, impor mutasi CSV bank, persetujuan ganda 4-mata, batas waktu bayar vs SLA verifikasi terpisah, serta audit hash chain.
11. `periode_kas`, `kas_mutasi` — Pembukuan kas append-only terpartisi, pencatatan surplus kode unik, proteksi trigger `BEFORE TRUNCATE` & hash chain `BEFORE INSERT`.
12. `konfigurasi_aturan` — Aturan bisnis berversi per tenant dengan RLS penuh (termasuk ambang nominal persetujuan ganda, `mutasi_auto_approve_enabled`, batas pilot, dan retensi PDP).
13. `outbox_event` — Transactional outbox pattern untuk dispatch notifikasi asinkron WhatsApp/SMS via adapter.
14. `job_log` — Jejak eksekusi penjadwalan sistem secara idempoten.
15. `sinkronisasi_simkopdes` — Dipulihkan via migrasi `000012` dengan RLS penuh; integrasi eksternal disekat melalui adapter stub (`internal/simkopdes/adapter.go`).

---

### Kelompok B: E-Katalog, Toko & Logistik (Aktif + Skema Pendukung)
Tabel-tabel ini memiliki endpoint aktif pada `/api/v1/katalog/*` dan `/api/v1/toko/*` (pemotongan stok atomik, promo berkuota, checkout, dan pembatalan/kedaluwarsa):
1. `unit_usaha`
2. `kategori_produk`, `merek`, `supplier`
3. `produk`, `produk_varian`, `produk_media`, `produk_harga`
4. `gudang`, `stok_saldo`, `stok_mutasi`
5. `promo`, `promo_produk`, `promo_kategori`, `promo_penggunaan`
6. `keranjang`, `keranjang_item`
7. `pesanan`, `pesanan_item`, `pengiriman_pesanan`, `retur_refund`
8. `pembelian_supplier`, `penerimaan_barang`

---

### Kelompok C: Tata Kelola & Kepatuhan Internal (Skema Dasar Terproteksi RLS)
1. `koperasi_legalitas`, `koperasi_anggaran_dasar` — Akta notaris, AHU Kemenkumham, NIB.
2. `anggota_alamat`, `anggota_keluarga`, `anggota_kontak`, `consent_data` — Data ahli waris dan rekam persetujuan PDP UU 27/2022 (`PERLU VERIFIKASI HUKUM`).
3. `pengurus_pengawas`, `rapat_anggota` — Buku daftar pengurus dan risalah Rapat Anggota Tahunan (RAT).
4. `kebijakan_keringanan_iuran`, `keringanan_iuran_riwayat` — Kebijakan dispensasi iuran anggota rentan.
5. `notifikasi`, `notifikasi_template` — Riwayat dan templat notifikasi kepada anggota.

---

## 3. Kebijakan Retensi & Integritas Skema
1. **Pembersihan Data Calon Anggota Ditolak (`PDP_REJECTED_RETENTION_DAYS`, default `30` hari — `PERLU VERIFIKASI HUKUM`)**: Pendaftaran dengan status `'ditolak'` dihapus baris PII-nya (`pendaftaran_anggota`, `pendaftaran_dokumen`, `dokumen`) dan berkas fisik KYC-nya di disk (`os.Remove`) secara terjadwal via `fn_purge_rejected_pendaftaran_pdp`, sambil mempertahankan jejak status tersanitasi tanpa PII di `pendaftaran_status_log` dan mencatat metrik anonim di `audit_log`.
2. **Kesesuaian dengan Aturan Bisnis**: Seluruh tabel di atas secara konsisten menggunakan Primary Key `uuid_generate_v7()`, tipe uang `NUMERIC(18,2)`, dan audit field `created_at`/`created_by`.
