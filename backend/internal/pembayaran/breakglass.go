package pembayaran

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
)

func (s *Service) authorizeDualControlOrBreakglassTx(
	ctx context.Context,
	tx pgx.Tx,
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
	if s.cfg.BreakglassTOTP == nil {
		return false, response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Verifikator MFA break-glass belum tersedia")
	}
	if err := s.cfg.BreakglassTOTP.VerifyBreakglassTOTP(ctx, tx, claims.UserID, strings.TrimSpace(mfaCode)); err != nil {
		return false, err
	}
	return true, nil
}
