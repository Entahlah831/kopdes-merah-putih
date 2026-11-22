-- name: CreateJenisIuran :one
INSERT INTO jenis_iuran (kode, nama, nominal, hari_jatuh_tempo, klasifikasi_akuntansi, aktif)
VALUES (sqlc.arg(kode), sqlc.arg(nama), sqlc.arg(nominal), sqlc.arg(hari_jatuh_tempo), sqlc.arg(klasifikasi_akuntansi), TRUE)
RETURNING id, kode, nama, nominal, hari_jatuh_tempo, klasifikasi_akuntansi, aktif, created_at;

-- name: ListJenisIuranAktif :many
SELECT id, kode, nama, nominal, hari_jatuh_tempo, klasifikasi_akuntansi, aktif, created_at
FROM jenis_iuran
WHERE aktif = TRUE
ORDER BY kode ASC;

-- name: GetJenisIuranByID :one
SELECT id, kode, nama, nominal, hari_jatuh_tempo, klasifikasi_akuntansi, aktif, created_at
FROM jenis_iuran
WHERE id = sqlc.arg(id);

-- name: InsertTagihanIuranIfMissing :one
INSERT INTO tagihan_iuran (
    anggota_id, jenis_iuran_id, periode_tahun, periode_bulan, nominal, tanggal_jatuh_tempo
) VALUES (
    sqlc.arg(anggota_id), sqlc.arg(jenis_iuran_id), sqlc.arg(periode_tahun),
    sqlc.arg(periode_bulan), sqlc.arg(nominal), sqlc.arg(tanggal_jatuh_tempo)
)
ON CONFLICT (anggota_id, jenis_iuran_id, periode_tahun, periode_bulan) DO NOTHING
RETURNING id;

-- name: ListTagihanIuran :many
SELECT t.id, t.anggota_id,
       t.jenis_iuran_id, j.kode AS jenis_kode, j.nama AS jenis_nama,
       t.periode_tahun, t.periode_bulan, t.nominal, t.nominal_keringanan,
       (t.nominal - t.nominal_keringanan)::bigint AS nominal_netto,
       t.tanggal_jatuh_tempo, t.status, t.alasan_keringanan, t.paid_at, t.created_at
FROM tagihan_iuran t
JOIN jenis_iuran j ON j.id = t.jenis_iuran_id
WHERE (sqlc.narg(anggota_filter)::uuid IS NULL OR t.anggota_id = sqlc.narg(anggota_filter)::uuid)
  AND (sqlc.arg(only_tunggakan)::bool = FALSE OR (t.status = 'belum_bayar' AND t.tanggal_jatuh_tempo < CURRENT_DATE))
  AND (sqlc.arg(status_filter)::text = '' OR t.status = sqlc.arg(status_filter)::text)
ORDER BY t.periode_tahun DESC, t.periode_bulan DESC, t.created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: GetTagihanIuranForUpdate :one
SELECT id, anggota_id, jenis_iuran_id, periode_tahun, periode_bulan, nominal, nominal_keringanan, status
FROM tagihan_iuran
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ApplyTagihanKeringanan :one
UPDATE tagihan_iuran
SET nominal_keringanan = sqlc.arg(nominal_keringanan),
    alasan_keringanan = sqlc.arg(alasan_keringanan),
    disetujui_oleh = sqlc.arg(disetujui_oleh),
    status = CASE
        WHEN sqlc.arg(nominal_keringanan)::bigint >= nominal THEN 'dibebaskan'
        ELSE status
    END
WHERE id = sqlc.arg(id)
  AND status = 'belum_bayar'
RETURNING id, nominal, nominal_keringanan, status, alasan_keringanan, updated_at;

-- name: UpdateTagihanStatus :exec
UPDATE tagihan_iuran
SET status = sqlc.arg(status),
    paid_at = CASE WHEN sqlc.arg(status) = 'lunas' THEN NOW() ELSE paid_at END
WHERE id = sqlc.arg(id);

-- name: LaporanTunggakanIuran :many
SELECT t.anggota_id,
       j.kode AS jenis_kode,
       j.nama AS jenis_nama,
       t.periode_tahun,
       t.periode_bulan,
       (t.nominal - t.nominal_keringanan)::bigint AS nominal_terutang,
       t.tanggal_jatuh_tempo
FROM tagihan_iuran t
JOIN jenis_iuran j ON j.id = t.jenis_iuran_id
WHERE t.status = 'belum_bayar'
  AND t.tanggal_jatuh_tempo < CURRENT_DATE
ORDER BY t.tanggal_jatuh_tempo ASC, t.anggota_id ASC;
