package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	keyProvider      security.KeyProvider
}

func NewAuthHandler(
	pool *pgxpool.Pool,
	tm *auth.TokenManager,
	blindIndexSecret []byte,
	loginLimiter *middleware.LoginRateLimiter,
	trustedProxies *middleware.TrustedProxyMatcher,
	keyProvider security.KeyProvider,
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
		keyProvider:      keyProvider,
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

	// Periksa apakah MFA TOTP aktif untuk pengguna ini (Task 7 & Item 3)
	var (
		mfaEnabled      bool
		totpSecretBytes []byte
		lastTotpStep    int64
		recoveryBytes   []byte
	)
	_ = h.pool.QueryRow(ctx, "SELECT mfa_enabled, totp_secret_enkripsi, last_totp_step, mfa_recovery_codes FROM fn_get_user_mfa($1);", userID).Scan(&mfaEnabled, &totpSecretBytes, &lastTotpStep, &recoveryBytes)

	// Peran istimewa (admin, bendahara, ketua, platform admin) WAJIB mengaktifkan MFA (login ditolak sampai MFA aktif)
	isPrivileged := isSuperAdmin || roleName == "admin" || roleName == "bendahara" || roleName == "ketua"

	if isPrivileged && !mfaEnabled {
		setupToken, err := h.tokenManager.GenerateToken(userID, targetKoperasiID, "mfa_setup", false, 15*time.Minute)
		if err != nil {
			response.InternalServerError(w, "Gagal membuat token setup MFA")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error": map[string]interface{}{
				"code":    "MFA_MANDATORY_SETUP_REQUIRED",
				"message": "Pengguna dengan peran istimewa (admin/bendahara/ketua) wajib mengaktifkan MFA TOTP sebelum dapat masuk ke sistem.",
			},
			"data": map[string]interface{}{
				"mfa_required":       true,
				"mfa_setup_required": true,
				"setup_token":        setupToken,
				"user_id":            userID,
				"koperasi_id":        targetKoperasiID,
				"role":               roleName,
			},
		})
		return
	}

	if mfaEnabled {
		sessionBytes := make([]byte, 32)
		if _, err := rand.Read(sessionBytes); err != nil {
			response.InternalServerError(w, "Gagal membuat sesi MFA")
			return
		}
		sessionToken := hex.EncodeToString(sessionBytes)

		var kopIDParam *uuid.UUID
		if targetKoperasiID != uuid.Nil {
			kopIDParam = &targetKoperasiID
		}

		_, err = h.pool.Exec(ctx, `
			INSERT INTO mfa_login_sessions (
				user_id, koperasi_id, role_name, is_super_admin, session_token, expires_at, attempts, is_locked
			) VALUES ($1, $2, $3, $4, $5, NOW() + INTERVAL '5 minutes', 0, false);
		`, userID, kopIDParam, roleName, isSuperAdmin, sessionToken)
		if err != nil {
			response.InternalServerError(w, "Gagal mencatat sesi login MFA")
			return
		}

		response.JSON(w, http.StatusOK, map[string]interface{}{
			"mfa_required":      true,
			"mfa_session_token": sessionToken,
			"user_id":           userID,
			"koperasi_id":       targetKoperasiID,
			"role":              roleName,
			"expires_in":        300,
			"message":           "Verifikasi MFA diperlukan. Masukkan kode 6 digit dari aplikasi autentikator.",
		})
		return
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

	if claims.Role == "mfa_setup" {
		response.Forbidden(w, "Token setup MFA tidak diizinkan mengakses data profil")
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

// HandleMFASetup menghasilkan secret Base32 baru dan QR URI untuk pendaftaran Google Authenticator
func (h *AuthHandler) HandleMFASetup(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	secret, err := security.GenerateTOTPSecret()
	if err != nil {
		response.InternalServerError(w, "Gagal membuat secret TOTP")
		return
	}

	qrURI := security.GenerateTOTPURI(secret, claims.UserID.String(), "Kopdes Merah Putih")

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"secret": secret,
		"qr_uri": qrURI,
	})
}

type MFAEnableRequest struct {
	Secret   string `json:"secret"`
	TOTPCode string `json:"totp_code"`
}

