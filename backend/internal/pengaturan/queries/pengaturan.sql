-- name: GetPengaturan :one
SELECT id, nama_koperasi, alamat_koperasi, telepon_koperasi, qris_unique_digits,
       ongkir_tetap, kebijakan_teks, catatan_akuntansi, active_rekening_qris_id, updated_at
FROM pengaturan
WHERE id = 1;

-- name: UpdatePengaturan :one
UPDATE pengaturan
SET nama_koperasi = COALESCE(NULLIF(sqlc.arg(nama_koperasi)::text, ''), nama_koperasi),
    alamat_koperasi = sqlc.arg(alamat_koperasi),
    telepon_koperasi = sqlc.arg(telepon_koperasi),
    qris_unique_digits = sqlc.arg(qris_unique_digits),
    ongkir_tetap = sqlc.arg(ongkir_tetap),
    kebijakan_teks = sqlc.arg(kebijakan_teks),
    active_rekening_qris_id = sqlc.narg(active_rekening_qris_id)
WHERE id = 1
RETURNING id, nama_koperasi, alamat_koperasi, telepon_koperasi, qris_unique_digits,
          ongkir_tetap, kebijakan_teks, catatan_akuntansi, active_rekening_qris_id, updated_at;

-- name: InsertAuditLog :exec
INSERT INTO audit_log (actor_user_id, actor_peran, aksi, entitas, entitas_id, ringkasan, ip_address)
VALUES (
    sqlc.narg(actor_user_id), sqlc.arg(actor_peran), sqlc.arg(aksi),
    sqlc.arg(entitas), sqlc.arg(entitas_id), sqlc.arg(ringkasan), sqlc.arg(ip_address)
);

-- name: ListAuditLog :many
SELECT id, actor_user_id, actor_peran, aksi, entitas, entitas_id, ringkasan, ip_address, created_at
FROM audit_log
WHERE (sqlc.arg(entitas_filter)::text = '' OR entitas = sqlc.arg(entitas_filter)::text)
  AND (sqlc.arg(aksi_filter)::text = '' OR aksi = sqlc.arg(aksi_filter)::text)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);
