package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	db "github.com/kopdes/merah-putih/internal/db"
)

type KatalogHandler struct {
	pool *pgxpool.Pool
}

func NewKatalogHandler(pool *pgxpool.Pool) *KatalogHandler {
	return &KatalogHandler{pool: pool}
}

// HandleListStokGudang menampilkan stok saldo produk di seluruh gudang koperasi
func (h *KatalogHandler) HandleListStokGudang(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var stok []db.ListStokProdukGudangRow

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		queries := db.New(tx)
		kopUUID := pgtype.UUID{Bytes: claims.KoperasiID, Valid: true}
		rows, err := queries.ListStokProdukGudang(ctx, kopUUID)
		if err != nil {
			return err
		}
		stok = rows
		return nil
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal memuat stok: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, stok, map[string]interface{}{
		"total_varian": len(stok),
	})
}

// HandleGetHargaAnggota menghitung harga final ritel dengan diskon anggota & promo terbaik
func (h *KatalogHandler) HandleGetHargaAnggota(w http.ResponseWriter, r *http.Request) {
	produkIDStr := r.URL.Query().Get("produk_id")
	anggotaIDStr := r.URL.Query().Get("anggota_id")

	produkID, err := uuid.Parse(produkIDStr)
	if err != nil {
		response.BadRequest(w, "Parameter produk_id (UUID) tidak valid")
		return
	}

	anggotaID, err := uuid.Parse(anggotaIDStr)
	if err != nil {
		response.BadRequest(w, "Parameter anggota_id (UUID) tidak valid")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var harga db.GetHargaAkhirAnggotaDenganPromoTerbaikRow

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		queries := db.New(tx)
		row, err := queries.GetHargaAkhirAnggotaDenganPromoTerbaik(ctx, db.GetHargaAkhirAnggotaDenganPromoTerbaikParams{
			KoperasiID: pgtype.UUID{Bytes: claims.KoperasiID, Valid: true},
			ID:         pgtype.UUID{Bytes: produkID, Valid: true},
			ID_2:       pgtype.UUID{Bytes: anggotaID, Valid: true},
		})
		if err != nil {
			return err
		}
		harga = row
		return nil
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, "Harga produk atau anggota tidak ditemukan")
			return
		}
		response.InternalServerError(w, fmt.Sprintf("Gagal menghitung harga produk: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, harga)
}

type CreateProdukRequest struct {
	Nama         string `json:"nama"`
	SKU          string `json:"sku"`
	Deskripsi    string `json:"deskripsi"`
	SatuanDasar  string `json:"satuan_dasar"`
	HargaUmum    int64  `json:"harga_umum"`
	HargaAnggota int64  `json:"harga_anggota"`
	StokAwal     int64  `json:"stok_awal"`
}

// HandleCreateProduk menambahkan produk baru beserta harga awal
func (h *KatalogHandler) HandleCreateProduk(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	var req CreateProdukRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.Nama == "" || req.SKU == "" {
		response.BadRequest(w, "Nama dan SKU produk wajib diisi")
		return
	}

	if req.SatuanDasar == "" {
		req.SatuanDasar = "pcs"
	}
	if req.HargaAnggota <= 0 {
		req.HargaAnggota = req.HargaUmum
	}

	ctx := r.Context()
	var produkID uuid.UUID

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// Pastikan unit_usaha tersedia
		var unitUsahaID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM unit_usaha WHERE koperasi_id = $1 AND deleted_at IS NULL LIMIT 1;`, claims.KoperasiID).Scan(&unitUsahaID)
		if err != nil {
			err = tx.QueryRow(ctx, `
				INSERT INTO unit_usaha (koperasi_id, kode_unit, nama, kode_kbli, jenis_unit, created_by)
				VALUES ($1, 'UNIT-DEFAULT', 'Unit Toko Koperasi', '47111', 'gerai_sembako', $2)
				RETURNING id;
			`, claims.KoperasiID, claims.UserID).Scan(&unitUsahaID)
			if err != nil {
				return fmt.Errorf("gagal membuat default unit usaha: %w", err)
			}
		}

		// Pastikan kategori tersedia
		var kategoriID uuid.UUID
		err = tx.QueryRow(ctx, `SELECT id FROM kategori_produk WHERE koperasi_id = $1 AND deleted_at IS NULL LIMIT 1;`, claims.KoperasiID).Scan(&kategoriID)
		if err != nil {
			err = tx.QueryRow(ctx, `
				INSERT INTO kategori_produk (koperasi_id, kode, nama, slug)
				VALUES ($1, 'KAT-UMUM', 'Kategori Umum', 'kategori-umum')
				ON CONFLICT (koperasi_id, slug) DO UPDATE SET nama = EXCLUDED.nama
				RETURNING id;
			`, claims.KoperasiID).Scan(&kategoriID)
			if err != nil {
				return fmt.Errorf("gagal membuat default kategori produk: %w", err)
			}
		}

		// Insert produk
		slug := fmt.Sprintf("%s-%s", strings.ToLower(req.SKU), uuid.New().String()[:6])
		err = tx.QueryRow(ctx, `
			INSERT INTO produk (
				koperasi_id, unit_usaha_id, kategori_id, sku, nama, slug, deskripsi, satuan, status, created_by
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'aktif', $9)
			RETURNING id;
		`, claims.KoperasiID, unitUsahaID, kategoriID, req.SKU, req.Nama, slug, req.Deskripsi, req.SatuanDasar, claims.UserID).Scan(&produkID)
		if err != nil {
			return fmt.Errorf("gagal membuat produk: %w", err)
		}

		// Insert harga produk
		_, err = tx.Exec(ctx, `
			INSERT INTO produk_harga (
				koperasi_id, produk_id, versi, harga_umum, harga_anggota, berlaku_mulai, status_aktif, created_by
			) VALUES ($1, $2, 1, $3, $4, NOW(), true, $5);
		`, claims.KoperasiID, produkID, req.HargaUmum, req.HargaAnggota, claims.UserID)
		if err != nil {
			return fmt.Errorf("gagal mencatat harga produk: %w", err)
		}

		// Inisialisasi saldo stok awal di gudang default
		var gudangID uuid.UUID
		err = tx.QueryRow(ctx, `SELECT id FROM gudang WHERE koperasi_id = $1 AND deleted_at IS NULL LIMIT 1;`, claims.KoperasiID).Scan(&gudangID)
		if err != nil {
			err = tx.QueryRow(ctx, `
				INSERT INTO gudang (koperasi_id, unit_usaha_id, kode, nama, status_aktif)
				VALUES ($1, $2, 'GDG-DEFAULT', 'Gudang Utama Toko', true)
				RETURNING id;
			`, claims.KoperasiID, unitUsahaID).Scan(&gudangID)
		}
		if err == nil {
			stokAwal := req.StokAwal
			if stokAwal <= 0 {
				stokAwal = 50 // default stok inventori awal jika tidak ditentukan
			}
			_, _ = tx.Exec(ctx, `
				INSERT INTO stok_saldo (koperasi_id, gudang_id, produk_id, qty_tersedia, qty_dipesan)
				VALUES ($1, $2, $3, $4, 0)
				ON CONFLICT (koperasi_id, gudang_id, produk_id, varian_id) DO NOTHING;
			`, claims.KoperasiID, gudangID, produkID, stokAwal)
		}

		return nil
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal menyimpan produk: %v", err))
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{
		"id":      produkID,
		"nama":    req.Nama,
		"sku":     req.SKU,
		"message": "Produk berhasil ditambahkan ke katalog",
	})
}

// HandleListProduk menampilkan daftar produk katalog aktif dengan harga
func (h *KatalogHandler) HandleListProduk(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var produkList []map[string]interface{}

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.sku, p.nama, p.satuan, p.status::text,
			       COALESCE(ph.harga_umum, 0)::bigint, COALESCE(ph.harga_anggota, 0)::bigint
			FROM produk p
			LEFT JOIN LATERAL (
				SELECT harga_umum, harga_anggota
				FROM produk_harga
				WHERE produk_id = p.id AND status_aktif = true AND deleted_at IS NULL
				ORDER BY berlaku_mulai DESC LIMIT 1
			) ph ON true
			WHERE p.koperasi_id = $1 AND p.deleted_at IS NULL
			ORDER BY p.created_at DESC;
		`, claims.KoperasiID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				id                     uuid.UUID
				sku, nama, satuan, st  string
				hUmum, hAnggota        int64
			)
			if err := rows.Scan(&id, &sku, &nama, &satuan, &st, &hUmum, &hAnggota); err != nil {
				return err
			}
			produkList = append(produkList, map[string]interface{}{
				"id":            id,
				"sku":           sku,
				"nama":          nama,
				"satuan_dasar":  satuan,
				"status_jual":   st,
				"harga_umum":    hUmum,
				"harga_anggota": hAnggota,
			})
		}
		return rows.Err()
	})

	if err != nil {
		response.InternalServerError(w, "Gagal mengambil daftar produk katalog")
		return
	}

	if produkList == nil {
		produkList = []map[string]interface{}{}
	}

	response.JSON(w, http.StatusOK, produkList)
}

