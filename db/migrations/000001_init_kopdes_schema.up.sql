-- =============================================================================
-- 000001_init_kopdes_schema.up.sql
-- Skema Ramping Kopdes Merah Putih: Single-Tenant (19 Tabel Inti)
-- Kompatibel dengan PostgreSQL 14 & 16
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Pembangkit PK UUID v7 (RFC 9562 time-ordered)
CREATE OR REPLACE FUNCTION uuid_generate_v7()
RETURNS uuid
LANGUAGE plpgsql
VOLATILE
AS $$
DECLARE
    v_time timestamp with time zone := clock_timestamp();
    v_secs bigint := EXTRACT(EPOCH FROM v_time);
    v_msec bigint := MOD(EXTRACT(MILLISECONDS FROM v_time)::bigint, 1000);
    v_timestamp bigint := (v_secs * 1000) + v_msec;
    v_hex text := lpad(to_hex(v_timestamp), 12, '0');
    v_rand bytea := gen_random_bytes(10);
    v_uuid_bytes bytea;
BEGIN
    v_uuid_bytes := decode(v_hex, 'hex') || v_rand;
    v_uuid_bytes := set_byte(v_uuid_bytes, 6, (get_byte(v_uuid_bytes, 6) & 15) | 112);
    v_uuid_bytes := set_byte(v_uuid_bytes, 8, (get_byte(v_uuid_bytes, 8) & 63) | 128);
    RETURN encode(v_uuid_bytes, 'hex')::uuid;
END;
$$;

CREATE OR REPLACE FUNCTION trg_fn_set_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at := NOW();
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION trg_fn_reject_audit_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'audit_log bersifat append-only: operasi % ditolak', TG_OP;
END;
$$;

