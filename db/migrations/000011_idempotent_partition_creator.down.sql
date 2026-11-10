-- ==============================================================================
-- ROLLBACK MIGRASI 000011: Hapus Generator Partisi SQL Idempoten
-- ==============================================================================

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_cron') THEN
        PERFORM cron.unschedule('kopdes_ensure_future_partitions');
    END IF;
EXCEPTION WHEN OTHERS THEN
    NULL;
END;
$$;

DROP FUNCTION IF EXISTS fn_ensure_future_partitions(integer);
