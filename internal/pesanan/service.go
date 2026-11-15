package pesanan

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
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
	OrderExpiryMinutes int
	Timezone           string
	TrustedProxies     []string
}

type Service struct {
	pool    *pgxpool.Pool
	q       *db.Queries
	kp      security.KeyProvider
	cfg     Config
	trusted *middleware.TrustedProxyMatcher
}

func NewService(pool *pgxpool.Pool, kp security.KeyProvider, cfg Config) *Service {
	if cfg.OrderExpiryMinutes <= 0 {
		cfg.OrderExpiryMinutes = 60
	}
	return &Service{
		pool:    pool,
		q:       db.New(pool),
		kp:      kp,
		cfg:     cfg,
		trusted: middleware.NewTrustedProxyMatcher(cfg.TrustedProxies),
	}
}

type CheckoutItemInput struct {
	ProdukID  string `json:"produk_id"`
	Kuantitas int32  `json:"kuantitas"`
}

type CheckoutInput struct {
	Items             []CheckoutItemInput `json:"items"`
	MetodePengiriman  string              `json:"metode_pengiriman"`
	MetodePembayaran  string              `json:"metode_pembayaran"`
	NamaPenerima      string              `json:"nama_penerima"`
	HPPenerima        string              `json:"hp_penerima"`
	AlamatPengantaran string              `json:"alamat_pengantaran"`
	KodePromo         string              `json:"kode_promo"`
	Catatan           string              `json:"catatan"`
	TanggalAntar      string              `json:"tanggal_antar"`
	IdempotencyKey    string              `json:"idempotency_key,omitempty"`
}

type PesananItemDTO struct {
	ID          uuid.UUID `json:"id"`
	ProdukID    uuid.UUID `json:"produk_id"`
	NamaProduk  string    `json:"nama_produk"`
	Kuantitas   int32     `json:"kuantitas"`
	HargaSatuan int64     `json:"harga_satuan"`
	Subtotal    int64     `json:"subtotal"`
}

