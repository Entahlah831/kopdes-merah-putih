-- ==============================================================================
-- 01_roles.sql: Inisialisasi Role Database dengan Pemisahan Wewenang (K1 & Task 8)
-- Password role diambil dari variabel lingkungan tanpa nilai default di produksi.
-- ==============================================================================

\getenv APP_ENV APP_ENV
\getenv KOPDES_APP_PASSWORD KOPDES_APP_PASSWORD
\getenv DB_PASSWORD DB_PASSWORD

\if :{?APP_ENV}
    SELECT set_config('app.env', :'APP_ENV', false);
\else
    SELECT set_config('app.env', 'development', false);
\endif

\if :{?KOPDES_APP_PASSWORD}
    SELECT set_config('app.app_pass', :'KOPDES_APP_PASSWORD', false);
\elif :{?DB_PASSWORD}
    SELECT set_config('app.app_pass', :'DB_PASSWORD', false);
\else
    SELECT set_config('app.app_pass', '', false);
\endif

DO $$
DECLARE
    v_env text := current_setting('app.env', true);
    v_pass text := current_setting('app.app_pass', true);
BEGIN
    -- Validasi ketat profil produksi: WAJIB variabel lingkungan dan DILARANG default dev
    IF v_env = 'production' THEN
        IF v_pass IS NULL OR v_pass = '' THEN
            RAISE EXCEPTION 'FATAL: Pada profil produksi, variabel lingkungan KOPDES_APP_PASSWORD (atau DB_PASSWORD) wajib disetel!';
        END IF;

        IF v_pass = 'kopdes_dev_password' THEN
            RAISE EXCEPTION 'FATAL: Pada profil produksi, dilarang menggunakan password default dev ("kopdes_dev_password")!';
        END IF;
    ELSE
        -- Profil non-produksi / development: fallback ke dev password jika tidak diisi
        IF v_pass IS NULL OR v_pass = '' THEN
            v_pass := 'kopdes_dev_password';
        END IF;
    END IF;

    -- 1. Role kopdes_migrator
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kopdes_migrator') THEN
        CREATE ROLE kopdes_migrator NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
    END IF;

    -- 2. Role kopdes_app dengan password dari variabel lingkungan
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kopdes_app') THEN
        EXECUTE format('CREATE ROLE kopdes_app WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;', v_pass);
    ELSE
        EXECUTE format('ALTER ROLE kopdes_app WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;', v_pass);
    END IF;

    -- 3. Role kopdes_readonly_auditor
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kopdes_readonly_auditor') THEN
        CREATE ROLE kopdes_readonly_auditor NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
    END IF;
END;
$$;

-- Bersihkan session setting password
SELECT set_config('app.app_pass', '', false);
