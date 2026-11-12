package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/auth"
)

type contextKey string

const (
	ContextKeyUserClaims contextKey = "kopdes_user_claims"
)

type AuthMiddleware struct {
	tokenManager *auth.TokenManager
}

func NewAuthMiddleware(tm *auth.TokenManager) *AuthMiddleware {
	return &AuthMiddleware{tokenManager: tm}
}

// RequireAuth memverifikasi token JWT pada header Authorization: Bearer <token>
func (am *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			response.Unauthorized(w, "Header Authorization diperlukan")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			response.Unauthorized(w, "Format Authorization wajib: Bearer <token>")
			return
		}

		tokenString := parts[1]
		claims, err := am.tokenManager.ValidateToken(tokenString)
		if err != nil {
			response.Unauthorized(w, "Token otentikasi tidak valid atau telah kedaluwarsa")
			return
		}

		// Token setup MFA strictly limited to enrollment endpoints only
		if claims.Role == "mfa_setup" {
			path := r.URL.Path
			if path != "/api/v1/auth/mfa/enable" && path != "/api/v1/auth/mfa/setup" {
				response.Forbidden(w, "Token setup MFA hanya berlaku untuk proses enrollment MFA (akses ditolak)")
				return
			}
		}

		// Simpan claims ke dalam request context
		ctx := context.WithValue(r.Context(), ContextKeyUserClaims, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRoles memastikan pengguna memiliki salah satu role yang diizinkan
func RequireRoles(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool)
	for _, role := range roles {
		allowed[role] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(ContextKeyUserClaims).(*auth.UserClaims)
			if !ok || claims == nil {
				response.Unauthorized(w, "Autentikasi diperlukan")
				return
			}

			if claims.IsPlatformAdmin || allowed[claims.Role] {
				next.ServeHTTP(w, r)
				return
			}

			response.Forbidden(w, "Anda tidak memiliki hak akses (role) untuk fitur ini")
		})
	}
}

// GetUserClaims mengambil informasi klaim autentikasi dari request context
func GetUserClaims(ctx context.Context) (*auth.UserClaims, bool) {
	claims, ok := ctx.Value(ContextKeyUserClaims).(*auth.UserClaims)
	return claims, ok
}

// MustGetKoperasiID mengambil ID tenant koperasi aktif dari context
func MustGetKoperasiID(ctx context.Context) (uuid.UUID, bool) {
	if claims, ok := GetUserClaims(ctx); ok && claims.KoperasiID != uuid.Nil {
		return claims.KoperasiID, true
	}
	return uuid.Nil, false
}

// MustGetUserID mengambil ID pengguna dari context
func MustGetUserID(ctx context.Context) (uuid.UUID, bool) {
	if claims, ok := GetUserClaims(ctx); ok && claims.UserID != uuid.Nil {
		return claims.UserID, true
	}
	return uuid.Nil, false
}
