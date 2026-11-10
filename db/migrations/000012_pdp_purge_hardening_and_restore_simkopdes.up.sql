-- ==============================================================================
-- MIGRASI 000012: Penguatan Pembersihan PDP, Audit Tanpa PII & Pemulihan SIMKOPDES (Task 3)
-- ==============================================================================

-- 1. Pulihkan tabel sinkronisasi_simkopdes (Adapter Stub)
CREATE TABLE IF NOT EXISTS sinkronisasi_simkopdes (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    kode_wilayah_bps varchar(20) NOT NULL,
    kode_koperasi_kemenkop varchar(50) NOT NULL,
    periode_laporan char(7) NOT NULL CHECK (periode_laporan ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    jenis_payload varchar(40) NOT NULL CHECK (jenis_payload IN ('profil_legalitas', 'rekap_anggota', 'rekap_iuran', 'rekap_unit_usaha_katalog')),
    payload_json jsonb NOT NULL,
    checksum_payload char(64) NOT NULL,
    status_kirim status_sinkronisasi_enum NOT NULL DEFAULT 'antrean',
    jumlah_retry integer NOT NULL DEFAULT 0 CHECK (jumlah_retry >= 0),
    http_status_terakhir integer,
    respon_terakhir text,
    jadwal_retry_berikutnya timestamptz,
    waktu_berhasil_kirim timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id)
);
COMMENT ON TABLE sinkronisasi_simkopdes IS 'Antrean & log sinkronisasi pelaporan ke SIMKOPDES Kemenkop (Permenkop 2/2025; skema endpoint API PERLU VERIFIKASI HUKUM & ADAPTER STUB).';

ALTER TABLE sinkronisasi_simkopdes ENABLE ROW LEVEL SECURITY;
ALTER TABLE sinkronisasi_simkopdes FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS pol_isolasi_tenant_sinkronisasi_simkopdes ON sinkronisasi_simkopdes;
CREATE POLICY pol_isolasi_tenant_sinkronisasi_simkopdes ON sinkronisasi_simkopdes
    USING (app_is_platform_admin() OR koperasi_id = app_current_koperasi_id())
    WITH CHECK (app_is_platform_admin() OR koperasi_id = app_current_koperasi_id());

GRANT SELECT, INSERT, UPDATE ON sinkronisasi_simkopdes TO kopdes_app;
GRANT SELECT ON sinkronisasi_simkopdes TO kopdes_readonly_auditor;

-- 2. Hapus FK dari tabel append-only pendaftaran_status_log ke pendaftaran_anggota
-- Hal ini memungkinkan penghapusan fisik PII pendaftaran_anggota tanpa melanggar FK atau mengubah append-only status log
ALTER TABLE pendaftaran_status_log DROP CONSTRAINT IF EXISTS pendaftaran_status_log_pendaftaran_id_fkey;

-- 3. Masukkan aturan konfigurasi retensi PDP (PERLU VERIFIKASI HUKUM)
INSERT INTO konfigurasi_aturan (
    koperasi_id, kunci_aturan, versi, nilai_json, keterangan, berlaku_mulai, status_aktif
) VALUES (
    NULL,
    'pdp_retensi_pendaftaran_ditolak_hari',
    1,
    '{"hari": 30}'::jsonb,
    'Masa retensi penghapusan berkas KYC calon anggota yang ditolak sesuai prinsip pembatasan penyimpanan UU 27/2022 PDP. PERLU VERIFIKASI HUKUM.',
    NOW(),
    true
) ON CONFLICT (koperasi_id, kunci_aturan, versi) DO UPDATE 
SET nilai_json = EXCLUDED.nilai_json, keterangan = EXCLUDED.keterangan;

-- 4. Penguatan Prosedur fn_purge_rejected_pendaftaran_pdp
CREATE OR REPLACE FUNCTION fn_purge_rejected_pendaftaran_pdp(
    p_koperasi_id uuid,
    p_retention_days int DEFAULT NULL
)
RETURNS int
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    v_current_koperasi_id text := current_setting('app.current_koperasi_id', true);
    v_is_platform_admin text := current_setting('app.is_platform_admin', true);
    v_retention_days int := p_retention_days;
    v_purged_count int := 0;
