package iuran

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	angapi "github.com/kopdes/merah-putih/internal/anggota/api"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	iurapi "github.com/kopdes/merah-putih/internal/iuran/api"
)

var (
	_ iurapi.BillPayer    = (*Service)(nil)
	_ iurapi.ReportReader = (*Service)(nil)
)

func (s *Service) LockPayableBillTx(ctx context.Context, tx pgx.Tx, billID uuid.UUID) (iurapi.PayableBill, error) {
	t, err := db.New(tx).GetTagihanIuranForUpdate(ctx, response.ToPGUUID(billID))
	if err != nil {
		return iurapi.PayableBill{}, err
	}
	return iurapi.PayableBill{
		ID:                response.FromPGUUID(t.ID),
		AnggotaID:         response.FromPGUUID(t.AnggotaID),
		JenisIuranID:      response.FromPGUUID(t.JenisIuranID),
		PeriodeTahun:      t.PeriodeTahun,
		PeriodeBulan:      t.PeriodeBulan,
		Nominal:           t.Nominal,
		NominalKeringanan: t.NominalKeringanan,
		Status:            t.Status,
	}, nil
}

func (s *Service) UpdateBillStatusTx(ctx context.Context, tx pgx.Tx, billID uuid.UUID, status string) error {
	return db.New(tx).UpdateTagihanStatus(ctx, db.UpdateTagihanStatusParams{
		Status: status,
		ID:     response.ToPGUUID(billID),
	})
}

func (s *Service) GetArrearsReport(ctx context.Context) ([]iurapi.TunggakanReportRow, error) {
	rows, err := s.q.LaporanTunggakanIuran(ctx)
	if err != nil {
		return nil, err
	}
	memberMap := s.lookupMembersMap(ctx, extractAnggotaIDsFromReport(rows))
	out := make([]iurapi.TunggakanReportRow, 0, len(rows))
	for _, r := range rows {
		m := memberMap[response.FromPGUUID(r.AnggotaID)]
		out = append(out, iurapi.TunggakanReportRow{
			NomorAnggota:      m.NomorAnggota,
			NamaLengkap:       m.NamaLengkap,
			JenisKode:         r.JenisKode,
			JenisNama:         r.JenisNama,
			PeriodeTahun:      r.PeriodeTahun,
			PeriodeBulan:      r.PeriodeBulan,
			NominalTerutang:   r.NominalTerutang,
			TanggalJatuhTempo: r.TanggalJatuhTempo.Time.Format("2006-01-02"),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TanggalJatuhTempo != out[j].TanggalJatuhTempo {
			return out[i].TanggalJatuhTempo < out[j].TanggalJatuhTempo
		}
		return out[i].NomorAnggota < out[j].NomorAnggota
	})
	return out, nil
}

func extractAnggotaIDsFromReport(rows []db.LaporanTunggakanIuranRow) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		id := response.FromPGUUID(r.AnggotaID)
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *Service) lookupMembersMap(ctx context.Context, ids []uuid.UUID) map[uuid.UUID]angapi.MemberSummary {
	if s.members == nil || len(ids) == 0 {
		return map[uuid.UUID]angapi.MemberSummary{}
	}
	res, err := s.members.GetSummariesByIDs(ctx, ids)
	if err != nil {
		return map[uuid.UUID]angapi.MemberSummary{}
	}
	return res
}
