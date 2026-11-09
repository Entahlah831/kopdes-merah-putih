-- =============================================================================
-- Migrasi 000004: Cakupan Penuh RLS dan Fungsi Login Terproteksi
-- Mengaktifkan RLS pada audit_log, job_log, konfigurasi_aturan, users, dan koperasi.
-- Menghilangkan kebutuhan bypass app.is_platform_admin pada alur login pra-autentikasi.
-- =============================================================================

-- 1. RLS pada audit_log (Tabel Induk Berpartisi)
ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS pol_isolasi_tenant_audit_log ON audit_log;
CREATE POLICY pol_isolasi_tenant_audit_log ON audit_log
    USING (app_is_platform_admin() OR koperasi_id = app_current_koperasi_id())
    WITH CHECK (app_is_platform_admin() OR koperasi_id = app_current_koperasi_id());

-- 2. RLS pada job_log
ALTER TABLE job_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_log FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS pol_isolasi_tenant_job_log ON job_log;
CREATE POLICY pol_isolasi_tenant_job_log ON job_log
    USING (app_is_platform_admin() OR koperasi_id = app_current_koperasi_id())
    WITH CHECK (app_is_platform_admin() OR koperasi_id = app_current_koperasi_id());

-- 3. RLS pada konfigurasi_aturan (Mendukung parameter global bila koperasi_id IS NULL)
ALTER TABLE konfigurasi_aturan ENABLE ROW LEVEL SECURITY;
ALTER TABLE konfigurasi_aturan FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS pol_isolasi_tenant_konfigurasi_aturan ON konfigurasi_aturan;
CREATE POLICY pol_isolasi_tenant_konfigurasi_aturan ON konfigurasi_aturan
    USING (app_is_platform_admin() OR koperasi_id IS NULL OR koperasi_id = app_current_koperasi_id())
    WITH CHECK (app_is_platform_admin() OR (koperasi_id IS NOT NULL AND koperasi_id = app_current_koperasi_id()));

-- 4. RLS pada koperasi
ALTER TABLE koperasi ENABLE ROW LEVEL SECURITY;
ALTER TABLE koperasi FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS pol_isolasi_koperasi ON koperasi;
DROP POLICY IF EXISTS pol_isolasi_koperasi_select ON koperasi;
CREATE POLICY pol_isolasi_koperasi_select ON koperasi FOR SELECT
    USING (app_is_platform_admin() OR app_current_koperasi_id() IS NULL OR id = app_current_koperasi_id());

DROP POLICY IF EXISTS pol_isolasi_koperasi_insert ON koperasi;
CREATE POLICY pol_isolasi_koperasi_insert ON koperasi FOR INSERT
    WITH CHECK (true);

DROP POLICY IF EXISTS pol_isolasi_koperasi_update ON koperasi;
CREATE POLICY pol_isolasi_koperasi_update ON koperasi FOR UPDATE
    USING (app_is_platform_admin() OR app_current_koperasi_id() IS NULL OR id = app_current_koperasi_id());

-- 5. RLS pada users
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS pol_isolasi_users_select ON users;
DROP POLICY IF EXISTS pol_isolasi_users ON users;
CREATE POLICY pol_isolasi_users_select ON users FOR SELECT
    USING (
        app_is_platform_admin()
        OR app_current_koperasi_id() IS NULL
        OR EXISTS (
            SELECT 1 FROM user_roles ur
            WHERE ur.user_id = users.id
              AND ur.koperasi_id = app_current_koperasi_id()
              AND ur.deleted_at IS NULL
        )
    );

DROP POLICY IF EXISTS pol_isolasi_users_insert ON users;
CREATE POLICY pol_isolasi_users_insert ON users FOR INSERT
    WITH CHECK (true);

DROP POLICY IF EXISTS pol_isolasi_users_update ON users;
CREATE POLICY pol_isolasi_users_update ON users FOR UPDATE
    USING (app_is_platform_admin() OR app_current_koperasi_id() IS NULL);

-- 6. Fungsi SECURITY DEFINER sempit untuk login pra-autentikasi (mencegah bypass RLS platform admin)
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

REVOKE EXECUTE ON FUNCTION fn_lookup_user_login(char(64)) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fn_lookup_user_login(char(64)) TO kopdes_app;
