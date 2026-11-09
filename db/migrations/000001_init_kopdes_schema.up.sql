-- =============================================================================
-- SCHEMA.SQL — DATABASE POSTGRESQL 16 APLIKASI KOPDES MERAH PUTIH
-- =============================================================================

-- 1. EKSTENSI & FUNGSI UTILITAS INTI
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Fungsi UUID v7 (time-ordered RFC 9562) murni PL/pgSQL untuk PostgreSQL 16
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
    v_uuid_bytes := set_byte(v_uuid_bytes, 6, (get_byte(v_uuid_bytes, 6) & 15) | 112); -- version 7 (0111)
    v_uuid_bytes := set_byte(v_uuid_bytes, 8, (get_byte(v_uuid_bytes, 8) & 63) | 128); -- variant RFC 9562 (10)
    RETURN encode(v_uuid_bytes, 'hex')::uuid;
END;
$$;
COMMENT ON FUNCTION uuid_generate_v7() IS 'Pembangkit PK UUID v7 berurutan waktu untuk lokalitas indeks B-tree PostgreSQL 16.';

-- Fungsi helper konteks RLS tenant dan peran platform
CREATE OR REPLACE FUNCTION app_current_koperasi_id()
RETURNS uuid
LANGUAGE sql
STABLE
AS $$
    SELECT NULLIF(current_setting('app.current_koperasi_id', true), '')::uuid;
$$;

CREATE OR REPLACE FUNCTION app_is_platform_admin()
RETURNS boolean
LANGUAGE sql
STABLE
AS $$
    SELECT COALESCE(current_setting('app.is_platform_admin', true) = 'true', false);
$$;

-- Trigger otomatis updated_at
CREATE OR REPLACE FUNCTION trg_fn_set_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at := NOW();
    RETURN NEW;
END;
$$;

-- Trigger proteksi tabel append-only (melarang UPDATE dan DELETE)
CREATE OR REPLACE FUNCTION trg_fn_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'Tabel % bersifat append-only; UPDATE dan DELETE dilarang. Gunakan entri reversal untuk koreksi.', TG_TABLE_NAME;
END;
$$;

-- Trigger proteksi soft-delete pada tabel keuangan/legal (melarang DELETE fisik)
CREATE OR REPLACE FUNCTION trg_fn_forbid_hard_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'Penghapusan fisik (DELETE) pada tabel legal/keuangan % dilarang (UU 8/1997). Gunakan soft delete (deleted_at).', TG_TABLE_NAME;
END;
$$;

-- 2. TIPE ENUM
CREATE TYPE tingkat_wilayah_enum AS ENUM ('provinsi', 'kabupaten_kota', 'kecamatan', 'desa_kelurahan');
CREATE TYPE status_pendaftaran_enum AS ENUM ('draft', 'diajukan', 'dalam_verifikasi', 'perlu_perbaikan', 'disetujui', 'ditolak', 'dibatalkan');
CREATE TYPE jenis_dokumen_anggota_enum AS ENUM ('ktp', 'kk', 'foto_diri', 'surat_domisili', 'form_pendaftaran', 'lainnya');
CREATE TYPE status_verifikasi_dokumen_enum AS ENUM ('menunggu', 'valid', 'tidak_valid');
CREATE TYPE status_anggota_enum AS ENUM ('calon', 'aktif', 'nonaktif', 'keluar', 'meninggal');
CREATE TYPE status_tagihan_iuran_enum AS ENUM ('belum_bayar', 'menunggu_verifikasi', 'lunas', 'menunggak', 'dibebaskan', 'dibatalkan');
CREATE TYPE status_produk_enum AS ENUM ('draft', 'aktif', 'nonaktif', 'arsip');
CREATE TYPE tipe_promo_enum AS ENUM ('persen', 'nominal');
CREATE TYPE cakupan_promo_enum AS ENUM ('produk', 'kategori', 'seluruh_katalog');
CREATE TYPE target_promo_enum AS ENUM ('semua_anggota_aktif', 'segmen_tertentu');
CREATE TYPE status_pesanan_enum AS ENUM ('draft', 'menunggu_pembayaran', 'menunggu_verifikasi', 'dibayar', 'diproses', 'siap_diambil', 'dikirim', 'selesai', 'batal', 'kedaluwarsa');
CREATE TYPE status_pembayaran_enum AS ENUM ('menunggu_pembayaran', 'menunggu_verifikasi', 'disetujui', 'ditolak', 'kedaluwarsa', 'dibatalkan');
CREATE TYPE jenis_kas_mutasi_enum AS ENUM ('pemasukan', 'pengeluaran');
CREATE TYPE status_periode_kas_enum AS ENUM ('buka', 'tutup');
CREATE TYPE status_sinkronisasi_enum AS ENUM ('antrean', 'dikirim', 'berhasil', 'gagal');

-- =============================================================================
-- M1: TENANT & LEGALITAS
-- =============================================================================

CREATE TABLE wilayah (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    kode_kemendagri varchar(20) NOT NULL UNIQUE,
    kode_bps varchar(20) UNIQUE,
    nama varchar(150) NOT NULL,
    tingkat tingkat_wilayah_enum NOT NULL,
    parent_id uuid REFERENCES wilayah(id),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz
);
COMMENT ON TABLE wilayah IS 'Referensi berjenjang Provinsi s.d. Desa/Kelurahan sesuai kode Kemendagri & BPS.';

CREATE TABLE koperasi (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    kode_koperasi varchar(32) NOT NULL UNIQUE,
    nama varchar(200) NOT NULL,
    jenis_kopdes varchar(20) NOT NULL CHECK (jenis_kopdes IN ('desa', 'kelurahan')),
    wilayah_desa_id uuid NOT NULL REFERENCES wilayah(id),
    alamat text NOT NULL,
    telepon varchar(30),
    email varchar(150),
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz
);
COMMENT ON TABLE koperasi IS 'Entitas tenant utama Koperasi Desa/Kelurahan Merah Putih (Inpres 9/2025).';

-- =============================================================================
-- M3 (BAGIAN AWAL): USERS, ROLES, PERMISSIONS & DOKUMEN DASAR (UNTUK REFERENSI FK)
-- =============================================================================

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    email_enkripsi bytea NOT NULL,
    email_hash char(64) NOT NULL UNIQUE,
    no_hp_enkripsi bytea NOT NULL,
    no_hp_hash char(64) NOT NULL,
    password_hash text NOT NULL,
    nama_lengkap varchar(150) NOT NULL,
    is_super_admin boolean NOT NULL DEFAULT false,
    status_aktif boolean NOT NULL DEFAULT true,
    anonim_pada timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz
);
COMMENT ON TABLE users IS 'Akun autentikasi pengguna lintas/per koperasi.';
COMMENT ON COLUMN users.email_enkripsi IS '[PII-TERBATAS] Email dienkripsi AES-256-GCM (UU 27/2022 PDP).';
COMMENT ON COLUMN users.email_hash IS '[PII-HASH] HMAC-SHA256 email untuk pencarian cepat & keunikan.';
COMMENT ON COLUMN users.no_hp_enkripsi IS '[PII-TERBATAS] Nomor HP dienkripsi AES-256-GCM (UU 27/2022 PDP).';

CREATE TABLE roles (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    kode varchar(50) NOT NULL UNIQUE,
    nama varchar(100) NOT NULL,
    deskripsi text,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz
);
COMMENT ON TABLE roles IS 'Daftar peran RBAC standar platform dan koperasi.';

CREATE TABLE permissions (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    kode varchar(80) NOT NULL UNIQUE,
    modul varchar(50) NOT NULL,
    deskripsi text,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);
COMMENT ON TABLE permissions IS 'Daftar izin granular per modul.';

CREATE TABLE role_permissions (
    role_id uuid NOT NULL REFERENCES roles(id),
    permission_id uuid NOT NULL REFERENCES permissions(id),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE user_roles (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    user_id uuid NOT NULL REFERENCES users(id),
    role_id uuid NOT NULL REFERENCES roles(id),
    status_aktif boolean NOT NULL DEFAULT true,
    berlaku_mulai timestamptz NOT NULL DEFAULT NOW(),
    berlaku_sampai timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, user_id, role_id)
);
COMMENT ON TABLE user_roles IS 'Penugasan peran pengguna per tenant koperasi.';

CREATE TABLE dokumen (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    nama_file varchar(255) NOT NULL,
    mime_type varchar(100) NOT NULL,
    ukuran_byte bigint NOT NULL CHECK (ukuran_byte > 0),
    storage_key varchar(500) NOT NULL UNIQUE,
    checksum_sha256 char(64) NOT NULL,
    klasifikasi_data varchar(30) NOT NULL CHECK (klasifikasi_data IN ('publik', 'internal', 'rahasia_pii', 'dokumen_keuangan_wajib_simpan')),
    retensi_sampai date NOT NULL,
    is_dihapus_retensi boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz
);
COMMENT ON TABLE dokumen IS 'Metadata dokumen elektronik, checksum integritas (UU ITE), dan jadwal retensi (UU 8/1997).';

-- Lanjutan M1 dengan FK ke users & dokumen
CREATE TABLE koperasi_legalitas (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    no_badan_hukum varchar(100) NOT NULL,
    no_sk_ahu varchar(100) NOT NULL,
    tanggal_sk_ahu date NOT NULL,
    nik_koperasi varchar(50),
    nib varchar(50),
    npwp_enkripsi bytea,
    npwp_hash char(64),
    no_persetujuan_kades varchar(100),
    tanggal_persetujuan_kades date,
    dokumen_sk_ahu_id uuid REFERENCES dokumen(id),
    berlaku_mulai date NOT NULL,
    berlaku_sampai date,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz
);
COMMENT ON TABLE koperasi_legalitas IS 'Data pengesahan badan hukum AHU Online (Permenkum 13/2025), NIK Koperasi, NIB, dan persetujuan Kades (Permendesa PDT 10/2025).';
COMMENT ON COLUMN koperasi_legalitas.npwp_enkripsi IS '[RAHASIA-PAJAK] NPWP Badan Hukum terenkripsi.';

