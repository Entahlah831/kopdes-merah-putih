-- ==============================================================================
-- MIGRASI 000017: Penguatan Mutasi Bank, MFA, dan Urutan Kunci Hash Chain
-- ==============================================================================

-- 1. Penguatan Mutasi Bank Impor (Hash Baris Unik & 1-to-1 Matching Constraint)
ALTER TABLE mutasi_bank_impor ADD COLUMN IF NOT EXISTS row_hash varchar(64);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mutasi_bank_row_hash ON mutasi_bank_impor(koperasi_id, row_hash);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mutasi_bank_pembayaran_1to1 
    ON mutasi_bank_impor(koperasi_id, pembayaran_id) 
    WHERE pembayaran_id IS NOT NULL AND status_rekonsiliasi IN ('tercocokkan', 'disetujui');

-- Konfigurasi bawaan auto-approve mutasi (default FALSE: wajib konfirmasi orang kedua)
INSERT INTO konfigurasi_aturan (koperasi_id, kunci_aturan, nilai_json, keterangan, berlaku_mulai, status_aktif, created_by)
SELECT NULL, 'mutasi_auto_approve_enabled', '{"enabled": false}'::jsonb, 'Fitur auto-approve mutasi bank (default nonaktif, butuh konfirmasi orang kedua)', NOW(), true, NULL
WHERE NOT EXISTS (SELECT 1 FROM konfigurasi_aturan WHERE kunci_aturan = 'mutasi_auto_approve_enabled');

-- 2. Penguatan MFA (Recovery Codes, Anti-Replay Step Tracking & Attempt Limiting)
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_totp_step bigint NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_recovery_codes bytea;

ALTER TABLE mfa_login_sessions ADD COLUMN IF NOT EXISTS attempts integer NOT NULL DEFAULT 0;
ALTER TABLE mfa_login_sessions ADD COLUMN IF NOT EXISTS is_locked boolean NOT NULL DEFAULT false;

-- Perbarui fungsi SECURITY DEFINER fn_get_user_mfa
DROP FUNCTION IF EXISTS fn_get_user_mfa(uuid);
CREATE OR REPLACE FUNCTION fn_get_user_mfa(p_user_id uuid)
RETURNS TABLE (
    mfa_enabled boolean,
    totp_secret_enkripsi bytea,
    last_totp_step bigint,
    mfa_recovery_codes bytea
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
BEGIN
    RETURN QUERY
    SELECT u.mfa_enabled, u.totp_secret_enkripsi, u.last_totp_step, u.mfa_recovery_codes
    FROM users u
    WHERE u.id = p_user_id AND u.deleted_at IS NULL;
END;
$$;

REVOKE ALL ON FUNCTION fn_get_user_mfa(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fn_get_user_mfa(uuid) TO kopdes_app;

-- Perbarui fungsi SECURITY DEFINER fn_set_user_mfa
DROP FUNCTION IF EXISTS fn_set_user_mfa(uuid, boolean, bytea);
CREATE OR REPLACE FUNCTION fn_set_user_mfa(
    p_user_id uuid,
    p_mfa_enabled boolean,
    p_totp_secret_enkripsi bytea,
    p_last_totp_step bigint DEFAULT 0,
    p_mfa_recovery_codes bytea DEFAULT NULL
)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
BEGIN
    UPDATE users
    SET mfa_enabled = p_mfa_enabled,
        totp_secret_enkripsi = p_totp_secret_enkripsi,
        last_totp_step = COALESCE(p_last_totp_step, last_totp_step),
        mfa_recovery_codes = COALESCE(p_mfa_recovery_codes, mfa_recovery_codes),
        updated_at = NOW()
    WHERE id = p_user_id AND deleted_at IS NULL;
END;
$$;

REVOKE ALL ON FUNCTION fn_set_user_mfa(uuid, boolean, bytea, bigint, bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fn_set_user_mfa(uuid, boolean, bytea, bigint, bytea) TO kopdes_app;

-- 4. Perbarui trg_fn_validasi_transisi_pembayaran agar mengizinkan penyelesaian langsung via mutasi bank
CREATE OR REPLACE FUNCTION trg_fn_validasi_transisi_pembayaran()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
BEGIN
    IF OLD.status = 'disetujui' AND NEW.status <> 'disetujui' THEN
        RAISE EXCEPTION 'Pembayaran yang sudah disetujui bersifat final dan tidak boleh diubah; gunakan transaksi reversal.';
    END IF;

    IF OLD.status = 'disetujui' AND (
        OLD.nominal_dasar <> NEW.nominal_dasar OR
        OLD.kode_unik <> NEW.kode_unik OR
        OLD.nominal_dibayar_aktual IS DISTINCT FROM NEW.nominal_dibayar_aktual
    ) THEN
        RAISE EXCEPTION 'Nominal pembayaran yang sudah disetujui tidak boleh dimodifikasi.';
    END IF;

    IF OLD.status <> NEW.status THEN
        IF OLD.status = 'menunggu_pembayaran' AND NEW.status NOT IN ('menunggu_verifikasi', 'menunggu_persetujuan_kedua', 'disetujui', 'kedaluwarsa', 'dibatalkan') THEN
            RAISE EXCEPTION 'Transisi status pembayaran tidak sah dari % ke %', OLD.status, NEW.status;
        ELSIF OLD.status = 'menunggu_verifikasi' AND NEW.status NOT IN ('menunggu_persetujuan_kedua', 'disetujui', 'ditolak') THEN
            RAISE EXCEPTION 'Transisi status pembayaran tidak sah dari % ke %', OLD.status, NEW.status;
        ELSIF OLD.status = 'menunggu_persetujuan_kedua' AND NEW.status NOT IN ('disetujui', 'ditolak') THEN
            RAISE EXCEPTION 'Transisi status pembayaran tidak sah dari % ke %', OLD.status, NEW.status;
        ELSIF OLD.status IN ('ditolak', 'kedaluwarsa', 'dibatalkan') THEN
            RAISE EXCEPTION 'Status terminal % tidak dapat diubah ke %', OLD.status, NEW.status;
        END IF;

        IF NEW.status IN ('disetujui', 'ditolak') THEN
            IF NEW.diverifikasi_oleh IS NULL THEN
                RAISE EXCEPTION 'Kolom diverifikasi_oleh wajib diisi saat menyetujui/menolak pembayaran.';
            END IF;
            IF NEW.diverifikasi_oleh = NEW.created_by OR NEW.diverifikasi_oleh = NEW.pembayar_user_id THEN
                RAISE EXCEPTION 'Pelanggaran Maker-Checker: verifikator tidak boleh menyetujui pembayaran miliknya sendiri.';
            END IF;
            NEW.waktu_verifikasi := COALESCE(NEW.waktu_verifikasi, NOW());
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

-- 5. Indeks Pencarian Baris Terakhir Hash Chain untuk 6 Tabel Append-Only
CREATE INDEX IF NOT EXISTS idx_audit_log_last_row ON audit_log (koperasi_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_pendaftaran_status_log_last_row ON pendaftaran_status_log (koperasi_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_pembayaran_verifikasi_log_last_row ON pembayaran_verifikasi_log (koperasi_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_kas_mutasi_last_row ON kas_mutasi (koperasi_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_stok_mutasi_last_row ON stok_mutasi (koperasi_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_anggota_status_log_last_row ON anggota_status_log (koperasi_id, id DESC);
