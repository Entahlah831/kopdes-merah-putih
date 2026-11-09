-- =============================================================================
-- QUERIES.SQL — KOLEKSI QUERY BERNAMA (GAYA SQLC) APLIKASI KOPDES MERAH PUTIH
-- =============================================================================

-- name: ListAntreanVerifikasiQRIS :many
SELECT
    p.id,
    p.no_pembayaran,
    p.jenis_transaksi,
    p.nominal_dasar,
    p.kode_unik,
    p.nominal_total,
    p.referensi_rrn,
    p.waktu_bayar_klaim,
    p.prioritas_sla,
    p.batas_sla_verifikasi,
    a.no_anggota,
    u.nama_lengkap AS nama_pembayar,
    rq.nama_merchant,
    rq.nmid,
    pb.storage_key AS bukti_storage_key,
    pb.file_hash_sha256,
    pb.nominal_klaim
FROM pembayaran p
JOIN users u ON u.id = p.pembayar_user_id
LEFT JOIN anggota a ON a.id = p.anggota_id
LEFT JOIN rekening_qris_koperasi rq ON rq.id = p.rekening_qris_id
LEFT JOIN LATERAL (
    SELECT storage_key, file_hash_sha256, nominal_klaim
    FROM pembayaran_bukti b
    WHERE b.pembayaran_id = p.id
    ORDER BY b.waktu_unggah DESC
    LIMIT 1
) pb ON true
WHERE p.koperasi_id = $1
  AND p.status = 'menunggu_verifikasi'
  AND p.deleted_at IS NULL
ORDER BY p.prioritas_sla DESC, p.batas_sla_verifikasi ASC NULLS LAST, p.created_at ASC
LIMIT $2 OFFSET $3;

-- name: MatchMutasiRekonsiliasiQRIS :one
SELECT
    p.id,
    p.no_pembayaran,
    p.pembayar_user_id,
    p.anggota_id,
    p.nominal_dasar,
    p.kode_unik,
    p.nominal_total,
    p.referensi_rrn,
    p.status,
    p.berlaku_sampai
FROM pembayaran p
WHERE p.koperasi_id = $1
  AND p.rekening_qris_id = $2
  AND p.nominal_total = $3
  AND p.status IN ('menunggu_pembayaran', 'menunggu_verifikasi')
  AND p.deleted_at IS NULL
LIMIT 1;

-- name: ListAnggotaMenunggakIuran :many
SELECT
    a.id AS anggota_id,
    a.no_anggota,
    a.nama_lengkap,
    COUNT(ti.id) AS jumlah_bulan_menunggak,
    MIN(ti.periode) AS periode_tertua,
    MAX(ti.periode) AS periode_terbaru,
    SUM(ti.nominal_tagihan - ti.nominal_terbayar)::numeric(18,2) AS total_nominal_tunggakan
FROM anggota a
JOIN tagihan_iuran ti ON ti.anggota_id = a.id AND ti.koperasi_id = a.koperasi_id
WHERE a.koperasi_id = $1
  AND a.status = 'aktif'
  AND a.deleted_at IS NULL
  AND ti.status IN ('belum_bayar', 'menunggak')
  AND ti.jatuh_tempo < CURRENT_DATE
  AND ti.deleted_at IS NULL
GROUP BY a.id, a.no_anggota, a.nama_lengkap
HAVING COUNT(ti.id) >= @min_bulan::bigint
ORDER BY jumlah_bulan_menunggak DESC, total_nominal_tunggakan DESC;

-- name: GetRekapIuranBulanan :many
SELECT
    koperasi_id,
    periode,
    total_anggota_ditagih,
    jumlah_lunas,
    jumlah_belum_lunas,
    jumlah_dibebaskan,
    total_nominal_tagihan,
    total_nominal_terbayar,
    total_piutang_iuran
FROM v_rekap_iuran_bulanan
WHERE koperasi_id = $1
  AND periode BETWEEN $2 AND $3