CREATE TABLE koperasi_anggaran_dasar (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    versi integer NOT NULL CHECK (versi >= 1),
    jenis_dokumen varchar(20) NOT NULL CHECK (jenis_dokumen IN ('AD', 'ART', 'AD_PERUBAHAN')),
    no_akta_notaris varchar(100) NOT NULL,
    nama_notaris_npak varchar(150) NOT NULL,
    tanggal_akta date NOT NULL,
    no_sk_ahu_perubahan varchar(100),
    dokumen_id uuid REFERENCES dokumen(id),
    berlaku_mulai date NOT NULL,
    berlaku_sampai date,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, jenis_dokumen, versi)
);
COMMENT ON TABLE koperasi_anggaran_dasar IS 'Riwayat berversi AD/ART koperasi dan Akta NPAK (UU 25/1992 & Permenkum 13/2025).';

CREATE TABLE unit_usaha (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    kode_unit varchar(30) NOT NULL,
    nama varchar(150) NOT NULL,
    kode_kbli varchar(10) NOT NULL,
    jenis_unit varchar(50) NOT NULL CHECK (jenis_unit IN ('gerai_sembako', 'apotek_desa', 'klinik_desa', 'logistik_desa', 'cold_storage', 'kantor_koperasi', 'lainnya')),
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, kode_unit)
);
COMMENT ON TABLE unit_usaha IS 'Unit usaha Kopdes Merah Putih sesuai KBLI (Permenkop 2/2025).';

CREATE TABLE koperasi_nomor_urut (
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    jenis_nomor varchar(30) NOT NULL CHECK (jenis_nomor IN ('anggota', 'registrasi', 'pesanan', 'pembayaran', 'kas')),
    periode_tahun integer NOT NULL,
    nilai_terakhir bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (koperasi_id, jenis_nomor, periode_tahun)
);
COMMENT ON TABLE koperasi_nomor_urut IS 'Counter atomik per koperasi untuk penomoran anggota/pesanan/pembayaran yang aman konkurensi.';

-- =============================================================================
-- M2: ANGGOTA & PENDAFTARAN
-- =============================================================================

CREATE TABLE pendaftaran_anggota (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    user_id uuid NOT NULL REFERENCES users(id),
    no_registrasi varchar(40) NOT NULL,
    nik_enkripsi bytea NOT NULL,
    nik_hash char(64) NOT NULL,
    nama_lengkap varchar(150) NOT NULL,
    tempat_lahir varchar(100) NOT NULL,
    tanggal_lahir_enkripsi bytea NOT NULL,
    jenis_kelamin char(1) NOT NULL CHECK (jenis_kelamin IN ('L', 'P')),
    pekerjaan varchar(100),
    alamat_ktp_enkripsi bytea NOT NULL,
    wilayah_desa_id uuid NOT NULL REFERENCES wilayah(id),
    status status_pendaftaran_enum NOT NULL DEFAULT 'draft',
    catatan_perbaikan text,
    alasan_ditolak text,
    diajukan_oleh uuid NOT NULL REFERENCES users(id),
    diverifikasi_oleh uuid REFERENCES users(id),
    disetujui_oleh uuid REFERENCES users(id),
    waktu_diajukan timestamptz,
    waktu_diverifikasi timestamptz,
    waktu_disetujui timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, no_registrasi),
    CONSTRAINT chk_pendaftaran_maker_checker CHECK (
        (diverifikasi_oleh IS NULL OR diverifikasi_oleh <> diajukan_oleh) AND
        (disetujui_oleh IS NULL OR disetujui_oleh <> diajukan_oleh)
    ),
    CONSTRAINT chk_pendaftaran_alasan_tolak CHECK (
        status <> 'ditolak' OR (alasan_ditolak IS NOT NULL AND length(trim(alasan_ditolak)) > 0)
    )
);
COMMENT ON TABLE pendaftaran_anggota IS 'Formulir pendaftaran calon anggota online dengan kontrol maker-checker.';
COMMENT ON COLUMN pendaftaran_anggota.nik_enkripsi IS '[PII-SPESIFIK/RAHASIA] NIK KTP dienkripsi AES-256-GCM (UU 27/2022 PDP).';
COMMENT ON COLUMN pendaftaran_anggota.nik_hash IS '[PII-HASH] HMAC-SHA256 NIK untuk cek duplikasi tanpa dekripsi.';
COMMENT ON COLUMN pendaftaran_anggota.tanggal_lahir_enkripsi IS '[PII-TERBATAS] Tanggal lahir terenkripsi.';
COMMENT ON COLUMN pendaftaran_anggota.alamat_ktp_enkripsi IS '[PII-TERBATAS] Alamat KTP lengkap terenkripsi.';

CREATE TABLE pendaftaran_status_log (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    pendaftaran_id uuid NOT NULL REFERENCES pendaftaran_anggota(id),
    status_lama status_pendaftaran_enum,
    status_baru status_pendaftaran_enum NOT NULL,
    catatan text,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid NOT NULL REFERENCES users(id)
);
COMMENT ON TABLE pendaftaran_status_log IS 'Jejak audit append-only perubahan status pendaftaran anggota.';

CREATE TABLE pendaftaran_dokumen (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    pendaftaran_id uuid NOT NULL REFERENCES pendaftaran_anggota(id),
    dokumen_id uuid NOT NULL REFERENCES dokumen(id),
    jenis_dokumen jenis_dokumen_anggota_enum NOT NULL,
    storage_key varchar(500) NOT NULL,
    checksum_sha256 char(64) NOT NULL,
    status_verifikasi status_verifikasi_dokumen_enum NOT NULL DEFAULT 'menunggu',
    catatan_verifikasi text,
    diverifikasi_oleh uuid REFERENCES users(id),
    waktu_verifikasi timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz
);
COMMENT ON TABLE pendaftaran_dokumen IS 'Lampiran KTP, KK, foto diri, dan surat domisili saat pendaftaran.';

CREATE TABLE anggota (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    user_id uuid NOT NULL REFERENCES users(id),
    pendaftaran_id uuid REFERENCES pendaftaran_anggota(id),
    no_anggota varchar(40) NOT NULL,
    nik_enkripsi bytea NOT NULL,
    nik_hash char(64) NOT NULL,
    nama_lengkap varchar(150) NOT NULL,
    tempat_lahir varchar(100) NOT NULL,
    tanggal_lahir_enkripsi bytea NOT NULL,
    jenis_kelamin char(1) NOT NULL CHECK (jenis_kelamin IN ('L', 'P')),
    pekerjaan varchar(100),
    status status_anggota_enum NOT NULL DEFAULT 'calon',
    tanggal_bergabung date NOT NULL DEFAULT CURRENT_DATE,
    tanggal_berhenti date,
    alasan_berhenti text,
    anonim_pada timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, no_anggota)
);
COMMENT ON TABLE anggota IS 'Buku Daftar Anggota resmi koperasi (UU 25/1992 Pasal 17-20).';
COMMENT ON COLUMN anggota.nik_enkripsi IS '[PII-SPESIFIK/RAHASIA] NIK KTP anggota dienkripsi AES-256-GCM (UU 27/2022 PDP).';
COMMENT ON COLUMN anggota.nik_hash IS '[PII-HASH] HMAC-SHA256 NIK untuk validasi 1 NIK = 1 keanggotaan aktif per koperasi.';

CREATE TABLE anggota_status_log (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    anggota_id uuid NOT NULL REFERENCES anggota(id),
    status_lama status_anggota_enum,
    status_baru status_anggota_enum NOT NULL,
    alasan text NOT NULL,
    sk_dokumen_id uuid REFERENCES dokumen(id),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid NOT NULL REFERENCES users(id)
);
COMMENT ON TABLE anggota_status_log IS 'Log append-only perubahan status anggota (calon, aktif, nonaktif, keluar, meninggal).';

CREATE TABLE anggota_alamat (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    anggota_id uuid NOT NULL REFERENCES anggota(id),
    jenis_alamat varchar(20) NOT NULL CHECK (jenis_alamat IN ('ktp', 'domisili', 'pengiriman')),
    alamat_enkripsi bytea NOT NULL,
    rt varchar(5),
    rw varchar(5),
    kode_pos varchar(10),
    wilayah_desa_id uuid NOT NULL REFERENCES wilayah(id),
    is_utama boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz
);
COMMENT ON COLUMN anggota_alamat.alamat_enkripsi IS '[PII-TERBATAS] Alamat tempat tinggal anggota terenkripsi.';

CREATE TABLE anggota_keluarga (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    anggota_id uuid NOT NULL REFERENCES anggota(id),
    nama_lengkap varchar(150) NOT NULL,
    hubungan varchar(30) NOT NULL CHECK (hubungan IN ('suami', 'istri', 'anak', 'orang_tua', 'saudara', 'lainnya')),
    nik_enkripsi bytea,
    nik_hash char(64),
    no_hp_enkripsi bytea,
    is_ahli_waris boolean NOT NULL DEFAULT false,
    persentase_waris numeric(5,2) CHECK (persentase_waris IS NULL OR (persentase_waris >= 0 AND persentase_waris <= 100)),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz
);
COMMENT ON TABLE anggota_keluarga IS 'Data keluarga dan penunjukan ahli waris anggota.';
COMMENT ON COLUMN anggota_keluarga.nik_enkripsi IS '[PII-SPESIFIK/RAHASIA] NIK keluarga/ahli waris terenkripsi.';

CREATE TABLE anggota_kontak (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    anggota_id uuid NOT NULL REFERENCES anggota(id),
    jenis_kontak varchar(20) NOT NULL CHECK (jenis_kontak IN ('hp', 'whatsapp', 'email', 'darurat')),
    nilai_enkripsi bytea NOT NULL,
    nilai_hash char(64) NOT NULL,
    is_utama boolean NOT NULL DEFAULT false,
    is_terverifikasi boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz
);
COMMENT ON COLUMN anggota_kontak.nilai_enkripsi IS '[PII-TERBATAS] Nomor kontak/email anggota terenkripsi.';

CREATE TABLE consent_data (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    user_id uuid NOT NULL REFERENCES users(id),
    anggota_id uuid REFERENCES anggota(id),
    tujuan_pemrosesan varchar(100) NOT NULL CHECK (tujuan_pemrosesan IN ('keanggotaan_koperasi', 'pelaporan_simkopdes', 'transaksi_katalog_qris', 'notifikasi_whatsapp')),
    versi_teks_consent varchar(30) NOT NULL,
    hash_teks_consent char(64) NOT NULL,
    is_setuju boolean NOT NULL,
    waktu_consent timestamptz NOT NULL DEFAULT NOW(),
    waktu_pencabutan timestamptz,
    ip_address inet NOT NULL,
    user_agent text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id)
);
COMMENT ON TABLE consent_data IS 'Bukti persetujuan eksplisit pemrosesan data pribadi sesuai UU 27/2022 PDP Pasal 20-23.';

