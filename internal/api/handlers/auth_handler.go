package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/auth"
	"github.com/kopdes/merah-putih/internal/security"
)

type AuthHandler struct {
	pool             *pgxpool.Pool
	tokenManager     *auth.TokenManager
	blindIndexSecret []byte
	loginLimiter     *middleware.LoginRateLimiter
	trustedProxies   *middleware.TrustedProxyMatcher
}

func NewAuthHandler(
	pool *pgxpool.Pool,
	tm *auth.TokenManager,
	blindIndexSecret []byte,
	loginLimiter *middleware.LoginRateLimiter,
	trustedProxies *middleware.TrustedProxyMatcher,
) *AuthHandler {
	if loginLimiter == nil {
		loginLimiter = middleware.NewLoginRateLimiter(5, 15*time.Minute, 10000)
	}
	return &AuthHandler{
		pool:             pool,
		tokenManager:     tm,
		blindIndexSecret: blindIndexSecret,
		loginLimiter:     loginLimiter,
		trustedProxies:   trustedProxies,
	}
}

type LoginRequest struct {
	Email      string     `json:"email"`
	Password   string     `json:"password"`
	KoperasiID *uuid.UUID `json:"koperasi_id,omitempty"`
}

type LoginResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int64     `json:"expires_in"` // detik (900 detik / 15 menit)
	UserID       uuid.UUID `json:"user_id"`
	KoperasiID   uuid.UUID `json:"koperasi_id"`
	NamaLengkap  string    `json:"nama_lengkap"`
	Role         string    `json:"role"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type RefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// HandleLogin memverifikasi kredensial email & password dan menerbitkan JWT (15 menit) + Refresh Token
func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.Email == "" || req.Password == "" {
		response.BadRequest(w, "Email dan password wajib diisi")
		return
	}

	ctx := r.Context()
	// Hitung blind-index hash deterministik dari email (UU 27/2022 PDP)
	emailHash := security.HashBlindIndex(req.Email, h.blindIndexSecret)
	accountKey := emailHash
	clientIP := middleware.GetClientIP(r, h.trustedProxies)

	// Periksa rate limiting akun dan IP untuk mencegah brute-force login
	if allowed, retryAfter := h.loginLimiter.CheckAllowed(clientIP, accountKey); !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
		response.Error(w, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "Terlalu banyak percobaan login yang gagal. Akun atau IP Anda dibatasi sementara.")
		return
	}

	var (
		userID          uuid.UUID
		passwordHash    string
		namaLengkap     string
		isSuperAdmin    bool
		statusAktif     bool
		koperasiID      uuid.NullUUID
		roleName        string
	)

	// Cari akun pengguna menggunakan fungsi SECURITY DEFINER dengan pilihan koperasi dan hierarki peran deterministik
	err := h.pool.QueryRow(ctx, `
		SELECT user_id, password_hash, nama_lengkap, is_super_admin, status_aktif,
		       koperasi_id, role_name
		FROM fn_lookup_user_login($1, $2);
	`, emailHash, req.KoperasiID).Scan(
		&userID, &passwordHash, &namaLengkap, &isSuperAdmin, &statusAktif,
		&koperasiID, &roleName,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Waktu konstan: jalankan verifikasi dummy bcrypt cost 12 agar durasi respon identik
			dummyHash := "$2a$12$e8Yy8n6P2ZtGgYpT.R0WSu9BfZoV0AomL4v1k8m7r3Wv6B7mG9dC."
			_ = security.CheckPassword(req.Password, dummyHash)
			h.loginLimiter.RecordFailure(clientIP, accountKey)
			response.Unauthorized(w, "Kombinasi email atau password salah")
			return
		}
		response.InternalServerError(w, "Gagal memproses autentikasi")
		return
	}

	if !statusAktif {
		response.Forbidden(w, "Akun Anda telah dinonaktifkan")
		return
	}

	// Verifikasi kata sandi dengan bcrypt
	if !security.CheckPassword(req.Password, passwordHash) {
		h.loginLimiter.RecordFailure(clientIP, accountKey)
		response.Unauthorized(w, "Kombinasi email atau password salah")
		return
	}

	h.loginLimiter.RecordSuccess(accountKey)

	targetKoperasiID := uuid.Nil
	if koperasiID.Valid {
		targetKoperasiID = koperasiID.UUID
	}

	// Access Token 15 menit sesuai rekomendasi OWASP
	ttl := 15 * time.Minute
	tokenString, err := h.tokenManager.GenerateToken(userID, targetKoperasiID, roleName, isSuperAdmin, ttl)
	if err != nil {
		response.InternalServerError(w, "Gagal membuat token otorisasi")
		return
	}

	// Terbitkan refresh token terotasi (7 hari)
	plainRefresh, refreshHash, err := auth.GenerateRefreshToken()
	if err != nil {
		response.InternalServerError(w, "Gagal membuat refresh token")
		return
	}

	var kopIDParam *uuid.UUID
	if targetKoperasiID != uuid.Nil {
		kopIDParam = &targetKoperasiID
	}

	_, err = h.pool.Exec(ctx, `
		INSERT INTO refresh_tokens (
			user_id, token_hash, koperasi_id, role_name, is_platform_admin, expires_at
		) VALUES ($1, $2, $3, $4, $5, NOW() + INTERVAL '7 days');
	`, userID, refreshHash, kopIDParam, roleName, isSuperAdmin)
	if err != nil {
		response.InternalServerError(w, "Gagal menyimpan refresh token")
		return
	}

	response.JSON(w, http.StatusOK, LoginResponse{
		AccessToken:  tokenString,
		RefreshToken: plainRefresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(ttl.Seconds()),
		UserID:       userID,
		KoperasiID:   targetKoperasiID,
		NamaLengkap:  namaLengkap,
		Role:         roleName,
	})
}

// HandleRefreshToken merotasi refresh token dan menerbitkan access token baru (15m)
func (h *AuthHandler) HandleRefreshToken(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		response.BadRequest(w, "Refresh token wajib disertakan")
		return
	}

	tokenHash := auth.HashRefreshToken(req.RefreshToken)
	ctx := r.Context()

	var (
		tokenID         uuid.UUID
		userID          uuid.UUID
		koperasiID      uuid.NullUUID
		roleName        string
		isPlatformAdmin bool
		expiresAt       time.Time
		revokedAt       *time.Time
	)

	err := h.pool.QueryRow(ctx, `
		SELECT id, user_id, koperasi_id, role_name, is_platform_admin, expires_at, revoked_at
		FROM refresh_tokens
		WHERE token_hash = $1;
	`, tokenHash).Scan(
		&tokenID, &userID, &koperasiID, &roleName, &isPlatformAdmin, &expiresAt, &revokedAt,
	)

	if err != nil {
		response.Unauthorized(w, "Refresh token tidak valid atau telah kedaluwarsa")
		return
	}

	// Deteksi Token Reuse: Jika token yang sudah dicabut digunakan kembali, cabut seluruh sesi user
	if revokedAt != nil {
		_, _ = h.pool.Exec(ctx, "UPDATE refresh_tokens SET revoked_at = NOW() WHERE user_id = $1;", userID)
		response.Unauthorized(w, "Indikasi pelanggaran keamanan: Refresh token telah dicabut sebelumnya. Seluruh sesi dibatalkan.")
		return
	}

	if time.Now().After(expiresAt) {
		response.Unauthorized(w, "Refresh token telah kedaluwarsa")
		return
	}

	// Periksa status keaktifan user
	var statusAktif bool
	err = h.pool.QueryRow(ctx, "SELECT status_aktif FROM users WHERE id = $1 AND deleted_at IS NULL;", userID).Scan(&statusAktif)
	if err != nil || !statusAktif {
		response.Forbidden(w, "Akun pengguna tidak aktif")
		return
	}

	targetKoperasiID := uuid.Nil
	if koperasiID.Valid {
		targetKoperasiID = koperasiID.UUID
	}

	// Buat access token baru (15 menit)
	newAccessTTL := 15 * time.Minute
	newAccessToken, err := h.tokenManager.GenerateToken(userID, targetKoperasiID, roleName, isPlatformAdmin, newAccessTTL)
	if err != nil {
		response.InternalServerError(w, "Gagal memperbarui access token")
		return
	}

	// Buat refresh token baru untuk rotasi
	newPlainRefresh, newRefreshHash, err := auth.GenerateRefreshToken()
	if err != nil {
		response.InternalServerError(w, "Gagal membuat refresh token baru")
		return
	}

	// Transaksi atomik rotasi refresh token
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		response.InternalServerError(w, "Gagal memproses rotasi token")
		return
	}
	defer tx.Rollback(ctx)

	// Cabut token lama dan catat token penggantinya
	_, err = tx.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = NOW(), replaced_by_token_hash = $2
		WHERE id = $1;
	`, tokenID, newRefreshHash)
	if err != nil {
		response.InternalServerError(w, "Gagal mencabut refresh token lama")
		return
	}

	var kopIDParam *uuid.UUID
	if targetKoperasiID != uuid.Nil {
		kopIDParam = &targetKoperasiID
	}

	// Simpan token baru
	_, err = tx.Exec(ctx, `
		INSERT INTO refresh_tokens (
			user_id, token_hash, koperasi_id, role_name, is_platform_admin, expires_at
		) VALUES ($1, $2, $3, $4, $5, NOW() + INTERVAL '7 days');
	`, userID, newRefreshHash, kopIDParam, roleName, isPlatformAdmin)
	if err != nil {
		response.InternalServerError(w, "Gagal menyimpan refresh token rotasi")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		response.InternalServerError(w, "Gagal menerapkan rotasi token")
		return
	}

	response.JSON(w, http.StatusOK, RefreshResponse{
		AccessToken:  newAccessToken,
		RefreshToken: newPlainRefresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(newAccessTTL.Seconds()),
	})
}

