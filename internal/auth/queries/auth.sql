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

-- name: GetPublicSettingsWithQRIS :one
SELECT p.nama_koperasi,
       p.alamat_koperasi,
       p.telepon_koperasi,
       p.qris_unique_digits,
       p.ongkir_tetap,
       p.kebijakan_teks,
       COALESCE(r.id::text, '')::text AS qris_rekening_id,
       COALESCE(r.nama_merchant, '')::text AS qris_merchant,
       COALESCE(r.nmid, '')::text AS qris_nmid,
       COALESCE(r.nama_bank, '')::text AS qris_bank,
       COALESCE(r.nomor_rekening, '')::text AS qris_nomor_rekening,
       COALESCE(r.jenis_pemilik, '')::text AS qris_jenis_pemilik,
       COALESCE(r.qris_image_url, '')::text AS qris_image_url
FROM pengaturan p
LEFT JOIN rekening_qris r ON r.id = p.active_rekening_qris_id AND r.aktif = TRUE
WHERE p.id = 1;

-- name: GetRateLimit :one
SELECT key, kategori, hit_count, window_reset_at, blocked_until, updated_at
FROM rate_limits
WHERE key = sqlc.arg(key);

-- name: UpsertRateLimitHit :one
INSERT INTO rate_limits (key, kategori, hit_count, window_reset_at, blocked_until, updated_at)
VALUES (
    sqlc.arg(key),
    sqlc.arg(kategori),
    1,
    NOW() + make_interval(secs => sqlc.arg(window_secs)::int),
    NULL,
    NOW()
)
ON CONFLICT (key) DO UPDATE
SET hit_count = CASE
        WHEN rate_limits.window_reset_at <= NOW() THEN 1
        ELSE rate_limits.hit_count + 1
    END,
    window_reset_at = CASE
        WHEN rate_limits.window_reset_at <= NOW() THEN NOW() + make_interval(secs => sqlc.arg(window_secs)::int)
        ELSE rate_limits.window_reset_at
    END,
    blocked_until = CASE
        WHEN rate_limits.window_reset_at <= NOW() AND (rate_limits.blocked_until IS NULL OR rate_limits.blocked_until <= NOW()) THEN NULL
        ELSE rate_limits.blocked_until
    END,
    updated_at = NOW()
RETURNING key, kategori, hit_count, window_reset_at, blocked_until, updated_at;

-- name: SetRateLimitBlocked :exec
INSERT INTO rate_limits (key, kategori, hit_count, window_reset_at, blocked_until, updated_at)
VALUES (
    sqlc.arg(key),
    sqlc.arg(kategori),
    1,
    NOW() + make_interval(secs => sqlc.arg(block_secs)::int),
    NOW() + make_interval(secs => sqlc.arg(block_secs)::int),
    NOW()
)
ON CONFLICT (key) DO UPDATE
SET blocked_until = NOW() + make_interval(secs => sqlc.arg(block_secs)::int),
    updated_at = NOW();

-- name: ListActiveRateLimitBlocks :many
SELECT key, kategori, blocked_until
FROM rate_limits
WHERE blocked_until IS NOT NULL AND blocked_until > NOW()
ORDER BY blocked_until DESC
LIMIT 1000;

-- name: CleanupExpiredRateLimits :execrows
DELETE FROM rate_limits
WHERE window_reset_at < NOW()
  AND (blocked_until IS NULL OR blocked_until < NOW());

-- name: GetUserByEmailHash :one
SELECT id, email_enc, email_hash, hp_enc, hp_hash, nama_lengkap, password_hash, peran,
       status_aktif, mfa_enabled, mfa_secret_enc, mfa_last_step, mfa_recovery_hashes,
       failed_login_count, locked_until, last_failed_at, created_at, updated_at
FROM users
WHERE email_hash = sqlc.arg(email_hash);

-- name: GetUserByHPHash :one
SELECT id, email_enc, email_hash, hp_enc, hp_hash, nama_lengkap, password_hash, peran,
       status_aktif, mfa_enabled, mfa_secret_enc, mfa_last_step, mfa_recovery_hashes,
       failed_login_count, locked_until, last_failed_at, created_at, updated_at
