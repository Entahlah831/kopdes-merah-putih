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
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresIn   int64     `json:"expires_in"` // detik
	UserID      uuid.UUID `json:"user_id"`
	KoperasiID  uuid.UUID `json:"koperasi_id"`
	NamaLengkap string    `json:"nama_lengkap"`
	Role        string    `json:"role"`
}

// HandleLogin memverifikasi kredensial email & password dan menerbitkan JWT
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

	// Cari akun pengguna menggunakan fungsi SECURITY DEFINER sempit tanpa membypass platform admin
	err := h.pool.QueryRow(ctx, `
		SELECT user_id, password_hash, nama_lengkap, is_super_admin, status_aktif,
		       koperasi_id, role_name
		FROM fn_lookup_user_login($1);
	`, emailHash).Scan(
		&userID, &passwordHash, &namaLengkap, &isSuperAdmin, &statusAktif,
		&koperasiID, &roleName,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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

	ttl := 24 * time.Hour
	tokenString, err := h.tokenManager.GenerateToken(userID, targetKoperasiID, roleName, isSuperAdmin, ttl)
	if err != nil {
		response.InternalServerError(w, "Gagal membuat token otorisasi")
		return
	}

	response.JSON(w, http.StatusOK, LoginResponse{
		AccessToken: tokenString,
		TokenType:   "Bearer",
		ExpiresIn:   int64(ttl.Seconds()),
		UserID:      userID,
		KoperasiID:  targetKoperasiID,
		NamaLengkap: namaLengkap,
		Role:        roleName,
	})
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
