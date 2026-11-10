-- ==============================================================================
-- ROLLBACK MIGRASI 000014
-- ==============================================================================

DROP TABLE IF EXISTS mutasi_bank_impor CASCADE;

ALTER TABLE pembayaran DROP COLUMN IF EXISTS persetujuan_pertama_oleh;
ALTER TABLE pembayaran DROP COLUMN IF EXISTS waktu_persetujuan_pertama;
ALTER TABLE pembayaran DROP COLUMN IF EXISTS persetujuan_kedua_oleh;
ALTER TABLE pembayaran DROP COLUMN IF EXISTS waktu_persetujuan_kedua;
ALTER TABLE pembayaran DROP COLUMN IF EXISTS is_anomali;
ALTER TABLE pembayaran DROP COLUMN IF EXISTS indikator_anomali;

DELETE FROM konfigurasi_aturan WHERE kunci_aturan = 'ambang_persetujuan_ganda_rp';