-- =============================================================================
-- M3 (LANJUTAN): TATA KELOLA PENGURUS, PENGAWAS & RAPAT ANGGOTA
-- =============================================================================

CREATE TABLE pengurus_pengawas (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    anggota_id uuid NOT NULL REFERENCES anggota(id),
    user_id uuid NOT NULL REFERENCES users(id),
    jenis_organ varchar(20) NOT NULL CHECK (jenis_organ IN ('pengurus', 'pengawas')),
    jabatan varchar(40) NOT NULL CHECK (jabatan IN ('ketua', 'sekretaris', 'bendahara', 'anggota_pengurus', 'ketua_pengawas', 'anggota_pengawas')),
    periode_mulai date NOT NULL,
    periode_selesai date NOT NULL,
    no_sk varchar(100) NOT NULL,
    tanggal_sk date NOT NULL,
    sk_dokumen_id uuid REFERENCES dokumen(id),
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    CHECK (periode_selesai > periode_mulai)
);
COMMENT ON TABLE pengurus_pengawas IS 'Daftar Pengurus dan Pengawas koperasi beserta masa jabatan & SK (UU 25/1992 Pasal 29-40).';

CREATE TABLE rapat_anggota (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    jenis_rapat varchar(30) NOT NULL CHECK (jenis_rapat IN ('rat', 'ralb', 'rapat_pendirian', 'rapat_khusus')),
    judul varchar(200) NOT NULL,
    jadwal_mulai timestamptz NOT NULL,
    jadwal_selesai timestamptz,
    lokasi varchar(200) NOT NULL,
    jumlah_anggota_hak_suara integer NOT NULL CHECK (jumlah_anggota_hak_suara >= 0),
    jumlah_hadir integer NOT NULL DEFAULT 0 CHECK (jumlah_hadir >= 0),
    syarat_kuorum_persen numeric(5,2) NOT NULL DEFAULT 50.01 CHECK (syarat_kuorum_persen > 0 AND syarat_kuorum_persen <= 100),
    is_kuorum_tercapai boolean NOT NULL DEFAULT false,
    ringkasan_keputusan text,
    berita_acara_dokumen_id uuid REFERENCES dokumen(id),
    status varchar(20) NOT NULL DEFAULT 'dijadwalkan' CHECK (status IN ('dijadwalkan', 'berlangsung', 'selesai', 'dibatalkan')),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz
);
COMMENT ON TABLE rapat_anggota IS 'Catatan kuorum, kehadiran, dan keputusan Rapat Anggota sebagai pemegang kekuasaan tertinggi (UU 25/1992 Pasal 22-28).';

-- =============================================================================
-- M4: IURAN BULANAN ANGGOTA
-- =============================================================================

CREATE TABLE jenis_iuran (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    kode varchar(30) NOT NULL,
    nama varchar(100) NOT NULL,
    versi integer NOT NULL DEFAULT 1 CHECK (versi >= 1),
    nominal numeric(18,2) NOT NULL CHECK (nominal > 0),
    periode_tagih varchar(20) NOT NULL DEFAULT 'bulanan' CHECK (periode_tagih IN ('bulanan')),
    hari_jatuh_tempo integer NOT NULL DEFAULT 10 CHECK (hari_jatuh_tempo BETWEEN 1 AND 28),
    denda_tipe varchar(20) NOT NULL DEFAULT 'tidak_ada' CHECK (denda_tipe IN ('tidak_ada', 'nominal_tetap', 'persen_bulanan')),
    denda_nilai numeric(18,2) NOT NULL DEFAULT 0 CHECK (denda_nilai >= 0),
    klasifikasi_akuntansi varchar(30) NOT NULL DEFAULT 'perlu_keputusan' CHECK (klasifikasi_akuntansi IN ('pendapatan_operasional', 'simpanan_wajib_ekuitas', 'perlu_keputusan')),
    berlaku_mulai date NOT NULL,
    berlaku_sampai date,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, kode, versi)
);
COMMENT ON TABLE jenis_iuran IS 'Konfigurasi berversi jenis iuran bulanan. PERLU KEPUTUSAN: klasifikasi_akuntansi apakah iuran bulanan diakui sebagai pendapatan operasional atau simpanan wajib.';

CREATE TABLE kebijakan_keringanan_iuran (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    kode varchar(30) NOT NULL,
    nama varchar(120) NOT NULL,
    tipe_keringanan varchar(30) NOT NULL CHECK (tipe_keringanan IN ('pembebasan_penuh', 'potongan_persen', 'potongan_nominal')),
    nilai_potongan numeric(18,2) NOT NULL CHECK (nilai_potongan >= 0),
    syarat_deskripsi text NOT NULL,
    berlaku_mulai date NOT NULL,
    berlaku_sampai date,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, kode)
);
COMMENT ON TABLE kebijakan_keringanan_iuran IS 'Aturan kebijakan pembebasan atau keringanan iuran anggota.';

CREATE TABLE job_log (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid REFERENCES koperasi(id),
    nama_job varchar(80) NOT NULL,
    kunci_idempoten varchar(150) NOT NULL UNIQUE,
    periode char(7),
    status varchar(20) NOT NULL CHECK (status IN ('berjalan', 'selesai', 'gagal')),
    jumlah_diproses integer NOT NULL DEFAULT 0,
    jumlah_berhasil integer NOT NULL DEFAULT 0,
    jumlah_dilewati integer NOT NULL DEFAULT 0,
    pesan_error text,
    mulai_pada timestamptz NOT NULL DEFAULT NOW(),
    selesai_pada timestamptz,
    created_by uuid REFERENCES users(id)
);
COMMENT ON TABLE job_log IS 'Catatan eksekusi job terjadwal (termasuk pembuatan tagihan bulanan idempoten).';

CREATE TABLE tagihan_iuran (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    anggota_id uuid NOT NULL REFERENCES anggota(id),
    jenis_iuran_id uuid NOT NULL REFERENCES jenis_iuran(id),
    periode char(7) NOT NULL CHECK (periode ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    jatuh_tempo date NOT NULL,
    nominal_pokok numeric(18,2) NOT NULL CHECK (nominal_pokok >= 0),
    nominal_keringanan numeric(18,2) NOT NULL DEFAULT 0 CHECK (nominal_keringanan >= 0),
    nominal_denda numeric(18,2) NOT NULL DEFAULT 0 CHECK (nominal_denda >= 0),
    nominal_tagihan numeric(18,2) GENERATED ALWAYS AS (nominal_pokok - nominal_keringanan + nominal_denda) STORED,
    nominal_terbayar numeric(18,2) NOT NULL DEFAULT 0 CHECK (nominal_terbayar >= 0),
    status status_tagihan_iuran_enum NOT NULL DEFAULT 'belum_bayar',
    job_eksekusi_id uuid REFERENCES job_log(id),
    waktu_lunas timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, anggota_id, jenis_iuran_id, periode),
    CHECK (nominal_keringanan <= nominal_pokok)
);
COMMENT ON TABLE tagihan_iuran IS 'Tagihan iuran bulanan per anggota per periode (idempoten melalui UNIQUE constraint).';

CREATE TABLE keringanan_iuran_riwayat (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    tagihan_iuran_id uuid NOT NULL REFERENCES tagihan_iuran(id),
    anggota_id uuid NOT NULL REFERENCES anggota(id),
    kebijakan_id uuid REFERENCES kebijakan_keringanan_iuran(id),
    jenis_keringanan varchar(30) NOT NULL CHECK (jenis_keringanan IN ('pembebasan_penuh', 'potongan_sebagian')),
    nominal_sebelum numeric(18,2) NOT NULL CHECK (nominal_sebelum >= 0),
    nominal_keringanan numeric(18,2) NOT NULL CHECK (nominal_keringanan > 0),
    alasan text NOT NULL,
    dokumen_pendukung_id uuid REFERENCES dokumen(id),
    diajukan_oleh uuid NOT NULL REFERENCES users(id),
    disetujui_oleh uuid NOT NULL REFERENCES users(id),
    waktu_persetujuan timestamptz NOT NULL DEFAULT NOW(),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid NOT NULL REFERENCES users(id),
    CONSTRAINT chk_keringanan_maker_checker CHECK (diajukan_oleh <> disetujui_oleh)
);
COMMENT ON TABLE keringanan_iuran_riwayat IS 'Riwayat persetujuan keringanan/pembebasan iuran dengan kontrol maker-checker.';

-- =============================================================================
-- M5: E-KATALOG DENGAN DISKON SPESIAL ANGGOTA
-- =============================================================================

CREATE TABLE kategori_produk (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    parent_id uuid REFERENCES kategori_produk(id),
    kode varchar(30) NOT NULL,
    nama varchar(120) NOT NULL,
    slug varchar(150) NOT NULL,
    urutan integer NOT NULL DEFAULT 0,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, slug)
);

CREATE TABLE merek (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    nama varchar(100) NOT NULL,
    slug varchar(120) NOT NULL,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, slug)
);

CREATE TABLE supplier (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    kode varchar(30) NOT NULL,
    nama varchar(150) NOT NULL,
    npwp_enkripsi bytea,
    kontak_nama varchar(100),
    no_hp_enkripsi bytea,
    alamat text,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, kode)
);

CREATE TABLE produk (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    unit_usaha_id uuid NOT NULL REFERENCES unit_usaha(id),
    kategori_id uuid NOT NULL REFERENCES kategori_produk(id),
    merek_id uuid REFERENCES merek(id),
    sku varchar(60) NOT NULL,
    nama varchar(200) NOT NULL,
    slug varchar(220) NOT NULL,
    deskripsi text,
    satuan varchar(30) NOT NULL,
    atribut jsonb NOT NULL DEFAULT '{}'::jsonb,
    status status_produk_enum NOT NULL DEFAULT 'draft',
    bisa_dibeli_umum boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, sku),
    UNIQUE (koperasi_id, slug)
);
COMMENT ON TABLE produk IS 'Master produk e-katalog koperasi dengan dukungan atribut JSONB dan pencarian teks.';

CREATE TABLE produk_varian (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    produk_id uuid NOT NULL REFERENCES produk(id),
    sku_varian varchar(60) NOT NULL,
    nama_varian varchar(120) NOT NULL,
    atribut_varian jsonb NOT NULL DEFAULT '{}'::jsonb,
    berat_gram integer NOT NULL DEFAULT 0 CHECK (berat_gram >= 0),
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, sku_varian)
);

