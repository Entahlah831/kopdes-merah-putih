-- =============================================================================
-- Migrasi 000006: Kolom jenis_pemilik pada rekening_qris_koperasi & relaksasi pilot
-- =============================================================================

ALTER TABLE rekening_qris_koperasi
ADD COLUMN IF NOT EXISTS jenis_pemilik varchar(30) NOT NULL DEFAULT 'badan_hukum_koperasi'
CHECK (jenis_pemilik IN ('badan_hukum_koperasi', 'perorangan_pengurus'));

COMMENT ON COLUMN rekening_qris_koperasi.jenis_pemilik IS 'Klasifikasi kepemilikan rekening merchant QRIS: badan_hukum_koperasi (standar resmi) atau perorangan_pengurus (pilot terkendali).';

-- Hapus konstrain kaku yang mewajibkan is_atas_nama_badan_hukum selalu true
ALTER TABLE rekening_qris_koperasi
DROP CONSTRAINT IF EXISTS rekening_qris_koperasi_is_atas_nama_badan_hukum_check;

-- Ganti dengan konstrain bersyarat yang mengizinkan perorangan_pengurus bila bukan badan hukum
ALTER TABLE rekening_qris_koperasi
ADD CONSTRAINT chk_rekening_qris_badan_hukum
CHECK (
    (jenis_pemilik = 'badan_hukum_koperasi' AND is_atas_nama_badan_hukum = true) OR
    (jenis_pemilik = 'perorangan_pengurus' AND is_atas_nama_badan_hukum = false)
);
