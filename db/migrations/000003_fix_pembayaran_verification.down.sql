-- ==============================================================================
-- ROLLBACK MIGRASI 000003: Revert Prosedur Persetujuan & Klaim Pembayaran (K3)
-- ==============================================================================

DROP FUNCTION IF EXISTS fn_klaim_bukti_pembayaran(uuid, uuid, varchar, text);
-- Catatan: fn_setujui_pembayaran_qris dikembalikan ke definisi skema 000001
