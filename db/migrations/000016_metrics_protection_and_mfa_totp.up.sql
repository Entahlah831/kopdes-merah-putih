-- ==============================================================================
-- MIGRASI 000016: Proteksi Metrik & MFA TOTP Akun Privileged (Task 7)
-- ==============================================================================

-- 1. Tambah kolom mfa_enabled dan totp_secret_enkripsi pada tabel users
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_enabled boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_secret_enkripsi bytea;

-- 2. Tabel mfa_login_sessions untuk menampung sesi sementara login dua-langkah (MFA step 1 -> step 2)
CREATE TABLE IF NOT EXISTS mfa_login_sessions (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    koperasi_id uuid REFERENCES koperasi(id) ON DELETE CASCADE,
    role_name varchar(50) NOT NULL,
    is_super_admin boolean NOT NULL DEFAULT false,
    session_token character(64) NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_mfa_login_session_token ON mfa_login_sessions(session_token);

GRANT SELECT, INSERT, UPDATE, DELETE ON mfa_login_sessions TO kopdes_app;

-- 3. Fungsi SECURITY DEFINER untuk mengecek status dan secret MFA pengguna secara aman
CREATE OR REPLACE FUNCTION fn_get_user_mfa(p_user_id uuid)
RETURNS TABLE (
    mfa_enabled boolean,
    totp_secret_enkripsi bytea
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
BEGIN
    RETURN QUERY
    SELECT u.mfa_enabled, u.totp_secret_enkripsi
    FROM users u
    WHERE u.id = p_user_id AND u.deleted_at IS NULL;
END;
$$;

REVOKE ALL ON FUNCTION fn_get_user_mfa(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fn_get_user_mfa(uuid) TO kopdes_app;

-- 4. Fungsi SECURITY DEFINER untuk memperbarui status dan secret MFA pengguna secara aman
CREATE OR REPLACE FUNCTION fn_set_user_mfa(
    p_user_id uuid,
    p_mfa_enabled boolean,
    p_totp_secret_enkripsi bytea
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
        updated_at = NOW()
    WHERE id = p_user_id AND deleted_at IS NULL;
END;
$$;

REVOKE ALL ON FUNCTION fn_set_user_mfa(uuid, boolean, bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fn_set_user_mfa(uuid, boolean, bytea) TO kopdes_app;