-- 1. Pengaturan koperasi (satu baris tunggal)
CREATE TABLE pengaturan (
    id                 INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    nama_koperasi      TEXT NOT NULL DEFAULT 'Kopdes Merah Putih',
    alamat_koperasi    TEXT NOT NULL DEFAULT '',
    telepon_koperasi   TEXT NOT NULL DEFAULT '',
    qris_unique_digits INT NOT NULL DEFAULT 3 CHECK (qris_unique_digits IN (3, 4)),
    ongkir_tetap       BIGINT NOT NULL DEFAULT 0 CHECK (ongkir_tetap >= 0),
    kebijakan_teks         TEXT NOT NULL DEFAULT '',
    catatan_akuntansi      TEXT NOT NULL DEFAULT 'KEPUTUSAN AKUNTAN',
    active_rekening_qris_id UUID,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO pengaturan (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

-- 2. Pengguna (admin, bendahara, petugas_toko, anggota)
CREATE TABLE users (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    email_enc           BYTEA,
    email_hash          TEXT UNIQUE,
    hp_enc              BYTEA,
    hp_hash             TEXT UNIQUE,
    nama_lengkap        TEXT NOT NULL,
    password_hash       TEXT NOT NULL DEFAULT '',
    peran               TEXT NOT NULL CHECK (peran IN ('admin', 'bendahara', 'petugas_toko', 'anggota')),
    status_aktif        BOOLEAN NOT NULL DEFAULT TRUE,
    mfa_enabled         BOOLEAN NOT NULL DEFAULT FALSE,
    mfa_secret_enc      BYTEA,
    mfa_last_step       BIGINT NOT NULL DEFAULT 0,
    mfa_recovery_hashes TEXT[] NOT NULL DEFAULT '{}',
    failed_login_count  INT NOT NULL DEFAULT 0,
    locked_until        TIMESTAMPTZ,
    last_failed_at      TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_users_identifier CHECK (email_hash IS NOT NULL OR hp_hash IS NOT NULL)
);

-- 3. Sesi opaque server-side
CREATE TABLE sesi (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      TEXT NOT NULL UNIQUE,
    mfa_verified    BOOLEAN NOT NULL DEFAULT FALSE,
    scope           TEXT NOT NULL DEFAULT 'full' CHECK (scope IN ('full', 'mfa_pending', 'mfa_setup')),
    mfa_attempts    INT NOT NULL DEFAULT 0,
    ip_address      TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    idle_expires_at TIMESTAMPTZ NOT NULL,
    abs_expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_sesi_user_aktif ON sesi (user_id) WHERE revoked_at IS NULL;

-- 4. Kode aktivasi & reset password sekali pakai
CREATE TABLE kode_aktivasi (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kode_hash  TEXT NOT NULL UNIQUE,
    jenis      TEXT NOT NULL CHECK (jenis IN ('aktivasi_awal', 'reset_password')),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 5. Pembatasan laju persisten di database
CREATE TABLE rate_limits (
    key             TEXT PRIMARY KEY,
    kategori        TEXT NOT NULL,
    hit_count       INT NOT NULL DEFAULT 0,
    window_reset_at TIMESTAMPTZ NOT NULL,
    blocked_until   TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 6. Anggota koperasi (NIK blind index berversi kunci; PII terenkripsi)
CREATE TABLE anggota (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    user_id           UUID UNIQUE REFERENCES users(id) ON DELETE SET NULL,
    nomor_anggota     TEXT NOT NULL UNIQUE,
    nama_lengkap      TEXT NOT NULL,
    nik_enc           BYTEA NOT NULL,
    nik_hash          TEXT NOT NULL UNIQUE,
    nik_key_version   TEXT NOT NULL DEFAULT 'v1',
    nik_masked        TEXT NOT NULL,
    hp_enc            BYTEA NOT NULL,
    hp_masked         TEXT NOT NULL,
    tanggal_lahir_enc BYTEA NOT NULL,
    alamat_enc        BYTEA NOT NULL,
    status            TEXT NOT NULL DEFAULT 'aktif' CHECK (status IN ('aktif', 'nonaktif', 'keluar')),
    alasan_status     TEXT NOT NULL DEFAULT '',
    tanggal_bergabung DATE NOT NULL DEFAULT CURRENT_DATE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 7. Pendaftaran publik calon anggota
CREATE TABLE pendaftaran (
    id                 UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    nama_lengkap       TEXT NOT NULL,
    email_enc          BYTEA,
    email_hash         TEXT,
    nik_enc            BYTEA NOT NULL,
    nik_hash           TEXT NOT NULL,
    nik_key_version    TEXT NOT NULL DEFAULT 'v1',
    nik_masked         TEXT NOT NULL,
    hp_enc             BYTEA NOT NULL,
    hp_hash            TEXT NOT NULL,
    hp_masked          TEXT NOT NULL,
    tanggal_lahir_enc  BYTEA NOT NULL,
    alamat_enc         BYTEA NOT NULL,
    pekerjaan          TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'diajukan' CHECK (status IN ('diajukan', 'perlu_perbaikan', 'disetujui', 'ditolak')),
    catatan_verifikasi TEXT NOT NULL DEFAULT '',
    verified_by        UUID REFERENCES users(id) ON DELETE SET NULL,
    verified_at        TIMESTAMPTZ,
    rejected_at        TIMESTAMPTZ,
    anggota_id         UUID REFERENCES anggota(id) ON DELETE SET NULL,
    upload_token_hash  TEXT NOT NULL,
    ip_address         TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_pendaftaran_status_created ON pendaftaran (status, created_at DESC);

-- 8. Berkas dokumen KYC terenkripsi
CREATE TABLE dokumen_kyc (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    pendaftaran_id UUID NOT NULL REFERENCES pendaftaran(id) ON DELETE CASCADE,
    tipe_dokumen   TEXT NOT NULL CHECK (tipe_dokumen IN ('ktp', 'kk', 'selfie_ktp')),
    storage_key    TEXT NOT NULL UNIQUE,
    key_id         TEXT NOT NULL,
    dek_wrapped    BYTEA NOT NULL,
    mime_type      TEXT NOT NULL,
    ukuran_bytes   BIGINT NOT NULL CHECK (ukuran_bytes > 0),
    lebar_px       INT NOT NULL CHECK (lebar_px > 0),
    tinggi_px      INT NOT NULL CHECK (tinggi_px > 0),
    sha256_plain   TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (pendaftaran_id, tipe_dokumen)
);

-- 9. Jenis iuran anggota
CREATE TABLE jenis_iuran (
    id                    UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    kode                  TEXT NOT NULL UNIQUE,
    nama                  TEXT NOT NULL,
    nominal               BIGINT NOT NULL CHECK (nominal > 0),
    hari_jatuh_tempo      INT NOT NULL DEFAULT 10 CHECK (hari_jatuh_tempo BETWEEN 1 AND 28),
    klasifikasi_akuntansi TEXT NOT NULL DEFAULT 'KEPUTUSAN AKUNTAN',
    aktif                 BOOLEAN NOT NULL DEFAULT TRUE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 10. Tagihan iuran bulanan per anggota
CREATE TABLE tagihan_iuran (
    id                 UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    anggota_id         UUID NOT NULL REFERENCES anggota(id) ON DELETE RESTRICT,
    jenis_iuran_id     UUID NOT NULL REFERENCES jenis_iuran(id) ON DELETE RESTRICT,
    periode_tahun      INT NOT NULL CHECK (periode_tahun BETWEEN 2020 AND 2100),
    periode_bulan      INT NOT NULL CHECK (periode_bulan BETWEEN 1 AND 12),
    nominal            BIGINT NOT NULL CHECK (nominal > 0),
    nominal_keringanan BIGINT NOT NULL DEFAULT 0 CHECK (nominal_keringanan >= 0 AND nominal_keringanan <= nominal),
    tanggal_jatuh_tempo DATE NOT NULL,
    status             TEXT NOT NULL DEFAULT 'belum_bayar' CHECK (status IN ('belum_bayar', 'menunggu_verifikasi', 'lunas', 'dibebaskan', 'batal')),
    alasan_keringanan  TEXT NOT NULL DEFAULT '',
    disetujui_oleh     UUID REFERENCES users(id) ON DELETE SET NULL,
    paid_at            TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (anggota_id, jenis_iuran_id, periode_tahun, periode_bulan)
);

-- 11. Kategori katalog toko
CREATE TABLE kategori (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    slug       TEXT NOT NULL UNIQUE,
    nama       TEXT NOT NULL,
    aktif      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 12. Produk katalog (harga umum & harga anggota, stok >= 0)
CREATE TABLE produk (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    kategori_id   UUID REFERENCES kategori(id) ON DELETE SET NULL,
    sku           TEXT NOT NULL UNIQUE,
    nama          TEXT NOT NULL,
    deskripsi     TEXT NOT NULL DEFAULT '',
    satuan        TEXT NOT NULL DEFAULT 'pcs',
    harga_umum    BIGINT CHECK (harga_umum IS NULL OR harga_umum > 0),
    harga_anggota BIGINT CHECK (harga_anggota IS NULL OR harga_anggota > 0),
    stok          INT NOT NULL DEFAULT 0 CHECK (stok >= 0),
    foto_url      TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'aktif' CHECK (status IN ('aktif', 'nonaktif')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 13. Promo sederhana
CREATE TABLE promo (
    id                 UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    kode               TEXT NOT NULL UNIQUE,
    nama               TEXT NOT NULL,
    tipe_diskon        TEXT NOT NULL CHECK (tipe_diskon IN ('persen', 'nominal')),
    nilai_diskon       BIGINT NOT NULL CHECK (nilai_diskon > 0),
    min_belanja        BIGINT NOT NULL DEFAULT 0 CHECK (min_belanja >= 0),
    kuota_total        INT NOT NULL DEFAULT 0 CHECK (kuota_total >= 0),
    kuota_terpakai     INT NOT NULL DEFAULT 0 CHECK (kuota_terpakai >= 0 AND kuota_terpakai <= kuota_total),
    sekali_per_anggota BOOLEAN NOT NULL DEFAULT FALSE,
    mulai_pada         TIMESTAMPTZ NOT NULL,
    berakhir_pada      TIMESTAMPTZ NOT NULL,
    aktif              BOOLEAN NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 14. Pesanan toko (tanpa keranjang server)
CREATE TABLE pesanan (
    id                     UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    nomor_pesanan          TEXT NOT NULL UNIQUE,
    user_id                UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    anggota_id             UUID REFERENCES anggota(id) ON DELETE SET NULL,
    promo_id               UUID REFERENCES promo(id) ON DELETE SET NULL,
    idempotency_key        TEXT UNIQUE,
    metode_pengiriman      TEXT NOT NULL CHECK (metode_pengiriman IN ('diantar', 'ambil_sendiri')),
    metode_pembayaran      TEXT NOT NULL CHECK (metode_pembayaran IN ('qris', 'tunai')),
    status                 TEXT NOT NULL DEFAULT 'baru' CHECK (status IN ('baru', 'menunggu_pembayaran', 'menunggu_verifikasi', 'dibayar', 'diproses', 'diantar', 'siap_diambil', 'selesai', 'batal')),
    subtotal               BIGINT NOT NULL CHECK (subtotal > 0),
    diskon                 BIGINT NOT NULL DEFAULT 0 CHECK (diskon >= 0),
    ongkir                 BIGINT NOT NULL DEFAULT 0 CHECK (ongkir >= 0),
    total_bayar            BIGINT NOT NULL CHECK (total_bayar >= 0),
    nama_penerima          TEXT NOT NULL,
    hp_penerima_enc        BYTEA NOT NULL,
    hp_penerima_masked     TEXT NOT NULL,
    alamat_pengantaran_enc BYTEA NOT NULL,
    catatan                TEXT NOT NULL DEFAULT '',
    tanggal_antar          DATE,
    expires_at             TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_pesanan_daftar_antar ON pesanan (tanggal_antar, status) WHERE metode_pengiriman = 'diantar';

-- 15. Item pesanan
CREATE TABLE pesanan_item (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    pesanan_id   UUID NOT NULL REFERENCES pesanan(id) ON DELETE CASCADE,
    produk_id    UUID NOT NULL REFERENCES produk(id) ON DELETE RESTRICT,
    nama_produk  TEXT NOT NULL,
    kuantitas    INT NOT NULL CHECK (kuantitas > 0),
    harga_satuan BIGINT NOT NULL CHECK (harga_satuan > 0),
    subtotal     BIGINT NOT NULL CHECK (subtotal > 0)
);

-- 16. Rekening & QRIS statis koperasi
CREATE TABLE rekening_qris (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    nama_merchant  TEXT NOT NULL,
    nmid           TEXT NOT NULL DEFAULT '',
    nama_bank      TEXT NOT NULL DEFAULT '',
    nomor_rekening TEXT NOT NULL DEFAULT '',
    nama_pemilik   TEXT NOT NULL,
    jenis_pemilik  TEXT NOT NULL CHECK (jenis_pemilik IN ('badan_hukum', 'perorangan')),
    qris_image_url TEXT NOT NULL DEFAULT '',
    qris_payload   TEXT NOT NULL DEFAULT '',
    aktif          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE pengaturan
    ADD CONSTRAINT fk_pengaturan_active_rekening_qris
    FOREIGN KEY (active_rekening_qris_id) REFERENCES rekening_qris(id) ON DELETE SET NULL;

-- 17. Pembayaran (QRIS statis + kode unik integer, serta tunai + nomor kwitansi berurutan)
CREATE SEQUENCE seq_nomor_kwitansi START 1;

CREATE TABLE pembayaran (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    nomor_pembayaran  TEXT NOT NULL UNIQUE,
    nomor_kwitansi    TEXT UNIQUE,
    idempotency_key   TEXT UNIQUE,
    pembayar_user_id  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    anggota_id        UUID REFERENCES anggota(id) ON DELETE SET NULL,
    rekening_qris_id  UUID REFERENCES rekening_qris(id) ON DELETE RESTRICT,
    jenis_transaksi   TEXT NOT NULL CHECK (jenis_transaksi IN ('iuran', 'pesanan')),
    metode            TEXT NOT NULL CHECK (metode IN ('qris', 'tunai')),
    nominal_pokok     BIGINT NOT NULL CHECK (nominal_pokok > 0),
    kode_unik         INT NOT NULL DEFAULT 0 CHECK (kode_unik >= 0),
    nominal_total     BIGINT NOT NULL CHECK (nominal_total > 0),
    nominal_dibayar   BIGINT NOT NULL DEFAULT 0 CHECK (nominal_dibayar >= 0),
    selisih_kode_unik BIGINT NOT NULL DEFAULT 0,
    catatan_akuntansi TEXT NOT NULL DEFAULT 'KEPUTUSAN AKUNTAN',
    status            TEXT NOT NULL DEFAULT 'menunggu_pembayaran' CHECK (status IN ('menunggu_pembayaran', 'menunggu_verifikasi', 'lunas', 'perlu_tindak_lanjut', 'ditolak', 'kedaluwarsa', 'batal')),
    bukti_url         TEXT NOT NULL DEFAULT '',
    bukti_hash        TEXT UNIQUE,
    catatan_pembayar  TEXT NOT NULL DEFAULT '',
    alasan_verifikasi TEXT NOT NULL DEFAULT '',
    dibuat_oleh       UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    diverifikasi_oleh UUID REFERENCES users(id) ON DELETE RESTRICT,
    verified_at       TIMESTAMPTZ,
    expires_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_pembayaran_qris_unik_aktif
    ON pembayaran (rekening_qris_id, nominal_total)
    WHERE metode = 'qris' AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi');

-- 18. Alokasi pembayaran ke tagihan iuran / pesanan
CREATE TABLE pembayaran_alokasi (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    pembayaran_id    UUID NOT NULL REFERENCES pembayaran(id) ON DELETE CASCADE,
    tagihan_iuran_id UUID REFERENCES tagihan_iuran(id) ON DELETE RESTRICT,
    pesanan_id       UUID REFERENCES pesanan(id) ON DELETE RESTRICT,
    nominal_alokasi  BIGINT NOT NULL CHECK (nominal_alokasi > 0),
    CONSTRAINT chk_alokasi_target CHECK (
        (tagihan_iuran_id IS NOT NULL AND pesanan_id IS NULL) OR
        (tagihan_iuran_id IS NULL AND pesanan_id IS NOT NULL)
    )
);

-- 19. Audit log append-only (UPDATE & DELETE ditolak trigger)
CREATE TABLE audit_log (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_peran   TEXT NOT NULL DEFAULT 'system',
    aksi          TEXT NOT NULL,
    entitas       TEXT NOT NULL,
    entitas_id    TEXT NOT NULL DEFAULT '',
    ringkasan     TEXT NOT NULL DEFAULT '',
    ip_address    TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER trg_audit_log_immutable
    BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION trg_fn_reject_audit_mutation();

-- Trigger updated_at
CREATE TRIGGER trg_pengaturan_updated BEFORE UPDATE ON pengaturan FOR EACH ROW EXECUTE FUNCTION trg_fn_set_updated_at();
CREATE TRIGGER trg_users_updated BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION trg_fn_set_updated_at();
CREATE TRIGGER trg_anggota_updated BEFORE UPDATE ON anggota FOR EACH ROW EXECUTE FUNCTION trg_fn_set_updated_at();
CREATE TRIGGER trg_pendaftaran_updated BEFORE UPDATE ON pendaftaran FOR EACH ROW EXECUTE FUNCTION trg_fn_set_updated_at();
CREATE TRIGGER trg_tagihan_updated BEFORE UPDATE ON tagihan_iuran FOR EACH ROW EXECUTE FUNCTION trg_fn_set_updated_at();
CREATE TRIGGER trg_produk_updated BEFORE UPDATE ON produk FOR EACH ROW EXECUTE FUNCTION trg_fn_set_updated_at();
CREATE TRIGGER trg_pesanan_updated BEFORE UPDATE ON pesanan FOR EACH ROW EXECUTE FUNCTION trg_fn_set_updated_at();
CREATE TRIGGER trg_rekening_updated BEFORE UPDATE ON rekening_qris FOR EACH ROW EXECUTE FUNCTION trg_fn_set_updated_at();
CREATE TRIGGER trg_pembayaran_updated BEFORE UPDATE ON pembayaran FOR EACH ROW EXECUTE FUNCTION trg_fn_set_updated_at();

-- Izin least-privilege untuk role runtime aplikasi non-superuser (kopdes_app)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kopdes_app') THEN
        GRANT USAGE ON SCHEMA public TO kopdes_app;
        GRANT SELECT, INSERT, UPDATE, DELETE ON
            pengaturan, users, sesi, kode_aktivasi, rate_limits,
            anggota, pendaftaran, dokumen_kyc, jenis_iuran, tagihan_iuran,
            kategori, produk, promo, pesanan, pesanan_item,
            rekening_qris, pembayaran, pembayaran_alokasi
        TO kopdes_app;
        GRANT SELECT, INSERT ON audit_log TO kopdes_app;
        REVOKE UPDATE, DELETE, TRUNCATE ON audit_log FROM PUBLIC, kopdes_app;
        GRANT USAGE, SELECT ON SEQUENCE seq_nomor_kwitansi TO kopdes_app;
    END IF;
END;
$$;
