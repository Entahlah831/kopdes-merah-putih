REVOKE DELETE ON ALL TABLES IN SCHEMA public FROM kopdes_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON refresh_tokens, idempotency_keys, mutasi_bank_impor, mfa_login_sessions TO kopdes_app;
