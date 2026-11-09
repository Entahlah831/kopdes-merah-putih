-- ==============================================================================
-- 01_roles.sql: Inisialisasi Role Database dengan Pemisahan Wewenang (K1)
-- ==============================================================================
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kopdes_migrator') THEN
        CREATE ROLE kopdes_migrator NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kopdes_app') THEN
        CREATE ROLE kopdes_app WITH LOGIN PASSWORD 'kopdes_dev_password' NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
    ELSE
        ALTER ROLE kopdes_app WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kopdes_readonly_auditor') THEN
        CREATE ROLE kopdes_readonly_auditor NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
    END IF;
END;
$$;
