package handlers

import (
	"encoding/json"
	"errors"
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
}

func NewAuthHandler(pool *pgxpool.Pool, tm *auth.TokenManager, blindIndexSecret []byte) *AuthHandler {
	return &AuthHandler{
		pool:             pool,
		tokenManager:     tm,
		blindIndexSecret: blindIndexSecret,
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

	var (
		userID          uuid.UUID
		passwordHash    string
		namaLengkap     string
		isSuperAdmin    bool
		statusAktif     bool
		koperasiID      uuid.NullUUID
		roleName        string
	)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		response.InternalServerError(w, "Gagal memulai sesi autentikasi")
		return
	}
	defer tx.Rollback(ctx)

	// Set app.is_platform_admin sementara agar login handler dapat membaca tabel user_roles lintas tenant
	if _, err := tx.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'true', true)"); err != nil {
		response.InternalServerError(w, "Gagal mengonfigurasi sesi autentikasi")
		return
	}

	// Cari akun pengguna berdasarkan email_hash
	query := `
		SELECT u.id, u.password_hash, u.nama_lengkap, u.is_super_admin, u.status_aktif,
		       ur.koperasi_id, COALESCE(r.nama, 'anggota') as role_name
		FROM users u
		LEFT JOIN user_roles ur ON ur.user_id = u.id AND ur.deleted_at IS NULL
		LEFT JOIN roles r ON r.id = ur.role_id
		WHERE u.email_hash = $1 AND u.deleted_at IS NULL
		LIMIT 1;
	`
	err = tx.QueryRow(ctx, query, emailHash).Scan(
		&userID, &passwordHash, &namaLengkap, &isSuperAdmin, &statusAktif,
		&koperasiID, &roleName,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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
		response.Unauthorized(w, "Kombinasi email atau password salah")
		return
	}

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

	err := h.pool.QueryRow(ctx, "SELECT nama_lengkap, created_at FROM users WHERE id = $1", claims.UserID).Scan(&namaLengkap, &createdAt)
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
