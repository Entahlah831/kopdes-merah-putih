-- name: CreateRekeningQRIS :one
INSERT INTO rekening_qris (
    nama_merchant, nmid, nama_bank, nomor_rekening, nama_pemilik,
    jenis_pemilik, qris_image_url, qris_payload, aktif
) VALUES (
    sqlc.arg(nama_merchant), sqlc.arg(nmid), sqlc.arg(nama_bank), sqlc.arg(nomor_rekening),
    sqlc.arg(nama_pemilik), sqlc.arg(jenis_pemilik), sqlc.arg(qris_image_url),
    sqlc.arg(qris_payload), TRUE
)
RETURNING id, nama_merchant, nmid, nama_bank, nomor_rekening, nama_pemilik, jenis_pemilik, qris_image_url, aktif, created_at;

-- name: GetRekeningQRISByID :one
SELECT id, nama_merchant, nmid, nama_bank, nomor_rekening, nama_pemilik,
       jenis_pemilik, qris_image_url, qris_payload, aktif, created_at
FROM rekening_qris
WHERE id = sqlc.arg(id);

-- name: ListRekeningQRIS :many
SELECT id, nama_merchant, nmid, nama_bank, nomor_rekening, nama_pemilik,
       jenis_pemilik, qris_image_url, aktif, created_at
FROM rekening_qris
WHERE aktif = TRUE
ORDER BY created_at DESC;

-- name: GetActiveRekeningFromPengaturan :one
SELECT r.id, r.nama_merchant, r.nmid, r.nama_bank, r.nomor_rekening, r.nama_pemilik,
       r.jenis_pemilik, r.qris_image_url, r.qris_payload, r.aktif
FROM pengaturan p
JOIN rekening_qris r ON r.id = p.active_rekening_qris_id
WHERE p.id = 1 AND r.aktif = TRUE;

-- name: GetPembayaranByIdempotencyKey :one
SELECT id, nomor_pembayaran, nomor_kwitansi, pembayar_user_id, anggota_id, rekening_qris_id,
       jenis_transaksi, metode, nominal_pokok, kode_unik, nominal_total, nominal_dibayar,
       selisih_kode_unik, catatan_akuntansi, status, bukti_url, dibuat_oleh, diverifikasi_oleh,
       verified_at, expires_at, created_at
FROM pembayaran
WHERE idempotency_key = sqlc.arg(idempotency_key);

-- name: GetPembayaranByBuktiHash :one
SELECT id, nomor_pembayaran FROM pembayaran WHERE bukti_hash = sqlc.arg(bukti_hash);

-- name: CountActivePendingQRISByUser :one
SELECT COUNT(*)::int
FROM pembayaran
WHERE pembayar_user_id = sqlc.arg(user_id)
  AND metode = 'qris'
  AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi');

-- name: CountActivePendingQRISGlobal :one
SELECT COUNT(*)::int
FROM pembayaran
WHERE metode = 'qris'
  AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi');

-- name: ListUsedActiveKodeUnik :many
SELECT kode_unik
FROM pembayaran
WHERE rekening_qris_id = sqlc.arg(rekening_qris_id)
  AND metode = 'qris'
  AND nominal_pokok = sqlc.arg(nominal_pokok)
  AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi');

-- name: SumPilotDailyAmountByRekening :one
SELECT COALESCE(SUM(nominal_total), 0)::bigint
FROM pembayaran
WHERE rekening_qris_id = sqlc.arg(rekening_qris_id)
  AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi', 'lunas')
  AND (created_at AT TIME ZONE sqlc.arg(tz)::text)::date = (NOW() AT TIME ZONE sqlc.arg(tz)::text)::date;

-- name: NextNomorKwitansi :one
SELECT nextval('seq_nomor_kwitansi')::bigint;

-- name: CreatePembayaran :one
INSERT INTO pembayaran (
    nomor_pembayaran, nomor_kwitansi, idempotency_key, pembayar_user_id, anggota_id,
    rekening_qris_id, jenis_transaksi, metode, nominal_pokok, kode_unik, nominal_total,
    nominal_dibayar, selisih_kode_unik, catatan_akuntansi, status, bukti_url, bukti_hash,
    catatan_pembayar, dibuat_oleh, diverifikasi_oleh, verified_at, expires_at
) VALUES (
    sqlc.arg(nomor_pembayaran), sqlc.narg(nomor_kwitansi), sqlc.narg(idempotency_key),
    sqlc.arg(pembayar_user_id), sqlc.narg(anggota_id), sqlc.narg(rekening_qris_id),
    sqlc.arg(jenis_transaksi), sqlc.arg(metode), sqlc.arg(nominal_pokok), sqlc.arg(kode_unik),
    sqlc.arg(nominal_total), sqlc.arg(nominal_dibayar), sqlc.arg(selisih_kode_unik),
    sqlc.arg(catatan_akuntansi), sqlc.arg(status), sqlc.arg(bukti_url), sqlc.narg(bukti_hash),
    sqlc.arg(catatan_pembayar), sqlc.arg(dibuat_oleh), sqlc.narg(diverifikasi_oleh),
    sqlc.narg(verified_at), sqlc.narg(expires_at)
)
RETURNING id, nomor_pembayaran, nomor_kwitansi, pembayar_user_id, anggota_id, rekening_qris_id,
          jenis_transaksi, metode, nominal_pokok, kode_unik, nominal_total, nominal_dibayar,
          selisih_kode_unik, catatan_akuntansi, status, bukti_url, dibuat_oleh, diverifikasi_oleh,
          verified_at, expires_at, created_at;

