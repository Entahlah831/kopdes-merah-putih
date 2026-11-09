-- =============================================================================
-- Migrasi 000009: Penyesuaian Lingkup Lokal Satu Cabang Kopdes & Retensi PDP 30 Hari
-- =============================================================================

-- 1. Hapus tabel sinkronisasi_simkopdes (De-scoped: fokus lokal satu kantor cabang koperasi, tanpa pelaporan nasional SIMKOPDES)
DROP TABLE IF EXISTS sinkronisasi_simkopdes CASCADE;

-- 2. Fungsi pembersihan otomatis berkas KYC pendaftaran ditolak setelah 30 hari (UU 27/2022 PDP)
CREATE OR REPLACE FUNCTION fn_purge_rejected_pendaftaran_pdp(
    p_koperasi_id uuid,
    p_retention_days int DEFAULT 30
)
RETURNS int
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_purged_count int := 0;
BEGIN
    -- 1. Hapus dokumen fisik/foto KYC dari pendaftaran yang berstatus 'ditolak' lebih dari 30 hari
    DELETE FROM pendaftaran_dokumen pd
    USING pendaftaran_anggota pa
    WHERE pd.pendaftaran_id = pa.id
      AND pa.koperasi_id = p_koperasi_id
      AND pa.status = 'ditolak'
      AND pa.updated_at < (NOW() - (p_retention_days || ' days')::interval);

    -- 2. Hapus baris pendaftaran ditolak (crypto-shredding PII)
    WITH deleted_rows AS (
        DELETE FROM pendaftaran_anggota
        WHERE koperasi_id = p_koperasi_id
          AND status = 'ditolak'
          AND updated_at < (NOW() - (p_retention_days || ' days')::interval)
        RETURNING id
    )
    SELECT count(*) INTO v_purged_count FROM deleted_rows;

    RETURN v_purged_count;
END;
$$;

COMMENT ON FUNCTION fn_purge_rejected_pendaftaran_pdp IS 'Prosedur pembersihan PII dan berkas KYC calon anggota yang ditolak setelah masa retensi 30 hari (Kepatuhan UU 27/2022 PDP).';
