package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	angapi "github.com/kopdes/merah-putih/internal/anggota/api"
	"github.com/kopdes/merah-putih/internal/api/response"
	authapi "github.com/kopdes/merah-putih/internal/auth/api"
	"github.com/kopdes/merah-putih/internal/db"
	pengapi "github.com/kopdes/merah-putih/internal/pengaturan/api"
	"github.com/kopdes/merah-putih/internal/security"
)

func (s *Service) SetMembers(m angapi.MemberReader) {
	s.cfg.Members = m
}

var (
	_ authapi.UserManager           = (*Service)(nil)
	_ authapi.BreakglassTOTPVerifier = (*Service)(nil)
	_ authapi.MaintenanceCleaner    = (*Service)(nil)
)

func (s *Service) logAudit(ctx context.Context, actorUID *uuid.UUID, actorPeran, aksi, entitas, entitasID, ringkasan, ip string) {
	if s == nil || s.cfg.Audit == nil {
		return
	}
	_ = s.cfg.Audit.Log(ctx, pengapi.AuditEntry{
		ActorUserID: actorUID, ActorPeran: actorPeran, Aksi: aksi,
		Entitas: entitas, EntitasID: entitasID, Ringkasan: ringkasan, IPAddress: ip,
	})
}

func (s *Service) logAuditTx(ctx context.Context, tx pgx.Tx, actorUID *uuid.UUID, actorPeran, aksi, entitas, entitasID, ringkasan, ip string) {
	if s == nil || s.cfg.Audit == nil {
		return
	}
	_ = s.cfg.Audit.LogTx(ctx, tx, pengapi.AuditEntry{
		ActorUserID: actorUID, ActorPeran: actorPeran, Aksi: aksi,
		Entitas: entitas, EntitasID: entitasID, Ringkasan: ringkasan, IPAddress: ip,
	})
}

func pgUUIDPtr(id pgtype.UUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	v := response.FromPGUUID(id)
	return &v
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	v := id
	return &v
}

func (s *Service) CreateMemberUserTx(ctx context.Context, tx pgx.Tx, in authapi.CreateMemberUserInput) (authapi.MemberUserCreated, error) {
	if s.cfg.KeyProvider == nil {
		return authapi.MemberUserCreated{}, response.NewDomainError(http.StatusInternalServerError, "KEY_PROVIDER_UNAVAILABLE", "Penyedia enkripsi belum siap")
	}
	userID, err := uuid.NewV7()
	if err != nil {
		userID = uuid.New()
	}
	peran := strings.TrimSpace(in.Peran)
	if peran == "" {
		peran = "anggota"
	}
	emailEnc, emailHash, hpEnc, hpHash := s.encryptUserContactFields(userID, in.Email, in.NoHP)
	u, err := db.New(tx).CreateUser(ctx, db.CreateUserParams{
		ID: response.ToPGUUID(userID), EmailEnc: emailEnc, EmailHash: emailHash,
		HpEnc: hpEnc, HpHash: hpHash, NamaLengkap: strings.TrimSpace(in.NamaLengkap),
		PasswordHash: "", Peran: peran, StatusAktif: true,
	})
	if err != nil {
		return authapi.MemberUserCreated{}, response.NewDomainError(http.StatusConflict, "USER_CONFLICT", "Akun dengan email/HP ini sudah ada")
	}
	return authapi.MemberUserCreated{UserID: response.FromPGUUID(u.ID), NamaLengkap: u.NamaLengkap}, nil
}

func (s *Service) CreateActivationOrResetCodeTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, actorUserID *uuid.UUID, jenis, noHP string, ttl time.Duration) (authapi.ActivationCodeResult, error) {
	var actorPG pgtype.UUID
	if actorUserID != nil && *actorUserID != uuid.Nil {
		actorPG = response.ToPGUUID(*actorUserID)
	}
	code, waLink, err := CreateActivationOrResetCodeTx(ctx, tx, response.ToPGUUID(userID), actorPG, jenis, noHP, ttl)
	if err != nil {
		return authapi.ActivationCodeResult{}, err
	}
	return authapi.ActivationCodeResult{Code: code, WhatsAppLink: waLink}, nil
}

func (s *Service) SetUserActiveAndRevokeTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, active bool) error {
	qt := db.New(tx)
	pgUID := response.ToPGUUID(userID)
	if err := qt.SetUserActiveByID(ctx, db.SetUserActiveByIDParams{ID: pgUID, StatusAktif: active}); err != nil {
		return fmt.Errorf("perbarui status aktif user: %w", err)
	}
	if !active {
		if err := qt.RevokeAllUserSessions(ctx, pgUID); err != nil {
			return fmt.Errorf("cabut sesi user: %w", err)
		}
	}
	return nil
}

func (s *Service) GetUserRoleByID(ctx context.Context, userID uuid.UUID) (string, error) {
	u, err := db.New(s.pool).GetUserByID(ctx, response.ToPGUUID(userID))
	if err != nil {
		return "", err
	}
	return u.Peran, nil
}

func (s *Service) VerifyBreakglassTOTP(ctx context.Context, tx pgx.Tx, userID uuid.UUID, code string) error {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" || s.cfg.KeyProvider == nil {
		return response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Break-glass wajib kode MFA TOTP terbaru yang valid")
	}
	qtx := db.New(tx)
	u, err := qtx.GetUserByID(ctx, response.ToPGUUID(userID))
	if err != nil || !u.MfaEnabled || len(u.MfaSecretEnc) == 0 {
		return response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Admin wajib mengaktifkan MFA TOTP untuk break-glass")
	}
	secBytes, err := s.cfg.KeyProvider.DecryptWithAAD(u.MfaSecretEnc, security.FormatAAD("users", "mfa_secret", userID.String()))
	if err != nil {
		return response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Gagal membuka rahasia MFA admin")
	}
	ok, step := security.ValidateTOTPCodeWithStep(string(secBytes), trimmed, time.Now().UTC())
	if !ok || step <= u.MfaLastStep {
		return response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Kode MFA break-glass salah atau sudah pernah digunakan")
	}
	if _, err := qtx.UpdateUserMFALastStep(ctx, db.UpdateUserMFALastStepParams{ID: u.ID, MfaLastStep: step}); err != nil {
		return response.NewDomainError(http.StatusUnauthorized, "INVALID_MFA_CODE", "Kode MFA break-glass sudah pernah digunakan")
	}
	return nil
}

func (s *Service) CleanupExpiredRateLimits(ctx context.Context) (int64, error) {
	return db.New(s.pool).CleanupExpiredRateLimits(ctx)
}

func (s *Service) CleanupExpiredSessions(ctx context.Context) (int64, error) {
	return db.New(s.pool).CleanupExpiredSessions(ctx)
}
