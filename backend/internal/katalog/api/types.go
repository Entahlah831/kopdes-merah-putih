package api

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrOutOfStock menandakan stok atau stok tersedia produk tidak mencukupi.
var ErrOutOfStock = errors.New("stok produk tidak mencukupi")

// OrderProductInfo berisi informasi produk terkunci FOR UPDATE saat checkout.
type OrderProductInfo struct {
	ID           uuid.UUID
	SKU          string
	Nama         string
	HargaUmum    *int64
	HargaAnggota *int64
	Stok         int32
	StokTertahan int32
	Status       string
}

// OrderPromoInfo berisi informasi promo terkunci FOR UPDATE saat checkout.
type OrderPromoInfo struct {
	ID               uuid.UUID
	Kode             string
	Nama             string
	TipeDiskon       string
	NilaiDiskon      int64
	MinBelanja       int64
	KuotaTotal       int32
	KuotaTerpakai    int32
	SekaliPerAnggota bool
	MulaiPada        time.Time
	BerakhirPada     time.Time
	Aktif            bool
}

// StockPromoManager mengelola reservasi, konfirmasi, dan pemulihan stok serta kuota promo dalam tx pesanan.
type StockPromoManager interface {
	GetProductForOrderUpdateTx(ctx context.Context, tx pgx.Tx, productID uuid.UUID) (OrderProductInfo, error)
	ReserveStockTx(ctx context.Context, tx pgx.Tx, productID uuid.UUID, qty int32) error
	ReleaseStockHoldTx(ctx context.Context, tx pgx.Tx, productID uuid.UUID, qty int32) error
	ConfirmReservedStockTx(ctx context.Context, tx pgx.Tx, productID uuid.UUID, qty int32) error
	DecrementAvailableStockTx(ctx context.Context, tx pgx.Tx, productID uuid.UUID, qty int32) error
	RestoreStockTx(ctx context.Context, tx pgx.Tx, productID uuid.UUID, qty int32) error
	GetPromoByCodeForUpdateTx(ctx context.Context, tx pgx.Tx, code string) (OrderPromoInfo, error)
	IncrementPromoUsedTx(ctx context.Context, tx pgx.Tx, promoID uuid.UUID) error
	DecrementPromoUsedTx(ctx context.Context, tx pgx.Tx, promoID uuid.UUID) error
}