// HandleUpdateProduk memperbarui data produk
func (h *KatalogHandler) HandleUpdateProduk(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	produkID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID produk tidak valid")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	var req CreateProdukRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	ctx := r.Context()
	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE produk
			SET nama = COALESCE(NULLIF($1, ''), nama),
			    deskripsi = COALESCE(NULLIF($2, ''), deskripsi),
			    updated_at = NOW()
			WHERE id = $3 AND koperasi_id = $4 AND deleted_at IS NULL;
		`, req.Nama, req.Deskripsi, produkID, claims.KoperasiID)
		return err
	})

	if err != nil {
		response.InternalServerError(w, "Gagal memperbarui produk")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"message": "Produk berhasil diperbarui"})
}

// HandleDeleteProduk melakukan soft delete pada produk
func (h *KatalogHandler) HandleDeleteProduk(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	produkID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID produk tidak valid")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE produk SET deleted_at = NOW() WHERE id = $1 AND koperasi_id = $2;
		`, produkID, claims.KoperasiID)
		return err
	})

	if err != nil {
		response.InternalServerError(w, "Gagal menghapus produk")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"message": "Produk berhasil dihapus"})
}

type CreatePromoRequest struct {
	Kode           string  `json:"kode"`
	Nama           string  `json:"nama"`
	TipePromo      string  `json:"tipe_promo"` // 'nominal_tetap' | 'persen'
	NilaiDiskon    float64 `json:"nilai_diskon"`
	MinimalBelanja int64   `json:"minimal_belanja"`
	DurasiHari     int     `json:"durasi_hari"`
}

