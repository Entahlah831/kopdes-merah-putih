-- ==============================================================================
-- MIGRASI 000015: Konfigurasi Aturan Guardrails Rekening Pribadi Pilot (Task 6)
-- ==============================================================================

-- 1. Batas nominal maksimum per transaksi untuk rekening QRIS pilot perorangan (Default: Rp 1.000.000)
INSERT INTO konfigurasi_aturan (
    koperasi_id, kunci_aturan, versi, nilai_json, keterangan, berlaku_mulai, status_aktif
) VALUES (
    NULL,
    'pilot_rekening_pribadi_max_per_transaksi',
    1,
    '{"nominal": 1000000}'::jsonb,
    'Batas nominal maksimum per transaksi untuk rekening QRIS perorangan pengurus (pilot mode)',
    NOW(),
    true
) ON CONFLICT (koperasi_id, kunci_aturan, versi) DO NOTHING;

-- 2. Batas nominal akumulasi harian untuk rekening QRIS pilot perorangan (Default: Rp 5.000.000)
INSERT INTO konfigurasi_aturan (
    koperasi_id, kunci_aturan, versi, nilai_json, keterangan, berlaku_mulai, status_aktif
) VALUES (
    NULL,
    'pilot_rekening_pribadi_max_per_hari',
    1,
    '{"nominal": 5000000}'::jsonb,
    'Batas nominal akumulasi pembayaran per hari untuk rekening QRIS perorangan pengurus (pilot mode)',
    NOW(),
    true
) ON CONFLICT (koperasi_id, kunci_aturan, versi) DO NOTHING;
