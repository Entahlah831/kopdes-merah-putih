-- ==============================================================================
-- ROLLBACK MIGRASI 000012
-- ==============================================================================

DROP TABLE IF EXISTS sinkronisasi_simkopdes CASCADE;
DELETE FROM konfigurasi_aturan WHERE kunci_aturan = 'pdp_retensi_pendaftaran_ditolak_hari';
