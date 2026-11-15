package iuran

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
)

type Service struct {
	pool    *pgxpool.Pool
	q       *db.Queries
	trusted *middleware.TrustedProxyMatcher
}

func NewService(pool *pgxpool.Pool, trusted []string) *Service {
	return &Service{
		pool:    pool,
		q:       db.New(pool),
		trusted: middleware.NewTrustedProxyMatcher(trusted),
	}
}

type CreateJenisInput struct {
	Action               string `json:"action,omitempty"`
	Periode              string `json:"periode,omitempty"`
	Kode                 string `json:"kode"`
	Nama                 string `json:"nama"`
	Nominal              int64  `json:"nominal"`
	HariJatuhTempo       int32  `json:"hari_jatuh_tempo"`
	KlasifikasiAkuntansi string `json:"klasifikasi_akuntansi"` // KEPUTUSAN AKUNTAN: wajib diverifikasi akuntan koperasi
}

type JenisDTO struct {
	ID                   uuid.UUID `json:"id"`
	Kode                 string    `json:"kode"`
	Nama                 string    `json:"nama"`
	Nominal              int64     `json:"nominal"`
	HariJatuhTempo       int32     `json:"hari_jatuh_tempo"`
	KlasifikasiAkuntansi string    `json:"klasifikasi_akuntansi"`
	Aktif                bool      `json:"aktif"`
}

type TagihanDTO struct {
	ID                uuid.UUID  `json:"id"`
	AnggotaID         uuid.UUID  `json:"anggota_id"`
	NomorAnggota      string     `json:"nomor_anggota"`
	NamaLengkap       string     `json:"nama_lengkap"`
	JenisKode         string     `json:"jenis_kode"`
	JenisNama         string     `json:"jenis_nama"`
	PeriodeTahun      int32      `json:"periode_tahun"`
	PeriodeBulan      int32      `json:"periode_bulan"`
	Nominal           int64      `json:"nominal"`
	NominalKeringanan int64      `json:"nominal_keringanan"`
	NominalNetto      int64      `json:"nominal_netto"`
	TanggalJatuhTempo string     `json:"tanggal_jatuh_tempo"`
	Status            string     `json:"status"`
	AlasanKeringanan  string     `json:"alasan_keringanan,omitempty"`
	PaidAt            *time.Time `json:"paid_at,omitempty"`
}

type GenerateResult struct {
	PeriodeTahun int32 `json:"periode_tahun"`
	PeriodeBulan int32 `json:"periode_bulan"`
	Dibuat       int   `json:"dibuat"`
	Dilewati     int   `json:"dilewati"`
}

type KeringananInput struct {
	NominalKeringanan int64  `json:"nominal_keringanan"`
	Alasan            string `json:"alasan"`
}

func (s *Service) CreateJenisOrGenerate(ctx context.Context, claims *middleware.SessionClaims, in CreateJenisInput, ip string) (interface{}, error) {
	if strings.EqualFold(strings.TrimSpace(in.Action), "generate") {
		th, bl, err := ParsePeriode(in.Periode)
		if err != nil {
			return nil, err
		}
		res, err := s.GenerateMonthlyBills(ctx, th, bl)
		if err != nil {
			return nil, err
		}
		_ = s.q.InsertAuditLog(ctx, db.InsertAuditLogParams{
			ActorUserID: response.ToPGUUID(claims.UserID),
			ActorPeran:  claims.Role,
			Aksi:        "iuran.generate",
			Entitas:     "tagihan_iuran",
			EntitasID:   fmt.Sprintf("%04d-%02d", th, bl),
			Ringkasan:   fmt.Sprintf("Generate tagihan %04d-%02d: %d dibuat", th, bl, res.Dibuat),
			IpAddress:   ip,
		})
		return res, nil
	}
	return s.createJenis(ctx, claims, in, ip)
}

