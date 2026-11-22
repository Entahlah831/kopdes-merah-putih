package anggota

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	authapi "github.com/kopdes/merah-putih/internal/auth/api"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/filestore"
	"github.com/kopdes/merah-putih/internal/security"
)

type PendaftaranListItem struct {
	db.ListPendaftaranRow
	DokumenTersedia []string `json:"dokumen_tersedia"`
}

func (s *Service) UploadDokumen(ctx context.Context, claims *middleware.SessionClaims, idStr, uploadTok, tipe string, raw []byte, ip string) (map[string]interface{}, error) {
	if !slices.Contains([]string{"ktp", "kk", "selfie_ktp"}, tipe) {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_DOC_TYPE", "tipe_dokumen wajib salah satu dari ktp, kk, selfie_ktp")
	}
	pgID, err := response.ParsePGUUID(idStr)
	if err != nil {
		return nil, err
	}
	row, err := db.New(s.pool).GetPendaftaranByID(ctx, pgID)
	if err != nil {
		return nil, err
	}
	tokHash := security.HashOpaqueToken(uploadTok)
	if strings.TrimSpace(uploadTok) == "" || !security.ConstantTimeEqualHex(row.UploadTokenHash, tokHash) {
		return nil, response.NewDomainError(http.StatusUnauthorized, "INVALID_UPLOAD_TOKEN", "X-Upload-Token tidak valid untuk pendaftaran ini")
	}
	if row.Status != "diajukan" && row.Status != "perlu_perbaikan" {
		return nil, response.NewDomainError(http.StatusConflict, "REGISTRATION_LOCKED", "Dokumen tidak dapat diubah karena pendaftaran sudah diputuskan")
	}
	return s.persistKYCDocument(ctx, row.ID, uuid.UUID(row.ID.Bytes).String(), row.Status, tipe, raw, claims, ip)
}

func (s *Service) persistKYCDocument(ctx context.Context, pgID pgtype.UUID, idStr, status, tipe string, raw []byte, claims *middleware.SessionClaims, ip string) (map[string]interface{}, error) {
	if s.cfg.FileStore == nil {
		return nil, filestore.ErrStorageUnavailable
	}
	meta, err := filestore.ValidateImage(raw, s.cfg.MaxKYCUploadBytes)
	if err != nil {
		return nil, err
	}
	q := db.New(s.pool)
	var oldKey string
	if prev, pErr := q.GetDokumenKYCByPendaftaranAndTipe(ctx, db.GetDokumenKYCByPendaftaranAndTipeParams{PendaftaranID: pgID, TipeDokumen: tipe}); pErr == nil {
		oldKey = prev.StorageKey
	}
	aad := security.FormatAAD("dokumen_kyc", tipe, idStr)
	stored, err := s.cfg.FileStore.SaveEncrypted(ctx, filestore.NamespaceKYC, uuid.NewString()+".enc", raw, aad)
	if err != nil {
		return nil, err
	}
	doc, err := q.UpsertDokumenKYC(ctx, db.UpsertDokumenKYCParams{
		PendaftaranID: pgID, TipeDokumen: tipe, StorageKey: stored.ObjectKey,
		KeyID: stored.KeyID, DekWrapped: stored.DEKWrapped, MimeType: meta.MimeType,
		UkuranBytes: meta.SizeBytes, LebarPx: meta.WidthPx, TinggiPx: meta.HeightPx,
		Sha256Plain: meta.SHA256Hex,
	})
	if err != nil {
		_ = s.cfg.FileStore.Delete(ctx, filestore.NamespaceKYC, stored.ObjectKey)
		return nil, err
	}
	s.finalizeKYCUploadEffects(ctx, q, pgID, doc, oldKey, stored.ObjectKey, status, tipe, claims, ip)
	return map[string]interface{}{
		"id": uuid.UUID(doc.ID.Bytes), "pendaftaran_id": idStr, "tipe_dokumen": doc.TipeDokumen,
		"mime_type": doc.MimeType, "ukuran_bytes": doc.UkuranBytes, "lebar_px": doc.LebarPx, "tinggi_px": doc.TinggiPx,
	}, nil
}

