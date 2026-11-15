package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/api/response"
)

type contextKey string

const (
	ContextKeySession contextKey = "kopdes_session_claims"
)

// SessionClaims merepresentasikan sesi pengguna terautentikasi di server.
type SessionClaims struct {
	SessionID   uuid.UUID
	UserID      uuid.UUID
	AnggotaID   *uuid.UUID
	Role        string
	Scope       string
	MFAVerified bool
}

// RequireRoles memastikan pengguna memiliki salah satu peran yang diizinkan.
func RequireRoles(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetSessionClaims(r.Context())
			if !ok || claims == nil {
				response.Unauthorized(w, "Autentikasi diperlukan")
				return
			}
			if !allowed[claims.Role] {
				response.Forbidden(w, "Anda tidak memiliki hak akses (peran) untuk fitur ini")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// GetSessionClaims mengambil informasi sesi pengguna dari request context.
func GetSessionClaims(ctx context.Context) (*SessionClaims, bool) {
	claims, ok := ctx.Value(ContextKeySession).(*SessionClaims)
	return claims, ok
}
