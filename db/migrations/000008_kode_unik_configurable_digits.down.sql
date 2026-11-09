ALTER TABLE pembayaran DROP CONSTRAINT IF EXISTS chk_pembayaran_kode_unik;
ALTER TABLE pembayaran ADD CONSTRAINT pembayaran_kode_unik_check CHECK (kode_unik BETWEEN 1 AND 999);

ALTER TABLE pembayaran DROP CONSTRAINT IF EXISTS chk_pembayaran_kode_unik_awal;
ALTER TABLE pembayaran ADD CONSTRAINT pembayaran_kode_unik_awal_check CHECK (kode_unik_awal BETWEEN 1 AND 999);
