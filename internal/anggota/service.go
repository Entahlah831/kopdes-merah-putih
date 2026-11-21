package anggota

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	angapi "github.com/kopdes/merah-putih/internal/anggota/api"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	authapi "github.com/kopdes/merah-putih/internal/auth/api"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/filestore"
	pengapi "github.com/kopdes/merah-putih/internal/pengaturan/api"
	"github.com/kopdes/merah-putih/internal/security"
)

var (
	_ angapi.MemberReader = (*Service)(nil)
	_ angapi.ReportReader = (*Service)(nil)
)

type Config struct {
	KeyProvider         security.KeyProvider
	BlindIndexSecret    []byte
	TrustedProxies      []string
	RegistrationEnabled bool
	CaptchaEnabled      bool
	CaptchaSecretKey    string
	TurnstileVerifyURL  string
	RetentionDays       int
	MaxKYCUploadBytes   int64
	HTTPClient          *http.Client
	FileStore           filestore.FileStore
	UserManager         authapi.UserManager
	Audit               pengapi.AuditWriter
}

type Service struct {
	pool    *pgxpool.Pool
	cfg     Config
	trusted *middleware.TrustedProxyMatcher
}

func NewService(pool *pgxpool.Pool, cfg Config) *Service {
	if cfg.TurnstileVerifyURL == "" {
		cfg.TurnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 30
	}
	if cfg.MaxKYCUploadBytes <= 0 {
		cfg.MaxKYCUploadBytes = 5 * 1024 * 1024
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Service{
		pool:    pool,
		cfg:     cfg,
		trusted: middleware.NewTrustedProxyMatcher(cfg.TrustedProxies),
	}
}

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

func uuidPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	v := id
	return &v
}

func (s *Service) GetByUserID(ctx context.Context, userID uuid.UUID) (angapi.MemberInfo, error) {
	row, err := db.New(s.pool).GetAnggotaByUserID(ctx, response.ToPGUUID(userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return angapi.MemberInfo{}, angapi.ErrNotFound
	}
	if err != nil {
		return angapi.MemberInfo{}, err
	}
	var uidPtr *uuid.UUID
	if row.UserID.Valid {
		v := response.FromPGUUID(row.UserID)
		uidPtr = &v
	}
	return angapi.MemberInfo{
		ID:               response.FromPGUUID(row.ID),
		UserID:           uidPtr,
		NomorAnggota:     row.NomorAnggota,
		NamaLengkap:      row.NamaLengkap,
		Status:           row.Status,
		TanggalBergabung: row.TanggalBergabung.Time,
	}, nil
}

func (s *Service) LookupByUserIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]angapi.MemberSummary, error) {
	out := make(map[uuid.UUID]angapi.MemberSummary, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	pgIDs := make([]pgtype.UUID, 0, len(userIDs))
	for _, u := range userIDs {
		pgIDs = append(pgIDs, response.ToPGUUID(u))
	}
	rows, err := db.New(s.pool).ListAnggotaSummariesByUserIDs(ctx, pgIDs)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		if !r.UserID.Valid {
			continue
		}
		uid := response.FromPGUUID(r.UserID)
		uidCopy := uid
		out[uid] = angapi.MemberSummary{
			ID: response.FromPGUUID(r.ID), UserID: &uidCopy, NomorAnggota: r.NomorAnggota,
			NamaLengkap: r.NamaLengkap, Status: r.Status,
		}
	}
	return out, nil
}

func (s *Service) GetSummariesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]angapi.MemberSummary, error) {
	out := make(map[uuid.UUID]angapi.MemberSummary, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	pgIDs := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		pgIDs = append(pgIDs, response.ToPGUUID(id))
	}
	rows, err := db.New(s.pool).ListAnggotaSummariesByIDs(ctx, pgIDs)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		aid := response.FromPGUUID(r.ID)
		var uidPtr *uuid.UUID
		if r.UserID.Valid {
			v := response.FromPGUUID(r.UserID)
			uidPtr = &v
		}
		out[aid] = angapi.MemberSummary{
			ID: aid, UserID: uidPtr, NomorAnggota: r.NomorAnggota,
			NamaLengkap: r.NamaLengkap, Status: r.Status,
		}
	}
	return out, nil
}

func (s *Service) ListActiveMemberIDs(ctx context.Context) ([]uuid.UUID, error) {
	pgIDs, err := db.New(s.pool).ListActiveAnggotaIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(pgIDs))
	for _, p := range pgIDs {
		out = append(out, response.FromPGUUID(p))
	}
	return out, nil
}

func (s *Service) GetMemberSummary(ctx context.Context) ([]angapi.MemberStatusCount, error) {
	rows, err := db.New(s.pool).LaporanRingkasanAnggota(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]angapi.MemberStatusCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, angapi.MemberStatusCount{Status: r.Status, Jumlah: r.Jumlah})
	}
	return out, nil
}

func (s *Service) List(ctx context.Context, statusFilter string, lim, off int32) ([]db.ListAnggotaRow, error) {
	return db.New(s.pool).ListAnggota(ctx, db.ListAnggotaParams{
		StatusFilter: strings.TrimSpace(statusFilter), Lim: lim, Off: off,
	})
}

