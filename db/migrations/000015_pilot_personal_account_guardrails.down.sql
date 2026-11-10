DELETE FROM konfigurasi_aturan WHERE kunci_aturan IN ('pilot_rekening_pribadi_max_per_transaksi', 'pilot_rekening_pribadi_max_per_hari') AND koperasi_id IS NULL;
