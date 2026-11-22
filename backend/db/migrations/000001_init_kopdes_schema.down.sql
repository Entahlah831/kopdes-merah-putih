DROP TRIGGER IF EXISTS trg_audit_log_immutable ON audit_log;
DROP TABLE IF EXISTS
    audit_log,
    pembayaran_alokasi,
    pembayaran,
    rekening_qris,
    pesanan_item,
    pesanan,
    promo,
    produk,
    kategori,
    tagihan_iuran,
    jenis_iuran,
    dokumen_kyc,
    pendaftaran,
    anggota,
    rate_limits,
    kode_aktivasi,
    sesi,
    users,
    pengaturan
CASCADE;
DROP SEQUENCE IF EXISTS seq_nomor_kwitansi;
DROP FUNCTION IF EXISTS trg_fn_reject_audit_mutation();
DROP FUNCTION IF EXISTS trg_fn_set_updated_at();
DROP FUNCTION IF EXISTS uuid_generate_v7();
