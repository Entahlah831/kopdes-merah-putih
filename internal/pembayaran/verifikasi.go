package pembayaran

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/filestore"
	pesapi "github.com/kopdes/merah-putih/internal/pesanan/api"
)

type VerifyInput struct {
	Keputusan              string `json:"keputusan"`
	NominalDibayar         int64  `json:"nominal_dibayar"`
	Alasan                 string `json:"alasan"`
	SelesaikanTindakLanjut bool   `json:"selesaikan_tindak_lanjut,omitempty"`
	Breakglass             bool   `json:"breakglass,omitempty"`
	MFACode                string `json:"mfa_code,omitempty"`
}

func (s *Service) AttachBukti(ctx context.Context, claims *middleware.SessionClaims, bayarIDStr string, rawBytes []byte, catatan, ip string) (*db.AttachPembayaranBuktiRow, error) {
	idPG, err := response.ParsePGUUID(bayarIDStr)
	if err != nil {
		return nil, err
	}
	meta, err := filestore.ValidateImage(rawBytes, s.cfg.UploadMaxBytes)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	cur, wrappedDEK, keyID, err := s.lockAndStoreBuktiFile(ctx, qtx, claims, idPG, meta.SHA256Hex, rawBytes)
	if err != nil {
		return nil, err
	}
	buktiURL := "/api/v1/pembayaran?bukti_id=" + response.FromPGUUID(cur.ID).String() + "&ext=" + meta.Ext
	updated, err := qtx.AttachPembayaranBukti(ctx, db.AttachPembayaranBuktiParams{
		BuktiUrl: buktiURL, BuktiHash: response.ToPGText(meta.SHA256Hex),
		BuktiKeyID: keyID, BuktiDekWrapped: wrappedDEK,
		CatatanPembayar: strings.TrimSpace(catatan), ID: cur.ID, PembayarUserID: cur.PembayarUserID,
	})
	if err != nil {
		return nil, fmt.Errorf("perbarui bukti pembayaran: %w", err)
	}
	s.logAuditTx(ctx, tx, claims, "pembayaran.upload_bukti", "pembayaran",
		response.FromPGUUID(cur.ID).String(), "Unggah bukti pembayaran "+cur.NomorPembayaran, ip)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *Service) lockAndStoreBuktiFile(ctx context.Context, qtx *db.Queries, claims *middleware.SessionClaims, idPG pgtype.UUID, buktiHash string, rawBytes []byte) (db.GetPembayaranForUpdateRow, []byte, string, error) {
	cur, err := qtx.GetPembayaranForUpdate(ctx, idPG)
	if errors.Is(err, pgx.ErrNoRows) {
		return cur, nil, "", response.NewDomainError(http.StatusNotFound, "PEMBAYARAN_NOT_FOUND", "Pembayaran tidak ditemukan")
	}
	if err != nil {
		return cur, nil, "", err
	}
	if err := validateBuktiAttachEligibility(claims, cur); err != nil {
		return cur, nil, "", err
	}
	if _, err := qtx.GetPembayaranByBuktiHash(ctx, response.ToPGText(buktiHash)); err == nil {
		return cur, nil, "", response.NewDomainError(http.StatusConflict, "DUPLICATE_BUKTI", "Berkas bukti transfer yang sama sudah pernah digunakan")
	}
	wrapped, keyID, err := s.saveEncryptedProofEnvelope(response.FromPGUUID(cur.ID), rawBytes)
	if err != nil {
		return cur, nil, "", err
	}
	return cur, wrapped, keyID, nil
}

func (s *Service) saveEncryptedProofEnvelope(bayarUUID uuid.UUID, rawBytes []byte) ([]byte, string, error) {
	store, err := s.activeFileStore()
	if err != nil {
		return nil, "", err
	}
	objKey := "bukti_" + bayarUUID.String() + ".enc"
	saved, err := store.SaveEncrypted(context.Background(), filestore.NamespaceBukti, objKey, rawBytes, formatBuktiAAD("pembayaran", bayarUUID.String()))
	if err != nil {
		return nil, "", err
	}
	return saved.DEKWrapped, saved.KeyID, nil
}

func (s *Service) readDecryptedProofEnvelope(bayarUUID uuid.UUID, wrappedDEK []byte) ([]byte, string, error) {
	objKey := "bukti_" + bayarUUID.String() + ".enc"
	aad := formatBuktiAAD("pembayaran", bayarUUID.String())
	if len(wrappedDEK) == 0 {
		return readLegacyDecrypted(s, objKey, aad)
	}
	store, err := s.activeFileStore()
	if err != nil {
		return nil, "", err
	}
	plain, err := store.LoadDecrypted(context.Background(), filestore.NamespaceBukti, objKey, wrappedDEK, aad)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", response.NewDomainError(http.StatusNotFound, "BUKTI_FILE_MISSING", "Berkas bukti tidak ditemukan di disk")
		}
		return nil, "", fmt.Errorf("dekripsi amplop bukti: %w", err)
	}
	return plain, http.DetectContentType(plain), nil
}

