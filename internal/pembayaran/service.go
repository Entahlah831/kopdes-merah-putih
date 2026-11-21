package pembayaran

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

type Config struct {
	PaymentsEnabled              bool
	AllowPersonalPilot           bool
	PilotMaxPerTx                int64
	PilotMaxPerDay               int64
	QRISExpiryHours              int
	PaymentExpiry                time.Duration
	MaxPendingPerUser            int
	MaxPendingGlobal             int
	DualControlBreakglassEnabled bool
	Timezone                     string
	UploadMaxBytes               int64
	ProofStorageDir              string
	LegacyKYCStorageDir          string
	BuktiRetentionDays           int
	KeyProvider                  security.KeyProvider
	DBKeyProvider                security.KeyProvider
	LegacyKeyProvider            security.KeyProvider
	TrustedProxies               []string
}

type Service struct {
	pool    *pgxpool.Pool
	q       *db.Queries
	cfg     Config
	trusted *middleware.TrustedProxyMatcher
}

func NewService(pool *pgxpool.Pool, cfg Config) (*Service, error) {
	if cfg.QRISExpiryHours <= 0 {
		cfg.QRISExpiryHours = 24
	}
	if cfg.PaymentExpiry <= 0 {
		cfg.PaymentExpiry = time.Duration(cfg.QRISExpiryHours) * time.Hour
	}
	if cfg.MaxPendingPerUser <= 0 {
		cfg.MaxPendingPerUser = 3
	}
	if cfg.MaxPendingGlobal <= 0 {
		cfg.MaxPendingGlobal = 200
	}
	if cfg.Timezone == "" {
		cfg.Timezone = "Asia/Jakarta"
	}
	if cfg.UploadMaxBytes <= 0 {
		cfg.UploadMaxBytes = 5 * 1024 * 1024
	}
	if cfg.BuktiRetentionDays <= 0 {
		cfg.BuktiRetentionDays = 365
	}
	if strings.TrimSpace(cfg.ProofStorageDir) != "" {
		abs, err := filepath.Abs(cfg.ProofStorageDir)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(abs, 0o700); err != nil {
			return nil, fmt.Errorf("buat direktori bukti bayar %s: %w", abs, err)
		}
		cfg.ProofStorageDir = abs
	}
	return &Service{
		pool:    pool,
		q:       db.New(pool),
		cfg:     cfg,
		trusted: middleware.NewTrustedProxyMatcher(cfg.TrustedProxies),
	}, nil
}

type CreatePembayaranInput struct {
	Action          string   `json:"action,omitempty"`
	PembayaranID    string   `json:"pembayaran_id,omitempty"`
	JenisTransaksi  string   `json:"jenis_transaksi"`
	Metode          string   `json:"metode"`
	TagihanIDs      []string `json:"tagihan_ids,omitempty"`
	PesananID       string   `json:"pesanan_id,omitempty"`
	CatatanPembayar string   `json:"catatan_pembayar,omitempty"`
	IdempotencyKey  string   `json:"idempotency_key,omitempty"`
	NamaMerchant    string   `json:"nama_merchant,omitempty"`
	NMID            string   `json:"nmid,omitempty"`
	NamaBank        string   `json:"nama_bank,omitempty"`
	NomorRekening   string   `json:"nomor_rekening,omitempty"`
	NamaPemilik     string   `json:"nama_pemilik,omitempty"`
	JenisPemilik    string   `json:"jenis_pemilik,omitempty"`
	QRISImageURL    string   `json:"qris_image_url,omitempty"`
	QRISPayload     string   `json:"qris_payload,omitempty"`
}

type PembayaranResponse struct {
	Pembayaran db.CreatePembayaranRow `json:"pembayaran"`
	Rekening   *RekeningInfo          `json:"rekening_qris,omitempty"`
}

type RekeningInfo struct {
	ID           uuid.UUID `json:"id"`
	NamaMerchant string    `json:"nama_merchant"`
	NMID         string    `json:"nmid"`
	NamaBank     string    `json:"nama_bank"`
	JenisPemilik string    `json:"jenis_pemilik"`
	QRISImageURL string    `json:"qris_image_url"`
	QRISPayload  string    `json:"qris_payload"`
}

