DROP INDEX IF EXISTS idx_anggota_status_log_last_row;
DROP INDEX IF EXISTS idx_stok_mutasi_last_row;
DROP INDEX IF EXISTS idx_kas_mutasi_last_row;
DROP INDEX IF EXISTS idx_pembayaran_verifikasi_log_last_row;
DROP INDEX IF EXISTS idx_pendaftaran_status_log_last_row;
DROP INDEX IF EXISTS idx_audit_log_last_row;

DROP INDEX IF EXISTS idx_mutasi_bank_pembayaran_1to1;
DROP INDEX IF EXISTS idx_mutasi_bank_row_hash;
ALTER TABLE mutasi_bank_impor DROP COLUMN IF EXISTS row_hash;

ALTER TABLE mfa_login_sessions DROP COLUMN IF EXISTS is_locked;
ALTER TABLE mfa_login_sessions DROP COLUMN IF EXISTS attempts;

ALTER TABLE users DROP COLUMN IF EXISTS mfa_recovery_codes;
ALTER TABLE users DROP COLUMN IF EXISTS last_totp_step;
