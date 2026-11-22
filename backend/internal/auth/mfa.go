package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

type MFASetupInput struct {
	Action     string `json:"action,omitempty"`
	Code       string `json:"code"`
	SetupToken string `json:"setup_token"`
}

func isSkipMFAAction(in MFASetupInput) bool {
	return strings.EqualFold(strings.TrimSpace(in.Action), "skip")
}

func validateSkipMFASetup(mfaEnabled bool) error {
	if mfaEnabled {
		return response.NewDomainError(http.StatusConflict, "MFA_ALREADY_ENABLED", "MFA sudah aktif untuk akun ini dan wajib diverifikasi")
	}
	return nil
}

type MFAVerifyInput struct {
	Code         string `json:"code"`
	RecoveryCode string `json:"recovery_code"`
}

func (s *Service) SetupMFA(ctx context.Context, claims *middleware.SessionClaims, in MFASetupInput, ip string) (map[string]interface{}, error) {
	if s.cfg.KeyProvider == nil {
		return nil, response.NewDomainError(http.StatusInternalServerError, "KEY_PROVIDER_UNAVAILABLE", "Penyedia enkripsi tidak siap")
	}
	q := db.New(s.pool)
	u, err := q.GetUserByID(ctx, response.ToPGUUID(claims.UserID))
	if err != nil {
		return nil, err
	}
	if isSkipMFAAction(in) {
		return s.skipMFASetup(ctx, q, claims, u, ip)
	}
	if u.MfaEnabled && claims.Scope != "full" {
		return nil, response.NewDomainError(http.StatusConflict, "MFA_ALREADY_ENABLED", "MFA sudah aktif untuk akun ini")
	}
	aad := security.FormatAAD("sesi", "mfa_setup_token", claims.SessionID.String())
	if strings.TrimSpace(in.Code) == "" {
		return s.beginMFASetup(ctx, u, aad)
	}
	return s.confirmMFASetup(ctx, claims, in, aad, ip)
}

func (s *Service) skipMFASetup(ctx context.Context, q *db.Queries, claims *middleware.SessionClaims, u db.User, ip string) (map[string]interface{}, error) {
	if err := validateSkipMFASetup(u.MfaEnabled); err != nil {
		return nil, err
	}
	if err := q.UpgradeSessionAfterMFA(ctx, response.ToPGUUID(claims.SessionID)); err != nil {
		return nil, fmt.Errorf("tunda setup mfa: %w", err)
	}
	s.logAudit(ctx, uuidPtr(claims.UserID), claims.Role, "mfa_setup_deferred", "sesi", claims.SessionID.String(), "Menunda aktivasi MFA ke pengaturan akun", ip)
	return map[string]interface{}{
		"mfa_enabled":  false,
		"mfa_deferred": true,
		"scope":        "full",
	}, nil
}

func (s *Service) beginMFASetup(_ context.Context, u db.User, aad []byte) (map[string]interface{}, error) {
	secret, err := security.GenerateTOTPSecret()
	if err != nil {
		return nil, fmt.Errorf("buat rahasia TOTP: %w", err)
	}
	encTok, err := s.cfg.KeyProvider.EncryptWithAAD([]byte(secret), aad)
	if err != nil {
		return nil, fmt.Errorf("enkripsi setup token MFA: %w", err)
	}
	label := u.NamaLengkap
	if decEmail := s.decryptUserField(u.EmailEnc, u.ID, "email"); decEmail != "" {
		label = decEmail
	}
	return map[string]interface{}{
		"secret":      secret,
		"otpauth_uri": security.GenerateTOTPURI(secret, label, s.cfg.MFAIssuer),
		"setup_token": base64.RawURLEncoding.EncodeToString(encTok),
	}, nil
}

func (s *Service) confirmMFASetup(ctx context.Context, claims *middleware.SessionClaims, in MFASetupInput, aad []byte, ip string) (map[string]interface{}, error) {
	rawEnc, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(in.SetupToken))
	if err != nil {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_SETUP_TOKEN", "Setup token MFA tidak valid")
	}
	secretBytes, err := s.cfg.KeyProvider.DecryptWithAAD(rawEnc, aad)
	if err != nil {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_SETUP_TOKEN", "Setup token MFA tidak cocok atau kedaluwarsa")
	}
	secret := string(secretBytes)
	valid, step := security.ValidateTOTPCodeWithStep(secret, strings.TrimSpace(in.Code), time.Now().UTC())
	if !valid {
		return nil, response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Kode TOTP konfirmasi salah")
	}
	return s.persistEnabledMFA(ctx, claims, secret, step, ip)
}

