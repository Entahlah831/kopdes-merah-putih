package anggota

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

type Config struct {
	KeyProvider      security.KeyProvider
	BlindIndexSecret []byte
	TrustedProxies   []string
}

type Service struct {
	pool    *pgxpool.Pool
	cfg     Config
	trusted *middleware.TrustedProxyMatcher
}

func NewService(pool *pgxpool.Pool, cfg Config) *Service {
	return &Service{
		pool:    pool,
		cfg:     cfg,
		trusted: middleware.NewTrustedProxyMatcher(cfg.TrustedProxies),
	}
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
			security.NeutralizeCSVCell(r.NomorAnggota),
			security.NeutralizeCSVCell(r.NamaLengkap),
			security.NeutralizeCSVCell(r.NikMasked),
			security.NeutralizeCSVCell(r.HpMasked),
			security.NeutralizeCSVCell(r.Status),
			security.NeutralizeCSVCell(tgl),
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

	q := db.New(s.pool)
	row, err := q.GetAnggotaByID(ctx, pgID)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"id":                uuid.UUID(row.ID.Bytes),
		"nomor_anggota":     row.NomorAnggota,
		"nama_lengkap":      row.NamaLengkap,
		"nik_masked":        row.NikMasked,
		"hp_masked":         row.HpMasked,
		"status":            row.Status,
		"alasan_status":     row.AlasanStatus,
		"peran":             row.Peran,
		"tanggal_bergabung": row.TanggalBergabung.Time.Format("2006-01-02"),
	}
	if unmask && s.cfg.KeyProvider != nil {
		s.appendUnmaskedFields(ctx, q, out, row, claims, idStr, ip)
	}
	return out, nil
}

func (s *Service) appendUnmaskedFields(ctx context.Context, q *db.Queries, out map[string]interface{}, row db.GetAnggotaByIDRow, claims *middleware.SessionClaims, idStr, ip string) {
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
	_ = q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID), ActorPeran: claims.Role,
		Aksi: "unmask_anggota", Entitas: "anggota", EntitasID: idStr,
		Ringkasan: "Admin membuka masking data PII anggota", IpAddress: ip,
	})
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

	qt := db.New(tx)
	updated, err := qt.UpdateAnggotaStatus(ctx, db.UpdateAnggotaStatusParams{
		ID: pgID, Status: in.Status, AlasanStatus: strings.TrimSpace(in.Alasan),
	})
	if err != nil {
		return zero, err
	}
	if updated.UserID.Valid {
		isActive := in.Status == "aktif"
		_ = qt.DeactivateUserByID(ctx, db.DeactivateUserByIDParams{ID: updated.UserID, StatusAktif: isActive})
		if !isActive {
			_ = qt.RevokeAllUserSessions(ctx, updated.UserID)
		}
	}
	_ = qt.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID), ActorPeran: claims.Role,
		Aksi: "update_status_anggota", Entitas: "anggota", EntitasID: idStr,
		Ringkasan: fmt.Sprintf("Ubah status ke %s", in.Status), IpAddress: ip,
	})
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	return updated, nil
}

var _ = pgtype.UUID{}
