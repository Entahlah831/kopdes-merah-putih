package pendaftaran

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

var nikRegex = regexp.MustCompile(`^[0-9]{16}$`)

type Config struct {
	RegistrationEnabled bool
	CaptchaEnabled      bool
	CaptchaSecretKey    string
	TurnstileVerifyURL  string
	KeyProvider         security.KeyProvider
	BlindIndexSecret    []byte
	RetentionDays       int
	TrustedProxies      []string
	HTTPClient          *http.Client
}

type Service struct {
	pool    *pgxpool.Pool
	store   *KYCFileStore
	cfg     Config
	trusted *middleware.TrustedProxyMatcher
}

func NewService(pool *pgxpool.Pool, store *KYCFileStore, cfg Config) *Service {
	if cfg.TurnstileVerifyURL == "" {
		cfg.TurnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 30
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Service{
		pool:    pool,
		store:   store,
		cfg:     cfg,
		trusted: middleware.NewTrustedProxyMatcher(cfg.TrustedProxies),
	}
}

type RegisterInput struct {
	NamaLengkap  string `json:"nama_lengkap"`
	Email        string `json:"email"`
	NIK          string `json:"nik"`
	NoHP         string `json:"no_hp"`
	TanggalLahir string `json:"tanggal_lahir"`
	Alamat       string `json:"alamat"`
	Pekerjaan    string `json:"pekerjaan"`
	CaptchaToken string `json:"captcha_token"`
}

type VerifyDecisionInput struct {
	Keputusan string `json:"keputusan"`
	Catatan   string `json:"catatan"`
}

func (s *Service) verifyTurnstile(ctx context.Context, token, remoteIP string) error {
	if !s.cfg.CaptchaEnabled {
		return nil
	}
	if strings.TrimSpace(token) == "" {
		return response.NewDomainError(http.StatusBadRequest, "CAPTCHA_REQUIRED", "Token CAPTCHA wajib diisi")
	}
	form := url.Values{}
	form.Set("secret", s.cfg.CaptchaSecretKey)
	form.Set("response", strings.TrimSpace(token))
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.TurnstileVerifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// #nosec G704 -- endpoint TurnstileVerifyURL dikonfigurasi internal/default resmi Cloudflare
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return response.NewDomainError(http.StatusBadGateway, "CAPTCHA_UNAVAILABLE", "Gagal menghubungi verifikasi CAPTCHA")
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out); err != nil || !out.Success {
		return response.NewDomainError(http.StatusBadRequest, "CAPTCHA_INVALID", "Verifikasi CAPTCHA gagal")
	}
	return nil
}

func validateRegisterInput(in RegisterInput) (nik, normHP string, err error) {
	nik = strings.TrimSpace(in.NIK)
	normHP = security.NormalizePhone(in.NoHP)
	if strings.TrimSpace(in.NamaLengkap) == "" || !nikRegex.MatchString(nik) || len(normHP) < 9 {
		return "", "", response.NewDomainError(http.StatusBadRequest, "INVALID_REGISTRATION_DATA", "Nama lengkap, NIK 16 digit, dan nomor HP valid wajib diisi")
	}
	if _, err := time.Parse("2006-01-02", strings.TrimSpace(in.TanggalLahir)); err != nil {
		return "", "", response.NewDomainError(http.StatusBadRequest, "INVALID_BIRTH_DATE", "tanggal_lahir harus berformat YYYY-MM-DD")
	}
	if strings.TrimSpace(in.Alamat) == "" {
		return "", "", response.NewDomainError(http.StatusBadRequest, "INVALID_ADDRESS", "Alamat wajib diisi")
	}
	return nik, normHP, nil
}

func (s *Service) RegisterPublic(ctx context.Context, in RegisterInput, ip string) (map[string]interface{}, error) {
	if !s.cfg.RegistrationEnabled {
		return nil, response.NewDomainError(http.StatusForbidden, "REGISTRATION_DISABLED", "Pendaftaran anggota baru sedang dinonaktifkan")
	}
	if s.cfg.KeyProvider == nil {
		return nil, response.NewDomainError(http.StatusInternalServerError, "KEY_PROVIDER_UNAVAILABLE", "Penyedia enkripsi tidak siap")
	}
	if err := s.verifyTurnstile(ctx, in.CaptchaToken, ip); err != nil {
		return nil, err
	}
	nik, normHP, err := validateRegisterInput(in)
	if err != nil {
		return nil, err
	}

	q := db.New(s.pool)
	nikHash := security.HashBlindIndex(nik, s.cfg.BlindIndexSecret)
	if activeCount, _ := q.CountActivePendaftaranByNIKHash(ctx, nikHash); activeCount > 0 {
		return nil, response.NewDomainError(http.StatusConflict, "DUPLICATE_REGISTRATION", "NIK ini sedang memiliki pendaftaran aktif")
	}
	if _, err := q.GetAnggotaByNIKHash(ctx, nikHash); err == nil {
		return nil, response.NewDomainError(http.StatusConflict, "ALREADY_MEMBER", "NIK sudah terdaftar sebagai anggota aktif")
	}

	return s.persistRegistration(ctx, in, nik, nikHash, normHP, ip)
}