func (s *Service) createJenis(ctx context.Context, claims *middleware.SessionClaims, in CreateJenisInput, ip string) (*JenisDTO, error) {
	kode := strings.ToUpper(strings.TrimSpace(in.Kode))
	nama := strings.TrimSpace(in.Nama)
	if kode == "" || nama == "" || in.Nominal <= 0 {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_IURAN_INPUT", "Kode, nama, dan nominal iuran (> 0) wajib valid")
	}
	hari := in.HariJatuhTempo
	if hari <= 0 {
		hari = 10
	}
	if hari > 28 {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_DUE_DAY", "Hari jatuh tempo harus antara 1 sampai 28")
	}
	klasifikasi := strings.TrimSpace(in.KlasifikasiAkuntansi)
	if klasifikasi == "" {
		// KEPUTUSAN AKUNTAN: klasifikasi default sebelum penetapan akuntan koperasi
		klasifikasi = "kewajiban_atau_modal_iuran_keputusan_akuntan"
	}
	row, err := s.q.CreateJenisIuran(ctx, db.CreateJenisIuranParams{
		Kode:                 kode,
		Nama:                 nama,
		Nominal:              in.Nominal,
		HariJatuhTempo:       hari,
		KlasifikasiAkuntansi: klasifikasi,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, response.NewDomainError(http.StatusConflict, "DUPLICATE_KODE_IURAN", "Kode jenis iuran sudah terdaftar")
		}
		return nil, fmt.Errorf("buat jenis iuran: %w", err)
	}
	_ = s.q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "iuran.jenis_create",
		Entitas:     "jenis_iuran",
		EntitasID:   response.FromPGUUID(row.ID).String(),
		Ringkasan:   "Tambah jenis iuran " + row.Kode,
		IpAddress:   ip,
	})
	return &JenisDTO{
		ID:                   response.FromPGUUID(row.ID),
		Kode:                 row.Kode,
		Nama:                 row.Nama,
		Nominal:              row.Nominal,
		HariJatuhTempo:       row.HariJatuhTempo,
		KlasifikasiAkuntansi: row.KlasifikasiAkuntansi,
		Aktif:                row.Aktif,
	}, nil
}

func (s *Service) ListJenisAktif(ctx context.Context) ([]JenisDTO, error) {
	rows, err := s.q.ListJenisIuranAktif(ctx)
	if err != nil {
		return nil, fmt.Errorf("ambil jenis iuran: %w", err)
	}
	out := make([]JenisDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, JenisDTO{
			ID:                   response.FromPGUUID(r.ID),
			Kode:                 r.Kode,
			Nama:                 r.Nama,
			Nominal:              r.Nominal,
			HariJatuhTempo:       r.HariJatuhTempo,
			KlasifikasiAkuntansi: r.KlasifikasiAkuntansi,
			Aktif:                r.Aktif,
		})
	}
	return out, nil
}

func (s *Service) GenerateMonthlyBills(ctx context.Context, tahun, bulan int32) (*GenerateResult, error) {
	if tahun < 2020 || tahun > 2100 || bulan < 1 || bulan > 12 {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_PERIODE", "Periode harus valid (YYYY-MM)")
	}
	jenisList, err := s.q.ListJenisIuranAktif(ctx)
	if err != nil {
		return nil, fmt.Errorf("ambil jenis iuran aktif: %w", err)
	}
	anggotaIDs, err := s.q.ListAktifAnggotaIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("ambil daftar anggota aktif: %w", err)
	}
	res := &GenerateResult{PeriodeTahun: tahun, PeriodeBulan: bulan}
	for _, j := range jenisList {
		due := time.Date(int(tahun), time.Month(bulan), int(j.HariJatuhTempo), 0, 0, 0, 0, time.UTC)
		for _, aID := range anggotaIDs {
			_, insErr := s.q.InsertTagihanIuranIfMissing(ctx, db.InsertTagihanIuranIfMissingParams{
				AnggotaID:         aID,
				JenisIuranID:      j.ID,
				PeriodeTahun:      tahun,
				PeriodeBulan:      bulan,
				Nominal:           j.Nominal,
				TanggalJatuhTempo: response.ToPGDate(due),
			})
			if errors.Is(insErr, pgx.ErrNoRows) {
				res.Dilewati++
				continue
			}
			if insErr != nil {
				return nil, fmt.Errorf("insert tagihan iuran: %w", insErr)
			}
			res.Dibuat++
		}
	}
	return res, nil
}