type PesananDTO struct {
	ID                uuid.UUID        `json:"id"`
	NomorPesanan      string           `json:"nomor_pesanan"`
	UserID            uuid.UUID        `json:"user_id"`
	AnggotaID         *uuid.UUID       `json:"anggota_id,omitempty"`
	MetodePengiriman  string           `json:"metode_pengiriman"`
	MetodePembayaran  string           `json:"metode_pembayaran"`
	Status            string           `json:"status"`
	Subtotal          int64            `json:"subtotal"`
	Diskon            int64            `json:"diskon"`
	Ongkir            int64            `json:"ongkir"`
	TotalBayar        int64            `json:"total_bayar"`
	NamaPenerima      string           `json:"nama_penerima"`
	HPPenerimaMasked  string           `json:"hp_penerima_masked"`
	HPPenerima        string           `json:"hp_penerima,omitempty"`
	AlamatPengantaran string           `json:"alamat_pengantaran,omitempty"`
	Catatan           string           `json:"catatan,omitempty"`
	ExpiresAt         *time.Time       `json:"expires_at,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	Items             []PesananItemDTO `json:"items,omitempty"`
}

type validatedCheckout struct {
	metodeKirim string
	metodeBayar string
	nama        string
	hpNorm      string
	alamat      string
	tglAntar    pgtype.Date
	items       []struct {
		id  uuid.UUID
		qty int32
	}
}

func validateCheckoutInput(in CheckoutInput) (*validatedCheckout, error) {
	if len(in.Items) == 0 || len(in.Items) > 50 {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_ITEMS", "Daftar item belanja harus antara 1 sampai 50 item")
	}
	kirim := strings.TrimSpace(in.MetodePengiriman)
	bayar := strings.TrimSpace(in.MetodePembayaran)
	if (kirim != "diantar" && kirim != "ambil_sendiri") || (bayar != "qris" && bayar != "tunai") {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_METHOD", "Metode pengiriman (diantar|ambil_sendiri) dan pembayaran (qris|tunai) wajib valid")
	}
	hpNorm := security.NormalizePhone(in.HPPenerima)
	nama := strings.TrimSpace(in.NamaPenerima)
	alamat := strings.TrimSpace(in.AlamatPengantaran)
	if nama == "" || len(hpNorm) < 9 {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_RECEIVER", "Nama penerima dan nomor HP penerima wajib valid")
	}
	if kirim == "diantar" && len(alamat) < 5 {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_ADDRESS", "Alamat pengantaran wajib diisi untuk pengiriman diantar")
	}
	var tglPG pgtype.Date
	if strings.TrimSpace(in.TanggalAntar) != "" {
		d, err := time.Parse("2006-01-02", strings.TrimSpace(in.TanggalAntar))
		if err != nil {
			return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_DATE", "tanggal_antar wajib YYYY-MM-DD")
		}
		tglPG = response.ToPGDate(d)
	}
	qtyByID := make(map[uuid.UUID]int32, len(in.Items))
	for _, it := range in.Items {
		pid, err := uuid.Parse(strings.TrimSpace(it.ProdukID))
		if err != nil || it.Kuantitas <= 0 {
			return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_ITEM", "produk_id dan kuantitas > 0 wajib valid")
		}
		qtyByID[pid] += it.Kuantitas
	}
	out := &validatedCheckout{metodeKirim: kirim, metodeBayar: bayar, nama: nama, hpNorm: hpNorm, alamat: alamat, tglAntar: tglPG}
	for pid, q := range qtyByID {
		out.items = append(out.items, struct {
			id  uuid.UUID
			qty int32
		}{id: pid, qty: q})
	}
	sort.Slice(out.items, func(i, j int) bool { return out.items[i].id.String() < out.items[j].id.String() })
	return out, nil
}

func (s *Service) Checkout(ctx context.Context, claims *middleware.SessionClaims, in CheckoutInput, idemKey, ip string) (*PesananDTO, bool, error) {
	if idemKey == "" {
		idemKey = strings.TrimSpace(in.IdempotencyKey)
	}
	if idemKey != "" {
		existing, err := s.q.GetPesananByIdempotencyKey(ctx, response.ToPGText(idemKey))
		if err == nil && response.FromPGUUID(existing.UserID) == claims.UserID {
			items, _ := s.q.ListPesananItems(ctx, existing.ID)
			dto := mapPesananCreated(existing, items)
			return &dto, true, nil
		}
	}
	vc, err := validateCheckoutInput(in)
	if err != nil {
		return nil, false, err
	}
	dto, err := s.executeCheckoutTx(ctx, claims, in, vc, idemKey, ip)
	return dto, false, err
}

func (s *Service) executeCheckoutTx(ctx context.Context, claims *middleware.SessionClaims, in CheckoutInput, vc *validatedCheckout, idemKey, ip string) (*PesananDTO, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	isMember, anggotaPG := s.resolveBuyerMembership(ctx, qtx, claims.UserID)
	lineItems, subtotal, err := lockAndComputeItems(ctx, qtx, vc, isMember)
	if err != nil {
		return nil, err
	}
	promoID, diskon, err := applyPromoTx(ctx, qtx, in.KodePromo, claims.UserID, anggotaPG, subtotal)
	if err != nil {
		return nil, err
	}
	peng, err := qtx.GetPengaturan(ctx)
	if err != nil {
		return nil, err
	}
	var ongkir int64
	if vc.metodeKirim == "diantar" {
		ongkir = peng.OngkirTetap
	}
	totalBayar := subtotal - diskon + ongkir

	dto, err := s.persistOrderTx(ctx, qtx, claims, vc, in.Catatan, idemKey, anggotaPG, promoID, subtotal, diskon, ongkir, totalBayar, lineItems, ip)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return dto, nil
}

func (s *Service) resolveBuyerMembership(ctx context.Context, qtx *db.Queries, userID uuid.UUID) (bool, pgtype.UUID) {
	ag, err := qtx.GetAnggotaByUserID(ctx, response.ToPGUUID(userID))
	if err == nil && ag.Status == "aktif" {
		return true, ag.ID
	}
	return false, pgtype.UUID{}
}

func lockAndComputeItems(ctx context.Context, qtx *db.Queries, vc *validatedCheckout, isMember bool) ([]db.CreatePesananItemParams, int64, error) {
	var subtotal int64
	lines := make([]db.CreatePesananItemParams, 0, len(vc.items))
	for _, it := range vc.items {
		pid := response.ToPGUUID(it.id)
		prod, err := qtx.GetProdukForUpdate(ctx, pid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, response.NewDomainError(http.StatusNotFound, "PRODUK_NOT_FOUND", "Produk tidak ditemukan")
		}
		if err != nil {
			return nil, 0, err
		}
		if prod.Status != "aktif" {
			return nil, 0, response.NewDomainError(http.StatusConflict, "PRODUK_INACTIVE", "Produk "+prod.Nama+" sedang tidak aktif")
		}
		unitPrice := prod.HargaUmum
		if isMember {
			unitPrice = prod.HargaAnggota
		}
		if !unitPrice.Valid {
			return nil, 0, response.NewDomainError(http.StatusConflict, "MISSING_ACTIVE_PRICE", "Produk "+prod.Nama+" belum memiliki harga aktif untuk tier Anda")
		}
		if prod.Stok < it.qty {
			return nil, 0, response.NewDomainError(http.StatusConflict, "OUT_OF_STOCK", "Stok produk "+prod.Nama+" tidak mencukupi")
		}
		lineSub := unitPrice.Int64 * int64(it.qty)
		subtotal += lineSub
		lines = append(lines, db.CreatePesananItemParams{
			ProdukID:    pid,
			NamaProduk:  prod.Nama,
			Kuantitas:   it.qty,
			HargaSatuan: unitPrice.Int64,
			Subtotal:    lineSub,
		})
	}
	return lines, subtotal, nil
}

func applyPromoTx(ctx context.Context, qtx *db.Queries, kodeRaw string, userID uuid.UUID, anggotaPG pgtype.UUID, subtotal int64) (pgtype.UUID, int64, error) {
	kode := strings.ToUpper(strings.TrimSpace(kodeRaw))
	if kode == "" {
		return pgtype.UUID{}, 0, nil
	}
	pr, err := qtx.GetPromoByKodeForUpdate(ctx, kode)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, 0, response.NewDomainError(http.StatusNotFound, "PROMO_NOT_FOUND", "Kode promo tidak ditemukan")
	}
	if err != nil {
		return pgtype.UUID{}, 0, err
	}
	now := time.Now().UTC()
	if !pr.Aktif || now.Before(pr.MulaiPada.Time) || now.After(pr.BerakhirPada.Time) {
		return pgtype.UUID{}, 0, response.NewDomainError(http.StatusConflict, "PROMO_INACTIVE", "Promo tidak aktif atau sudah berakhir")
	}
	if pr.KuotaTotal > 0 && pr.KuotaTerpakai >= pr.KuotaTotal {
		return pgtype.UUID{}, 0, response.NewDomainError(http.StatusConflict, "PROMO_QUOTA_EXHAUSTED", "Kuota promo sudah habis")
	}
	if subtotal < pr.MinBelanja {
		return pgtype.UUID{}, 0, response.NewDomainError(http.StatusBadRequest, "PROMO_MIN_SPEND", fmt.Sprintf("Belanja minimum promo %d", pr.MinBelanja))
	}
	if pr.SekaliPerAnggota {
		used, err := qtx.CountPromoUsageByAnggota(ctx, db.CountPromoUsageByAnggotaParams{
			PromoID:   pr.ID,
			AnggotaID: anggotaPG,
			UserID:    response.ToPGUUID(userID),
		})
		if err != nil {
			return pgtype.UUID{}, 0, err
		}
		if used > 0 {
			return pgtype.UUID{}, 0, response.NewDomainError(http.StatusConflict, "PROMO_ALREADY_USED", "Promo ini hanya dapat digunakan 1 kali per anggota")
		}
	}
	diskon := pr.NilaiDiskon
	if pr.TipeDiskon == "persen" {
		diskon = (subtotal * pr.NilaiDiskon) / 100
	}
	if diskon > subtotal {
		diskon = subtotal
	}
	if err := qtx.IncrementPromoUsed(ctx, pr.ID); err != nil {
		return pgtype.UUID{}, 0, err
	}
	return pr.ID, diskon, nil
}

func (s *Service) persistOrderTx(
	ctx context.Context,
	qtx *db.Queries,
	claims *middleware.SessionClaims,
	vc *validatedCheckout,
	catatan, idemKey string,
	anggotaPG, promoID pgtype.UUID,
	subtotal, diskon, ongkir, totalBayar int64,
	lines []db.CreatePesananItemParams,
	ip string,
) (*PesananDTO, error) {
	orderID := uuid.New()
	hpEnc, err := s.kp.EncryptWithAAD([]byte(vc.hpNorm), security.FormatAAD("pesanan", "hp_penerima", orderID.String()))
	if err != nil {
		return nil, err
	}
	alamatEnc, err := s.kp.EncryptWithAAD([]byte(vc.alamat), security.FormatAAD("pesanan", "alamat_pengantaran", orderID.String()))
	if err != nil {
		return nil, err
	}
	status := "baru"
	var expiresPG pgtype.Timestamptz
	if vc.metodeBayar == "qris" {
		status = "menunggu_pembayaran"
		expiresPG = response.ToPGTimestamptz(time.Now().UTC().Add(time.Duration(s.cfg.OrderExpiryMinutes) * time.Minute))
	}
	row, err := qtx.CreatePesanan(ctx, db.CreatePesananParams{
		ID:                   response.ToPGUUID(orderID),
		NomorPesanan:         generateOrderNumber(),
		UserID:               response.ToPGUUID(claims.UserID),
		AnggotaID:            anggotaPG,
		PromoID:              promoID,
		IdempotencyKey:       response.ToPGText(idemKey),
		MetodePengiriman:     vc.metodeKirim,
		MetodePembayaran:     vc.metodeBayar,
		Status:               status,
		Subtotal:             subtotal,
		Diskon:               diskon,
		Ongkir:               ongkir,
		TotalBayar:           totalBayar,
		NamaPenerima:         vc.nama,
		HpPenerimaEnc:        hpEnc,
		HpPenerimaMasked:     security.MaskPhone(vc.hpNorm),
		AlamatPengantaranEnc: alamatEnc,
		Catatan:              strings.TrimSpace(catatan),
		TanggalAntar:         vc.tglAntar,
		ExpiresAt:            expiresPG,
	})
	if err != nil {
		return nil, fmt.Errorf("simpan pesanan: %w", err)
	}
	return saveOrderItemsAndAudit(ctx, qtx, claims, row, lines, totalBayar, orderID, ip)
}

func saveOrderItemsAndAudit(ctx context.Context, qtx *db.Queries, claims *middleware.SessionClaims, row db.CreatePesananRow, lines []db.CreatePesananItemParams, totalBayar int64, orderID uuid.UUID, ip string) (*PesananDTO, error) {
	savedItems := make([]db.PesananItem, 0, len(lines))
	for _, li := range lines {
		li.PesananID = row.ID
		it, err := qtx.CreatePesananItem(ctx, li)
		if err != nil {
			return nil, fmt.Errorf("simpan item pesanan: %w", err)
		}
		savedItems = append(savedItems, it)
	}
	_ = qtx.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "pesanan.checkout",
		Entitas:     "pesanan",
		EntitasID:   orderID.String(),
		Ringkasan:   fmt.Sprintf("Checkout %s total %d", row.NomorPesanan, totalBayar),
		IpAddress:   ip,
	})
	dto := mapPesananFromCreate(row, savedItems)
	return &dto, nil
}

func generateOrderNumber() string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return "ORD-" + time.Now().UTC().Format("20060102-150405") + "-" + strings.ToUpper(hex.EncodeToString(b[:]))
}
