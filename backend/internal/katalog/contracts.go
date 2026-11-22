package katalog

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	katapi "github.com/kopdes/merah-putih/internal/katalog/api"
)

var _ katapi.StockPromoManager = (*Service)(nil)

func (s *Service) GetProductForOrderUpdateTx(ctx context.Context, tx pgx.Tx, productID uuid.UUID) (katapi.OrderProductInfo, error) {
	row, err := db.New(tx).GetProdukForUpdate(ctx, response.ToPGUUID(productID))
	if err != nil {
		return katapi.OrderProductInfo{}, err
	}
	var hu, ha *int64
	if row.HargaUmum.Valid {
		v := row.HargaUmum.Int64
		hu = &v
	}
	if row.HargaAnggota.Valid {
		v := row.HargaAnggota.Int64
		ha = &v
	}
	return katapi.OrderProductInfo{
		ID: response.FromPGUUID(row.ID), SKU: row.Sku, Nama: row.Nama,
		HargaUmum: hu, HargaAnggota: ha, Stok: row.Stok, StokTertahan: row.StokTertahan, Status: row.Status,
	}, nil
}

func (s *Service) ReserveStockTx(ctx context.Context, tx pgx.Tx, productID uuid.UUID, qty int32) error {
	_, err := db.New(tx).ReserveProdukStock(ctx, db.ReserveProdukStockParams{ID: response.ToPGUUID(productID), Qty: qty})
	if errors.Is(err, pgx.ErrNoRows) {
		return katapi.ErrOutOfStock
	}
	return err
}

func (s *Service) ReleaseStockHoldTx(ctx context.Context, tx pgx.Tx, productID uuid.UUID, qty int32) error {
	return db.New(tx).ReleaseProdukStockHold(ctx, db.ReleaseProdukStockHoldParams{ID: response.ToPGUUID(productID), Qty: qty})
}

func (s *Service) ConfirmReservedStockTx(ctx context.Context, tx pgx.Tx, productID uuid.UUID, qty int32) error {
	_, err := db.New(tx).ConfirmReservedProdukStock(ctx, db.ConfirmReservedProdukStockParams{ID: response.ToPGUUID(productID), Qty: qty})
	if errors.Is(err, pgx.ErrNoRows) {
		return katapi.ErrOutOfStock
	}
	return err
}

func (s *Service) DecrementAvailableStockTx(ctx context.Context, tx pgx.Tx, productID uuid.UUID, qty int32) error {
	_, err := db.New(tx).DecrementAvailableProdukStock(ctx, db.DecrementAvailableProdukStockParams{ID: response.ToPGUUID(productID), Qty: qty})
	if errors.Is(err, pgx.ErrNoRows) {
		return katapi.ErrOutOfStock
	}
	return err
}

func (s *Service) RestoreStockTx(ctx context.Context, tx pgx.Tx, productID uuid.UUID, qty int32) error {
	return db.New(tx).RestoreProdukStock(ctx, db.RestoreProdukStockParams{ID: response.ToPGUUID(productID), Qty: qty})
}

func (s *Service) GetPromoByCodeForUpdateTx(ctx context.Context, tx pgx.Tx, code string) (katapi.OrderPromoInfo, error) {
	pr, err := db.New(tx).GetPromoByKodeForUpdate(ctx, code)
	if err != nil {
		return katapi.OrderPromoInfo{}, err
	}
	return katapi.OrderPromoInfo{
		ID: response.FromPGUUID(pr.ID), Kode: pr.Kode, Nama: pr.Nama,
		TipeDiskon: pr.TipeDiskon, NilaiDiskon: pr.NilaiDiskon, MinBelanja: pr.MinBelanja,
		KuotaTotal: pr.KuotaTotal, KuotaTerpakai: pr.KuotaTerpakai,
		SekaliPerAnggota: pr.SekaliPerAnggota, MulaiPada: pr.MulaiPada.Time,
		BerakhirPada: pr.BerakhirPada.Time, Aktif: pr.Aktif,
	}, nil
}

func (s *Service) IncrementPromoUsedTx(ctx context.Context, tx pgx.Tx, promoID uuid.UUID) error {
	return db.New(tx).IncrementPromoUsed(ctx, response.ToPGUUID(promoID))
}

func (s *Service) DecrementPromoUsedTx(ctx context.Context, tx pgx.Tx, promoID uuid.UUID) error {
	return db.New(tx).DecrementPromoUsed(ctx, response.ToPGUUID(promoID))
}
