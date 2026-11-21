package pembayaran

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/filestore"
)

type RefundInput struct {
	Aksi             string `json:"aksi"`
	Nominal          int64  `json:"nominal"`
	Metode           string `json:"metode"`
	Alasan           string `json:"alasan"`
	CatatanPelaksana string `json:"catatan_pelaksana"`
	Breakglass       bool   `json:"breakglass,omitempty"`
	MFACode          string `json:"mfa_code,omitempty"`
}

func (s *Service) ProcessRefund(ctx context.Context, claims *middleware.SessionClaims, bayarIDStr string, in RefundInput, rawFile []byte, ip string) (int, interface{}, error) {
	idPG, err := response.ParsePGUUID(bayarIDStr)
	if err != nil {
		return 0, nil, err
	}
	aksi := strings.ToLower(strings.TrimSpace(in.Aksi))
	switch aksi {
	case "ajukan":
		row, rErr := s.requestRefund(ctx, claims, idPG, in, ip)
		return http.StatusCreated, row, rErr
	case "setujui", "tolak":
		row, rErr := s.decideRefund(ctx, claims, idPG, aksi, in, rawFile, ip)
		return http.StatusOK, row, rErr
	default:
		return 0, nil, response.NewDomainError(http.StatusBadRequest, "INVALID_ACTION", "aksi refund harus ajukan, setujui, atau tolak")
	}
}

