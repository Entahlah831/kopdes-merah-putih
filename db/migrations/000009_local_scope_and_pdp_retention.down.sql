-- Rollback Migrasi 000009
DROP FUNCTION IF EXISTS fn_purge_rejected_pendaftaran_pdp(uuid, int);

CREATE TABLE IF NOT EXISTS sinkronisasi_simkopdes (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    kode_wilayah_bps varchar(20) NOT NULL,
    kode_koperasi_kemenkop varchar(50) NOT NULL,
    periode_laporan char(7) NOT NULL CHECK (periode_laporan ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    jenis_payload varchar(40) NOT NULL CHECK (jenis_payload IN ('profil_legalitas', 'rekap_anggota', 'rekap_iuran', 'rekap_unit_usaha_katalog')),
    payload_terkirim jsonb,
    status_pengiriman varchar(20) NOT NULL DEFAULT 'draft' CHECK (status_pengiriman IN ('draft', 'antrean', 'terkirim', 'gagal', 'diterima')),
    kode_referensi_kemenkop varchar(100),
    pesan_respon text,
    dikirim_pada timestamptz,
    waktu_respon timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid NOT NULL REFERENCES users(id)
);
