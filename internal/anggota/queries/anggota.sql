-- name: CountTotalAnggota :one
SELECT COUNT(*)::int FROM anggota;

-- name: CreateAnggota :one
INSERT INTO anggota (
    user_id, nomor_anggota, nama_lengkap, nik_enc, nik_hash, nik_key_version, nik_masked,
    hp_enc, hp_masked, tanggal_lahir_enc, alamat_enc, status
) VALUES (
    sqlc.narg(user_id), sqlc.arg(nomor_anggota), sqlc.arg(nama_lengkap),
    sqlc.arg(nik_enc), sqlc.arg(nik_hash), sqlc.arg(nik_key_version), sqlc.arg(nik_masked),
    sqlc.arg(hp_enc), sqlc.arg(hp_masked), sqlc.arg(tanggal_lahir_enc), sqlc.arg(alamat_enc),
    sqlc.arg(status)
)
RETURNING id, user_id, nomor_anggota, nama_lengkap, nik_masked, hp_masked, status, tanggal_bergabung, created_at;

-- name: GetAnggotaByNIKHash :one
SELECT id, user_id, nomor_anggota, nama_lengkap, nik_masked, hp_masked, status, tanggal_bergabung, created_at
FROM anggota
WHERE nik_hash = sqlc.arg(nik_hash);

-- name: GetAnggotaByID :one
SELECT a.id, a.user_id, a.nomor_anggota, a.nama_lengkap, a.nik_enc, a.nik_key_version, a.nik_masked,
       a.hp_enc, a.hp_masked, a.tanggal_lahir_enc, a.alamat_enc, a.status, a.alasan_status,
       a.tanggal_bergabung, a.created_at, a.updated_at,
       COALESCE(u.peran, 'anggota')::text AS peran,
       u.email_enc AS user_email_enc
FROM anggota a
LEFT JOIN users u ON u.id = a.user_id
WHERE a.id = sqlc.arg(id);

-- name: GetAnggotaByUserID :one
SELECT id, user_id, nomor_anggota, nama_lengkap, nik_masked, hp_masked, status, tanggal_bergabung
FROM anggota
WHERE user_id = sqlc.arg(user_id);

-- name: ListAnggota :many
SELECT id, user_id, nomor_anggota, nama_lengkap, nik_masked, hp_masked, status, tanggal_bergabung, created_at
FROM anggota
WHERE (sqlc.arg(status_filter)::text = '' OR status = sqlc.arg(status_filter)::text)
ORDER BY created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: UpdateAnggotaStatus :one
UPDATE anggota
SET status = sqlc.arg(status),
    alasan_status = sqlc.arg(alasan_status)
WHERE id = sqlc.arg(id)
RETURNING id, user_id, nomor_anggota, nama_lengkap, status, alasan_status, updated_at;

-- name: DeactivateUserByID :exec
UPDATE users
SET status_aktif = sqlc.arg(status_aktif)
WHERE id = sqlc.arg(id);