func (s *Service) persistEnabledMFA(ctx context.Context, claims *middleware.SessionClaims, secret string, step int64, ip string) (map[string]interface{}, error) {
	recPlain, err := security.GenerateRecoveryCodes(8)
	if err != nil {
		return nil, fmt.Errorf("buat recovery codes: %w", err)
	}
	recHashes := make([]string, len(recPlain))
	for i, c := range recPlain {
		recHashes[i] = security.HashRecoveryCode(c)
	}
	encSecret, err := s.cfg.KeyProvider.EncryptWithAAD([]byte(secret), security.FormatAAD("users", "mfa_secret", claims.UserID.String()))
	if err != nil {
		return nil, fmt.Errorf("enkripsi mfa_secret: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("mulai transaksi mfa setup: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qt := db.New(tx)
	pgUID := response.ToPGUUID(claims.UserID)
	if err := qt.EnableUserMFA(ctx, db.EnableUserMFAParams{
		ID:                pgUID,
		MfaSecretEnc:      encSecret,
		MfaRecoveryHashes: recHashes,
		MfaLastStep:       step,
	}); err != nil {
		return nil, fmt.Errorf("aktifkan mfa: %w", err)
	}
	if err := qt.UpgradeSessionAfterMFA(ctx, response.ToPGUUID(claims.SessionID)); err != nil {
		return nil, fmt.Errorf("upgrade sesi mfa: %w", err)
	}
	s.logAuditTx(ctx, tx, uuidPtr(claims.UserID), claims.Role, "mfa_setup", "users", claims.UserID.String(), "Aktivasi MFA TOTP", ip)
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit mfa setup: %w", err)
	}
	return map[string]interface{}{
		"mfa_enabled":    true,
		"scope":          "full",
		"recovery_codes": recPlain,
	}, nil
}

func (s *Service) VerifyMFA(ctx context.Context, claims *middleware.SessionClaims, in MFAVerifyInput, ip string) (map[string]interface{}, error) {
	q := db.New(s.pool)
	u, err := q.GetUserByID(ctx, response.ToPGUUID(claims.UserID))
	if err != nil {
		return nil, err
	}
	if !u.MfaEnabled || len(u.MfaSecretEnc) == 0 || s.cfg.KeyProvider == nil {
		return nil, response.NewDomainError(http.StatusBadRequest, "MFA_NOT_ENABLED", "MFA belum diaktifkan pada akun ini")
	}

	verified := false
	if strings.TrimSpace(in.Code) != "" {
		verified = s.verifyTOTPWithAntiReplay(ctx, u, strings.TrimSpace(in.Code))
	} else if strings.TrimSpace(in.RecoveryCode) != "" {
		verified = s.consumeRecoveryCode(ctx, u, strings.TrimSpace(in.RecoveryCode))
	}
	if !verified {
		return nil, s.handleFailedMFA(ctx, claims)
	}

	if err := q.UpgradeSessionAfterMFA(ctx, response.ToPGUUID(claims.SessionID)); err != nil {
		return nil, fmt.Errorf("upgrade sesi mfa: %w", err)
	}
	s.logAudit(ctx, pgUUIDPtr(u.ID), u.Peran, "mfa_verify", "sesi", claims.SessionID.String(), "Verifikasi MFA berhasil", ip)
	return map[string]interface{}{"mfa_verified": true, "scope": "full"}, nil
}

func (s *Service) verifyTOTPWithAntiReplay(ctx context.Context, u db.User, code string) bool {
	uid := uuid.UUID(u.ID.Bytes).String()
	secBytes, err := s.cfg.KeyProvider.DecryptWithAAD(u.MfaSecretEnc, security.FormatAAD("users", "mfa_secret", uid))
	if err != nil {
		return false
	}
	ok, step := security.ValidateTOTPCodeWithStep(string(secBytes), code, time.Now().UTC())
	if !ok || step <= u.MfaLastStep {
		return false
	}
	_, err = db.New(s.pool).UpdateUserMFALastStep(ctx, db.UpdateUserMFALastStepParams{
		ID:          u.ID,
		MfaLastStep: step,
	})
	return err == nil
}

func (s *Service) consumeRecoveryCode(ctx context.Context, u db.User, raw string) bool {
	targetHash := security.HashRecoveryCode(raw)
	remaining := make([]string, 0, len(u.MfaRecoveryHashes))
	found := false
	for _, h := range u.MfaRecoveryHashes {
		if !found && security.ConstantTimeEqualHex(h, targetHash) {
			found = true
			continue
		}
		remaining = append(remaining, h)
	}
	if !found {
		return false
	}
	return db.New(s.pool).UpdateUserRecoveryHashes(ctx, db.UpdateUserRecoveryHashesParams{
		ID:                u.ID,
		MfaRecoveryHashes: remaining,
	}) == nil
}

func (s *Service) handleFailedMFA(ctx context.Context, claims *middleware.SessionClaims) error {
	res, _ := db.New(s.pool).IncrementSessionMFAAttempts(ctx, db.IncrementSessionMFAAttemptsParams{
		ID:          response.ToPGUUID(claims.SessionID),
		MaxAttempts: int32(s.cfg.MFAMaxAttempts), // #nosec G115 -- bounded small integer
	})
	if res.RevokedAt.Valid {
		return response.NewDomainError(http.StatusUnauthorized, "MFA_SESSION_REVOKED", "Batas percobaan MFA terlampaui, silakan login ulang")
	}
	return response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Kode MFA atau recovery code tidak valid")
}

// unused pgtype guard if needed later
var _ = pgtype.UUID{}
