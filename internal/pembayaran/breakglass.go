package pembayaran

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

func (s *Service) authorizeDualControlOrBreakglassTx(
	ctx context.Context,
	qtx *db.Queries,
	claims *middleware.SessionClaims,
	makerUserID uuid.UUID,
	breakglassReq bool,
	mfaCode string,
	alasan string,
) (bool, error) {
	if makerUserID != claims.UserID {
		return false, nil
	}
	if !s.cfg.DualControlBreakglassEnabled || !breakglassReq {
		return false, response.NewDomainError(http.StatusForbidden, "DUAL_CONTROL_VIOLATION", "Pembuat dan pelaksana transaksi harus orang yang berbeda")
	}
	if claims.Role != "admin" {
		return false, response.NewDomainError(http.StatusForbidden, "DUAL_CONTROL_VIOLATION", "Mode darurat kendali ganda hanya tersedia bagi admin")
	}
	if len(strings.TrimSpace(alasan)) < 10 {
		return false, response.NewDomainError(http.StatusBadRequest, "REASON_REQUIRED", "Break-glass mewajibkan alasan jelas minimal 10 karakter")
	}
	if err := s.verifyAdminBreakglassTOTPTx(ctx, qtx, claims.UserID, strings.TrimSpace(mfaCode)); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) verifyAdminBreakglassTOTPTx(ctx context.Context, qtx *db.Queries, userID uuid.UUID, code string) error {
	if code == "" || s.cfg.DBKeyProvider == nil {
		return response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Break-glass wajib kode MFA TOTP terbaru yang valid")
	}
	u, err := qtx.GetUserByID(ctx, response.ToPGUUID(userID))
	if err != nil || !u.MfaEnabled || len(u.MfaSecretEnc) == 0 {
		return response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Admin wajib mengaktifkan MFA TOTP untuk break-glass")
	}
	secBytes, err := s.cfg.DBKeyProvider.DecryptWithAAD(u.MfaSecretEnc, security.FormatAAD("users", "mfa_secret", userID.String()))
	if err != nil {
		return response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Gagal membuka rahasia MFA admin")
	}
	ok, step := security.ValidateTOTPCodeWithStep(string(secBytes), code, time.Now().UTC())
	if !ok || step <= u.MfaLastStep {
		return response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Kode MFA break-glass salah atau sudah pernah digunakan")
	}
	if _, err := qtx.UpdateUserMFALastStep(ctx, db.UpdateUserMFALastStepParams{ID: u.ID, MfaLastStep: step}); err != nil {
		return response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Kode MFA break-glass sudah pernah digunakan")
	}
	return nil
}
