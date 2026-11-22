package pengaturan

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	pengapi "github.com/kopdes/merah-putih/internal/pengaturan/api"
)

// Service mengelola pengaturan koperasi satu baris dan penulisan audit_log terpusat.
type Service struct {
	pool    *pgxpool.Pool
	trusted *middleware.TrustedProxyMatcher
}

func NewService(pool *pgxpool.Pool, trustedProxies []string) *Service {
	return &Service{
		pool:    pool,
		trusted: middleware.NewTrustedProxyMatcher(trustedProxies),
	}
}

type UpdateInput struct {
	NamaKoperasi         string  `json:"nama_koperasi"`
	AlamatKoperasi       string  `json:"alamat_koperasi"`
	TeleponKoperasi      string  `json:"telepon_koperasi"`
	QRISUniqueDigits     int32   `json:"qris_unique_digits"`
	OngkirTetap          int64   `json:"ongkir_tetap"`
	KebijakanTeks        string  `json:"kebijakan_teks"`
	ActiveRekeningQRISID *string `json:"active_rekening_qris_id"`
}

func mapSettings(row db.Pengaturan) pengapi.Settings {
	var rekID *uuid.UUID
	if row.ActiveRekeningQrisID.Valid {
		v := response.FromPGUUID(row.ActiveRekeningQrisID)
		rekID = &v
	}
	return pengapi.Settings{
		NamaKoperasi:         row.NamaKoperasi,
		AlamatKoperasi:       row.AlamatKoperasi,
		TeleponKoperasi:      row.TeleponKoperasi,
		QRISUniqueDigits:     row.QrisUniqueDigits,
		OngkirTetap:          row.OngkirTetap,
		KebijakanTeks:        row.KebijakanTeks,
		CatatanAkuntansi:     row.CatatanAkuntansi,
		ActiveRekeningQRISID: rekID,
		UpdatedAt:            row.UpdatedAt.Time,
	}
}

func (s *Service) GetSettings(ctx context.Context) (pengapi.Settings, error) {
	row, err := db.New(s.pool).GetPengaturan(ctx)
	if err != nil {
		return pengapi.Settings{}, err
	}
	return mapSettings(row), nil
}

func (s *Service) GetSettingsTx(ctx context.Context, tx pgx.Tx) (pengapi.Settings, error) {
	row, err := db.New(tx).GetPengaturan(ctx)
	if err != nil {
		return pengapi.Settings{}, err
	}
	return mapSettings(row), nil
}

func buildAuditParams(entry pengapi.AuditEntry) db.InsertAuditLogParams {
	var actorUID pgtype.UUID
	if entry.ActorUserID != nil && *entry.ActorUserID != uuid.Nil {
		actorUID = response.ToPGUUID(*entry.ActorUserID)
	}
	return db.InsertAuditLogParams{
		ActorUserID: actorUID,
		ActorPeran:  entry.ActorPeran,
		Aksi:        entry.Aksi,
		Entitas:     entry.Entitas,
		EntitasID:   entry.EntitasID,
		Ringkasan:   entry.Ringkasan,
		IpAddress:   entry.IPAddress,
	}
}

func (s *Service) Log(ctx context.Context, entry pengapi.AuditEntry) error {
	if s == nil || s.pool == nil {
		return nil
	}
	return db.New(s.pool).InsertAuditLog(ctx, buildAuditParams(entry))
}

func (s *Service) LogTx(ctx context.Context, tx pgx.Tx, entry pengapi.AuditEntry) error {
	if tx == nil {
		return s.Log(ctx, entry)
	}
	return db.New(tx).InsertAuditLog(ctx, buildAuditParams(entry))
}

func (s *Service) UpdatePengaturan(ctx context.Context, claims *middleware.SessionClaims, in UpdateInput, ip string) (db.Pengaturan, error) {
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
	row, err := db.New(s.pool).UpdatePengaturan(ctx, db.UpdatePengaturanParams{
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
	actorID := claims.UserID
	_ = s.Log(ctx, pengapi.AuditEntry{
		ActorUserID: &actorID,
		ActorPeran:  claims.Role,
		Aksi:        "update_pengaturan",
		Entitas:     "pengaturan",
		EntitasID:   "1",
		Ringkasan:   fmt.Sprintf("Pembaruan pengaturan koperasi %q", row.NamaKoperasi),
		IPAddress:   ip,
	})
	return row, nil
}

// AuditLogDTO merepresentasikan satu baris log audit aman untuk UI admin.
type AuditLogDTO struct {
	ID          uuid.UUID  `json:"id"`
	ActorUserID *uuid.UUID `json:"actor_user_id,omitempty"`
	ActorPeran  string     `json:"actor_peran"`
	Aksi        string     `json:"aksi"`
	Entitas     string     `json:"entitas"`
	EntitasID   string     `json:"entitas_id"`
	Ringkasan   string     `json:"ringkasan"`
	IPAddress   string     `json:"ip_address"`
	CreatedAt   time.Time  `json:"created_at"`
}

func mapAuditLogRow(row db.AuditLog) AuditLogDTO {
	var actorID *uuid.UUID
	if row.ActorUserID.Valid {
		v := response.FromPGUUID(row.ActorUserID)
		actorID = &v
	}
	return AuditLogDTO{
		ID:          response.FromPGUUID(row.ID),
		ActorUserID: actorID,
		ActorPeran:  row.ActorPeran,
		Aksi:        row.Aksi,
		Entitas:     row.Entitas,
		EntitasID:   row.EntitasID,
		Ringkasan:   row.Ringkasan,
		IPAddress:   row.IpAddress,
		CreatedAt:   row.CreatedAt.Time,
	}
}

// ListAuditLog mengambil daftar log audit terbaru dengan filter opsional entitas/aksi dan paginasi.
func (s *Service) ListAuditLog(ctx context.Context, entitas, aksi string, lim, off int32) ([]AuditLogDTO, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("pool database tidak tersedia")
	}
	rows, err := db.New(s.pool).ListAuditLog(ctx, db.ListAuditLogParams{
		EntitasFilter: strings.TrimSpace(entitas),
		AksiFilter:    strings.TrimSpace(aksi),
		Lim:           lim,
		Off:           off,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AuditLogDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapAuditLogRow(r))
	}
	return out, nil
}