type targetResolution struct {
	nominalPokok   int64
	pembayarUserID uuid.UUID
	anggotaPG      pgtype.UUID
	tagihanPGs     []pgtype.UUID
	pesananPG      pgtype.UUID
	allocNominals  map[string]int64
}

func (s *Service) CreatePembayaran(ctx context.Context, claims *middleware.SessionClaims, in CreatePembayaranInput, idemKey, ip string) (*PembayaranResponse, bool, error) {
	if !s.cfg.PaymentsEnabled {
		return nil, false, response.NewDomainError(http.StatusServiceUnavailable, "PAYMENTS_DISABLED", "Fitur pembayaran sedang dinonaktifkan")
	}
	if idemKey == "" {
		idemKey = strings.TrimSpace(in.IdempotencyKey)
	}
	if idemKey != "" {
		existing, err := s.q.GetPembayaranByIdempotencyKey(ctx, response.ToPGText(idemKey))
		if err == nil && response.FromPGUUID(existing.PembayarUserID) == claims.UserID {
			return &PembayaranResponse{Pembayaran: mapCreateRowFromIdem(existing)}, true, nil
		}
	}
	jenis := strings.TrimSpace(in.JenisTransaksi)
	metode := strings.TrimSpace(in.Metode)
	if (jenis != "iuran" && jenis != "pesanan") || (metode != "qris" && metode != "tunai") {
		return nil, false, response.NewDomainError(http.StatusBadRequest, "INVALID_PAYMENT_INPUT", "jenis_transaksi (iuran|pesanan) dan metode (qris|tunai) wajib valid")
	}
	if metode == "tunai" && claims.Role == "anggota" {
		return nil, false, response.NewDomainError(http.StatusForbidden, "FORBIDDEN", "Pembayaran tunai dicatat oleh petugas")
	}
	res, err := s.createPembayaranTx(ctx, claims, jenis, metode, in, idemKey, ip)
	return res, false, err
}

func (s *Service) createPembayaranTx(ctx context.Context, claims *middleware.SessionClaims, jenis, metode string, in CreatePembayaranInput, idemKey, ip string) (*PembayaranResponse, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	target, err := resolvePaymentTargetTx(ctx, qtx, claims, jenis, in)
	if err != nil {
		return nil, err
	}
	params, rekInfo, err := s.preparePaymentParamsTx(ctx, qtx, claims, jenis, metode, in.CatatanPembayar, idemKey, target)
	if err != nil {
		return nil, err
	}
	created, err := qtx.CreatePembayaran(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("simpan pembayaran: %w", err)
	}
	if err := saveAllocationsTx(ctx, qtx, created.ID, target); err != nil {
		return nil, err
	}
	_ = qtx.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "pembayaran.create_" + metode,
		Entitas:     "pembayaran",
		EntitasID:   response.FromPGUUID(created.ID).String(),
		Ringkasan:   fmt.Sprintf("%s %s nominal %d", created.NomorPembayaran, metode, created.NominalTotal),
		IpAddress:   ip,
	})
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &PembayaranResponse{Pembayaran: created, Rekening: rekInfo}, nil
}

func resolvePaymentTargetTx(ctx context.Context, qtx *db.Queries, claims *middleware.SessionClaims, jenis string, in CreatePembayaranInput) (*targetResolution, error) {
	if jenis == "iuran" {
		return resolveTagihanTargetsTx(ctx, qtx, claims, in.TagihanIDs)
	}
	return resolvePesananTargetTx(ctx, qtx, claims, in.PesananID)
}