ORDER BY periode DESC;

-- name: GetHargaAkhirAnggotaDenganPromoTerbaik :one
WITH status_kelayakan AS (
    SELECT
        a.id AS anggota_id,
        (a.status = 'aktif' AND (
            SELECT COUNT(*)
            FROM tagihan_iuran ti
            WHERE ti.koperasi_id = a.koperasi_id
              AND ti.anggota_id = a.id
              AND ti.status = 'menunggak'
              AND ti.deleted_at IS NULL
        ) <= COALESCE((
            SELECT (ka.nilai_json->>'maks_bulan_menunggak_diskon')::int
            FROM konfigurasi_aturan ka
            WHERE (ka.koperasi_id = a.koperasi_id OR ka.koperasi_id IS NULL)
              AND ka.kunci_aturan = 'kebijakan_diskon_anggota'
              AND ka.status_aktif = true
            ORDER BY ka.koperasi_id NULLS LAST, ka.versi DESC
            LIMIT 1
        ), 2)) AS berhak_diskon
    FROM anggota a
    WHERE a.id = $3 AND a.koperasi_id = $1 AND a.deleted_at IS NULL
),
harga_aktif AS (
    SELECT
        ph.id AS produk_harga_id,
        p.id AS produk_id,
        p.kategori_id,
        ph.harga_umum,
        ph.harga_anggota
    FROM produk p
    JOIN produk_harga ph ON ph.produk_id = p.id AND ph.koperasi_id = p.koperasi_id
    WHERE p.koperasi_id = $1
      AND p.id = $2
      AND ph.status_aktif = true
      AND ph.berlaku_mulai <= NOW()
      AND (ph.berlaku_sampai IS NULL OR ph.berlaku_sampai > NOW())
    ORDER BY ph.versi DESC
    LIMIT 1
),
kandidat_promo AS (
    SELECT
        pr.id AS promo_id,
        pr.kode AS kode_promo,
        pr.prioritas,
        LEAST(
            CASE
                WHEN pr.tipe_promo = 'persen' THEN ROUND((ha.harga_anggota * pr.nilai_diskon / 100.0), 2)
                ELSE pr.nilai_diskon
            END,
            COALESCE(pr.maksimal_potongan, ha.harga_anggota)
        )::numeric(18,2) AS potongan_satuan
    FROM harga_aktif ha
    JOIN status_kelayakan sk ON sk.berhak_diskon = true
    JOIN promo pr ON pr.koperasi_id = $1
    WHERE pr.status_aktif = true
      AND pr.deleted_at IS NULL
      AND NOW() BETWEEN pr.berlaku_mulai AND pr.berlaku_sampai
      AND (pr.kuota_total IS NULL OR pr.kuota_terpakai < pr.kuota_total)
      AND (
          pr.cakupan = 'seluruh_katalog'
          OR (pr.cakupan = 'produk' AND EXISTS (SELECT 1 FROM promo_produk pp WHERE pp.promo_id = pr.id AND pp.produk_id = ha.produk_id))
          OR (pr.cakupan = 'kategori' AND EXISTS (SELECT 1 FROM promo_kategori pk WHERE pk.promo_id = pr.id AND pk.kategori_id = ha.kategori_id))
      )
      AND (
          SELECT COUNT(*)
          FROM promo_penggunaan pu
          WHERE pu.promo_id = pr.id
            AND pu.anggota_id = sk.anggota_id
            AND pu.status IN ('dipesan', 'terpakai')
      ) < pr.batas_per_anggota
    ORDER BY potongan_satuan DESC, pr.prioritas DESC
    LIMIT 1
)
SELECT
    ha.produk_id,
    ha.produk_harga_id,
    ha.harga_umum,
    ha.harga_anggota,
    COALESCE(sk.berhak_diskon, false) AS berhak_harga_anggota,
    CASE WHEN COALESCE(sk.berhak_diskon, false) THEN ha.harga_anggota ELSE ha.harga_umum END AS harga_dasar_dipakai,
    kp.promo_id,
    kp.kode_promo,
    COALESCE(kp.potongan_satuan, 0.00)::numeric(18,2) AS diskon_promo_satuan,
    (CASE WHEN COALESCE(sk.berhak_diskon, false) THEN ha.harga_anggota ELSE ha.harga_umum END - COALESCE(kp.potongan_satuan, 0.00))::numeric(18,2) AS harga_akhir_satuan
