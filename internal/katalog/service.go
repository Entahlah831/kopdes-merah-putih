package katalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
)

type Service struct {
	pool    *pgxpool.Pool
	q       *db.Queries
	trusted *middleware.TrustedProxyMatcher
}

func NewService(pool *pgxpool.Pool, trusted []string) *Service {
	return &Service{
		pool:    pool,
		q:       db.New(pool),
		trusted: middleware.NewTrustedProxyMatcher(trusted),
	}
}

type ProdukDTO struct {
	ID                 uuid.UUID  `json:"id"`
	KategoriID         *uuid.UUID `json:"kategori_id,omitempty"`
	NamaKategori       string     `json:"nama_kategori"`
	SKU                string     `json:"sku"`
	Nama               string     `json:"nama"`
	Deskripsi          string     `json:"deskripsi"`
	Satuan             string     `json:"satuan"`
	HargaUmum          *int64     `json:"harga_umum,omitempty"`
	HargaAnggota       *int64     `json:"harga_anggota,omitempty"`
	HargaAktif         *int64     `json:"harga_aktif"`
	HargaAktifTersedia bool       `json:"harga_aktif_tersedia"`
	TierHarga          string     `json:"tier_harga"`
	Stok               int32      `json:"stok"`
	StokTertahan       int32      `json:"stok_tertahan"`
	StokTersedia       int32      `json:"stok_tersedia"`
	FotoURL            string     `json:"foto_url,omitempty"`
	Status             string     `json:"status"`
}

type WriteProdukInput struct {
	Action           string `json:"action,omitempty"`
	ID               string `json:"id,omitempty"`
	KategoriID       string `json:"kategori_id,omitempty"`
	Slug             string `json:"slug,omitempty"`
	SKU              string `json:"sku"`
	Nama             string `json:"nama"`
	Deskripsi        string `json:"deskripsi"`
	Satuan           string `json:"satuan"`
	HargaUmum        *int64 `json:"harga_umum"`
	HargaAnggota     *int64 `json:"harga_anggota"`
	Stok             int32  `json:"stok"`
	FotoURL          string `json:"foto_url"`
	Status           string `json:"status"`
	Kode             string `json:"kode,omitempty"`
	TipeDiskon       string `json:"tipe_diskon,omitempty"`
	NilaiDiskon      int64  `json:"nilai_diskon,omitempty"`
	MinBelanja       int64  `json:"min_belanja,omitempty"`
	KuotaTotal       int32  `json:"kuota_total,omitempty"`
	SekaliPerAnggota bool   `json:"sekali_per_anggota,omitempty"`
	MulaiPada        string `json:"mulai_pada,omitempty"`
	BerakhirPada     string `json:"berakhir_pada,omitempty"`
}

func (s *Service) isActiveMember(ctx context.Context, claims *middleware.SessionClaims) bool {
	if claims == nil {
		return false
	}
	ag, err := s.q.GetAnggotaByUserID(ctx, response.ToPGUUID(claims.UserID))
	if err != nil {
		return false
	}
	return ag.Status == "aktif"
}

func (s *Service) ListProduk(ctx context.Context, claims *middleware.SessionClaims, katIDStr, search string, includeNonaktif bool, lim, off int32) ([]ProdukDTO, string, error) {
	isMember := s.isActiveMember(ctx, claims)
	isStaff := claims != nil && claims.Role != "anggota"
	if includeNonaktif && !isStaff {
		includeNonaktif = false
	}
	var katPG pgtype.UUID
	if strings.TrimSpace(katIDStr) != "" {
		parsed, err := response.ParsePGUUID(katIDStr)
		if err != nil {
			return nil, "", err
		}
		katPG = parsed
	}
	rows, err := s.q.ListProduk(ctx, db.ListProdukParams{
		IncludeNonaktif: includeNonaktif,
		KategoriID:      katPG,
		Search:          strings.TrimSpace(search),
		Lim:             lim,
		Off:             off,
	})
	if err != nil {
		return nil, "", fmt.Errorf("ambil daftar produk: %w", err)
	}
	tier := "umum"
	if isMember {
		tier = "anggota"
	}
	return mapProdukRows(rows, isMember, isStaff, tier), tier, nil
}