func (s *Service) ListTagihan(ctx context.Context, claims *middleware.SessionClaims, scope, statusFilter string, lim, off int32) ([]TagihanDTO, error) {
	var anggotaFilter pgtype.UUID
	onlyTunggakan := false
	if claims.Role == "anggota" || strings.EqualFold(scope, "saya") {
		ag, err := s.q.GetAnggotaByUserID(ctx, response.ToPGUUID(claims.UserID))
		if errors.Is(err, pgx.ErrNoRows) {
			return []TagihanDTO{}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("cari anggota user: %w", err)
		}
		anggotaFilter = ag.ID
	} else if strings.EqualFold(scope, "tunggakan") {
		onlyTunggakan = true
	}
	rows, err := s.q.ListTagihanIuran(ctx, db.ListTagihanIuranParams{
		AnggotaFilter: anggotaFilter,
		OnlyTunggakan: onlyTunggakan,
		StatusFilter:  strings.TrimSpace(statusFilter),
		Lim:           lim,
		Off:           off,
	})
	if err != nil {
		return nil, fmt.Errorf("list tagihan iuran: %w", err)
	}
	return mapTagihanRows(rows), nil
}

func mapTagihanRows(rows []db.ListTagihanIuranRow) []TagihanDTO {
	out := make([]TagihanDTO, 0, len(rows))
	for _, r := range rows {
		dto := TagihanDTO{
			ID:                response.FromPGUUID(r.ID),
			AnggotaID:         response.FromPGUUID(r.AnggotaID),
			NomorAnggota:      r.NomorAnggota,
			NamaLengkap:       r.NamaLengkap,
			JenisKode:         r.JenisKode,
			JenisNama:         r.JenisNama,
			PeriodeTahun:      r.PeriodeTahun,
			PeriodeBulan:      r.PeriodeBulan,
			Nominal:           r.Nominal,
			NominalKeringanan: r.NominalKeringanan,
			NominalNetto:      r.NominalNetto,
			TanggalJatuhTempo: r.TanggalJatuhTempo.Time.Format("2006-01-02"),
			Status:            r.Status,
			AlasanKeringanan:  r.AlasanKeringanan,
		}
		if r.PaidAt.Valid {
			t := r.PaidAt.Time
			dto.PaidAt = &t
		}
		out = append(out, dto)
	}
	return out
}

func (s *Service) ApplyKeringanan(ctx context.Context, claims *middleware.SessionClaims, idStr string, in KeringananInput, ip string) (*db.ApplyTagihanKeringananRow, error) {
	idPG, err := response.ParsePGUUID(idStr)
	if err != nil {
		return nil, err
	}
	alasan := strings.TrimSpace(in.Alasan)
	if alasan == "" || in.NominalKeringanan < 0 {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_KERINGANAN", "Nominal keringanan >= 0 dan alasan wajib diisi")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	t, err := qtx.GetTagihanIuranForUpdate(ctx, idPG)
	if err != nil {
		return nil, err
	}
	if t.Status != "belum_bayar" {
		return nil, response.NewDomainError(http.StatusConflict, "INVALID_TAGIHAN_STATUS", "Keringanan hanya dapat diberikan pada tagihan berstatus belum_bayar")
	}
	if in.NominalKeringanan > t.Nominal {
		return nil, response.NewDomainError(http.StatusBadRequest, "KERINGANAN_MELEBIHI_NOMINAL", "Nominal keringanan tidak boleh melebihi nominal tagihan")
	}
	updated, err := qtx.ApplyTagihanKeringanan(ctx, db.ApplyTagihanKeringananParams{
		NominalKeringanan: in.NominalKeringanan,
		AlasanKeringanan:  alasan,
		DisetujuiOleh:     response.ToPGUUID(claims.UserID),
		ID:                idPG,
	})
	if err != nil {
		return nil, fmt.Errorf("simpan keringanan: %w", err)
	}
	_ = qtx.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "iuran.keringanan",
		Entitas:     "tagihan_iuran",
		EntitasID:   idStr,
		Ringkasan:   fmt.Sprintf("Keringanan %d -> status %s", in.NominalKeringanan, updated.Status),
		IpAddress:   ip,
	})
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &updated, nil
}

func ParsePeriode(s string) (int32, int32, error) {
	parts := strings.Split(strings.TrimSpace(s), "-")
	if len(parts) != 2 {
		return 0, 0, response.NewDomainError(http.StatusBadRequest, "INVALID_PERIODE", "Format periode wajib YYYY-MM")
	}
	th, err1 := strconv.Atoi(parts[0])
	bl, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || th < 2020 || th > 2100 || bl < 1 || bl > 12 {
		return 0, 0, response.NewDomainError(http.StatusBadRequest, "INVALID_PERIODE", "Format periode wajib YYYY-MM")
	}
	return int32(th), int32(bl), nil
}
