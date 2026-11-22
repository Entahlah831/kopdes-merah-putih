package pesanan

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	katapi "github.com/kopdes/merah-putih/internal/katalog/api"
	pesapi "github.com/kopdes/merah-putih/internal/pesanan/api"
)

var (
	_ pesapi.OrderPayer   = (*Service)(nil)
	_ pesapi.ReportReader = (*Service)(nil)
)

func (s *Service) LockPayableOrderTx(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (pesapi.PayableOrder, error) {
	ord, err := db.New(tx).GetPesananForUpdate(ctx, response.ToPGUUID(orderID))
	if err != nil {
		return pesapi.PayableOrder{}, err
	}
	var angPtr, promoPtr *uuid.UUID
	if ord.AnggotaID.Valid {
		v := response.FromPGUUID(ord.AnggotaID)
		angPtr = &v
	}
	if ord.PromoID.Valid {
		v := response.FromPGUUID(ord.PromoID)
		promoPtr = &v
	}
	var holdPtr, expPtr *time.Time
	if ord.StokHoldExpiresAt.Valid {
		t := ord.StokHoldExpiresAt.Time
		holdPtr = &t
	}
	if ord.ExpiresAt.Valid {
		t := ord.ExpiresAt.Time
		expPtr = &t
	}
	return pesapi.PayableOrder{
		ID: response.FromPGUUID(ord.ID), NomorPesanan: ord.NomorPesanan,
		UserID: response.FromPGUUID(ord.UserID), AnggotaID: angPtr, PromoID: promoPtr,
		MetodePengiriman: ord.MetodePengiriman, MetodePembayaran: ord.MetodePembayaran,
		Status: ord.Status, StokStatus: ord.StokStatus,
		StokHoldExpiresAt: holdPtr, TotalBayar: ord.TotalBayar, ExpiresAt: expPtr,
	}, nil
}

func (s *Service) ConfirmPaidOrderTx(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) error {
	qtx := db.New(tx)
	pgID := response.ToPGUUID(orderID)
	ord, err := qtx.GetPesananForUpdate(ctx, pgID)
	if err != nil {
		return err
	}
	if ord.Status == "batal" {
		return response.NewDomainError(http.StatusConflict, "PESANAN_ALREADY_CANCELLED", "Pesanan sudah dibatalkan")
	}
	if ord.StokStatus != "deducted" && (ord.Status == "baru" || ord.Status == "menunggu_pembayaran" || ord.Status == "menunggu_verifikasi") {
		if err := s.confirmOrderStockOnPayTx(ctx, tx, qtx, pgID, ord.StokStatus); err != nil {
			return err
		}
		if err := qtx.UpdatePesananStokStatus(ctx, db.UpdatePesananStokStatusParams{ID: pgID, StokStatus: "deducted"}); err != nil {
			return err
		}
	}
	_, err = qtx.UpdatePesananStatus(ctx, db.UpdatePesananStatusParams{Status: "dibayar", ID: pgID})
	return err
}

func (s *Service) confirmOrderStockOnPayTx(ctx context.Context, tx pgx.Tx, qtx *db.Queries, pgID pgtype.UUID, stokStatus string) error {
	if s.cfg.Stock == nil {
		return response.NewDomainError(http.StatusInternalServerError, "STOCK_MANAGER_UNAVAILABLE", "Pengelola stok katalog belum siap")
	}
	items, err := qtx.ListPesananItems(ctx, pgID)
	if err != nil {
		return err
	}
	for _, it := range items {
		pid := response.FromPGUUID(it.ProdukID)
		if stokStatus == "reserved" {
			err = s.cfg.Stock.ConfirmReservedStockTx(ctx, tx, pid, it.Kuantitas)
		} else {
			err = s.cfg.Stock.DecrementAvailableStockTx(ctx, tx, pid, it.Kuantitas)
		}
		if err != nil {
			if errors.Is(err, katapi.ErrOutOfStock) {
				return &pesapi.StockRunoutError{ProductName: it.NamaProduk}
			}
			return err
		}
	}
	return nil
}

func (s *Service) CancelOrderAfterRefundTx(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, alasan string) error {
	qtx := db.New(tx)
	pgID := response.ToPGUUID(orderID)
	ord, err := qtx.GetPesananForUpdate(ctx, pgID)
	if err != nil || ord.Status == "batal" {
		return err
	}
	if err := s.releaseOrRestoreOrderResourcesTx(ctx, tx, qtx, pgID, ord.PromoID, ord.StokStatus, ord.StokStatus == "deducted"); err != nil {
		return err
	}
	_, err = qtx.UpdatePesananStatus(ctx, db.UpdatePesananStatusParams{
		Status: "batal", Catatan: "Refund selesai: " + alasan, ID: pgID,
	})
	return err
}

func (s *Service) GetDailySalesReport(ctx context.Context, dari, sampai *time.Time) ([]pesapi.SalesReportRow, error) {
	var dariPG, sampaiPG pgtype.Date
	if dari != nil {
		dariPG = response.ToPGDate(*dari)
	}
	if sampai != nil {
		sampaiPG = response.ToPGDate(*sampai)
	}
	rows, err := s.q.LaporanPenjualanHarian(ctx, db.LaporanPenjualanHarianParams{
		Tz: s.timezoneOrDefault(), Dari: dariPG, Sampai: sampaiPG,
	})
	if err != nil {
		return nil, err
	}
	out := make([]pesapi.SalesReportRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, pesapi.SalesReportRow{
			Tanggal: r.Tanggal.Time.Format("2006-01-02"), JumlahPesanan: r.JumlahPesanan,
			TotalSubtotal: r.TotalSubtotal, TotalDiskon: r.TotalDiskon,
			TotalOngkir: r.TotalOngkir, TotalOmzet: r.TotalOmzet,
		})
	}
	return out, nil
}
