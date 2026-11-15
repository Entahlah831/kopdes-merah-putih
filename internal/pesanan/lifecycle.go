package pesanan

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

type DaftarAntarItem struct {
	ID                uuid.UUID `json:"id"`
	NomorPesanan      string    `json:"nomor_pesanan"`
	Status            string    `json:"status"`
	TotalBayar        int64     `json:"total_bayar"`
	NamaPenerima      string    `json:"nama_penerima"`
	HPPenerima        string    `json:"hp_penerima"`
	AlamatPengantaran string    `json:"alamat_pengantaran"`
	Catatan           string    `json:"catatan,omitempty"`
	TanggalAntar      string    `json:"tanggal_antar,omitempty"`
	LabelAntar        string    `json:"label_antar"`
}

var allowedOrderTransitions = map[string]map[string]bool{
	"baru":                {"menunggu_pembayaran": true, "menunggu_verifikasi": true, "dibayar": true, "diproses": true, "batal": true},
	"menunggu_pembayaran": {"menunggu_verifikasi": true, "dibayar": true, "batal": true},
	"menunggu_verifikasi": {"dibayar": true, "batal": true},
	"dibayar":             {"diproses": true, "diantar": true, "siap_diambil": true, "selesai": true, "batal": true},
	"diproses":            {"diantar": true, "siap_diambil": true, "selesai": true, "batal": true},
	"diantar":             {"selesai": true},
	"siap_diambil":        {"selesai": true},
}

func (s *Service) List(ctx context.Context, claims *middleware.SessionClaims, userFilterStr, statusFilter string, lim, off int32) ([]PesananDTO, error) {
	var userFilter pgtype.UUID
	if claims.Role == "anggota" {
		userFilter = response.ToPGUUID(claims.UserID)
	} else if strings.TrimSpace(userFilterStr) != "" {
		uid, err := response.ParsePGUUID(userFilterStr)
		if err != nil {
			return nil, err
		}
		userFilter = uid
	}
	rows, err := s.q.ListPesanan(ctx, db.ListPesananParams{
		UserFilter:   userFilter,
		StatusFilter: strings.TrimSpace(statusFilter),
		Lim:          lim,
		Off:          off,
	})
	if err != nil {
		return nil, fmt.Errorf("ambil daftar pesanan: %w", err)
	}
	out := make([]PesananDTO, 0, len(rows))
	for _, r := range rows {
		dto := PesananDTO{
			ID:               response.FromPGUUID(r.ID),
			NomorPesanan:     r.NomorPesanan,
			UserID:           response.FromPGUUID(r.UserID),
			MetodePengiriman: r.MetodePengiriman,
			MetodePembayaran: r.MetodePembayaran,
			Status:           r.Status,
			Subtotal:         r.Subtotal,
			Diskon:           r.Diskon,
			Ongkir:           r.Ongkir,
			TotalBayar:       r.TotalBayar,
			NamaPenerima:     r.NamaPenerima,
			HPPenerimaMasked: r.HpPenerimaMasked,
			CreatedAt:        r.CreatedAt.Time,
		}
		if r.AnggotaID.Valid {
			aid := response.FromPGUUID(r.AnggotaID)
			dto.AnggotaID = &aid
		}
		if r.ExpiresAt.Valid {
			t := r.ExpiresAt.Time
			dto.ExpiresAt = &t
		}
		out = append(out, dto)
	}
	return out, nil
}

