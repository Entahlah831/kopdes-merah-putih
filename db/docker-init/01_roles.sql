-- ==============================================================================
-- 01_roles.sql: Inisialisasi Role Aplikasi Non-Superuser (kopdes_app)
-- Password diambil dari DB_PASSWORD (single source of truth).
-- ==============================================================================

\getenv APP_ENV APP_ENV
\getenv DB_PASSWORD DB_PASSWORD

\if :{?APP_ENV}
    SELECT set_config('app.env', :'APP_ENV', false);
\else
    SELECT set_config('app.env', 'development', false);
\endif

\if :{?DB_PASSWORD}
    SELECT set_config('app.app_pass', :'DB_PASSWORD', false);
\else
    SELECT set_config('app.app_pass', '', false);
\endif

DO $$
DECLARE
    v_env text := current_setting('app.env', true);
    v_pass text := current_setting('app.app_pass', true);
BEGIN
    IF v_pass IS NULL OR v_pass = '' THEN
        RAISE EXCEPTION 'FATAL: Variabel lingkungan DB_PASSWORD wajib disetel (APP_ENV=%).', COALESCE(v_env, '');
    END IF;

    IF v_env = 'production' AND v_pass = 'kopdes_dev_password' THEN
        RAISE EXCEPTION 'FATAL: Pada profil produksi, dilarang menggunakan password default lemah!';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kopdes_app') THEN
        EXECUTE format('CREATE ROLE kopdes_app WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;', v_pass);
    ELSE
        EXECUTE format('ALTER ROLE kopdes_app WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;', v_pass);
    END IF;
END;
$$;

SELECT set_config('app.app_pass', '', false);
