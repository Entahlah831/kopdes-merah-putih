package pendaftaran

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
	"github.com/kopdes/merah-putih/internal/auth"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

func (s *Service) UploadDokumen(ctx context.Context, claims *middleware.SessionClaims, idStr, uploadTok, tipe string, raw []byte, ip string) (map[string]interface{}, error) {
	if !slices.Contains([]string{"ktp", "kk", "selfie_ktp"}, tipe) {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_DOC_TYPE", "tipe_dokumen wajib salah satu dari ktp, kk, selfie_ktp")
	}
	pgID, err := response.ParsePGUUID(idStr)
	if err != nil {
		return nil, err
	}
	q := db.New(s.pool)
	row, err := q.GetPendaftaranByID(ctx, pgID)
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
	q := db.New(s.pool)
	var oldKey string
	if prev, err := q.GetDokumenKYCByPendaftaranAndTipe(ctx, db.GetDokumenKYCByPendaftaranAndTipeParams{PendaftaranID: pgID, TipeDokumen: tipe}); err == nil {
		oldKey = prev.StorageKey
	}
	stored, err := s.store.SaveEncrypted(idStr, tipe, raw)
	if err != nil {
		return nil, err
	}
	doc, err := q.UpsertDokumenKYC(ctx, db.UpsertDokumenKYCParams{
		PendaftaranID: pgID, TipeDokumen: tipe, StorageKey: stored.StorageKey,
		KeyID: stored.KeyID, DekWrapped: stored.DEKWrapped, MimeType: stored.MimeType,
		UkuranBytes: stored.SizeBytes, LebarPx: stored.WidthPx, TinggiPx: stored.HeightPx,
		Sha256Plain: stored.SHA256Plain,
	})
	if err != nil {
		s.store.Remove(stored.StorageKey)
		return nil, err
	}
	if oldKey != "" && oldKey != stored.StorageKey {
		s.store.Remove(oldKey)
	}
	if status == "perlu_perbaikan" {
		_ = q.ResubmitPendaftaranAfterFix(ctx, pgID)
	}
	var actorUID pgtype.UUID
	actorPeran := "publik"
	if claims != nil {
		actorUID = response.ToPGUUID(claims.UserID)
		actorPeran = claims.Role
	}
	_ = q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: actorUID, ActorPeran: actorPeran, Aksi: "upload_kyc",
		Entitas: "dokumen_kyc", EntitasID: uuid.UUID(doc.ID.Bytes).String(),
		Ringkasan: "Unggah dokumen terenkripsi tipe=" + tipe, IpAddress: ip,
	})
	return map[string]interface{}{
		"id": uuid.UUID(doc.ID.Bytes), "pendaftaran_id": idStr, "tipe_dokumen": doc.TipeDokumen,
		"mime_type": doc.MimeType, "ukuran_bytes": doc.UkuranBytes, "lebar_px": doc.LebarPx, "tinggi_px": doc.TinggiPx,
	}, nil
}

func (s *Service) ListPendaftaran(ctx context.Context, statusFilter string, lim, off int32) ([]db.ListPendaftaranRow, error) {
	return db.New(s.pool).ListPendaftaran(ctx, db.ListPendaftaranParams{
		StatusFilter: strings.TrimSpace(statusFilter), Lim: lim, Off: off,
	})
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
	_ = qt.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID), ActorPeran: claims.Role, Aksi: "verify_pendaftaran_" + in.Keputusan,
		Entitas: "pendaftaran", EntitasID: idStr, Ringkasan: strings.TrimSpace(in.Catatan), IpAddress: ip,
	})
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": uuid.UUID(dec.ID.Bytes), "status": dec.Status, "catatan_verifikasi": dec.CatatanVerifikasi}, nil
}