func (s *Service) finalizeKYCUploadEffects(ctx context.Context, q *db.Queries, pgID pgtype.UUID, doc db.UpsertDokumenKYCRow, oldKey, newKey, status, tipe string, claims *middleware.SessionClaims, ip string) {
	if oldKey != "" && oldKey != newKey {
		_ = s.cfg.FileStore.Delete(ctx, filestore.NamespaceKYC, oldKey)
	}
	if status == "perlu_perbaikan" {
		_ = q.ResubmitPendaftaranAfterFix(ctx, pgID)
	}
	var actorPtr *uuid.UUID
	actorPeran := "publik"
	if claims != nil {
		actorPtr = uuidPtr(claims.UserID)
		actorPeran = claims.Role
	}
	s.logAudit(ctx, actorPtr, actorPeran, "upload_kyc", "dokumen_kyc",
		uuid.UUID(doc.ID.Bytes).String(), "Unggah dokumen terenkripsi tipe="+tipe, ip)
}

func (s *Service) ListPendaftaran(ctx context.Context, statusFilter string, lim, off int32) ([]PendaftaranListItem, error) {
	q := db.New(s.pool)
	rows, err := q.ListPendaftaran(ctx, db.ListPendaftaranParams{
		StatusFilter: strings.TrimSpace(statusFilter), Lim: lim, Off: off,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	docMap := map[uuid.UUID][]string{}
	if len(ids) > 0 {
		if docs, dErr := q.ListDokumenTipeByPendaftaranIDs(ctx, ids); dErr == nil {
			for _, d := range docs {
				pid := response.FromPGUUID(d.PendaftaranID)
				docMap[pid] = append(docMap[pid], d.TipeDokumen)
			}
		}
	}
	out := make([]PendaftaranListItem, 0, len(rows))
	for _, r := range rows {
		avail := docMap[response.FromPGUUID(r.ID)]
		if avail == nil {
			avail = []string{}
		}
		out = append(out, PendaftaranListItem{ListPendaftaranRow: r, DokumenTersedia: avail})
	}
	return out, nil
}

func (s *Service) VerifyDecision(ctx context.Context, claims *middleware.SessionClaims, idStr string, in VerifyDecisionInput, ip string) (map[string]interface{}, error) {
	if !slices.Contains([]string{"disetujui", "ditolak", "perlu_perbaikan"}, in.Keputusan) {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_DECISION", "keputusan wajib disetujui, ditolak, atau perlu_perbaikan")
	}
	if in.Keputusan != "disetujui" && strings.TrimSpace(in.Catatan) == "" {
		return nil, response.NewDomainError(http.StatusBadRequest, "NOTE_REQUIRED", "catatan wajib diisi untuk penolakan atau permintaan perbaikan")
	}
	pgID, err := response.ParsePGUUID(idStr)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qt := db.New(tx)
	row, err := qt.GetPendaftaranByIDForUpdate(ctx, pgID)
	if err != nil {
		return nil, err
	}
	if row.Status != "diajukan" && row.Status != "perlu_perbaikan" {
		return nil, response.NewDomainError(http.StatusConflict, "ALREADY_DECIDED", "Pendaftaran sudah memiliki keputusan akhir")
	}
	if in.Keputusan == "disetujui" {
		return s.approveInTx(ctx, tx, qt, row, claims, in.Catatan, ip)
	}
	dec, err := qt.UpdatePendaftaranDecision(ctx, db.UpdatePendaftaranDecisionParams{
		ID: row.ID, Status: in.Keputusan, CatatanVerifikasi: strings.TrimSpace(in.Catatan),
		VerifiedBy: response.ToPGUUID(claims.UserID),
	})
	if err != nil {
		return nil, err
	}
	s.logAuditTx(ctx, tx, uuidPtr(claims.UserID), claims.Role, "verify_pendaftaran_"+in.Keputusan, "pendaftaran", idStr, strings.TrimSpace(in.Catatan), ip)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": uuid.UUID(dec.ID.Bytes), "status": dec.Status, "catatan_verifikasi": dec.CatatanVerifikasi}, nil
}

func (s *Service) approveInTx(ctx context.Context, tx pgx.Tx, qt *db.Queries, row db.GetPendaftaranByIDForUpdateRow, claims *middleware.SessionClaims, note, ip string) (map[string]interface{}, error) {
	if s.cfg.UserManager == nil {
		return nil, response.NewDomainError(http.StatusInternalServerError, "USER_MANAGER_UNAVAILABLE", "Pengelola akun user belum siap")
	}
	docs, err := qt.ListDokumenKYCByPendaftaran(ctx, row.ID)
	if err != nil || !hasKTPDocument(docs) {
		return nil, response.NewDomainError(http.StatusBadRequest, "KTP_REQUIRED", "Dokumen KTP wajib diunggah sebelum persetujuan anggota")
	}
	regID := uuid.UUID(row.ID.Bytes).String()
	hpPlain, _ := s.cfg.KeyProvider.DecryptWithAAD(row.HpEnc, security.FormatAAD("pendaftaran", "hp", regID))
	var emPlain []byte
	if len(row.EmailEnc) > 0 {
		emPlain, _ = s.cfg.KeyProvider.DecryptWithAAD(row.EmailEnc, security.FormatAAD("pendaftaran", "email", regID))
	}
	uCreated, err := s.cfg.UserManager.CreateMemberUserTx(ctx, tx, authapi.CreateMemberUserInput{
		Email: string(emPlain), NoHP: string(hpPlain), NamaLengkap: row.NamaLengkap, Peran: "anggota",
	})
	if err != nil {
		return nil, err
	}
	ang, anggotaID, err := s.createApprovedAnggotaTx(ctx, qt, row, response.ToPGUUID(uCreated.UserID), regID, hpPlain)
	if err != nil {
		return nil, err
	}
	if _, err := qt.UpdatePendaftaranDecision(ctx, db.UpdatePendaftaranDecisionParams{
		ID: row.ID, Status: "disetujui", CatatanVerifikasi: strings.TrimSpace(note),
		VerifiedBy: response.ToPGUUID(claims.UserID), AnggotaID: ang.ID,
	}); err != nil {
		return nil, err
	}
	actorID := claims.UserID
	codeRes, err := s.cfg.UserManager.CreateActivationOrResetCodeTx(ctx, tx, uCreated.UserID, &actorID, "aktivasi_awal", string(hpPlain), 24*time.Hour)
	if err != nil {
		return nil, err
	}
	s.logAuditTx(ctx, tx, uuidPtr(claims.UserID), claims.Role, "verify_pendaftaran_disetujui", "anggota",
		anggotaID.String(), "Persetujuan anggota "+ang.NomorAnggota, ip)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id": regID, "status": "disetujui", "anggota_id": anggotaID,
		"nomor_anggota": ang.NomorAnggota, "activation_code": codeRes.Code, "wa_link": codeRes.WhatsAppLink,
	}, nil
}

func (s *Service) createApprovedAnggotaTx(ctx context.Context, qt *db.Queries, row db.GetPendaftaranByIDForUpdateRow, userID pgtype.UUID, regID string, hpPlain []byte) (db.CreateAnggotaRow, uuid.UUID, error) {
	nikPlain, _ := s.cfg.KeyProvider.DecryptWithAAD(row.NikEnc, security.FormatAAD("pendaftaran", "nik", regID))
	addrPlain, _ := s.cfg.KeyProvider.DecryptWithAAD(row.AlamatEnc, security.FormatAAD("pendaftaran", "alamat", regID))

	anggotaID := newUUIDv7()
	total, _ := qt.CountTotalAnggota(ctx)
	nomorAnggota := fmt.Sprintf("KMP-%s-%04d-%s", time.Now().Format("200601"), total+1, strings.ToUpper(anggotaID.String()[:4]))
	angNikEnc, _ := s.cfg.KeyProvider.EncryptWithAAD(nikPlain, security.FormatAAD("anggota", "nik", anggotaID.String()))
	angHPEnc, _ := s.cfg.KeyProvider.EncryptWithAAD(hpPlain, security.FormatAAD("anggota", "hp", anggotaID.String()))
	var angDobEnc []byte
	if len(row.TanggalLahirEnc) > 0 {
		if dobPlain, err := s.cfg.KeyProvider.DecryptWithAAD(row.TanggalLahirEnc, security.FormatAAD("pendaftaran", "tanggal_lahir", regID)); err == nil && len(dobPlain) > 0 {
			angDobEnc, _ = s.cfg.KeyProvider.EncryptWithAAD(dobPlain, security.FormatAAD("anggota", "tanggal_lahir", anggotaID.String()))
		}
	}
	angAddrEnc, _ := s.cfg.KeyProvider.EncryptWithAAD(addrPlain, security.FormatAAD("anggota", "alamat", anggotaID.String()))

	ang, err := qt.CreateAnggota(ctx, db.CreateAnggotaParams{
		ID: response.ToPGUUID(anggotaID), UserID: userID, NomorAnggota: nomorAnggota, NamaLengkap: row.NamaLengkap,
		NikEnc: angNikEnc, NikHash: row.NikHash, NikKeyVersion: row.NikKeyVersion, NikMasked: row.NikMasked,
		HpEnc: angHPEnc, HpMasked: row.HpMasked, TanggalLahirEnc: angDobEnc, AlamatEnc: angAddrEnc, Status: "aktif",
	})
	if err != nil {
		return ang, anggotaID, response.NewDomainError(http.StatusConflict, "MEMBER_ALREADY_EXISTS", "NIK anggota sudah aktif")
	}
	return ang, anggotaID, nil
}

func (s *Service) GetDokumenDecrypted(ctx context.Context, claims *middleware.SessionClaims, idStr, tipe, ip string) ([]byte, string, error) {
	if s.cfg.FileStore == nil {
		return nil, "", filestore.ErrStorageUnavailable
	}
	pgID, err := response.ParsePGUUID(idStr)
	if err != nil {
		return nil, "", err
	}
	doc, err := db.New(s.pool).GetDokumenKYCByPendaftaranAndTipe(ctx, db.GetDokumenKYCByPendaftaranAndTipeParams{
		PendaftaranID: pgID, TipeDokumen: tipe,
	})
	if err != nil {
		return nil, "", err
	}
	plain, err := s.cfg.FileStore.LoadDecrypted(ctx, filestore.NamespaceKYC, doc.StorageKey, doc.DekWrapped, security.FormatAAD("dokumen_kyc", tipe, idStr))
	if err != nil {
		return nil, "", err
	}
	s.logAudit(ctx, uuidPtr(claims.UserID), claims.Role, "lihat_dokumen_kyc", "dokumen_kyc",
		uuid.UUID(doc.ID.Bytes).String(), "Dekripsi dokumen "+tipe, ip)
	return plain, doc.MimeType, nil
}

// PurgeExpiredRejected menghapus pendaftaran ditolak melewati masa retensi PDP (PERLU VERIFIKASI HUKUM) beserta berkas KYC terenkripsi.
func (s *Service) PurgeExpiredRejected(ctx context.Context, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		retentionDays = s.cfg.RetentionDays
	}
	q := db.New(s.pool)
	rows, err := q.ListExpiredRejectedPendaftaran(ctx, int32(retentionDays)) // #nosec G115 -- hari retensi bernilai wajar
	if err != nil {
		return 0, err
	}
	purged := 0
	for _, r := range rows {
		docs, _ := q.ListDokumenKYCByPendaftaran(ctx, r.ID)
		for _, d := range docs {
			if s.cfg.FileStore != nil {
				_ = s.cfg.FileStore.Delete(ctx, filestore.NamespaceKYC, d.StorageKey)
			}
		}
		if err := q.DeletePendaftaranByID(ctx, r.ID); err == nil {
			purged++
		}
	}
	return purged, nil
}

func hasKTPDocument(docs []db.ListDokumenKYCByPendaftaranRow) bool {
	for _, d := range docs {
		if d.TipeDokumen == "ktp" {
			return true
		}
	}
	return false
}

func newUUIDv7() uuid.UUID {
	if u, err := uuid.NewV7(); err == nil {
		return u
	}
	return uuid.New()
}
