-- ==============================================================================
-- MIGRASI 000011: Generator Partisi SQL Idempoten 3 Bulan ke Depan & Terjadwal (Task 2)
-- Tabel berpartisi: audit_log, kas_mutasi, stok_mutasi, notifikasi
-- ==============================================================================

CREATE OR REPLACE FUNCTION fn_ensure_future_partitions(p_months_ahead integer DEFAULT 3)
RETURNS jsonb
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    v_table text;
    v_tables text[] := ARRAY['audit_log', 'kas_mutasi', 'stok_mutasi', 'notifikasi'];
    v_month_offset integer;
    v_start timestamptz;
    v_end timestamptz;
    v_part_name text;
    v_created_count integer := 0;
    v_details jsonb := '[]'::jsonb;
BEGIN
    IF p_months_ahead IS NULL OR p_months_ahead < 1 THEN
        p_months_ahead := 3;
    END IF;

    FOREACH v_table IN ARRAY v_tables LOOP
        FOR v_month_offset IN 0..p_months_ahead LOOP
            v_start := date_trunc('month', clock_timestamp() + (v_month_offset || ' month')::interval);
            v_end := date_trunc('month', clock_timestamp() + ((v_month_offset + 1) || ' month')::interval);
            v_part_name := v_table || '_y' || to_char(v_start, 'YYYY') || 'm' || to_char(v_start, 'MM');

            IF NOT EXISTS (
                SELECT 1 FROM pg_class WHERE relname = v_part_name
            ) THEN
                BEGIN
                    EXECUTE format(
                        'CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L);',
                        v_part_name, v_table, v_start, v_end
                    );

                    -- Atur hak akses non-superuser aplikasi dan read-only auditor
                    EXECUTE format('GRANT SELECT, INSERT, UPDATE ON %I TO kopdes_app;', v_part_name);
                    IF v_table IN ('audit_log', 'kas_mutasi', 'stok_mutasi') THEN
                        EXECUTE format('REVOKE UPDATE, DELETE ON %I FROM kopdes_app;', v_part_name);
                    END IF;
                    EXECUTE format('GRANT SELECT ON %I TO kopdes_readonly_auditor;', v_part_name);

                    v_created_count := v_created_count + 1;
                    v_details := v_details || jsonb_build_object('table', v_table, 'partition', v_part_name, 'status', 'created');
                EXCEPTION WHEN duplicate_table THEN
                    -- Diabaikan jika dibuat serentak oleh worker lain
                    NULL;
                END;
            END IF;
        END LOOP;
    END LOOP;

    RETURN jsonb_build_object(
        'created_count', v_created_count,
        'months_ahead', p_months_ahead,
        'details', v_details
    );
END;
$$;

COMMENT ON FUNCTION fn_ensure_future_partitions(integer) IS 
'Fungsi SQL idempoten pembuat partisi bulanan ke depan untuk tabel berpartisi Kopdes. Dijalankan terjadwal oleh runner Go atau pg_cron.';

REVOKE EXECUTE ON FUNCTION fn_ensure_future_partitions(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fn_ensure_future_partitions(integer) TO kopdes_app;

-- Integrasi pg_cron jika ekstensi terpasang di klaster PostgreSQL
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_cron') THEN
        PERFORM cron.schedule('kopdes_ensure_future_partitions', '0 1 1 * *', 'SELECT fn_ensure_future_partitions(3)');
    END IF;
EXCEPTION WHEN OTHERS THEN
    NULL;
END;
$$;
