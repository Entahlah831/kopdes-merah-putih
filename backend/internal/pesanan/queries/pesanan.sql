-- name: GetPesananByIdempotencyKey :one
SELECT id, nomor_pesanan, user_id, anggota_id, promo_id, metode_pengiriman, metode_pembayaran,
       status, stok_status, stok_hold_expires_at, subtotal, diskon, ongkir, total_bayar,
       nama_penerima, hp_penerima_masked, catatan, tanggal_antar, expires_at, created_at
FROM pesanan
WHERE idempotency_key = sqlc.arg(idempotency_key);

-- name: CountPromoUsageByAnggota :one
SELECT COUNT(*)::int
FROM pesanan
WHERE promo_id = sqlc.arg(promo_id)
  AND (
      (sqlc.narg(anggota_id)::uuid IS NOT NULL AND anggota_id = sqlc.narg(anggota_id)::uuid) OR
      user_id = sqlc.arg(user_id)
  )
  AND status <> 'batal';

-- name: CreatePesanan :one
INSERT INTO pesanan (
    id, nomor_pesanan, user_id, anggota_id, promo_id, idempotency_key,
    metode_pengiriman, metode_pembayaran, status, stok_status, stok_hold_expires_at,
    subtotal, diskon, ongkir, total_bayar,
    nama_penerima, hp_penerima_enc, hp_penerima_masked, alamat_pengantaran_enc,
    catatan, tanggal_antar, expires_at
) VALUES (
    COALESCE(sqlc.narg(id), uuid_generate_v7()),
    sqlc.arg(nomor_pesanan), sqlc.arg(user_id), sqlc.arg(anggota_id), sqlc.arg(promo_id), sqlc.arg(idempotency_key),
    sqlc.arg(metode_pengiriman), sqlc.arg(metode_pembayaran), sqlc.arg(status), sqlc.arg(stok_status), sqlc.narg(stok_hold_expires_at),
    sqlc.arg(subtotal), sqlc.arg(diskon), sqlc.arg(ongkir), sqlc.arg(total_bayar),
    sqlc.arg(nama_penerima), sqlc.arg(hp_penerima_enc), sqlc.arg(hp_penerima_masked), sqlc.arg(alamat_pengantaran_enc),
    sqlc.arg(catatan), sqlc.narg(tanggal_antar), sqlc.narg(expires_at)
)
RETURNING id, nomor_pesanan, user_id, anggota_id, promo_id, metode_pengiriman, metode_pembayaran,
          status, stok_status, stok_hold_expires_at, subtotal, diskon, ongkir, total_bayar,
          nama_penerima, hp_penerima_masked, catatan, tanggal_antar, expires_at, created_at;

-- name: CreatePesananItem :one
INSERT INTO pesanan_item (pesanan_id, produk_id, nama_produk, kuantitas, harga_satuan, subtotal)
VALUES (
    sqlc.arg(pesanan_id), sqlc.arg(produk_id), sqlc.arg(nama_produk),
    sqlc.arg(kuantitas), sqlc.arg(harga_satuan), sqlc.arg(subtotal)
)
RETURNING id, pesanan_id, produk_id, nama_produk, kuantitas, harga_satuan, subtotal;

-- name: GetPesananByID :one
SELECT id, nomor_pesanan, user_id, anggota_id, promo_id, metode_pengiriman, metode_pembayaran,
       status, stok_status, stok_hold_expires_at, subtotal, diskon, ongkir, total_bayar,
       nama_penerima, hp_penerima_enc, hp_penerima_masked, alamat_pengantaran_enc,
       catatan, tanggal_antar, expires_at, created_at, updated_at
FROM pesanan
WHERE id = sqlc.arg(id);

-- name: GetPesananForUpdate :one
SELECT id, nomor_pesanan, user_id, anggota_id, promo_id, metode_pengiriman, metode_pembayaran,
       status, stok_status, stok_hold_expires_at, total_bayar, expires_at
FROM pesanan
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ListPesanan :many
SELECT id, nomor_pesanan, user_id, anggota_id, promo_id, metode_pengiriman, metode_pembayaran,
       status, stok_status, stok_hold_expires_at, subtotal, diskon, ongkir, total_bayar,
       nama_penerima, hp_penerima_masked, tanggal_antar, expires_at, created_at
FROM pesanan
WHERE (sqlc.narg(user_filter)::uuid IS NULL OR user_id = sqlc.narg(user_filter)::uuid)
  AND (sqlc.arg(status_filter)::text = '' OR status = sqlc.arg(status_filter)::text)
ORDER BY created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: ListPesananItems :many
SELECT id, pesanan_id, produk_id, nama_produk, kuantitas, harga_satuan, subtotal
FROM pesanan_item
WHERE pesanan_id = sqlc.arg(pesanan_id)
ORDER BY produk_id ASC, id ASC;

-- name: UpdatePesananStatus :one
UPDATE pesanan
SET status = sqlc.arg(status),
    catatan = COALESCE(NULLIF(sqlc.arg(catatan)::text, ''), catatan)
WHERE id = sqlc.arg(id)
RETURNING id, nomor_pesanan, status, updated_at;

-- name: UpdatePesananStokStatus :exec
UPDATE pesanan
SET stok_status = sqlc.arg(stok_status),
    stok_hold_expires_at = sqlc.narg(stok_hold_expires_at)
WHERE id = sqlc.arg(id);

-- name: ListDaftarAntar :many
SELECT id, nomor_pesanan, status, total_bayar, nama_penerima, hp_penerima_enc,
       hp_penerima_masked, alamat_pengantaran_enc, catatan, tanggal_antar, created_at
FROM pesanan
WHERE metode_pengiriman = 'diantar'
  AND status IN ('dibayar', 'diproses', 'diantar')
  AND (sqlc.narg(tanggal)::date IS NULL OR tanggal_antar = sqlc.narg(tanggal)::date)
ORDER BY created_at ASC;

-- name: ListExpiredUnpaidPesanan :many
SELECT id, promo_id, stok_status
FROM pesanan
WHERE status IN ('baru', 'menunggu_pembayaran')
  AND expires_at IS NOT NULL
  AND expires_at < NOW()
FOR UPDATE;

-- name: ListExpiredStockHoldPesanan :many
SELECT id, status
FROM pesanan
WHERE stok_status = 'reserved'
  AND stok_hold_expires_at IS NOT NULL
  AND stok_hold_expires_at < NOW()
FOR UPDATE;

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