BEGIN
    -- A. Validasi p_koperasi_id terhadap app_current_koperasi_id()
    IF v_is_platform_admin IS DISTINCT FROM 'true' THEN
        IF v_current_koperasi_id IS NULL OR v_current_koperasi_id = '' OR v_current_koperasi_id::uuid <> p_koperasi_id THEN
            RAISE EXCEPTION 'AKSES DITOLAK: Tenant id % tidak sesuai dengan konteks sesi terautentikasi (%)', p_koperasi_id, v_current_koperasi_id
                USING ERRCODE = '42501';
        END IF;
    END IF;

    -- B. Ambil masa retensi dari konfigurasi_aturan jika tidak ditentukan (PERLU VERIFIKASI HUKUM)
    IF v_retention_days IS NULL OR v_retention_days <= 0 THEN
        SELECT COALESCE((nilai_json->>'hari')::int, 30) INTO v_retention_days
        FROM konfigurasi_aturan
        WHERE (koperasi_id = p_koperasi_id OR koperasi_id IS NULL)
          AND kunci_aturan = 'pdp_retensi_pendaftaran_ditolak_hari'
          AND status_aktif = true
          AND deleted_at IS NULL
        ORDER BY koperasi_id NULLS LAST, versi DESC
        LIMIT 1;

        IF v_retention_days IS NULL THEN
            v_retention_days := 30;
        END IF;
    END IF;

    -- C. Hapus pendaftaran_dokumen terlebih dahulu dan bersihkan entri dokumen terkait
    WITH deleted_pendaftaran_dokumen AS (
        DELETE FROM pendaftaran_dokumen pd
        USING pendaftaran_anggota pa
        WHERE pd.pendaftaran_id = pa.id
          AND pa.koperasi_id = p_koperasi_id
          AND pa.status = 'ditolak'
          AND pa.updated_at < (clock_timestamp() - (v_retention_days || ' days')::interval)
        RETURNING pd.dokumen_id
    )
    DELETE FROM dokumen d
    WHERE d.id IN (SELECT dokumen_id FROM deleted_pendaftaran_dokumen);

    -- D. Hapus pendaftaran_anggota (crypto-shredding PII)
    WITH deleted_rows AS (
        DELETE FROM pendaftaran_anggota
        WHERE koperasi_id = p_koperasi_id
          AND status = 'ditolak'
          AND updated_at < (clock_timestamp() - (v_retention_days || ' days')::interval)
        RETURNING id
    )
    SELECT count(*) INTO v_purged_count FROM deleted_rows;

    -- F. Catat aksi ke audit_log tanpa PII
    IF v_purged_count > 0 THEN
        INSERT INTO audit_log (
            koperasi_id,
            user_id,
            nama_tabel,
            record_id,
            aksi,
            data_baru,
            created_at
        ) VALUES (
            p_koperasi_id,
            NULL,
            'pendaftaran_anggota',
            p_koperasi_id,
            'ANONYMIZE',
            jsonb_build_object(
                'purged_count', v_purged_count,
                'retention_days', v_retention_days,
                'catatan', 'Pembersihan berkas pendaftaran ditolak (UU 27/2022 PDP). PERLU VERIFIKASI HUKUM.'
            ),
            clock_timestamp()
        );
    END IF;

    RETURN v_purged_count;
END;
$$;

COMMENT ON FUNCTION fn_purge_rejected_pendaftaran_pdp(uuid, int) IS 
'Pembersihan PII & berkas KYC pendaftaran ditolak setelah masa retensi sesuai konfigurasi (PERLU VERIFIKASI HUKUM). Validasi tenant ketat & audit tanpa PII.';

REVOKE ALL ON FUNCTION fn_purge_rejected_pendaftaran_pdp(uuid, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fn_purge_rejected_pendaftaran_pdp(uuid, int) TO kopdes_app;