func (s *Service) approveInTx(ctx context.Context, tx pgx.Tx, qt *db.Queries, row db.GetPendaftaranByIDForUpdateRow, claims *middleware.SessionClaims, note, ip string) (map[string]interface{}, error) {
	docs, err := qt.ListDokumenKYCByPendaftaran(ctx, row.ID)
	if err != nil || !hasKTPDocument(docs) {
		return nil, response.NewDomainError(http.StatusBadRequest, "KTP_REQUIRED", "Dokumen KTP wajib diunggah sebelum persetujuan anggota")
	}
	regID := uuid.UUID(row.ID.Bytes).String()
	hpPlain, _ := s.cfg.KeyProvider.DecryptWithAAD(row.HpEnc, security.FormatAAD("pendaftaran", "hp", regID))
	u, err := s.createApprovedUserTx(ctx, qt, row, regID, hpPlain)
	if err != nil {
		return nil, err
	}
	ang, anggotaID, err := s.createApprovedAnggotaTx(ctx, qt, row, u.ID, regID, hpPlain)
	if err != nil {
		return nil, err
	}
	if _, err := qt.UpdatePendaftaranDecision(ctx, db.UpdatePendaftaranDecisionParams{
		ID: row.ID, Status: "disetujui", CatatanVerifikasi: strings.TrimSpace(note),
		VerifiedBy: response.ToPGUUID(claims.UserID), AnggotaID: ang.ID,
	}); err != nil {
		return nil, err
	}
	code, waLink, err := auth.CreateActivationOrResetCodeTx(ctx, tx, u.ID, response.ToPGUUID(claims.UserID), "aktivasi_awal", string(hpPlain), 24*time.Hour)
	if err != nil {
		return nil, err
	}
	_ = qt.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID), ActorPeran: claims.Role, Aksi: "verify_pendaftaran_disetujui",
		Entitas: "anggota", EntitasID: anggotaID.String(), Ringkasan: "Persetujuan anggota " + ang.NomorAnggota, IpAddress: ip,
	})
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id": regID, "status": "disetujui", "anggota_id": anggotaID,
		"nomor_anggota": ang.NomorAnggota, "activation_code": code, "wa_link": waLink,
	}, nil
}

func (s *Service) createApprovedUserTx(ctx context.Context, qt *db.Queries, row db.GetPendaftaranByIDForUpdateRow, regID string, hpPlain []byte) (db.CreateUserRow, error) {
	userID := newUUIDv7()
	var emailEnc []byte
	if len(row.EmailEnc) > 0 {
		emPlain, _ := s.cfg.KeyProvider.DecryptWithAAD(row.EmailEnc, security.FormatAAD("pendaftaran", "email", regID))
		emailEnc, _ = s.cfg.KeyProvider.EncryptWithAAD(emPlain, security.FormatAAD("users", "email", userID.String()))
	}
	userHP, _ := s.cfg.KeyProvider.EncryptWithAAD(hpPlain, security.FormatAAD("users", "hp", userID.String()))
	u, err := qt.CreateUser(ctx, db.CreateUserParams{
		ID: response.ToPGUUID(userID), EmailEnc: emailEnc, EmailHash: row.EmailHash,
		HpEnc: userHP, HpHash: pgtype.Text{String: row.HpHash, Valid: true},
		NamaLengkap: row.NamaLengkap, PasswordHash: "", Peran: "anggota", StatusAktif: true,
	})
	if err != nil {
		return u, response.NewDomainError(http.StatusConflict, "USER_CONFLICT", "Akun dengan email/HP ini sudah ada")
	}
	return u, nil
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
	pgID, err := response.ParsePGUUID(idStr)
	if err != nil {
		return nil, "", err
	}
	q := db.New(s.pool)
	doc, err := q.GetDokumenKYCByPendaftaranAndTipe(ctx, db.GetDokumenKYCByPendaftaranAndTipeParams{
		PendaftaranID: pgID, TipeDokumen: tipe,
	})
	if err != nil {
		return nil, "", err
	}
	plain, err := s.store.LoadDecrypted(idStr, tipe, doc.StorageKey, doc.DekWrapped)
	if err != nil {
		return nil, "", err
	}
	_ = q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID), ActorPeran: claims.Role, Aksi: "lihat_dokumen_kyc",
		Entitas: "dokumen_kyc", EntitasID: uuid.UUID(doc.ID.Bytes).String(), Ringkasan: "Dekripsi dokumen " + tipe, IpAddress: ip,
	})
	return plain, doc.MimeType, nil
}

// PurgeExpiredRejected menghapus pendaftaran ditolak melewati masa retensi PDP (PERLU VERIFIKASI HUKUM) beserta berkas KYC terenkripsi di disk.
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
			s.store.Remove(d.StorageKey)
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