func (s *Service) GetDetail(ctx context.Context, claims *middleware.SessionClaims, idStr string) (*PesananDTO, error) {
	idPG, err := response.ParsePGUUID(idStr)
	if err != nil {
		return nil, err
	}
	row, err := s.q.GetPesananByID(ctx, idPG)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, response.NewDomainError(http.StatusNotFound, "PESANAN_NOT_FOUND", "Pesanan tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	ownerID := response.FromPGUUID(row.UserID)
	if claims.Role == "anggota" && ownerID != claims.UserID {
		return nil, response.NewDomainError(http.StatusForbidden, "FORBIDDEN", "Tidak berhak melihat pesanan anggota lain")
	}
	items, err := s.q.ListPesananItems(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	hpPlain, _ := s.kp.DecryptWithAAD(row.HpPenerimaEnc, security.FormatAAD("pesanan", "hp_penerima", idStr))
	alamatPlain, _ := s.kp.DecryptWithAAD(row.AlamatPengantaranEnc, security.FormatAAD("pesanan", "alamat_pengantaran", idStr))
	dto := PesananDTO{
		ID:                response.FromPGUUID(row.ID),
		NomorPesanan:      row.NomorPesanan,
		UserID:            ownerID,
		MetodePengiriman:  row.MetodePengiriman,
		MetodePembayaran:  row.MetodePembayaran,
		Status:            row.Status,
		Subtotal:          row.Subtotal,
		Diskon:            row.Diskon,
		Ongkir:            row.Ongkir,
		TotalBayar:        row.TotalBayar,
		NamaPenerima:      row.NamaPenerima,
		HPPenerimaMasked:  row.HpPenerimaMasked,
		HPPenerima:        string(hpPlain),
		AlamatPengantaran: string(alamatPlain),
		Catatan:           row.Catatan,
		CreatedAt:         row.CreatedAt.Time,
		Items:             mapItems(items),
	}
	if row.AnggotaID.Valid {
		aid := response.FromPGUUID(row.AnggotaID)
		dto.AnggotaID = &aid
	}
	if row.ExpiresAt.Valid {
		t := row.ExpiresAt.Time
		dto.ExpiresAt = &t
	}
	return &dto, nil
}

func (s *Service) UpdateStatus(ctx context.Context, claims *middleware.SessionClaims, idStr, newStatus, catatan, ip string) (*db.UpdatePesananStatusRow, error) {
	idPG, err := response.ParsePGUUID(idStr)
	if err != nil {
		return nil, err
	}
	st := strings.TrimSpace(newStatus)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	cur, err := qtx.GetPesananForUpdate(ctx, idPG)
	if err != nil {
		return nil, err
	}
	if err := authorizeOrderTransition(claims, cur, st); err != nil {
		return nil, err
	}
	if !orderHadDeductedStock(cur.Status) && orderHadDeductedStock(st) {
		if err := deductOrderStockTx(ctx, qtx, cur.ID); err != nil {
			return nil, err
		}
	}
	if st == "batal" && cur.Status != "batal" {
		if err := restoreOrderResourcesTx(ctx, qtx, cur.ID, cur.PromoID, orderHadDeductedStock(cur.Status)); err != nil {
			return nil, err
		}
	}
	updated, err := qtx.UpdatePesananStatus(ctx, db.UpdatePesananStatusParams{
		Status:  st,
		Catatan: strings.TrimSpace(catatan),
		ID:      idPG,
	})
	if err != nil {
		return nil, fmt.Errorf("ubah status pesanan: %w", err)
	}
	_ = qtx.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "pesanan.status_" + st,
		Entitas:     "pesanan",
		EntitasID:   idStr,
		Ringkasan:   fmt.Sprintf("Pesanan %s: %s -> %s", cur.NomorPesanan, cur.Status, st),
		IpAddress:   ip,
	})
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &updated, nil
}

func authorizeOrderTransition(claims *middleware.SessionClaims, cur db.GetPesananForUpdateRow, st string) error {
	if claims.Role == "anggota" {
		if response.FromPGUUID(cur.UserID) != claims.UserID {
			return response.NewDomainError(http.StatusForbidden, "FORBIDDEN", "Tidak berhak mengubah pesanan orang lain")
		}
		if st != "batal" || (cur.Status != "baru" && cur.Status != "menunggu_pembayaran") {
			return response.NewDomainError(http.StatusForbidden, "FORBIDDEN", "Anggota hanya boleh membatalkan pesanan yang belum dibayar")
		}
		return nil
	}
	if !allowedOrderTransitions[cur.Status][st] {
		return response.NewDomainError(http.StatusConflict, "INVALID_TRANSITION", fmt.Sprintf("Transisi status %s -> %s tidak valid", cur.Status, st))
	}
	return nil
}

