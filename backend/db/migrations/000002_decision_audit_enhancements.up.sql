-- =============================================================================
-- 000002_decision_audit_enhancements.up.sql
-- Implementasi hasil Audit Keputusan (D1, D2, D4, D8, D10)
-- Kompatibel PostgreSQL 14 & 16
-- =============================================================================

-- D1: Reservasi stok tertahan berbatas waktu pada produk dan pesanan
ALTER TABLE produk
    ADD COLUMN stok_tertahan INT NOT NULL DEFAULT 0,
    ADD CONSTRAINT chk_produk_stok_tertahan CHECK (stok_tertahan >= 0 AND stok_tertahan <= stok);

ALTER TABLE pesanan
    ADD COLUMN stok_status TEXT NOT NULL DEFAULT 'none'
        CHECK (stok_status IN ('none', 'reserved', 'deducted', 'released')),
    ADD COLUMN stok_hold_expires_at TIMESTAMPTZ;

CREATE INDEX idx_pesanan_stok_hold_aktif
    ON pesanan (stok_hold_expires_at)
    WHERE stok_status = 'reserved' AND stok_hold_expires_at IS NOT NULL;

-- D2 & D4: Status dikembalikan + metadata amplop terenkripsi bukti bayar
ALTER TABLE pembayaran DROP CONSTRAINT IF EXISTS pembayaran_status_check;
ALTER TABLE pembayaran
    ADD CONSTRAINT pembayaran_status_check
    CHECK (status IN (
        'menunggu_pembayaran', 'menunggu_verifikasi', 'lunas',
        'perlu_tindak_lanjut', 'ditolak', 'kedaluwarsa', 'batal', 'dikembalikan'
    ));

ALTER TABLE pembayaran
    ADD COLUMN bukti_key_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN bukti_dek_wrapped BYTEA;

-- Tabel #20: Pencatatan pengembalian dana manual dengan aturan dual control
CREATE TABLE pengembalian_dana (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    pembayaran_id     UUID NOT NULL UNIQUE REFERENCES pembayaran(id) ON DELETE RESTRICT,
    nominal           BIGINT NOT NULL CHECK (nominal > 0),
    metode            TEXT NOT NULL CHECK (metode IN ('tunai', 'transfer')),
    alasan            TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'diajukan' CHECK (status IN ('diajukan', 'selesai', 'ditolak')),
    bukti_url         TEXT NOT NULL DEFAULT '',
    bukti_hash        TEXT UNIQUE,
    bukti_key_id      TEXT NOT NULL DEFAULT '',
    bukti_dek_wrapped BYTEA,
    catatan_pelaksana TEXT NOT NULL DEFAULT '',
    diajukan_oleh     UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    dilaksanakan_oleh UUID REFERENCES users(id) ON DELETE RESTRICT,
    breakglass        BOOLEAN NOT NULL DEFAULT FALSE,
    selesai_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER trg_pengembalian_dana_updated
    BEFORE UPDATE ON pengembalian_dana
    FOR EACH ROW EXECUTE FUNCTION trg_fn_set_updated_at();

-- Tabel #21: Telemetry status eksekusi tugas worker terjadwal (D8)
CREATE TABLE worker_runs (
    job_name         TEXT PRIMARY KEY,
    last_started_at  TIMESTAMPTZ,
    last_finished_at TIMESTAMPTZ,
    last_status      TEXT NOT NULL DEFAULT 'idle' CHECK (last_status IN ('idle', 'running', 'ok', 'error')),
    last_summary     TEXT NOT NULL DEFAULT '',
    last_error       TEXT NOT NULL DEFAULT '',
    run_count        BIGINT NOT NULL DEFAULT 0 CHECK (run_count >= 0),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER trg_worker_runs_updated
    BEFORE UPDATE ON worker_runs
    FOR EACH ROW EXECUTE FUNCTION trg_fn_set_updated_at();

-- D10: Minimisasi data pribadi — tanggal_lahir opsional pada pendaftaran & anggota
ALTER TABLE pendaftaran ALTER COLUMN tanggal_lahir_enc DROP NOT NULL;
ALTER TABLE anggota ALTER COLUMN tanggal_lahir_enc DROP NOT NULL;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kopdes_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON pengembalian_dana, worker_runs TO kopdes_app;
    END IF;
END;
$$;
