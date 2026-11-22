package laporan

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	angapi "github.com/kopdes/merah-putih/internal/anggota/api"
	"github.com/kopdes/merah-putih/internal/api/response"
	iurapi "github.com/kopdes/merah-putih/internal/iuran/api"
	bayarapi "github.com/kopdes/merah-putih/internal/pembayaran/api"
	pesapi "github.com/kopdes/merah-putih/internal/pesanan/api"
	"github.com/kopdes/merah-putih/internal/security"
)

// Service menyusun laporan operasional read-only lintas domain via kontrak api/.
type Service struct {
	anggota    angapi.ReportReader
	iuran      iurapi.ReportReader
	pesanan    pesapi.ReportReader
	pembayaran bayarapi.ReportReader
	timezone   string
}

func NewService(
	anggota angapi.ReportReader,
	iuran iurapi.ReportReader,
	pesanan pesapi.ReportReader,
	pembayaran bayarapi.ReportReader,
	timezone string,
) *Service {
	if strings.TrimSpace(timezone) == "" {
		timezone = "Asia/Jakarta"
	}
	return &Service{
		anggota:    anggota,
		iuran:      iuran,
		pesanan:    pesanan,
		pembayaran: pembayaran,
		timezone:   timezone,
	}
}

func parseOptionalDateRange(dariStr, sampaiStr string) (*time.Time, *time.Time, error) {
	var dari, sampai *time.Time
	if strings.TrimSpace(dariStr) != "" {
		d, err := time.Parse("2006-01-02", strings.TrimSpace(dariStr))
		if err != nil {
			return nil, nil, response.NewDomainError(http.StatusBadRequest, "INVALID_DATE", "Parameter dari wajib YYYY-MM-DD")
		}
		dari = &d
	}
	if strings.TrimSpace(sampaiStr) != "" {
		d, err := time.Parse("2006-01-02", strings.TrimSpace(sampaiStr))
		if err != nil {
			return nil, nil, response.NewDomainError(http.StatusBadRequest, "INVALID_DATE", "Parameter sampai wajib YYYY-MM-DD")
		}
		sampai = &d
	}
	return dari, sampai, nil
}

func (s *Service) GetReport(ctx context.Context, jenis, dariStr, sampaiStr string) (interface{}, []string, [][]string, error) {
	j := strings.ToLower(strings.TrimSpace(jenis))
	if j == "" {
		j = "anggota"
	}
	dari, sampai, err := parseOptionalDateRange(dariStr, sampaiStr)
	if err != nil {
		return nil, nil, nil, err
	}
	switch j {
	case "anggota":
		return s.reportAnggota(ctx)
	case "tunggakan":
		return s.reportTunggakan(ctx)
	case "penjualan":
		return s.reportPenjualan(ctx, dari, sampai)
	case "pembayaran":
		return s.reportPembayaran(ctx, dari, sampai)
	default:
		return nil, nil, nil, response.NewDomainError(http.StatusBadRequest, "INVALID_REPORT_TYPE", "jenis harus anggota, tunggakan, penjualan, atau pembayaran")
	}
}

func (s *Service) reportAnggota(ctx context.Context) (interface{}, []string, [][]string, error) {
	rows, err := s.anggota.GetMemberSummary(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("laporan anggota: %w", err)
	}
	hdr := []string{"status", "jumlah"}
	tbl := make([][]string, 0, len(rows))
	for _, r := range rows {
		tbl = append(tbl, []string{security.NeutralizeCSVCell(r.Status), strconv.FormatInt(r.Jumlah, 10)})
	}
	return rows, hdr, tbl, nil
}

func (s *Service) reportTunggakan(ctx context.Context) (interface{}, []string, [][]string, error) {
	rows, err := s.iuran.GetArrearsReport(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("laporan tunggakan: %w", err)
	}
	hdr := []string{"nomor_anggota", "nama_lengkap", "jenis_kode", "jenis_nama", "periode_tahun", "periode_bulan", "nominal_terutang", "tanggal_jatuh_tempo"}
	tbl := make([][]string, 0, len(rows))
	for _, r := range rows {
		tbl = append(tbl, []string{
			security.NeutralizeCSVCell(r.NomorAnggota),
			security.NeutralizeCSVCell(r.NamaLengkap),
			security.NeutralizeCSVCell(r.JenisKode),
			security.NeutralizeCSVCell(r.JenisNama),
			fmt.Sprintf("%d", r.PeriodeTahun),
			fmt.Sprintf("%d", r.PeriodeBulan),
			strconv.FormatInt(r.NominalTerutang, 10),
			r.TanggalJatuhTempo,
		})
	}
	return rows, hdr, tbl, nil
}

func (s *Service) reportPenjualan(ctx context.Context, dari, sampai *time.Time) (interface{}, []string, [][]string, error) {
	rows, err := s.pesanan.GetDailySalesReport(ctx, dari, sampai)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("laporan penjualan: %w", err)
	}
	hdr := []string{"tanggal", "jumlah_pesanan", "total_subtotal", "total_diskon", "total_ongkir", "total_omzet"}
	tbl := make([][]string, 0, len(rows))
	for _, r := range rows {
		tbl = append(tbl, []string{
			r.Tanggal,
			strconv.FormatInt(r.JumlahPesanan, 10),
			strconv.FormatInt(r.TotalSubtotal, 10),
			strconv.FormatInt(r.TotalDiskon, 10),
			strconv.FormatInt(r.TotalOngkir, 10),
			strconv.FormatInt(r.TotalOmzet, 10),
		})
	}
	return rows, hdr, tbl, nil
}

func (s *Service) reportPembayaran(ctx context.Context, dari, sampai *time.Time) (interface{}, []string, [][]string, error) {
	rows, err := s.pembayaran.GetPaymentSummaryReport(ctx, dari, sampai)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("laporan pembayaran: %w", err)
	}
	hdr := []string{"jenis_transaksi", "metode", "status", "jumlah_transaksi", "total_pokok", "total_selisih_kode_unik", "total_dibayar", "total_dikembalikan"}
	tbl := make([][]string, 0, len(rows))
	for _, r := range rows {
		tbl = append(tbl, []string{
			security.NeutralizeCSVCell(r.JenisTransaksi),
			security.NeutralizeCSVCell(r.Metode),
			security.NeutralizeCSVCell(r.Status),
			strconv.FormatInt(r.JumlahTransaksi, 10),
			strconv.FormatInt(r.TotalPokok, 10),
			strconv.FormatInt(r.TotalSelisihKodeUnik, 10),
			strconv.FormatInt(r.TotalDibayar, 10),
			strconv.FormatInt(r.TotalDikembalikan, 10),
		})
	}
	return rows, hdr, tbl, nil
}

func RenderCSV(header []string, rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if err := cw.Write(header); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if err := cw.Write(r); err != nil {
			return nil, err
		}
	}
	cw.Flush()
	return buf.Bytes(), cw.Error()
}
