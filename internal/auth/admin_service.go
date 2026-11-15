package auth

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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

type PengaturanInput struct {
	NamaKoperasi         string  `json:"nama_koperasi"`
	AlamatKoperasi       string  `json:"alamat_koperasi"`
	TeleponKoperasi      string  `json:"telepon_koperasi"`
	QRISUniqueDigits     int32   `json:"qris_unique_digits"`
	OngkirTetap          int64   `json:"ongkir_tetap"`
	KebijakanTeks        string  `json:"kebijakan_teks"`
	ActiveRekeningQRISID *string `json:"active_rekening_qris_id"`
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
	_ = qt.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: codeRow.UserID,
		ActorPeran:  "system",
		Aksi:        "password_activate_or_reset",
		Entitas:     "users",
		EntitasID:   uuid.UUID(codeRow.UserID.Bytes).String(),
		Ringkasan:   "Aktivasi/reset password via kode satu kali pakai",
		IpAddress:   ip,
	})
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
	_ = q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: u.ID,
		ActorPeran:  u.Peran,
		Aksi:        "password_change",
		Entitas:     "users",
		EntitasID:   claims.UserID.String(),
		Ringkasan:   "Perubahan kata sandi mandiri",
		IpAddress:   ip,
	})
	return nil
}

func (s *Service) UpdatePengaturan(ctx context.Context, claims *middleware.SessionClaims, in PengaturanInput, ip string) (db.Pengaturan, error) {
	if strings.TrimSpace(in.NamaKoperasi) == "" {
		return db.Pengaturan{}, response.NewDomainError(http.StatusBadRequest, "INVALID_NAMA_KOPERASI", "nama_koperasi wajib diisi")
	}
	if in.QRISUniqueDigits == 0 {
		in.QRISUniqueDigits = 3
	}
	if in.QRISUniqueDigits != 3 && in.QRISUniqueDigits != 4 {
		return db.Pengaturan{}, response.NewDomainError(http.StatusBadRequest, "INVALID_QRIS_DIGITS", "qris_unique_digits hanya boleh 3 atau 4")
	}
	if in.OngkirTetap < 0 {
		return db.Pengaturan{}, response.NewDomainError(http.StatusBadRequest, "INVALID_ONGKIR", "ongkir_tetap tidak boleh negatif")
	}
	var rekID pgtype.UUID
	if in.ActiveRekeningQRISID != nil && strings.TrimSpace(*in.ActiveRekeningQRISID) != "" {
		parsed, err := response.ParsePGUUID(*in.ActiveRekeningQRISID)
		if err != nil {
			return db.Pengaturan{}, err
		}
		rekID = parsed
	}
	q := db.New(s.pool)
	row, err := q.UpdatePengaturan(ctx, db.UpdatePengaturanParams{
		NamaKoperasi:         strings.TrimSpace(in.NamaKoperasi),
		AlamatKoperasi:       strings.TrimSpace(in.AlamatKoperasi),
		TeleponKoperasi:      strings.TrimSpace(in.TeleponKoperasi),
		QrisUniqueDigits:     in.QRISUniqueDigits,
		OngkirTetap:          in.OngkirTetap,
		KebijakanTeks:        strings.TrimSpace(in.KebijakanTeks),
		ActiveRekeningQrisID: rekID,
	})
	if err != nil {
		return db.Pengaturan{}, err
	}
	_ = q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "update_pengaturan",
		Entitas:     "pengaturan",
		EntitasID:   "1",
		Ringkasan:   fmt.Sprintf("Pembaruan pengaturan koperasi %q", row.NamaKoperasi),
		IpAddress:   ip,
	})
	return row, nil
}

func (s *Service) ListUsers(ctx context.Context, lim, off int32) ([]map[string]interface{}, error) {
	rows, err := db.New(s.pool).ListUsers(ctx, db.ListUsersParams{Lim: lim, Off: off})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]interface{}{
			"id":            uuid.UUID(r.ID.Bytes),
			"nama_lengkap":  r.NamaLengkap,
			"peran":         r.Peran,
			"status_aktif":  r.StatusAktif,
			"mfa_enabled":   r.MfaEnabled,
			"email":         s.decryptUserField(r.EmailEnc, r.ID, "email"),
			"no_hp":         security.MaskPhone(s.decryptUserField(r.HpEnc, r.ID, "hp")),
			"anggota_id":    r.AnggotaID,
			"nomor_anggota": r.NomorAnggota,
			"created_at":    r.CreatedAt.Time,
		})
	}
	return out, nil
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

func (s *Service) persistNewUser(ctx context.Context, claims *middleware.SessionClaims, in CreateUserInput, pwdHash, ip string) (map[string]interface{}, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qt := db.New(tx)
	userID, err := uuid.NewV7()
	if err != nil {
		userID = uuid.New()
	}
	var emailEnc, hpEnc []byte
	var emailHash, hpHash pgtype.Text
	if em := strings.ToLower(strings.TrimSpace(in.Email)); em != "" {
		emailHash = pgtype.Text{String: security.HashBlindIndex(em, s.cfg.BlindIndexSecret), Valid: true}
		emailEnc, _ = s.cfg.KeyProvider.EncryptWithAAD([]byte(em), security.FormatAAD("users", "email", userID.String()))
	}
	normHP := security.NormalizePhone(in.NoHP)
	if normHP != "" {
		hpHash = pgtype.Text{String: security.HashBlindIndex(normHP, s.cfg.BlindIndexSecret), Valid: true}
		hpEnc, _ = s.cfg.KeyProvider.EncryptWithAAD([]byte(normHP), security.FormatAAD("users", "hp", userID.String()))
	}
	u, err := qt.CreateUser(ctx, db.CreateUserParams{
		ID:           response.ToPGUUID(userID),
		EmailEnc:     emailEnc,
		EmailHash:    emailHash,
		HpEnc:        hpEnc,
		HpHash:       hpHash,
		NamaLengkap:  strings.TrimSpace(in.NamaLengkap),
		PasswordHash: pwdHash,
		Peran:        in.Peran,
		StatusAktif:  true,
	})
	if err != nil {
		return nil, response.NewDomainError(http.StatusConflict, "USER_EXISTS", "Email atau nomor HP sudah terdaftar")
	}

	res := map[string]interface{}{"id": uuid.UUID(u.ID.Bytes), "nama_lengkap": u.NamaLengkap, "peran": u.Peran, "status_aktif": u.StatusAktif}
	if pwdHash == "" {
		code, waLink, err := CreateActivationOrResetCodeTx(ctx, tx, u.ID, response.ToPGUUID(claims.UserID), "aktivasi_awal", normHP, 24*time.Hour)
		if err != nil {
			return nil, err
		}
		res["activation_code"] = code
		res["wa_link"] = waLink
	}
	_ = qt.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID), ActorPeran: claims.Role,
		Aksi: "create_user", Entitas: "users", EntitasID: uuid.UUID(u.ID.Bytes).String(),
		Ringkasan: "Buat user peran=" + u.Peran, IpAddress: ip,
	})
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return res, nil
}
