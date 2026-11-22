package pembayaran

import (
	"context"
	"errors"
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
	bayarapi "github.com/kopdes/merah-putih/internal/pembayaran/api"
	pengapi "github.com/kopdes/merah-putih/internal/pengaturan/api"
	"github.com/kopdes/merah-putih/internal/security"
)

var (
	_ bayarapi.ActiveQRISReader     = (*Service)(nil)
	_ bayarapi.ActivePaymentChecker = (*Service)(nil)
	_ bayarapi.ReportReader         = (*Service)(nil)
)

func (s *Service) activeFileStore() (filestore.FileStore, error) {
	if s != nil && s.cfg.FileStore != nil {
		return s.cfg.FileStore, nil
	}
	if s == nil || strings.TrimSpace(s.cfg.ProofStorageDir) == "" {
		return nil, response.NewDomainError(http.StatusInternalServerError, "STORAGE_NOT_CONFIGURED", "Direktori penyimpanan bukti belum siap")
	}
	var fallbacks []string
	if strings.TrimSpace(s.cfg.LegacyKYCStorageDir) != "" {
		fallbacks = append(fallbacks, s.cfg.LegacyKYCStorageDir)
	}
	store, err := filestore.NewLocalFileStore(map[string]filestore.NamespaceConfig{
		filestore.NamespaceBukti: {
			Dir:               s.cfg.ProofStorageDir,
			FallbackDirs:      fallbacks,
			KeyProvider:       s.cfg.KeyProvider,
			LegacyKeyProvider: s.cfg.LegacyKeyProvider,
		},
	})
	if err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Service) logAuditTx(ctx context.Context, tx pgx.Tx, claims *middleware.SessionClaims, aksi, entitas, entitasID, ringkasan, ip string) {
	if s == nil || s.cfg.Audit == nil {
		return
	}
	var actorPtr *uuid.UUID
	peran := "system"
	if claims != nil {
		u := claims.UserID
		actorPtr = &u
		peran = claims.Role
	}
	entry := pengapi.AuditEntry{
		ActorUserID: actorPtr, ActorPeran: peran, Aksi: aksi,
		Entitas: entitas, EntitasID: entitasID, Ringkasan: ringkasan, IPAddress: ip,
	}
	if tx != nil {
		_ = s.cfg.Audit.LogTx(ctx, tx, entry)
		return
	}
	_ = s.cfg.Audit.Log(ctx, entry)
}

func (s *Service) GetPublicQRISInfo(ctx context.Context, activeID *uuid.UUID) (bayarapi.PublicQRISInfo, error) {
	if activeID == nil || *activeID == uuid.Nil {
		return bayarapi.PublicQRISInfo{}, nil
	}
	r, err := s.q.GetActiveRekeningQRISByID(ctx, response.ToPGUUID(*activeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return bayarapi.PublicQRISInfo{}, nil
	}
	if err != nil {
		return bayarapi.PublicQRISInfo{}, err
	}
	return bayarapi.PublicQRISInfo{
		RekeningID:    response.FromPGUUID(r.ID).String(),
		NamaMerchant:  r.NamaMerchant,
		NMID:          r.Nmid,
		NamaBank:      r.NamaBank,
		NomorRekening: r.NomorRekening,
		JenisPemilik:  r.JenisPemilik,
		QRISImageURL:  r.QrisImageUrl,
	}, nil
}

func (s *Service) HasActivePembayaranForPesanan(ctx context.Context, orderID uuid.UUID) (bool, error) {
	return s.q.HasActivePembayaranForPesanan(ctx, response.ToPGUUID(orderID))
}

func (s *Service) HasActivePembayaranForPesananTx(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (bool, error) {
	return s.q.WithTx(tx).HasActivePembayaranForPesanan(ctx, response.ToPGUUID(orderID))
}

func (s *Service) HasActivePembayaranForTagihan(ctx context.Context, billID uuid.UUID) (bool, error) {
	return s.q.HasActivePembayaranForTagihan(ctx, response.ToPGUUID(billID))
}

func (s *Service) GetPaymentSummaryReport(ctx context.Context, dari, sampai *time.Time) ([]bayarapi.RekapPembayaranRow, error) {
	var dariPG, sampaiPG pgtype.Date
	if dari != nil {
		dariPG = response.ToPGDate(*dari)
	}
	if sampai != nil {
		sampaiPG = response.ToPGDate(*sampai)
	}
	tz := strings.TrimSpace(s.cfg.Timezone)
	if tz == "" {
		tz = "Asia/Jakarta"
	}
	rows, err := s.q.LaporanPembayaranRekap(ctx, db.LaporanPembayaranRekapParams{
		Dari: dariPG, Sampai: sampaiPG, Tz: tz,
	})
	if err != nil {
		return nil, err
	}
	out := make([]bayarapi.RekapPembayaranRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, bayarapi.RekapPembayaranRow{
			JenisTransaksi: r.JenisTransaksi, Metode: r.Metode, Status: r.Status,
			JumlahTransaksi: r.JumlahTransaksi, TotalPokok: r.TotalPokok,
			TotalSelisihKodeUnik: r.TotalSelisihKodeUnik, TotalDibayar: r.TotalDibayar,
			TotalDikembalikan: r.TotalDikembalikan,
		})
	}
	return out, nil
}

func readLegacyDecrypted(s *Service, objectKey string, aad []byte) ([]byte, string, error) {
	legacyKP := s.cfg.LegacyKeyProvider
	if legacyKP == nil {
		legacyKP = s.cfg.KeyProvider
	}
	for _, dir := range []string{s.cfg.ProofStorageDir, s.cfg.LegacyKYCStorageDir} {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		target, err := filestore.SafeObjectPath(dir, objectKey)
		if err != nil {
			continue
		}
		// #nosec G304,G703 -- target tervalidasi SafeObjectPath berbasis UUID
		raw, rErr := os.ReadFile(target)
		if rErr != nil {
			continue
		}
		if legacyKP != nil {
			plain, dErr := legacyKP.DecryptWithAAD(raw, aad)
			if dErr != nil {
				return nil, "", dErr
			}
			return plain, http.DetectContentType(plain), nil
		}
		return raw, http.DetectContentType(raw), nil
	}
	return nil, "", response.NewDomainError(http.StatusNotFound, "BUKTI_FILE_MISSING", "Berkas bukti tidak ditemukan di disk")
}

func formatBuktiAAD(entity, id string) []byte {
	return security.FormatAAD(entity, "bukti", id)
}
