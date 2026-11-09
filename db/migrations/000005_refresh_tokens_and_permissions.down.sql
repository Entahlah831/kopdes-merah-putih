-- Rollback Migrasi 000005

DROP FUNCTION IF EXISTS fn_lookup_user_login(char(64), uuid);

-- Kembalikan fn_lookup_user_login 1-parameter dari 000004
CREATE OR REPLACE FUNCTION fn_lookup_user_login(p_email_hash char(64))
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
STRICT
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
BEGIN
    RETURN QUERY
    SELECT u.id, u.password_hash::text, u.nama_lengkap, u.is_super_admin, u.status_aktif,
           ur.koperasi_id, COALESCE(r.nama, 'anggota')::varchar(50) as role_name
    FROM users u
    LEFT JOIN user_roles ur ON ur.user_id = u.id AND ur.deleted_at IS NULL
    LEFT JOIN roles r ON r.id = ur.role_id
    WHERE u.email_hash = p_email_hash AND u.deleted_at IS NULL
    ORDER BY ur.created_at ASC
    LIMIT 1;
END;
$$;
GRANT EXECUTE ON FUNCTION fn_lookup_user_login(char(64)) TO kopdes_app;

DROP TABLE IF EXISTS refresh_tokens CASCADE;
