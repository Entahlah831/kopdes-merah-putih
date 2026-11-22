-- =============================================================================
-- 000002_decision_audit_enhancements.down.sql
-- =============================================================================

DELETE FROM anggota WHERE tanggal_lahir_enc IS NULL;
UPDATE pendaftaran SET tanggal_lahir_enc = '\x00' WHERE tanggal_lahir_enc IS NULL;
ALTER TABLE pendaftaran ALTER COLUMN tanggal_lahir_enc SET NOT NULL;
UPDATE anggota SET tanggal_lahir_enc = '\x00' WHERE tanggal_lahir_enc IS NULL;
ALTER TABLE anggota ALTER COLUMN tanggal_lahir_enc SET NOT NULL;

DROP TRIGGER IF EXISTS trg_worker_runs_updated ON worker_runs;
DROP TABLE IF EXISTS worker_runs;

DROP TRIGGER IF EXISTS trg_pengembalian_dana_updated ON pengembalian_dana;
DROP TABLE IF EXISTS pengembalian_dana;

ALTER TABLE pembayaran
    DROP COLUMN IF EXISTS bukti_dek_wrapped,
    DROP COLUMN IF EXISTS bukti_key_id;

ALTER TABLE pembayaran DROP CONSTRAINT IF EXISTS pembayaran_status_check;
ALTER TABLE pembayaran
    ADD CONSTRAINT pembayaran_status_check
    CHECK (status IN (
        'menunggu_pembayaran', 'menunggu_verifikasi', 'lunas',
        'perlu_tindak_lanjut', 'ditolak', 'kedaluwarsa', 'batal'
    ));

DROP INDEX IF EXISTS idx_pesanan_stok_hold_aktif;
ALTER TABLE pesanan
    DROP COLUMN IF EXISTS stok_hold_expires_at,
    DROP COLUMN IF EXISTS stok_status;

ALTER TABLE produk
    DROP CONSTRAINT IF EXISTS chk_produk_stok_tertahan,
    DROP COLUMN IF EXISTS stok_tertahan;