func mapProdukRows(rows []db.ListProdukRow, isMember, isStaff bool, tier string) []ProdukDTO {
	out := make([]ProdukDTO, 0, len(rows))
	for _, r := range rows {
		tersedia := r.Stok - r.StokTertahan
		if tersedia < 0 {
			tersedia = 0
		}
		dto := ProdukDTO{
			ID:           response.FromPGUUID(r.ID),
			NamaKategori: r.NamaKategori,
			SKU:          r.Sku,
			Nama:         r.Nama,
			Deskripsi:    r.Deskripsi,
			Satuan:       r.Satuan,
			TierHarga:    tier,
			Stok:         r.Stok,
			StokTertahan: r.StokTertahan,
			StokTersedia: tersedia,
			FotoURL:      r.FotoUrl,
			Status:       r.Status,
		}
		if r.KategoriID.Valid {
			kid := response.FromPGUUID(r.KategoriID)
			dto.KategoriID = &kid
		}
		assignActivePrice(&dto, r.HargaUmum, r.HargaAnggota, isMember, isStaff)
		out = append(out, dto)
	}
	return out
}

func assignActivePrice(dto *ProdukDTO, umum, anggota pgtype.Int8, isMember, isStaff bool) {
	if isStaff {
		if umum.Valid {
			v := umum.Int64
			dto.HargaUmum = &v
		}
		if anggota.Valid {
			v := anggota.Int64
			dto.HargaAnggota = &v
		}
	}
	target := umum
	if isMember {
		target = anggota
	}
	if target.Valid {
		v := target.Int64
		dto.HargaAktif = &v
		dto.HargaAktifTersedia = true
	}
}

func (s *Service) ListKategori(ctx context.Context) ([]db.Kategori, error) {
	rows, err := s.q.ListKategoriAktif(ctx)
	if err != nil {
		return nil, fmt.Errorf("ambil kategori: %w", err)
	}
	return rows, nil
}

func (s *Service) ListPromoAktif(ctx context.Context) ([]db.ListPromoAktifRow, error) {
	rows, err := s.q.ListPromoAktif(ctx)
	if err != nil {
		return nil, fmt.Errorf("ambil promo aktif: %w", err)
	}
	return rows, nil
}

func (s *Service) HandleWrite(ctx context.Context, claims *middleware.SessionClaims, in WriteProdukInput, ip string) (int, interface{}, error) {
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action == "" && strings.TrimSpace(in.ID) != "" {
		action = "update_produk"
	}
	switch action {
	case "create_kategori":
		kat, err := s.createKategori(ctx, claims, in, ip)
		return http.StatusCreated, kat, err
	case "create_promo":
		pr, err := s.createPromo(ctx, claims, in, ip)
		return http.StatusCreated, pr, err
	case "update_produk":
		prod, err := s.updateProduk(ctx, claims, in, ip)
		return http.StatusOK, prod, err
	default:
		prod, err := s.createProduk(ctx, claims, in, ip)
		return http.StatusCreated, prod, err
	}
}

func (s *Service) createKategori(ctx context.Context, claims *middleware.SessionClaims, in WriteProdukInput, ip string) (*db.Kategori, error) {
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	nama := strings.TrimSpace(in.Nama)
	if slug == "" || nama == "" {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_KATEGORI", "Slug dan nama kategori wajib diisi")
	}
	row, err := s.q.CreateKategori(ctx, db.CreateKategoriParams{Slug: slug, Nama: nama})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, response.NewDomainError(http.StatusConflict, "DUPLICATE_SLUG", "Slug kategori sudah terdaftar")
		}
		return nil, fmt.Errorf("buat kategori: %w", err)
	}
	_ = s.q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "katalog.kategori_create",
		Entitas:     "kategori",
		EntitasID:   response.FromPGUUID(row.ID).String(),
		Ringkasan:   "Tambah kategori " + row.Slug,
		IpAddress:   ip,
	})
	return &row, nil
}

func validateProdukPrices(umum, anggota *int64, stok int32, status string) (string, error) {
	if umum != nil && *umum <= 0 {
		return "", response.NewDomainError(http.StatusBadRequest, "INVALID_PRICE", "harga_umum harus lebih besar dari 0")
	}
	if anggota != nil && *anggota <= 0 {
		return "", response.NewDomainError(http.StatusBadRequest, "INVALID_PRICE", "harga_anggota harus lebih besar dari 0")
	}
	if stok < 0 {
		return "", response.NewDomainError(http.StatusBadRequest, "INVALID_STOCK", "Stok tidak boleh negatif")
	}
	st := strings.TrimSpace(status)
	if st == "" {
		st = "aktif"
	}
	if st != "aktif" && st != "nonaktif" {
		return "", response.NewDomainError(http.StatusBadRequest, "INVALID_STATUS", "Status produk harus aktif atau nonaktif")
	}
	return st, nil
}

