package api

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PayableOrder merepresentasikan pesanan terkunci FOR UPDATE untuk pembuatan/verifikasi pembayaran.
type PayableOrder struct {
	ID                uuid.UUID
	NomorPesanan      string
	UserID            uuid.UUID
	AnggotaID         *uuid.UUID
	PromoID           *uuid.UUID
	MetodePengiriman  string
	MetodePembayaran  string
	Status            string
	StokStatus        string
	StokHoldExpiresAt *time.Time
	TotalBayar        int64
	ExpiresAt         *time.Time
}

// StockRunoutError menandakan stok produk habis setelah masa reservasi berakhir saat konfirmasi pembayaran.
type StockRunoutError struct {
	ProductName string
}

func (e *StockRunoutError) Error() string {
	return "stok tidak mencukupi setelah masa reservasi berakhir: " + e.ProductName
}

// SalesReportRow merepresentasikan ringkasan penjualan harian untuk modul laporan.
type SalesReportRow struct {
	Tanggal       string `json:"tanggal"`
	JumlahPesanan int64  `json:"jumlah_pesanan"`
	TotalSubtotal int64  `json:"total_subtotal"`
	TotalDiskon   int64  `json:"total_diskon"`
	TotalOngkir   int64  `json:"total_ongkir"`
	TotalOmzet    int64  `json:"total_omzet"`
}

// OrderPayer memungkinkan modul pembayaran mengunci, mengonfirmasi, atau membatalkan pesanan dalam satu tx.
type OrderPayer interface {
	LockPayableOrderTx(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (PayableOrder, error)
	ConfirmPaidOrderTx(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) error
	CancelOrderAfterRefundTx(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, alasan string) error
}

// ReportReader menyediakan laporan penjualan harian bagi modul laporan.
type ReportReader interface {
	GetDailySalesReport(ctx context.Context, dari, sampai *time.Time) ([]SalesReportRow, error)
}
