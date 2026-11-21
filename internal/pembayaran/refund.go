package pembayaran

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
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
	_ = qtx.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID), ActorPeran: claims.Role,
		Aksi: "pembayaran.refund_request", Entitas: "pembayaran",
		EntitasID: response.FromPGUUID(cur.ID).String(),
		Ringkasan: fmt.Sprintf("Ajukan refund %s nominal %d (%s)", cur.NomorPembayaran, in.Nominal, metode), IpAddress: ip,
	})
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

	cur, ref, usedBG, err := s.lockAndAuthorizeRefundDecisionTx(ctx, qtx, claims, idPG, aksi, in)
	if err != nil {
		return nil, err
	}
	bURL, bHashPG, bKeyID, bWrapped, err := s.maybeStoreRefundProofTx(ctx, qtx, response.FromPGUUID(ref.ID), rawFile)
	if err != nil {
		return nil, err
	}
	targetStatus := "ditolak"
	auditAksi := "pembayaran.refund_rejected"
	if aksi == "setujui" {
		targetStatus = "selesai"
		auditAksi = "pembayaran.refund_approved"
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
		if err := applyApprovedRefundEffectsTx(ctx, qtx, cur.ID, ref.Alasan); err != nil {
			return nil, err
		}
	}
	_ = qtx.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID), ActorPeran: claims.Role,
		Aksi: auditAksi, Entitas: "pembayaran", EntitasID: response.FromPGUUID(cur.ID).String(),
		Ringkasan: fmt.Sprintf("Refund %s -> %s nominal %d", cur.NomorPembayaran, targetStatus, ref.Nominal), IpAddress: ip,
	})
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *Service) lockAndAuthorizeRefundDecisionTx(ctx context.Context, qtx *db.Queries, claims *middleware.SessionClaims, idPG pgtype.UUID, aksi string, in RefundInput) (db.GetPembayaranForUpdateRow, db.GetPengembalianByPembayaranIDForUpdateRow, bool, error) {
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
	usedBG, err := s.authorizeDualControlOrBreakglassTx(ctx, qtx, claims, response.FromPGUUID(ref.DiajukanOleh), in.Breakglass, in.MFACode, note)
	if err != nil {
		return cur, ref, false, err
	}
	return cur, ref, usedBG, nil
}

