-- =============================================================================
-- Migrasi 000010: Trigger Otomatis Hash Chaining Append-Only & Advisory Lock
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE OR REPLACE FUNCTION fn_maintain_hash_chain()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_base_table text;
    v_lock_key bigint;
    v_prev_hash char(64);
BEGIN
    -- 0. Tentukan tabel induk jika tabel target merupakan partisi bulanan
    IF TG_TABLE_NAME LIKE 'audit_log%' THEN
        v_base_table := 'audit_log';
    ELSIF TG_TABLE_NAME LIKE 'kas_mutasi%' THEN
        v_base_table := 'kas_mutasi';
    ELSE
        v_base_table := TG_TABLE_NAME;
    END IF;

    -- 1. Serialisasi konkuren per tenant dan tabel dasar menggunakan transactional advisory lock
    v_lock_key := ('x' || substr(md5(v_base_table || ':' || NEW.koperasi_id::text), 1, 16))::bit(64)::bigint;
    PERFORM pg_advisory_xact_lock(v_lock_key);

    -- 2. Pastikan ID terisi dan tetapkan created_at monolitik sesuai urutan lock jika mendekati waktu saat ini
    IF NEW.id IS NULL THEN
        NEW.id := uuid_generate_v7();
    END IF;
    IF NEW.created_at IS NULL OR abs(extract(epoch from (NEW.created_at - clock_timestamp()))) < 60 THEN
        NEW.created_at := clock_timestamp();
    END IF;

    -- 3. Cari row_hash terakhir untuk tenant ini pada tabel dasar yang sama
    EXECUTE format('
        SELECT row_hash 
        FROM %I 
        WHERE koperasi_id = $1 AND row_hash IS NOT NULL 
        ORDER BY created_at DESC, id DESC 
        LIMIT 1
    ', v_base_table) INTO v_prev_hash USING NEW.koperasi_id;

    IF v_prev_hash IS NULL OR v_prev_hash = '' THEN
        v_prev_hash := '0000000000000000000000000000000000000000000000000000000000000000';
    END IF;

    NEW.prev_hash := v_prev_hash;

    -- 4. Hitung row_hash menggunakan pgcrypto digest SHA-256
    NEW.row_hash := encode(digest(
        NEW.prev_hash || ':' || v_base_table || ':' || NEW.id::text || ':' || NEW.koperasi_id::text || ':' || to_char(NEW.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
        'sha256'
    ), 'hex');

    -- 5. Catat jejak audit context (IP, User-Agent, Request ID) bila tersedia di sesi
    IF v_base_table = 'audit_log' THEN
        IF NEW.ip_address IS NULL AND NULLIF(current_setting('app.client_ip', true), '') IS NOT NULL THEN
            BEGIN
                NEW.ip_address := current_setting('app.client_ip', true)::inet;
            EXCEPTION WHEN OTHERS THEN
                NULL;
            END;
        END IF;
    ELSE
        NEW.ip_address := COALESCE(NEW.ip_address, NULLIF(current_setting('app.client_ip', true), ''));
    END IF;
    NEW.user_agent := COALESCE(NEW.user_agent, NULLIF(current_setting('app.user_agent', true), ''));
    NEW.request_id := COALESCE(NEW.request_id, NULLIF(current_setting('app.request_id', true), ''));

    RETURN NEW;
END;
$$;

COMMENT ON FUNCTION fn_maintain_hash_chain IS 'Mengisi otomatis prev_hash dan row_hash dengan serialisasi advisory lock per tenant dan tabel.';

-- Pasang trigger pada audit_log
DROP TRIGGER IF EXISTS trg_hash_chain_audit_log ON audit_log;
CREATE TRIGGER trg_hash_chain_audit_log
BEFORE INSERT ON audit_log
FOR EACH ROW EXECUTE FUNCTION fn_maintain_hash_chain();

-- Pasang trigger pada kas_mutasi
DROP TRIGGER IF EXISTS trg_hash_chain_kas_mutasi ON kas_mutasi;
CREATE TRIGGER trg_hash_chain_kas_mutasi
BEFORE INSERT ON kas_mutasi
FOR EACH ROW EXECUTE FUNCTION fn_maintain_hash_chain();

-- Pasang trigger pada pembayaran_verifikasi_log
DROP TRIGGER IF EXISTS trg_hash_chain_pembayaran_verif ON pembayaran_verifikasi_log;
CREATE TRIGGER trg_hash_chain_pembayaran_verif
BEFORE INSERT ON pembayaran_verifikasi_log
FOR EACH ROW EXECUTE FUNCTION fn_maintain_hash_chain();
