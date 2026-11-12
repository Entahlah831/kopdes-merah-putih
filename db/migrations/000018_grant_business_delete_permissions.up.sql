-- =============================================================================
-- Migrasi 000018: Izin DELETE Tabel Bisnis Non-Append-Only & Proteksi Append-Only
-- =============================================================================
-- Memberikan hak DELETE pada tabel bisnis reguler (keranjang_item, dll) untuk
-- role non-superuser kopdes_app, dengan tetap mempertahankan REVOKE UPDATE, DELETE
-- pada seluruh tabel append-only sesuai aturan Rule 4.
-- =============================================================================

GRANT DELETE ON ALL TABLES IN SCHEMA public TO kopdes_app;

-- Pertahankan larangan keras UPDATE, DELETE, TRUNCATE pada tabel append-only
REVOKE UPDATE, DELETE, TRUNCATE ON 
    audit_log, 
    pendaftaran_status_log, 
    anggota_status_log, 
    kas_mutasi, 
    stok_mutasi, 
    pembayaran_verifikasi_log 
FROM kopdes_app;
