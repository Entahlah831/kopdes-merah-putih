DROP TRIGGER IF EXISTS trg_prevent_truncate_pembayaran_verif ON pembayaran_verifikasi_log;
DROP TRIGGER IF EXISTS trg_prevent_truncate_kas_mutasi ON kas_mutasi;
DROP TRIGGER IF EXISTS trg_prevent_truncate_audit_log ON audit_log;
DROP FUNCTION IF EXISTS fn_prevent_truncate();

ALTER TABLE audit_log
DROP COLUMN IF EXISTS prev_hash,
DROP COLUMN IF EXISTS row_hash,
DROP COLUMN IF EXISTS ip_address,
DROP COLUMN IF EXISTS user_agent,
DROP COLUMN IF EXISTS request_id;

ALTER TABLE pembayaran_verifikasi_log
DROP COLUMN IF EXISTS prev_hash,
DROP COLUMN IF EXISTS row_hash,
DROP COLUMN IF EXISTS ip_address,
DROP COLUMN IF EXISTS user_agent,
DROP COLUMN IF EXISTS request_id;

ALTER TABLE kas_mutasi
DROP COLUMN IF EXISTS prev_hash,
DROP COLUMN IF EXISTS row_hash,
DROP COLUMN IF EXISTS ip_address,
DROP COLUMN IF EXISTS user_agent,
DROP COLUMN IF EXISTS request_id;