func (s *Service) requestRefund(ctx context.Context, claims *middleware.SessionClaims, idPG pgtype.UUID, in RefundInput, ip string) (*db.UpsertPengembalianRequestRow, error) {
	metode := strings.ToLower(strings.TrimSpace(in.Metode))
	alasan := strings.TrimSpace(in.Alasan)
	if (metode != "tunai" && metode != "transfer") || alasan == "" || in.Nominal <= 0 {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_REFUND_INPUT", "metode (tunai|transfer), nominal > 0, dan alasan wajib diisi")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	cur, err := qtx.GetPembayaranForUpdate(ctx, idPG)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, response.NewDomainError(http.StatusNotFound, "PEMBAYARAN_NOT_FOUND", "Pembayaran tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	if err := validateRefundEligibility(ctx, qtx, cur, in.Nominal); err != nil {
		return nil, err
	}
	row, err := qtx.UpsertPengembalianRequest(ctx, db.UpsertPengembalianRequestParams{
		PembayaranID: idPG, Nominal: in.Nominal, Metode: metode,
		Alasan: alasan, DiajukanOleh: response.ToPGUUID(claims.UserID),
	})
	if err != nil {
		return nil, fmt.Errorf("simpan pengajuan refund: %w", err)
	}
	s.logAuditTx(ctx, tx, claims, "pembayaran.refund_request", "pembayaran",
		response.FromPGUUID(cur.ID).String(), fmt.Sprintf("Ajukan refund %s nominal %d (%s)", cur.NomorPembayaran, in.Nominal, metode), ip)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &row, nil
}

func validateRefundEligibility(ctx context.Context, qtx *db.Queries, cur db.GetPembayaranForUpdateRow, nominal int64) error {
	if cur.Status != "lunas" && cur.Status != "perlu_tindak_lanjut" {
		return response.NewDomainError(http.StatusConflict, "INVALID_PAYMENT_STATUS", "Refund hanya untuk pembayaran lunas atau perlu tindak lanjut")
	}
	maxRefund := cur.NominalDibayar
	if maxRefund <= 0 || cur.NominalTotal > maxRefund {
		if cur.NominalDibayar > 0 {
			maxRefund = cur.NominalDibayar
		} else {
			maxRefund = cur.NominalTotal
		}
	}
	if nominal > maxRefund {
		return response.NewDomainError(http.StatusBadRequest, "REFUND_EXCEEDS_PAYMENT", "Nominal pengembalian melebihi nominal pembayaran")
	}
	if ex, err := qtx.GetPengembalianByPembayaranID(ctx, cur.ID); err == nil {
		if ex.Status == "diajukan" || ex.Status == "selesai" {
			return response.NewDomainError(http.StatusConflict, "REFUND_ALREADY_EXISTS", "Pengembalian dana sudah diajukan atau selesai")
		}
	}
	return nil
}

func (s *Service) decideRefund(ctx context.Context, claims *middleware.SessionClaims, idPG pgtype.UUID, aksi string, in RefundInput, rawFile []byte, ip string) (*db.CompletePengembalianDecisionRow, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	cur, ref, usedBG, err := s.lockAndAuthorizeRefundDecisionTx(ctx, tx, qtx, claims, idPG, aksi, in)
	if err != nil {
		return nil, err
	}
	bURL, bHashPG, bKeyID, bWrapped, err := s.maybeStoreRefundProofTx(ctx, qtx, response.FromPGUUID(ref.ID), rawFile)
	if err != nil {
		return nil, err
	}
	targetStatus, auditAksi := "ditolak", "pembayaran.refund_rejected"
	if aksi == "setujui" {
		targetStatus, auditAksi = "selesai", "pembayaran.refund_approved"
	}
	if usedBG {
		auditAksi = "pembayaran.refund_breakglass"
	}
	updated, err := qtx.CompletePengembalianDecision(ctx, db.CompletePengembalianDecisionParams{
		Status: targetStatus, CatatanPelaksana: strings.TrimSpace(in.CatatanPelaksana),
		DilaksanakanOleh: response.ToPGUUID(claims.UserID), Breakglass: usedBG,
		BuktiUrl: bURL, BuktiHash: bHashPG, BuktiKeyID: bKeyID, BuktiDekWrapped: bWrapped, ID: ref.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("simpan keputusan refund: %w", err)
	}
	if aksi == "setujui" {
		if err := s.applyApprovedRefundEffectsTx(ctx, tx, qtx, cur.ID, ref.Alasan); err != nil {
			return nil, err
		}
	}
	s.logAuditTx(ctx, tx, claims, auditAksi, "pembayaran", response.FromPGUUID(cur.ID).String(),
		fmt.Sprintf("Refund %s -> %s nominal %d", cur.NomorPembayaran, targetStatus, ref.Nominal), ip)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *Service) lockAndAuthorizeRefundDecisionTx(ctx context.Context, tx pgx.Tx, qtx *db.Queries, claims *middleware.SessionClaims, idPG pgtype.UUID, aksi string, in RefundInput) (db.GetPembayaranForUpdateRow, db.GetPengembalianByPembayaranIDForUpdateRow, bool, error) {
	cur, err := qtx.GetPembayaranForUpdate(ctx, idPG)
	if errors.Is(err, pgx.ErrNoRows) {
		return cur, db.GetPengembalianByPembayaranIDForUpdateRow{}, false, response.NewDomainError(http.StatusNotFound, "PEMBAYARAN_NOT_FOUND", "Pembayaran tidak ditemukan")
	}
	if err != nil {
		return cur, db.GetPengembalianByPembayaranIDForUpdateRow{}, false, err
	}
	ref, err := qtx.GetPengembalianByPembayaranIDForUpdate(ctx, cur.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return cur, ref, false, response.NewDomainError(http.StatusNotFound, "REFUND_NOT_FOUND", "Pengajuan pengembalian dana tidak ditemukan")
	}
	if err != nil {
		return cur, ref, false, err
	}
	if ref.Status != "diajukan" {
		return cur, ref, false, response.NewDomainError(http.StatusConflict, "INVALID_REFUND_STATUS", "Pengembalian dana tidak berstatus diajukan")
	}
	note := strings.TrimSpace(in.CatatanPelaksana)
	if aksi == "tolak" && note == "" {
		return cur, ref, false, response.NewDomainError(http.StatusBadRequest, "REASON_REQUIRED", "catatan_pelaksana wajib diisi saat menolak refund")
	}
	usedBG, err := s.authorizeDualControlOrBreakglassTx(ctx, tx, claims, response.FromPGUUID(ref.DiajukanOleh), in.Breakglass, in.MFACode, note)
	if err != nil {
		return cur, ref, false, err
	}
	return cur, ref, usedBG, nil
}

func (s *Service) maybeStoreRefundProofTx(ctx context.Context, qtx *db.Queries, refID uuid.UUID, rawFile []byte) (string, pgtype.Text, string, []byte, error) {
	if len(rawFile) == 0 {
		return "", pgtype.Text{}, "", nil, nil
	}
	meta, err := filestore.ValidateImage(rawFile, s.cfg.UploadMaxBytes)
	if err != nil {
		return "", pgtype.Text{}, "", nil, err
	}
	if _, err := qtx.GetPengembalianByBuktiHash(ctx, response.ToPGText(meta.SHA256Hex)); err == nil {
		return "", pgtype.Text{}, "", nil, response.NewDomainError(http.StatusConflict, "DUPLICATE_BUKTI", "Berkas bukti refund ini sudah pernah digunakan")
	}
	store, err := s.activeFileStore()
	if err != nil {
		return "", pgtype.Text{}, "", nil, err
	}
	saved, err := store.SaveEncrypted(ctx, filestore.NamespaceBukti, "refund_"+refID.String()+".enc", rawFile, formatBuktiAAD("pengembalian_dana", refID.String()))
	if err != nil {
		return "", pgtype.Text{}, "", nil, err
	}
	url := "/api/v1/pembayaran?refund_bukti_id=" + refID.String() + "&ext=" + meta.Ext
	return url, response.ToPGText(meta.SHA256Hex), saved.KeyID, saved.DEKWrapped, nil
}

func (s *Service) applyApprovedRefundEffectsTx(ctx context.Context, tx pgx.Tx, qtx *db.Queries, bayarID pgtype.UUID, alasan string) error {
	if err := qtx.MarkPembayaranDikembalikan(ctx, bayarID); err != nil {
		return err
	}
	allocs, err := qtx.ListAlokasiByPembayaran(ctx, bayarID)
	if err != nil {
		return err
	}
	for _, al := range allocs {
		if al.TagihanIuranID.Valid {
			if s.cfg.Bills == nil {
				return response.NewDomainError(http.StatusInternalServerError, "BILL_PAYER_UNAVAILABLE", "Layanan iuran belum terhubung")
			}
			if err := s.cfg.Bills.UpdateBillStatusTx(ctx, tx, response.FromPGUUID(al.TagihanIuranID), "belum_bayar"); err != nil {
				return err
			}
		}
		if al.PesananID.Valid {
			if s.cfg.Orders == nil {
				return response.NewDomainError(http.StatusInternalServerError, "ORDER_PAYER_UNAVAILABLE", "Layanan pesanan belum terhubung")
			}
			if err := s.cfg.Orders.CancelOrderAfterRefundTx(ctx, tx, response.FromPGUUID(al.PesananID), alasan); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) ReadRefundBukti(ctx context.Context, claims *middleware.SessionClaims, refIDStr string) ([]byte, string, error) {
	if claims.Role != "admin" && claims.Role != "bendahara" {
		return nil, "", response.NewDomainError(http.StatusForbidden, "FORBIDDEN", "Hanya admin atau bendahara yang berhak melihat bukti refund")
	}
	idPG, err := response.ParsePGUUID(refIDStr)
	if err != nil {
		return nil, "", err
	}
	row, err := s.q.GetPengembalianByID(ctx, idPG)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", response.NewDomainError(http.StatusNotFound, "REFUND_NOT_FOUND", "Data pengembalian dana tidak ditemukan")
	}
	if err != nil || row.BuktiUrl == "" {
		return nil, "", response.NewDomainError(http.StatusNotFound, "BUKTI_NOT_FOUND", "Bukti refund belum tersedia")
	}
	store, err := s.activeFileStore()
	if err != nil {
		return nil, "", err
	}
	refUUID := response.FromPGUUID(row.ID)
	plain, err := store.LoadDecrypted(ctx, filestore.NamespaceBukti, "refund_"+refUUID.String()+".enc", row.BuktiDekWrapped, formatBuktiAAD("pengembalian_dana", refUUID.String()))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", response.NewDomainError(http.StatusNotFound, "BUKTI_FILE_MISSING", "Berkas bukti refund tidak ditemukan")
		}
		return nil, "", fmt.Errorf("dekripsi bukti refund: %w", err)
	}
	return plain, http.DetectContentType(plain), nil
}