CREATE TABLE produk_media (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    produk_id uuid NOT NULL REFERENCES produk(id),
    varian_id uuid REFERENCES produk_varian(id),
    dokumen_id uuid REFERENCES dokumen(id),
    url_publik varchar(500) NOT NULL,
    urutan integer NOT NULL DEFAULT 0,
    is_utama boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz
);

CREATE TABLE produk_harga (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    produk_id uuid NOT NULL REFERENCES produk(id),
    varian_id uuid REFERENCES produk_varian(id),
    versi integer NOT NULL DEFAULT 1 CHECK (versi >= 1),
    harga_umum numeric(18,2) NOT NULL CHECK (harga_umum >= 0),
    harga_anggota numeric(18,2) NOT NULL CHECK (harga_anggota >= 0 AND harga_anggota <= harga_umum),
    berlaku_mulai timestamptz NOT NULL,
    berlaku_sampai timestamptz,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    CHECK (berlaku_sampai IS NULL OR berlaku_sampai > berlaku_mulai)
);
COMMENT ON TABLE produk_harga IS 'Daftar harga produk berversi yang memisahkan harga_umum dan harga_anggota spesial koperasi.';

CREATE TABLE gudang (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    unit_usaha_id uuid NOT NULL REFERENCES unit_usaha(id),
    kode varchar(30) NOT NULL,
    nama varchar(120) NOT NULL,
    alamat text,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, kode)
);

CREATE TABLE stok_saldo (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    gudang_id uuid NOT NULL REFERENCES gudang(id),
    produk_id uuid NOT NULL REFERENCES produk(id),
    varian_id uuid REFERENCES produk_varian(id),
    qty_tersedia numeric(18,2) NOT NULL DEFAULT 0 CHECK (qty_tersedia >= 0),
    qty_dipesan numeric(18,2) NOT NULL DEFAULT 0 CHECK (qty_dipesan >= 0),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE NULLS NOT DISTINCT (koperasi_id, gudang_id, produk_id, varian_id)
);
COMMENT ON TABLE stok_saldo IS 'Cache saldo stok terkini per gudang/produk; diperbarui otomatis dari ledger stok_mutasi (dilarang minus).';

CREATE TABLE stok_mutasi (
    id uuid NOT NULL DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    gudang_id uuid NOT NULL REFERENCES gudang(id),
    produk_id uuid NOT NULL REFERENCES produk(id),
    varian_id uuid REFERENCES produk_varian(id),
    jenis_mutasi varchar(30) NOT NULL CHECK (jenis_mutasi IN ('masuk_pembelian', 'keluar_penjualan', 'retur_penjualan', 'retur_pembelian', 'penyesuaian_opname', 'reversal')),
    qty_perubahan numeric(18,2) NOT NULL CHECK (qty_perubahan <> 0),
    qty_saldo_setelah numeric(18,2) NOT NULL CHECK (qty_saldo_setelah >= 0),
    referensi_tipe varchar(30) NOT NULL,
    referensi_id uuid NOT NULL,
    reversal_dari_id uuid,
    catatan text,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid NOT NULL REFERENCES users(id),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
COMMENT ON TABLE stok_mutasi IS 'Ledger mutasi stok append-only dipartisi bulanan; koreksi hanya melalui reversal.';

CREATE TABLE promo (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    kode varchar(40) NOT NULL,
    nama varchar(150) NOT NULL,
    tipe_promo tipe_promo_enum NOT NULL,
    nilai_diskon numeric(18,2) NOT NULL CHECK (nilai_diskon > 0),
    cakupan cakupan_promo_enum NOT NULL,
    target_anggota target_promo_enum NOT NULL DEFAULT 'semua_anggota_aktif',
    segmen_kriteria jsonb NOT NULL DEFAULT '{}'::jsonb,
    berlaku_mulai timestamptz NOT NULL,
    berlaku_sampai timestamptz NOT NULL,
    kuota_total integer CHECK (kuota_total IS NULL OR kuota_total > 0),
    kuota_terpakai integer NOT NULL DEFAULT 0 CHECK (kuota_terpakai >= 0),
    batas_per_anggota integer NOT NULL DEFAULT 1 CHECK (batas_per_anggota > 0),
    minimal_belanja numeric(18,2) NOT NULL DEFAULT 0 CHECK (minimal_belanja >= 0),
    maksimal_potongan numeric(18,2) CHECK (maksimal_potongan IS NULL OR maksimal_potongan > 0),
    boleh_digabung boolean NOT NULL DEFAULT false,
    prioritas integer NOT NULL DEFAULT 1,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, kode),
    CHECK (berlaku_sampai > berlaku_mulai),
    CHECK (tipe_promo <> 'persen' OR nilai_diskon <= 100),
    CHECK (kuota_total IS NULL OR kuota_terpakai <= kuota_total)
);
COMMENT ON TABLE promo IS 'Aturan diskon/promo khusus anggota aktif koperasi.';

CREATE TABLE promo_produk (
    promo_id uuid NOT NULL REFERENCES promo(id),
    produk_id uuid NOT NULL REFERENCES produk(id),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (promo_id, produk_id)
);

CREATE TABLE promo_kategori (
    promo_id uuid NOT NULL REFERENCES promo(id),
    kategori_id uuid NOT NULL REFERENCES kategori_produk(id),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (promo_id, kategori_id)
);

CREATE TABLE keranjang (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    user_id uuid NOT NULL REFERENCES users(id),
    anggota_id uuid REFERENCES anggota(id),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (koperasi_id, user_id)
);

CREATE TABLE keranjang_item (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    keranjang_id uuid NOT NULL REFERENCES keranjang(id) ON DELETE CASCADE,
    produk_id uuid NOT NULL REFERENCES produk(id),
    varian_id uuid REFERENCES produk_varian(id),
    qty numeric(18,2) NOT NULL CHECK (qty > 0),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE NULLS NOT DISTINCT (keranjang_id, produk_id, varian_id)
);

CREATE TABLE pesanan (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    unit_usaha_id uuid NOT NULL REFERENCES unit_usaha(id),
    no_pesanan varchar(40) NOT NULL,
    pembeli_user_id uuid NOT NULL REFERENCES users(id),
    anggota_id uuid REFERENCES anggota(id),
    is_harga_anggota boolean NOT NULL DEFAULT false,
    status status_pesanan_enum NOT NULL DEFAULT 'draft',
    subtotal_harga_umum numeric(18,2) NOT NULL DEFAULT 0 CHECK (subtotal_harga_umum >= 0),
    subtotal_harga_anggota numeric(18,2) NOT NULL DEFAULT 0 CHECK (subtotal_harga_anggota >= 0),
    total_diskon_promo numeric(18,2) NOT NULL DEFAULT 0 CHECK (total_diskon_promo >= 0),
    biaya_pengiriman numeric(18,2) NOT NULL DEFAULT 0 CHECK (biaya_pengiriman >= 0),
    total_akhir numeric(18,2) NOT NULL DEFAULT 0 CHECK (total_akhir >= 0),
    metode_serah_terima varchar(30) NOT NULL DEFAULT 'ambil_di_koperasi' CHECK (metode_serah_terima IN ('ambil_di_koperasi', 'kirim_alamat')),
    catatan_pembeli text,
    batas_waktu_bayar timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, no_pesanan),
    CHECK (NOT is_harga_anggota OR anggota_id IS NOT NULL)
);
COMMENT ON TABLE pesanan IS 'Transaksi pesanan e-katalog untuk anggota maupun pembeli umum.';

CREATE TABLE pesanan_item (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    pesanan_id uuid NOT NULL REFERENCES pesanan(id),
    produk_id uuid NOT NULL REFERENCES produk(id),
    varian_id uuid REFERENCES produk_varian(id),
    produk_harga_id uuid NOT NULL REFERENCES produk_harga(id),
    promo_id uuid REFERENCES promo(id),
    nama_produk_snapshot varchar(200) NOT NULL,
    sku_snapshot varchar(60) NOT NULL,
    qty numeric(18,2) NOT NULL CHECK (qty > 0),
    snapshot_harga_umum numeric(18,2) NOT NULL CHECK (snapshot_harga_umum >= 0),
    snapshot_harga_anggota numeric(18,2) NOT NULL CHECK (snapshot_harga_anggota >= 0),
    snapshot_harga_dipakai numeric(18,2) NOT NULL CHECK (snapshot_harga_dipakai >= 0),
    snapshot_diskon_promo numeric(18,2) NOT NULL DEFAULT 0 CHECK (snapshot_diskon_promo >= 0),
    snapshot_harga_akhir_satuan numeric(18,2) NOT NULL CHECK (snapshot_harga_akhir_satuan >= 0),
    subtotal_akhir numeric(18,2) NOT NULL CHECK (subtotal_akhir >= 0),
    created_at timestamptz NOT NULL DEFAULT NOW()
);
COMMENT ON TABLE pesanan_item IS 'Item pesanan yang menyimpan snapshot harga umum, harga anggota, dan diskon saat transaksi dibuat.';

CREATE TABLE promo_penggunaan (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    promo_id uuid NOT NULL REFERENCES promo(id),
    anggota_id uuid NOT NULL REFERENCES anggota(id),
    pesanan_id uuid NOT NULL REFERENCES pesanan(id),
    nominal_potongan numeric(18,2) NOT NULL CHECK (nominal_potongan > 0),
    status varchar(20) NOT NULL DEFAULT 'dipesan' CHECK (status IN ('dipesan', 'terpakai', 'dibatalkan')),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (promo_id, pesanan_id)
);
COMMENT ON TABLE promo_penggunaan IS 'Jejak pemakaian kuota promo per anggota dan per pesanan.';