-- name: CreatePembayaranAlokasi :one
INSERT INTO pembayaran_alokasi (pembayaran_id, tagihan_iuran_id, pesanan_id, nominal_alokasi)
VALUES (sqlc.arg(pembayaran_id), sqlc.narg(tagihan_iuran_id), sqlc.narg(pesanan_id), sqlc.arg(nominal_alokasi))
RETURNING id, pembayaran_id, tagihan_iuran_id, pesanan_id, nominal_alokasi;

-- name: GetPembayaranByID :one
SELECT id, nomor_pembayaran, nomor_kwitansi, pembayar_user_id, anggota_id, rekening_qris_id,
       jenis_transaksi, metode, nominal_pokok, kode_unik, nominal_total, nominal_dibayar,
       selisih_kode_unik, catatan_akuntansi, status, bukti_url, bukti_hash, catatan_pembayar,
       alasan_verifikasi, dibuat_oleh, diverifikasi_oleh, verified_at, expires_at, created_at, updated_at
FROM pembayaran
WHERE id = sqlc.arg(id);

-- name: GetPembayaranForUpdate :one
SELECT id, nomor_pembayaran, pembayar_user_id, anggota_id, jenis_transaksi, metode,
       nominal_pokok, kode_unik, nominal_total, status, dibuat_oleh, diverifikasi_oleh, expires_at
FROM pembayaran
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: AttachPembayaranBukti :one
UPDATE pembayaran
SET bukti_url = sqlc.arg(bukti_url),
    bukti_hash = sqlc.arg(bukti_hash),
    catatan_pembayar = sqlc.arg(catatan_pembayar),
    status = 'menunggu_verifikasi'
WHERE id = sqlc.arg(id)
  AND pembayar_user_id = sqlc.arg(pembayar_user_id)
  AND status = 'menunggu_pembayaran'
RETURNING id, nomor_pembayaran, status, bukti_url, updated_at;

-- name: VerifyPembayaranDecision :one
UPDATE pembayaran
SET status = sqlc.arg(status),
    nominal_dibayar = sqlc.arg(nominal_dibayar),
    nomor_kwitansi = COALESCE(nomor_kwitansi, sqlc.narg(nomor_kwitansi)),
    alasan_verifikasi = sqlc.arg(alasan_verifikasi),
    diverifikasi_oleh = sqlc.arg(diverifikasi_oleh),
    verified_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING id, nomor_pembayaran, nomor_kwitansi, status, nominal_dibayar, selisih_kode_unik, diverifikasi_oleh, verified_at;

-- name: ListPembayaran :many
SELECT id, nomor_pembayaran, nomor_kwitansi, pembayar_user_id, anggota_id, rekening_qris_id,
       jenis_transaksi, metode, nominal_pokok, kode_unik, nominal_total, nominal_dibayar,
       selisih_kode_unik, status, bukti_url, dibuat_oleh, diverifikasi_oleh,
       verified_at, expires_at, created_at
FROM pembayaran
WHERE (sqlc.narg(user_filter)::uuid IS NULL OR pembayar_user_id = sqlc.narg(user_filter)::uuid)
  AND (sqlc.arg(status_filter)::text = '' OR status = sqlc.arg(status_filter)::text)
ORDER BY created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: ListAlokasiByPembayaran :many
SELECT id, pembayaran_id, tagihan_iuran_id, pesanan_id, nominal_alokasi
FROM pembayaran_alokasi
WHERE pembayaran_id = sqlc.arg(pembayaran_id);

-- name: ListExpiredPendingPembayaran :many
SELECT id
FROM pembayaran
WHERE metode = 'qris'
  AND status = 'menunggu_pembayaran'
  AND expires_at IS NOT NULL
  AND expires_at < NOW()
FOR UPDATE;

-- name: MarkPembayaranKedaluwarsa :exec
UPDATE pembayaran
SET status = 'kedaluwarsa'
WHERE id = sqlc.arg(id);