func resolveTagihanTargetsTx(ctx context.Context, qtx *db.Queries, claims *middleware.SessionClaims, ids []string) (*targetResolution, error) {
	if len(ids) == 0 || len(ids) > 24 {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_TAGIHAN", "tagihan_ids wajib 1 sampai 24 item")
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	res := &targetResolution{pembayarUserID: claims.UserID, allocNominals: make(map[string]int64, len(sorted))}
	var callerAnggotaID pgtype.UUID
	if claims.Role == "anggota" {
		ag, err := qtx.GetAnggotaByUserID(ctx, response.ToPGUUID(claims.UserID))
		if err != nil {
			return nil, response.NewDomainError(http.StatusForbidden, "NOT_A_MEMBER", "Akun tidak terhubung dengan data anggota")
		}
		callerAnggotaID = ag.ID
	}
	seen := make(map[string]bool, len(sorted))
	for _, raw := range sorted {
		pgID, err := response.ParsePGUUID(raw)
		if err != nil {
			return nil, err
		}
		idKey := response.FromPGUUID(pgID).String()
		if seen[idKey] {
			continue
		}
		seen[idKey] = true
		t, err := qtx.GetTagihanIuranForUpdate(ctx, pgID)
		if err != nil {
			return nil, err
		}
		if claims.Role == "anggota" && t.AnggotaID != callerAnggotaID {
			return nil, response.NewDomainError(http.StatusForbidden, "FORBIDDEN", "Tidak berhak membayar tagihan anggota lain")
		}
		if t.Status != "belum_bayar" {
			return nil, response.NewDomainError(http.StatusConflict, "TAGIHAN_NOT_PAYABLE", "Tagihan harus berstatus belum_bayar")
		}
		active, err := qtx.HasActivePembayaranForTagihan(ctx, pgID)
		if err != nil {
			return nil, err
		}
		if active {
			return nil, response.NewDomainError(http.StatusConflict, "ACTIVE_PAYMENT_EXISTS", "Tagihan sudah memiliki pembayaran aktif menunggu selesai")
		}
		netto := t.Nominal - t.NominalKeringanan
		if netto <= 0 {
			return nil, response.NewDomainError(http.StatusBadRequest, "ZERO_BILL", "Tagihan bernilai 0 tidak memerlukan pembayaran")
		}
		if !res.anggotaPG.Valid {
			res.anggotaPG = t.AnggotaID
		}
		res.nominalPokok += netto
		res.tagihanPGs = append(res.tagihanPGs, pgID)
		res.allocNominals[idKey] = netto
	}
	return res, nil
}

func resolvePesananTargetTx(ctx context.Context, qtx *db.Queries, claims *middleware.SessionClaims, idStr string) (*targetResolution, error) {
	pgID, err := response.ParsePGUUID(idStr)
	if err != nil {
		return nil, err
	}
	ord, err := qtx.GetPesananForUpdate(ctx, pgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, response.NewDomainError(http.StatusNotFound, "PESANAN_NOT_FOUND", "Pesanan tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	ownerID := response.FromPGUUID(ord.UserID)
	if claims.Role == "anggota" && ownerID != claims.UserID {
		return nil, response.NewDomainError(http.StatusForbidden, "FORBIDDEN", "Tidak berhak membayar pesanan orang lain")
	}
	if ord.Status != "menunggu_pembayaran" && ord.Status != "baru" {
		return nil, response.NewDomainError(http.StatusConflict, "PESANAN_NOT_PAYABLE", "Status pesanan tidak dapat dibayar")
	}
	if ord.ExpiresAt.Valid && time.Now().UTC().After(ord.ExpiresAt.Time) {
		return nil, response.NewDomainError(http.StatusConflict, "PESANAN_EXPIRED", "Batas waktu pembayaran pesanan telah habis")
	}
	active, err := qtx.HasActivePembayaranForPesanan(ctx, pgID)
	if err != nil {
		return nil, err
	}
	if active {
		return nil, response.NewDomainError(http.StatusConflict, "ACTIVE_PAYMENT_EXISTS", "Pesanan sudah memiliki pembayaran aktif menunggu selesai")
	}
	return &targetResolution{
		nominalPokok:   ord.TotalBayar,
		pembayarUserID: ownerID,
		anggotaPG:      ord.AnggotaID,
		pesananPG:      pgID,
	}, nil
}

func (s *Service) preparePaymentParamsTx(
	ctx context.Context,
	qtx *db.Queries,
	claims *middleware.SessionClaims,
	jenis, metode, catatan, idemKey string,
	target *targetResolution,
) (db.CreatePembayaranParams, *RekeningInfo, error) {
	// KEPUTUSAN AKUNTAN: perlakuan akuntansi selisih kode unik ditetapkan akuntan koperasi
	catatanAkt := "selisih_kode_unik_pendapatan_atau_titipan_keputusan_akuntan"
	params := db.CreatePembayaranParams{
		NomorPembayaran:  generatePayNumber(),
		IdempotencyKey:   response.ToPGText(idemKey),
		PembayarUserID:   response.ToPGUUID(target.pembayarUserID),
		AnggotaID:        target.anggotaPG,
		JenisTransaksi:   jenis,
		Metode:           metode,
		NominalPokok:     target.nominalPokok,
		NominalTotal:     target.nominalPokok,
		CatatanAkuntansi: catatanAkt,
		Status:           "menunggu_verifikasi",
		CatatanPembayar:  strings.TrimSpace(catatan),
		DibuatOleh:       response.ToPGUUID(claims.UserID),
	}
	if metode == "tunai" {
		seq, err := qtx.NextNomorKwitansi(ctx)
		if err != nil {
			return params, nil, err
		}
		kwt := fmt.Sprintf("KWT-%s-%06d", time.Now().UTC().Format("2006"), seq)
		params.NomorKwitansi = response.ToPGText(kwt)
		params.NominalDibayar = target.nominalPokok
		return params, nil, nil
	}
	rek, kodeUnik, err := s.allocateQRISCodeTx(ctx, qtx, target.pembayarUserID, target.nominalPokok)
	if err != nil {
		return params, nil, err
	}
	params.RekeningQrisID = rek.ID
	params.KodeUnik = kodeUnik
	params.NominalTotal = target.nominalPokok + int64(kodeUnik)
	params.SelisihKodeUnik = int64(kodeUnik)
	params.Status = "menunggu_pembayaran"
	params.ExpiresAt = response.ToPGTimestamptz(time.Now().UTC().Add(s.cfg.PaymentExpiry))
	info := &RekeningInfo{
		ID:           response.FromPGUUID(rek.ID),
		NamaMerchant: rek.NamaMerchant,
		NMID:         rek.Nmid,
		NamaBank:     rek.NamaBank,
		JenisPemilik: rek.JenisPemilik,
		QRISImageURL: rek.QrisImageUrl,
		QRISPayload:  rek.QrisPayload,
	}
	return params, info, nil
}

func (s *Service) allocateQRISCodeTx(ctx context.Context, qtx *db.Queries, userID uuid.UUID, nominalPokok int64) (*db.GetActiveRekeningFromPengaturanRow, int32, error) {
	uCnt, err := qtx.CountActivePendingQRISByUser(ctx, response.ToPGUUID(userID))
	if err != nil {
		return nil, 0, err
	}
	if int(uCnt) >= s.cfg.MaxPendingPerUser {
		return nil, 0, response.NewDomainError(http.StatusTooManyRequests, "TOO_MANY_PENDING_QRIS", "Terlalu banyak tagihan QRIS aktif milik Anda")
	}
	gCnt, err := qtx.CountActivePendingQRISGlobal(ctx)
	if err != nil {
		return nil, 0, err
	}
	if int(gCnt) >= s.cfg.MaxPendingGlobal {
		return nil, 0, response.NewDomainError(http.StatusServiceUnavailable, "PAYMENT_QUEUE_FULL", "Antrean pembayaran QRIS sedang penuh")
	}
	rek, err := qtx.GetActiveRekeningFromPengaturan(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, response.NewDomainError(http.StatusConflict, "NO_ACTIVE_QRIS", "Rekening QRIS aktif belum dikonfigurasi")
	}
	if err != nil {
		return nil, 0, err
	}
	if err := s.enforcePilotLimitsTx(ctx, qtx, rek, nominalPokok); err != nil {
		return nil, 0, err
	}
	peng, err := qtx.GetPengaturan(ctx)
	if err != nil {
		return nil, 0, err
	}
	usedList, err := qtx.ListUsedActiveKodeUnik(ctx, db.ListUsedActiveKodeUnikParams{
		RekeningQrisID: rek.ID,
		NominalPokok:   nominalPokok,
	})
	if err != nil {
		return nil, 0, err
	}
	kode, err := pickRandomUniqueCode(int(peng.QrisUniqueDigits), usedList)
	if err != nil {
		return nil, 0, err
	}
	return &rek, kode, nil
}

func (s *Service) enforcePilotLimitsTx(ctx context.Context, qtx *db.Queries, rek db.GetActiveRekeningFromPengaturanRow, nominalPokok int64) error {
	if rek.JenisPemilik != "perorangan" {
		return nil
	}
	if !s.cfg.AllowPersonalPilot {
		return response.NewDomainError(http.StatusForbidden, "PILOT_DISABLED", "Mode rekening pilot pribadi dinonaktifkan; gunakan rekening resmi koperasi")
	}
	if nominalPokok > s.cfg.PilotMaxPerTx {
		return response.NewDomainError(http.StatusBadRequest, "PILOT_TX_LIMIT", fmt.Sprintf("Nominal melebihi batas per transaksi pilot (%d)", s.cfg.PilotMaxPerTx))
	}
	dailySum, err := qtx.SumPilotDailyAmountByRekening(ctx, db.SumPilotDailyAmountByRekeningParams{
		RekeningQrisID: rek.ID,
		Tz:             s.cfg.Timezone,
	})
	if err != nil {
		return err
	}
	if dailySum+nominalPokok > s.cfg.PilotMaxPerDay {
		return response.NewDomainError(http.StatusConflict, "PILOT_DAILY_LIMIT", "Batas harian rekening pilot pribadi tercapai")
	}
	return nil
}

func pickRandomUniqueCode(digits int, used []int32) (int32, error) {
	maxCode := 999
	if digits >= 4 {
		maxCode = 9999
	}
	taken := make(map[int32]bool, len(used))
	for _, u := range used {
		taken[u] = true
	}
	for i := 0; i < 64; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(maxCode)))
		if err != nil {
			break
		}
		cand := int32(n.Int64() + 1) // #nosec G115 -- dibatasi maxCode <= 9999
		if !taken[cand] {
			return cand, nil
		}
	}
	for c := int32(1); c <= int32(maxCode); c++ { // #nosec G115 -- maxCode <= 9999
		if !taken[c] {
			return c, nil
		}
	}
	return 0, response.NewDomainError(http.StatusServiceUnavailable, "KODE_UNIK_EXHAUSTED", "Slot kode unik untuk nominal ini sedang penuh")
}

