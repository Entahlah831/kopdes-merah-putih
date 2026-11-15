package pembayaran

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
)

type VerifyInput struct {
	Keputusan               string `json:"keputusan"`
	NominalDibayar          int64  `json:"nominal_dibayar"`
	Alasan                  string `json:"alasan"`
	SelesaikanTindakLanjut  bool   `json:"selesaikan_tindak_lanjut,omitempty"`
}

func (s *Service) proofTargetPath(bayarID uuid.UUID, ext string) (string, error) {
	if s.cfg.ProofStorageDir == "" {
		return "", response.NewDomainError(http.StatusInternalServerError, "STORAGE_NOT_CONFIGURED", "Direktori penyimpanan bukti belum siap")
	}
	name := "bukti_" + bayarID.String() + ext
	full := filepath.Join(s.cfg.ProofStorageDir, name)
	if !strings.HasPrefix(full, s.cfg.ProofStorageDir+string(filepath.Separator)) {
		return "", fmt.Errorf("path bukti keluar dari direktori")
	}
	return full, nil
}

func (s *Service) AttachBukti(ctx context.Context, claims *middleware.SessionClaims, bayarIDStr string, rawBytes []byte, catatan, ip string) (*db.AttachPembayaranBuktiRow, error) {
	idPG, err := response.ParsePGUUID(bayarIDStr)
	if err != nil {
		return nil, err
	}
	ext, buktiHash, err := validateProofImage(rawBytes, s.cfg.UploadMaxBytes)
	if err != nil {
		return nil, err
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
	if err := validateBuktiAttachEligibility(claims, cur); err != nil {
		return nil, err
	}
	if _, err := qtx.GetPembayaranByBuktiHash(ctx, response.ToPGText(buktiHash)); err == nil {
		return nil, response.NewDomainError(http.StatusConflict, "DUPLICATE_BUKTI", "Berkas bukti transfer yang sama sudah pernah digunakan")
	}
	target, err := s.proofTargetPath(response.FromPGUUID(cur.ID), ext)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- target dibangun dari UUID tervalidasi dan prefix direktori terkontrol
	if err := os.WriteFile(target, rawBytes, 0o600); err != nil {
		return nil, fmt.Errorf("simpan berkas bukti: %w", err)
	}
	buktiURL := "/api/v1/pembayaran?bukti_id=" + response.FromPGUUID(cur.ID).String() + "&ext=" + ext
	updated, err := qtx.AttachPembayaranBukti(ctx, db.AttachPembayaranBuktiParams{
		BuktiUrl:        buktiURL,
		BuktiHash:       response.ToPGText(buktiHash),
		CatatanPembayar: strings.TrimSpace(catatan),
		ID:              cur.ID,
		PembayarUserID:  cur.PembayarUserID,
	})
	if err != nil {
		return nil, fmt.Errorf("perbarui bukti pembayaran: %w", err)
	}
	_ = qtx.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "pembayaran.upload_bukti",
		Entitas:     "pembayaran",
		EntitasID:   response.FromPGUUID(cur.ID).String(),
		Ringkasan:   "Unggah bukti pembayaran " + cur.NomorPembayaran,
		IpAddress:   ip,
	})
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &updated, nil
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
	ext := ".jpg"
	if strings.Contains(row.BuktiUrl, ".png") {
		ext = ".png"
	}
	target, err := s.proofTargetPath(response.FromPGUUID(row.ID), ext)
	if err != nil {
		return nil, "", err
	}
	// #nosec G304 -- target diverifikasi proofTargetPath dari UUID
	raw, err := os.ReadFile(target)
	if err != nil {
		return nil, "", response.NewDomainError(http.StatusNotFound, "BUKTI_FILE_MISSING", "Berkas bukti tidak ditemukan di disk")
	}
	return raw, http.DetectContentType(raw), nil
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
	if response.FromPGUUID(cur.DibuatOleh) == claims.UserID {
		return nil, response.NewDomainError(http.StatusForbidden, "DUAL_CONTROL_VIOLATION", "Pembuat dan verifikator pembayaran harus orang yang berbeda")
	}
	if cur.Status != "menunggu_verifikasi" && cur.Status != "perlu_tindak_lanjut" {
		return nil, response.NewDomainError(http.StatusConflict, "INVALID_PAYMENT_STATUS", "Pembayaran tidak dalam status siap verifikasi")
	}
	updated, err := applyVerificationDecisionTx(ctx, qtx, claims, cur, keputusan, in, ip)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return updated, nil
}