func orderHadDeductedStock(status string) bool {
	return status == "dibayar" || status == "diproses" || status == "diantar" || status == "siap_diambil" || status == "selesai"
}

func deductOrderStockTx(ctx context.Context, qtx *db.Queries, pesananID pgtype.UUID) error {
	items, err := qtx.ListPesananItems(ctx, pesananID)
	if err != nil {
		return err
	}
	for _, it := range items {
		if _, err := qtx.DecrementProdukStock(ctx, db.DecrementProdukStockParams{
			ID:  it.ProdukID,
			Qty: it.Kuantitas,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return response.NewDomainError(http.StatusConflict, "OUT_OF_STOCK", "Stok produk "+it.NamaProduk+" tidak mencukupi")
			}
			return err
		}
	}
	return nil
}

func restoreOrderResourcesTx(ctx context.Context, qtx *db.Queries, pesananID, promoID pgtype.UUID, restoreStock bool) error {
	if restoreStock {
		items, err := qtx.ListPesananItems(ctx, pesananID)
		if err != nil {
			return err
		}
		for _, it := range items {
			if err := qtx.RestoreProdukStock(ctx, db.RestoreProdukStockParams{ID: it.ProdukID, Qty: it.Kuantitas}); err != nil {
				return err
			}
		}
	}
	if promoID.Valid {
		if err := qtx.DecrementPromoUsed(ctx, promoID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListDaftarAntar(ctx context.Context, tanggalStr string) ([]DaftarAntarItem, error) {
	var tglPG pgtype.Date
	if strings.TrimSpace(tanggalStr) != "" {
		d, err := time.Parse("2006-01-02", strings.TrimSpace(tanggalStr))
		if err != nil {
			return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_DATE", "tanggal wajib YYYY-MM-DD")
		}
		tglPG = response.ToPGDate(d)
	}
	rows, err := s.q.ListDaftarAntar(ctx, tglPG)
	if err != nil {
		return nil, fmt.Errorf("ambil daftar antar: %w", err)
	}
	out := make([]DaftarAntarItem, 0, len(rows))
	for _, r := range rows {
		oid := response.FromPGUUID(r.ID)
		hp, _ := s.kp.DecryptWithAAD(r.HpPenerimaEnc, security.FormatAAD("pesanan", "hp_penerima", oid.String()))
		alamat, _ := s.kp.DecryptWithAAD(r.AlamatPengantaranEnc, security.FormatAAD("pesanan", "alamat_pengantaran", oid.String()))
		tgl := ""
		if r.TanggalAntar.Valid {
			tgl = r.TanggalAntar.Time.Format("2006-01-02")
		}
		out = append(out, DaftarAntarItem{
			ID:                oid,
			NomorPesanan:      r.NomorPesanan,
			Status:            r.Status,
			TotalBayar:        r.TotalBayar,
			NamaPenerima:      r.NamaPenerima,
			HPPenerima:        string(hp),
			AlamatPengantaran: string(alamat),
			Catatan:           r.Catatan,
			TanggalAntar:      tgl,
			LabelAntar:        fmt.Sprintf("%s | %s (%s) | %s | Rp%d", r.NomorPesanan, r.NamaPenerima, string(hp), string(alamat), r.TotalBayar),
		})
	}
	return out, nil
}

func ExportDaftarAntarCSV(items []DaftarAntarItem) ([]byte, error) {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"nomor_pesanan", "status", "tanggal_antar", "nama_penerima", "hp_penerima", "alamat_pengantaran", "total_bayar", "catatan"})
	for _, it := range items {
		_ = cw.Write([]string{
			security.NeutralizeCSVCell(it.NomorPesanan),
			security.NeutralizeCSVCell(it.Status),
			security.NeutralizeCSVCell(it.TanggalAntar),
			security.NeutralizeCSVCell(it.NamaPenerima),
			security.NeutralizeCSVCell(it.HPPenerima),
			security.NeutralizeCSVCell(it.AlamatPengantaran),
			fmt.Sprintf("%d", it.TotalBayar),
			security.NeutralizeCSVCell(it.Catatan),
		})
	}
	cw.Flush()
	return buf.Bytes(), cw.Error()
}

func (s *Service) ExpireUnpaidOrders(ctx context.Context) (int, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	expired, err := qtx.ListExpiredUnpaidPesanan(ctx)
	if err != nil {
		return 0, fmt.Errorf("ambil pesanan kedaluwarsa: %w", err)
	}
	for _, row := range expired {
		if err := restoreOrderResourcesTx(ctx, qtx, row.ID, row.PromoID, false); err != nil {
			return 0, err
		}
		if _, err := qtx.UpdatePesananStatus(ctx, db.UpdatePesananStatusParams{
			Status:  "batal",
			Catatan: "Kedaluwarsa otomatis",
			ID:      row.ID,
		}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(expired), nil
}

func mapPesananCreated(r db.GetPesananByIdempotencyKeyRow, items []db.PesananItem) PesananDTO {
	dto := PesananDTO{
		ID:               response.FromPGUUID(r.ID),
		NomorPesanan:     r.NomorPesanan,
		UserID:           response.FromPGUUID(r.UserID),
		MetodePengiriman: r.MetodePengiriman,
		MetodePembayaran: r.MetodePembayaran,
		Status:           r.Status,
		Subtotal:         r.Subtotal,
		Diskon:           r.Diskon,
		Ongkir:           r.Ongkir,
		TotalBayar:       r.TotalBayar,
		NamaPenerima:     r.NamaPenerima,
		HPPenerimaMasked: r.HpPenerimaMasked,
		Catatan:          r.Catatan,
		CreatedAt:        r.CreatedAt.Time,
		Items:            mapItems(items),
	}
	if r.ExpiresAt.Valid {
		t := r.ExpiresAt.Time
		dto.ExpiresAt = &t
	}
	return dto
}

func mapPesananFromCreate(r db.CreatePesananRow, items []db.PesananItem) PesananDTO {
	dto := PesananDTO{
		ID:               response.FromPGUUID(r.ID),
		NomorPesanan:     r.NomorPesanan,
		UserID:           response.FromPGUUID(r.UserID),
		MetodePengiriman: r.MetodePengiriman,
		MetodePembayaran: r.MetodePembayaran,
		Status:           r.Status,
		Subtotal:         r.Subtotal,
		Diskon:           r.Diskon,
		Ongkir:           r.Ongkir,
		TotalBayar:       r.TotalBayar,
		NamaPenerima:     r.NamaPenerima,
		HPPenerimaMasked: r.HpPenerimaMasked,
		Catatan:          r.Catatan,
		CreatedAt:        r.CreatedAt.Time,
		Items:            mapItems(items),
	}
	if r.ExpiresAt.Valid {
		t := r.ExpiresAt.Time
		dto.ExpiresAt = &t
	}
	return dto
}

func mapItems(items []db.PesananItem) []PesananItemDTO {
	out := make([]PesananItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, PesananItemDTO{
			ID:          response.FromPGUUID(it.ID),
			ProdukID:    response.FromPGUUID(it.ProdukID),
			NamaProduk:  it.NamaProduk,
			Kuantitas:   it.Kuantitas,
			HargaSatuan: it.HargaSatuan,
			Subtotal:    it.Subtotal,
		})
	}
	return out
}
