-- ==============================================================================
-- MIGRASI 000014: Impor Mutasi Bank, Rekonsiliasi Otomatis & Persetujuan Ganda (Task 5)
-- ==============================================================================

-- 1. Tambahkan status 'menunggu_persetujuan_kedua' pada enum status pembayaran
ALTER TYPE status_pembayaran_enum ADD VALUE IF NOT EXISTS 'menunggu_persetujuan_kedua' BEFORE 'disetujui';

-- 2. Tambah kolom persetujuan ganda dan penanda anomali pada tabel pembayaran
ALTER TABLE pembayaran ADD COLUMN IF NOT EXISTS persetujuan_pertama_oleh uuid REFERENCES users(id);
ALTER TABLE pembayaran ADD COLUMN IF NOT EXISTS waktu_persetujuan_pertama timestamptz;
ALTER TABLE pembayaran ADD COLUMN IF NOT EXISTS persetujuan_kedua_oleh uuid REFERENCES users(id);
ALTER TABLE pembayaran ADD COLUMN IF NOT EXISTS waktu_persetujuan_kedua timestamptz;
ALTER TABLE pembayaran ADD COLUMN IF NOT EXISTS is_anomali boolean NOT NULL DEFAULT false;
ALTER TABLE pembayaran ADD COLUMN IF NOT EXISTS indikator_anomali text[] DEFAULT ARRAY[]::text[];

-- 3. Tabel mutasi_bank_impor untuk mencatat hasil impor rekening koran / settlement bank (CSV)
CREATE TABLE IF NOT EXISTS mutasi_bank_impor (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
    koperasi_id uuid NOT NULL REFERENCES koperasi(id),
    no_rekening varchar(50) NOT NULL,
    tanggal_mutasi timestamptz NOT NULL,
    keterangan text NOT NULL,
    tipe varchar(5) NOT NULL CHECK (tipe IN ('CR', 'DB')),
    nominal numeric(18,2) NOT NULL CHECK (nominal > 0),
    saldo_akhir numeric(18,2),
    status_rekonsiliasi varchar(30) NOT NULL DEFAULT 'belum_cocok' CHECK (status_rekonsiliasi IN ('belum_cocok', 'tercocokkan', 'anomali')),
    pembayaran_id uuid REFERENCES pembayaran(id),
    catatan_rekonsiliasi text,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    created_by uuid NOT NULL REFERENCES users(id)
);

COMMENT ON TABLE mutasi_bank_impor IS 'Log mutasi rekening bank hasil impor CSV untuk pencocokan otomatis dan rekonsiliasi harian.';

CREATE INDEX IF NOT EXISTS idx_mutasi_bank_lookup ON mutasi_bank_impor(koperasi_id, tanggal_mutasi, nominal);

ALTER TABLE mutasi_bank_impor ENABLE ROW LEVEL SECURITY;
ALTER TABLE mutasi_bank_impor FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS pol_isolasi_tenant_mutasi_bank_impor ON mutasi_bank_impor;
CREATE POLICY pol_isolasi_tenant_mutasi_bank_impor ON mutasi_bank_impor
    USING (app_is_platform_admin() OR koperasi_id = app_current_koperasi_id())
    WITH CHECK (app_is_platform_admin() OR koperasi_id = app_current_koperasi_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON mutasi_bank_impor TO kopdes_app;
GRANT SELECT ON mutasi_bank_impor TO kopdes_readonly_auditor;

-- 4. Konfigurasi default ambang batas persetujuan ganda (Rp 5.000.000)
INSERT INTO konfigurasi_aturan (
    koperasi_id, kunci_aturan, versi, nilai_json, keterangan, berlaku_mulai, status_aktif
) VALUES (
    NULL,
    'ambang_persetujuan_ganda_rp',
    1,
    '{"nominal": 5000000}'::jsonb,
    'Ambang nominal transaksi yang mewajibkan persetujuan ganda dua verifikator berbeda (Maker-Checker bertingkat)',
    NOW(),
    true
) ON CONFLICT (koperasi_id, kunci_aturan, versi) DO NOTHING;

-- 5. Perbarui fungsi validasi transisi status pembayaran untuk mendukung 'menunggu_persetujuan_kedua'
CREATE OR REPLACE FUNCTION trg_fn_validasi_transisi_pembayaran()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
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
        ELSIF OLD.status = 'menunggu_verifikasi' AND NEW.status NOT IN ('menunggu_persetujuan_kedua', 'disetujui', 'ditolak') THEN
            RAISE EXCEPTION 'Transisi status pembayaran tidak sah dari % ke %', OLD.status, NEW.status;
        ELSIF OLD.status = 'menunggu_persetujuan_kedua' AND NEW.status NOT IN ('disetujui', 'ditolak') THEN
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

-- 6. Perbarui fungsi fn_setujui_pembayaran_qris untuk mengizinkan persetujuan dari 'menunggu_persetujuan_kedua'
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
    v_total_dibutuhkan numeric(18,2);
BEGIN
    SELECT * INTO v_pay FROM pembayaran WHERE id = p_pembayaran_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Pembayaran % tidak ditemukan.', p_pembayaran_id;
    END IF;

    -- Validasi status awal: pembayaran dalam antrean menunggu_verifikasi atau menunggu_persetujuan_kedua yang sah untuk disetujui
    IF v_pay.status NOT IN ('menunggu_verifikasi', 'menunggu_persetujuan_kedua') THEN
        RAISE EXCEPTION 'Pembayaran % tidak dalam status menunggu verifikasi (status saat ini: %)', p_pembayaran_id, v_pay.status;
    END IF;

    -- Validasi Maker-Checker
    IF v_pay.pembayar_user_id = p_admin_user_id OR v_pay.created_by = p_admin_user_id THEN
        RAISE EXCEPTION 'MAKER_CHECKER_VIOLATION: Pembuat transaksi dilarang memverifikasi pembayarannya sendiri';
    END IF;

    -- Validasi nominal aktual tidak boleh kurang bayar
    v_total_dibutuhkan := v_pay.nominal_dasar + v_pay.kode_unik;
    IF p_nominal_aktual < v_total_dibutuhkan THEN
        RAISE EXCEPTION 'Nominal aktual (%) kurang dari total pembayaran (%)', p_nominal_aktual, v_total_dibutuhkan;
    END IF;

    -- Update pembayaran ke status disetujui
    UPDATE pembayaran
    SET status = 'disetujui',
        nominal_dibayar_aktual = p_nominal_aktual,
        diverifikasi_oleh = p_admin_user_id,
        waktu_verifikasi = NOW(),
        updated_at = NOW()
    WHERE id = p_pembayaran_id;

    -- Catat log verifikasi dengan status lama aktual
    INSERT INTO pembayaran_verifikasi_log (
        koperasi_id, pembayaran_id, status_lama, status_baru, aksi, catatan, created_by
    ) VALUES (
        v_pay.koperasi_id, v_pay.id, v_pay.status, 'disetujui', 'setujui', p_catatan, p_admin_user_id
    );

    -- Perbarui tagihan iuran atau pesanan terkait jika terdaftar di alokasi
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

    -- Catat mutasi kas pokok
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

    -- Catat kode unik pada kas mutasi jika ada
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

    -- Update akumulasi total pemasukan periode kas
    UPDATE periode_kas
    SET total_pemasukan = total_pemasukan + v_pay.nominal_dasar + v_pay.kode_unik,
        updated_at = NOW()
    WHERE id = p_periode_kas_id;

    -- Publikasikan event outbox untuk audit / integrasi
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


