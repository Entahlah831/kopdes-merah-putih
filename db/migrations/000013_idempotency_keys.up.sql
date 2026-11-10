-- ==============================================================================
-- MIGRASI 000013: Tabel Idempotensi Database & Proteksi Perbedaan Body (Task 4)
-- ==============================================================================

CREATE TABLE IF NOT EXISTS idempotency_keys (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    user_id uuid NOT NULL REFERENCES users(id),
    kunci varchar(120) NOT NULL,
    request_hash char(64) NOT NULL,
    response_code integer NOT NULL,
    response_body jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (koperasi_id, user_id, kunci)
);

COMMENT ON TABLE idempotency_keys IS 'Penyimpanan kunci idempotensi berbasis database: mencegah duplikasi mutasi dan menolak payload berbeda (HTTP 422).';

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_lookup 
ON idempotency_keys(koperasi_id, user_id, kunci);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_expires_at 
ON idempotency_keys(expires_at);

ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS pol_isolasi_tenant_idempotency_keys ON idempotency_keys;
CREATE POLICY pol_isolasi_tenant_idempotency_keys ON idempotency_keys
    USING (app_is_platform_admin() OR koperasi_id = app_current_koperasi_id())
    WITH CHECK (app_is_platform_admin() OR koperasi_id = app_current_koperasi_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON idempotency_keys TO kopdes_app;
GRANT SELECT ON idempotency_keys TO kopdes_readonly_auditor;