CREATE TABLE pengiriman_pesanan (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    pesanan_id uuid NOT NULL REFERENCES pesanan(id) UNIQUE,
    gudang_id uuid NOT NULL REFERENCES gudang(id),
    metode varchar(30) NOT NULL CHECK (metode IN ('ambil_sendiri', 'kurir_desa', 'ekspedisi')),
    nama_penerima varchar(150) NOT NULL,
    no_hp_penerima_enkripsi bytea,
    alamat_tujuan_enkripsi bytea,
    no_resi varchar(100),
    waktu_diserahkan timestamptz,
    diserahkan_oleh uuid REFERENCES users(id),
    bukti_serah_terima_dokumen_id uuid REFERENCES dokumen(id),
    status varchar(30) NOT NULL DEFAULT 'menunggu' CHECK (status IN ('menunggu', 'siap_diambil', 'dalam_pengiriman', 'diterima', 'gagal')),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE TABLE retur_refund (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    pesanan_id uuid NOT NULL REFERENCES pesanan(id),
    anggota_id uuid REFERENCES anggota(id),
    no_retur varchar(40) NOT NULL,
    alasan text NOT NULL,
    jenis_penyelesaian varchar(30) NOT NULL CHECK (jenis_penyelesaian IN ('tukar_barang', 'refund_kas')),
    nominal_refund numeric(18,2) NOT NULL DEFAULT 0 CHECK (nominal_refund >= 0),
    status varchar(20) NOT NULL DEFAULT 'diajukan' CHECK (status IN ('diajukan', 'disetujui', 'ditolak', 'selesai')),
    diajukan_oleh uuid NOT NULL REFERENCES users(id),
    disetujui_oleh uuid REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, no_retur),
    CONSTRAINT chk_retur_maker_checker CHECK (disetujui_oleh IS NULL OR diajukan_oleh <> disetujui_oleh)
);

CREATE TABLE pembelian_supplier (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    supplier_id uuid NOT NULL REFERENCES supplier(id),
    gudang_id uuid NOT NULL REFERENCES gudang(id),
    no_po varchar(40) NOT NULL,
    tanggal_po date NOT NULL DEFAULT CURRENT_DATE,
    total_pembelian numeric(18,2) NOT NULL CHECK (total_pembelian >= 0),
    status varchar(30) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'dipesan', 'diterima_sebagian', 'diterima_penuh', 'dibayar', 'batal')),
    dibuat_oleh uuid NOT NULL REFERENCES users(id),
    disetujui_oleh uuid REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, no_po),
    CONSTRAINT chk_po_maker_checker CHECK (disetujui_oleh IS NULL OR dibuat_oleh <> disetujui_oleh)
);

CREATE TABLE penerimaan_barang (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    pembelian_id uuid NOT NULL REFERENCES pembelian_supplier(id),
    gudang_id uuid NOT NULL REFERENCES gudang(id),
    no_surat_jalan varchar(80) NOT NULL,
    tanggal_terima timestamptz NOT NULL DEFAULT NOW(),
    diterima_oleh uuid NOT NULL REFERENCES users(id),
    catatan text,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid NOT NULL REFERENCES users(id)
);

-- =============================================================================
-- M6: PEMBAYARAN QRIS STATIS
-- =============================================================================

CREATE TABLE penyedia_pembayaran (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    kode varchar(40) NOT NULL UNIQUE,
    nama varchar(100) NOT NULL,
    tipe_integrasi varchar(30) NOT NULL CHECK (tipe_integrasi IN ('qris_statis_manual', 'qris_dinamis_webhook', 'virtual_account', 'tunai')),
    konfigurasi_endpoint jsonb NOT NULL DEFAULT '{}'::jsonb,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);
COMMENT ON TABLE penyedia_pembayaran IS 'Abstraksi provider pembayaran untuk migrasi mulus dari QRIS statis ke QRIS dinamis/payment gateway.';

CREATE TABLE metode_pembayaran (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    penyedia_id uuid NOT NULL REFERENCES penyedia_pembayaran(id),
    kode varchar(40) NOT NULL,
    nama varchar(100) NOT NULL,
    butuh_kode_unik boolean NOT NULL DEFAULT true,
    butuh_verifikasi_manual boolean NOT NULL DEFAULT true,
    batas_waktu_menit integer NOT NULL DEFAULT 1440 CHECK (batas_waktu_menit > 0),
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, kode)
);

CREATE TABLE rekening_qris_koperasi (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    penyedia_id uuid NOT NULL REFERENCES penyedia_pembayaran(id),
    nama_merchant varchar(150) NOT NULL,
    nmid varchar(50) NOT NULL,
    nama_bank_pjp varchar(100) NOT NULL,
    no_rekening_settlement varchar(60) NOT NULL,
    nama_pemilik_rekening varchar(150) NOT NULL,
    is_atas_nama_badan_hukum boolean NOT NULL DEFAULT true CHECK (is_atas_nama_badan_hukum = true),
    qris_payload_statis text NOT NULL,
    qris_image_dokumen_id uuid REFERENCES dokumen(id),
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, nmid)
);
COMMENT ON TABLE rekening_qris_koperasi IS 'Rekening merchant QRIS statis wajib atas nama badan hukum koperasi (PADG BI 21/18/PADG/2019).';

CREATE TABLE pembayaran (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    no_pembayaran varchar(40) NOT NULL,
    pembayar_user_id uuid NOT NULL REFERENCES users(id),
    anggota_id uuid REFERENCES anggota(id),
    metode_pembayaran_id uuid NOT NULL REFERENCES metode_pembayaran(id),
    rekening_qris_id uuid REFERENCES rekening_qris_koperasi(id),
    jenis_transaksi varchar(30) NOT NULL CHECK (jenis_transaksi IN ('iuran_bulanan', 'pesanan_katalog', 'gabungan')),
    nominal_dasar numeric(18,2) NOT NULL CHECK (nominal_dasar > 0),
    kode_unik integer NOT NULL CHECK (kode_unik BETWEEN 1 AND 999),
    nominal_total numeric(18,2) GENERATED ALWAYS AS (nominal_dasar + kode_unik) STORED,
    kode_unik_awal integer NOT NULL CHECK (kode_unik_awal BETWEEN 1 AND 999),
    jumlah_percobaan integer NOT NULL DEFAULT 1 CHECK (jumlah_percobaan BETWEEN 1 AND 999),
    nominal_dibayar_aktual numeric(18,2) CHECK (nominal_dibayar_aktual IS NULL OR nominal_dibayar_aktual >= 0),
    selisih_bayar numeric(18,2) GENERATED ALWAYS AS (COALESCE(nominal_dibayar_aktual, 0) - (nominal_dasar + kode_unik)) STORED,
    perlakuan_selisih varchar(30) NOT NULL DEFAULT 'perlu_keputusan' CHECK (perlakuan_selisih IN ('nihil', 'pendapatan_kode_unik', 'titipan_anggota', 'kurang_bayar_tolak', 'perlu_keputusan')),
    referensi_rrn varchar(100),
    waktu_bayar_klaim timestamptz,
    berlaku_sampai timestamptz NOT NULL,
    status status_pembayaran_enum NOT NULL DEFAULT 'menunggu_pembayaran',
    prioritas_sla integer NOT NULL DEFAULT 10,
    batas_sla_verifikasi timestamptz,
    alasan_ditolak text,
    diverifikasi_oleh uuid REFERENCES users(id),
    waktu_verifikasi timestamptz,
    is_reversal boolean NOT NULL DEFAULT false,
    reversal_dari_id uuid REFERENCES pembayaran(id),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid NOT NULL REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE (koperasi_id, no_pembayaran),
    CONSTRAINT chk_pembayaran_maker_checker CHECK (diverifikasi_oleh IS NULL OR diverifikasi_oleh <> pembayar_user_id),
    CONSTRAINT chk_pembayaran_alasan_ditolak CHECK (status <> 'ditolak' OR (alasan_ditolak IS NOT NULL AND length(trim(alasan_ditolak)) > 0))
);
COMMENT ON TABLE pembayaran IS 'Transaksi pembayaran QRIS statis dengan kode unik 3 digit (1..999) anti-tabrakan dan verifikasi berjenjang.';

CREATE TABLE pembayaran_alokasi (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    pembayaran_id uuid NOT NULL REFERENCES pembayaran(id),
    jenis_alokasi varchar(20) NOT NULL CHECK (jenis_alokasi IN ('tagihan_iuran', 'pesanan')),
    tagihan_iuran_id uuid REFERENCES tagihan_iuran(id),
    pesanan_id uuid REFERENCES pesanan(id),
    urutan_alokasi integer NOT NULL DEFAULT 1 CHECK (urutan_alokasi >= 1),
    nominal_alokasi numeric(18,2) NOT NULL CHECK (nominal_alokasi > 0),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid NOT NULL REFERENCES users(id),
    CONSTRAINT chk_alokasi_target_sah CHECK (
        (jenis_alokasi = 'tagihan_iuran' AND tagihan_iuran_id IS NOT NULL AND pesanan_id IS NULL) OR
        (jenis_alokasi = 'pesanan' AND pesanan_id IS NOT NULL AND tagihan_iuran_id IS NULL)
    )
);
COMMENT ON TABLE pembayaran_alokasi IS 'Alokasi pembayaran ke pesanan atau tagihan_iuran (mendukung rapel banyak bulan dari tagihan tertua).';

CREATE TABLE pembayaran_bukti (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    pembayaran_id uuid NOT NULL REFERENCES pembayaran(id),
    dokumen_id uuid NOT NULL REFERENCES dokumen(id),
    storage_key varchar(500) NOT NULL,
    file_hash_sha256 char(64) NOT NULL UNIQUE,
    nominal_klaim numeric(18,2) NOT NULL CHECK (nominal_klaim > 0),
    waktu_unggah timestamptz NOT NULL DEFAULT NOW(),
    diunggah_oleh uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT NOW()
);
COMMENT ON TABLE pembayaran_bukti IS 'Bukti tangkapan layar pembayaran QRIS dengan UNIQUE file_hash_sha256 anti-penggunaan ulang bukti.';

CREATE TABLE pembayaran_verifikasi_log (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    pembayaran_id uuid NOT NULL REFERENCES pembayaran(id),
    status_lama status_pembayaran_enum,
    status_baru status_pembayaran_enum NOT NULL,
    aksi varchar(30) NOT NULL CHECK (aksi IN ('buat_tagihan', 'unggah_bukti', 'setujui', 'tolak', 'kedaluwarsa', 'batal', 'reversal')),
    catatan text,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid NOT NULL REFERENCES users(id)
);
COMMENT ON TABLE pembayaran_verifikasi_log IS 'Log verifikasi pembayaran append-only.';

-- =============================================================================
-- M7: PENCATATAN KAS RINGAN
-- =============================================================================

