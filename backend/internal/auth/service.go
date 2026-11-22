package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	angapi "github.com/kopdes/merah-putih/internal/anggota/api"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	pengapi "github.com/kopdes/merah-putih/internal/pengaturan/api"
	"github.com/kopdes/merah-putih/internal/security"
)

// Config menyimpan parameter runtime untuk domain autentikasi.
type Config struct {
	AppEnv               string
	BlindIndexSecret     []byte
	KeyProvider          security.KeyProvider
	Audit                pengapi.AuditWriter
	Members              angapi.MemberReader
	BcryptCost           int
	SessionIdleTTL       time.Duration
	SessionAbsTTL        time.Duration
	CookieSecure         string
	AllowBearerAuth      bool
	MFARequiredRoles     []string
	MFAIssuer            string
	MFAMaxAttempts       int
	LoginMaxAttempts     int
	LoginLockoutDuration time.Duration
	TrustedProxies       []string
	CaptchaEnabled       bool
	CaptchaSecretKey     string
	DummyBcryptHash      string
}

type Service struct {
	pool    *pgxpool.Pool
	cfg     Config
	limiter *middleware.LoginRateLimiter
	trusted *middleware.TrustedProxyMatcher
}

func NewService(pool *pgxpool.Pool, cfg Config, limiter *middleware.LoginRateLimiter) *Service {
	if cfg.BcryptCost < 4 {
		cfg.BcryptCost = 10
	}
	if cfg.SessionIdleTTL <= 0 {
		cfg.SessionIdleTTL = 30 * time.Minute
	}
	if cfg.SessionAbsTTL < cfg.SessionIdleTTL {
		cfg.SessionAbsTTL = 12 * time.Hour
	}
	if cfg.MFAMaxAttempts <= 0 {
		cfg.MFAMaxAttempts = 3
	}
	if cfg.MFAIssuer == "" {
		cfg.MFAIssuer = "Kopdes Merah Putih"
	}
	if cfg.DummyBcryptHash == "" {
		if h, err := security.HashPasswordWithCost("dummy-kopdes-constant-time", cfg.BcryptCost); err == nil {
			cfg.DummyBcryptHash = h
		}
	}
	if limiter == nil {
		limiter = middleware.NewLoginRateLimiter(cfg.LoginMaxAttempts, cfg.LoginLockoutDuration, 10000)
	}
	return &Service{
		pool:    pool,
		cfg:     cfg,
		limiter: limiter,
		trusted: middleware.NewTrustedProxyMatcher(cfg.TrustedProxies),
	}
}

type LoginInput struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
	IP         string `json:"-"`
	UserAgent  string `json:"-"`
}

type LoginResult struct {
	SessionToken     string     `json:"session_token,omitempty"`
	Scope            string     `json:"scope"`
	MFARequired      bool       `json:"mfa_required"`
	MFASetupRequired bool       `json:"mfa_setup_required"`
	ExpiresAt        time.Time  `json:"expires_at"`
	UserID           uuid.UUID  `json:"user_id"`
	Role             string     `json:"peran"`
	NamaLengkap      string     `json:"nama_lengkap"`
}

func normalizeIdentifier(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if strings.Contains(s, "@") {
		return s
	}
	return security.NormalizePhone(s)
}

func (s *Service) roleRequiresMFA(role string) bool {
	for _, r := range s.cfg.MFARequiredRoles {
		if strings.EqualFold(strings.TrimSpace(r), strings.TrimSpace(role)) {
			return true
		}
	}
	return false
}

func (s *Service) findUserByIdentifier(ctx context.Context, norm string) (db.User, error) {
	q := db.New(s.pool)
	hash := security.HashBlindIndex(norm, s.cfg.BlindIndexSecret)
	if strings.Contains(norm, "@") {
		return q.GetUserByEmailHash(ctx, pgtype.Text{String: hash, Valid: true})
	}
	return q.GetUserByHPHash(ctx, pgtype.Text{String: hash, Valid: true})
}

func (s *Service) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	norm := normalizeIdentifier(in.Identifier)
	if norm == "" || in.Password == "" {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_CREDENTIALS_INPUT", "Identifier dan password wajib diisi")
	}
	accHash := security.HashBlindIndex(norm, s.cfg.BlindIndexSecret)
	if allowed, _ := s.limiter.CheckAllowed(in.IP, accHash); !allowed {
		return nil, response.NewDomainError(http.StatusTooManyRequests, "ACCOUNT_LOCKED", "Terlalu banyak percobaan login gagal. Coba lagi nanti.")
	}

	user, err := s.findUserByIdentifier(ctx, norm)
	if err != nil {
		security.DummyBcryptCheck(in.Password, s.cfg.DummyBcryptHash)
		s.limiter.RecordFailure(in.IP, accHash)
		return nil, response.NewDomainError(http.StatusUnauthorized, "INVALID_CREDENTIALS", "Kredensial tidak valid")
	}
	if user.LockedUntil.Valid && time.Now().Before(user.LockedUntil.Time) {
		return nil, response.NewDomainError(http.StatusTooManyRequests, "ACCOUNT_LOCKED", "Akun terkunci sementara karena percobaan login gagal")
	}
	if !user.StatusAktif || user.PasswordHash == "" || !security.CheckPassword(in.Password, user.PasswordHash) {
		s.recordFailedAttempt(ctx, user.ID, in.IP, accHash)
		return nil, response.NewDomainError(http.StatusUnauthorized, "INVALID_CREDENTIALS", "Kredensial tidak valid")
	}

	return s.issueSessionForUser(ctx, user, in, accHash)
}