FROM harga_aktif ha
LEFT JOIN status_kelayakan sk ON true
LEFT JOIN kandidat_promo kp ON true;

-- name: ListStokProdukGudang :many
SELECT
    ss.gudang_id,
    g.nama AS nama_gudang,
    p.id AS produk_id,
    p.sku,
    p.nama AS nama_produk,
    pv.id AS varian_id,
    pv.sku_varian,
    pv.nama_varian,
    ss.qty_tersedia,
    ss.qty_dipesan,
    (ss.qty_tersedia - ss.qty_dipesan)::numeric(18,2) AS qty_bebas_jual,
    ss.updated_at
FROM stok_saldo ss
JOIN gudang g ON g.id = ss.gudang_id
JOIN produk p ON p.id = ss.produk_id
LEFT JOIN produk_varian pv ON pv.id = ss.varian_id
WHERE ss.koperasi_id = $1
  AND p.deleted_at IS NULL
ORDER BY g.nama ASC, p.nama ASC;

-- name: GetArusKasBulanan :many
SELECT
    koperasi_id,
    periode,
    kategori,
    jenis,
    jumlah_transaksi,
    total_nominal
FROM v_arus_kas_bulanan
WHERE koperasi_id = $1
  AND periode = $2
ORDER BY jenis ASC, kategori ASC;

-- name: BuildPayloadLaporanSIMKOPDES :one
SELECT
    k.id AS koperasi_id,
    k.kode_koperasi,
    k.nama AS nama_koperasi,
    w.kode_bps AS kode_wilayah_bps,
    w.kode_kemendagri,
    kl.no_badan_hukum,
    kl.no_sk_ahu,
    kl.nik_koperasi,
    kl.nib,
    jsonb_build_object(
        'periode', $2::text,
        'anggota_aktif_total', (SELECT COUNT(*) FROM anggota a WHERE a.koperasi_id = k.id AND a.status = 'aktif' AND a.deleted_at IS NULL),
        'anggota_aktif_perempuan', (SELECT COUNT(*) FROM anggota a WHERE a.koperasi_id = k.id AND a.status = 'aktif' AND a.jenis_kelamin = 'P' AND a.deleted_at IS NULL),
        'anggota_aktif_laki', (SELECT COUNT(*) FROM anggota a WHERE a.koperasi_id = k.id AND a.status = 'aktif' AND a.jenis_kelamin = 'L' AND a.deleted_at IS NULL),
        'total_iuran_terkumpul', COALESCE((SELECT total_nominal_terbayar FROM v_rekap_iuran_bulanan vr WHERE vr.koperasi_id = k.id AND vr.periode = $2), 0),
        'total_penjualan_katalog', COALESCE((SELECT total_penjualan_bersih FROM v_rekap_penjualan_bulanan vp WHERE vp.koperasi_id = k.id AND vp.periode = $2), 0),
        'jumlah_unit_usaha_aktif', (SELECT COUNT(*) FROM unit_usaha uu WHERE uu.koperasi_id = k.id AND uu.status_aktif = true AND uu.deleted_at IS NULL)
    ) AS payload_simkopdes
FROM koperasi k
JOIN wilayah w ON w.id = k.wilayah_desa_id
LEFT JOIN koperasi_legalitas kl ON kl.koperasi_id = k.id AND kl.status_aktif = true AND kl.deleted_at IS NULL
WHERE k.id = $1
  AND k.deleted_at IS NULL
LIMIT 1;