// HandleCreatePromo membuat promosi baru
func (h *KatalogHandler) HandleCreatePromo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	var req CreatePromoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.Kode == "" || req.Nama == "" || req.NilaiDiskon <= 0 {
		response.BadRequest(w, "Kode, nama, dan nilai diskon wajib diisi")
		return
	}
	tipePromo := "nominal"
	if req.TipePromo == "persen" {
		tipePromo = "persen"
	}
	if req.DurasiHari <= 0 {
		req.DurasiHari = 30
	}

	ctx := r.Context()
	var newID uuid.UUID

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO promo (
				koperasi_id, kode, nama, tipe_promo, nilai_diskon, cakupan, target_anggota,
				berlaku_mulai, berlaku_sampai, minimal_belanja, status_aktif, created_by
			) VALUES (
				$1, $2, $3, $4::tipe_promo_enum, $5, 'seluruh_katalog', 'semua_anggota_aktif',
				NOW(), NOW() + ($6 || ' days')::interval, $7, true, $8
			)
			RETURNING id;
		`, claims.KoperasiID, req.Kode, req.Nama, tipePromo, req.NilaiDiskon, fmt.Sprintf("%d", req.DurasiHari), req.MinimalBelanja, claims.UserID).Scan(&newID)
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal membuat promo: %v", err))
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{
		"id":      newID,
		"kode":    req.Kode,
		"message": "Promo berhasil dibuat",
	})
}

// HandleListPromo menampilkan daftar promo aktif
func (h *KatalogHandler) HandleListPromo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var promoList []map[string]interface{}

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, kode, nama, tipe_promo::text, nilai_diskon::bigint, minimal_belanja::bigint, berlaku_sampai
			FROM promo
			WHERE koperasi_id = $1 AND status_aktif = true AND berlaku_sampai > NOW() AND deleted_at IS NULL
			ORDER BY created_at DESC;
		`, claims.KoperasiID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				id         uuid.UUID
				kode, nama string
				tipe       string
				diskon     int64
				minBelanja int64
				sampai     time.Time
			)
			if err := rows.Scan(&id, &kode, &nama, &tipe, &diskon, &minBelanja, &sampai); err != nil {
				return err
			}
			promoList = append(promoList, map[string]interface{}{
				"id":              id,
				"kode":            kode,
				"nama":            nama,
				"tipe_promo":      tipe,
				"nilai_diskon":    diskon,
				"minimal_belanja": minBelanja,
				"berlaku_sampai":  sampai.Format(time.RFC3339),
			})
		}
		return rows.Err()
	})

	if err != nil {
		response.InternalServerError(w, "Gagal mengambil daftar promo")
		return
	}

	if promoList == nil {
		promoList = []map[string]interface{}{}
	}

	response.JSON(w, http.StatusOK, promoList)
}

