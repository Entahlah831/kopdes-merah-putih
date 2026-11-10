DROP FUNCTION IF EXISTS fn_get_user_mfa(uuid);
DROP TABLE IF EXISTS mfa_login_sessions CASCADE;
ALTER TABLE users DROP COLUMN IF EXISTS totp_secret_enkripsi;
ALTER TABLE users DROP COLUMN IF EXISTS mfa_enabled;
