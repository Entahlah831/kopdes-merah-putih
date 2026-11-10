# Entity Relationship Diagram (ERD) — Kopdes Merah Putih

Dokumen ini memetakan arsitektur data relasional PostgreSQL 16 untuk aplikasi Koperasi Desa (Kopdes) Merah Putih.

## 1. Diagram ERD Inti (Mermaid)

```mermaid
erDiagram
    wilayah ||--o{ koperasi : "lokasi desa"
    koperasi ||--o{ users : "karyawan/pengurus"
    koperasi ||--o{ anggota : "keanggotaan"
    koperasi ||--o{ rekening_qris_koperasi : "rekening merchant"
    koperasi ||--o{ jenis_iuran : "master tarif"
    koperasi ||--o{ produk : "katalog ritel"
    koperasi ||--o{ gudang : "penyimpanan"
    koperasi ||--o{ pesanan : "transaksi toko"
    koperasi ||--o{ pembayaran : "transaksi kas"
    koperasi ||--o{ periode_kas : "tutup buku"

    users ||--o{ user_roles : "memiliki peran"
    roles ||--o{ user_roles : "ditetapkan ke"
    roles ||--o{ role_permissions : "hak akses"
    permissions ||--o{ role_permissions : "diizinkan"

    anggota ||--o{ anggota_alamat : "alamat domisili"
    anggota ||--o{ anggota_kontak : "telepon/sosmed"
    anggota ||--o{ tagihan_iuran : "kewajiban bulanan"
    anggota ||--o{ pesanan : "belanja"
    anggota ||--o{ pembayaran : "pembayaran qris"

    jenis_iuran ||--o{ tagihan_iuran : "referensi nominal"

    produk ||--o{ produk_varian : "varian sku"
    produk ||--o{ produk_harga : "riwayat tarif"
    produk ||--o{ stok_saldo : "saldo fisik"

    gudang ||--o{ stok_saldo : "lokasi stok"
    stok_saldo ||--o{ stok_mutasi : "riwayat mutasi (append-only)"

    pesanan ||--o{ pesanan_item : "rincian barang"
    rekening_qris_koperasi ||--o{ pembayaran : "tujuan transfer"
    pembayaran ||--o{ pembayaran_alokasi : "alokasi pelunasan"
    pembayaran ||--o{ pembayaran_bukti : "bukti transfer"
    pembayaran ||--o{ pembayaran_verifikasi_log : "verifikasi (append-only)"
    pembayaran ||--o{ kas_mutasi : "jurnal kas (append-only)"

    koperasi {
        uuid id PK
        string kode_koperasi
        string nama
        uuid wilayah_desa_id FK
        string status
    }

    anggota {
        uuid id PK
        uuid koperasi_id FK
        string no_anggota
        string nik_hash
        bytea nik_enkripsi
        string nama_lengkap
        string status
    }

    rekening_qris_koperasi {
        uuid id PK
        uuid koperasi_id FK
        string nmid
        string nama_merchant
        boolean status_aktif
    }

    pembayaran {
        uuid id PK
        uuid koperasi_id FK
        string no_pembayaran
        uuid pembayar_user_id FK
        uuid rekening_qris_id FK
        numeric nominal_dasar
        integer kode_unik
        numeric nominal_total
        integer kode_unik_awal
        integer jumlah_percobaan
        string status
        uuid diverifikasi_oleh FK
    }

    tagihan_iuran {
        uuid id PK
        uuid koperasi_id FK
        uuid anggota_id FK
        uuid jenis_iuran_id FK
        string periode
        numeric nominal_pokok
        string status
    }
```

## 2. Prinsip Arsitektur Database
1. **Multi-Tenant Row-Level Security (RLS)**:
   - Setiap tabel bisnis dilengkapi `koperasi_id uuid NOT NULL REFERENCES koperasi(id)`.
   - RLS diaktifkan (`FORCE ROW LEVEL SECURITY`) dengan kebijakan `koperasi_id = app_current_koperasi_id()`.
2. **Anti-Tabrakan QRIS Statis**:
   - Partial unique index `idx_pembayaran_qris_nominal_aktif` pada `(koperasi_id, rekening_qris_id, nominal_total)` untuk transaksi pending (`menunggu_pembayaran`, `menunggu_verifikasi`).
   - Algoritma pencarian kode unik (1..999) menggunakan HMAC-SHA256 deterministik + linear probing dengan PostgreSQL `SAVEPOINT`.
3. **Integritas Keuangan & Audit Append-Only**:
   - Dilarang `UPDATE` / `DELETE` pada tabel audit dan jurnal transaksi: `kas_mutasi`, `stok_mutasi`, `pembayaran_verifikasi_log`, `anggota_status_log`, `pendaftaran_status_log`, dan `audit_log`.
4. **Perlindungan Data Pribadi (Enkripsi AES-256-GCM)**:
   - Data sensitif seperti NIK, nomor HP, email, alamat disimpan terenkripsi (`*_enkripsi`, AES-256-GCM + AAD) dengan indeks blind-index HKDF-SHA256 (`*_hash`). Evaluasi kepatuhan hukum formal memerlukan peninjauan ahli hukum secara berkala.