// HandleMFAEnable memvalidasi kode TOTP pertama kali dan menyimpan secret terenkripsi ke database
func (h *AuthHandler) HandleMFAEnable(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	var req MFAEnableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.Secret == "" || req.TOTPCode == "" {
		response.BadRequest(w, "Secret dan kode TOTP wajib disertakan")
		return
	}

	if !security.ValidateTOTPCode(req.Secret, req.TOTPCode, time.Now()) {
		response.BadRequest(w, "Kode TOTP tidak valid atau telah kedaluwarsa")
		return
	}

	if h.keyProvider == nil {
		response.InternalServerError(w, "Enkripsi master key belum dikonfigurasi")
		return
	}

	aad := []byte(fmt.Sprintf("users:totp:%s", claims.UserID))
	encryptedSecret, err := h.keyProvider.EncryptWithAAD([]byte(req.Secret), aad)
	if err != nil {
		response.InternalServerError(w, "Gagal mengenkripsi secret TOTP")
		return
	}

	recoveryCodes, err := security.GenerateRecoveryCodes(8)
	if err != nil {
		response.InternalServerError(w, "Gagal menghasilkan kode pemulihan")
		return
	}

	recoveryHashes := make([]string, len(recoveryCodes))
	for i, c := range recoveryCodes {
		recoveryHashes[i] = security.HashRecoveryCode(c)
	}
	recoveryJSON, _ := json.Marshal(recoveryHashes)
	recoveryAAD := []byte(fmt.Sprintf("users:recovery:%s", claims.UserID))
	encryptedRecovery, err := h.keyProvider.EncryptWithAAD(recoveryJSON, recoveryAAD)
	if err != nil {
		response.InternalServerError(w, "Gagal mengenkripsi kode pemulihan")
		return
	}

	ctx := r.Context()
	_, err = h.pool.Exec(ctx, "SELECT fn_set_user_mfa($1, true, $2, 0, $3);", claims.UserID, encryptedSecret, encryptedRecovery)
	if err != nil {
		response.InternalServerError(w, "Gagal mengaktifkan MFA pengguna")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"message":        "MFA TOTP berhasil diaktifkan",
		"recovery_codes": recoveryCodes,
	})
}

type MFAVerifyRequest struct {
	MFASessionToken string `json:"mfa_session_token"`
	TOTPCode        string `json:"totp_code"`
	RecoveryCode    string `json:"recovery_code"`
}