func (s *Service) maybeStoreRefundProofTx(ctx context.Context, qtx *db.Queries, refID uuid.UUID, rawFile []byte) (string, pgtype.Text, string, []byte, error) {
	if len(rawFile) == 0 {
		return "", pgtype.Text{}, "", nil, nil
	}
	ext, buktiHash, err := validateProofImage(rawFile, s.cfg.UploadMaxBytes)
	if err != nil {
		return "", pgtype.Text{}, "", nil, err
	}
	if _, err := qtx.GetPengembalianByBuktiHash(ctx, response.ToPGText(buktiHash)); err == nil {
		return "", pgtype.Text{}, "", nil, response.NewDomainError(http.StatusConflict, "DUPLICATE_BUKTI", "Berkas bukti refund ini sudah pernah digunakan")
	}
	target, err := s.refundProofPath(refID)
	if err != nil {
		return "", pgtype.Text{}, "", nil, err
	}
	if s.cfg.KeyProvider == nil {
		return "", pgtype.Text{}, "", nil, fmt.Errorf("key provider wajib tersedia")
	}
	aad := security.FormatAAD("pengembalian_dana", "bukti", refID.String())
	cipherBytes, wrapped, keyID, err := security.EncryptEnvelopeFile(s.cfg.KeyProvider, rawFile, aad)
	if err != nil {
		return "", pgtype.Text{}, "", nil, fmt.Errorf("enkripsi bukti refund: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", pgtype.Text{}, "", nil, err
	}
	// #nosec G304,G703 -- target diverifikasi refundProofPath dari UUID
	if err := os.WriteFile(target, cipherBytes, 0o600); err != nil {
		return "", pgtype.Text{}, "", nil, err
	}
	url := "/api/v1/pembayaran?refund_bukti_id=" + refID.String() + "&ext=" + ext
	return url, response.ToPGText(buktiHash), keyID, wrapped, nil
}

func (s *Service) refundProofPath(refID uuid.UUID) (string, error) {
	if s.cfg.ProofStorageDir == "" {
		return "", response.NewDomainError(http.StatusInternalServerError, "STORAGE_NOT_CONFIGURED", "Direktori penyimpanan bukti belum siap")
	}
	full := filepath.Join(s.cfg.ProofStorageDir, "refund_"+refID.String()+".enc")
	if !strings.HasPrefix(full, s.cfg.ProofStorageDir+string(filepath.Separator)) {
		return "", fmt.Errorf("path bukti refund keluar dari direktori")
	}
	return full, nil
}

func applyApprovedRefundEffectsTx(ctx context.Context, qtx *db.Queries, bayarID pgtype.UUID, alasan string) error {
	if err := qtx.MarkPembayaranDikembalikan(ctx, bayarID); err != nil {
		return err
	}
	allocs, err := qtx.ListAlokasiByPembayaran(ctx, bayarID)
	if err != nil {
		return err
	}
	for _, al := range allocs {
		if al.TagihanIuranID.Valid {
			if err := qtx.UpdateTagihanStatus(ctx, db.UpdateTagihanStatusParams{Status: "belum_bayar", ID: al.TagihanIuranID}); err != nil {
				return err
			}
		}
		if al.PesananID.Valid {
			if err := cancelAndRestoreOrderAfterRefundTx(ctx, qtx, al.PesananID, alasan); err != nil {
				return err
			}
		}
	}
	return nil
}

func cancelAndRestoreOrderAfterRefundTx(ctx context.Context, qtx *db.Queries, pesananID pgtype.UUID, alasan string) error {
	ord, err := qtx.GetPesananForUpdate(ctx, pesananID)
	if err != nil || ord.Status == "batal" {
		return err
	}
	items, err := qtx.ListPesananItems(ctx, pesananID)
	if err != nil {
		return err
	}
	for _, it := range items {
		switch ord.StokStatus {
		case "reserved":
			err = qtx.ReleaseProdukStockHold(ctx, db.ReleaseProdukStockHoldParams{ID: it.ProdukID, Qty: it.Kuantitas})
		case "deducted":
			err = qtx.RestoreProdukStock(ctx, db.RestoreProdukStockParams{ID: it.ProdukID, Qty: it.Kuantitas})
		}
		if err != nil {
			return err
		}
	}
	if ord.PromoID.Valid {
		_ = qtx.DecrementPromoUsed(ctx, ord.PromoID)
	}
	if err := qtx.UpdatePesananStokStatus(ctx, db.UpdatePesananStokStatusParams{ID: pesananID, StokStatus: "released"}); err != nil {
		return err
	}
	_, err = qtx.UpdatePesananStatus(ctx, db.UpdatePesananStatusParams{Status: "batal", Catatan: "Refund selesai: " + alasan, ID: pesananID})
	return err
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
	refUUID := response.FromPGUUID(row.ID)
	target, err := s.refundProofPath(refUUID)
	if err != nil {
		return nil, "", err
	}
	// #nosec G304,G703 -- target diverifikasi refundProofPath dari UUID tervalidasi
	cipherBytes, err := os.ReadFile(target)
	if err != nil {
		return nil, "", response.NewDomainError(http.StatusNotFound, "BUKTI_FILE_MISSING", "Berkas bukti refund tidak ditemukan")
	}
	plain, err := security.DecryptEnvelopeFile(s.cfg.KeyProvider, cipherBytes, row.BuktiDekWrapped, security.FormatAAD("pengembalian_dana", "bukti", refUUID.String()))
	if err != nil {
		return nil, "", fmt.Errorf("dekripsi bukti refund: %w", err)
	}
	return plain, http.DetectContentType(plain), nil
}