// HandleRevokeToken mencabut refresh token tertentu (logout)
func (h *AuthHandler) HandleRevokeToken(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		response.BadRequest(w, "Refresh token wajib disertakan")
		return
	}

	tokenHash := auth.HashRefreshToken(req.RefreshToken)
	ctx := r.Context()

	_, err := h.pool.Exec(ctx, "UPDATE refresh_tokens SET revoked_at = NOW() WHERE token_hash = $1;", tokenHash)
	if err != nil {
		response.InternalServerError(w, "Gagal mencabut token")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Refresh token berhasil dicabut"})
}

// HandleGetProfile mengembalikan profil pengguna yang sedang login
func (h *AuthHandler) HandleGetProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Sesi tidak ditemukan")
		return
	}

	ctx := r.Context()
	var (
		namaLengkap string
		createdAt   time.Time
	)

	var err error
	if claims.KoperasiID != uuid.Nil || claims.IsPlatformAdmin {
		err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT nama_lengkap, created_at FROM users WHERE id = $1", claims.UserID).Scan(&namaLengkap, &createdAt)
		})
	} else {
		err = h.pool.QueryRow(ctx, "SELECT nama_lengkap, created_at FROM users WHERE id = $1", claims.UserID).Scan(&namaLengkap, &createdAt)
	}
	if err != nil {
		response.NotFound(w, "Data pengguna tidak ditemukan")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"user_id":           claims.UserID,
		"koperasi_id":       claims.KoperasiID,
		"nama_lengkap":      namaLengkap,
		"role":              claims.Role,
		"is_platform_admin": claims.IsPlatformAdmin,
		"terdaftar_sejak":   createdAt.Format(time.RFC3339),
	})
}