FROM users
WHERE hp_hash = sqlc.arg(hp_hash);

-- name: GetUserByID :one
SELECT id, email_enc, email_hash, hp_enc, hp_hash, nama_lengkap, password_hash, peran,
       status_aktif, mfa_enabled, mfa_secret_enc, mfa_last_step, mfa_recovery_hashes,
       failed_login_count, locked_until, last_failed_at, created_at, updated_at
FROM users
WHERE id = sqlc.arg(id);

-- name: CreateUser :one
INSERT INTO users (
    id, email_enc, email_hash, hp_enc, hp_hash, nama_lengkap, password_hash, peran, status_aktif
) VALUES (
    COALESCE(sqlc.narg(id), uuid_generate_v7()),
    sqlc.narg(email_enc), sqlc.narg(email_hash), sqlc.narg(hp_enc), sqlc.narg(hp_hash),
    sqlc.arg(nama_lengkap), sqlc.arg(password_hash), sqlc.arg(peran), sqlc.arg(status_aktif)
)
RETURNING id, email_enc, email_hash, hp_enc, hp_hash, nama_lengkap, peran, status_aktif, mfa_enabled, created_at;

-- name: IncrementFailedLogin :one
UPDATE users
SET failed_login_count = failed_login_count + 1,
    last_failed_at = NOW(),
    locked_until = CASE
        WHEN failed_login_count + 1 >= sqlc.arg(max_attempts)::int
        THEN NOW() + make_interval(secs => sqlc.arg(lockout_secs)::int)
        ELSE locked_until
    END
WHERE id = sqlc.arg(id)
RETURNING failed_login_count, locked_until;

-- name: ResetFailedLogin :exec
UPDATE users
SET failed_login_count = 0, locked_until = NULL
WHERE id = sqlc.arg(id);

-- name: UpdateUserSandiHash :exec
UPDATE users
SET password_hash = sqlc.arg(password_hash),
    failed_login_count = 0,
    locked_until = NULL
WHERE id = sqlc.arg(id);

-- name: EnableUserMFA :exec
UPDATE users
SET mfa_enabled = TRUE,
    mfa_secret_enc = sqlc.arg(mfa_secret_enc),
    mfa_recovery_hashes = sqlc.arg(mfa_recovery_hashes),
    mfa_last_step = sqlc.arg(mfa_last_step)
WHERE id = sqlc.arg(id);

-- name: UpdateUserMFALastStep :one
UPDATE users
SET mfa_last_step = sqlc.arg(mfa_last_step)
WHERE id = sqlc.arg(id) AND mfa_last_step < sqlc.arg(mfa_last_step)
RETURNING mfa_last_step;

-- name: UpdateUserRecoveryHashes :exec
UPDATE users
SET mfa_recovery_hashes = sqlc.arg(mfa_recovery_hashes)
WHERE id = sqlc.arg(id);

-- name: ResetUserMFAByID :exec
UPDATE users
SET mfa_enabled = FALSE,
    mfa_secret_enc = NULL,
    mfa_recovery_hashes = '{}',
    mfa_last_step = 0,
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: UpdateUserByAdmin :one
UPDATE users
SET peran = COALESCE(NULLIF(sqlc.arg(peran)::text, ''), peran),
    status_aktif = sqlc.arg(status_aktif),
    mfa_enabled = CASE WHEN sqlc.arg(reset_mfa)::bool THEN FALSE ELSE mfa_enabled END,
    mfa_secret_enc = CASE WHEN sqlc.arg(reset_mfa)::bool THEN NULL ELSE mfa_secret_enc END,
    mfa_recovery_hashes = CASE WHEN sqlc.arg(reset_mfa)::bool THEN '{}' ELSE mfa_recovery_hashes END,
    mfa_last_step = CASE WHEN sqlc.arg(reset_mfa)::bool THEN 0 ELSE mfa_last_step END
WHERE id = sqlc.arg(id)
RETURNING id, nama_lengkap, peran, status_aktif, mfa_enabled, created_at, updated_at;

-- name: ListUsers :many
SELECT u.id, u.email_enc, u.hp_enc, u.nama_lengkap, u.peran, u.status_aktif, u.mfa_enabled,
       COALESCE(a.id::text, '')::text AS anggota_id,
       COALESCE(a.nomor_anggota, '')::text AS nomor_anggota,
       u.created_at