func (s *Service) recordFailedAttempt(ctx context.Context, userID pgtype.UUID, ip, accHash string) {
	s.limiter.RecordFailure(ip, accHash)
	maxAtt := int32(5)
	if s.cfg.LoginMaxAttempts > 0 && s.cfg.LoginMaxAttempts <= 1000 {
		maxAtt = int32(s.cfg.LoginMaxAttempts)
	}
	lockSecs := int32(900)
	if secs := s.cfg.LoginLockoutDuration.Seconds(); secs > 0 && secs <= 86400 {
		lockSecs = int32(secs)
	}
	_, _ = db.New(s.pool).IncrementFailedLogin(ctx, db.IncrementFailedLoginParams{
		ID:          userID,
		MaxAttempts: maxAtt,
		LockoutSecs: lockSecs,
	})
}

func (s *Service) issueSessionForUser(ctx context.Context, user db.User, in LoginInput, accHash string) (*LoginResult, error) {
	q := db.New(s.pool)
	s.limiter.RecordSuccess(accHash)
	_ = q.ResetFailedLogin(ctx, user.ID)

	rawTok, tokHash, err := security.GenerateOpaqueToken(32)
	if err != nil {
		return nil, fmt.Errorf("buat token sesi: %w", err)
	}

	scope := "full"
	mfaVerified := true
	mfaReq, mfaSetup := false, false
	if user.MfaEnabled {
		scope, mfaVerified, mfaReq = "mfa_pending", false, true
	} else if s.roleRequiresMFA(user.Peran) {
		scope, mfaVerified, mfaSetup = "mfa_setup", false, true
	}

	now := time.Now().UTC()
	idleExp := now.Add(s.cfg.SessionIdleTTL)
	absExp := now.Add(s.cfg.SessionAbsTTL)
	if _, err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID:        user.ID,
		TokenHash:     tokHash,
		MfaVerified:   mfaVerified,
		Scope:         scope,
		IpAddress:     in.IP,
		UserAgent:     truncateStr(in.UserAgent, 250),
		IdleExpiresAt: response.ToPGTimestamptz(idleExp),
		AbsExpiresAt:  response.ToPGTimestamptz(absExp),
	}); err != nil {
		return nil, fmt.Errorf("simpan sesi: %w", err)
	}

	uid := response.FromPGUUID(user.ID)
	s.logAudit(ctx, &uid, user.Peran, "login", "sesi", uid.String(), fmt.Sprintf("Login scope=%s", scope), in.IP)

	return &LoginResult{
		SessionToken:     rawTok,
		Scope:            scope,
		MFARequired:      mfaReq,
		MFASetupRequired: mfaSetup,
		ExpiresAt:        idleExp,
		UserID:           uid,
		Role:             user.Peran,
		NamaLengkap:      user.NamaLengkap,
	}, nil
}

func (s *Service) Logout(ctx context.Context, claims *middleware.SessionClaims, ip string) error {
	q := db.New(s.pool)
	if err := q.RevokeSessionByID(ctx, response.ToPGUUID(claims.SessionID)); err != nil {
		return fmt.Errorf("logout sesi: %w", err)
	}
	s.logAudit(ctx, uuidPtr(claims.UserID), claims.Role, "logout", "sesi", claims.SessionID.String(), "Logout sesi", ip)
	return nil
}

func (s *Service) Me(ctx context.Context, claims *middleware.SessionClaims) (map[string]interface{}, error) {
	u, err := db.New(s.pool).GetUserByID(ctx, response.ToPGUUID(claims.UserID))
	if err != nil {
		return nil, err
	}
	email := s.decryptUserField(u.EmailEnc, u.ID, "email")
	hp := s.decryptUserField(u.HpEnc, u.ID, "hp")
	return map[string]interface{}{
		"user_id":     claims.UserID,
		"anggota_id":  claims.AnggotaID,
		"nama":        u.NamaLengkap,
		"peran":       u.Peran,
		"scope":       claims.Scope,
		"mfa_enabled": u.MfaEnabled,
		"email":       email,
		"no_hp":       security.MaskPhone(hp),
	}, nil
}

func (s *Service) decryptUserField(enc []byte, userID pgtype.UUID, col string) string {
	if len(enc) == 0 || s.cfg.KeyProvider == nil {
		return ""
	}
	uid := uuid.UUID(userID.Bytes).String()
	pt, err := s.cfg.KeyProvider.DecryptWithAAD(enc, security.FormatAAD("users", col, uid))
	if err != nil {
		return ""
	}
	return string(pt)
}

// CreateActivationOrResetCodeTx menghasilkan kode aktivasi/reset sekali pakai di dalam transaksi pemanggil.
func CreateActivationOrResetCodeTx(ctx context.Context, tx pgx.Tx, userID pgtype.UUID, createdBy pgtype.UUID, jenis, noHP string, ttl time.Duration) (string, string, error) {
	buf := make([]byte, 9)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("buat kode acak: %w", err)
	}
	rawCode := strings.TrimRight(base64.RawURLEncoding.EncodeToString(buf), "=")
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	_, err := db.New(tx).CreateActivationCode(ctx, db.CreateActivationCodeParams{
		UserID:    userID,
		KodeHash:  security.HashOpaqueToken(rawCode),
		Jenis:     jenis,
		ExpiresAt: response.ToPGTimestamptz(time.Now().UTC().Add(ttl)),
		CreatedBy: createdBy,
	})
	if err != nil {
		return "", "", fmt.Errorf("simpan kode aktivasi: %w", err)
	}
	waPhone := security.NormalizePhone(noHP)
	if strings.HasPrefix(waPhone, "0") {
		waPhone = "62" + waPhone[1:]
	}
	msg := url.QueryEscape("Halo, kode aktivasi akun Kopdes Merah Putih Anda: " + rawCode + " (berlaku 24 jam).")
	waLink := "https://wa.me/" + waPhone + "?text=" + msg
	return rawCode, waLink, nil
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