func (s *Service) createProduk(ctx context.Context, claims *middleware.SessionClaims, in WriteProdukInput, ip string) (*db.CreateProdukRow, error) {
	sku := strings.ToUpper(strings.TrimSpace(in.SKU))
	nama := strings.TrimSpace(in.Nama)
	satuan := strings.TrimSpace(in.Satuan)
	if sku == "" || nama == "" || satuan == "" {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_PRODUK", "SKU, nama, dan satuan wajib diisi")
	}
	st, err := validateProdukPrices(in.HargaUmum, in.HargaAnggota, in.Stok, in.Status)
	if err != nil {
		return nil, err
	}
	fotoURL, err := validateFotoURL(in.FotoURL)
	if err != nil {
		return nil, err
	}
	var katPG pgtype.UUID
	if strings.TrimSpace(in.KategoriID) != "" {
		katPG, err = response.ParsePGUUID(in.KategoriID)
		if err != nil {
			return nil, err
		}
	}
	row, err := s.q.CreateProduk(ctx, db.CreateProdukParams{
		KategoriID:   katPG,
		Sku:          sku,
		Nama:         nama,
		Deskripsi:    strings.TrimSpace(in.Deskripsi),
		Satuan:       satuan,
		HargaUmum:    response.ToPGInt8(in.HargaUmum),
		HargaAnggota: response.ToPGInt8(in.HargaAnggota),
		Stok:         in.Stok,
		FotoUrl:      fotoURL,
		Status:       st,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, response.NewDomainError(http.StatusConflict, "DUPLICATE_SKU", "SKU produk sudah digunakan")
		}
		return nil, fmt.Errorf("buat produk: %w", err)
	}
	_ = s.q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "katalog.produk_create",
		Entitas:     "produk",
		EntitasID:   response.FromPGUUID(row.ID).String(),
		Ringkasan:   "Tambah produk " + row.Sku,
		IpAddress:   ip,
	})
	return &row, nil
}

func (s *Service) updateProduk(ctx context.Context, claims *middleware.SessionClaims, in WriteProdukInput, ip string) (*db.UpdateProdukRow, error) {
	idPG, err := response.ParsePGUUID(in.ID)
	if err != nil {
		return nil, err
	}
	existing, err := s.q.GetProdukByID(ctx, idPG)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, response.NewDomainError(http.StatusNotFound, "PRODUK_NOT_FOUND", "Produk tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	nama := pickNonEmpty(in.Nama, existing.Nama)
	satuan := pickNonEmpty(in.Satuan, existing.Satuan)
	st, err := validateProdukPrices(in.HargaUmum, in.HargaAnggota, in.Stok, pickNonEmpty(in.Status, existing.Status))
	if err != nil {
		return nil, err
	}
	if in.Stok < existing.StokTertahan {
		return nil, response.NewDomainError(http.StatusConflict, "STOK_RESERVED_CONFLICT", fmt.Sprintf("Stok tidak boleh lebih kecil dari stok tertahan (%d)", existing.StokTertahan))
	}
	fotoURL, err := validateFotoURL(in.FotoURL)
	if err != nil {
		return nil, err
	}
	katPG := existing.KategoriID
	if strings.TrimSpace(in.KategoriID) != "" {
		katPG, err = response.ParsePGUUID(in.KategoriID)
		if err != nil {
			return nil, err
		}
	}
	row, err := s.q.UpdateProduk(ctx, db.UpdateProdukParams{
		KategoriID:   katPG,
		Nama:         nama,
		Deskripsi:    strings.TrimSpace(in.Deskripsi),
		Satuan:       satuan,
		HargaUmum:    response.ToPGInt8(in.HargaUmum),
		HargaAnggota: response.ToPGInt8(in.HargaAnggota),
		Stok:         in.Stok,
		FotoUrl:      fotoURL,
		Status:       st,
		ID:           idPG,
	})
	if err != nil {
		return nil, fmt.Errorf("perbarui produk: %w", err)
	}
	_ = s.q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "katalog.produk_update",
		Entitas:     "produk",
		EntitasID:   in.ID,
		Ringkasan:   "Update produk " + nama,
		IpAddress:   ip,
	})
	return &row, nil
}