FROM users u
LEFT JOIN anggota a ON a.user_id = u.id
ORDER BY u.created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: CreateSession :one
INSERT INTO sesi (
    user_id, token_hash, mfa_verified, scope, ip_address, user_agent, idle_expires_at, abs_expires_at
) VALUES (
    sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.arg(mfa_verified), sqlc.arg(scope),
    sqlc.arg(ip_address), sqlc.arg(user_agent), sqlc.arg(idle_expires_at), sqlc.arg(abs_expires_at)
)
RETURNING id, user_id, mfa_verified, scope, idle_expires_at, abs_expires_at, created_at;

-- name: GetActiveSessionByDigest :one
SELECT s.id AS session_id,
       s.user_id,
       s.mfa_verified,
       s.scope,
       s.mfa_attempts,
       s.last_active_at,
       s.idle_expires_at,
       s.abs_expires_at,
       u.peran,
       u.status_aktif,
       u.mfa_enabled,
       a.id AS anggota_id,
       COALESCE(a.status, '')::text AS anggota_status
FROM sesi s
JOIN users u ON u.id = s.user_id
LEFT JOIN anggota a ON a.user_id = u.id
WHERE s.token_hash = sqlc.arg(token_hash)
  AND s.revoked_at IS NULL
  AND s.idle_expires_at > NOW()
  AND s.abs_expires_at > NOW();

-- name: TouchSessionActivity :exec
UPDATE sesi
SET last_active_at = NOW(),
    idle_expires_at = LEAST(abs_expires_at, NOW() + make_interval(secs => sqlc.arg(idle_secs)::int))
WHERE id = sqlc.arg(id)
  AND revoked_at IS NULL
  AND (last_active_at IS NULL OR last_active_at <= NOW() - INTERVAL '60 seconds');

-- name: IncrementSessionMFAAttempts :one
UPDATE sesi
SET mfa_attempts = mfa_attempts + 1,
    revoked_at = CASE WHEN mfa_attempts + 1 >= sqlc.arg(max_attempts)::int THEN NOW() ELSE revoked_at END
WHERE id = sqlc.arg(id) AND revoked_at IS NULL
RETURNING mfa_attempts, revoked_at;

-- name: UpgradeSessionAfterMFA :exec
UPDATE sesi
SET mfa_verified = TRUE,
    scope = 'full',
    mfa_attempts = 0,
    last_active_at = NOW()
WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeSessionByID :exec
UPDATE sesi
SET revoked_at = NOW()
WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :exec
UPDATE sesi
SET revoked_at = NOW()
WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL;

-- name: CreateActivationCode :one
INSERT INTO kode_aktivasi (user_id, kode_hash, jenis, expires_at, created_by)
VALUES (sqlc.arg(user_id), sqlc.arg(kode_hash), sqlc.arg(jenis), sqlc.arg(expires_at), sqlc.narg(created_by))
RETURNING id, user_id, jenis, expires_at, created_at;

-- name: GetValidActivationCodeByHashForUpdate :one
SELECT id, user_id, jenis, expires_at, used_at
FROM kode_aktivasi
WHERE kode_hash = sqlc.arg(kode_hash)
  AND used_at IS NULL
  AND expires_at > NOW()
FOR UPDATE;

-- name: ConsumeActivationCode :exec
UPDATE kode_aktivasi
SET used_at = NOW()
WHERE id = sqlc.arg(id);

-- name: InvalidateUserActivationCodes :exec
UPDATE kode_aktivasi
SET used_at = NOW()
WHERE user_id = sqlc.arg(user_id) AND used_at IS NULL;

-- name: InsertAuditLog :exec
INSERT INTO audit_log (actor_user_id, actor_peran, aksi, entitas, entitas_id, ringkasan, ip_address)
VALUES (
    sqlc.narg(actor_user_id), sqlc.arg(actor_peran), sqlc.arg(aksi),
    sqlc.arg(entitas), sqlc.arg(entitas_id), sqlc.arg(ringkasan), sqlc.arg(ip_address)
);
