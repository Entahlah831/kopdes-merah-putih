ALTER TABLE rekening_qris_koperasi
DROP CONSTRAINT IF EXISTS chk_rekening_qris_badan_hukum;

ALTER TABLE rekening_qris_koperasi
ADD CONSTRAINT rekening_qris_koperasi_is_atas_nama_badan_hukum_check
CHECK (is_atas_nama_badan_hukum = true);

ALTER TABLE rekening_qris_koperasi
DROP COLUMN IF EXISTS jenis_pemilik;
