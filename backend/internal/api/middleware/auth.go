package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

type contextKey string

const ContextKeySession contextKey = "kopdes_session_claims"

const (
	SessionCookieHostName  = "__Host-kopdes_session"
	SessionCookiePlainName = "kopdes_session"
	SessionCookieName      = SessionCookiePlainName
	SessionTouchInterval   = time.Minute
)

// SessionAuthOptions mengatur perilaku autentikasi sesi cookie-utama dan Bearer opsional.
type SessionAuthOptions struct {
	IdleTTL      time.Duration
	AllowBearer  bool
	MemberLookup func(ctx context.Context, userID uuid.UUID) (anggotaID uuid.UUID, status string, found bool, err error)
}

// SessionClaims merepresentasikan sesi pengguna terautentikasi di server.
type SessionClaims struct {
	SessionID   uuid.UUID
	UserID      uuid.UUID
	AnggotaID   *uuid.UUID
	Role        string
	Scope       string
	MFAVerified bool
	ViaCookie   bool
}

// HasSessionCookie memeriksa apakah permintaan membawa cookie sesi kopdes.
func HasSessionCookie(r *http.Request) bool {
	for _, name := range []string{SessionCookieHostName, SessionCookiePlainName} {
		if c, err := r.Cookie(name); err == nil && strings.TrimSpace(c.Value) != "" {
			return true
		}
	}
	return false
}

// ExtractSessionToken mengambil token sesi dari cookie HttpOnly terlebih dahulu, lalu Bearer bila diizinkan.
func ExtractSessionToken(r *http.Request, allowBearer bool) (string, bool) {
	for _, name := range []string{SessionCookieHostName, SessionCookiePlainName} {
		if c, err := r.Cookie(name); err == nil {
			if tok := strings.TrimSpace(c.Value); tok != "" {
				return tok, true
			}
		}
	}
	if allowBearer {
		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "Bearer ") {
			if tok := strings.TrimSpace(authHeader[7:]); tok != "" {
				return tok, false
			}
		}
	}
	return "", false
}

// ExtractRawSessionToken mempertahankan kompatibilitas pemanggil yang mengizinkan Bearer.
func ExtractRawSessionToken(r *http.Request) string {
	tok, _ := ExtractSessionToken(r, true)
	return tok
}

func resolveSessionFromDB(ctx context.Context, pool *pgxpool.Pool, rawToken string, opts SessionAuthOptions, viaCookie bool) (*SessionClaims, bool) {
	if pool == nil || rawToken == "" {
		return nil, false
	}
	q := db.New(pool)
	row, err := q.GetActiveSessionByDigest(ctx, security.HashOpaqueToken(rawToken))
	if err != nil || !row.StatusAktif {
		return nil, false
	}
	userID := uuid.UUID(row.UserID.Bytes)
	var anggotaPtr *uuid.UUID
	if opts.MemberLookup != nil {
		aid, st, found, mErr := opts.MemberLookup(ctx, userID)
		if mErr != nil {
			return nil, false
		}
		if found {
			if st != "" && st != "aktif" {
				return nil, false
			}
			anggotaPtr = &aid
		}
	}
	maybeTouchSessionActivity(ctx, q, row, opts.IdleTTL)
	return &SessionClaims{
		SessionID:   uuid.UUID(row.SessionID.Bytes),
		UserID:      userID,
		AnggotaID:   anggotaPtr,
		Role:        row.Peran,
		Scope:       row.Scope,
		MFAVerified: row.MfaVerified,
		ViaCookie:   viaCookie,
	}, true
}

func maybeTouchSessionActivity(ctx context.Context, q *db.Queries, row db.GetActiveSessionByDigestRow, idleTTL time.Duration) {
	if row.LastActiveAt.Valid && time.Since(row.LastActiveAt.Time) < SessionTouchInterval {
		return
	}
	idleSecs := int32(idleTTL.Seconds())
	if idleSecs <= 0 {
		idleSecs = 1800
	}
	_ = q.TouchSessionActivity(ctx, db.TouchSessionActivityParams{
		ID:       row.SessionID,
		IdleSecs: idleSecs,
	})
}

// OptionalSession menyematkan SessionClaims ke context dengan mengizinkan Bearer (kompatibel).
func OptionalSession(pool *pgxpool.Pool, idleTTL time.Duration) func(http.Handler) http.Handler {
	return OptionalSessionWithOptions(pool, SessionAuthOptions{IdleTTL: idleTTL, AllowBearer: true})
}

// OptionalSessionWithOptions menyematkan SessionClaims sesuai pengaturan transport sesi.
func OptionalSessionWithOptions(pool *pgxpool.Pool, opts SessionAuthOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tok, viaCookie := ExtractSessionToken(r, opts.AllowBearer); tok != "" {
				if claims, ok := resolveSessionFromDB(r.Context(), pool, tok, opts, viaCookie); ok {
					ctx := context.WithValue(r.Context(), ContextKeySession, claims)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSession mewajibkan sesi aktif di database (mode kompatibel AllowBearer=true).
func RequireSession(pool *pgxpool.Pool, idleTTL time.Duration, allowLimitedScope bool) func(http.Handler) http.Handler {
	return RequireSessionWithOptions(pool, SessionAuthOptions{IdleTTL: idleTTL, AllowBearer: true}, allowLimitedScope)
}

// RequireSessionWithOptions mewajibkan sesi aktif menggunakan cookie HttpOnly (atau Bearer bila diaktifkan).
func RequireSessionWithOptions(pool *pgxpool.Pool, opts SessionAuthOptions, allowLimitedScope bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok, viaCookie := ExtractSessionToken(r, opts.AllowBearer)
			claims, ok := resolveSessionFromDB(r.Context(), pool, tok, opts, viaCookie)
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
