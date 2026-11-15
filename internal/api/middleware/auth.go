package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

type contextKey string

const ContextKeySession contextKey = "kopdes_session_claims"

const SessionCookieName = "kopdes_session"

// SessionClaims merepresentasikan sesi pengguna terautentikasi di server.
type SessionClaims struct {
	SessionID   uuid.UUID
	UserID      uuid.UUID
	AnggotaID   *uuid.UUID
	Role        string
	Scope       string
	MFAVerified bool
}

// ExtractRawSessionToken mengambil token sesi opaque dari header Bearer atau cookie.
func ExtractRawSessionToken(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "Bearer ") {
		if tok := strings.TrimSpace(authHeader[7:]); tok != "" {
			return tok
		}
	}
	if c, err := r.Cookie(SessionCookieName); err == nil {
		if tok := strings.TrimSpace(c.Value); tok != "" {
			return tok
		}
	}
	return ""
}

func pgUUIDToPtr(u pgtype.UUID) *uuid.UUID {
	if !u.Valid {
		return nil
	}
	val := uuid.UUID(u.Bytes)
	return &val
}

func resolveSessionFromDB(ctx context.Context, pool *pgxpool.Pool, rawToken string, idleTTL time.Duration) (*SessionClaims, bool) {
	if pool == nil || rawToken == "" {
		return nil, false
	}
	q := db.New(pool)
	row, err := q.GetActiveSessionByDigest(ctx, security.HashOpaqueToken(rawToken))
	if err != nil || !row.StatusAktif {
		return nil, false
	}
	if row.AnggotaID.Valid && row.AnggotaStatus != "" && row.AnggotaStatus != "aktif" {
		return nil, false
	}
	idleSecs := int32(idleTTL.Seconds())
	if idleSecs <= 0 {
		idleSecs = 1800
	}
	_ = q.TouchSessionActivity(ctx, db.TouchSessionActivityParams{
		ID:       row.SessionID,
		IdleSecs: idleSecs,
	})
	return &SessionClaims{
		SessionID:   uuid.UUID(row.SessionID.Bytes),
		UserID:      uuid.UUID(row.UserID.Bytes),
		AnggotaID:   pgUUIDToPtr(row.AnggotaID),
		Role:        row.Peran,
		Scope:       row.Scope,
		MFAVerified: row.MfaVerified,
	}, true
}

// OptionalSession menyematkan SessionClaims ke context bila token sesi valid tersedia.
func OptionalSession(pool *pgxpool.Pool, idleTTL time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tok := ExtractRawSessionToken(r); tok != "" {
				if claims, ok := resolveSessionFromDB(r.Context(), pool, tok, idleTTL); ok {
					ctx := context.WithValue(r.Context(), ContextKeySession, claims)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSession mewajibkan sesi opaque aktif di database.
func RequireSession(pool *pgxpool.Pool, idleTTL time.Duration, allowLimitedScope bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := ExtractRawSessionToken(r)
			claims, ok := resolveSessionFromDB(r.Context(), pool, tok, idleTTL)
			if !ok {
				response.Unauthorized(w, "Sesi tidak valid atau telah kedaluwarsa")
				return
			}
			if !allowLimitedScope && claims.Scope != "full" {
				response.Forbidden(w, "Selesaikan aktivasi atau verifikasi MFA terlebih dahulu")
				return
			}
			ctx := context.WithValue(r.Context(), ContextKeySession, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRoles memastikan pengguna memiliki salah satu peran yang diizinkan pada sesi scope penuh.
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
			if claims.Scope != "full" {
				response.Forbidden(w, "Sesi MFA terbatas tidak diizinkan mengakses endpoint ini")
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
