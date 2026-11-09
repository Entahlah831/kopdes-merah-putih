-- ==============================================================================
-- MIGRASI 000003: Perbaikan Alur Verifikasi Pembayaran Atomik & Klaim Bukti (K3)
-- ==============================================================================

-- 1. Prosedur Klaim / Submit Bukti Pembayaran (Transisi: menunggu_pembayaran -> menunggu_verifikasi)
CREATE OR REPLACE FUNCTION fn_klaim_bukti_pembayaran(
    p_pembayaran_id uuid,
    p_user_id uuid,
    p_referensi_rrn varchar(100),
    p_catatan text
)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    v_pay pembayaran%ROWTYPE;
BEGIN
    SELECT * INTO v_pay FROM pembayaran WHERE id = p_pembayaran_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Pembayaran % tidak ditemukan.', p_pembayaran_id;
    END IF;

    IF v_pay.status <> 'menunggu_pembayaran' THEN
        RAISE EXCEPTION 'Pembayaran % tidak dalam status menunggu pembayaran (status saat ini: %)', p_pembayaran_id, v_pay.status;
    END IF;

    UPDATE pembayaran
    SET status = 'menunggu_verifikasi',
        waktu_bayar_klaim = NOW(),
        referensi_rrn = COALESCE(p_referensi_rrn, referensi_rrn),
        updated_at = NOW()
    WHERE id = p_pembayaran_id;

    INSERT INTO pembayaran_verifikasi_log (
        koperasi_id, pembayaran_id, status_lama, status_baru, aksi, catatan, created_by
    ) VALUES (
        v_pay.koperasi_id, v_pay.id, 'menunggu_pembayaran', 'menunggu_verifikasi', 'unggah_bukti', p_catatan, p_user_id
    );
END;
$$;

-- 2. Prosedur Atomik Persetujuan Pembayaran QRIS (Transisi: menunggu_verifikasi -> disetujui)
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

    -- Validasi status awal: hanya pembayaran dalam antrean menunggu_verifikasi yang sah untuk disetujui
    IF v_pay.status <> 'menunggu_verifikasi' THEN
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

    -- Catat log verifikasi dengan skema kolom yang benar
    INSERT INTO pembayaran_verifikasi_log (
        koperasi_id, pembayaran_id, status_lama, status_baru, aksi, catatan, created_by
    ) VALUES (
        v_pay.koperasi_id, v_pay.id, 'menunggu_verifikasi', 'disetujui', 'setujui', p_catatan, p_admin_user_id
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
