-- =============================================================================
-- Migrasi 000005: Refresh Token Rotation, Revokasi, dan Hierarki Peran Deterministik
-- =============================================================================

-- 1. Tabel refresh_tokens untuk rotasi token dan deteksi reuse
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash char(64) NOT NULL UNIQUE,
    koperasi_id uuid REFERENCES koperasi(id),
    role_name varchar(50) NOT NULL,
    is_platform_admin boolean NOT NULL DEFAULT false,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    replaced_by_token_hash char(64),
    created_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash ON refresh_tokens(token_hash);

GRANT SELECT, INSERT, UPDATE, DELETE ON refresh_tokens TO kopdes_app;

-- 2. Update fn_lookup_user_login dengan dukungan explicit p_koperasi_id dan urutan hierarki deterministik
CREATE OR REPLACE FUNCTION fn_lookup_user_login(p_email_hash char(64), p_koperasi_id uuid DEFAULT NULL)
RETURNS TABLE (
    user_id uuid,
    password_hash text,
    nama_lengkap varchar(150),
    is_super_admin boolean,
    status_aktif boolean,
    koperasi_id uuid,
    role_name varchar(50)
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
BEGIN
    RETURN QUERY
    SELECT u.id, u.password_hash::text, u.nama_lengkap, u.is_super_admin, u.status_aktif,
           ur.koperasi_id, COALESCE(r.nama, 'anggota')::varchar(50) as role_name
    FROM users u
    LEFT JOIN user_roles ur ON ur.user_id = u.id AND ur.deleted_at IS NULL AND (p_koperasi_id IS NULL OR ur.koperasi_id = p_koperasi_id)
    LEFT JOIN roles r ON r.id = ur.role_id
    WHERE u.email_hash = p_email_hash AND u.deleted_at IS NULL
    ORDER BY
        CASE r.kode
            WHEN 'admin' THEN 1
            WHEN 'ketua' THEN 2
            WHEN 'bendahara' THEN 3
            WHEN 'sekretaris' THEN 4
            WHEN 'pengurus' THEN 5
            WHEN 'pengawas' THEN 6
            WHEN 'operator' THEN 7
            WHEN 'anggota' THEN 8
            ELSE 99
        END ASC,
        ur.created_at ASC
    LIMIT 1;
END;
$$;

REVOKE EXECUTE ON FUNCTION fn_lookup_user_login(char(64), uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fn_lookup_user_login(char(64), uuid) TO kopdes_app;
