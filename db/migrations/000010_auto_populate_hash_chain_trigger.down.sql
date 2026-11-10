-- Rollback Migrasi 000010
DROP TRIGGER IF EXISTS trg_hash_chain_audit_log ON audit_log;
DROP TRIGGER IF EXISTS trg_hash_chain_kas_mutasi ON kas_mutasi;
DROP TRIGGER IF EXISTS trg_hash_chain_pembayaran_verif ON pembayaran_verifikasi_log;
DROP FUNCTION IF EXISTS fn_maintain_hash_chain();
