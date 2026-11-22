package api

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PublicQRISInfo berisi metadata non-rahasia rekening QRIS aktif untuk public config.
type PublicQRISInfo struct {
	RekeningID    string
	NamaMerchant  string
	NMID          string
	NamaBank      string
	NomorRekening string
	JenisPemilik  string
	QRISImageURL  string
}

// RekapPembayaranRow merepresentasikan satu baris ringkasan pembayaran untuk laporan.
type RekapPembayaranRow struct {
	JenisTransaksi       string `json:"jenis_transaksi"`
	Metode               string `json:"metode"`
	Status               string `json:"status"`
	JumlahTransaksi      int64  `json:"jumlah_transaksi"`
	TotalPokok           int64  `json:"total_pokok"`
	TotalSelisihKodeUnik int64  `json:"total_selisih_kode_unik"`
	TotalDibayar         int64  `json:"total_dibayar"`
	TotalDikembalikan    int64  `json:"total_dikembalikan"`
}

// ActiveQRISReader membaca rekening QRIS aktif dari modul pembayaran tanpa cross-table SQL JOIN.
type ActiveQRISReader interface {
	GetPublicQRISInfo(ctx context.Context, activeID *uuid.UUID) (PublicQRISInfo, error)
}

// ActivePaymentChecker memeriksa pembayaran pending/menunggu verifikasi untuk pesanan atau tagihan.
type ActivePaymentChecker interface {
	HasActivePembayaranForPesanan(ctx context.Context, orderID uuid.UUID) (bool, error)
	HasActivePembayaranForPesananTx(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (bool, error)
	HasActivePembayaranForTagihan(ctx context.Context, billID uuid.UUID) (bool, error)
}

// ReportReader menyediakan ringkasan pembayaran bagi modul laporan.
type ReportReader interface {
	GetPaymentSummaryReport(ctx context.Context, dari, sampai *time.Time) ([]RekapPembayaranRow, error)
}