func validateBuktiAttachEligibility(claims *middleware.SessionClaims, cur db.GetPembayaranForUpdateRow) error {
	if claims.Role == "anggota" && response.FromPGUUID(cur.PembayarUserID) != claims.UserID {
		return response.NewDomainError(http.StatusForbidden, "FORBIDDEN", "Tidak berhak mengunggah bukti untuk pembayaran orang lain")
	}
	if cur.Metode != "qris" || cur.Status != "menunggu_pembayaran" {
		return response.NewDomainError(http.StatusConflict, "INVALID_PAYMENT_STATUS", "Bukti hanya dapat diunggah untuk QRIS berstatus menunggu_pembayaran")
	}
	if cur.ExpiresAt.Valid && time.Now().UTC().After(cur.ExpiresAt.Time) {
		return response.NewDomainError(http.StatusConflict, "PEMBAYARAN_EXPIRED", "Tagihan QRIS ini sudah kedaluwarsa")
	}
	return nil
}

func (s *Service) ReadBukti(ctx context.Context, claims *middleware.SessionClaims, bayarIDStr string) ([]byte, string, error) {
	idPG, err := response.ParsePGUUID(bayarIDStr)
	if err != nil {
		return nil, "", err
	}
	row, err := s.q.GetPembayaranByID(ctx, idPG)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", response.NewDomainError(http.StatusNotFound, "PEMBAYARAN_NOT_FOUND", "Pembayaran tidak ditemukan")
	}
	if err != nil {
		return nil, "", err
	}
	if claims.Role != "admin" && claims.Role != "bendahara" && response.FromPGUUID(row.PembayarUserID) != claims.UserID {
		return nil, "", response.NewDomainError(http.StatusForbidden, "FORBIDDEN", "Tidak berhak mengunduh bukti ini")
	}
	if row.BuktiUrl == "" {
		return nil, "", response.NewDomainError(http.StatusNotFound, "BUKTI_NOT_FOUND", "Bukti belum diunggah")
	}
	return s.readDecryptedProofEnvelope(response.FromPGUUID(row.ID), row.BuktiDekWrapped)
}

// PurgeExpiredBukti menghapus berkas bukti pembayaran non-lunas melewati masa retensi (PERLU VERIFIKASI HUKUM).
func (s *Service) PurgeExpiredBukti(ctx context.Context, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		retentionDays = s.cfg.BuktiRetentionDays
	}
	if retentionDays <= 0 {
		retentionDays = 365
	}
	rows, err := s.q.ListExpiredNonPaidPembayaranWithBukti(ctx, int32(retentionDays)) // #nosec G115 -- hari retensi bernilai wajar
	if err != nil {
		return 0, fmt.Errorf("ambil bukti kedaluwarsa: %w", err)
	}
	store, _ := s.activeFileStore()
	purged := 0
	for _, r := range rows {
		if store != nil {
			_ = store.Delete(ctx, filestore.NamespaceBukti, "bukti_"+response.FromPGUUID(r.ID).String()+".enc")
		}
		if err := s.q.ClearPembayaranBukti(ctx, r.ID); err == nil {
			purged++
		}
	}
	if purged > 0 {
		s.logAuditTx(ctx, nil, nil, "pembayaran.bukti_purge", "pembayaran", "",
			fmt.Sprintf("Pembersihan %d berkas bukti non-lunas > %d hari", purged, retentionDays), "127.0.0.1")
	}
	return purged, nil
}

