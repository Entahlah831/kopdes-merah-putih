-- Migrasi 000008: Mendukung kode unik dinamis (3 digit: 1..999 atau 4 digit: 1..9999)
ALTER TABLE pembayaran DROP CONSTRAINT IF EXISTS pembayaran_kode_unik_check;
ALTER TABLE pembayaran ADD CONSTRAINT chk_pembayaran_kode_unik CHECK (kode_unik BETWEEN 1 AND 9999);

ALTER TABLE pembayaran DROP CONSTRAINT IF EXISTS pembayaran_kode_unik_awal_check;
ALTER TABLE pembayaran ADD CONSTRAINT chk_pembayaran_kode_unik_awal CHECK (kode_unik_awal BETWEEN 1 AND 9999);
