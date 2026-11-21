-- name: LaporanRingkasanAnggota :many
SELECT status, COUNT(*)::bigint AS jumlah
FROM anggota
GROUP BY status
ORDER BY status ASC;

-- name: LaporanTunggakanIuran :many
SELECT a.nomor_anggota,
       a.nama_lengkap,
       j.kode AS jenis_kode,
       j.nama AS jenis_nama,
       t.periode_tahun,
       t.periode_bulan,
       (t.nominal - t.nominal_keringanan)::bigint AS nominal_terutang,
       t.tanggal_jatuh_tempo
FROM tagihan_iuran t
JOIN anggota a ON a.id = t.anggota_id
JOIN jenis_iuran j ON j.id = t.jenis_iuran_id
WHERE t.status = 'belum_bayar'
  AND t.tanggal_jatuh_tempo < CURRENT_DATE
ORDER BY t.tanggal_jatuh_tempo ASC, a.nomor_anggota ASC;

-- name: LaporanPenjualanHarian :many
SELECT (created_at AT TIME ZONE sqlc.arg(tz)::text)::date AS tanggal,
       COUNT(*)::bigint AS jumlah_pesanan,
       COALESCE(SUM(subtotal), 0)::bigint AS total_subtotal,
       COALESCE(SUM(diskon), 0)::bigint AS total_diskon,
       COALESCE(SUM(ongkir), 0)::bigint AS total_ongkir,
       COALESCE(SUM(total_bayar), 0)::bigint AS total_omzet
FROM pesanan
WHERE status IN ('dibayar', 'diproses', 'diantar', 'siap_diambil', 'selesai')
  AND (sqlc.narg(dari)::date IS NULL OR (created_at AT TIME ZONE sqlc.arg(tz)::text)::date >= sqlc.narg(dari)::date)
  AND (sqlc.narg(sampai)::date IS NULL OR (created_at AT TIME ZONE sqlc.arg(tz)::text)::date <= sqlc.narg(sampai)::date)
GROUP BY (created_at AT TIME ZONE sqlc.arg(tz)::text)::date
ORDER BY tanggal DESC;

-- name: LaporanPembayaranRekap :many
SELECT p.jenis_transaksi,
       p.metode,
       p.status,
       COUNT(*)::bigint AS jumlah_transaksi,
       COALESCE(SUM(p.nominal_pokok), 0)::bigint AS total_pokok,
       COALESCE(SUM(p.selisih_kode_unik), 0)::bigint AS total_selisih_kode_unik,
       COALESCE(SUM(p.nominal_dibayar), 0)::bigint AS total_dibayar,
       COALESCE(SUM(CASE WHEN pd.status = 'selesai' THEN pd.nominal ELSE 0 END), 0)::bigint AS total_dikembalikan
FROM pembayaran p
LEFT JOIN pengembalian_dana pd ON pd.pembayaran_id = p.id
WHERE (sqlc.narg(dari)::date IS NULL OR (p.created_at AT TIME ZONE sqlc.arg(tz)::text)::date >= sqlc.narg(dari)::date)
  AND (sqlc.narg(sampai)::date IS NULL OR (p.created_at AT TIME ZONE sqlc.arg(tz)::text)::date <= sqlc.narg(sampai)::date)
GROUP BY p.jenis_transaksi, p.metode, p.status
ORDER BY p.jenis_transaksi ASC, p.metode ASC, p.status ASC;