func saveAllocationsTx(ctx context.Context, qtx *db.Queries, pembayaranID pgtype.UUID, target *targetResolution) error {
	if target.pesananPG.Valid {
		_, err := qtx.CreatePembayaranAlokasi(ctx, db.CreatePembayaranAlokasiParams{
			PembayaranID:   pembayaranID,
			PesananID:      target.pesananPG,
			NominalAlokasi: target.nominalPokok,
		})
		return err
	}
	for _, tid := range target.tagihanPGs {
		nom := target.allocNominals[response.FromPGUUID(tid).String()]
		if _, err := qtx.CreatePembayaranAlokasi(ctx, db.CreatePembayaranAlokasiParams{
			PembayaranID:   pembayaranID,
			TagihanIuranID: tid,
			NominalAlokasi: nom,
		}); err != nil {
			return err
		}
	}
	return nil
}

func mapCreateRowFromIdem(r db.GetPembayaranByIdempotencyKeyRow) db.CreatePembayaranRow {
	return db.CreatePembayaranRow(r)
}

func generatePayNumber() string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return "PAY-" + time.Now().UTC().Format("20060102-150405") + "-" + strings.ToUpper(hex.EncodeToString(b[:]))
}

func validateProofImage(raw []byte, maxBytes int64) (string, string, error) {
	if int64(len(raw)) == 0 || int64(len(raw)) > maxBytes {
		return "", "", response.NewDomainError(http.StatusBadRequest, "INVALID_FILE_SIZE", "Ukuran berkas bukti melebihi batas atau kosong")
	}
	mime := http.DetectContentType(raw)
	ext := ""
	switch mime {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	default:
		return "", "", response.NewDomainError(http.StatusBadRequest, "INVALID_MIME_TYPE", "Bukti transfer wajib berupa gambar JPEG atau PNG asli")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8000 || cfg.Height > 8000 {
		return "", "", response.NewDomainError(http.StatusBadRequest, "INVALID_IMAGE_DIMENSIONS", "Gambar bukti rusak atau dimensi melebihi 8000x8000px")
	}
	sum := sha256.Sum256(raw)
	return ext, hex.EncodeToString(sum[:]), nil
}
