-- name: CountActivePendaftaranByNIKHash :one
SELECT COUNT(*)::int
FROM pendaftaran
WHERE nik_hash = sqlc.arg(nik_hash)
  AND status IN ('diajukan', 'perlu_perbaikan');

-- name: CreatePendaftaran :one
INSERT INTO pendaftaran (
    nama_lengkap, email_enc, email_hash, nik_enc, nik_hash, nik_key_version, nik_masked,
    hp_enc, hp_hash, hp_masked, tanggal_lahir_enc, alamat_enc, pekerjaan,
    status, upload_token_hash, ip_address
) VALUES (
    sqlc.arg(nama_lengkap), sqlc.narg(email_enc), sqlc.narg(email_hash),
    sqlc.arg(nik_enc), sqlc.arg(nik_hash), sqlc.arg(nik_key_version), sqlc.arg(nik_masked),
    sqlc.arg(hp_enc), sqlc.arg(hp_hash), sqlc.arg(hp_masked),
    sqlc.arg(tanggal_lahir_enc), sqlc.arg(alamat_enc), sqlc.arg(pekerjaan),
    'diajukan', sqlc.arg(upload_token_hash), sqlc.arg(ip_address)
)
RETURNING id, nama_lengkap, nik_masked, hp_masked, pekerjaan, status, created_at;

-- name: GetPendaftaranByID :one
SELECT id, nama_lengkap, email_enc, email_hash, nik_enc, nik_hash, nik_key_version, nik_masked,
       hp_enc, hp_hash, hp_masked, tanggal_lahir_enc, alamat_enc, pekerjaan,
       status, catatan_verifikasi, verified_by, verified_at, rejected_at, anggota_id,
       upload_token_hash, ip_address, created_at, updated_at
FROM pendaftaran
WHERE id = sqlc.arg(id);

-- name: GetPendaftaranByIDForUpdate :one
SELECT id, nama_lengkap, email_enc, email_hash, nik_enc, nik_hash, nik_key_version, nik_masked,
       hp_enc, hp_hash, hp_masked, tanggal_lahir_enc, alamat_enc, pekerjaan,
       status, catatan_verifikasi, verified_by, verified_at, rejected_at, anggota_id
FROM pendaftaran
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ListPendaftaran :many
SELECT id, nama_lengkap, nik_masked, hp_masked, pekerjaan, status, catatan_verifikasi,
       verified_at, anggota_id, created_at
FROM pendaftaran
WHERE (sqlc.arg(status_filter)::text = '' OR status = sqlc.arg(status_filter)::text)
ORDER BY created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: UpdatePendaftaranDecision :one
UPDATE pendaftaran
SET status = sqlc.arg(status),
    catatan_verifikasi = sqlc.arg(catatan_verifikasi),
    verified_by = sqlc.narg(verified_by),
    verified_at = NOW(),
    rejected_at = CASE WHEN sqlc.arg(status) = 'ditolak' THEN NOW() ELSE NULL END,
    anggota_id = COALESCE(sqlc.narg(anggota_id), anggota_id)
WHERE id = sqlc.arg(id)
RETURNING id, status, catatan_verifikasi, verified_at, rejected_at, anggota_id, updated_at;

-- name: ResubmitPendaftaranAfterFix :exec
UPDATE pendaftaran
SET status = 'diajukan'
WHERE id = sqlc.arg(id) AND status = 'perlu_perbaikan';

-- name: UpsertDokumenKYC :one
INSERT INTO dokumen_kyc (
    pendaftaran_id, tipe_dokumen, storage_key, key_id, dek_wrapped,
    mime_type, ukuran_bytes, lebar_px, tinggi_px, sha256_plain
) VALUES (
    sqlc.arg(pendaftaran_id), sqlc.arg(tipe_dokumen), sqlc.arg(storage_key),
    sqlc.arg(key_id), sqlc.arg(dek_wrapped), sqlc.arg(mime_type),
    sqlc.arg(ukuran_bytes), sqlc.arg(lebar_px), sqlc.arg(tinggi_px), sqlc.arg(sha256_plain)
)
ON CONFLICT (pendaftaran_id, tipe_dokumen) DO UPDATE
SET storage_key = EXCLUDED.storage_key,
    key_id = EXCLUDED.key_id,
    dek_wrapped = EXCLUDED.dek_wrapped,
    mime_type = EXCLUDED.mime_type,
    ukuran_bytes = EXCLUDED.ukuran_bytes,
    lebar_px = EXCLUDED.lebar_px,
    tinggi_px = EXCLUDED.tinggi_px,
    sha256_plain = EXCLUDED.sha256_plain,
    created_at = NOW()
RETURNING id, pendaftaran_id, tipe_dokumen, mime_type, ukuran_bytes, lebar_px, tinggi_px, created_at;

-- name: ListDokumenKYCByPendaftaran :many
SELECT id, pendaftaran_id, tipe_dokumen, storage_key, mime_type, ukuran_bytes, lebar_px, tinggi_px, created_at
FROM dokumen_kyc
WHERE pendaftaran_id = sqlc.arg(pendaftaran_id)
ORDER BY tipe_dokumen ASC;

-- name: GetDokumenKYCByPendaftaranAndTipe :one
SELECT id, pendaftaran_id, tipe_dokumen, storage_key, key_id, dek_wrapped,
       mime_type, ukuran_bytes, lebar_px, tinggi_px, sha256_plain, created_at
FROM dokumen_kyc
WHERE pendaftaran_id = sqlc.arg(pendaftaran_id)
  AND tipe_dokumen = sqlc.arg(tipe_dokumen);

-- name: ListExpiredRejectedPendaftaran :many
SELECT id, created_at, rejected_at
FROM pendaftaran
WHERE status = 'ditolak'
  AND rejected_at IS NOT NULL
  AND rejected_at < NOW() - make_interval(days => sqlc.arg(retention_days)::int);

-- name: DeletePendaftaranByID :exec
DELETE FROM pendaftaran
WHERE id = sqlc.arg(id);