// HandleMFAVerify memvalidasi kode TOTP / recovery code tahap kedua login dan menerbitkan Access & Refresh Token
func (h *AuthHandler) HandleMFAVerify(w http.ResponseWriter, r *http.Request) {
	var req MFAVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.MFASessionToken == "" || (req.TOTPCode == "" && req.RecoveryCode == "") {
		response.BadRequest(w, "Sesi MFA token dan kode TOTP atau recovery code wajib disertakan")
		return
	}

	ctx := r.Context()
	var (
		sessionID    uuid.UUID
		userID       uuid.UUID
		koperasiID   uuid.NullUUID
		roleName     string
		isSuperAdmin bool
		attempts     int
		isLocked     bool
	)

	err := h.pool.QueryRow(ctx, `
		SELECT id, user_id, koperasi_id, role_name, is_super_admin, attempts, is_locked
		FROM mfa_login_sessions
		WHERE session_token = $1 AND expires_at > NOW();
	`, req.MFASessionToken).Scan(&sessionID, &userID, &koperasiID, &roleName, &isSuperAdmin, &attempts, &isLocked)

	if err != nil {
		response.Unauthorized(w, "Sesi MFA tidak valid atau telah kedaluwarsa")
		return
	}

	if isLocked || attempts >= 3 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error": map[string]interface{}{
				"code":    "MFA_SESSION_LOCKED",
				"message": "Sesi MFA terkunci karena melebihi batas percobaan (maksimal 3 kali). Silakan login ulang.",
			},
		})
		return
	}

	recordFailure := func(reasonCode, reasonMsg string) {
		var newAttempts int
		var lockedNow bool
		_ = h.pool.QueryRow(ctx, `
			UPDATE mfa_login_sessions
			SET attempts = attempts + 1,
			    is_locked = (attempts + 1 >= 3)
			WHERE id = $1
			RETURNING attempts, is_locked;
		`, sessionID).Scan(&newAttempts, &lockedNow)

		if lockedNow || newAttempts >= 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error": map[string]interface{}{
					"code":    "MFA_SESSION_LOCKED",
					"message": "Sesi MFA terkunci karena melebihi batas percobaan (maksimal 3 kali). Silakan login ulang.",
				},
			})
			return
		}

		response.Unauthorized(w, reasonMsg)
	}

	// Ambil secret dan konfigurasi MFA pengguna
	var (
		mfaEnabled      bool
		encryptedSecret []byte
		lastTotpStep    int64
		recoveryBytes   []byte
	)
	err = h.pool.QueryRow(ctx, `
		SELECT mfa_enabled, totp_secret_enkripsi, last_totp_step, mfa_recovery_codes
		FROM fn_get_user_mfa($1);
	`, userID).Scan(&mfaEnabled, &encryptedSecret, &lastTotpStep, &recoveryBytes)
	if err != nil || !mfaEnabled || len(encryptedSecret) == 0 {
		response.Unauthorized(w, "Konfigurasi MFA pengguna tidak ditemukan")
		return
	}

	if h.keyProvider == nil {
		response.InternalServerError(w, "Enkripsi master key belum dikonfigurasi")
		return
	}

	// Tentukan apakah verifikasi via Recovery Code atau TOTP Code
	useRecovery := req.RecoveryCode != "" || (len(strings.TrimSpace(req.TOTPCode)) > 6 && strings.Contains(req.TOTPCode, "-"))
	recoveryInput := req.RecoveryCode
	if recoveryInput == "" && useRecovery {
		recoveryInput = req.TOTPCode
	}

	if useRecovery {
		if len(recoveryBytes) == 0 {
			recordFailure("INVALID_RECOVERY_CODE", "Kode pemulihan tidak valid atau sudah pernah digunakan")
			return
		}

		recAAD := []byte(fmt.Sprintf("users:recovery:%s", userID))
		decryptedJSON, err := h.keyProvider.DecryptWithAAD(recoveryBytes, recAAD)
		if err != nil {
			response.InternalServerError(w, "Gagal memproses kode pemulihan")
			return
		}

		var hashes []string
		if err := json.Unmarshal(decryptedJSON, &hashes); err != nil {
			response.InternalServerError(w, "Gagal membaca format kode pemulihan")
			return
		}

		targetHash := security.HashRecoveryCode(recoveryInput)
		matchedIdx := -1
		for i, hVal := range hashes {
			if hVal == targetHash {
				matchedIdx = i
				break
			}
		}

		if matchedIdx == -1 {
			recordFailure("INVALID_RECOVERY_CODE", "Kode pemulihan tidak valid atau sudah pernah digunakan")
			return
		}

		// Hanguskan kode pemulihan yang sudah digunakan (one-time use / burn)
		remainingHashes := append(hashes[:matchedIdx], hashes[matchedIdx+1:]...)
		newJSON, _ := json.Marshal(remainingHashes)
		newEncrypted, err := h.keyProvider.EncryptWithAAD(newJSON, recAAD)
		if err != nil {
			response.InternalServerError(w, "Gagal memperbarui kode pemulihan")
			return
		}

		_, _ = h.pool.Exec(ctx, "SELECT fn_set_user_mfa($1, true, $2, $3, $4);", userID, encryptedSecret, lastTotpStep, newEncrypted)
	} else {
		// Verifikasi kode TOTP 6-digit
		totpAAD := []byte(fmt.Sprintf("users:totp:%s", userID))
		plainSecret, err := h.keyProvider.DecryptWithAAD(encryptedSecret, totpAAD)
		if err != nil {
			response.InternalServerError(w, "Gagal mendekripsi secret MFA")
			return
		}

		valid, matchedStep := security.ValidateTOTPCodeWithStep(string(plainSecret), req.TOTPCode, time.Now())
		if !valid {
			recordFailure("INVALID_TOTP_CODE", "Kode TOTP tidak valid atau telah kedaluwarsa")
			return
		}

		// Cegah pakai ulang kode TOTP dalam jendela yang sama atau jendela sebelumnya (Anti-Replay)
		if matchedStep <= lastTotpStep {
			recordFailure("TOTP_REPLAY_DETECTED", "Kode TOTP sudah pernah digunakan dalam jendela waktu ini. Tunggu kode berikutnya.")
			return
		}

		// Update last_totp_step agar tidak bisa di-replay di jendela yang sama
		_, _ = h.pool.Exec(ctx, "SELECT fn_set_user_mfa($1, true, $2, $3, $4);", userID, encryptedSecret, matchedStep, recoveryBytes)
	}

	var namaLengkap string
	_ = h.pool.QueryRow(ctx, "SELECT nama_lengkap FROM users WHERE id = $1;", userID).Scan(&namaLengkap)

	// Hapus sesi MFA setelah verifikasi berhasil (one-time use)
	_, _ = h.pool.Exec(ctx, "DELETE FROM mfa_login_sessions WHERE id = $1;", sessionID)

	targetKoperasiID := uuid.Nil
	if koperasiID.Valid {
		targetKoperasiID = koperasiID.UUID
	}

	ttl := 15 * time.Minute
	tokenString, err := h.tokenManager.GenerateToken(userID, targetKoperasiID, roleName, isSuperAdmin, ttl)
	if err != nil {
		response.InternalServerError(w, "Gagal membuat token otorisasi")
		return
	}

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

