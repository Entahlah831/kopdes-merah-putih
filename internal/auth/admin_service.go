package auth

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	angapi "github.com/kopdes/merah-putih/internal/anggota/api"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

type PasswordInput struct {
	Code            string `json:"code"`
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type CreateUserInput struct {
	Email       string `json:"email"`
	NoHP        string `json:"no_hp"`
	NamaLengkap string `json:"nama_lengkap"`
	Peran       string `json:"peran"`
	Password    string `json:"password"`
}

type UpdateUserInput struct {
	Peran          string `json:"peran"`
	StatusAktif    *bool  `json:"status_aktif"`
	ResetMFA       bool   `json:"reset_mfa"`
	IssueResetCode bool   `json:"issue_reset_code"`
}

func validatePasswordPolicy(pwd string) error {
	if len(pwd) < 10 {
		return response.NewDomainError(http.StatusBadRequest, "WEAK_PASSWORD", "Kata sandi minimal 10 karakter")
	}
	if len(pwd) > 72 {
		return response.NewDomainError(http.StatusBadRequest, "PASSWORD_TOO_LONG", "Kata sandi maksimal 72 byte")
	}
	return nil
}

func (s *Service) ChangeOrActivatePassword(ctx context.Context, claims *middleware.SessionClaims, in PasswordInput, ip string) error {
	if err := validatePasswordPolicy(in.NewPassword); err != nil {
		return err
	}
	if strings.TrimSpace(in.Code) != "" {
		return s.consumeActivationPassword(ctx, strings.TrimSpace(in.Code), in.NewPassword, ip)
	}
	if claims == nil {
		return response.NewDomainError(http.StatusUnauthorized, "UNAUTHORIZED", "Autentikasi atau kode aktivasi wajib diisi")
	}
	return s.changeAuthenticatedPassword(ctx, claims, in.CurrentPassword, in.NewPassword, ip)
}

func (s *Service) consumeActivationPassword(ctx context.Context, rawCode, newPassword, ip string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("mulai transaksi aktivasi: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qt := db.New(tx)
	codeRow, err := qt.GetValidActivationCodeByHashForUpdate(ctx, security.HashOpaqueToken(rawCode))
	if err != nil {
		return response.NewDomainError(http.StatusBadRequest, "INVALID_ACTIVATION_CODE", "Kode aktivasi/reset tidak valid atau telah kedaluwarsa")
	}
	hash, err := security.HashPasswordWithCost(newPassword, s.cfg.BcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := qt.UpdateUserSandiHash(ctx, db.UpdateUserSandiHashParams{ID: codeRow.UserID, PasswordHash: hash}); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if err := qt.ConsumeActivationCode(ctx, codeRow.ID); err != nil {
		return fmt.Errorf("tandai kode terpakai: %w", err)
	}
	if err := qt.RevokeAllUserSessions(ctx, codeRow.UserID); err != nil {
		return fmt.Errorf("cabut sesi lama: %w", err)
	}
	s.logAuditTx(ctx, tx, pgUUIDPtr(codeRow.UserID), "system", "password_activate_or_reset", "users",
		response.FromPGUUID(codeRow.UserID).String(), "Aktivasi/reset password via kode satu kali pakai", ip)
	return tx.Commit(ctx)
}

func (s *Service) changeAuthenticatedPassword(ctx context.Context, claims *middleware.SessionClaims, curPwd, newPwd, ip string) error {
	q := db.New(s.pool)
	u, err := q.GetUserByID(ctx, response.ToPGUUID(claims.UserID))
	if err != nil {
		return err
	}
	if !security.CheckPassword(curPwd, u.PasswordHash) {
		return response.NewDomainError(http.StatusUnauthorized, "INVALID_CURRENT_PASSWORD", "Kata sandi saat ini salah")
	}
	hash, err := security.HashPasswordWithCost(newPwd, s.cfg.BcryptCost)
	if err != nil {
		return fmt.Errorf("hash password baru: %w", err)
	}
	if err := q.UpdateUserSandiHash(ctx, db.UpdateUserSandiHashParams{ID: u.ID, PasswordHash: hash}); err != nil {
		return fmt.Errorf("perbarui password: %w", err)
	}
	s.logAudit(ctx, pgUUIDPtr(u.ID), u.Peran, "password_change", "users", claims.UserID.String(), "Perubahan kata sandi mandiri", ip)
	return nil
}

func (s *Service) ListUsers(ctx context.Context, lim, off int32) ([]map[string]interface{}, error) {
	rows, err := db.New(s.pool).ListUsers(ctx, db.ListUsersParams{Lim: lim, Off: off})
	if err != nil {
		return nil, err
	}
	memberByUser := s.lookupMemberSummaries(ctx, rows)
	out := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		uid := response.FromPGUUID(r.ID)
		m := memberByUser[uid]
		anggotaID := ""
		if m.ID != uuid.Nil {
			anggotaID = m.ID.String()
		}
		out = append(out, map[string]interface{}{
			"id":            uid,
			"nama_lengkap":  r.NamaLengkap,
			"peran":         r.Peran,
			"status_aktif":  r.StatusAktif,
			"mfa_enabled":   r.MfaEnabled,
			"email":         s.decryptUserField(r.EmailEnc, r.ID, "email"),
			"no_hp":         security.MaskPhone(s.decryptUserField(r.HpEnc, r.ID, "hp")),
			"anggota_id":    anggotaID,
			"nomor_anggota": m.NomorAnggota,
			"created_at":    r.CreatedAt.Time,
		})
	}
	return out, nil
}

func (s *Service) lookupMemberSummaries(ctx context.Context, rows []db.ListUsersRow) map[uuid.UUID]angapi.MemberSummary {
	if s.cfg.Members == nil || len(rows) == 0 {
		return map[uuid.UUID]angapi.MemberSummary{}
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, response.FromPGUUID(r.ID))
	}
	res, err := s.cfg.Members.LookupByUserIDs(ctx, ids)
	if err != nil {
		return map[uuid.UUID]angapi.MemberSummary{}
	}
	return res
}

func (s *Service) CreateUser(ctx context.Context, claims *middleware.SessionClaims, in CreateUserInput, ip string) (map[string]interface{}, error) {
	if s.cfg.KeyProvider == nil {
		return nil, response.NewDomainError(http.StatusInternalServerError, "KEY_PROVIDER_UNAVAILABLE", "Penyedia enkripsi belum siap")
	}
	if !slices.Contains([]string{"admin", "bendahara", "petugas_toko", "anggota"}, in.Peran) {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_ROLE", "Peran tidak valid")
	}
	if strings.TrimSpace(in.NamaLengkap) == "" || (strings.TrimSpace(in.Email) == "" && strings.TrimSpace(in.NoHP) == "") {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_INPUT", "Nama lengkap dan minimal satu email/no_hp wajib diisi")
	}
	pwdHash := ""
	if strings.TrimSpace(in.Password) != "" {
		if err := validatePasswordPolicy(in.Password); err != nil {
			return nil, err
		}
		h, err := security.HashPasswordWithCost(in.Password, s.cfg.BcryptCost)
		if err != nil {
			return nil, err
		}
		pwdHash = h
	}
	return s.persistNewUser(ctx, claims, in, pwdHash, ip)
}

func (s *Service) encryptUserContactFields(userID uuid.UUID, email, noHP string) (emailEnc []byte, emailHash pgtype.Text, hpEnc []byte, hpHash pgtype.Text) {
	if em := strings.ToLower(strings.TrimSpace(email)); em != "" {
		emailHash = pgtype.Text{String: security.HashBlindIndex(em, s.cfg.BlindIndexSecret), Valid: true}
		emailEnc, _ = s.cfg.KeyProvider.EncryptWithAAD([]byte(em), security.FormatAAD("users", "email", userID.String()))
	}
	if normHP := security.NormalizePhone(noHP); normHP != "" {
		hpHash = pgtype.Text{String: security.HashBlindIndex(normHP, s.cfg.BlindIndexSecret), Valid: true}
		hpEnc, _ = s.cfg.KeyProvider.EncryptWithAAD([]byte(normHP), security.FormatAAD("users", "hp", userID.String()))
	}
	return emailEnc, emailHash, hpEnc, hpHash
}

func (s *Service) persistNewUser(ctx context.Context, claims *middleware.SessionClaims, in CreateUserInput, pwdHash, ip string) (map[string]interface{}, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	u, err := s.insertUserRowTx(ctx, tx, in, pwdHash)
	if err != nil {
		return nil, err
	}
	res := map[string]interface{}{"id": uuid.UUID(u.ID.Bytes), "nama_lengkap": u.NamaLengkap, "peran": u.Peran, "status_aktif": u.StatusAktif}
	if pwdHash == "" {
		code, waLink, err := CreateActivationOrResetCodeTx(ctx, tx, u.ID, response.ToPGUUID(claims.UserID), "aktivasi_awal", security.NormalizePhone(in.NoHP), 24*time.Hour)
		if err != nil {
			return nil, err
		}
		res["activation_code"] = code
		res["wa_link"] = waLink
	}
	s.logAuditTx(ctx, tx, uuidPtr(claims.UserID), claims.Role, "create_user", "users",
		response.FromPGUUID(u.ID).String(), "Buat user peran="+u.Peran, ip)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Service) insertUserRowTx(ctx context.Context, tx pgx.Tx, in CreateUserInput, pwdHash string) (db.CreateUserRow, error) {
	userID, err := uuid.NewV7()
	if err != nil {
		userID = uuid.New()
	}
	emailEnc, emailHash, hpEnc, hpHash := s.encryptUserContactFields(userID, in.Email, in.NoHP)
	u, err := db.New(tx).CreateUser(ctx, db.CreateUserParams{
		ID: response.ToPGUUID(userID), EmailEnc: emailEnc, EmailHash: emailHash,
		HpEnc: hpEnc, HpHash: hpHash, NamaLengkap: strings.TrimSpace(in.NamaLengkap),
		PasswordHash: pwdHash, Peran: in.Peran, StatusAktif: true,
	})
	if err != nil {
		return u, response.NewDomainError(http.StatusConflict, "USER_EXISTS", "Email atau nomor HP sudah terdaftar")
	}
	return u, nil
}
