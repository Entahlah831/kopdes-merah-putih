package api

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PayableBill merepresentasikan tagihan iuran terkunci FOR UPDATE untuk alokasi pembayaran.
type PayableBill struct {
	ID                uuid.UUID
	AnggotaID         uuid.UUID
	JenisIuranID      uuid.UUID
	PeriodeTahun      int32
	PeriodeBulan      int32
	Nominal           int64
	NominalKeringanan int64
	Status            string
}

// TunggakanReportRow merepresentasikan satu baris laporan tunggakan iuran.
type TunggakanReportRow struct {
	NomorAnggota      string `json:"nomor_anggota"`
	NamaLengkap       string `json:"nama_lengkap"`
	JenisKode         string `json:"jenis_kode"`
	JenisNama         string `json:"jenis_nama"`
	PeriodeTahun      int32  `json:"periode_tahun"`
	PeriodeBulan      int32  `json:"periode_bulan"`
	NominalTerutang   int64  `json:"nominal_terutang"`
	TanggalJatuhTempo string `json:"tanggal_jatuh_tempo"`
}

// BillPayer memungkinkan modul pembayaran mengunci dan memperbarui status tagihan iuran dalam satu transaksi.
type BillPayer interface {
	LockPayableBillTx(ctx context.Context, tx pgx.Tx, billID uuid.UUID) (PayableBill, error)
	UpdateBillStatusTx(ctx context.Context, tx pgx.Tx, billID uuid.UUID, status string) error
}

// ReportReader menyediakan laporan tunggakan iuran bagi modul laporan.
type ReportReader interface {
	GetArrearsReport(ctx context.Context) ([]TunggakanReportRow, error)
}
