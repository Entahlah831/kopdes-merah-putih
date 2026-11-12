# Arsitektur & Spesifikasi Urutan Advisory Lock Hash Chain

Dokumen ini mendefinisikan hierarki penguncian transaksional (*transactional advisory lock*) pada tabel-tabel *append-only* berantai hash kriptografis (*hash chain*) di Kopdes Merah Putih, guna menjamin integritas rantai tanpa percabangan (*zero-fork*) dan mencegah kebuntuan (*deadlock prevention*).

---

## 1. Latar Belakang & Masalah Konkurensi
Setiap pencatatan data ke tabel *append-only* menghitung hash kriptografis:
$$\text{row\_hash} = \text{SHA256}(\text{prev\_hash} \parallel \text{table} \parallel \text{id} \parallel \text{koperasi\_id} \parallel \text{created\_at})$$

Di mana $\text{prev\_hash}$ diambil dari baris terakhir baris data tenant terkait. Apabila beberapa transaksi konkuren menyisipkan data secara bersamaan tanpa serialisasi:
1. Dapat terjadi *race condition* di mana dua transaksi membaca $\text{prev\_hash}$ yang sama, menghasilkan percabangan rantai (*chain fork*).
2. Jika dua transaksi menyisipkan ke beberapa tabel sekaligus dalam urutan berbeda (misalnya Transaksi A: `kas_mutasi` $\to$ `audit_log`, sedangkan Transaksi B: `audit_log` $\to$ `kas_mutasi`), PostgreSQL akan mendeteksi *deadlock* (`ERROR: deadlock detected`).

---

## 2. Mekanisme Serialisasi Berbasis Advisory Lock
Di PostgreSQL 16, serialisasi diterapkan via trigger `BEFORE INSERT` fungsi `fn_maintain_hash_chain()` menggunakan:
```sql
v_lock_key := ('x' || substr(md5(v_base_table || ':' || NEW.koperasi_id::text), 1, 16))::bit(64)::bigint;
PERFORM pg_advisory_xact_lock(v_lock_key);
```
Kunci advisory ini terikat pada transaksi berjalan (*transaction-scoped*) dan otomatis dilepaskan saat transaksi melakukan `COMMIT` atau `ROLLBACK`.

---

## 3. Hierarki Urutan Kunci Kanonikal (Deadlock Prevention Hierarchy)
Untuk menjamin pencegahan *deadlock* sesuai Teorema Havender (1968), seluruh alur transaksi yang memodifikasi lebih dari satu tabel *append-only* **WAJIB** mengeksekusi operasi penyisipan (*insert*) secara berurutan sesuai hierarki kanonikal alfabetis berikut:

| Level | Tabel Append-Only | Keterangan & Peruntukan |
|:---:|:---|:---|
| **1** | `anggota_status_log` | Riwayat transisi status keanggotaan warga desa |
| **2** | `audit_log` | Log jejak audit keamanan & aktivitas operasional pengguna |
| **3** | `kas_mutasi` | Mutasi debit/kredit saldo kas koperasi desa |
| **4** | `pembayaran_verifikasi_log` | Log jejak verifikasi dan persetujuan pembayaran tagihan/iuran |
| **5** | `pendaftaran_status_log` | Riwayat alur pendaftaran calon anggota |
| **6** | `stok_mutasi` | Mutasi kuantitas stok produk retail/toko koperasi |

### Aturan Eksekusi Transaksi:
1. **Urutan Menanjak (Ascending Only)**: Jika suatu alur membutuhkan penulisan ke `kas_mutasi`, `pembayaran_verifikasi_log`, dan `audit_log`, maka penyisipan wajib dimulai dari `audit_log` (Level 2), kemudian `kas_mutasi` (Level 3), dan terakhir `pembayaran_verifikasi_log` (Level 4).
2. **Dilarang Interleaving / Acak**: Dilarang menulis `pembayaran_verifikasi_log` sebelum `kas_mutasi` atau `audit_log`.

---

## 4. Indeks Pencarian Baris Terakhir (Last-Row Lookup Index)
Pencarian `prev_hash` baris terakhir memanfaatkan indeks komposit monolitik:
```sql
CREATE INDEX idx_audit_log_last_row ON audit_log (koperasi_id, created_at DESC, id DESC);
CREATE INDEX idx_kas_mutasi_last_row ON kas_mutasi (koperasi_id, created_at DESC, id DESC);
CREATE INDEX idx_pembayaran_verifikasi_log_last_row ON pembayaran_verifikasi_log (koperasi_id, created_at DESC, id DESC);
CREATE INDEX idx_stok_mutasi_last_row ON stok_mutasi (koperasi_id, created_at DESC, id DESC);
CREATE INDEX idx_pendaftaran_status_log_last_row ON pendaftaran_status_log (koperasi_id, created_at DESC, id DESC);
CREATE INDEX idx_anggota_status_log_last_row ON anggota_status_log (koperasi_id, created_at DESC, id DESC);
```
Dengan indeks ini, eksekusi:
```sql
SELECT row_hash FROM table WHERE koperasi_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1;
```
dieksekusi dalam kompleksitas $\mathcal{O}(1)$ melalui *Index Scan Backward*, menghilangkan beban *sequential scan* pada tabel bernilai jutaan baris.