func (s *Service) ExportCSV(ctx context.Context, statusFilter string) ([]byte, error) {
	rows, err := s.List(ctx, statusFilter, 5000, 0)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"nomor_anggota", "nama_lengkap", "nik_masked", "hp_masked", "status", "tanggal_bergabung"})
	for _, r := range rows {
		tgl := ""
		if r.TanggalBergabung.Valid {
			tgl = r.TanggalBergabung.Time.Format("2006-01-02")
		}
		_ = w.Write([]string{
			security.NeutralizeCSVCell(r.NomorAnggota), security.NeutralizeCSVCell(r.NamaLengkap),
			security.NeutralizeCSVCell(r.NikMasked), security.NeutralizeCSVCell(r.HpMasked),
			security.NeutralizeCSVCell(r.Status), security.NeutralizeCSVCell(tgl),
		})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func (s *Service) GetDetail(ctx context.Context, claims *middleware.SessionClaims, idStr string, unmask bool, ip string) (map[string]interface{}, error) {
	pgID, err := response.ParsePGUUID(idStr)
	if err != nil {
		return nil, err
	}
	if claims.Role == "anggota" && (claims.AnggotaID == nil || claims.AnggotaID.String() != idStr) {
		return nil, response.NewDomainError(http.StatusForbidden, "FORBIDDEN", "Anggota hanya boleh mengakses profilnya sendiri")
	}
	if unmask && claims.Role != "admin" {
		return nil, response.NewDomainError(http.StatusForbidden, "UNMASK_FORBIDDEN", "Hanya admin yang dapat membuka masking PII anggota")
	}
	row, err := db.New(s.pool).GetAnggotaByID(ctx, pgID)
	if err != nil {
		return nil, err
	}
	peran := s.resolveUserRole(ctx, row.UserID)
	out := map[string]interface{}{
		"id": uuid.UUID(row.ID.Bytes), "nomor_anggota": row.NomorAnggota, "nama_lengkap": row.NamaLengkap,
		"nik_masked": row.NikMasked, "hp_masked": row.HpMasked, "status": row.Status,
		"alasan_status": row.AlasanStatus, "peran": peran,
		"tanggal_bergabung": row.TanggalBergabung.Time.Format("2006-01-02"),
	}
	if unmask && s.cfg.KeyProvider != nil {
		s.appendUnmaskedFields(ctx, out, row, claims, idStr, ip)
	}
	return out, nil
}

func (s *Service) resolveUserRole(ctx context.Context, userID pgtype.UUID) string {
	if !userID.Valid || s.cfg.UserManager == nil {
		return "anggota"
	}
	if role, err := s.cfg.UserManager.GetUserRoleByID(ctx, response.FromPGUUID(userID)); err == nil && role != "" {
		return role
	}
	return "anggota"
}

func (s *Service) appendUnmaskedFields(ctx context.Context, out map[string]interface{}, row db.GetAnggotaByIDRow, claims *middleware.SessionClaims, idStr, ip string) {
	dec := func(ct []byte, col string) string {
		if len(ct) == 0 {
			return ""
		}
		pt, err := s.cfg.KeyProvider.DecryptWithAAD(ct, security.FormatAAD("anggota", col, idStr))
		if err != nil {
			return ""
		}
		return string(pt)
	}
	out["nik"] = dec(row.NikEnc, "nik")
	out["no_hp"] = dec(row.HpEnc, "hp")
	out["tanggal_lahir"] = dec(row.TanggalLahirEnc, "tanggal_lahir")
	out["alamat"] = dec(row.AlamatEnc, "alamat")
	s.logAudit(ctx, uuidPtr(claims.UserID), claims.Role, "unmask_anggota", "anggota", idStr,
		"Admin membuka masking data PII anggota", ip)
}

type UpdateStatusInput struct {
	Status string `json:"status"`
	Alasan string `json:"alasan"`
}

func (s *Service) UpdateStatus(ctx context.Context, claims *middleware.SessionClaims, idStr string, in UpdateStatusInput, ip string) (db.UpdateAnggotaStatusRow, error) {
	var zero db.UpdateAnggotaStatusRow
	if !slices.Contains([]string{"aktif", "nonaktif", "keluar"}, in.Status) {
		return zero, response.NewDomainError(http.StatusBadRequest, "INVALID_STATUS", "status wajib aktif, nonaktif, atau keluar")
	}
	pgID, err := response.ParsePGUUID(idStr)
	if err != nil {
		return zero, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	updated, err := db.New(tx).UpdateAnggotaStatus(ctx, db.UpdateAnggotaStatusParams{
		ID: pgID, Status: in.Status, AlasanStatus: strings.TrimSpace(in.Alasan),
	})
	if err != nil {
		return zero, err
	}
	if updated.UserID.Valid && s.cfg.UserManager != nil {
		if err := s.cfg.UserManager.SetUserActiveAndRevokeTx(ctx, tx, response.FromPGUUID(updated.UserID), in.Status == "aktif"); err != nil {
			return zero, err
		}
	}
	s.logAuditTx(ctx, tx, uuidPtr(claims.UserID), claims.Role, "update_status_anggota", "anggota", idStr,
		fmt.Sprintf("Ubah status ke %s", in.Status), ip)
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	return updated, nil
}