func evaluateApprovalTx(
	ctx context.Context,
	qtx *db.Queries,
	cur db.GetPembayaranForUpdateRow,
	in VerifyInput,
) (finalStatus, alasan string, kwtPG pgtype.Text, err error) {
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
	kwtPG = response.ToPGText(fmt.Sprintf("KWT-%s-%06d", time.Now().UTC().Format("2006"), seq))
	return "lunas", alasan, kwtPG, nil
}

func applyVerificationDecisionTx(
	ctx context.Context,
	qtx *db.Queries,
	claims *middleware.SessionClaims,
	cur db.GetPembayaranForUpdateRow,
	keputusan string,
	in VerifyInput,
	ip string,
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
	updated, err := qtx.VerifyPembayaranDecision(ctx, db.VerifyPembayaranDecisionParams{
		Status:           finalStatus,
		NominalDibayar:   in.NominalDibayar,
		NomorKwitansi:    kwtPG,
		AlasanVerifikasi: alasan,
		DiverifikasiOleh: response.ToPGUUID(claims.UserID),
		ID:               cur.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("verifikasi pembayaran: %w", err)
	}
	if finalStatus == "lunas" {
		if err := markAllocatedTargetsPaidTx(ctx, qtx, cur.ID); err != nil {
			return nil, err
		}
	}
	_ = qtx.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "pembayaran.verify_" + finalStatus,
		Entitas:     "pembayaran",
		EntitasID:   response.FromPGUUID(cur.ID).String(),
		Ringkasan:   fmt.Sprintf("%s -> %s (dibayar %d)", cur.NomorPembayaran, finalStatus, in.NominalDibayar),
		IpAddress:   ip,
	})
	return &updated, nil
}

func markAllocatedTargetsPaidTx(ctx context.Context, qtx *db.Queries, bayarID pgtype.UUID) error {
	allocs, err := qtx.ListAlokasiByPembayaran(ctx, bayarID)
	if err != nil {
		return err
	}
	for _, al := range allocs {
		if al.TagihanIuranID.Valid {
			if err := qtx.UpdateTagihanStatus(ctx, db.UpdateTagihanStatusParams{
				Status: "lunas",
				ID:     al.TagihanIuranID,
			}); err != nil {
				return err
			}
		}
		if al.PesananID.Valid {
			if _, err := qtx.UpdatePesananStatus(ctx, db.UpdatePesananStatusParams{
				Status: "dibayar",
				ID:     al.PesananID,
			}); err != nil {
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
		NamaMerchant:  strings.TrimSpace(in.NamaMerchant),
		Nmid:          strings.TrimSpace(in.NMID),
		NamaBank:      strings.TrimSpace(in.NamaBank),
		NomorRekening: strings.TrimSpace(in.NomorRekening),
		NamaPemilik:   strings.TrimSpace(in.NamaPemilik),
		JenisPemilik:  jenis,
		QrisImageUrl:  strings.TrimSpace(in.QRISImageURL),
		QrisPayload:   strings.TrimSpace(in.QRISPayload),
	})
	if err != nil {
		return nil, fmt.Errorf("simpan rekening QRIS: %w", err)
	}
	_ = s.q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "pembayaran.rekening_create",
		Entitas:     "rekening_qris",
		EntitasID:   response.FromPGUUID(row.ID).String(),
		Ringkasan:   "Tambah rekening QRIS " + row.NamaMerchant,
		IpAddress:   ip,
	})
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
		UserFilter:   userFilter,
		StatusFilter: strings.TrimSpace(statusFilter),
		Lim:          lim,
		Off:          off,
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