CREATE TABLE periode_kas (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    periode char(7) NOT NULL CHECK (periode ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    saldo_awal numeric(18,2) NOT NULL DEFAULT 0,
    total_pemasukan numeric(18,2) NOT NULL DEFAULT 0 CHECK (total_pemasukan >= 0),
    total_pengeluaran numeric(18,2) NOT NULL DEFAULT 0 CHECK (total_pengeluaran >= 0),
    saldo_akhir numeric(18,2) GENERATED ALWAYS AS (saldo_awal + total_pemasukan - total_pengeluaran) STORED,
    status status_periode_kas_enum NOT NULL DEFAULT 'buka',
    dibuka_oleh uuid NOT NULL REFERENCES users(id),
    ditutup_oleh uuid REFERENCES users(id),
    waktu_buka timestamptz NOT NULL DEFAULT NOW(),
    waktu_tutup timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (koperasi_id, periode),
    CONSTRAINT chk_periode_kas_maker_checker CHECK (ditutup_oleh IS NULL OR dibuka_oleh <> ditutup_oleh)
);
COMMENT ON TABLE periode_kas IS 'Kontrol pembukaan dan penutupan buku kas bulanan koperasi.';

CREATE TABLE kas_mutasi (
    id uuid NOT NULL DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    periode_kas_id uuid NOT NULL REFERENCES periode_kas(id),
    unit_usaha_id uuid REFERENCES unit_usaha(id),
    no_bukti_kas varchar(40) NOT NULL,
    jenis jenis_kas_mutasi_enum NOT NULL,
    kategori varchar(40) NOT NULL CHECK (kategori IN ('iuran_pokok_bulanan', 'selisih_kode_unik_qris', 'kelebihan_bayar_anggota', 'penjualan_katalog', 'pembelian_stok', 'refund_pesanan', 'biaya_operasional', 'koreksi_reversal')),
    nominal numeric(18,2) NOT NULL CHECK (nominal > 0),
    pembayaran_id uuid REFERENCES pembayaran(id),
    pesanan_id uuid REFERENCES pesanan(id),
    pembelian_id uuid REFERENCES pembelian_supplier(id),
    is_reversal boolean NOT NULL DEFAULT false,
    reversal_dari_id uuid,
    keterangan text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid NOT NULL REFERENCES users(id),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
COMMENT ON TABLE kas_mutasi IS 'Buku mutasi kas ringan append-only dipartisi bulanan; selisih kode unik QRIS dicatat terpisah.';

-- =============================================================================
-- M8: PELAPORAN SIMKOPDES, AUDIT LOG, NOTIFIKASI, KONFIGURASI & OUTBOX
-- =============================================================================

CREATE TABLE konfigurasi_aturan (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid REFERENCES koperasi(id),
    kunci_aturan varchar(80) NOT NULL,
    versi integer NOT NULL DEFAULT 1 CHECK (versi >= 1),
    nilai_json jsonb NOT NULL,
    keterangan text NOT NULL,
    berlaku_mulai timestamptz NOT NULL,
    berlaku_sampai timestamptz,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id),
    deleted_at timestamptz,
    UNIQUE NULLS NOT DISTINCT (koperasi_id, kunci_aturan, versi)
);
COMMENT ON TABLE konfigurasi_aturan IS 'Tabel aturan bisnis berversi (batas tunggakan diskon, TTL QRIS, perlakuan kode unik, parameter AD/ART).';

CREATE TABLE sinkronisasi_simkopdes (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    kode_wilayah_bps varchar(20) NOT NULL,
    kode_koperasi_kemenkop varchar(50) NOT NULL,
    periode_laporan char(7) NOT NULL CHECK (periode_laporan ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    jenis_payload varchar(40) NOT NULL CHECK (jenis_payload IN ('profil_legalitas', 'rekap_anggota', 'rekap_iuran', 'rekap_unit_usaha_katalog')),
    payload_json jsonb NOT NULL,
    checksum_payload char(64) NOT NULL,
    status_kirim status_sinkronisasi_enum NOT NULL DEFAULT 'antrean',
    jumlah_retry integer NOT NULL DEFAULT 0 CHECK (jumlah_retry >= 0),
    http_status_terakhir integer,
    respon_terakhir text,
    jadwal_retry_berikutnya timestamptz,
    waktu_berhasil_kirim timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid REFERENCES users(id)
);
COMMENT ON TABLE sinkronisasi_simkopdes IS 'Antrean & log sinkronisasi pelaporan ke SIMKOPDES Kemenkop (Permenkop 2/2025; skema endpoint API PERLU VERIFIKASI).';

CREATE TABLE audit_log (
    id uuid NOT NULL DEFAULT uuid_generate_v7(),
    koperasi_id uuid,
    user_id uuid,
    nama_tabel varchar(80) NOT NULL,
    record_id uuid NOT NULL,
    aksi varchar(30) NOT NULL CHECK (aksi IN ('INSERT', 'UPDATE', 'DELETE', 'APPROVE', 'REJECT', 'ANONYMIZE')),
    data_lama jsonb,
    data_baru jsonb,
    ip_address inet,
    user_agent text,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid,
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
COMMENT ON TABLE audit_log IS 'Jejak audit sistem append-only dipartisi bulanan untuk kepatuhan UU PDP & UU ITE.';

CREATE TABLE notifikasi_template (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    kode varchar(60) NOT NULL UNIQUE,
    kanal varchar(20) NOT NULL CHECK (kanal IN ('whatsapp', 'email', 'in_app')),
    judul_template varchar(200) NOT NULL,
    isi_template text NOT NULL,
    status_aktif boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE TABLE notifikasi (
    id uuid NOT NULL DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    user_id uuid NOT NULL REFERENCES users(id),
    template_id uuid REFERENCES notifikasi_template(id),
    kanal varchar(20) NOT NULL CHECK (kanal IN ('whatsapp', 'email', 'in_app')),
    tujuan_enkripsi bytea,
    judul varchar(200) NOT NULL,
    pesan text NOT NULL,
    status_kirim varchar(20) NOT NULL DEFAULT 'antrean' CHECK (status_kirim IN ('antrean', 'terkirim', 'gagal', 'dibaca')),
    waktu_kirim timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE outbox_event (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    topik varchar(80) NOT NULL,
    agregat_tipe varchar(50) NOT NULL,
    agregat_id uuid NOT NULL,
    payload jsonb NOT NULL,
    status varchar(20) NOT NULL DEFAULT 'menunggu' CHECK (status IN ('menunggu', 'diproses', 'selesai', 'gagal')),
    jumlah_percobaan integer NOT NULL DEFAULT 0,
    tersedia_pada timestamptz NOT NULL DEFAULT NOW(),
    diproses_pada timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW()
);
COMMENT ON TABLE outbox_event IS 'Transactional outbox untuk pengiriman notifikasi dan sinkronisasi asinkron secara atomik.';

-- =============================================================================
-- CONTOH PARTISI BULANAN (2026-11 & 2026-12)
-- =============================================================================

CREATE TABLE audit_log_y2026m11 PARTITION OF audit_log FOR VALUES FROM ('2026-11-01 00:00:00+00') TO ('2026-12-01 00:00:00+00');
CREATE TABLE audit_log_y2026m12 PARTITION OF audit_log FOR VALUES FROM ('2026-12-01 00:00:00+00') TO ('2027-01-01 00:00:00+00');

CREATE TABLE stok_mutasi_y2026m11 PARTITION OF stok_mutasi FOR VALUES FROM ('2026-11-01 00:00:00+00') TO ('2026-12-01 00:00:00+00');
CREATE TABLE stok_mutasi_y2026m12 PARTITION OF stok_mutasi FOR VALUES FROM ('2026-12-01 00:00:00+00') TO ('2027-01-01 00:00:00+00');

CREATE TABLE kas_mutasi_y2026m11 PARTITION OF kas_mutasi FOR VALUES FROM ('2026-11-01 00:00:00+00') TO ('2026-12-01 00:00:00+00');
CREATE TABLE kas_mutasi_y2026m12 PARTITION OF kas_mutasi FOR VALUES FROM ('2026-12-01 00:00:00+00') TO ('2027-01-01 00:00:00+00');

CREATE TABLE notifikasi_y2026m11 PARTITION OF notifikasi FOR VALUES FROM ('2026-11-01 00:00:00+00') TO ('2026-12-01 00:00:00+00');
CREATE TABLE notifikasi_y2026m12 PARTITION OF notifikasi FOR VALUES FROM ('2026-12-01 00:00:00+00') TO ('2027-01-01 00:00:00+00');

-- =============================================================================
-- INDEKS (B-TREE, COMPOSITE, PARTIAL, GIN, BRIN) BESERTA COMMENT ALASAN
-- =============================================================================

CREATE UNIQUE INDEX idx_anggota_nik_aktif_unik
    ON anggota (koperasi_id, nik_hash)
    WHERE status IN ('calon', 'aktif', 'nonaktif') AND deleted_at IS NULL;
COMMENT ON INDEX idx_anggota_nik_aktif_unik IS 'Menjamin 1 NIK hanya memiliki 1 keanggotaan aktif/calon per koperasi tanpa menghalangi pendaftaran ulang eks-anggota.';

CREATE UNIQUE INDEX idx_pembayaran_qris_nominal_aktif
    ON pembayaran (koperasi_id, rekening_qris_id, nominal_total)
    WHERE status IN ('menunggu_pembayaran', 'menunggu_verifikasi') AND deleted_at IS NULL;
COMMENT ON INDEX idx_pembayaran_qris_nominal_aktif IS 'Mencegah dua transaksi QRIS aktif pada rekening yang sama memiliki nominal_total identik.';

CREATE UNIQUE INDEX idx_pembayaran_qris_rrn_unik
    ON pembayaran (rekening_qris_id, referensi_rrn)
    WHERE referensi_rrn IS NOT NULL AND status NOT IN ('ditolak', 'dibatalkan') AND deleted_at IS NULL;
COMMENT ON INDEX idx_pembayaran_qris_rrn_unik IS 'Mencegah klaim ganda nomor referensi RRN QRIS yang sama.';

CREATE INDEX idx_pembayaran_antrean_verifikasi
    ON pembayaran (koperasi_id, prioritas_sla DESC, batas_sla_verifikasi ASC, created_at ASC)
    WHERE status = 'menunggu_verifikasi' AND deleted_at IS NULL;
COMMENT ON INDEX idx_pembayaran_antrean_verifikasi IS 'Mempercepat dashboard admin_pembayaran mengambil antrean verifikasi berdasarkan prioritas dan SLA.';

CREATE INDEX idx_tagihan_iuran_fifo_tunggakan
    ON tagihan_iuran (koperasi_id, anggota_id, periode ASC)
    WHERE status IN ('belum_bayar', 'menunggak') AND deleted_at IS NULL;
COMMENT ON INDEX idx_tagihan_iuran_fifo_tunggakan IS 'Mengoptimalkan pengurutan tagihan tertua saat pembayaran rapel dan pengecekan tunggakan N bulan.';

CREATE INDEX idx_produk_atribut_gin ON produk USING gin (atribut jsonb_path_ops);
COMMENT ON INDEX idx_produk_atribut_gin IS 'Mempercepat filter spesifikasi dinamis produk pada atribut JSONB.';

CREATE INDEX idx_produk_fts_gin ON produk USING gin (to_tsvector('indonesian', coalesce(nama, '') || ' ' || coalesce(deskripsi, '')));
COMMENT ON INDEX idx_produk_fts_gin IS 'Pencarian full-text bahasa Indonesia pada katalog produk koperasi.';

CREATE INDEX idx_audit_log_created_brin ON audit_log USING brin (created_at);
COMMENT ON INDEX idx_audit_log_created_brin IS 'Indeks BRIN hemat ruang untuk rentang waktu audit_log skala puluhan juta baris.';

CREATE INDEX idx_kas_mutasi_created_brin ON kas_mutasi USING brin (created_at);
COMMENT ON INDEX idx_kas_mutasi_created_brin IS 'Indeks BRIN untuk pemindaian histori arus kas berurutan waktu.';

CREATE INDEX idx_stok_mutasi_created_brin ON stok_mutasi USING brin (created_at);
COMMENT ON INDEX idx_stok_mutasi_created_brin IS 'Indeks BRIN untuk laporan mutasi stok historis.';

CREATE INDEX idx_outbox_event_antrean
    ON outbox_event (tersedia_pada ASC, created_at ASC)
    WHERE status = 'menunggu';
COMMENT ON INDEX idx_outbox_event_antrean IS 'Mempercepat worker poller mengambil event outbox yang belum terkirim.';

-- =============================================================================
-- FUNGSI & TRIGGER BISNIS INTI
-- =============================================================================

CREATE TRIGGER trg_audit_log_append_only BEFORE UPDATE OR DELETE ON audit_log FOR EACH ROW EXECUTE FUNCTION trg_fn_append_only();
CREATE TRIGGER trg_pendaftaran_status_log_append_only BEFORE UPDATE OR DELETE ON pendaftaran_status_log FOR EACH ROW EXECUTE FUNCTION trg_fn_append_only();
CREATE TRIGGER trg_anggota_status_log_append_only BEFORE UPDATE OR DELETE ON anggota_status_log FOR EACH ROW EXECUTE FUNCTION trg_fn_append_only();
CREATE TRIGGER trg_kas_mutasi_append_only BEFORE UPDATE OR DELETE ON kas_mutasi FOR EACH ROW EXECUTE FUNCTION trg_fn_append_only();
CREATE TRIGGER trg_stok_mutasi_append_only BEFORE UPDATE OR DELETE ON stok_mutasi FOR EACH ROW EXECUTE FUNCTION trg_fn_append_only();
CREATE TRIGGER trg_pembayaran_verifikasi_log_append_only BEFORE UPDATE OR DELETE ON pembayaran_verifikasi_log FOR EACH ROW EXECUTE FUNCTION trg_fn_append_only();

CREATE TRIGGER trg_koperasi_no_delete BEFORE DELETE ON koperasi FOR EACH ROW EXECUTE FUNCTION trg_fn_forbid_hard_delete();
CREATE TRIGGER trg_anggota_no_delete BEFORE DELETE ON anggota FOR EACH ROW EXECUTE FUNCTION trg_fn_forbid_hard_delete();
CREATE TRIGGER trg_tagihan_iuran_no_delete BEFORE DELETE ON tagihan_iuran FOR EACH ROW EXECUTE FUNCTION trg_fn_forbid_hard_delete();
CREATE TRIGGER trg_pesanan_no_delete BEFORE DELETE ON pesanan FOR EACH ROW EXECUTE FUNCTION trg_fn_forbid_hard_delete();
CREATE TRIGGER trg_pembayaran_no_delete BEFORE DELETE ON pembayaran FOR EACH ROW EXECUTE FUNCTION trg_fn_forbid_hard_delete();

-- Trigger ledger stok_mutasi -> update stok_saldo & cegah stok minus
CREATE OR REPLACE FUNCTION trg_fn_proses_stok_mutasi()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_saldo_sekarang numeric(18,2);
    v_saldo_baru numeric(18,2);
BEGIN
    INSERT INTO stok_saldo (koperasi_id, gudang_id, produk_id, varian_id, qty_tersedia, qty_dipesan)
    VALUES (NEW.koperasi_id, NEW.gudang_id, NEW.produk_id, NEW.varian_id, 0, 0)
    ON CONFLICT (koperasi_id, gudang_id, produk_id, varian_id) DO NOTHING;

    SELECT qty_tersedia INTO v_saldo_sekarang
    FROM stok_saldo
    WHERE koperasi_id = NEW.koperasi_id
      AND gudang_id = NEW.gudang_id
      AND produk_id = NEW.produk_id
      AND varian_id IS NOT DISTINCT FROM NEW.varian_id
    FOR UPDATE;

    v_saldo_baru := v_saldo_sekarang + NEW.qty_perubahan;
    IF v_saldo_baru < 0 THEN
        RAISE EXCEPTION 'Stok tidak mencukupi untuk produk % di gudang % (saldo: %, perubahan: %)',
            NEW.produk_id, NEW.gudang_id, v_saldo_sekarang, NEW.qty_perubahan;
    END IF;

    UPDATE stok_saldo
    SET qty_tersedia = v_saldo_baru,
        updated_at = NOW()
    WHERE koperasi_id = NEW.koperasi_id
      AND gudang_id = NEW.gudang_id
      AND produk_id = NEW.produk_id
      AND varian_id IS NOT DISTINCT FROM NEW.varian_id;

    NEW.qty_saldo_setelah := v_saldo_baru;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_stok_mutasi_update_saldo
BEFORE INSERT ON stok_mutasi
FOR EACH ROW EXECUTE FUNCTION trg_fn_proses_stok_mutasi();

-- Trigger validasi harga/diskon spesial anggota aktif & batas tunggakan iuran
CREATE OR REPLACE FUNCTION trg_fn_validasi_hak_diskon_anggota()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_status_anggota status_anggota_enum;
    v_batas_tunggakan int := 2;
    v_jumlah_tunggakan int := 0;
BEGIN
    IF NEW.is_harga_anggota = false AND NEW.total_diskon_promo = 0 THEN
        RETURN NEW;
    END IF;

    IF NEW.anggota_id IS NULL THEN
        RAISE EXCEPTION 'Harga spesial anggota atau promo anggota hanya berlaku bagi pesanan yang memiliki anggota_id.';
    END IF;

    SELECT status INTO v_status_anggota
    FROM anggota
    WHERE id = NEW.anggota_id AND koperasi_id = NEW.koperasi_id AND deleted_at IS NULL;

    IF v_status_anggota IS DISTINCT FROM 'aktif' THEN
        RAISE EXCEPTION 'Anggota % berstatus %, tidak berhak atas harga/diskon spesial anggota aktif.', NEW.anggota_id, v_status_anggota;
    END IF;

    SELECT COALESCE((nilai_json->>'maks_bulan_menunggak_diskon')::int, 2)
    INTO v_batas_tunggakan
    FROM konfigurasi_aturan
    WHERE (koperasi_id = NEW.koperasi_id OR koperasi_id IS NULL)
      AND kunci_aturan = 'kebijakan_diskon_anggota'
      AND status_aktif = true
      AND berlaku_mulai <= NOW()
      AND (berlaku_sampai IS NULL OR berlaku_sampai > NOW())
    ORDER BY koperasi_id NULLS LAST, versi DESC
    LIMIT 1;

    SELECT COUNT(*) INTO v_jumlah_tunggakan
    FROM tagihan_iuran
    WHERE koperasi_id = NEW.koperasi_id
      AND anggota_id = NEW.anggota_id
      AND status = 'menunggak'
      AND deleted_at IS NULL;

    IF v_jumlah_tunggakan > v_batas_tunggakan THEN
        RAISE EXCEPTION 'Anggota memiliki % bulan tunggakan iuran (melebihi batas % bulan), diskon spesial ditangguhkan.',
            v_jumlah_tunggakan, v_batas_tunggakan;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_pesanan_validasi_diskon_anggota
BEFORE INSERT OR UPDATE ON pesanan
FOR EACH ROW EXECUTE FUNCTION trg_fn_validasi_hak_diskon_anggota();

-- Trigger transisi status pembayaran QRIS, imutabilitas status disetujui, & maker-checker
CREATE OR REPLACE FUNCTION trg_fn_validasi_transisi_pembayaran()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status = 'disetujui' AND NEW.status <> 'disetujui' THEN
        RAISE EXCEPTION 'Pembayaran yang sudah disetujui bersifat final dan tidak boleh diubah; gunakan transaksi reversal.';
    END IF;

    IF OLD.status = 'disetujui' AND (
        OLD.nominal_dasar <> NEW.nominal_dasar OR
        OLD.kode_unik <> NEW.kode_unik OR
        OLD.nominal_dibayar_aktual IS DISTINCT FROM NEW.nominal_dibayar_aktual
    ) THEN
        RAISE EXCEPTION 'Nominal pembayaran yang sudah disetujui tidak boleh dimodifikasi.';
    END IF;

    IF OLD.status <> NEW.status THEN
        IF OLD.status = 'menunggu_pembayaran' AND NEW.status NOT IN ('menunggu_verifikasi', 'kedaluwarsa', 'dibatalkan') THEN
            RAISE EXCEPTION 'Transisi status pembayaran tidak sah dari % ke %', OLD.status, NEW.status;
        ELSIF OLD.status = 'menunggu_verifikasi' AND NEW.status NOT IN ('disetujui', 'ditolak') THEN
            RAISE EXCEPTION 'Transisi status pembayaran tidak sah dari % ke %', OLD.status, NEW.status;
        ELSIF OLD.status IN ('ditolak', 'kedaluwarsa', 'dibatalkan') THEN
            RAISE EXCEPTION 'Status terminal % tidak dapat diubah ke %', OLD.status, NEW.status;
        END IF;

        IF NEW.status IN ('disetujui', 'ditolak') THEN
            IF NEW.diverifikasi_oleh IS NULL THEN
                RAISE EXCEPTION 'Kolom diverifikasi_oleh wajib diisi saat menyetujui/menolak pembayaran.';
            END IF;
            IF NEW.diverifikasi_oleh = NEW.created_by OR NEW.diverifikasi_oleh = NEW.pembayar_user_id THEN
                RAISE EXCEPTION 'Pelanggaran Maker-Checker: verifikator tidak boleh menyetujui pembayaran miliknya sendiri.';
            END IF;
            NEW.waktu_verifikasi := COALESCE(NEW.waktu_verifikasi, NOW());
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_pembayaran_transisi_status
BEFORE UPDATE ON pembayaran
FOR EACH ROW EXECUTE FUNCTION trg_fn_validasi_transisi_pembayaran();

-- Prosedur atomik persetujuan pembayaran QRIS dalam SATU transaksi DB
CREATE OR REPLACE FUNCTION fn_setujui_pembayaran_qris(
    p_pembayaran_id uuid,
    p_admin_user_id uuid,
    p_nominal_aktual numeric(18,2),
    p_periode_kas_id uuid,
    p_catatan text
)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    v_pay pembayaran%ROWTYPE;
    v_alokasi RECORD;
BEGIN
    SELECT * INTO v_pay FROM pembayaran WHERE id = p_pembayaran_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Pembayaran % tidak ditemukan.', p_pembayaran_id;
    END IF;

    UPDATE pembayaran
    SET status = 'disetujui',
        nominal_dibayar_aktual = p_nominal_aktual,
        diverifikasi_oleh = p_admin_user_id,
        waktu_verifikasi = NOW(),
        updated_at = NOW()
    WHERE id = p_pembayaran_id;

    INSERT INTO pembayaran_verifikasi_log (koperasi_id, pembayaran_id, status_lama, status_baru, aksi, catatan, created_by)
    VALUES (v_pay.koperasi_id, v_pay.id, v_pay.status, 'disetujui', 'setujui', p_catatan, p_admin_user_id);

    FOR v_alokasi IN
        SELECT * FROM pembayaran_alokasi
        WHERE pembayaran_id = p_pembayaran_id
        ORDER BY urutan_alokasi ASC
    LOOP
        IF v_alokasi.jenis_alokasi = 'tagihan_iuran' THEN
            UPDATE tagihan_iuran
            SET nominal_terbayar = nominal_terbayar + v_alokasi.nominal_alokasi,
                status = CASE WHEN (nominal_terbayar + v_alokasi.nominal_alokasi) >= nominal_tagihan THEN 'lunas'::status_tagihan_iuran_enum ELSE status END,
                waktu_lunas = CASE WHEN (nominal_terbayar + v_alokasi.nominal_alokasi) >= nominal_tagihan THEN NOW() ELSE waktu_lunas END,
                updated_at = NOW()
            WHERE id = v_alokasi.tagihan_iuran_id;
        ELSIF v_alokasi.jenis_alokasi = 'pesanan' THEN
            UPDATE pesanan
            SET status = 'dibayar',
                updated_at = NOW()
            WHERE id = v_alokasi.pesanan_id;
        END IF;
    END LOOP;

    INSERT INTO kas_mutasi (koperasi_id, periode_kas_id, no_bukti_kas, jenis, kategori, nominal, pembayaran_id, keterangan, created_by)
    VALUES (
        v_pay.koperasi_id,
        p_periode_kas_id,
        'KM-' || v_pay.no_pembayaran,
        'pemasukan',
        CASE WHEN v_pay.jenis_transaksi = 'iuran_bulanan' THEN 'iuran_pokok_bulanan' ELSE 'penjualan_katalog' END,
        v_pay.nominal_dasar,
        v_pay.id,
        'Penerimaan pokok pembayaran ' || v_pay.no_pembayaran,
        p_admin_user_id
    );

    IF v_pay.kode_unik > 0 THEN
        INSERT INTO kas_mutasi (koperasi_id, periode_kas_id, no_bukti_kas, jenis, kategori, nominal, pembayaran_id, keterangan, created_by)
        VALUES (
            v_pay.koperasi_id,
            p_periode_kas_id,
            'KM-UNIK-' || v_pay.no_pembayaran,
            'pemasukan',
            'selisih_kode_unik_qris',
            v_pay.kode_unik,
            v_pay.id,
            'Selisih kode unik QRIS pembayaran ' || v_pay.no_pembayaran,
            p_admin_user_id
        );
    END IF;

    UPDATE periode_kas
    SET total_pemasukan = total_pemasukan + v_pay.nominal_dasar + v_pay.kode_unik,
        updated_at = NOW()
    WHERE id = p_periode_kas_id;

    INSERT INTO outbox_event (koperasi_id, topik, agregat_tipe, agregat_id, payload)
    VALUES (
        v_pay.koperasi_id,
        'pembayaran.disetujui',
        'pembayaran',
        v_pay.id,
        jsonb_build_object(
            'pembayaran_id', v_pay.id,
            'no_pembayaran', v_pay.no_pembayaran,
            'nominal_total', v_pay.nominal_total,
            'diverifikasi_oleh', p_admin_user_id
        )
    );
END;
$$;

-- =============================================================================
-- VIEWS PELAPORAN KAS RINGAN (M7)
-- =============================================================================

CREATE OR REPLACE VIEW v_arus_kas_bulanan AS
SELECT
    km.koperasi_id,
    pk.periode,
    km.kategori,
    km.jenis,
    COUNT(*) AS jumlah_transaksi,
    SUM(km.nominal) AS total_nominal
FROM kas_mutasi km
JOIN periode_kas pk ON pk.id = km.periode_kas_id
GROUP BY km.koperasi_id, pk.periode, km.kategori, km.jenis;

CREATE OR REPLACE VIEW v_rekap_iuran_bulanan AS
SELECT
    ti.koperasi_id,
    ti.periode,
    COUNT(*) AS total_anggota_ditagih,
    COUNT(*) FILTER (WHERE ti.status = 'lunas') AS jumlah_lunas,
    COUNT(*) FILTER (WHERE ti.status IN ('belum_bayar', 'menunggak')) AS jumlah_belum_lunas,
    COUNT(*) FILTER (WHERE ti.status = 'dibebaskan') AS jumlah_dibebaskan,
    SUM(ti.nominal_tagihan) AS total_nominal_tagihan,
    SUM(ti.nominal_terbayar) AS total_nominal_terbayar,
    SUM(ti.nominal_tagihan - ti.nominal_terbayar) AS total_piutang_iuran
FROM tagihan_iuran ti
WHERE ti.deleted_at IS NULL
GROUP BY ti.koperasi_id, ti.periode;

CREATE OR REPLACE VIEW v_rekap_penjualan_bulanan AS
SELECT
    p.koperasi_id,
    to_char(p.created_at, 'YYYY-MM') AS periode,
    COUNT(*) AS jumlah_pesanan,
    SUM(p.subtotal_harga_umum) AS nilai_harga_umum,
    SUM(p.subtotal_harga_umum - p.subtotal_harga_anggota) AS penghematan_harga_anggota,
    SUM(p.total_diskon_promo) AS total_diskon_promo,
    SUM(p.total_akhir) AS total_penjualan_bersih
FROM pesanan p
WHERE p.status IN ('dibayar', 'diproses', 'siap_diambil', 'dikirim', 'selesai')
  AND p.deleted_at IS NULL
GROUP BY p.koperasi_id, to_char(p.created_at, 'YYYY-MM');

-- =============================================================================
-- KEBIJAKAN ROW LEVEL SECURITY (RLS) MULTI-TENANT
-- =============================================================================

DO $$
DECLARE
    t text;
    tenant_tables text[] := ARRAY[
        'koperasi_legalitas', 'koperasi_anggaran_dasar', 'unit_usaha', 'koperasi_nomor_urut',
        'user_roles', 'dokumen', 'pendaftaran_anggota', 'pendaftaran_status_log', 'pendaftaran_dokumen',
        'anggota', 'anggota_status_log', 'anggota_alamat', 'anggota_keluarga', 'anggota_kontak',
        'consent_data', 'pengurus_pengawas', 'rapat_anggota', 'jenis_iuran', 'kebijakan_keringanan_iuran',
        'tagihan_iuran', 'keringanan_iuran_riwayat', 'kategori_produk', 'merek', 'supplier',
        'produk', 'produk_varian', 'produk_media', 'produk_harga', 'gudang', 'stok_saldo', 'stok_mutasi',
        'promo', 'promo_produk', 'promo_kategori', 'keranjang', 'keranjang_item', 'pesanan', 'pesanan_item',
        'promo_penggunaan', 'pengiriman_pesanan', 'retur_refund', 'pembelian_supplier', 'penerimaan_barang',
        'metode_pembayaran', 'rekening_qris_koperasi', 'pembayaran', 'pembayaran_alokasi', 'pembayaran_bukti',
        'pembayaran_verifikasi_log', 'periode_kas', 'kas_mutasi', 'sinkronisasi_simkopdes', 'notifikasi', 'outbox_event'
    ];
BEGIN
    FOREACH t IN ARRAY tenant_tables LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY;', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY;', t);
        EXECUTE format(
            'CREATE POLICY pol_isolasi_tenant_%I ON %I USING (app_is_platform_admin() OR koperasi_id = app_current_koperasi_id()) WITH CHECK (app_is_platform_admin() OR koperasi_id = app_current_koperasi_id());',
            t, t
        );
    END LOOP;
END;
$$;

-- =============================================================================
-- PERAN DATABASE (ROLES) & HAK AKSES (GRANT)
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kopdes_migrator') THEN
        CREATE ROLE kopdes_migrator NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kopdes_app') THEN
        CREATE ROLE kopdes_app NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kopdes_readonly_auditor') THEN
        CREATE ROLE kopdes_readonly_auditor NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
    END IF;
END;
$$;

REVOKE CREATE ON SCHEMA public FROM kopdes_app;
GRANT USAGE ON SCHEMA public TO kopdes_app, kopdes_readonly_auditor;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA public TO kopdes_app;
REVOKE UPDATE, DELETE ON audit_log, pendaftaran_status_log, anggota_status_log, kas_mutasi, stok_mutasi, pembayaran_verifikasi_log FROM kopdes_app;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO kopdes_readonly_auditor;