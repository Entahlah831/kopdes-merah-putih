-- ==============================================================================
-- ROLLBACK MIGRASI 000013: Hapus Tabel Idempotensi
-- ==============================================================================

DROP TABLE IF EXISTS idempotency_keys CASCADE;
