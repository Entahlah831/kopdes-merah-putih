-- name: CreateKategori :one
INSERT INTO kategori (slug, nama, aktif)
VALUES (sqlc.arg(slug), sqlc.arg(nama), TRUE)
RETURNING id, slug, nama, aktif, created_at;

-- name: ListKategoriAktif :many
SELECT id, slug, nama, aktif, created_at
FROM kategori
WHERE aktif = TRUE
ORDER BY nama ASC;

-- name: CreateProduk :one
INSERT INTO produk (
    kategori_id, sku, nama, deskripsi, satuan, harga_umum, harga_anggota, stok, foto_url, status
) VALUES (
    sqlc.narg(kategori_id), sqlc.arg(sku), sqlc.arg(nama), sqlc.arg(deskripsi),
    sqlc.arg(satuan), sqlc.narg(harga_umum), sqlc.narg(harga_anggota),
    sqlc.arg(stok), sqlc.arg(foto_url), sqlc.arg(status)
)
RETURNING id, kategori_id, sku, nama, deskripsi, satuan, harga_umum, harga_anggota, stok, stok_tertahan, foto_url, status, created_at;

-- name: UpdateProduk :one
UPDATE produk
SET kategori_id = sqlc.narg(kategori_id),
    nama = sqlc.arg(nama),
    deskripsi = sqlc.arg(deskripsi),
    satuan = sqlc.arg(satuan),
    harga_umum = sqlc.narg(harga_umum),
    harga_anggota = sqlc.narg(harga_anggota),
    stok = sqlc.arg(stok),
    foto_url = sqlc.arg(foto_url),
    status = sqlc.arg(status)
WHERE id = sqlc.arg(id)
RETURNING id, kategori_id, sku, nama, deskripsi, satuan, harga_umum, harga_anggota, stok, stok_tertahan, foto_url, status, updated_at;

-- name: GetProdukByID :one
SELECT p.id, p.kategori_id, COALESCE(k.nama, '')::text AS nama_kategori,
       p.sku, p.nama, p.deskripsi, p.satuan, p.harga_umum, p.harga_anggota,
       p.stok, p.stok_tertahan, p.foto_url, p.status, p.created_at, p.updated_at
FROM produk p
LEFT JOIN kategori k ON k.id = p.kategori_id
WHERE p.id = sqlc.arg(id);

-- name: GetProdukForUpdate :one
SELECT id, sku, nama, harga_umum, harga_anggota, stok, stok_tertahan, status
FROM produk
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ListProduk :many
SELECT p.id, p.kategori_id, COALESCE(k.nama, '')::text AS nama_kategori,
       p.sku, p.nama, p.deskripsi, p.satuan, p.harga_umum, p.harga_anggota,
       p.stok, p.stok_tertahan, p.foto_url, p.status, p.created_at
FROM produk p
LEFT JOIN kategori k ON k.id = p.kategori_id
WHERE (sqlc.arg(include_nonaktif)::bool = TRUE OR p.status = 'aktif')
  AND (sqlc.narg(kategori_id)::uuid IS NULL OR p.kategori_id = sqlc.narg(kategori_id)::uuid)
  AND (sqlc.arg(search)::text = '' OR p.nama ILIKE '%' || sqlc.arg(search)::text || '%' OR p.sku ILIKE '%' || sqlc.arg(search)::text || '%')
ORDER BY p.nama ASC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: ReserveProdukStock :one
UPDATE produk
SET stok_tertahan = stok_tertahan + sqlc.arg(qty)::int
WHERE id = sqlc.arg(id)
  AND status = 'aktif'
  AND (stok - stok_tertahan) >= sqlc.arg(qty)::int
RETURNING id, stok, stok_tertahan;

-- name: ReleaseProdukStockHold :exec
UPDATE produk
SET stok_tertahan = GREATEST(0, stok_tertahan - sqlc.arg(qty)::int)
WHERE id = sqlc.arg(id);

-- name: ConfirmReservedProdukStock :one
UPDATE produk
SET stok = stok - sqlc.arg(qty)::int,
    stok_tertahan = stok_tertahan - sqlc.arg(qty)::int
WHERE id = sqlc.arg(id)
  AND stok >= sqlc.arg(qty)::int
  AND stok_tertahan >= sqlc.arg(qty)::int
RETURNING id, stok, stok_tertahan;

-- name: DecrementAvailableProdukStock :one
UPDATE produk
SET stok = stok - sqlc.arg(qty)::int
WHERE id = sqlc.arg(id)
  AND status = 'aktif'
  AND (stok - stok_tertahan) >= sqlc.arg(qty)::int
RETURNING id, stok, stok_tertahan;

-- name: DecrementProdukStock :one
UPDATE produk
SET stok = stok - sqlc.arg(qty)::int
WHERE id = sqlc.arg(id) AND status = 'aktif' AND stok >= sqlc.arg(qty)::int
RETURNING id, stok;

-- name: RestoreProdukStock :exec
UPDATE produk
SET stok = stok + sqlc.arg(qty)::int
WHERE id = sqlc.arg(id);

-- name: CreatePromo :one
INSERT INTO promo (
    kode, nama, tipe_diskon, nilai_diskon, min_belanja, kuota_total,
    sekali_per_anggota, mulai_pada, berakhir_pada, aktif
) VALUES (
    sqlc.arg(kode), sqlc.arg(nama), sqlc.arg(tipe_diskon), sqlc.arg(nilai_diskon),
    sqlc.arg(min_belanja), sqlc.arg(kuota_total), sqlc.arg(sekali_per_anggota),
    sqlc.arg(mulai_pada), sqlc.arg(berakhir_pada), TRUE
)
RETURNING id, kode, nama, tipe_diskon, nilai_diskon, min_belanja, kuota_total, kuota_terpakai, sekali_per_anggota, mulai_pada, berakhir_pada, aktif;

-- name: GetPromoByKodeForUpdate :one
SELECT id, kode, nama, tipe_diskon, nilai_diskon, min_belanja, kuota_total,
       kuota_terpakai, sekali_per_anggota, mulai_pada, berakhir_pada, aktif
FROM promo
WHERE kode = sqlc.arg(kode)
FOR UPDATE;

-- name: ListPromoAktif :many
SELECT id, kode, nama, tipe_diskon, nilai_diskon, min_belanja, kuota_total, kuota_terpakai,
       sekali_per_anggota, mulai_pada, berakhir_pada, aktif
FROM promo
WHERE aktif = TRUE AND berakhir_pada >= NOW()
ORDER BY berakhir_pada ASC;

-- name: IncrementPromoUsed :exec
UPDATE promo
SET kuota_terpakai = kuota_terpakai + 1
WHERE id = sqlc.arg(id) AND (kuota_total = 0 OR kuota_terpakai < kuota_total);

-- name: DecrementPromoUsed :exec
UPDATE promo
SET kuota_terpakai = GREATEST(0, kuota_terpakai - 1)
WHERE id = sqlc.arg(id);
