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
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
)

func (s *Service) UpdateUser(ctx context.Context, claims *middleware.SessionClaims, idStr string, in UpdateUserInput, ip string) (map[string]interface{}, error) {
	pgUID, err := response.ParsePGUUID(idStr)
	if err != nil {
		return nil, err
	}
	if in.Peran != "" && !slices.Contains([]string{"admin", "bendahara", "petugas_toko", "anggota"}, in.Peran) {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_ROLE", "Peran tidak valid")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qt := db.New(tx)
	cur, err := qt.GetUserByID(ctx, pgUID)
	if err != nil {
		return nil, err
	}
	active := cur.StatusAktif
	if in.StatusAktif != nil {
		active = *in.StatusAktif
	}
	updated, err := qt.UpdateUserByAdmin(ctx, db.UpdateUserByAdminParams{
		ID:          pgUID,
		Peran:       strings.TrimSpace(in.Peran),
		StatusAktif: active,
		ResetMfa:    in.ResetMFA,
	})
	if err != nil {
		return nil, err
	}
	return s.buildAdminUpdateResult(ctx, tx, qt, claims, idStr, pgUID, cur, updated, in, ip)
}

func (s *Service) buildAdminUpdateResult(ctx context.Context, tx pgx.Tx, qt *db.Queries, claims *middleware.SessionClaims, idStr string, pgUID pgtype.UUID, cur db.User, updated db.UpdateUserByAdminRow, in UpdateUserInput, ip string) (map[string]interface{}, error) {
	if !updated.StatusAktif || in.ResetMFA {
		_ = qt.RevokeAllUserSessions(ctx, pgUID)
	}
	out := map[string]interface{}{
		"id":           uuid.UUID(updated.ID.Bytes),
		"nama_lengkap": updated.NamaLengkap,
		"peran":        updated.Peran,
		"status_aktif": updated.StatusAktif,
		"mfa_enabled":  updated.MfaEnabled,
	}
	if in.IssueResetCode {
		_ = qt.InvalidateUserActivationCodes(ctx, pgUID)
		hp := s.decryptUserField(cur.HpEnc, cur.ID, "hp")
		code, waLink, err := CreateActivationOrResetCodeTx(ctx, tx, pgUID, response.ToPGUUID(claims.UserID), "reset_password", hp, 24*time.Hour)
		if err != nil {
			return nil, err
		}
		out["reset_code"] = code
		out["wa_link"] = waLink
	}
	_ = qt.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID), ActorPeran: claims.Role,
		Aksi: "update_user", Entitas: "users", EntitasID: idStr,
		Ringkasan: fmt.Sprintf("Update user aktif=%t reset_mfa=%t", updated.StatusAktif, in.ResetMFA), IpAddress: ip,
	})
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
