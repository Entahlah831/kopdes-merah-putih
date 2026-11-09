-- =============================================================================
-- Migrasi 000007: Append-Only Hash Chaining & Perlindungan TRUNCATE
-- =============================================================================

-- 1. Tambah kolom hash chain dan jejak metadata pada kas_mutasi
ALTER TABLE kas_mutasi
ADD COLUMN IF NOT EXISTS prev_hash char(64),
ADD COLUMN IF NOT EXISTS row_hash char(64),
ADD COLUMN IF NOT EXISTS ip_address varchar(45),
ADD COLUMN IF NOT EXISTS user_agent text,
ADD COLUMN IF NOT EXISTS request_id varchar(100);

-- 2. Tambah kolom hash chain dan jejak metadata pada pembayaran_verifikasi_log
ALTER TABLE pembayaran_verifikasi_log
ADD COLUMN IF NOT EXISTS prev_hash char(64),
ADD COLUMN IF NOT EXISTS row_hash char(64),
ADD COLUMN IF NOT EXISTS ip_address varchar(45),
ADD COLUMN IF NOT EXISTS user_agent text,
ADD COLUMN IF NOT EXISTS request_id varchar(100);

-- 3. Tambah kolom hash chain dan jejak metadata pada audit_log
ALTER TABLE audit_log
ADD COLUMN IF NOT EXISTS prev_hash char(64),
ADD COLUMN IF NOT EXISTS row_hash char(64),
ADD COLUMN IF NOT EXISTS ip_address varchar(45),
ADD COLUMN IF NOT EXISTS user_agent text,
ADD COLUMN IF NOT EXISTS request_id varchar(100);

-- 4. Fungsi trigger penolakan TRUNCATE pada tabel append-only
CREATE OR REPLACE FUNCTION fn_prevent_truncate()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'TABEL APPEND-ONLY: Operasi TRUNCATE dilarang pada tabel %', TG_TABLE_NAME;
END;
$$;

DROP TRIGGER IF EXISTS trg_prevent_truncate_audit_log ON audit_log;
CREATE TRIGGER trg_prevent_truncate_audit_log
BEFORE TRUNCATE ON audit_log
FOR EACH STATEMENT EXECUTE FUNCTION fn_prevent_truncate();

DROP TRIGGER IF EXISTS trg_prevent_truncate_kas_mutasi ON kas_mutasi;
CREATE TRIGGER trg_prevent_truncate_kas_mutasi
BEFORE TRUNCATE ON kas_mutasi
FOR EACH STATEMENT EXECUTE FUNCTION fn_prevent_truncate();

DROP TRIGGER IF EXISTS trg_prevent_truncate_pembayaran_verif ON pembayaran_verifikasi_log;
CREATE TRIGGER trg_prevent_truncate_pembayaran_verif
BEFORE TRUNCATE ON pembayaran_verifikasi_log
FOR EACH STATEMENT EXECUTE FUNCTION fn_prevent_truncate();