func (s *Service) createPromo(ctx context.Context, claims *middleware.SessionClaims, in WriteProdukInput, ip string) (*db.CreatePromoRow, error) {
	kode := strings.ToUpper(strings.TrimSpace(in.Kode))
	nama := strings.TrimSpace(in.Nama)
	tipe := strings.TrimSpace(in.TipeDiskon)
	if kode == "" || nama == "" || (tipe != "persen" && tipe != "nominal") || in.NilaiDiskon <= 0 || in.KuotaTotal <= 0 {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_PROMO", "Kode, nama, tipe_diskon (persen|nominal), nilai_diskon > 0, dan kuota_total > 0 wajib valid")
	}
	if tipe == "persen" && in.NilaiDiskon > 100 {
		return nil, response.NewDomainError(http.StatusBadRequest, "INVALID_PROMO", "Diskon persen maksimal 100")
	}
	mulai, akhir, err := parsePromoWindow(in.MulaiPada, in.BerakhirPada)
	if err != nil {
		return nil, err
	}
	row, err := s.q.CreatePromo(ctx, db.CreatePromoParams{
		Kode:             kode,
		Nama:             nama,
		TipeDiskon:       tipe,
		NilaiDiskon:      in.NilaiDiskon,
		MinBelanja:       in.MinBelanja,
		KuotaTotal:       in.KuotaTotal,
		SekaliPerAnggota: in.SekaliPerAnggota,
		MulaiPada:        response.ToPGTimestamptz(mulai),
		BerakhirPada:     response.ToPGTimestamptz(akhir),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, response.NewDomainError(http.StatusConflict, "DUPLICATE_PROMO", "Kode promo sudah ada")
		}
		return nil, fmt.Errorf("buat promo: %w", err)
	}
	_ = s.q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID),
		ActorPeran:  claims.Role,
		Aksi:        "katalog.promo_create",
		Entitas:     "promo",
		EntitasID:   response.FromPGUUID(row.ID).String(),
		Ringkasan:   "Tambah promo " + row.Kode,
		IpAddress:   ip,
	})
	return &row, nil
}

func parsePromoWindow(mulaiStr, akhirStr string) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	mulai := now
	akhir := now.Add(30 * 24 * time.Hour)
	if strings.TrimSpace(mulaiStr) != "" {
		t, err := time.Parse(time.RFC3339, mulaiStr)
		if err != nil {
			return time.Time{}, time.Time{}, response.NewDomainError(http.StatusBadRequest, "INVALID_DATE", "mulai_pada harus RFC3339")
		}
		mulai = t
	}
	if strings.TrimSpace(akhirStr) != "" {
		t, err := time.Parse(time.RFC3339, akhirStr)
		if err != nil {
			return time.Time{}, time.Time{}, response.NewDomainError(http.StatusBadRequest, "INVALID_DATE", "berakhir_pada harus RFC3339")
		}
		akhir = t
	}
	if !akhir.After(mulai) {
		return time.Time{}, time.Time{}, response.NewDomainError(http.StatusBadRequest, "INVALID_DATE", "berakhir_pada harus setelah mulai_pada")
	}
	return mulai, akhir, nil
}

func pickNonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func validateFotoURL(raw string) (string, error) {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "", nil
	}
	if len(u) > 512 || strings.ContainsAny(u, " \t\r\n") {
		return "", response.NewDomainError(http.StatusBadRequest, "INVALID_FOTO_URL", "foto_url maksimal 512 karakter tanpa spasi")
	}
	if strings.HasPrefix(u, "/") {
		if strings.HasPrefix(u, "//") {
			return "", response.NewDomainError(http.StatusBadRequest, "INVALID_FOTO_URL", "foto_url relatif tidak boleh diawali '//'")
		}
		return u, nil
	}
	parsed, err := url.Parse(u)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || strings.TrimSpace(parsed.Host) == "" {
		return "", response.NewDomainError(http.StatusBadRequest, "INVALID_FOTO_URL", "foto_url wajib berupa path relatif '/...' atau URL https:// yang valid")
	}
	return u, nil
}