type encryptedRegistrationPII struct {
	nikEnc, hpEnc, dobEnc, addrEnc, emailEnc []byte
	emailHash                                pgtype.Text
}

func (s *Service) encryptRegistrationPII(idStr string, in RegisterInput, nik, normHP string) (encryptedRegistrationPII, error) {
	var out encryptedRegistrationPII
	var err error
	out.nikEnc, err = s.cfg.KeyProvider.EncryptWithAAD([]byte(nik), security.FormatAAD("pendaftaran", "nik", idStr))
	if err != nil {
		return out, err
	}
	out.hpEnc, _ = s.cfg.KeyProvider.EncryptWithAAD([]byte(normHP), security.FormatAAD("pendaftaran", "hp", idStr))
	out.dobEnc, _ = s.cfg.KeyProvider.EncryptWithAAD([]byte(strings.TrimSpace(in.TanggalLahir)), security.FormatAAD("pendaftaran", "tanggal_lahir", idStr))
	out.addrEnc, _ = s.cfg.KeyProvider.EncryptWithAAD([]byte(strings.TrimSpace(in.Alamat)), security.FormatAAD("pendaftaran", "alamat", idStr))
	if em := strings.ToLower(strings.TrimSpace(in.Email)); em != "" {
		out.emailHash = pgtype.Text{String: security.HashBlindIndex(em, s.cfg.BlindIndexSecret), Valid: true}
		out.emailEnc, _ = s.cfg.KeyProvider.EncryptWithAAD([]byte(em), security.FormatAAD("pendaftaran", "email", idStr))
	}
	return out, nil
}

func (s *Service) persistRegistration(ctx context.Context, in RegisterInput, nik, nikHash, normHP, ip string) (map[string]interface{}, error) {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	idStr := id.String()
	rawTok, tokHash, err := security.GenerateOpaqueToken(24)
	if err != nil {
		return nil, err
	}
	pii, err := s.encryptRegistrationPII(idStr, in, nik, normHP)
	if err != nil {
		return nil, err
	}

	q := db.New(s.pool)
	row, err := q.CreatePendaftaran(ctx, db.CreatePendaftaranParams{
		ID:              response.ToPGUUID(id),
		NamaLengkap:     strings.TrimSpace(in.NamaLengkap),
		EmailEnc:        pii.emailEnc,
		EmailHash:       pii.emailHash,
		NikEnc:          pii.nikEnc,
		NikHash:         nikHash,
		NikKeyVersion:   security.BlindIndexKeyVersion(s.cfg.BlindIndexSecret),
		NikMasked:       security.MaskNIK(nik),
		HpEnc:           pii.hpEnc,
		HpHash:          security.HashBlindIndex(normHP, s.cfg.BlindIndexSecret),
		HpMasked:        security.MaskPhone(normHP),
		TanggalLahirEnc: pii.dobEnc,
		AlamatEnc:       pii.addrEnc,
		Pekerjaan:       strings.TrimSpace(in.Pekerjaan),
		UploadTokenHash: tokHash,
		IpAddress:       ip,
	})
	if err != nil {
		return nil, err
	}
	_ = q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorPeran: "publik", Aksi: "submit_pendaftaran", Entitas: "pendaftaran",
		EntitasID: idStr, Ringkasan: "Pengajuan pendaftaran calon anggota", IpAddress: ip,
	})
	return map[string]interface{}{
		"id":           uuid.UUID(row.ID.Bytes),
		"nama_lengkap": row.NamaLengkap,
		"nik_masked":   row.NikMasked,
		"hp_masked":    row.HpMasked,
		"status":       row.Status,
		"upload_token": rawTok,
		"created_at":   row.CreatedAt.Time,
	}, nil
}