func (s *Service) VerifyDecision(ctx context.Context, claims *middleware.SessionClaims, bayarIDStr string, in VerifyInput, ip string) (*db.VerifyPembayaranDecisionRow, error) {
	idPG, err := response.ParsePGUUID(bayarIDStr)
	if err != nil {
		return nil, err
	}
	keputusan := strings.ToLower(strings.TrimSpace(in.Keputusan))
	if keputusan != "setuju" && keputusan != "tolak" {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_DECISION", "Keputusan harus setuju atau tolak")
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
	usedBG, err := s.authorizeDualControlOrBreakglassTx(ctx, tx, claims, response.FromPGUUID(cur.DibuatOleh), in.Breakglass, in.MFACode, in.Alasan)
	if err != nil {
		return nil, err
	}
	if cur.Status != "menunggu_verifikasi" && cur.Status != "perlu_tindak_lanjut" {
		return nil, response.NewDomainError(http.StatusConflict, "INVALID_PAYMENT_STATUS", "Pembayaran tidak dalam status siap verifikasi")
	}
	updated, err := s.applyVerificationDecisionTx(ctx, tx, qtx, claims, cur, keputusan, in, usedBG, ip)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return updated, nil
}

func evaluateApprovalTx(ctx context.Context, qtx *db.Queries, cur db.GetPembayaranForUpdateRow, in VerifyInput) (finalStatus, alasan string, kwtPG pgtype.Text, err error) {
	alasan = strings.TrimSpace(in.Alasan)
	if in.NominalDibayar <= 0 {
		return "", "", kwtPG, response.NewDomainError(http.StatusBadRequest, "INVALID_AMOUNT", "nominal_dibayar harus > 0")
	}
	mismatch := in.NominalDibayar != cur.NominalTotal
	canResolve := cur.Status == "perlu_tindak_lanjut" && in.SelesaikanTindakLanjut && alasan != ""
	if mismatch && !canResolve {
		if alasan == "" {
			alasan = fmt.Sprintf("Selisih nominal: tagihan %d, dibayar %d", cur.NominalTotal, in.NominalDibayar)
		}
		return "perlu_tindak_lanjut", alasan, kwtPG, nil
	}
	seq, err := qtx.NextNomorKwitansi(ctx)
	if err != nil {
		return "", "", kwtPG, err
	}
	return "lunas", alasan, response.ToPGText(fmt.Sprintf("KWT-%s-%06d", time.Now().UTC().Format("2006"), seq)), nil
}

func (s *Service) applyVerificationDecisionTx(
	ctx context.Context, tx pgx.Tx, qtx *db.Queries, claims *middleware.SessionClaims,
	cur db.GetPembayaranForUpdateRow, keputusan string, in VerifyInput, usedBG bool, ip string,
) (*db.VerifyPembayaranDecisionRow, error) {
	alasan := strings.TrimSpace(in.Alasan)
	finalStatus := "ditolak"
	var kwtPG pgtype.Text
	if keputusan == "tolak" {
		if alasan == "" {
			return nil, response.NewDomainError(http.StatusBadRequest, "REASON_REQUIRED", "Alasan penolakan wajib diisi")
		}
	} else {
		var err error
		finalStatus, alasan, kwtPG, err = evaluateApprovalTx(ctx, qtx, cur, in)
		if err != nil {
			return nil, err
		}
	}
	if finalStatus == "lunas" {
		sp, spErr := tx.Begin(ctx)
		if spErr != nil {
			return nil, spErr
		}
		spErr = s.markAllocatedTargetsPaidTx(ctx, sp, qtx.WithTx(sp), cur.ID)
		var runoutErr *pesapi.StockRunoutError
		if errors.As(spErr, &runoutErr) {
			_ = sp.Rollback(ctx)
			finalStatus, kwtPG = "perlu_tindak_lanjut", pgtype.Text{}
			alasan = fmt.Sprintf("Stok tidak mencukupi setelah masa reservasi berakhir (%s): perlu pengembalian dana atau kesepakatan ulang", runoutErr.ProductName)
		} else if spErr != nil {
			_ = sp.Rollback(ctx)
			return nil, spErr
		} else if cErr := sp.Commit(ctx); cErr != nil {
			return nil, cErr
		}
	}
	return s.persistVerificationOutcomeTx(ctx, tx, qtx, claims, cur, finalStatus, in.NominalDibayar, kwtPG, alasan, usedBG, ip)
}

func (s *Service) persistVerificationOutcomeTx(ctx context.Context, tx pgx.Tx, qtx *db.Queries, claims *middleware.SessionClaims, cur db.GetPembayaranForUpdateRow, finalStatus string, nominalDibayar int64, kwtPG pgtype.Text, alasan string, usedBG bool, ip string) (*db.VerifyPembayaranDecisionRow, error) {
	updated, err := qtx.VerifyPembayaranDecision(ctx, db.VerifyPembayaranDecisionParams{
		Status: finalStatus, NominalDibayar: nominalDibayar, NomorKwitansi: kwtPG,
		AlasanVerifikasi: alasan, DiverifikasiOleh: response.ToPGUUID(claims.UserID), ID: cur.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("verifikasi pembayaran: %w", err)
	}
	aksiAudit := "pembayaran.verify_" + finalStatus
	if usedBG {
		aksiAudit = "pembayaran.verify_breakglass"
	}
	s.logAuditTx(ctx, tx, claims, aksiAudit, "pembayaran", response.FromPGUUID(cur.ID).String(),
		fmt.Sprintf("%s -> %s (dibayar %d, breakglass=%t)", cur.NomorPembayaran, finalStatus, nominalDibayar, usedBG), ip)
	return &updated, nil
}

func (s *Service) markAllocatedTargetsPaidTx(ctx context.Context, tx pgx.Tx, qtx *db.Queries, bayarID pgtype.UUID) error {
	allocs, err := qtx.ListAlokasiByPembayaran(ctx, bayarID)
	if err != nil {
		return err
	}
	for _, al := range allocs {
		if al.TagihanIuranID.Valid {
			if s.cfg.Bills == nil {
				return response.NewDomainError(http.StatusInternalServerError, "BILL_PAYER_UNAVAILABLE", "Layanan iuran belum terhubung")
			}
			if err := s.cfg.Bills.UpdateBillStatusTx(ctx, tx, response.FromPGUUID(al.TagihanIuranID), "lunas"); err != nil {
				return err
			}
		}
		if al.PesananID.Valid {
			if s.cfg.Orders == nil {
				return response.NewDomainError(http.StatusInternalServerError, "ORDER_PAYER_UNAVAILABLE", "Layanan pesanan belum terhubung")
			}
			if err := s.cfg.Orders.ConfirmPaidOrderTx(ctx, tx, response.FromPGUUID(al.PesananID)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) CreateRekening(ctx context.Context, claims *middleware.SessionClaims, in CreatePembayaranInput, ip string) (*db.CreateRekeningQRISRow, error) {
	if claims.Role != "admin" && claims.Role != "bendahara" {
		return nil, response.NewDomainError(http.StatusForbidden, "FORBIDDEN", "Hanya admin atau bendahara yang dapat menambah rekening QRIS")
	}
	jenis := strings.TrimSpace(in.JenisPemilik)
	if jenis == "" {
		jenis = "badan_hukum"
	}
	if (jenis != "badan_hukum" && jenis != "perorangan") || strings.TrimSpace(in.NamaMerchant) == "" || strings.TrimSpace(in.NMID) == "" {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_REKENING", "nama_merchant, nmid, dan jenis_pemilik (badan_hukum|perorangan) wajib valid")
	}
	row, err := s.q.CreateRekeningQRIS(ctx, db.CreateRekeningQRISParams{
		NamaMerchant: strings.TrimSpace(in.NamaMerchant), Nmid: strings.TrimSpace(in.NMID),
		NamaBank: strings.TrimSpace(in.NamaBank), NomorRekening: strings.TrimSpace(in.NomorRekening),
		NamaPemilik: strings.TrimSpace(in.NamaPemilik), JenisPemilik: jenis,
		QrisImageUrl: strings.TrimSpace(in.QRISImageURL), QrisPayload: strings.TrimSpace(in.QRISPayload),
	})
	if err != nil {
		return nil, fmt.Errorf("simpan rekening QRIS: %w", err)
	}
	s.logAuditTx(ctx, nil, claims, "pembayaran.rekening_create", "rekening_qris",
		response.FromPGUUID(row.ID).String(), "Tambah rekening QRIS "+row.NamaMerchant, ip)
	return &row, nil
}

func (s *Service) ListRekening(ctx context.Context) ([]db.ListRekeningQRISRow, error) {
	return s.q.ListRekeningQRIS(ctx)
}

func (s *Service) List(ctx context.Context, claims *middleware.SessionClaims, userFilterStr, statusFilter string, lim, off int32) ([]db.ListPembayaranRow, error) {
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
	return s.q.ListPembayaran(ctx, db.ListPembayaranParams{
		UserFilter: userFilter, StatusFilter: strings.TrimSpace(statusFilter), Lim: lim, Off: off,
	})
}

func (s *Service) GetDetail(ctx context.Context, claims *middleware.SessionClaims, idStr string) (map[string]interface{}, error) {
	idPG, err := response.ParsePGUUID(idStr)
	if err != nil {
		return nil, err
	}
	row, err := s.q.GetPembayaranByID(ctx, idPG)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, response.NewDomainError(http.StatusNotFound, "PEMBAYARAN_NOT_FOUND", "Pembayaran tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	if claims.Role == "anggota" && response.FromPGUUID(row.PembayarUserID) != claims.UserID {
		return nil, response.NewDomainError(http.StatusForbidden, "FORBIDDEN", "Tidak berhak melihat pembayaran orang lain")
	}
	allocs, err := s.q.ListAlokasiByPembayaran(ctx, idPG)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"pembayaran": row, "alokasi": allocs}, nil
}

func (s *Service) ExpirePendingPayments(ctx context.Context) (int, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	ids, err := qtx.ListExpiredPendingPembayaran(ctx)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := qtx.MarkPembayaranKedaluwarsa(ctx, id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(ids), nil
}
